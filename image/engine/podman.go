package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// PodmanResolver reports the endpoint the Podman CLI would talk to, given
// the current process environment and ~/.config/containers/ layout.
//
// Resolution order, mirroring `podman` itself:
//  1. CONTAINER_HOST — Podman's explicit override
//  2. DOCKER_HOST — accepted for back-compat with layerx pre-context users
//     who scripted DOCKER_HOST=$(podman system connection list ...) already
//  3. CONTAINER_CONNECTION — env-selected connection name
//  4. Default connection in ~/.config/containers/podman-connections.json
//     (Podman 4.4+; the current source of truth)
//  5. active_service in ~/.config/containers/containers.conf (older Podman)
//  6. no active endpoint (Endpoint{}, nil)
//
// SSH-only connection URIs are returned verbatim. layerx's moby/moby/client
// consumer either dials them via the SDK's ssh:// helper or errors out at
// connect time — either way the user sees a real message pointing at the
// exact URI, not a silent connection to the local socket.
type PodmanResolver struct {
	env   env
	files files
}

func NewPodmanResolver() *PodmanResolver {
	return &PodmanResolver{env: os.Getenv, files: osFiles{}}
}

// newPodmanResolverWithDeps is the test seam.
func newPodmanResolverWithDeps(e env, f files) *PodmanResolver {
	return &PodmanResolver{env: e, files: f}
}

func (r *PodmanResolver) Name() string { return "podman" }

func (r *PodmanResolver) Resolve() (Endpoint, error) {
	if h := r.env("CONTAINER_HOST"); h != "" {
		return Endpoint{Host: h, Source: "env:CONTAINER_HOST"}, nil
	}

	// Read whichever file exists. json first, since 4.4+ writes both and
	// treats the json file as authoritative.
	conns, defaultName, cfgSource, cfgErr := r.loadConnections()
	if cfgErr != nil {
		return Endpoint{}, cfgErr
	}

	requested := r.env("CONTAINER_CONNECTION")
	origin := "env:CONTAINER_CONNECTION"
	if requested == "" {
		requested = defaultName
		origin = cfgSource // e.g. "podman-connections.json" or "containers.conf"
	}
	if requested != "" {
		uri, ok := conns[requested]
		if !ok {
			names := make([]string, 0, len(conns))
			for k := range conns {
				names = append(names, k)
			}
			nameSort(names)
			return Endpoint{}, &ErrConnectionNotFound{
				Engine:    "podman",
				Requested: requested,
				Available: names,
				Origin:    origin,
			}
		}
		return Endpoint{Host: uri, Source: "podman-connection:" + requested}, nil
	}

	// DOCKER_HOST as a last-resort back-compat fallback: only consulted when
	// no Podman-specific env var or config file named a connection.
	if h := r.env("DOCKER_HOST"); h != "" {
		return Endpoint{Host: h, Source: "env:DOCKER_HOST"}, nil
	}

	return Endpoint{}, nil
}

// loadConnections reads whichever of the two Podman config files exists on
// disk and returns (name → URI, default name, source label, err).
//
// Podman 4.4+ writes podman-connections.json; older versions (and admin-
// installed system configs) use containers.conf. When both are present the
// JSON file wins, mirroring Podman's own read order.
func (r *PodmanResolver) loadConnections() (map[string]string, string, string, error) {
	jsonPath := configPath(r.files, "containers", "podman-connections.json")
	if jsonPath != "" {
		data, err := r.files.readFile(jsonPath)
		switch {
		case err == nil:
			conns, def, perr := parsePodmanConnectionsJSON(data, jsonPath)
			if perr != nil {
				return nil, "", "", perr
			}
			return conns, def, "podman-connections.json", nil
		case !isNotExist(err):
			return nil, "", "", err
		}
	}

	confPath := configPath(r.files, "containers", "containers.conf")
	if confPath != "" {
		data, err := r.files.readFile(confPath)
		switch {
		case err == nil:
			conns, def, perr := parsePodmanContainersConf(data, confPath)
			if perr != nil {
				return nil, "", "", perr
			}
			return conns, def, "containers.conf", nil
		case !isNotExist(err):
			return nil, "", "", err
		}
	}
	return map[string]string{}, "", "", nil
}

// podmanConnectionsFile is the on-disk shape of podman-connections.json,
// scoped to what we consume. Podman writes additional fields (Identity,
// IsMachine, Default farm entries) that we ignore.
type podmanConnectionsFile struct {
	Connection struct {
		Default     string                            `json:"Default"`
		Connections map[string]podmanConnectionsEntry `json:"Connections"`
	} `json:"Connection"`
}

type podmanConnectionsEntry struct {
	URI string `json:"URI"`
}

func parsePodmanConnectionsJSON(data []byte, path string) (map[string]string, string, error) {
	var f podmanConnectionsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, "", &ErrConfigMalformed{Engine: "podman", Path: path, Cause: err}
	}
	out := make(map[string]string, len(f.Connection.Connections))
	for name, entry := range f.Connection.Connections {
		if entry.URI != "" {
			out[name] = entry.URI
		}
	}
	return out, f.Connection.Default, nil
}

