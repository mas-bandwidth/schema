package cstable

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// csQuote spells s as a C# string literal. C# reads \x with one to four hex
// digits, so an \xNN spelling followed by a hex digit extends the escape; this
// quoter never emits \x. A control rune is a fixed-width \uNNNN, a non-BMP
// rune a fixed-width \UNNNNNNNN, and printable text passes as written.
func csQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\a':
			b.WriteString(`\a`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\v':
			b.WriteString(`\v`)
		default:
			switch {
			case r < 0x20 || r == 0x7f:
				fmt.Fprintf(&b, `\u%04X`, r)
			case r > 0xFFFF:
				fmt.Fprintf(&b, `\U%08X`, r)
			case !unicode.IsPrint(r):
				fmt.Fprintf(&b, `\u%04X`, r)
			default:
				b.WriteRune(r)
			}
		}
		i += size
	}
	b.WriteByte('"')
	return b.String()
}

// csQuoteList spells a list as C# string literals joined by ", ", the form the
// tag columns use.
func csQuoteList(items []string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = csQuote(item)
	}
	return strings.Join(quoted, ", ")
}
