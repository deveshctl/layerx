package image

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Option configures a DockerResolver.
type Option func(*DockerResolver)

// WithClient injects a Docker API client (used for testing).
func WithClient(cli client.APIClient) Option {
	return func(r *DockerResolver) { r.cli = cli }
}

// WithPlatform pins the resolver to a specific image variant for
// multi-platform images. nil (or unset) means "use the daemon's default
// platform", which is the historical behaviour.
//
// The platform flows into ImagePull (so the right manifest is fetched on a
// cold pull), ImageSave (so only the requested variant is exported on a
// multi-platform image store), and ImageInspect (for the digest read used
// as the cache key). The pull and save paths work back to API 1.32; the
// inspect-side platform option requires API 1.49 (the moby client gates it
// with requiresVersion and returns an error on older daemons).
//
// Cache-key invariant: AnalyzeWithOptions keys cache entries by ImageID,
// which on API 1.49+ daemons returns the platform-specific image config
// digest when WithPlatform is set. Different platforms therefore land in
// different cache directories — `layerx nginx --platform linux/amd64` and
// `layerx nginx --platform linux/arm64` cannot collide on cache. On older
// daemons the inspect call errors out (requiresVersion 1.49), digestErr is
// non-nil, and AnalyzeWithOptions skips the cache entirely. Either way,
// platform-mismatched entries cannot share a cache slot.
func WithPlatform(p *ocispec.Platform) Option {
	return func(r *DockerResolver) { r.platform = p }
}

// DockerResolver resolves image layers via the Docker daemon.
type DockerResolver struct {
	cli      client.APIClient
	platform *ocispec.Platform
}

// NewDockerResolver creates a resolver connected to the local Docker daemon.
func NewDockerResolver(opts ...Option) (Resolver, error) {
	r := &DockerResolver{}
	for _, opt := range opts {
		opt(r)
	}
	if r.cli == nil {
		// API-version negotiation is on by default in moby/moby/client;
		// previously this used client.WithAPIVersionNegotiation, now a
		// deprecated no-op. Do NOT pin a version with WithVersion — that
		// disables negotiation and breaks on Docker Engine upgrades.
		cli, err := client.New(client.FromEnv)
		if err != nil {
			return nil, fmt.Errorf("cannot connect to Docker daemon: %w", err)
		}
		r.cli = cli
	}
	return r, nil
}

func NewDockerResolverWithHost(host string, opts ...Option) (Resolver, error) {
	r := &DockerResolver{}
	for _, opt := range opts {
		opt(r)
	}
	if r.cli == nil {
		cli, err := client.New(client.FromEnv, client.WithHost(host))
		if err != nil {
			return nil, fmt.Errorf("cannot connect to Docker daemon at %s: %w", host, err)
		}
		r.cli = cli
	}
	return r, nil
}

// Inspect returns lightweight image metadata without exporting the full tar.
// It does not pull the image — if the image is not local, it returns an error.
func (r *DockerResolver) Inspect(ctx context.Context, imageRef string) (*ImageMeta, error) {
	inspect, err := r.cli.ImageInspect(ctx, imageRef, r.inspectOpts()...)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if isImageInspectNotFound(err) {
			return nil, &ErrImageNotFound{Ref: imageRef, Cause: err}
		}
		if isDaemonUnreachable(err) {
			return nil, &ErrDaemonNotRunning{Cause: err}
		}
		return nil, fmt.Errorf("failed to inspect image %s: %w", imageRef, err)
	}
	return &ImageMeta{Size: inspect.Size}, nil
}

// ImageID returns the image content digest from the Docker daemon. It does
// not pull; the caller is expected to ensure the image is local first
// (AnalyzeWithProgress does this).
func (r *DockerResolver) ImageID(ctx context.Context, imageRef string) (string, error) {
	inspect, err := r.cli.ImageInspect(ctx, imageRef, r.inspectOpts()...)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		if isImageInspectNotFound(err) {
			return "", &ErrImageNotFound{Ref: imageRef, Cause: err}
		}
		if isDaemonUnreachable(err) {
			return "", &ErrDaemonNotRunning{Cause: err}
		}
		return "", fmt.Errorf("failed to inspect image %s: %w", imageRef, err)
	}
	return inspect.ID, nil
}

