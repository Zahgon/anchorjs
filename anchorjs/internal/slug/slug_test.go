package slug_test

import (
	"testing"

	"github.com/bryanbraun/anchorjs/anchorjs/internal/slug"
)

// These tests exercise the DOM-independent portion of AnchorJS's urlify
// pipeline (slug.Pipeline). They mirror the behaviors asserted by the original
// Jasmine specs (test/spec/AnchorSpec.js, the "urlify" describe group and the
// truncate spec) that do not depend on the browser's HTML-entity decoding
// step. The full urlify behavior (including entity decoding) remains covered by
// the 39 browser specs; these are additional native tests and do not replace
// any of them.
//
// Callers pass text with HTML entities ALREADY decoded, so the non-breaking
// space that the original inputs write as "&nbsp;" is supplied here as the raw
// U+00A0 code point.
const nbsp = "\u00A0"

func TestPipelineRemovesNonURLSafeCharacters(t *testing.T) {
	// Mirrors the original "removes non-url-safe characters" spec: several
	// differently-punctuated inputs all collapse to the same slug.
	const want = "one-two-three-four-five-six-seven-eight-nine-ten"
	inputs := []string{
		"one two three four five six seven eight nine ten",
		"one&two+three$four,five:six;seven=eight?nine@ten",
		"one.two/three(four)five*six#seven!eight%nine>ten",
		"one\\two<three>four" + nbsp + "five\tsix\bseven\veight\tnine\nten",
	}
	for _, in := range inputs {
		if got := slug.Pipeline(in, 64); got != want {
			t.Errorf("Pipeline(%q, 64) = %q; want %q", in, got, want)
		}
	}
}

func TestPipelineTrimsWhitespace(t *testing.T) {
	inputs := []string{
		"\n abc\r",
		"abc  ",
		"abc\n ",
	}
	const want = "abc"
	for _, in := range inputs {
		if got := slug.Pipeline(in, 64); got != want {
			t.Errorf("Pipeline(%q, 64) = %q; want %q", in, got, want)
		}
	}
}

func TestPipelineRemovesApostrophes(t *testing.T) {
	if got := slug.Pipeline("don't", 64); got != "dont" {
		t.Errorf("Pipeline(%q, 64) = %q; want %q", "don't", got, "dont")
	}
}

func TestPipelineTruncates(t *testing.T) {
	const base = "Today you are you! That is truer than true! There is no one alive who is you-er than you!"
	cases := []struct {
		truncate int
		want     string
	}{
		{64, "today-you-are-you-that-is-truer-than-true-there-is-no-one-alive"},
		{41, "today-you-are-you-that-is-truer-than-true"},
		{17, "today-you-are-you"},
		{87, "today-you-are-you-that-is-truer-than-true-there-is-no-one-alive-who-is-you-er-than-you"},
	}
	for _, c := range cases {
		if got := slug.Pipeline(base, c.truncate); got != c.want {
			t.Errorf("Pipeline(base, %d) = %q; want %q", c.truncate, got, c.want)
		}
	}
}

func TestPipelinePreservesUnicode(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		truncate int
		want     string
	}{
		{
			name:     "cyrillic is lowercased",
			input:    "Заголовок первого уровня",
			truncate: 64,
			want:     "заголовок-первого-уровня",
		},
		{
			name:     "japanese is unchanged",
			input:    "この不思議な港の話",
			truncate: 64,
			want:     "この不思議な港の話",
		},
		{
			name:     "emoji are preserved",
			input:    "Use ⚡ and 👪 all over the 🌐 can 🔗 inside your webpages.",
			truncate: 64,
			want:     "use-⚡-and-👪-all-over-the-🌐-can-🔗-inside-your-webpages",
		},
	}
	for _, c := range cases {
		if got := slug.Pipeline(c.input, c.truncate); got != c.want {
			t.Errorf("%s: Pipeline(%q, %d) = %q; want %q", c.name, c.input, c.truncate, got, c.want)
		}
	}
}

func TestSubstringCountsUTF16CodeUnits(t *testing.T) {
	// An emoji outside the BMP occupies two UTF-16 code units, matching the
	// original JS substring(0, truncate) semantics.
	if got := slug.Substring("👪x", 1); got != "\uFFFD" {
		// Truncating at 1 code unit splits the surrogate pair; utf16.Decode of a
		// lone high surrogate yields U+FFFD (the Unicode replacement character).
		t.Errorf("Substring(%q, 1) = %q; want lone-surrogate replacement U+FFFD", "👪x", got)
	}
	if got := slug.Substring("👪x", 2); got != "👪" {
		t.Errorf("Substring(%q, 2) = %q; want %q", "👪x", got, "👪")
	}
	if got := slug.Substring("abc", -5); got != "" {
		t.Errorf("Substring(%q, -5) = %q; want empty", "abc", got)
	}
	if got := slug.Substring("abc", 100); got != "abc" {
		t.Errorf("Substring(%q, 100) = %q; want %q", "abc", got, "abc")
	}
}
