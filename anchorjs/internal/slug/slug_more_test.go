package slug_test

import (
	"testing"

	"github.com/bryanbraun/anchorjs/anchorjs/internal/slug"
)

// The following tests break the DOM-independent urlify behaviors into one
// focused test function per observable behavior, mirroring the granularity of
// the upstream Jasmine specs (test/spec/AnchorSpec.js). Each function drives a
// single public slug function through a representative input and asserts the
// observable result. They are additional native tests: they do not replace the
// existing table-driven tests in slug_test.go, nor the 39 browser specs.

func TestTrimRemovesLeadingWhitespace(t *testing.T) {
	if got := slug.Trim("   lead"); got != "lead" {
		t.Errorf("Trim(%q) = %q; want leading whitespace removed", "   lead", got)
	}
}

func TestTrimRemovesTrailingWhitespace(t *testing.T) {
	if got := slug.Trim("trail   "); got != "trail" {
		t.Errorf("Trim(%q) = %q; want trailing whitespace removed", "trail   ", got)
	}
}

func TestTrimRemovesSurroundingNewlines(t *testing.T) {
	if got := slug.Trim("\n\r mid \r\n"); got != "mid" {
		t.Errorf("Trim(%q) = %q; want surrounding newlines removed", "\n\r mid \r\n", got)
	}
}

func TestTrimPreservesInteriorWhitespace(t *testing.T) {
	if got := slug.Trim("  a b c  "); got != "a b c" {
		t.Errorf("Trim(%q) = %q; want interior spaces preserved", "  a b c  ", got)
	}
}

func TestTrimHandlesNonBreakingSpaceEdges(t *testing.T) {
	in := nbsp + "x" + nbsp
	if got := slug.Trim(in); got != "x" {
		t.Errorf("Trim(%q) = %q; want non-breaking-space edges removed", in, got)
	}
}

func TestTrimEmptyStringStaysEmpty(t *testing.T) {
	if got := slug.Trim("     "); got != "" {
		t.Errorf("Trim(all-spaces) = %q; want empty string", got)
	}
}

func TestRemoveApostrophesStripsSingleQuote(t *testing.T) {
	if got := slug.RemoveApostrophes("it's"); got != "its" {
		t.Errorf("RemoveApostrophes(%q) = %q; want apostrophe removed", "it's", got)
	}
}

func TestRemoveApostrophesStripsMultiple(t *testing.T) {
	if got := slug.RemoveApostrophes("y'all's"); got != "yalls" {
		t.Errorf("RemoveApostrophes(%q) = %q; want all apostrophes removed", "y'all's", got)
	}
}

func TestRemoveApostrophesLeavesOtherPunctuation(t *testing.T) {
	if got := slug.RemoveApostrophes("a-b_c"); got != "a-b_c" {
		t.Errorf("RemoveApostrophes(%q) = %q; want non-apostrophe punctuation kept", "a-b_c", got)
	}
}

func TestReplaceNonsafeCharsTurnsSpaceIntoHyphen(t *testing.T) {
	if got := slug.ReplaceNonsafeChars("a b"); got != "a-b" {
		t.Errorf("ReplaceNonsafeChars(%q) = %q; want space replaced by hyphen", "a b", got)
	}
}

func TestReplaceNonsafeCharsHandlesPunctuation(t *testing.T) {
	if got := slug.ReplaceNonsafeChars("a?b"); got != "a-b" {
		t.Errorf("ReplaceNonsafeChars(%q) = %q; want punctuation replaced by hyphen", "a?b", got)
	}
}

func TestReplaceNonsafeCharsKeepsAsciiLetters(t *testing.T) {
	if got := slug.ReplaceNonsafeChars("abcXYZ"); got != "abcXYZ" {
		t.Errorf("ReplaceNonsafeChars(%q) = %q; want ascii letters untouched", "abcXYZ", got)
	}
}

func TestReplaceNonsafeCharsKeepsUnicodeLetters(t *testing.T) {
	in := "café"
	if got := slug.ReplaceNonsafeChars(in); got != in {
		t.Errorf("ReplaceNonsafeChars(%q) = %q; want unicode letters untouched", in, got)
	}
}

func TestCollapseHyphensCollapsesRuns(t *testing.T) {
	if got := slug.CollapseHyphens("a---b"); got != "a-b" {
		t.Errorf("CollapseHyphens(%q) = %q; want hyphen run collapsed", "a---b", got)
	}
}

func TestCollapseHyphensLeavesSingleHyphen(t *testing.T) {
	if got := slug.CollapseHyphens("a-b"); got != "a-b" {
		t.Errorf("CollapseHyphens(%q) = %q; want single hyphen preserved", "a-b", got)
	}
}

func TestCollapseHyphensHandlesLongRun(t *testing.T) {
	if got := slug.CollapseHyphens("x----------y"); got != "x-y" {
		t.Errorf("CollapseHyphens(long run) = %q; want collapsed to single hyphen", got)
	}
}

