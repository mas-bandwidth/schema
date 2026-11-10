package tablewire

import (
	"bytes"
	"unicode/utf8"
)

// ILL-FORMED TEXT IS DAMAGE (docs/SPEC-TABLES.md §3, §4): a kind 12 payload is
// well-formed UTF-8 with no zero byte among its bytes, checked as it ARRIVES
// and before the reader's own bound, because a payload that is not text is not
// text at whatever length the reader would have kept. It is SPEC.md §4.7's
// rule in this wire's idiom, and the read refuses wire_damaged.
func textValid(b []byte) bool {
	return utf8.Valid(b) && bytes.IndexByte(b, 0) < 0
}
