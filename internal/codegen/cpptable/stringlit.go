package cpptable

import (
	"fmt"
	"strings"
)

// cStringLit renders bytes as a C++ string literal: printable ASCII as
// itself, a quote and a backslash escaped, and every other byte as a
// three-digit octal escape, which no following digit can extend. UTF-8
// passes as written, so a literal the emitter writes from schema text
// decodes, under C++'s own escape rules, to exactly those bytes.
func cStringLit(b []byte) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, c := range b {
		switch {
		case c == '"':
			sb.WriteString(`\"`)
		case c == '\\':
			sb.WriteString(`\\`)
		case c >= 0x20 && c < 0x7f:
			sb.WriteByte(c)
		default:
			fmt.Fprintf(&sb, "\\%03o", c)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}
