// Command build reproduces the AnchorJS distribution workflow for the Go/WASM
// target. It mirrors the original npm "build" script, whose three steps were
// uglify (produce anchor.min.js) -> add-banner (prepend the @license banner)
// -> copy-to-docs (publish the artifact under docs/).
//
// The Go/WASM equivalents are:
//
//	uglify        -> compile cmd/anchorjs-wasm to dist/anchorjs.wasm
//	add-banner    -> write dist/anchorjs.license.js, the @license Expat banner
//	                 matching the original banner.js format, alongside the
//	                 generated dist/wasm_exec.js toolchain loader
//	copy-to-docs  -> copy dist/anchorjs.wasm, dist/wasm_exec.js and the banner
//	                 into docs/ so the documentation site loads the built target
//
// Run from the module root: go run ./build
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// distMeta carries the package identity used in the license banner. These
// values match the original package.json (name anchor-js, version 5.0.0) so the
// generated banner is byte-for-byte equivalent to the JavaScript banner.js
// output aside from the build date.
type distMeta struct {
	Version  string
	Homepage string
	License  string
	Year     int
	FullDate string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "build failed:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}

	meta := newDistMeta()

	distDir := filepath.Join(root, "dist")
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		return err
	}

	if err := buildWASM(root, distDir); err != nil {
		return err
	}
	fmt.Println("compiled dist/anchorjs.wasm")

	if err := copyLoader(root, distDir); err != nil {
		return err
	}
	fmt.Println("published dist/wasm_exec.js")

	if err := writeBanner(distDir, meta); err != nil {
		return err
	}
	fmt.Println("wrote dist/anchorjs.license.js")

	if err := copyToDocs(distDir, docsDir); err != nil {
		return err
	}
	fmt.Println("copied artifacts to docs/")

	return nil
}

func moduleRoot() (string, error) {
	// The build command lives in <root>/build, so the module root is its parent.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot determine build source location")
	}
	return filepath.Dir(filepath.Dir(thisFile)), nil
}

func newDistMeta() distMeta {
	now := time.Now()
	return distMeta{
		Version:  "5.0.0",
		Homepage: "https://www.bryanbraun.com/anchorjs/",
		License:  "MIT",
		Year:     now.Year(),
		FullDate: fmt.Sprintf("%04d-%02d-%02d", now.Year(), int(now.Month()), now.Day()),
	}
}

func buildWASM(root, distDir string) error {
	out := filepath.Join(distDir, "anchorjs.wasm")
	cmd := exec.Command("go", "build", "-o", out, "./cmd/anchorjs-wasm")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// copyLoader publishes the Go toolchain's wasm_exec.js loader into dist/. This
// file is generated runtime glue shipped with the Go SDK ($GOROOT/misc/wasm or
// lib/wasm); it is required to instantiate any Go WASM module in the browser.
func copyLoader(root, distDir string) error {
	dst := filepath.Join(distDir, "wasm_exec.js")
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	goroot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return err
	}
	base := trimNewline(string(goroot))
	candidates := []string{
		filepath.Join(base, "lib", "wasm", "wasm_exec.js"),
		filepath.Join(base, "misc", "wasm", "wasm_exec.js"),
	}
	for _, src := range candidates {
		if _, err := os.Stat(src); err == nil {
			return copyFile(src, dst)
		}
	}
	return fmt.Errorf("wasm_exec.js not found in GOROOT %q", base)
}

// bannerText builds the @license Expat banner in the exact multi-line form
// emitted by the original banner.js (magnet Expat header, AnchorJS - vX - date,
// homepage, copyright, closing magnet line, then @license-end).
func bannerText(m distMeta) string {
	return fmt.Sprintf(`// @license magnet:?xt=urn:btih:d3d9a9a6595521f9666a5e94cc830dab83b65699&dn=expat.txt Expat
//
// AnchorJS - v%s - %s
// %s
// Copyright (c) %d Bryan Braun; Licensed %s
//
// @license magnet:?xt=urn:btih:d3d9a9a6595521f9666a5e94cc830dab83b65699&dn=expat.txt Expat
// @license-end
`, m.Version, m.FullDate, m.Homepage, m.Year, m.License)
}

func writeBanner(distDir string, m distMeta) error {
	return os.WriteFile(filepath.Join(distDir, "anchorjs.license.js"), []byte(bannerText(m)), 0o644)
}

func copyToDocs(distDir, docsDir string) error {
	names := []string{"anchorjs.wasm", "wasm_exec.js", "anchorjs.license.js"}
	for _, name := range names {
		if err := copyFile(filepath.Join(distDir, name), filepath.Join(docsDir, name)); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