// inspectOpts builds the per-call ImageInspectOption slice, attaching the
// resolver's pinned --platform when set. The Manifests option is always
// requested when --platform is in use because it lets us produce the
// "available platforms" list for ErrPlatformNotInImage; daemons older than
// API 1.48 silently ignore the field.
func (r *DockerResolver) inspectOpts() []client.ImageInspectOption {
	if r.platform == nil {
		return nil
	}
	return []client.ImageInspectOption{
		client.ImageInspectWithPlatform(r.platform),
	}
}

// inspectOptsWithManifests is the variant used by enumeratePlatforms to read
// the Manifests array — independent of the platform-pin so we can list
// platforms even after a platform-mismatch failure (which already swallowed
// the platform-pinned inspect).
func (r *DockerResolver) inspectOptsWithManifests() []client.ImageInspectOption {
	return []client.ImageInspectOption{
		client.ImageInspectWithManifests(true),
	}
}

// NewExtractor creates an Extractor using this resolver's Docker client.
func (r *DockerResolver) NewExtractor() Extractor {
	return NewDockerExtractor(r.cli)
}

// Resolve fetches the image, exports it as a tar, and parses the layer list.
func (r *DockerResolver) Resolve(ctx context.Context, imageRef string) ([]Layer, error) {
	return r.ResolveWithProgress(ctx, imageRef, nil)
}

// ResolveWithProgress fetches the image with progress reporting via the channel.
func (r *DockerResolver) ResolveWithProgress(ctx context.Context, imageRef string, progress chan<- ProgressEvent) ([]Layer, error) {
	if err := r.ensureImageWithProgress(ctx, imageRef, progress); err != nil {
		return nil, err
	}

	emitProgress(progress, ProgressEvent{Phase: PhaseExporting})

	rc, err := r.cli.ImageSave(ctx, []string{imageRef}, r.saveOpts()...)
	if err != nil {
		return nil, fmt.Errorf("failed to export image %s: %w", imageRef, err)
	}
	defer rc.Close()

	emitProgress(progress, ProgressEvent{Phase: PhaseParsing})

	return parseLayers(ctx, rc)
}

// saveOpts builds the per-call ImageSaveOption slice, scoping the export to
// the resolver's pinned --platform when set. Without this, ImageSave on a
// multi-platform-image-store daemon (Docker 25+ with containerd image store)
// emits an OCI index containing every variant — and parseLayers picks the
// first manifest, which is not necessarily the one the user asked for.
//
// The save-side platform option requires daemon API 1.48 or newer; older
// daemons return an error from ImageSave that we surface verbatim. There is
// no point papering over that, because the same daemon ignored the pull-side
// platform option too, so the wrong variant is what is local — silently
// falling back would lie about which image we exported.
func (r *DockerResolver) saveOpts() []client.ImageSaveOption {
	if r.platform == nil {
		return nil
	}
	return []client.ImageSaveOption{
		client.ImageSaveWithPlatforms(*r.platform),
	}
}

