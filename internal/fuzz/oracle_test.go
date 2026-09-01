// The structural oracle over generated output. The fuzzer's old contract
// stopped at "the backend returned bytes without panicking" — but the defect
// register's worst emitter bugs (duplicate C functions, colliding names,
// unparseable output) all RETURN bytes happily. This oracle runs on every
// fuzz iteration and rejects the classes that are cheap to detect
// structurally; the compile fuzz (compile_test.go) carries the classes that
// need a real compiler.
//
// Design rule: zero false positives beats maximal coverage. Every check here
// is one a target-language compiler would hard-error on:
//
//   - Go: the file must parse (go/parser is in-process and cheap), and no two
//     top-level declarations may share a name — Go has no overloading.
//   - Rust: no two same-namespace `pub` items may share a name — Rust has no
//     overloading either. Matched at column 0, where this emitter puts every
//     top-level item.
//   - C / C++ / C#: overloading (C++/C#) and legitimately repeated member
//     lines (`public int A;` in two classes) make name-level checks lie, so
//     the check is exact-duplicate DEFINITION LINES within one file — two
//     byte-identical `inline bool WriteShip(...)` openers in one translation
//     unit are a redefinition whatever the bodies say. Pure forward
//     declarations (`struct ShipData;`) may repeat and are excluded.
package fuzz_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
	"testing"
)

func checkGenerated(t *testing.T, backend string, out map[string][]byte) {
	t.Helper()
	for name, data := range out {
		switch {
		case strings.HasSuffix(name, ".go"):
			checkGoSource(t, backend, name, data)
		case strings.HasSuffix(name, ".rs"):
			checkRustDups(t, backend, name, data)
		case strings.HasSuffix(name, ".h"):
			checkDupDefLines(t, backend, name, data, cFamilyDefLine)
		case strings.HasSuffix(name, ".cs"):
			checkDupDefLines(t, backend, name, data, csTypeDefLine)
		case strings.HasSuffix(name, ".dart"):
			checkDupDefLines(t, backend, name, data, dartTypeDefLine)
		case strings.HasSuffix(name, ".js"):
			checkDupDefLines(t, backend, name, data, jsDefLine)
		}
	}
}

// checkGoSource: generated Go must parse, and no two top-level declarations
// may collide. go/parser gives us this exactly — no regex guesswork.
func checkGoSource(t *testing.T, backend, name string, data []byte) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, data, 0)
	if err != nil {
		t.Fatalf("%s: %s is not valid Go syntax — the emitter shipped bytes a Go compiler rejects:\n%v\n---- %s ----\n%s",
			backend, name, err, name, data)
	}
	seen := map[string]bool{}
	flag := func(key string) {
		if key == "" || strings.HasSuffix(key, "._") {
			return // blank identifiers may repeat
		}
		if seen[key] {
			t.Fatalf("%s: %s declares %q twice at top level — a Go compiler rejects the file:\n---- %s ----\n%s",
				backend, name, key, name, data)
		}
		seen[key] = true
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			recv := ""
			if d.Recv != nil && len(d.Recv.List) == 1 {
				recv = typeKey(d.Recv.List[0].Type) + "."
			}
			flag(recv + d.Name.Name)
		case *ast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					flag(s.Name.Name)
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if n.Name != "_" {
							flag(n.Name)
						}
					}
				}
			}
		}
	}
}

func typeKey(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.StarExpr:
		return typeKey(e.X)
	case *ast.Ident:
		return e.Name
	default:
		return fmt.Sprintf("%T", e)
	}
}

// rustItem matches a top-level public item as this emitter spells them:
// column 0, `pub` keyword, name right after the item keyword. Items inside
// impl blocks are indented and never match.
var rustItem = regexp.MustCompile(`^pub (fn|struct|enum|const|static|mod|trait|type|union) ([A-Za-z_][A-Za-z0-9_]*)`)

// rust namespaces: struct/enum/trait/type/union live in the type namespace,
// fn/const/static in the value namespace, mod in its own — same-namespace
// name collisions are hard errors, cross-namespace ones mostly are not
// (unit structs blur this; we stay on the safe side).
var rustNamespace = map[string]string{
	"struct": "type", "enum": "type", "trait": "type", "type": "type", "union": "type",
	"fn": "value", "const": "value", "static": "value",
	"mod": "mod",
}

