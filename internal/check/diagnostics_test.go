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

		// ---- fixed point and the 128-bit family (SPEC §4.3, §4.6) ----
		{name: "fixed I+F is not a storage width", want: "must equal a storage width",
			src: "package t\ntype T { x fixed(16, 15) [min = 0, max = 1] }\n"},
		{name: "fixed with zero integer bits", want: "at least one integer bit",
			src: "package t\ntype T { x fixed(0, 32) [min = 0, max = 1] }\n"},
		{name: "fixed with negative fractional bits", want: "cannot be negative",
			src: "package t\ntype T { x fixed(24, -8) [min = 0, max = 1] }\n"},
		{name: "fixed without bounds", want: "requires [min = A, max = B]",
			src: "package t\ntype T { x fixed(48, 16) }\n"},
		{name: "fixed bounds outside the Q format", want: "do not fit fixed(8, 8)",
			src: "package t\ntype T { x fixed(8, 8) [min = -300, max = 300] }\n"},
		{name: "fixed with resolution", want: "resolution applies to float",
			src: "package t\ntype T { x fixed(16, 16) [min = 0, max = 1, resolution = 0.1] }\n"},
		// fixed defaults are LEGAL since 2026-08-12 (whole units, exact) — the
		// old rejection case lives on as the good corner below; what stays
		// illegal is inexactness and range violation (cases at the bottom).
		{name: "bare int128", want: "int128 requires",
			src: "package t\ntype T { x int128 }\n"},
		{name: "uint128 with a range", want: "not valid on uint128",
			src: "package t\ntype T { x uint128 [min = 0, max = 5] }\n"},
		{name: "int128 range above its storage", want: "does not fit its declared storage int128",
			src: "package t\ntype T { x int128 [min = 0, max = 340282366920938463463374607431768211456] }\n"},

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
		{name: "fixed-composite quantize scale must be a power of two", want: "positive power of two",
			src: "package t\ntype V { x fixed(16, 16) [min = -8, max = 8] }\nobject O { p V [interpolate, quantize = 100] \n b bool }\n"},
		{name: "fixed-composite quantize cannot exceed the storage's fraction", want: "cannot be finer than the storage",
			src: "package t\ntype V { x fixed(8, 8) [min = -8, max = 8] }\nobject O { p V [interpolate, quantize = 512] \n b bool }\n"},
		{name: "fixed-composite quantize takes no max", want: "the bound comes from the components",
			src: "package t\ntype V { x fixed(16, 16) [min = -8, max = 8] }\nobject O { p V [interpolate, quantize = 256, max = 8] \n b bool }\n"},
		// an unbounded fixed component is already illegal by §4.3 (bounds are
		// part of the wire format) — the narrowing rule never sees one alone;
		// this documents the error the author gets, and that the composite
		// rule survives the multi-error pass beside it
		{name: "fixed-composite quantize under an unbounded component", want: "requires [min = A, max = B]",
			src: "package t\ntype V { x fixed(16, 16) }\nobject O { p V [interpolate, quantize = 256] \n b bool }\n"},
		{name: "fixed-composite quantize is 64-bit at most", want: "wider than 64 bits",
			src: "package t\ntype V { x fixed(112, 16) [min = -8, max = 8] }\nobject O { p V [interpolate, quantize = 256] \n b bool }\n"},
		{name: "mixed float/fixed composite falls to the float rule", want: "every component of V to be a float scalar",
			src: "package t\ntype V { x fixed(16, 16) [min = -8, max = 8]\n y float64 }\nobject O { p V [interpolate, quantize = 256, max = 8] \n b bool }\n"},
		// the deferred pass also CLOSES a latent hole: with the composite in
		// a later-sorting file, the inline rule used to validate components
		// against an empty shell — vacuously passing anything
		{name: "non-float non-fixed component rejected across file order", want: "every component of V to be a float scalar",
			srcs: map[string]string{
				"A_Objects.schema": "package t\nobject O { p V [interpolate, quantize = 10, max = 1] \n b bool }\n",
				"Z_Types.schema":   "package t\ntype V { x float64\n s string(8) }\n"},
		},

		// ---- namespace, names, claims ----
		{name: "duplicate declaration", want: "duplicate declaration",
			src: "package t\ntype A { x uint8 }\nenum A { B }\n"},
		{name: "duplicate field across branches", want: "duplicate field",
			src: "package t\ntype T {\n    c bool\n    if c { x uint8 } else { x uint8 }\n}\n"},
		{name: "reserved word in a target language", want: "reserved word",
			src: "package t\ntype T { class uint8 }\n"},
		{name: "Go export-casing collision", want: "both become AtRest",
			src: "package t\ntype T {\n    at_rest bool\n    atRest bool\n}\n"},
		{name: "MessageType is a claimed name", want: "generated message dispatch surface",
			src: "package t\nmessage M { }\nenum MessageType { A }\n"},
		{name: "a decl collides with an enum's flat variant constant (Go)", want: "variant constant",
			src: "package t\nenum Team { Red }\ntype TeamRed { x uint8 }\n"},
		{name: "a decl collides with a message's tag constant", want: "tag constant",
			src: "package t\nmessage Chat { }\ntype MessageTypeChat { x uint8 }\n"},
		{name: "a decl collides with a flags mask constant", want: "mask constant",
			src: "package t\nflags Caps { Fast }\nconst CapsFast = 1\n"},
		{name: "two decls whose GENERATED symbols collide", want: "both generate the symbol WriteFooMaxBits",
			src: "package t\ntype WriteFoo { x uint8 }\ntype FooMaxBits { y uint8 }\n"},
		{name: "a decl shadows a name generated Rust references unqualified", want: "references unqualified",
			src: "package t\ntype Default { x uint8 }\n"},
		{name: "two enum variants collide as Rust associated constants", want: "associated constant FOO_BAR",
			src: "package t\nenum E { FOOBar, FooBar }\ntype T { e E }\n"},
		{name: "field collides with its sibling's length companion", want: "length companion",
			src: "package t\ntype T {\n    text string(8)\n    text_length uint8\n}\n"},
		{name: "field collides with its sibling's count companion", want: "count companion",
			src: "package t\ntype T {\n    items [<= 4]uint8\n    items_count uint8\n}\n"},
		{name: "message field would shadow the dispatch method", want: "dispatch method",
			src: "package t\nmessage M { message_type uint8 }\n"},
		{name: "field export equals its declaring type's name (C# CS0542)", want: "enclosing type",
			src: "package t\nmessage Timescale { timescale float64 }\n"},
		{name: "object field export equals a generated family class name", want: "enclosing class",
			src: "package t\nobject Ship { ship_state uint8 [interpolate, min = 0, max = 3] }\n"},
		{name: "float triple degenerate at float32", want: "degenerate at float32",
			src: "package t\ntype T { x float32 [min = 1.0, max = 1.00000001, resolution = 0.000000001] }\n"},
		{name: "resolution collapses to zero at float32", want: "collapses to zero at float32",
			src: "package t\ntype T { x float32 [min = 0.0, max = 1.0, resolution = 1e-46] }\n"},
		{name: "enum max above the 32-bit tag wire", want: "32-bit tag wire",
			src: "package t\nenum E [max = 2147483648] { A }\ntype T { e E }\n"},
		{name: "message field would shadow the C# Type dispatch property", want: "Type dispatch property",
			src: "package t\nmessage M { type_ uint8 }\n"},
		{name: "a message named Type cannot carry its own C# dispatch property", want: "Type dispatch property",
			src: "package t\nmessage Type { }\n"},
		{name: "a decl named Schema collides with the C# Schema class", want: "C# Schema class",
			src: "package t\ntype Schema { x uint8 }\n"},
		{name: "a decl collides with a message's generated Zero helper (C#)", want: "both generate the symbol ZeroChat",
			src: "package t\nmessage Chat { }\ntype ZeroChat { x uint8 }\n"},
		{name: "two types whose Rust snake spellings collide", want: "both generate the symbol write_ab_test",
			src: "package t\ntype ABTest { x uint8 }\ntype AbTest { y uint8 }\n"},
		{name: "two consts whose Rust constant spellings collide", want: "both generate the symbol AB_MAX",
			src: "package t\nconst ABMax = 1\nconst AbMax = 2\n"},
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

		// ---- native type mapping (SPEC §4.2) ----
		{name: "cpp_native without cpp_include", want: "cpp_native and cpp_include go together",
			src: "package t\ntype V [cpp_native = VMath] { x float64 }\n"},
		{name: "cpp_include without cpp_native", want: "cpp_native and cpp_include go together",
			src: "package t\ntype V [cpp_include = \"v.h\"] { x float64 }\n"},
		{name: "cpp_native takes an identifier", want: "cpp_native takes an identifier",
			src: "package t\ntype V [cpp_native = \"VMath\", cpp_include = \"v.h\"] { x float64 }\n"},
		{name: "cpp_include takes a string", want: "cpp_include takes a quoted header path",
			src: "package t\ntype V [cpp_native = VMath, cpp_include = vh] { x float64 }\n"},
		{name: "a valued type attr that is not a binding is still rejected", want: "bare identifier",
			src: "package t\ntype V [vec3 = 4] { x float64 }\n"},
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
		srcs map[string]string
	}{
		{name: "nested if with cond in the same branch",
			src: "package t\ntype T {\n    a bool\n    if a {\n        b bool\n        if b { x uint8 }\n    }\n}\n"},
		{name: "fixed default in whole units, exactly representable (the reopened door)",
			src: "package t\ntype Q { w fixed(2, 30) [min = -1, max = 1] = 1.0 \n x fixed(16, 16) [min = 0, max = 100] = 0.5 \n y fixed(48, 16) [min = -10, max = 10] = 3 }\n"},
		{name: "field named flags at block scope",
			src: "package t\nflags F { A }\ntype T { flags F }\n"},
		{name: "message as a field type",
			src: "package t\nmessage M { x uint8 }\ntype T { m M }\n"},
		{name: "empty type and empty message",
			src: "package t\ntype T { }\nmessage M { }\n"},
		{name: "full-range uint64",
			src: "package t\ntype T { n uint64 [min = 0, max = 18446744073709551615] }\n"},
		{name: "fixed at every storage width, F = 0, and the sign-bit-only corner",
			src: "package t\ntype T {\n    a fixed(8, 8) [min = -100, max = 100]\n    b fixed(16, 16) [min = -180, max = 180]\n    c fixed(32, 0) [min = 0, max = 1000000]\n    d fixed(48, 16) [min = -30000, max = 30000]\n    e fixed(112, 16) [min = -1000000, max = 1000000]\n    f fixed(1, 63) [min = -1, max = 0]\n}\n"},
		{name: "int128 with a range only 128 bits can hold, and raw uint128",
			src: "package t\ntype T {\n    flux int128 [min = -1267650600228229401496703205376, max = 1267650600228229401496703205376]\n    id   uint128\n}\n"},
		{name: "128-bit defaults inside their range",
			src: "package t\ntype T {\n    a int128 [min = -10, max = 10] = -5\n    b uint128 = 7\n}\n"},
		{name: "a [local] fixed object field carries no bounds (it reaches no wire)",
			src: "package t\nobject O {\n    cache fixed(16, 16) [local]\n    size  uint8 [interpolate, min = 0, max = 100]\n}\n"},
		{name: "headroom enum with non-variant wire values",
			src: "package t\nenum E [max = 15] { A, B }\ntype T { e E }\n"},
		{name: "field named message_type on a plain type (only messages claim the method)",
			src: "package t\ntype T { message_type uint8 }\n"},
		{name: "a type named WriteFoo with no type Foo anywhere",
			src: "package t\ntype WriteFoo { x uint8 }\n"},
		{name: "messages spread across two files (aspect layout is not enforced)",
			srcs: map[string]string{
				"A.schema": "package t\nmessage Ping { x uint8 }\n",
				"B.schema": "package t\nmessage Pong { y uint8 }\n",
			}},
		// the game's exact file order: Objects.schema sorts BEFORE
		// Types.schema, so at the object's resolution the composite is a
		// bare shell — rule 2b classification must wait for the full
		// component list (the deferred pass; found the day rule 2b landed)
		{name: "fixed-composite quantize with the composite in a later-sorting file",
			srcs: map[string]string{
				"A_Objects.schema": "package t\nobject O { rotation Q [interpolate, quantize = 1024] \n b bool }\n",
				"Z_Types.schema":   "package t\ntype Q { x fixed(2, 30) [min = -1, max = 1]\n y fixed(2, 30) [min = -1, max = 1]\n z fixed(2, 30) [min = -1, max = 1]\n w fixed(2, 30) [min = -1, max = 1] = 1.0 }\n"},
		},
		{name: "float-composite quantize with the composite in a later-sorting file",
			srcs: map[string]string{
				"A_Objects.schema": "package t\nobject O { p V [interpolate, quantize = 10, max = 1] \n b bool }\n",
				"Z_Types.schema":   "package t\ntype V { x float64\n y float64 }\n"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := tc.srcs
			if sources == nil {
				sources = map[string]string{"T.schema": tc.src}
			}
			if errs := runUnit(t, sources); len(errs) > 0 {
				t.Fatalf("legal schema rejected: %v", errs)
			}
		})
	}
}