// ensureImageWithProgress checks if the image exists locally; if not, pulls it with progress.
func (r *DockerResolver) ensureImageWithProgress(ctx context.Context, imageRef string, progress chan<- ProgressEvent) error {
	if isImageDigestRef(imageRef) {
		_, err := r.cli.ImageInspect(ctx, imageRef, r.inspectOpts()...)
		if err == nil {
			return nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if isImageInspectNotFound(err) {
			return &ErrImageNotFound{Ref: imageRef, Cause: err}
		}
		if isDaemonUnreachable(err) {
			return &ErrDaemonNotRunning{Cause: err}
		}
		return fmt.Errorf("failed to inspect image %s: %w", imageRef, err)
	}

	// When --platform is pinned, we cannot use the cheap "is the image
	// locally cached?" short-circuit: a previous run may have populated the
	// daemon with a different variant under the same tag. Letting a stale
	// local copy of "linux/amd64" satisfy a "linux/arm64" request would
	// silently inspect the wrong image. Skip ImageList and let ImagePull
	// run — the daemon's content store deduplicates layers, so the cost of
	// the redundant pull is at most the manifest fetch when every layer
	// blob is already present.
	if r.platform == nil {
		f := make(client.Filters).Add("reference", imageRef)
		result, err := r.cli.ImageList(ctx, client.ImageListOptions{Filters: f})
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return &ErrDaemonNotRunning{Cause: err}
		}
		if len(result.Items) > 0 {
			return nil
		}
	}

	emitProgress(progress, ProgressEvent{Phase: PhasePulling})

	rc, err := r.cli.ImagePull(ctx, imageRef, r.pullOpts())
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		// Platform-mismatch must be checked before the image-not-found
		// classifier: the daemon's "no matching manifest for X in the
		// manifest list entries" message contains the substring "manifest
		// for ", which isImageNotFoundMessage also matches. Without this
		// ordering, a bad --platform on a containerd image store (where
		// every pull goes through the daemon, no local short-circuit) is
		// reported as "image not found" instead of "platform not found".
		if isPlatformPullFailure(err.Error()) {
			return r.classifyPlatformMissing(ctx, imageRef, err)
		}
		if isImageNotFoundMessage(err.Error()) {
			return r.classifyPullNotFound(ctx, imageRef, err)
		}
		return &ErrPullFailed{Ref: imageRef, Cause: err}
	}
	defer rc.Close()

	if err := r.streamPullProgress(ctx, rc, progress); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if isPlatformPullFailure(err.Error()) {
			return r.classifyPlatformMissing(ctx, imageRef, err)
		}
		if isImageNotFoundMessage(err.Error()) {
			return r.classifyPullNotFound(ctx, imageRef, err)
		}
		return &ErrPullFailed{Ref: imageRef, Cause: err}
	}
	return nil
}

// pullOpts builds the ImagePullOptions for the active resolve, attaching
// the pinned --platform when set. The daemon resolves the manifest list and
// pulls only the matching manifest's layer blobs.
func (r *DockerResolver) pullOpts() client.ImagePullOptions {
	opts := client.ImagePullOptions{}
	if r.platform != nil {
		opts.Platforms = []ocispec.Platform{*r.platform}
	}
	return opts
}

// classifyPullNotFound disambiguates a "not found" pull error: the image
// reference itself doesn't exist (returned as ErrImageNotFound, the historic
// behaviour), versus the image exists but lacks the requested platform
// variant (returned as ErrPlatformNotInImage with an Available list when we
// can recover one). Without --platform pinned, every "not found" stays
// ErrImageNotFound so the new branch cannot regress existing callers.
func (r *DockerResolver) classifyPullNotFound(ctx context.Context, imageRef string, cause error) error {
	if r.platform == nil {
		return &ErrImageNotFound{Ref: imageRef, Cause: cause}
	}
	// Try a platform-less inspect to learn whether the image exists at all.
	// A fresh probe avoids depending on whatever state the failed pull left.
	if _, err := r.cli.ImageInspect(ctx, imageRef); err == nil {
		// Image is present locally without the requested platform — this is
		// the "asked for arm64 on an amd64-only image" case.
		return r.classifyPlatformMissing(ctx, imageRef, cause)
	}
	return &ErrImageNotFound{Ref: imageRef, Cause: cause}
}

