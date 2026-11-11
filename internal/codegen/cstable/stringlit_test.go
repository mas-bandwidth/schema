package cstable

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// csLiteralText is the schema text the emitted literals must carry: a DEL, two
// hex digits after it, a backslash, a double quote, a non-ASCII rune and a
// newline. C# reads \x with one to four hex digits, so a following hex digit
// extends the escape when the spelling uses Go's \x form.
const csLiteralText = "lit \x7fAFZQXJ \\ \" é Ω 😀\nsecond line"

// csLiteralSentinel is the part of the text no escape can swallow: every
// emitted literal that carries the schema text still holds it.
const csLiteralSentinel = "ZQXJ"

// TestCSharpEmittedStringLiteralsUseCSharpEscapes generates a unit whose table,
// field and arm doc text and tags carry text C# does not spell like Go, and
// requires every emitted literal that carries it to decode, under C# rules, to
// exactly that text.
func TestCSharpEmittedStringLiteralsUseCSharpEscapes(t *testing.T) {
	t.Parallel()
	files, err := Generate(csharpLiteralUnit(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range csharpLiteralFindings(files, csLiteralText) {
		t.Error(finding)
	}
}

// TestCSharpEmittedStringLiteralsUseCSharpEscapesNegativeControl rewrites the
// emitted doc column back to Go's spelling and requires the check above to go
// red on the text it was told to carry.
func TestCSharpEmittedStringLiteralsUseCSharpEscapesNegativeControl(t *testing.T) {
	t.Parallel()
	files, err := Generate(csharpLiteralUnit(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := csharpLiteralFindings(files, csLiteralText); len(got) != 0 {
		t.Fatalf("the unmodified artifacts already fail the check: %v", got)
	}
	replaced := csharpRevertDocColumn(files)
	if replaced == 0 {
		t.Fatal("negative-control anchor: no doc-column literal carried the schema text")
	}
	findings := csharpLiteralFindings(files, csLiteralText)
	if len(findings) == 0 {
		t.Fatal("NEGATIVE CONTROL FAILED: the doc column reverted to Go's spelling stayed green")
	}
	if joined := strings.Join(findings, "\n"); !strings.Contains(joined, csLiteralSentinel) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the check went red, but not on the doc text:\n%s", joined)
	}
}

func csharpLiteralUnit(t *testing.T) *ir.Unit {
	t.Helper()
	u := unitFrom(t, `package probe
table Root {
    value int32
    choice Choice
}
union Choice {
    a int32
    b bool
}
`)
	st := u.Tables["Root"]
	if st == nil {
		t.Fatal("the probe unit has no Root table")
	}
	st.Doc, st.Tags = csLiteralText, []string{csLiteralText}
	for _, f := range st.Fields {
		f.Doc, f.Tags, f.JsonKey = csLiteralText, []string{csLiteralText}, csLiteralText
	}
	un := u.Unions["Choice"]
	if un == nil {
		un = u.TableUnions["Choice"]
	}
	if un == nil {
		t.Fatal("the probe unit has no Choice union")
	}
	un.Doc, un.Tags = csLiteralText, []string{csLiteralText}
	for i := range un.Variants {
		un.Variants[i].Doc, un.Variants[i].Tags = csLiteralText, []string{csLiteralText}
		if f := un.Variants[i].F; f != nil {
			f.Doc, f.Tags, f.JsonKey = csLiteralText, []string{csLiteralText}, csLiteralText
		}
	}
	return u
}

// csharpLiteralFindings returns one line per emitted literal that carries the
// schema text but does not decode to it. A literal with no sentinel is not
// carrying the text and is left alone.
func csharpLiteralFindings(files map[string][]byte, want string) []string {
	var findings []string
	found := 0
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, lit := range scanCSStringLiterals(string(files[name])) {
			if lit.err != nil {
				if strings.Contains(lit.raw, csLiteralSentinel) {
					findings = append(findings, fmt.Sprintf("%s: a literal carrying the schema text does not decode: %v", name, lit.err))
				}
				continue
			}
			if lit.value == want {
				found++
			}
			if strings.Contains(lit.value, csLiteralSentinel) && lit.value != want {
				findings = append(findings, fmt.Sprintf("%s: a literal carrying the schema text decodes to %q, not %q", name, lit.value, want))
			}
		}
	}
	if found == 0 {
		findings = append(findings, fmt.Sprintf("no emitted literal decodes to the schema text %q", want))
	}
	return findings
}

// csharpRevertDocColumn rewrites each emitted doc-column literal that carries
// the schema text with Go's strconv.Quote spelling, the reversed witness.
func csharpRevertDocColumn(files map[string][]byte) int {
	replaced := 0
	for name, data := range files {
		src := string(data)
		var out strings.Builder
		last := 0
		for _, lit := range scanCSStringLiterals(src) {
			if !lit.regular || !strings.HasSuffix(src[:lit.start], "Doc = ") {
				continue
			}
			if lit.err != nil && !strings.Contains(lit.raw, csLiteralSentinel) {
				continue
			}
			if !strings.Contains(lit.value, csLiteralSentinel) {
				continue
			}
			out.WriteString(src[last:lit.start])
			out.WriteString(strconv.Quote(csLiteralText))
			last = lit.end
			replaced++
		}
		out.WriteString(src[last:])
		files[name] = []byte(out.String())
	}
	return replaced
}

type csLiteral struct {
	start   int
	end     int
	raw     string
	value   string
	err     error
	regular bool
}

// scanCSStringLiterals finds the double-quoted literals in C# source, skipping
// comments and character literals, and decodes each under C#'s rules.
func scanCSStringLiterals(src string) []csLiteral {
	var out []csLiteral
	for i := 0; i < len(src); {
		switch {
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '/':
			i += 2
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			if i+1 < len(src) {
				i += 2
			}
		case src[i] == '\'':
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
		case src[i] == '@' && i+1 < len(src) && src[i+1] == '"':
			start := i
			i += 2
			rawStart := i
			for i < len(src) {
				if src[i] == '"' {
					if i+1 < len(src) && src[i+1] == '"' {
						i += 2
						continue
					}
					break
				}
				i++
			}
			raw := src[rawStart:i]
			if i < len(src) {
				i++
			}
			out = append(out, csLiteral{start: start, end: i, raw: raw, value: strings.ReplaceAll(raw, `""`, `"`)})
		case src[i] == '"':
			start := i
			i++
			rawStart := i
			for i < len(src) {
				if src[i] == '\\' {
					i += 2
					continue
				}
				if src[i] == '"' {
					break
				}
				i++
			}
			raw := src[rawStart:i]
			if i < len(src) {
				i++
			}
			value, err := decodeCSString(raw)
			out = append(out, csLiteral{start: start, end: i, raw: raw, value: value, err: err, regular: true})
		default:
			i++
		}
	}
	return out
}

// decodeCSString decodes one C# string literal body: the simple escapes, \x
// with one to four hex digits, \u with four and \U with eight.
func decodeCSString(raw string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(raw); {
		if raw[i] != '\\' {
			b.WriteByte(raw[i])
			i++
			continue
		}
		i++
		if i >= len(raw) {
			return "", fmt.Errorf("trailing backslash")
		}
		e := raw[i]
		i++
		switch e {
		case '\'':
			b.WriteByte('\'')
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case '0':
			b.WriteByte(0)
		case 'a':
			b.WriteByte(7)
		case 'b':
			b.WriteByte(8)
		case 'f':
			b.WriteByte(12)
		case 'n':
			b.WriteByte(10)
		case 'r':
			b.WriteByte(13)
		case 't':
			b.WriteByte(9)
		case 'v':
			b.WriteByte(11)
		case 'x':
			j := i
			for j < len(raw) && j < i+4 && isCSHex(raw[j]) {
				j++
			}
			if j == i {
				return "", fmt.Errorf(`\x with no hex digits`)
			}
			n, err := strconv.ParseUint(raw[i:j], 16, 32)
			if err != nil {
				return "", fmt.Errorf(`bad \x escape: %v`, err)
			}
			b.WriteRune(rune(n))
			i = j
		case 'u':
			if i+4 > len(raw) {
				return "", fmt.Errorf(`short \u escape`)
			}
			n, err := strconv.ParseUint(raw[i:i+4], 16, 32)
			if err != nil {
				return "", fmt.Errorf(`bad \u escape: %v`, err)
			}
			b.WriteRune(rune(n))
			i += 4
		case 'U':
			if i+8 > len(raw) {
				return "", fmt.Errorf(`short \U escape`)
			}
			n, err := strconv.ParseUint(raw[i:i+8], 16, 64)
			if err != nil {
				return "", fmt.Errorf(`bad \U escape: %v`, err)
			}
			b.WriteRune(rune(n))
			i += 8
		default:
			return "", fmt.Errorf(`unknown escape \%c`, e)
		}
	}
	return b.String(), nil
}

func isCSHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
