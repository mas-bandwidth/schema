// The break-the-language suite (SPEC §7.2 gate 6's diagnostics half, seeded
// on Glenn's ask 2026-08-05: "look for ways the schema language could break,
// and verify that the compiler catches them"). Every case is an illegal
// schema and the substring its diagnostic must carry — a way to break the
// language that the compiler provably catches, forever.
package check

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/internal/parser"
)

func runUnit(t *testing.T, sources map[string]string) []error {
	t.Helper()
	var files []SourceFile
	for name, src := range sources {
		f, perrs := parser.Parse(name, []byte(src))
		if len(perrs) > 0 {
			return perrs
		}
		files = append(files, SourceFile{
			Path:  name,
			Name:  name,
			Base:  strings.TrimSuffix(name, ".schema"),
			Bytes: []byte(src),
			AST:   f,
		})
	}
	_, errs := Unit(files)
	return errs
}

func TestDiagnostics(t *testing.T) {
	cases := []struct {
		name string
		src  string // one file; multi-file cases use srcs
		srcs map[string]string
		want string // required substring of some diagnostic
	}{
		// ---- cycles: infinite size, infinite folding ----
		{name: "type cycle, mutual", want: "type composition cycle",
			src: "package t\ntype A { b B }\ntype B { a A }\n"},
		{name: "type cycle, self", want: "type composition cycle",
			src: "package t\ntype A { again A }\n"},
		{name: "type cycle, through a fixed array", want: "type composition cycle",
			src: "package t\ntype A { b [4]B }\ntype B { a A }\n"},
		{name: "const cycle", want: "reference cycle",
			src: "package t\nconst A = B + 1\nconst B = A + 1\n"},
		{name: "const cycle, three-way (the stack-overflow repro)", want: "reference cycle",
			src: "package t\nconst A = B + C\nconst B = C\nconst C = B\n"},

		// ---- back-references: forward, sideways, wrong type ----
		{name: "back-reference that goes forward", want: "dominance",
			src: "package t\ntype T {\n    if flag { x uint8 }\n    flag bool\n}\n"},
		{name: "back-reference into a sibling branch", want: "dominance",
			src: "package t\ntype T {\n    a bool\n    if a { inner bool }\n    if inner { x uint8 }\n}\n"},
		{name: "back-reference to an undeclared field", want: "dominance",
			src: "package t\ntype T {\n    if ghost { x uint8 }\n}\n"},
		{name: "if condition is not a bool", want: "must be a bool",
			src: "package t\ntype T {\n    n uint8\n    if n { x uint8 }\n}\n"},
		{name: "if condition is a bool array", want: "must be a bool",
			src: "package t\ntype T {\n    flags [4]bool\n    if flags { x uint8 }\n}\n"},

		// ---- ranges and storage ----
		{name: "range does not fit storage", want: "does not fit its declared storage",
			src: "package t\ntype T { h int8 [min = 0, max = 1000] }\n"},
		{name: "min without max", want: "min without max",
			src: "package t\ntype T { h int16 [min = 0] }\n"},
		{name: "degenerate range", want: "degenerate range",
			src: "package t\ntype T { h int16 [min = 5, max = 5] }\n"},
		{name: "unknown attribute", want: "unknown attribute",
			src: "package t\ntype T { h int16 [flavor = 3] }\n"},
		{name: "resolution on an integer", want: "resolution applies to float",
			src: "package t\ntype T { h int16 [min = 0, max = 10, resolution = 1] }\n"},
		{name: "range on an enum field", want: "derives its range",
			src: "package t\nenum E { A }\ntype T { e E [min = 0, max = 1] }\n"},
		{name: "float64 compressed in a plain type", want: "compressed float is float32",
			src: "package t\ntype T { v float64 [min = 0, max = 1, resolution = 0.1] }\n"},

		// ---- widths and bounds ----
		{name: "bits zero", want: "outside [1, 64]",
			src: "package t\ntype T { b bits(0) }\n"},
		{name: "bits sixty-five", want: "outside [1, 64]",
			src: "package t\ntype T { b bits(65) }\n"},
		{name: "const value overflows its width", want: "does not fit",
			src: "package t\ntype T { const(256, 8) }\n"},
		{name: "negative wire constant", want: "does not fit",
			src: "package t\ntype T { const(-1, 8) }\n"},
		{name: "string below two", want: "below 2",
			src: "package t\ntype T { s string(1) }\n"},
		{name: "array bound zero", want: "below 1",
			src: "package t\ntype T { a [0]uint8 }\n"},
		{name: "count range backwards", want: "0 <= Min < N",
			src: "package t\ntype T { a [5..3]uint8 }\n"},
		{name: "bound above int32", want: "counts live in int32",
			src: "package t\ntype T { a [<= 5000000000]uint8 }\n"},
		{name: "string bound above int32", want: "lengths live in int32",
			src: "package t\ntype T { s string(5000000000) }\n"},
		{name: "division by zero in a constant", want: "division by zero",
			src: "package t\nconst Z = 0\nconst A = 5 / Z\n"},

		// ---- enums, flags, contexts ----
		{name: "variant named None", want: "None",
			src: "package t\nenum E { None, A }\n"},
		{name: "enum max below variant count", want: "below its variant count",
			src: "package t\nenum E [max = 2] { A, B, C }\n"},
		{name: "Max reference to a non-enum", want: "is not an enum",
			src: "package t\ntype V { x uint8 }\nconst N = V.Max + 1\n"},
		{name: "context without local", want: "legal only beside [local]",
			src: "package t\ncontexts { client }\nobject O { a bool [context = client] \n b uint8 }\n"},
		{name: "undeclared context", want: "not declared",
			src: "package t\ncontexts { client }\nobject O { a bool [local, context = server] \n b uint8 }\n"},
		{name: "context all is reserved", want: "reserved",
			src: "package t\ncontexts { all, client }\n"},

		// ---- objects ----
		{name: "object body admits plain fields only", want: "plain fields only",
			src: "package t\nobject O {\n    a bool\n    if a { x uint8 }\n}\n"},
		{name: "empty object", want: "empty object",
			src: "package t\nobject O { }\n"},
		{name: "object as a field type", want: "not a field type",
			src: "package t\nobject O { a bool }\ntype T { o O }\n"},
		{name: "quantize without interpolate", want: "quantize belongs to the [interpolate]",
			src: "package t\ntype V { x float64 }\nobject O { p V [quantize = 10, max = 1] \n b bool }\n"},
		{name: "quantize on a non-composite", want: "component-wise",
			src: "package t\nobject O { p uint32 [interpolate, quantize = 10, max = 1] \n b bool }\n"},

		// ---- namespace, names, claims ----
		{name: "duplicate declaration", want: "duplicate declaration",
			src: "package t\ntype A { x uint8 }\nenum A { B }\n"},
		{name: "duplicate field across branches", want: "duplicate field",
			src: "package t\ntype T {\n    c bool\n    if c { x uint8 } else { x uint8 }\n}\n"},
		{name: "reserved word in a target language", want: "reserved word",
			src: "package t\ntype T { class uint8 }\n"},
		{name: "Go export-casing collision", want: "Go export-casing",
			src: "package t\ntype T {\n    at_rest bool\n    atRest bool\n}\n"},
		{name: "MessageType is a claimed name", want: "claimed name",
			src: "package t\nmessage M { }\nenum MessageType { A }\n"},
		{name: "package mismatch across files", want: "does not match",
			srcs: map[string]string{
				"A.schema": "package a\ntype T { x uint8 }\n",
				"B.schema": "package b\ntype U { y uint8 }\n",
			}},

		// ---- defaults ----
		{name: "default out of range", want: "outside its range",
			src: "package t\ntype T { h int16 [min = 0, max = 10] = 99 }\n"},
		{name: "default on an array", want: "array takes no specified default",
			src: "package t\ntype T { a [4]uint8 = 1 }\n"},
		{name: "enum default is not a variant", want: "names a variant",
			src: "package t\nenum E { A }\ntype T { e E = Zzz }\n"},
		{name: "bool default is not a bool", want: "true or false",
			src: "package t\ntype T { b bool = 3 }\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := tc.srcs
			if sources == nil {
				sources = map[string]string{"T.schema": tc.src}
			}
			errs := runUnit(t, sources)
			if len(errs) == 0 {
				t.Fatalf("compiled clean — the language broke silently (want %q)", tc.want)
			}
			for _, e := range errs {
				if strings.Contains(e.Error(), tc.want) {
					return
				}
			}
			t.Fatalf("no diagnostic contains %q; got: %v", tc.want, errs)
		})
	}
}

// TestGoodCornersStillCompile guards the other direction: legal corners the
// diagnostics above sit beside must stay legal.
func TestGoodCornersStillCompile(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "nested if with cond in the same branch",
			src: "package t\ntype T {\n    a bool\n    if a {\n        b bool\n        if b { x uint8 }\n    }\n}\n"},
		{name: "field named flags at block scope",
			src: "package t\nflags F { A }\ntype T { flags F }\n"},
		{name: "message as a field type",
			src: "package t\nmessage M { x uint8 }\ntype T { m M }\n"},
		{name: "empty type and empty message",
			src: "package t\ntype T { }\nmessage M { }\n"},
		{name: "full-range uint64",
			src: "package t\ntype T { n uint64 [min = 0, max = 18446744073709551615] }\n"},
		{name: "headroom enum with non-variant wire values",
			src: "package t\nenum E [max = 15] { A, B }\ntype T { e E }\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if errs := runUnit(t, map[string]string{"T.schema": tc.src}); len(errs) > 0 {
				t.Fatalf("legal schema rejected: %v", errs)
			}
		})
	}
}