// classifyPlatformMissing builds ErrPlatformNotInImage with a best-effort
// list of available platforms. A daemon that supports the multi-platform
// image store (Docker 25+ with containerd snapshotter) returns a Manifests
// array on inspect when asked; older daemons return nothing useful and we
// fall back to a list with a single Architecture/OS/Variant entry, or an
// empty list when even that is unavailable.
func (r *DockerResolver) classifyPlatformMissing(ctx context.Context, imageRef string, cause error) error {
	requested := FormatPlatform(r.platform)
	available, localOnly := r.enumeratePlatforms(ctx, imageRef)
	if requested == "" {
		// The platform pin became empty between the request and the error.
		// Don't lie about which platform was asked for; surface the cause.
		return &ErrPullFailed{Ref: imageRef, Cause: cause}
	}
	err := &ErrPlatformNotInImage{
		Ref:       imageRef,
		Requested: requested,
		Available: available,
	}
	if localOnly {
		// The daemon could not give us the manifest list, so what we have is
		// just whatever variant was already pulled. Disclaim that explicitly
		// — without it, a user who asks for linux/arm64dd on a daemon that has
		// only linux/amd64 cached sees "Available: linux/amd64" and reasonably
		// (but wrongly) concludes layerx thinks linux/arm64 doesn't exist.
		err.AvailableSource = "locally cached only — enable the daemon's containerd image store to see every platform this image advertises"
	}
	return err
}