// parsePodmanContainersConf reads the minimal subset of containers.conf
// layerx needs: the [engine] active_service key and the
// [engine.service_destinations.NAME] uri key for each named destination.
//
// This is a scoped INI/TOML-shape parser rather than a full TOML decoder
// on purpose: containers.conf is stable in shape at the level we consume,
// the surface is a few lines, and pulling in a TOML dep for this is
// disproportionate. The parser recognises quoted-string values, bracketed
// section headers, and # / ; line comments — everything else in the file
// is silently ignored, so any additional stanzas or future keys pass
// through without triggering a malformed-config error.
func parsePodmanContainersConf(data []byte, path string) (map[string]string, string, error) {
	const (
		enginePrefix = "engine"
		destsPrefix  = "engine.service_destinations."
	)
	conns := map[string]string{}
	var (
		defaultName string
		section     string
	)
	for lineNo, raw := range splitLines(data) {
		line := strings.TrimSpace(stripComment(raw))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			// Section names may be quoted per-segment (e.g.
			// [engine.service_destinations."my-connection"]); strip
			// segment-level quoting so we match on the bare name.
			section = unquoteSectionName(section)
			continue
		}
		key, value, ok := splitKeyValue(line)
		if !ok {
			// Silently tolerate lines we cannot key/value split (e.g.
			// TOML array continuations from unrelated sections). Real
			// containers.conf keys we care about are all simple
			// key = "value" pairs.
			continue
		}
		unquoted, err := unquoteString(value)
		if err != nil {
			return nil, "", &ErrConfigMalformed{
				Engine: "podman",
				Path:   path,
				Cause:  fmt.Errorf("line %d: %w", lineNo+1, err),
			}
		}
		switch {
		case section == enginePrefix && key == "active_service":
			defaultName = unquoted
		case strings.HasPrefix(section, destsPrefix) && key == "uri":
			name := strings.TrimPrefix(section, destsPrefix)
			if name != "" && unquoted != "" {
				conns[name] = unquoted
			}
		}
	}
	return conns, defaultName, nil
}

// splitLines splits data on \n (dropping \r before). Zero allocations for
// small config files — data is walked once with a start-index cursor.
func splitLines(data []byte) []string {
	// Config files are small (< 100 KiB), so materialising the slice is
	// cheap and simplifies the range-loop in the caller. bytes.Split
	// would work but forces the caller to convert per-line.
	out := make([]string, 0, 32)
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			end := i
			if end > start && data[end-1] == '\r' {
				end--
			}
			out = append(out, string(data[start:end]))
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, string(data[start:]))
	}
	return out
}

// stripComment removes an unquoted # or ; comment tail from line. Quotes
// span at most one comment marker; this is a scoped parser, not a full
// TOML tokenizer.
func stripComment(line string) string {
	inQuote := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '"' && (i == 0 || line[i-1] != '\\') {
			inQuote = !inQuote
			continue
		}
		if !inQuote && (c == '#' || c == ';') {
			return line[:i]
		}
	}
	return line
}

func splitKeyValue(line string) (string, string, bool) {
	eq := strings.IndexByte(line, '=')
	if eq < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:eq]), strings.TrimSpace(line[eq+1:]), true
}

// unquoteString peels one layer of "..." quoting, honouring \\ and \" as
// escapes. Bare values (no quotes) pass through unchanged so we tolerate
// containers.conf files hand-edited without quotes.
func unquoteString(s string) (string, error) {
	if len(s) == 0 {
		return "", nil
	}
	if s[0] != '"' {
		// TOML also permits triple-quoted / literal strings; a real
		// Podman-generated containers.conf never uses those for URIs,
		// so a bare value is the only shape we need to accept beyond
		// standard double quotes.
		return s, nil
	}
	if len(s) < 2 || s[len(s)-1] != '"' {
		return "", fmt.Errorf("unterminated string %q", s)
	}
	inner := s[1 : len(s)-1]
	var b strings.Builder
	b.Grow(len(inner))
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		if i+1 >= len(inner) {
			return "", fmt.Errorf("dangling escape in %q", s)
		}
		i++
		switch inner[i] {
		case '\\', '"':
			b.WriteByte(inner[i])
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		default:
			// Unknown escape: preserve verbatim rather than error,
			// matching TOML's forgiving stance for values we don't
			// interpret.
			b.WriteByte('\\')
			b.WriteByte(inner[i])
		}
	}
	return b.String(), nil
}

// unquoteSectionName strips one layer of quoting from each dot-segment of
// section, so [engine.service_destinations."my-remote"] resolves to the
// same section identifier as [engine.service_destinations.my-remote].
func unquoteSectionName(section string) string {
	if !strings.Contains(section, `"`) {
		return section
	}
	parts := strings.Split(section, ".")
	for i, p := range parts {
		if len(p) >= 2 && p[0] == '"' && p[len(p)-1] == '"' {
			parts[i] = p[1 : len(p)-1]
		}
	}
	return strings.Join(parts, ".")
}
