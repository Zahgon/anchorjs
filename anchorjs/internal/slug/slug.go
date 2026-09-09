// Package slug implements the pure (DOM-independent) string transforms used by
// AnchorJS's urlify pipeline. It is deliberately build-tag-neutral so the same
// code compiles both into the js/wasm library and into native unit tests.
//
// The urlify text pipeline must reproduce JavaScript string semantics exactly.
// JavaScript strings are sequences of UTF-16 code units, and String.prototype
// methods like trim/substring/toLowerCase/replace operate on those code units.
// To match byte-for-byte (including surrogate-pair handling for emoji and the
// UTF-16 length used by substring/truncate), we convert to []uint16, operate on
// code units, and convert back.
//
// The only part of urlify that is NOT in this package is the HTML-entity
// decoding step (textarea.innerHTML -> textarea.value), which depends on the
// browser's HTML parser and therefore lives in the js/wasm code.
package slug

import (
	"strings"
	"unicode/utf16"
)

// NonsafeCodeUnits is the set of single-UTF-16-code-unit characters replaced by
// '-' via the original regex:
//
//	/[& +$,:;=?@"#{}|^~[`%!'<>\]./()*\\\n\t\b\v\u00A0]/g
//
// i.e. the character class: & space + $ , : ; = ? @ " # { } | ^ ~ [ ` % ! '
// < > ] . / ( ) * \ \n \t \b \v \u00A0
//
// Note: the apostrophe (') is in this set, but apostrophes are already removed
// earlier in the pipeline, so it never matches here in practice. It is included
// to faithfully mirror the regex character class.
var NonsafeCodeUnits = map[uint16]bool{
	'&':    true,
	' ':    true,
	'+':    true,
	'$':    true,
	',':    true,
	':':    true,
	';':    true,
	'=':    true,
	'?':    true,
	'@':    true,
	'"':    true,
	'#':    true,
	'{':    true,
	'}':    true,
	'|':    true,
	'^':    true,
	'~':    true,
	'[':    true,
	'`':    true,
	'%':    true,
	'!':    true,
	'\'':   true,
	'<':    true,
	'>':    true,
	']':    true,
	'.':    true,
	'/':    true,
	'(':    true,
	')':    true,
	'*':    true,
	'\\':   true,
	'\n':   true, // 0x0A
	'\t':   true, // 0x09
	'\b':   true, // 0x08 backspace
	'\v':   true, // 0x0B vertical tab
	0x00A0: true, // non-breaking space
}

// isJSTrimWhitespace reports whether a code unit is whitespace for
// String.prototype.trim (WhiteSpace + LineTerminator per ECMAScript):
//
//	\t \n \v \f \r space \u00A0 \u1680 \u2000-\u200A \u2028 \u2029 \u202F
//	\u205F \u3000 \uFEFF
func isJSTrimWhitespace(u uint16) bool {
	switch u {
	case 0x0009, 0x000A, 0x000B, 0x000C, 0x000D, 0x0020,
		0x00A0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	if u >= 0x2000 && u <= 0x200A {
		return true
	}
	return false
}

func toUnits(s string) []uint16 {
	return utf16.Encode([]rune(s))
}

func fromUnits(u []uint16) string {
	return string(utf16.Decode(u))
}

// Trim mirrors String.prototype.trim over UTF-16 code units.
func Trim(s string) string {
	u := toUnits(s)
	start := 0
	end := len(u)
	for start < end && isJSTrimWhitespace(u[start]) {
		start++
	}
	for end > start && isJSTrimWhitespace(u[end-1]) {
		end--
	}
	return fromUnits(u[start:end])
}

// RemoveApostrophes mirrors .replace(/'/gi, '').
func RemoveApostrophes(s string) string {
	// The apostrophe is a single ASCII code unit; a byte replace is equivalent.
	return strings.ReplaceAll(s, "'", "")
}

// ReplaceNonsafeChars mirrors .replace(nonsafeChars, '-') operating on UTF-16
// code units (the regex matches single code units).
func ReplaceNonsafeChars(s string) string {
	u := toUnits(s)
	out := make([]uint16, 0, len(u))
	const hyphen = uint16('-')
	for _, c := range u {
		if NonsafeCodeUnits[c] {
			out = append(out, hyphen)
		} else {
			out = append(out, c)
		}
	}
	return fromUnits(out)
}

// CollapseHyphens mirrors .replace(/-{2,}/g, '-').
func CollapseHyphens(s string) string {
	// Operate on bytes; '-' is ASCII so byte-level collapse is equivalent.
	var b strings.Builder
	b.Grow(len(s))
	prevHyphen := false
	for i := 0; i < len(s); i++ {
		if s[i] == '-' {
			if prevHyphen {
				continue
			}
			prevHyphen = true
			b.WriteByte('-')
		} else {
			prevHyphen = false
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// Substring mirrors .substring(0, truncate) where truncate counts UTF-16 code
// units. Negative/NaN truncate is coerced by JS substring to 0; large values
// are clamped to the string length.
func Substring(s string, truncate int) string {
	if truncate < 0 {
		truncate = 0
	}
	u := toUnits(s)
	if truncate > len(u) {
		truncate = len(u)
	}
	return fromUnits(u[:truncate])
}

// TrimEdgeHyphens mirrors .replace(/^-+|-+$/gm, ''). The `m` (multiline) flag
// makes ^ and $ match at line boundaries, so leading/trailing hyphens are
// trimmed on every line (split on \n). By this stage newlines have been
// converted to hyphens by ReplaceNonsafeChars and collapsed, so in practice
// there are no newlines left; but we faithfully honor the multiline semantics.
func TrimEdgeHyphens(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(strings.TrimLeft(line, "-"), "-")
	}
	return strings.Join(lines, "\n")
}

// ToLowerCase mirrors String.prototype.toLowerCase. Go's strings.ToLower
// performs Unicode simple case folding to lower case, matching JS default
// (locale-independent) lowercasing for the characters exercised by AnchorJS
// (ASCII, Cyrillic, etc.). Characters without a lowercase mapping (emoji,
// Japanese, Malayalam) are left unchanged by both.
func ToLowerCase(s string) string {
	return strings.ToLower(s)
}

// Pipeline applies the DOM-independent portion of urlify in the exact original
// order: trim -> remove apostrophes -> replace non-safe chars -> collapse
// repeated hyphens -> substring(0, truncate) -> trim edge hyphens ->
// toLowerCase. The caller supplies text that has ALREADY had HTML entities
// decoded (the one browser-dependent step) and the resolved truncate length.
func Pipeline(text string, truncate int) string {
	s := Trim(text)
	s = RemoveApostrophes(s)
	s = ReplaceNonsafeChars(s)
	s = CollapseHyphens(s)
	s = Substring(s, truncate)
	s = TrimEdgeHyphens(s)
	s = ToLowerCase(s)
	return s
}