// enumeratePlatforms reads the multi-platform manifest list for imageRef and
// returns the available "os/arch[/variant]" strings in declaration order. The
// second return reports whether the list came from the local-cache fallback
// (true) rather than the manifest list (false): callers use that to disclaim
// an incomplete list in user-facing errors. Falls back to a single-element
// slice with the image's own Architecture/OS/Variant when the daemon doesn't
// expose a Manifests array, and to nil when even that read fails. Errors are
// intentionally swallowed — this is best-effort context for an error message,
// not a failure path.
func (r *DockerResolver) enumeratePlatforms(ctx context.Context, imageRef string) ([]string, bool) {
	inspect, err := r.cli.ImageInspect(ctx, imageRef, r.inspectOptsWithManifests()...)
	if err != nil {
		return nil, false
	}
	var out []string
	for _, m := range inspect.Manifests {
		if m.ImageData == nil {
			continue
		}
		s := FormatPlatform(&m.ImageData.Platform)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	if len(out) > 0 {
		return out, false
	}
	if inspect.Architecture != "" {
		single := ocispec.Platform{
			OS:           inspect.Os,
			Architecture: inspect.Architecture,
			Variant:      inspect.Variant,
		}
		if s := FormatPlatform(&single); s != "" {
			return []string{s}, true
		}
	}
	return nil, false
}

// isPlatformPullFailure matches the daemon-side phrases that mean "the
// requested platform is not available in this image". The Docker daemon
// surfaces this as a streaming errorDetail event during pull — separate
// from the registry-level "manifest unknown" / "not found" set covered
// by isImageNotFoundMessage.
func isPlatformPullFailure(s string) bool {
	s = strings.ToLower(s)
	for _, needle := range []string{
		"no matching manifest",
		"image does not exist for the requested platform",
		"platform mismatch",
	} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// isImageDigestRef reports whether ref is a content-addressable image
// identifier (sha256:<64hex>) rather than a name/tag. The Docker reference
// filter does not match content digests, so callers must use ImageInspect
// directly for IDs. Validating the hex tail prevents a malformed ref that
// happens to start with "sha256:" from silently bypassing the normal path.
func isImageDigestRef(ref string) bool {
	const prefix = "sha256:"
	if !strings.HasPrefix(ref, prefix) {
		return false
	}
	hex := ref[len(prefix):]
	if len(hex) != 64 {
		return false
	}
	for _, c := range hex {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// streamPullProgress reads JSON pull events and sends progress updates.
// Returns an error if the stream reports a transport-level failure (decode
// error, daemon hang-up) or an in-band errorDetail event (auth failure,
// "manifest not found", registry 5xx). The Docker daemon returns HTTP 200
// for /images/create even when the pull fails, encoding the failure as an
// inline JSON errorDetail message — so the in-band check is required to
// avoid reporting a failed pull as success.
//
// progress may be nil; in that case the stream is still consumed and the
// errorDetail check still runs, but no progress events are emitted.
func (r *DockerResolver) streamPullProgress(ctx context.Context, rc client.ImagePullResponse, progress chan<- ProgressEvent) error {
	type layerProgress struct {
		current int64
		total   int64
		done    bool
	}
	layers := make(map[string]*layerProgress)

	for msg, err := range rc.JSONMessages(ctx) {
		if err != nil {
			return err
		}
		// In-band failure (auth, manifest-not-found, registry 5xx): the
		// daemon returns HTTP 200 and encodes the failure as an
		// errorDetail JSON message. Surface as a real error rather than
		// reporting the pull as success. When errorDetail.Message is
		// empty, fall back to the daemon-supplied Code and Status
		// (e.g. Code=401 with Status="Pulling from private/foo") so the
		// user gets some diagnostic content even on registries that
		// emit terse error events.
		if msg.Error != nil {
			if msg.Error.Message != "" {
				return errors.New(msg.Error.Message)
			}
			parts := []string{"pull failed"}
			if msg.Error.Code != 0 {
				parts = append(parts, fmt.Sprintf("registry error code %d", msg.Error.Code))
			}
			if msg.Status != "" {
				parts = append(parts, "last status: "+msg.Status)
			}
			if len(parts) == 1 {
				parts = append(parts, "registry returned an empty error")
			}
			return errors.New(strings.Join(parts, "; "))
		}
		if msg.ID == "" {
			continue
		}

		// progress==nil callers (CI, JSON export) only need the
		// errorDetail check above. Skip the per-layer bookkeeping below
		// — pre-diff, those callers used io.Copy(io.Discard) and avoided
		// the work entirely; preserve that for chatty large pulls.
		if progress == nil {
			continue
		}

		lp, exists := layers[msg.ID]
		if !exists {
			lp = &layerProgress{}
			layers[msg.ID] = lp
		}

		switch msg.Status {
		case "Download complete", "Pull complete":
			lp.done = true
			if lp.total > 0 {
				lp.current = lp.total
			}
		case "Downloading":
			if msg.Progress != nil {
				lp.current = msg.Progress.Current
				lp.total = msg.Progress.Total
			}
		}

		var totalBytes, currentBytes int64
		done := 0
		for _, l := range layers {
			currentBytes += l.current
			totalBytes += l.total
			if l.done {
				done++
			}
		}

		emitProgress(progress, ProgressEvent{
			Phase:       PhasePulling,
			LayersDone:  done,
			LayersTotal: len(layers),
			BytesCurr:   currentBytes,
			BytesTotal:  totalBytes,
		})
	}
	return nil
}

// parseLayers reads a Docker image tar archive and returns the layer list
// with ID, Size, Command, and Tree populated.
// Supports both legacy Docker format (config as <sha>.json at root) and
// OCI format (config as blobs/sha256/<digest>).
//
// Memory bound: the input stream is spooled to a temp file, then walked in
// two passes. Pass 1 reads small metadata (manifest.json, config). Pass 2
// streams each layer through decompress + ParseLayerTar, dropping the buffer
// before the next layer. Peak heap is bounded by the largest single layer
// rather than the sum of all blobs in the archive.
func parseLayers(ctx context.Context, r io.Reader) ([]Layer, error) {
	spool, err := os.CreateTemp("", "layerx-resolve-*.tar")
	if err != nil {
		return nil, &ErrArchiveInfra{Op: "creating temp spool", Cause: err}
	}
	spoolPath := spool.Name()
	defer os.Remove(spoolPath)
	defer spool.Close()

	if _, err := copyCtx(ctx, spool, r); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, &ErrArchiveInfra{Op: "spooling image archive", Cause: err}
	}

	// Pass 1: collect manifest.json, root *.json (legacy config), and a
	// header map of every entry's declared size. Bodies of layer/blob
	// entries are streamed past without buffering.
	manifestData, rootJSON, headers, err := scanResolveMetadata(spool)
	if err != nil {
		return nil, err
	}

	if manifestData == nil {
		return nil, fmt.Errorf("invalid image archive: manifest.json not found")
	}

	var manifests []dockerManifest
	if err := json.Unmarshal(manifestData, &manifests); err != nil {
		return nil, fmt.Errorf("invalid image archive: cannot parse manifest: %w", err)
	}
	if len(manifests) == 0 {
		return nil, fmt.Errorf("invalid image archive: empty manifest")
	}
	manifest := manifests[0]

	// Resolve config: legacy (<sha>.json at root) or OCI (blobs/sha256/...).
	configData, ok := rootJSON[manifest.Config]
	if !ok {
		configData, err = readEntryFromSpool(spool, manifest.Config)
		if err != nil {
			return nil, fmt.Errorf("invalid image archive: config %s not found: %w", manifest.Config, err)
		}
	}

	var config imageConfig
	if err := json.Unmarshal(configData, &config); err != nil {
		return nil, fmt.Errorf("invalid image archive: cannot parse config: %w", err)
	}

	var commands []string
	for _, entry := range config.History {
		if !entry.EmptyLayer {
			commands = append(commands, entry.CreatedBy)
		}
	}

	layers := make([]Layer, len(manifest.Layers))
	for i, layerPath := range manifest.Layers {
		layers[i] = Layer{
			Index: i,
			ID:    extractShortID(layerPath),
			Size:  headers[layerPath],
		}
		if i < len(commands) {
			layers[i].Command = commands[i]
		}
	}

	// Pass 2: stream each layer one at a time. tar.Reader is an io.Reader
	// bounded to the current entry, so wrapping it with the streaming
	// gzip-detector lets ParseLayerTar consume the layer without buffering
	// the full compressed blob. Peak heap = the FileTree of the layer being
	// parsed, not the gzip bytes feeding it.
	keep := make(map[string]int, len(manifest.Layers))
	for i, p := range manifest.Layers {
		keep[p] = i
	}

	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek spool: %w", err)
	}
	tr := tar.NewReader(spool)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading image archive: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		idx, want := keep[hdr.Name]
		if !want {
			continue
		}
		if hdr.Size == 0 {
			continue
		}
		dec, err := decompressIfGzipStream(tr)
		if err != nil {
			return nil, fmt.Errorf("decompressing layer %s: %w", hdr.Name, err)
		}
		tree, parseErr := ParseLayerTar(dec)
		dec.Close()
		if parseErr != nil {
			return nil, fmt.Errorf("parsing layer %s: %w", hdr.Name, parseErr)
		}
		layers[idx].Tree = tree
	}

	return layers, nil
}

// scanResolveMetadata walks the spool once, returning manifest.json bytes,
// any root-level *.json blobs (legacy config payloads), and a header map of
// declared sizes for every entry. Layer / blob bodies are streamed past
// without buffering.
func scanResolveMetadata(spool *os.File) ([]byte, map[string][]byte, map[string]int64, error) {
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return nil, nil, nil, fmt.Errorf("seek spool: %w", err)
	}
	tr := tar.NewReader(spool)

	var manifestData []byte
	rootJSON := make(map[string][]byte)
	headers := make(map[string]int64)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, nil, fmt.Errorf("reading image archive: %w", err)
		}
		headers[hdr.Name] = hdr.Size

		switch {
		case hdr.Name == "manifest.json":
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("reading manifest.json: %w", err)
			}
			manifestData = data
		case strings.HasSuffix(hdr.Name, ".json") && !strings.Contains(hdr.Name, "/"):
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("reading %s: %w", hdr.Name, err)
			}
			rootJSON[hdr.Name] = data
		default:
			// Layer/blob body — skip without buffering.
		}
	}
	return manifestData, rootJSON, headers, nil
}

