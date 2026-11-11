package goosetable_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
)

// hostileString carries every character a Go quote spells with an escape
// Goose does not have, beside letters: a no-break space, a soft hyphen, a
// BOM, a C1 control, BEL, VT and FF.
const hostileString = "\u00a0A\u00adB\ufeffC\u0085D\aE\vF\fG"

// TestGooseEmittedStringLiteralsUseGooseEscapes holds every string literal
// the table surface writes to Goose's own escape vocabulary: a quote, a
// backslash, the control bytes and \xNN, never a Go-only spelling. A schema
// whose string default and text key hold such characters is generated in
// process; each emitted literal is decoded, its escapes are checked against
// the vocabulary, and the default and text key must decode back to the
// schema's bytes.
func TestGooseEmittedStringLiteralsUseGooseEscapes(t *testing.T) {
	t.Parallel()
	const src = `package stringlits

enum Mood { Calm, Alert }

fixed table Hostile {
    label string(64) = %s | json = %s
    mood Mood = Calm
}
`
	u := unitFromSchema(t, "StringLit.schema", fmt.Sprintf(src, quoteRaw(hostileString), quoteRaw(hostileString+"Key")))
	files, err := goosetable.Generate(u)
	if err != nil {
		t.Fatalf("goosetable.Generate: %v", err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	seen := map[string]bool{}
	for _, name := range names {
		if !strings.HasSuffix(name, ".goose") {
			continue
		}
		lits, err := scanGooseStringLiterals(files[name])
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		for _, lit := range lits {
			seen[lit] = true
		}
	}
	for _, want := range []string{hostileString, hostileString + "Key"} {
		if !seen[want] {
			t.Errorf("no emitted string literal decodes to the schema bytes %q", want)
		}
	}
}

// TestGooseEmittedStringLiteralsUseGooseEscapesNegativeControl runs the same
// scan over a literal Go quotes: its \u spelling is refused by name.
func TestGooseEmittedStringLiteralsUseGooseEscapesNegativeControl(t *testing.T) {
	t.Parallel()
	src := "let s: const u8[:] = " + fmt.Sprintf("%q", hostileString) + ";\n"
	_, err := scanGooseStringLiterals([]byte(src))
	if err == nil {
		t.Fatal("the scan accepted a Go-quoted literal")
	}
	if !strings.Contains(err.Error(), `\u`) {
		t.Errorf("the scan refused the literal without naming \\u: %v", err)
	}
}

// quoteRaw puts raw bytes in a schema string literal; the schema language has
// no escape sequences, so the bytes are written as they are.
func quoteRaw(s string) string {
	return `"` + s + `"`
}

// scanGooseStringLiterals returns every double-quoted string literal in src,
// decoded, and refuses an escape outside Goose's vocabulary
// (\n \t \r \0 \\ \" \' \xNN). Comments and single-quoted character literals
// are skipped.
func scanGooseStringLiterals(src []byte) ([]string, error) {
	var lits []string
	for i := 0; i < len(src); {
		switch c := src[i]; {
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			i += 2
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			if i+1 >= len(src) {
				return nil, fmt.Errorf("unterminated block comment")
			}
			i += 2
		case c == '\'':
			i++
			for i < len(src) {
				if src[i] == '\\' {
					i += 2
					continue
				}
				if src[i] == '\'' {
					i++
					break
				}
				i++
			}
		case c == '"':
			lit, next, err := scanGooseString(src, i)
			if err != nil {
				return nil, err
			}
			lits = append(lits, lit)
			i = next
		default:
			i++
		}
	}
	return lits, nil
}

// scanGooseString decodes one string literal starting at src[i] == '"' and
// returns the index just past its closing quote.
func scanGooseString(src []byte, i int) (string, int, error) {
	var b strings.Builder
	i++
	for {
		if i >= len(src) {
			return "", i, fmt.Errorf("unterminated string literal")
		}
		c := src[i]
		if c == '"' {
			return b.String(), i + 1, nil
		}
		if c != '\\' {
			b.WriteByte(c)
			i++
			continue
		}
		if i+1 >= len(src) {
			return "", i, fmt.Errorf("unterminated string literal")
		}
		switch esc := src[i+1]; esc {
		case 'n':
			b.WriteByte('\n')
			i += 2
		case 't':
			b.WriteByte('\t')
			i += 2
		case 'r':
			b.WriteByte('\r')
			i += 2
		case '0':
			b.WriteByte(0)
			i += 2
		case '\\':
			b.WriteByte('\\')
			i += 2
		case '"':
			b.WriteByte('"')
			i += 2
		case '\'':
			b.WriteByte('\'')
			i += 2
		case 'x':
			if i+3 >= len(src) {
				return "", i, fmt.Errorf("string literal ends in a short \\x escape")
			}
			hi, ok := hexDigit(src[i+2])
			if !ok {
				return "", i, fmt.Errorf("string literal uses escape \\x without two hex digits")
			}
			lo, ok := hexDigit(src[i+3])
			if !ok {
				return "", i, fmt.Errorf("string literal uses escape \\x without two hex digits")
			}
			b.WriteByte(hi<<4 | lo)
			i += 4
		default:
			return "", i, fmt.Errorf("string literal uses escape \\%c, which Goose does not have", esc)
		}
	}
}

func hexDigit(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
