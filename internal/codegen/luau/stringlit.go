package luau

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// StringLit renders s as a Luau string literal. Go's %q is close but not
// Luau: it writes a non-printable rune as \uXXXX or \UXXXXXXXX, which Luau
// rejects (Luau's form is \u{X}), so a schema name, doc comment, json key or
// default that carries one would emit a module that does not load. Every
// other spelling matches %q: printable runes as themselves, the named escapes
// Luau shares (\a \b \f \n \r \t \v \\ \"), and \xNN for any other byte below
// 0x80 and for a byte that is not UTF-8.
func StringLit(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && n == 1 {
			fmt.Fprintf(&b, "\\x%02x", s[i])
			i++
			continue
		}
		i += n
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
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
			case r < 0x80 && !strconv.IsPrint(r):
				fmt.Fprintf(&b, "\\x%02x", r)
			case !strconv.IsPrint(r):
				fmt.Fprintf(&b, "\\u{%x}", r)
			default:
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// luauVerbs rewrites a format's %q verbs to %s over StringLit of their
// argument, so a generator can keep writing %q and get Luau's quoting. It
// handles the verbs this package's generators use: flags, width and
// precision, and %%; an explicit argument index is not supported.
func luauVerbs(format string, args []any) (string, []any) {
	if !strings.Contains(format, "%q") {
		return format, args
	}
	var f strings.Builder
	out := append([]any(nil), args...)
	arg := 0
	for i := 0; i < len(format); i++ {
		c := format[i]
		f.WriteByte(c)
		if c != '%' {
			continue
		}
		j := i + 1
		for j < len(format) && strings.IndexByte("+-# 0123456789.*", format[j]) >= 0 {
			if format[j] == '*' {
				arg++
			}
			j++
		}
		if j >= len(format) {
			break
		}
		verb := format[j]
		if verb == '[' {
			panic("luau: an explicit argument index in a generator format")
		}
		if verb == '%' {
			f.WriteString(format[i+1 : j+1])
			i = j
			continue
		}
		if verb == 'q' && arg < len(out) {
			switch v := out[arg].(type) {
			case string:
				out[arg] = StringLit(v)
			case []byte:
				out[arg] = StringLit(string(v))
			case fmt.Stringer:
				out[arg] = StringLit(v.String())
			default:
				panic(fmt.Sprintf("luau: %%q of a %T in a generator format", v))
			}
			f.WriteString(format[i+1:j] + "s")
		} else {
			f.WriteString(format[i+1 : j+1])
		}
		arg++
		i = j
	}
	return f.String(), out
}

// Sprintf is fmt.Sprintf with %q spelled as a Luau string literal.
func Sprintf(format string, args ...any) string {
	format, args = luauVerbs(format, args)
	return fmt.Sprintf(format, args...)
}

// Fprintf is fmt.Fprintf with %q spelled as a Luau string literal.
func Fprintf(w io.Writer, format string, args ...any) {
	format, args = luauVerbs(format, args)
	_, _ = fmt.Fprintf(w, format, args...) // every caller writes to an in-memory buffer, which cannot fail
}