// readEntryFromSpool walks the spool and returns the bytes of a single named
// entry. Used for OCI configs that live under blobs/sha256/.
func readEntryFromSpool(spool *os.File, name string) ([]byte, error) {
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek spool: %w", err)
	}
	tr := tar.NewReader(spool)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("entry not found: %s", name)
		}
		if err != nil {
			return nil, fmt.Errorf("reading image archive: %w", err)
		}
		if hdr.Name == name {
			return io.ReadAll(tr)
		}
	}
}

// decompressIfGzip returns a reader that decompresses gzip data, or wraps raw
// bytes directly. Docker 25+ OCI format stores layer blobs as gzip-compressed tar.
// Callers must Close the returned reader.
func decompressIfGzip(data []byte) (io.ReadCloser, error) {
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		return gzip.NewReader(bytes.NewReader(data))
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// decompressIfGzipStream is the streaming variant of decompressIfGzip. It
// peeks the first 2 bytes via a bufio.Reader and gzip-wraps the rest if the
// gzip magic is present, otherwise returns the buffered reader unchanged.
// Callers must Close the returned reader.
//
// Use this for layer blobs that may be arbitrarily large (multi-GB ML model
// images): it avoids buffering the full compressed blob in memory before
// decompression.
func decompressIfGzipStream(r io.Reader) (io.ReadCloser, error) {
	br := bufio.NewReader(r)
	magic, err := br.Peek(2)
	// Peek returns io.EOF on a totally empty body and io.ErrUnexpectedEOF on
	// a 1-byte body. Both are legitimate for a tiny non-gzip layer (e.g. an
	// empty placeholder); treat them as "not gzip, just hand back the bytes".
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	if len(magic) >= 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		return gzip.NewReader(br)
	}
	return io.NopCloser(br), nil
}

