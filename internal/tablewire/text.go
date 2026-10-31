package tablewire

import (
	"bytes"
	"unicode/utf8"
)

// ILL-FORMED TEXT IS DAMAGE (docs/SPEC-TABLES.md §3, §4): a kind 12 payload is
// well-formed UTF-8 with no zero byte among its bytes, checked as it ARRIVES
// and before the reader's own bound, because a payload that is not text is not
// text at whatever length the reader would have kept. It is SPEC.md §4.7's
// rule in this wire's idiom: the field reads its declared default, one
// `malformed` counts, and the parent reads on past L.
func textValid(b []byte) bool {
	return utf8.Valid(b) && bytes.IndexByte(b, 0) < 0
}

// textBoundary is where a clamp cuts a well-formed payload longer than the
// reader's bound: the last whole code point that fits within `bound` bytes
// (§3, §16.2), so the clamp can never invent ill-formed storage.
func textBoundary(b []byte, bound int) int {
	if len(b) <= bound {
		return len(b)
	}
	cut := bound
	for cut > 0 && b[cut]&0xC0 == 0x80 {
		cut--
	}
	return cut
}
