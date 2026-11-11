package cpptable

// A C++ string literal the table emitter writes from schema text decodes,
// under C++'s own escape rules, to exactly the schema's bytes (SPEC §4.1,
// docs/SPEC-TABLES.md §8.1, §16.4). A Go strconv.Quote spelling is not that
// literal: C++ reads \xNN greedily, so a following hex digit extends it.
// The positive test scans every emitted file's string literals with a C++
// literal lexer; the negative control holds the lexer to refusing both
// spellings this rule exists to exclude.
//
// The schema language's tags are identifiers ([A-Za-z_][A-Za-z0-9_]*,
// SPEC §4.1), so they refuse every byte below; a JSON key is a quoted string
// scanned byte-for-byte with no escapes (SPEC §4.2), so it carries every
// special byte except the closing quote; a doc comment is a line comment and
// carries all of them.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// cppSchemaErrors parses and checks one schema source, answering the
// diagnostics rather than failing so a test can say which spellings the
// language refuses.
func cppSchemaErrors(src string) []error {
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		return perrs
	}
	_, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	return cerrs
}

// cppEmitInDir writes one schema into dir, parses and checks it from the
// file, and generates its C++ table tree in process.
func cppEmitInDir(t *testing.T, dir, src string) map[string][]byte {
	t.Helper()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f, perrs := parser.Parse("Probe.schema", raw)
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: raw, AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	files, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return files
}

func isCppHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func cppHexValue(b string) (uint64, error) {
	var v uint64
	for i := 0; i < len(b); i++ {
		c := b[i]
		v <<= 4
		switch {
		case c >= '0' && c <= '9':
			v += uint64(c - '0')
		case c >= 'a' && c <= 'f':
			v += uint64(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			v += uint64(c - 'A' + 10)
		default:
			return 0, fmt.Errorf("not a hex digit %q", c)
		}
	}
	return v, nil
}

// decodeCppString decodes one double-quoted C++ narrow string literal into
// the bytes it denotes. It implements the escapes the language defines for
// such a literal, and refuses a hex escape that runs past the byte it must
// denote because C++ consumes every following hex digit.
func decodeCppString(raw string) ([]byte, error) {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return nil, fmt.Errorf("malformed string literal")
	}
	body := raw[1 : len(raw)-1]
	out := make([]byte, 0, len(body))
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != '\\' {
			out = append(out, c)
			continue
		}
		i++
		if i >= len(body) {
			return nil, fmt.Errorf("trailing backslash")
		}
		switch e := body[i]; e {
		case 'n':
			out = append(out, '\n')
		case 't':
			out = append(out, '\t')
		case 'r':
			out = append(out, '\r')
		case 'a':
			out = append(out, 0x07)
		case 'b':
			out = append(out, 0x08)
		case 'f':
			out = append(out, 0x0c)
		case 'v':
			out = append(out, 0x0b)
		case '\\', '"', '\'', '?':
			out = append(out, e)
		case 'x':
			j := i + 1
			for j < len(body) && isCppHexDigit(body[j]) {
				j++
			}
			if j == i+1 {
				return nil, fmt.Errorf("hex escape with no digits")
			}
			if j-(i+1) > 2 {
				return nil, fmt.Errorf("hex escape consumes following hex digits")
			}
			v, err := cppHexValue(body[i+1 : j])
			if err != nil {
				return nil, err
			}
			out = append(out, byte(v))
			i = j - 1
		case 'u', 'U':
			width := 4
			if e == 'U' {
				width = 8
			}
			if i+width >= len(body) {
				return nil, fmt.Errorf("short \\%c escape", e)
			}
			v, err := cppHexValue(body[i+1 : i+1+width])
			if err != nil {
				return nil, err
			}
			r := rune(v)
			if !utf8.ValidRune(r) {
				return nil, fmt.Errorf("\\%c escape names no Unicode scalar value", e)
			}
			out = utf8.AppendRune(out, r)
			i += width
		default:
			if e >= '0' && e <= '7' {
				j := i
				for j < len(body) && j < i+3 && body[j] >= '0' && body[j] <= '7' {
					j++
				}
				v, err := strconv.ParseUint(body[i:j], 8, 16)
				if err != nil {
					return nil, err
				}
				out = append(out, byte(v))
				i = j - 1
				continue
			}
			return nil, fmt.Errorf("unknown escape \\%c", e)
		}
	}
	return out, nil
}

// lexCppStringLiterals returns every double-quoted string literal in src,
// decoded, and every literal the lexer refuses. It skips comments, character
// literals and everything that is not a quoted literal, so a quote inside a
// comment is not mistaken for one.
func lexCppStringLiterals(src string) ([]string, []error) {
	var out []string
	var errs []error
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			if i+1 < len(src) {
				i += 2
			}
		case c == '\'':
			i++
			for i < len(src) && src[i] != '\'' {
				if src[i] == '\\' && i+1 < len(src) {
					i += 2
				} else {
					i++
				}
			}
			if i < len(src) {
				i++
			}
		case c == '"':
			start := i
			i++
			for i < len(src) && src[i] != '"' {
				if src[i] == '\\' && i+1 < len(src) {
					i += 2
				} else {
					i++
				}
			}
			if i >= len(src) {
				errs = append(errs, fmt.Errorf("unterminated string literal starting %q", src[start:min(start+40, len(src))]))
				return out, errs
			}
			i++
			raw := src[start:i]
			value, err := decodeCppString(raw)
			if err != nil {
				errs = append(errs, fmt.Errorf("%v: %s", err, raw))
				continue
			}
			out = append(out, string(value))
		default:
			i++
		}
	}
	return out, errs
}