type dockerManifest struct {
	Config string   `json:"Config"`
	Layers []string `json:"Layers"`
}

type imageConfig struct {
	OS           string               `json:"os"`
	Architecture string               `json:"architecture"`
	Variant      string               `json:"variant"`
	History      []configHistoryEntry `json:"history"`
}

type configHistoryEntry struct {
	CreatedBy  string `json:"created_by"`
	EmptyLayer bool   `json:"empty_layer"`
}

// extractShortID derives a 12-char short ID from a layer path.
// Handles both legacy format ("aabbcc.../layer.tar") and
// OCI format ("blobs/sha256/aabbcc...").
func extractShortID(layerPath string) string {
	parts := strings.Split(layerPath, "/")
	var id string
	if len(parts) >= 3 && parts[0] == "blobs" {
		id = parts[2]
	} else {
		id = parts[0]
	}
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// isImageNotFoundMessage classifies a registry pull error as "ref does not
// exist" (vs network / auth / 5xx). Conservative substring match against the
// phrases emitted by Docker Hub, GHCR, ECR, and GCR pull paths.
func isImageNotFoundMessage(s string) bool {
	s = strings.ToLower(s)
	for _, needle := range []string{
		"manifest unknown",
		"manifest for ",
		"not found",
		"repository does not exist",
		"pull access denied",
	} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// isDaemonUnreachable substring-matches moby connection-failure messages.
// Substring match works on every supported transport (unix socket, Windows
// named pipe, TCP) without depending on internal SDK types.
func isDaemonUnreachable(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, needle := range []string{
		"cannot connect to the docker daemon",
		"is the docker daemon running",
		"docker daemon is not running",
		"no such file or directory",
		"connection refused",
		"connect: permission denied",
		"file does not exist",
	} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// isImageInspectNotFound matches the daemon's canonical "no such image"
// phrase. moby/moby/client v0.4.1 does not export an IsErrNotFound helper.
func isImageInspectNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "no such image")
}

