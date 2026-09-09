package main

import (
	"strings"
	"testing"
)

// TestBannerMatchesOriginalFormat locks the generated @license banner to the
// exact multi-line structure produced by the original banner.js so the Go/WASM
// distribution advertises the same Expat license header as the JS release.
func TestBannerMatchesOriginalFormat(t *testing.T) {
	meta := distMeta{
		Version:  "5.0.0",
		Homepage: "https://www.bryanbraun.com/anchorjs/",
		License:  "MIT",
		Year:     2026,
		FullDate: "2026-09-08",
	}

	got := bannerText(meta)

	want := "// @license magnet:?xt=urn:btih:d3d9a9a6595521f9666a5e94cc830dab83b65699&dn=expat.txt Expat\n" +
		"//\n" +
		"// AnchorJS - v5.0.0 - 2026-09-08\n" +
		"// https://www.bryanbraun.com/anchorjs/\n" +
		"// Copyright (c) 2026 Bryan Braun; Licensed MIT\n" +
		"//\n" +
		"// @license magnet:?xt=urn:btih:d3d9a9a6595521f9666a5e94cc830dab83b65699&dn=expat.txt Expat\n" +
		"// @license-end\n"

	if got != want {
		t.Errorf("banner mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// TestBannerStartsWithLicenseComment mirrors banner.js's guard that only
// prepends the banner when the target does not already begin with "//".
func TestBannerStartsWithLicenseComment(t *testing.T) {
	got := bannerText(newDistMeta())
	if !strings.HasPrefix(got, "// @license magnet:") {
		t.Errorf("banner must start with the @license magnet comment, got %q", got[:min(40, len(got))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
