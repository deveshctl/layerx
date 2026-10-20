package image

import (
	"os"
	"path/filepath"
	"testing"
)

// Fuzz targets for the tar/archive parsers. layerx parses untrusted tar
// bytes (from `docker save` or user-supplied archives), so the bug class
// fuzzing finds — panics on malformed headers, infinite loops on degenerate
// streams, allocations driven by attacker-controlled sizes — maps directly
// onto the threat model. Each target asserts process survival, never a
// specific output: the contract is "no panic, no OOM, terminate within
// the deadline" for any byte string.

// writeBytesToTempFile materialises fuzz input as a real *os.File so the
// spool-walking parsers (which seek) get the same shape they see in
// production. The harness uses t.TempDir so a corpus of inputs that crashes
// the process does not leave debris between iterations.
func writeBytesToTempFile(t *testing.T, data []byte) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fuzz.tar")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// FuzzReadImageArchiveMetadata exercises the production metadata reader for
// Docker-save and standalone OCI archives, including descriptor parsing and
// the OCI fallback. Malformed input must return an error, never panic or hang.
func FuzzReadImageArchiveMetadata(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("not a tar"))
	// A minimal-shaped tar: blank-padded buffer ends up failing tar.Next
	// quickly, exercising the early-EOF path.
	f.Add(make([]byte, 1024))
	f.Add(buildTar(f, map[string][]byte{
		"manifest.json": []byte(`[{"Config":"config.json","Layers":["layer.tar"]}]`),
		"config.json":   []byte(`{}`),
		"layer.tar":     []byte("payload"),
	}).Bytes())
	f.Add(buildTar(f, map[string][]byte{
		"index.json":        []byte(`{"manifests":[{"digest":"sha256:aaaa"}]}`),
		"blobs/sha256/aaaa": []byte(`{"config":{"digest":"sha256:bbbb"},"layers":[{"digest":"sha256:cccc"}]}`),
	}).Bytes())

	f.Fuzz(func(t *testing.T, data []byte) {
		spool := writeBytesToTempFile(t, data)
		// Discard returns deliberately. The assertion is "does not panic" —
		// any returned error (including the "manifest.json not found"
		// sentinel) is acceptable behaviour.
		_, _ = readImageArchiveMetadata(spool)
	})
}

// FuzzScanResolveMetadata exercises the production outer-tar scan, including
// metadata-size and entry-count limits. Malformed input must not panic or hang.
func FuzzScanResolveMetadata(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("garbage"))
	f.Add(make([]byte, 1024))
	f.Add(buildTar(f, map[string][]byte{
		"manifest.json": []byte(`[]`),
		"config.json":   []byte(`{}`),
		"layer.tar":     []byte("payload"),
	}).Bytes())

	f.Fuzz(func(t *testing.T, data []byte) {
		spool := writeBytesToTempFile(t, data)
		_, _, _, _ = scanResolveMetadata(spool)
	})
}

// FuzzFindFileInLayer exercises the per-layer tar walker. Inputs include
// the filePath argument so the fuzzer can drive whiteout / opaque-whiteout
// branches and the path-not-regular bailout. Bounded by MaxSaveSize via
// the production code path; the assertion remains "no panic, no hang".
func FuzzFindFileInLayer(f *testing.F) {
	f.Add([]byte{}, "etc/passwd")
	f.Add([]byte("plain"), "")
	f.Add(make([]byte, 1024), "etc/.wh.passwd")

	f.Fuzz(func(t *testing.T, layerBytes []byte, filePath string) {
		_, _, _ = findFileInLayer(layerBytes, filePath)
	})
}