// TestCppEmittedStringLiteralsUseCppEscapes builds a schema whose doc
// comments and JSON keys carry a backslash, a double quote, a tab, a byte
// Go quotes as \xNN followed by a hex digit, U+00A0 and U+FEFF, emits its
// C++ table unit in process, and requires every emitted string literal to
// decode under C++ rules and every one of those schema texts to be present
// as a literal of exactly its bytes. Tags are identifiers, so the language
// refuses every one of those bytes there; the test says so by name.
func TestCppEmittedStringLiteralsUseCppEscapes(t *testing.T) {
	t.Parallel()

	const (
		tab  = "\t"
		ctrl = "\x01"
		nbsp = "\u00a0"
		bom  = "\ufeff"
	)
	tableDoc := `Table doc: backslash \ quote " tab ` + tab + ` control ` + ctrl + `a nbsp ` + nbsp + ` bom ` + bom
	fieldDoc := `Field doc: backslash \ quote " tab ` + tab + ` control ` + ctrl + `a nbsp ` + nbsp + ` bom ` + bom
	trailingDoc := `Field ending in a backslash \`
	controlDoc := `Field with ` + ctrl + `a and ` + nbsp + ` and ` + bom + ` and " and \`
	src := "package probe\n\n" +
		"/// " + tableDoc + "\n" +
		"table Payload {\n" +
		"    /// " + fieldDoc + "\n" +
		"    a string(16) | json = \"a\\b\", tag_one\n" +
		"    /// " + controlDoc + "\n" +
		"    b string(16) | json = \"b" + ctrl + "a" + nbsp + bom + "\", tag_two\n" +
		"    /// " + trailingDoc + "\n" +
		"    c string(16) | json = \"c" + tab + "d\", tag_three\n" +
		"    d string(16) | json = \"d" + nbsp + "e" + bom + "\", tag_four\n" +
		"    e string(16) | json = \"e\\\"\n" +
		"}\n"

	want := []string{
		tableDoc,
		fieldDoc,
		controlDoc,
		trailingDoc,
		`a\b`,
		"b" + ctrl + "a" + nbsp + bom,
		"c" + tab + "d",
		"d" + nbsp + "e" + bom,
		`e\`,
		"tag_one", "tag_two", "tag_three", "tag_four",
	}

	for _, tc := range []struct{ name, tag string }{
		{"backslash", `tag\one`},
		{"double quote", `tag"one`},
		{"tab", "tag\tone"},
		{"control byte", "tag" + ctrl + "one"},
		{"U+00A0", "tag" + nbsp + "one"},
		{"U+FEFF", "tag" + bom + "one"},
	} {
		tagSrc := "package probe\ntable Tagged {\n    x int32 | " + tc.tag + "\n}\n"
		if errs := cppSchemaErrors(tagSrc); len(errs) == 0 {
			t.Errorf("the schema language accepted a tag carrying a %s: tags are identifiers (SPEC §4.1)", tc.name)
		}
	}
	// A JSON key is a quoted string scanned byte-for-byte with no escapes
	// (SPEC §4.2), so the closing quote is the one byte it cannot carry.
	quotedKey := "package probe\ntable Quoted {\n    x int32 | json = \"a\"b\"\n}\n"
	if errs := cppSchemaErrors(quotedKey); len(errs) == 0 {
		t.Error("the schema language accepted a JSON key carrying a double quote; the scanner ends the literal there (SPEC §4.2)")
	}

	files := cppEmitInDir(t, t.TempDir(), src)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	got := make([]string, 0)
	for _, name := range names {
		literals, errs := lexCppStringLiterals(string(files[name]))
		for _, err := range errs {
			t.Errorf("%s: the C++ lexer refused a string literal: %v", name, err)
		}
		got = append(got, literals...)
	}
	seen := make(map[string]bool, len(got))
	for _, literal := range got {
		seen[literal] = true
	}
	for _, text := range want {
		if !seen[text] {
			t.Errorf("no C++ string literal decodes to the schema text %q", text)
		}
	}
}

// TestCppEmittedStringLiteralsUseCppEscapesNegativeControl holds the lexer to
// refusing the two spellings the rule excludes: a Go \xNN escape followed by
// a hex digit, which C++ reads as one longer escape, and an unescaped key
// ending in a backslash, which escapes the literal's closing quote.
func TestCppEmittedStringLiteralsUseCppEscapesNegativeControl(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"hex escape extended by following digit", strconv.Quote("\x01" + "ab"), "hex escape"},
		{"backslash escapes the closing quote", `"a\"`, "unterminated"},
	} {
		_, errs := lexCppStringLiterals(tc.src)
		if len(errs) == 0 {
			t.Errorf("%s: the lexer accepted %s; it must refuse it naming %q", tc.name, tc.src, tc.want)
			continue
		}
		named := false
		for _, err := range errs {
			if strings.Contains(err.Error(), tc.want) {
				named = true
			}
		}
		if !named {
			t.Errorf("%s: the lexer refused %s without naming %q: %v", tc.name, tc.src, tc.want, errs)
		}
	}
}