func checkRustDups(t *testing.T, backend, name string, data []byte) {
	t.Helper()
	seen := map[string]int{}
	for i, line := range strings.Split(string(data), "\n") {
		m := rustItem.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := rustNamespace[m[1]] + " " + m[2]
		if prev, dup := seen[key]; dup {
			t.Fatalf("%s: %s declares %s %q twice (lines %d and %d) — rustc rejects the file:\n---- %s ----\n%s",
				backend, name, m[1], m[2], prev, i+1, name, data)
		}
		seen[key] = i + 1
	}
}

// cFamilyDefLine matches lines that OPEN a top-level definition in the C and
// C++ emitters' output shapes (both emit at column 0 inside the namespace /
// extern block). Kept deliberately narrow: a miss costs coverage, a false
// match costs a bogus crash report. The SCHEMA_*_INLINE spellings are the
// C++ wire spine's inlining macros (the read demand, and the write switch
// defaulting to `inline`) — without them here the entire generated wire
// surface would be invisible to this check.
var cFamilyDefLine = regexp.MustCompile(`^(static |inline |constexpr |struct |class |enum |typedef |union |SCHEMA_WRITE_INLINE |SCHEMA_READ_INLINE )`)

// csTypeDefLine matches C# type declarations (indented inside the namespace).
// Member and method lines are excluded on purpose: identical member text in
// two classes is legal and common, and repeated `partial class` openers are
// legal C# an emitter may legitimately produce, so `partial` is excluded too.
var csTypeDefLine = regexp.MustCompile(`^(public |internal |static )*(sealed |static )*(class|struct|enum|interface) `)

// dartTypeDefLine matches Dart top-level type declarations (column 0).
// Members and methods are excluded for the same reason as C#'s.
var dartTypeDefLine = regexp.MustCompile(`^(abstract )?(final |base |sealed )?(class|enum|extension|mixin) `)

// jsDefLine matches the js emitters' EXPORTED surface only: checkDupDefLines
// trims lines before matching, so a bare `const ` would also match the
// function-local consts the codecs emit legitimately (e0 aliases in element
// loops). `export` cannot appear in function scope, so it is top-level by
// construction — the miss on unexported module-scope consts is the cheap
// side of the harness's own trade (a false match costs a bogus report).
var jsDefLine = regexp.MustCompile(`^export (const |class |function |function\* )`)

// pure forward declarations may legally repeat.
var forwardDecl = regexp.MustCompile(`^(struct|class|enum|union)\s+[A-Za-z_][A-Za-z0-9_]*\s*;$`)

func checkDupDefLines(t *testing.T, backend, name string, data []byte, def *regexp.Regexp) {
	t.Helper()
	seen := map[string]int{}
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if !def.MatchString(line) || forwardDecl.MatchString(line) {
			continue
		}
		if prev, dup := seen[line]; dup {
			t.Fatalf("%s: %s repeats a definition line (lines %d and %d) — a redefinition in any consumer that includes it:\n  %s\n---- %s ----\n%s",
				backend, name, prev, i+1, line, name, data)
		}
		seen[line] = i + 1
	}
}

// The matchers' own discrimination, pinned: what each must catch and what it
// must NOT (checkDupDefLines trims before matching, so a matcher that hits
// function-local lines files bogus crash reports).
func TestDefLineMatchers(t *testing.T) {
	match := []struct {
		re   *regexp.Regexp
		line string
		want bool
	}{
		{jsDefLine, "export const ProbeId = 244837814094590n;", true},
		{jsDefLine, "export class ProbeHeader {", true},
		{jsDefLine, "export function writeProbeHeader(value, stream) {", true},
		{jsDefLine, "const e0 = value.Items[i0];", false}, // function-local alias, trimmed
		{jsDefLine, "const NUMBER_SCRATCH = { value: 0 };", false},
		{dartTypeDefLine, "final class ProbeHeader {", true},
		{dartTypeDefLine, "abstract final class Weapon {", true},
		{dartTypeDefLine, "enum Team {", true},
		{dartTypeDefLine, "extension ProbeWire on ProbeHeader {", true},
		{dartTypeDefLine, "int w = 1073741824;", false},
		{dartTypeDefLine, "final int hi;", false},
	}
	for _, c := range match {
		if got := c.re.MatchString(c.line); got != c.want {
			t.Errorf("%v on %q: got %v, want %v", c.re, c.line, got, c.want)
		}
	}
}