func TestTrimEdgeHyphensRemovesLeading(t *testing.T) {
	if got := slug.TrimEdgeHyphens("-abc"); got != "abc" {
		t.Errorf("TrimEdgeHyphens(%q) = %q; want leading hyphen removed", "-abc", got)
	}
}

func TestTrimEdgeHyphensRemovesTrailing(t *testing.T) {
	if got := slug.TrimEdgeHyphens("abc-"); got != "abc" {
		t.Errorf("TrimEdgeHyphens(%q) = %q; want trailing hyphen removed", "abc-", got)
	}
}

func TestTrimEdgeHyphensKeepsInterior(t *testing.T) {
	if got := slug.TrimEdgeHyphens("-a-b-"); got != "a-b" {
		t.Errorf("TrimEdgeHyphens(%q) = %q; want only edge hyphens removed", "-a-b-", got)
	}
}

func TestToLowerCaseLowersAscii(t *testing.T) {
	if got := slug.ToLowerCase("HeLLo"); got != "hello" {
		t.Errorf("ToLowerCase(%q) = %q; want ascii lowercased", "HeLLo", got)
	}
}

func TestToLowerCaseLowersCyrillic(t *testing.T) {
	in := "ПРИВЕТ"
	if got := slug.ToLowerCase(in); got != "привет" {
		t.Errorf("ToLowerCase(%q) = %q; want cyrillic lowercased", in, got)
	}
}

func TestToLowerCaseLeavesCJKUnchanged(t *testing.T) {
	in := "港"
	if got := slug.ToLowerCase(in); got != in {
		t.Errorf("ToLowerCase(%q) = %q; want CJK unchanged", in, got)
	}
}

func TestSubstringZeroLengthIsEmpty(t *testing.T) {
	if got := slug.Substring("hello", 0); got != "" {
		t.Errorf("Substring(%q, 0) = %q; want empty", "hello", got)
	}
}

func TestSubstringShorterThanLimitReturnsAll(t *testing.T) {
	if got := slug.Substring("hi", 5); got != "hi" {
		t.Errorf("Substring(%q, 5) = %q; want whole string", "hi", got)
	}
}

func TestSubstringExactLength(t *testing.T) {
	if got := slug.Substring("abcd", 4); got != "abcd" {
		t.Errorf("Substring(%q, 4) = %q; want whole string", "abcd", got)
	}
}

func TestSubstringCutsAsciiAtLimit(t *testing.T) {
	if got := slug.Substring("abcdef", 3); got != "abc" {
		t.Errorf("Substring(%q, 3) = %q; want first 3 code units", "abcdef", got)
	}
}

func TestPipelineLowercasesResult(t *testing.T) {
	if got := slug.Pipeline("Hello World", 64); got != "hello-world" {
		t.Errorf("Pipeline(%q, 64) = %q; want lowercased hyphenated slug", "Hello World", got)
	}
}

func TestPipelineCollapsesMixedSeparators(t *testing.T) {
	if got := slug.Pipeline("a   ///   b", 64); got != "a-b" {
		t.Errorf("Pipeline(%q, 64) = %q; want separators collapsed", "a   ///   b", got)
	}
}

func TestPipelineTrimsEdgeHyphensAfterReplace(t *testing.T) {
	if got := slug.Pipeline("!hello!", 64); got != "hello" {
		t.Errorf("Pipeline(%q, 64) = %q; want edge hyphens trimmed", "!hello!", got)
	}
}

func TestPipelineEmptyInput(t *testing.T) {
	if got := slug.Pipeline("", 64); got != "" {
		t.Errorf("Pipeline(empty) = %q; want empty", got)
	}
}

func TestPipelineWhitespaceOnlyInput(t *testing.T) {
	if got := slug.Pipeline("     ", 64); got != "" {
		t.Errorf("Pipeline(all whitespace) = %q; want empty", got)
	}
}

func TestPipelinePunctuationOnlyInput(t *testing.T) {
	if got := slug.Pipeline("!?.,", 64); got != "" {
		t.Errorf("Pipeline(punctuation only) = %q; want empty", got)
	}
}

func TestPipelineSingleWordUnchanged(t *testing.T) {
	if got := slug.Pipeline("heading", 64); got != "heading" {
		t.Errorf("Pipeline(%q, 64) = %q; want single word slug", "heading", got)
	}
}

func TestPipelineKeepsUnderscores(t *testing.T) {
	if got := slug.Pipeline("a_b", 64); got != "a_b" {
		t.Errorf("Pipeline(%q, 64) = %q; want underscore preserved", "a_b", got)
	}
}

func TestPipelineTruncatesShortLimit(t *testing.T) {
	if got := slug.Pipeline("alpha beta gamma", 5); got != "alpha" {
		t.Errorf("Pipeline(%q, 5) = %q; want truncated to first word", "alpha beta gamma", got)
	}
}
