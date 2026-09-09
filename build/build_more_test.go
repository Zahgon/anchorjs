package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests exercise the file-orchestration helpers in build.go against
// temporary directories so the build tool's non-banner logic is covered
// natively (the banner formatting is already covered by build_test.go).

func TestTrimNewlineStripsTrailingLineEndings(t *testing.T) {
	cases := map[string]string{
		"value\n":     "value",
		"value\r\n":   "value",
		"value":       "value",
		"value\n\r":   "value",
		"a\nb\n":      "a\nb",
		"   spaced\n": "   spaced",
	}
	for in, want := range cases {
		if got := trimNewline(in); got != want {
			t.Errorf("trimNewline(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestCopyFileDuplicatesContents(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "nested", "dst.bin")
	payload := []byte("arbitrary bytes \x00\x01\x02 for copy")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile returned error: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("copied contents = %q; want %q", got, payload)
	}
}

func TestCopyFileMissingSourceErrors(t *testing.T) {
	dir := t.TempDir()
	err := copyFile(filepath.Join(dir, "does-not-exist"), filepath.Join(dir, "out"))
	if err == nil {
		t.Errorf("copyFile with missing source returned nil error; want an error")
	}
}

func mustModuleRoot(t *testing.T) string {
	t.Helper()
	root, err := moduleRoot()
	if err != nil {
		t.Fatalf("moduleRoot returned error: %v", err)
	}
	return root
}

func TestModuleRootPointsAtRepoRoot(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatalf("moduleRoot returned error: %v", err)
	}
	if root == "" {
		t.Fatalf("moduleRoot returned empty string")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		t.Fatalf("moduleRoot %q is not an existing directory (err=%v)", root, err)
	}
	// The module root must contain go.mod (it is the repository root).
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Errorf("moduleRoot %q does not contain go.mod: %v", root, err)
	}
}

func TestWriteBannerCreatesLicenseFile(t *testing.T) {
	dir := t.TempDir()
	meta := newDistMeta()
	if err := writeBanner(dir, meta); err != nil {
		t.Fatalf("writeBanner returned error: %v", err)
	}
	out := filepath.Join(dir, "anchorjs.license.js")
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read banner file: %v", err)
	}
	if !strings.Contains(string(data), meta.Version) {
		t.Errorf("banner file missing version %q", meta.Version)
	}
}

func TestCopyToDocsCopiesAllArtifacts(t *testing.T) {
	dist := t.TempDir()
	docs := t.TempDir()
	names := []string{"anchorjs.wasm", "wasm_exec.js", "anchorjs.license.js"}
	for i, name := range names {
		body := strings.Repeat("x", i+1)
		if err := os.WriteFile(filepath.Join(dist, name), []byte(body), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	if err := copyToDocs(dist, docs); err != nil {
		t.Fatalf("copyToDocs returned error: %v", err)
	}
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(docs, name)); err != nil {
			t.Errorf("expected %s copied to docs: %v", name, err)
		}
	}
}

func TestCopyToDocsMissingArtifactErrors(t *testing.T) {
	dist := t.TempDir()
	docs := t.TempDir()
	if err := copyToDocs(dist, docs); err == nil {
		t.Errorf("copyToDocs with empty dist returned nil error; want an error")
	}
}

func TestCopyLoaderShortCircuitsWhenPresent(t *testing.T) {
	dist := t.TempDir()
	dst := filepath.Join(dist, "wasm_exec.js")
	if err := os.WriteFile(dst, []byte("existing loader"), 0o644); err != nil {
		t.Fatalf("seed loader: %v", err)
	}
	if err := copyLoader(mustModuleRoot(t), dist); err != nil {
		t.Fatalf("copyLoader short-circuit returned error: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read loader: %v", err)
	}
	if string(data) != "existing loader" {
		t.Errorf("copyLoader overwrote existing loader; contents = %q", data)
	}
}

func TestCopyLoaderFetchesFromGoroot(t *testing.T) {
	dist := t.TempDir()
	if err := copyLoader(mustModuleRoot(t), dist); err != nil {
		t.Fatalf("copyLoader from GOROOT returned error: %v", err)
	}
	info, err := os.Stat(filepath.Join(dist, "wasm_exec.js"))
	if err != nil {
		t.Fatalf("wasm_exec.js not copied: %v", err)
	}
	if info.Size() == 0 {
		t.Errorf("copied wasm_exec.js is empty")
	}
}

func TestBuildWASMProducesModule(t *testing.T) {
	dist := t.TempDir()
	if err := buildWASM(mustModuleRoot(t), dist); err != nil {
		t.Fatalf("buildWASM returned error: %v", err)
	}
	info, err := os.Stat(filepath.Join(dist, "anchorjs.wasm"))
	if err != nil {
		t.Fatalf("anchorjs.wasm not produced: %v", err)
	}
	if info.Size() == 0 {
		t.Errorf("produced anchorjs.wasm is empty")
	}
}

// TestRunRegeneratesArtifacts exercises the top-level run() orchestrator
// end-to-end. run() resolves the real module root via runtime.Caller and
// regenerates dist/ (and copies into docs/), which is the build tool's whole
// purpose, so invoking it here both covers the orchestration path and verifies
// the expected artifacts are produced.
func TestRunRegeneratesArtifacts(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatalf("moduleRoot returned error: %v", err)
	}
	if err := run(); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	for _, name := range []string{"anchorjs.wasm", "wasm_exec.js", "anchorjs.license.js"} {
		info, statErr := os.Stat(filepath.Join(root, "dist", name))
		if statErr != nil {
			t.Fatalf("expected dist/%s after run(): %v", name, statErr)
		}
		if info.Size() == 0 {
			t.Errorf("dist/%s is empty after run()", name)
		}
	}
}
