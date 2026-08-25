// The break-the-language suite (SPEC §7.2 gate 6's diagnostics half, seeded
// on Glenn's ask 2026-08-05: "look for ways the schema language could break,
// and verify that the compiler catches them"). Every case is an illegal
// schema and the substring its diagnostic must carry — a way to break the
// language that the compiler provably catches, forever.
package check

import (
	"math"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/internal/ast"
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
		{name: "implicit const past int64 (FuzzGeneratedCompiles 0c59b1fd)", want: "does not fit int64, the default constant storage",
			src: "package t\nconst A = 20000000000000000000\n"},
		{name: "implicit const past int64, uint64 range (constexpr narrowing repro)", want: "does not fit int64, the default constant storage",
			src: "package t\nconst A = 18446744073709551615\n"},
		{name: "implicit const below int64 min", want: "does not fit int64, the default constant storage",
			src: "package t\nconst A = -9223372036854775809\n"},
		{name: "min without max", want: "min without max",
			src: "package t\ntype T { h int16 [min = 0] }\n"},
		{name: "inverted range", want: "inverted range",
			src: "package t\ntype T { h int16 [min = 6, max = 5] }\n"},
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
		// ---- ufixed(I, F): the unsigned sibling (Glenn, 2026-08-15: "ufixed
		// is fine" — §9 q17 closed). Same shape rules, unsigned domain, and
		// the diagnostics name the ufixed spelling. ----
		{name: "ufixed bounds below zero", want: "do not fit ufixed(16, 16)",
			src: "package t\ntype T { x ufixed(16, 16) [min = -1, max = 5] }\n"},
		{name: "ufixed with zero integer bits", want: "at least one integer bit",
			src: "package t\ntype T { x ufixed(0, 32) [min = 0, max = 1] }\n"},
		{name: "ufixed I+F is not a storage width", want: "must equal a storage width",
			src: "package t\ntype T { x ufixed(16, 15) [min = 0, max = 1] }\n"},
		{name: "ufixed without bounds", want: "ufixed(48, 16) requires [min = A, max = B]",
			src: "package t\ntype T { x ufixed(48, 16) }\n"},
		{name: "ufixed bounds above the unsigned domain", want: "do not fit ufixed(8, 8)",
			src: "package t\ntype T { x ufixed(8, 8) [min = 0, max = 300] }\n"},
		{name: "ufixed(64, 0) bounds clamp at int64's ceiling", want: "do not fit ufixed(64, 0)",
			src: "package t\ntype T { x ufixed(64, 0) [min = 0, max = 18446744073709551615] }\n"},
		{name: "ufixed default below its unsigned range", want: "outside its range",
			src: "package t\ntype T { x ufixed(16, 16) [min = 0, max = 5] = -1.0 }\n"},
		{name: "ufixed in a table closure", want: "ufixed(I, F) has no table-wire kind",
			src: "package t\ntable T { x ufixed(16, 16) [min = 0, max = 5] }\n"},
		{name: "ufixed components do not narrow (rule 2b is signed-only)", want: "signed fixed(I, F) only",
			src: "package t\ntype V { x ufixed(16, 16) [min = 0, max = 5]\n y ufixed(16, 16) [min = 0, max = 5] }\nobject O { p V [interpolate, quantize = 256] \n b bool }\n"},
		{name: "ufixed with resolution", want: "resolution applies to float",
			src: "package t\ntype T { x ufixed(16, 16) [min = 0, max = 1, resolution = 0.1] }\n"},

		{name: "bare int128", want: "int128 requires",
			src: "package t\ntype T { x int128 }\n"},
		{name: "uint128 with a range", want: "not valid on uint128",
			src: "package t\ntype T { x uint128 [min = 0, max = 5] }\n"},
		{name: "int128 range above its storage", want: "does not fit its declared storage int128",
			src: "package t\ntype T { x int128 [min = 0, max = 340282366920938463463374607431768211456] }\n"},

		// ---- enums, flags, contexts ----
		{name: "variant named None", want: "None",
			src: "package t\nenum E { None, A }\n"},
		{name: "variant named Max", want: "carries its extent as the member Max",
			src: "package t\nenum E { A, Max }\n"},
		{name: "a decl collides with an enum's generated Max extent (Go)", want: "generated Max extent",
			src: "package t\nenum Team { Red }\nconst TeamMax = 3\n"},
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
		// valued attributes written bare. FOUND BY THE FUZZER (internal/fuzz)
		// in 3 seconds: `[min, max]` reached expression evaluation as a nil
		// and panicked the compiler with a stack trace instead of pointing at
		// the typo. One case per valued attribute — the guard is a table, and
		// a table with an untested entry is a table that will lose one.
		{name: "min written without a value", want: "requires a value",
			src: "package t\ntype T { n int32 [min, max = 10] }\n"},
		{name: "max written without a value", want: "requires a value",
			src: "package t\ntype T { n int32 [min = 0, max] }\n"},
		{name: "resolution written without a value", want: "requires a value",
			src: "package t\ntype T { x float32 [min = 0, max = 1, resolution] }\n"},
		{name: "quantize written without a value", want: "requires a value",
			src: "package t\ntype V { x float64 }\nobject O { p V [interpolate, quantize, max = 1] \n b bool }\n"},
		{name: "round written without a value", want: "requires a value",
			src: "package t\nobject O { x float32 [interpolate, min = 0, max = 1, resolution = 0.1, round] \n b bool }\n"},

		// cycle guards: every resolver that can re-enter itself must REJECT,
		// not recurse. Before the enum guard these crashed the compiler with
		// a raw "fatal error: stack overflow" — no diagnostic, no position.
		{name: "enum [max = E.Max] self-cycle", want: "reference cycle",
			src: "package t\nenum Alpha [max = Alpha.Max] { One }\n"},
		{name: "enum [max = E.Max] mutual cycle across files", want: "reference cycle",
			srcs: map[string]string{
				"A_e.schema": "package t\nenum Alpha [max = Zed.Max] { One, Two }\n",
				"Z_e.schema": "package t\nenum Zed [max = Alpha.Max] { Three }\n"},
		},
		{name: "enum [max = E.Max] three-enum cycle", want: "reference cycle",
			src: "package t\nenum A [max = B.Max] { One }\nenum B [max = C.Max] { Two }\nenum C [max = A.Max] { Three }\n"},
		{name: "constant self-cycle", want: "reference cycle",
			src: "package t\nconst A = A + 1\n"},
		{name: "constant mutual cycle", want: "reference cycle",
			src: "package t\nconst A = B + 1\nconst B = A + 1\n"},

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

		// ---- package names that cannot compile (the 2026-08-16 ruling on the
		// compile fuzzer's `package exit` specimen — Glenn, verbatim: "Refuse
		// the colliding names with a clear diagnostic"). Before the rule, the
		// checker said `ok: package exit` and clang rejected the generated
		// `namespace exit` against <cstdlib> with "redefinition of 'exit' as
		// different kind of symbol"; `package for` checked clean and generated
		// keyword namespaces; `package main` generated Go that fails with
		// "function main is undeclared in the main package". All three proven
		// on the pre-change compiler, 2026-08-16. ----
		{name: "package exit collides with libc at C++ namespace scope", want: "C standard library identifier",
			src: "package exit\ntype A { x uint8 }\n"},
		{name: "package free collides with libc", want: "C standard library identifier",
			src: "package free\ntype A { x uint8 }\n"},
		{name: "package time collides with libc", want: "C standard library identifier",
			src: "package time\ntype A { x uint8 }\n"},
		{name: "package memcpy collides with libc", want: "C standard library identifier",
			src: "package memcpy\ntype A { x uint8 }\n"},
		{name: "package errno collides with libc's object-like macro", want: "C standard library identifier",
			src: "package errno\ntype A { x uint8 }\n"},
		{name: "package name is a target reserved word", want: "reserved word",
			src: "package for\ntype A { x uint8 }\n"},
		{name: "package main is not importable Go", want: "cannot be imported",
			src: "package main\ntype A { x uint8 }\n"},

		// ---- non-finite compressed-float parameters (Glenn, 2026-08-15:
		// "attempting to send NaN or INF or anything else through compressed
		// float is non-conforming and should assert out on write too" — the
		// compiler's half of the ruling; the runtimes carry the write
		// asserts). Two levels per parameter: finite at float64, and finite
		// at FLOAT32, where every runtime evaluates the triple. Before the
		// rule, the float32-overflow shapes below CHECKED CLEAN and the C++
		// emitter printed -Inf.0f — a token no C++ compiler accepts. The
		// float64-level infinities ride integer literals, the one spelling
		// that reached +/-Inf silently (a float literal beyond the double's
		// range is a parse error; constant arithmetic is overflow-guarded).
		// NaN has no spelling at all — its arm is TestNonFiniteTripleNaN. ----
		{name: "compressed float min -Inf at float32", want: "min = -1e+39 overflows float32",
			src: "package t\ntype T { x float32 [min = -1e39, max = 1e39, resolution = 1e30] }\n"},
		{name: "compressed float min +Inf at float32", want: "min = 1e+39 overflows float32",
			src: "package t\ntype T { x float32 [min = 1e39, max = 2e39, resolution = 1e30] }\n"},
		{name: "compressed float max +Inf at float32", want: "max = 1e+39 overflows float32",
			src: "package t\ntype T { x float32 [min = 0.0, max = 1e39, resolution = 1e30] }\n"},
		// (a -Inf max cannot be its own diagnostic: min < max puts min below
		// it, so the min arm always fires first — the parameter is still
		// covered, by ordering rather than by a separate message)
		{name: "compressed float resolution +Inf at float32", want: "resolution = 1e+39 overflows float32",
			src: "package t\ntype T { x float32 [min = 0.0, max = 1.0, resolution = 1e39] }\n"},
		{name: "compressed float min +Inf at float64, integer literal", want: "does not fit float64",
			src: "package t\ntype T { x float32 [min = 1" + strings.Repeat("0", 400) + ", max = 1.0, resolution = 0.1] }\n"},
		{name: "compressed float min -Inf at float64, negated integer literal", want: "does not fit float64",
			src: "package t\ntype T { x float32 [min = -1" + strings.Repeat("0", 400) + ", max = 1.0, resolution = 0.1] }\n"},
		// The through-a-const vehicle is refused EARLIER since the #22 guard
		// landed: an implicitly-typed const past int64 fails at its own
		// declaration, so the float64-finiteness arm can no longer be reached
		// through a const — the schema is refused either way, and the float64
		// arm keeps its coverage via the integer-literal cases beside this one.
		{name: "compressed float max +Inf at float64, through a const", want: "does not fit int64, the default constant storage",
			src: "package t\nconst Big = 1" + strings.Repeat("0", 400) + "\ntype T { x float32 [min = 0.0, max = Big, resolution = 0.1] }\n"},
		{name: "compressed float resolution +Inf at float64, integer literal", want: "does not fit float64",
			src: "package t\ntype T { x float32 [min = 0.0, max = 1.0, resolution = 1" + strings.Repeat("0", 400) + "] }\n"},
		{name: "interpolate float64 triple infinite at float32 — rule 4 shares the path", want: "overflows float32, where every runtime evaluates the triple",
			src: "package t\nobject O { x float64 [interpolate, min = -1e39, max = 1e39, resolution = 1e30]\n b bool }\n"},
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
		{name: "quantize scale beyond float64 exactness", want: "not exactly representable in float64",
			src: "package t\ntype V {\n    x float32\n    y float32\n}\nobject O { p V [interpolate, quantize = 9007199254740993, max = 1] }\n"},
		{name: "a type collides with an object view's flat wire function (Rust/C)", want: "both generate the symbol write_ship_data_shallow",
			src: "package t\nobject Ship { x uint8 [interpolate, min = 0, max = 3] }\ntype ShipDataShallow { y uint8 }\n"},
		{name: "a const collides with an enum's C variant #define", want: "variant constants (C form)",
			src: "package t\nenum DriveMode { Ludicrous }\nconst DriveModeLudicrous = 1\ntype T { m DriveMode }\n"},
		{name: "two enums whose C debug-name functions collide", want: "both generate the symbol enum_name_ab_mode",
			src: "package t\nenum ABMode { X }\nenum AbMode { Y }\ntype T { x uint8 }\n"},
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
		{name: "ufixed at every storage width, the full unsigned domains, and the one-bit corner",
			src: "package t\ntype T {\n    a ufixed(8, 8) [min = 0, max = 255]\n    b ufixed(16, 16) [min = 0, max = 360]\n    c ufixed(32, 0) [min = 0, max = 4294967295]\n    d ufixed(48, 16) [min = 0, max = 281474976710655]\n    e ufixed(112, 16) [min = 0, max = 2000000]\n    f ufixed(1, 63) [min = 0, max = 1]\n    g ufixed(16, 16) [min = 3, max = 3]\n}\n"},
		{name: "ufixed default in whole units, exactly representable",
			src: "package t\ntype T { x ufixed(2, 30) [min = 0, max = 1] = 1.0 \n y ufixed(16, 16) [min = 0, max = 100] = 0.5 }\n"},
		{name: "un-narrowed ufixed composite dissolves (rule 2, sign-agnostic delegation)",
			src: "package t\ntype V { x ufixed(48, 16) [min = 0, max = 100]\n y ufixed(48, 16) [min = 0, max = 100] }\nobject O { p V [interpolate] \n b bool }\n"},
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
		// consts are order-free across files (SPEC §4.2) — including the
		// classification of a bare referrer against an explicitly
		// float-typed referent. Both name orders must compile: resolution
		// runs in NAME order, so these two cases put the referent on either
		// side of the referrer.
		{name: "bare const referencing a float-typed const that resolves later",
			src: "package t\nconst Aaa = Mid + 1\nconst Mid float64 = 3\n"},
		{name: "bare const referencing a float-typed const that resolves earlier",
			src: "package t\nconst Zzz = Mid + 1\nconst Mid float64 = 3\n"},
		{name: "the same, across files in both directions",
			srcs: map[string]string{
				"A_use.schema":   "package t\nconst Alpha = Zeta + 1\n",
				"Z_decl.schema":  "package t\nconst Zeta float64 = 3\n",
				"M_other.schema": "package t\nconst Mu = Zeta * 2\n",
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

// TestNonFiniteTripleNaN exercises the NaN arm of the non-finite rule (SPEC
// §4.6; Glenn, 2026-08-15). The grammar cannot spell NaN — a float literal
// beyond the double's range is a parse error, every constant arithmetic
// result is overflow-guarded, and division by zero is refused — so the arm is
// exercised the only way a NaN can exist in the checker's input: planted
// directly in the AST. Defense in depth, proven rather than assumed.
func TestNonFiniteTripleNaN(t *testing.T) {
	src := "package t\ntype T { x float32 [min = 0.5, max = 1.0, resolution = 0.25] }\n"
	f, perrs := parser.Parse("T.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatal(perrs[0])
	}
	planted := false
	for _, d := range f.Decls {
		td, ok := d.(*ast.TypeDecl)
		if !ok {
			continue
		}
		for _, item := range td.Body.Items {
			fld, ok := item.(*ast.Field)
			if !ok {
				continue
			}
			for i := range fld.Attrs {
				if fld.Attrs[i].Key == "min" {
					fld.Attrs[i].Value = &ast.FloatLit{Pos: fld.Attrs[i].Pos, Value: math.NaN(), Text: "NaN"}
					planted = true
				}
			}
		}
	}
	if !planted {
		t.Fatal("no min attribute found to plant the NaN in")
	}
	_, errs := Unit([]SourceFile{{Path: "T.schema", Name: "T.schema", Base: "T", Bytes: []byte(src), AST: f}})
	for _, err := range errs {
		if strings.Contains(err.Error(), "is not finite") {
			return
		}
	}
	t.Fatalf("a NaN compressed-float bound must be rejected as non-finite; got %v", errs)
}

// The package-name refusal is exact, case-sensitive match (C++ namespaces are
// case-sensitive): neighbors of a refused name stay legal, and so does a name
// that merely contains one.
func TestPackageNameNeighborsAccepted(t *testing.T) {
	for _, pkg := range []string{"exits", "exit2", "myexit", "mallocs", "timer", "Exit"} {
		errs := runUnit(t, map[string]string{"a.schema": "package " + pkg + "\ntype A { x uint8 }\n"})
		if len(errs) > 0 {
			t.Fatalf("package %s must stay legal, got %v", pkg, errs)
		}
	}
}
