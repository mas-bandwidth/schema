// The break-the-language suite (SPEC §7.2 gate 6's diagnostics half): look
// for ways the schema language could break, and verify the compiler catches
// them. Every case is an illegal
// schema and the substring its diagnostic must carry — a way to break the
// language that the compiler provably catches, forever.
package check

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
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

// flags65 spells 65 comma-separated variant names — one past uint64 storage.
var flags65 = func() string {
	names := make([]string, 65)
	for i := range names {
		names[i] = fmt.Sprintf("V%d", i)
	}
	return strings.Join(names, ", ")
}()

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
			src: "package t\ntype T { h int8 | min = 0, max = 1000 }\n"},
		{name: "implicit const past int64 (FuzzGeneratedCompiles 0c59b1fd)", want: "does not fit int64, the default constant storage",
			src: "package t\nconst A = 20000000000000000000\n"},
		{name: "implicit const past int64, uint64 range (constexpr narrowing repro)", want: "does not fit int64, the default constant storage",
			src: "package t\nconst A = 18446744073709551615\n"},
		{name: "implicit const below int64 min", want: "does not fit int64, the default constant storage",
			src: "package t\nconst A = -9223372036854775809\n"},
		{name: "min without max", want: "min without max",
			src: "package t\ntype T { h int16 | min = 0 }\n"},
		{name: "inverted range", want: "inverted range",
			src: "package t\ntype T { h int16 | min = 6, max = 5 }\n"},
		{name: "unknown attribute", want: "unknown attribute",
			src: "package t\ntype T { h int16 | flavor = 3 }\n"},
		{name: "resolution on an integer", want: "resolution applies to float",
			src: "package t\ntype T { h int16 | min = 0, max = 10, resolution = 1 }\n"},
		{name: "range on an enum field", want: "derives its range",
			src: "package t\nenum E { A }\ntype T { e E | min = 0, max = 1 }\n"},
		{name: "float64 compressed in a plain type", want: "compressed float is float32",
			src: "package t\ntype T { v float64 | min = 0, max = 1, resolution = 0.1 }\n"},

		// ---- a range that excludes zero (issue #346) ----
		// Zero initialization is the language's rule, so a field whose range
		// starts above zero or ends below it is BORN outside its own range: the
		// write side refuses the fresh value and a read-side clamp substitutes
		// one the author never wrote. The fix is a declared default in range.
		{name: "a range above zero with no default", want: "excludes zero",
			src: "package t\ntype T { x uint8 | min = 1, max = 255 }\n"},
		{name: "a range below zero with no default", want: "excludes zero",
			src: "package t\ntype T { x int8 | min = -100, max = -1 }\n"},
		{name: "a degenerate range that excludes zero", want: "excludes zero",
			src: "package t\ntype T { x int32 | min = 7, max = 7 }\n"},
		{name: "a fixed range in whole units that excludes zero", want: "excludes zero",
			src: "package t\ntype T { x fixed(16, 16) | min = 3, max = 8 }\n"},
		{name: "a ufixed range that excludes zero", want: "excludes zero",
			src: "package t\ntype T { x ufixed(16, 16) | min = 3, max = 3 }\n"},
		{name: "an int128 range that excludes zero", want: "excludes zero",
			src: "package t\ntype T { x int128 | min = 1, max = 1267650600228229401496703205376 }\n"},
		{name: "a compressed-float range that excludes zero", want: "excludes zero",
			src: "package t\ntype T { v float32 | min = 1, max = 2, resolution = 0.01 }\n"},
		{name: "a table field's range that excludes zero", want: "excludes zero",
			src: "package t\ntable T { hp uint8 | min = 1, max = 100 }\n"},
		// an ARRAY takes no specified default, so for an array the only fix is
		// a range that reaches zero — the diagnostic must say so rather than
		// naming a fix the language refuses
		{name: "an array of a range that excludes zero", want: "widen the range to reach zero",
			src: "package t\ntype T { xs [4]uint8 | min = 1, max = 255 }\n"},
		// the declared default is itself range-checked, on both range kinds
		{name: "a float default outside its compressed range", want: "is outside its range",
			src: "package t\ntype T { v float32 = 5 | min = 1, max = 2, resolution = 0.01 }\n"},

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

		// ---- wide text (SPEC §4.12, §4.6): every refusal the construct owes.
		// The type itself compiles, examples-wide/ being the corpus that pins
		// it, so what a case here breaks is one rule and never the construct.
		{name: "wstring below two", want: "wstring(1): N below 2",
			src: "package t\ntype T { s wstring(1) }\n"},
		{name: "wstring bound above int32", want: "lengths live in int32",
			src: "package t\ntype T { s wstring(5000000000) }\n"},
		{name: "wstring takes no default", want: "defaults cover bool, integer, float, enum, string, bytes and flags fields",
			src: "package t\ntype T { s wstring(4) = 1 }\n"},
		{name: "wstring takes no min/max", want: "min/max apply to integer fields",
			src: "package t\ntype T { s wstring(4) | min = 0, max = 3 }\n"},
		{name: "an array of wstring is not in v1", want: "an array of wstring(N) is not supported",
			src: "package t\ntype T { a [4]wstring(4) }\n"},
		{name: "wstring collides with its sibling's length companion", want: "length companion",
			src: "package t\ntype T {\n    text wstring(8)\n    text_length uint8\n}\n"},
		// the TABLE half of the row is schema#522 and has landed nowhere, so a
		// table closure refuses wide text BY NAME rather than emitting
		// something else (docs/SPEC-TABLES.md §11)
		{name: "wstring inside a table closure", want: "not carried on the TABLE wire yet",
			src: "package t\ntable T { s wstring(4) }\n"},
		{name: "an optional wstring inside a table closure", want: "not carried on the TABLE wire yet",
			src: "package t\ntable T { s ?wstring(4) }\n"},
		{name: "unbounded wide text", want: "*wstring is specified ahead of its implementation",
			src: "package t\ntype T { s *wstring }\n"},
		// memcmp over UTF-8 is a portable order and little-endian code units
		// have none, so the key diagnostic names string(N) (§2.8, §11)
		{name: "a wstring map key", want: "a wstring(N) key is refused",
			src: "package t\ntable T { m map[wstring(4)]uint8 }\n"},
		{name: "array bound zero", want: "below 1",
			src: "package t\ntype T { a [0]uint8 }\n"},
		{name: "count range backwards", want: "0 <= Min < N",
			src: "package t\ntype T { a [5..3]uint8 }\n"},
		// bounds are capped one at a time (SPEC §4.3) and their PRODUCT is
		// capped too (SPEC §4.6, docs/SPEC-TABLES.md §11): three nested arrays
		// of legal bounds are a wire width no arithmetic represents
		{name: "nested array bounds whose product passes the cap", want: "past the cap of 8796093022208 bits",
			src: "package t\ntype L { v uint64 }\ntype I { a [2147483647]L }\ntype M { a [2147483647]I }\n"},
		// ---- the qualification section (SPEC §4.2) ----
		{name: "the retired field attribute block names its replacement", want: "qualifiers follow | to the end of the line",
			src: "package t\ntype T { h int16 " + "[min = 0, max = 5] }\n"},
		{name: "the retired declaration attribute block names its replacement", want: "the body opens on the next line",
			src: "package t\nenum E " + "[max = 5] { A }\n"},
		{name: "empty qualification section", want: "empty qualification section",
			src: "package t\ntype T { h int16 | }\n"},
		{name: "a constant takes no qualification", want: "| is never an operator",
			src: "package t\nconst X = 1 | 2\n"},
		{name: "a union declaration takes no qualification", want: "union declaration takes no qualification",
			src: "package t\nunion U | x { }\n"},
		{name: "a block comment cannot sit right of |", want: "cannot sit right of |",
			src: "package t\ntype T { h int16 | min = 0, /* c */ max = 5 }\n"},
		{name: "the retired <= bound names its replacement", want: "spell it [..N], the range literal",
			src: "package t\ntype T { a [" + "<= 4]uint8 }\n"}, // spliced so the migration sweep never respells this fixture
		{name: "bound above int32", want: "counts live in int32",
			src: "package t\ntype T { a [..5000000000]uint8 }\n"},
		{name: "string bound above int32", want: "lengths live in int32",
			src: "package t\ntype T { s string(5000000000) }\n"},
		{name: "division by zero in a constant", want: "division by zero",
			src: "package t\nconst Z = 0\nconst A = 5 / Z\n"},

		// ---- fixed point and the 128-bit family (SPEC §4.3, §4.6) ----
		{name: "fixed I+F is not a storage width", want: "must equal a storage width",
			src: "package t\ntype T { x fixed(16, 15) | min = 0, max = 1 }\n"},
		{name: "fixed with zero integer bits", want: "at least one integer bit",
			src: "package t\ntype T { x fixed(0, 32) | min = 0, max = 1 }\n"},
		{name: "fixed with negative fractional bits", want: "cannot be negative",
			src: "package t\ntype T { x fixed(24, -8) | min = 0, max = 1 }\n"},
		{name: "fixed without bounds", want: "requires | min = A, max = B",
			src: "package t\ntype T { x fixed(48, 16) }\n"},
		{name: "fixed bounds outside the Q format", want: "do not fit fixed(8, 8)",
			src: "package t\ntype T { x fixed(8, 8) | min = -300, max = 300 }\n"},
		{name: "fixed with resolution", want: "resolution applies to float",
			src: "package t\ntype T { x fixed(16, 16) | min = 0, max = 1, resolution = 0.1 }\n"},
		// fixed defaults are LEGAL (whole units, exact) — the
		// good corner below pins the accepted form; what stays
		// illegal is inexactness and range violation (cases at the bottom).
		// ---- ufixed(I, F): the unsigned sibling. Same shape rules, unsigned domain, and
		// the diagnostics name the ufixed spelling. ----
		{name: "ufixed bounds below zero", want: "do not fit ufixed(16, 16)",
			src: "package t\ntype T { x ufixed(16, 16) | min = -1, max = 5 }\n"},
		{name: "ufixed with zero integer bits", want: "at least one integer bit",
			src: "package t\ntype T { x ufixed(0, 32) | min = 0, max = 1 }\n"},
		{name: "ufixed I+F is not a storage width", want: "must equal a storage width",
			src: "package t\ntype T { x ufixed(16, 15) | min = 0, max = 1 }\n"},
		{name: "ufixed without bounds", want: "ufixed(48, 16) requires | min = A, max = B",
			src: "package t\ntype T { x ufixed(48, 16) }\n"},
		{name: "ufixed bounds above the unsigned domain", want: "do not fit ufixed(8, 8)",
			src: "package t\ntype T { x ufixed(8, 8) | min = 0, max = 300 }\n"},
		{name: "ufixed(64, 0) bounds clamp at int64's ceiling", want: "do not fit ufixed(64, 0)",
			src: "package t\ntype T { x ufixed(64, 0) | min = 0, max = 18446744073709551615 }\n"},
		{name: "ufixed default below its unsigned range", want: "outside its range",
			src: "package t\ntype T { x ufixed(16, 16) = -1.0 | min = 0, max = 5 }\n"},
		{name: "a table declaration takes tags and was and no other valued key", want: "is not an attribute a table declaration takes",
			src: "package t\ntable T | pinned, min = 3\n{\n    x int32\n}\n"},
		{name: "message is not part of the language", want: "messages are not part of the language",
			src: "package t\nmessage M { x uint8 }\n"},
		{name: "object is not part of the language", want: "objects are not part of the language",
			src: "package t\nobject O { x uint8 }\n"},
		{name: "contexts are not part of the language", want: "contexts are not part of the language",
			src: "package t\ncontexts { client, server }\n"},
		{name: "round is not part of the language", want: "round is not part of the language",
			src: "package t\ntype T { x float32 | min = 0, max = 1, resolution = 0.1, round = up }\n"},
		{name: "bare round is refused by name too", want: "round is not part of the language",
			src: "package t\ntype T { x float32 | min = 0, max = 1, resolution = 0.1, round }\n"},
		{name: "a UTF-8 BOM is refused", want: "UTF-8 BOM rejected",
			src: "\xEF\xBB\xBFpackage t\n"},
		{name: "ufixed with resolution", want: "resolution applies to float",
			src: "package t\ntype T { x ufixed(16, 16) | min = 0, max = 1, resolution = 0.1 }\n"},

		{name: "bare int128", want: "int128 requires",
			src: "package t\ntype T { x int128 }\n"},
		{name: "uint128 with a range", want: "not valid on uint128",
			src: "package t\ntype T { x uint128 | min = 0, max = 5 }\n"},
		{name: "int128 range above its storage", want: "does not fit its declared storage int128",
			src: "package t\ntype T { x int128 | min = 0, max = 340282366920938463463374607431768211456 }\n"},

		// ---- enums, flags, contexts ----
		{name: "variant named None", want: "None",
			src: "package t\nenum E { None, A }\n"},
		{name: "variant named Max", want: "carries its extent as the member Max",
			src: "package t\nenum E { A, Max }\n"},
		{name: "a decl collides with an enum's generated Max extent (Go)", want: "generated Max extent",
			src: "package t\nenum Team { Red }\nconst TeamMax = 3\n"},
		{name: "enum max below variant count", want: "below its variant count",
			src: "package t\nenum E | max = 2\n{ A, B, C }\n"},
		{name: "Max reference to a non-enum", want: "is not an enum",
			src: "package t\ntype V { x uint8 }\nconst N = V.Max + 1\n"},
		{name: "Max reference to a flags declaration", want: "has no .Max",
			src: "package t\nflags F { A, B }\nconst N = F.Max\n"},
		{name: "Count reference to a non-enum, non-flags declaration", want: "neither an enum nor a flags declaration",
			src: "package t\ntype V { x uint8 }\nconst N = V.Count\n"},
		{name: "Count reference undefined", want: "undefined enum or flags",
			src: "package t\nconst N = F.Count\n"},
		{name: "variant named Count", want: "carries its declared variant count as the member Count",
			src: "package t\nenum E { A, Count }\n"},
		{name: "a decl collides with an enum's generated Count constant (Go)", want: "generated Count constant",
			src: "package t\nenum Weapon { Laser }\nconst WeaponCount = 3\n"},
		{name: "65 flags variants overflow uint64 storage", want: "one bit per variant, up to 64",
			src: "package t\nflags F { " + flags65 + " }\n"},
		// ---- unions (SPEC §4.8) ----
		{name: "union variant named none (exported spelling)", want: "every union has None = 0 implicitly",
			src: "package t\nunion U {\n    none Box\n}\ntype Box { x uint8 }\n"},
		{name: "union variant named max (exported spelling)", want: "carries its extent as the member Max",
			src: "package t\nunion U {\n    max_ Box\n}\ntype Box { x uint8 }\n"},
		{name: "union variant exporting Type", want: "the tag member's own name",
			src: "package t\nunion U {\n    type_ Box\n}\ntype Box { x uint8 }\n"},
		{name: "union variant exporting the union's name", want: "C# refuses (CS0542)",
			src: "package t\nunion U {\n    u Box\n}\ntype Box { x uint8 }\n"},
		{name: "union variants colliding after export mapping", want: "both become BoxA",
			src: "package t\nunion U {\n    box_a Box\n    boxA Box\n}\ntype Box { x uint8 }\n"},
		// AN ARM IS A FIELD LINE (docs/SPEC-TABLES.md §2.6): an enum, a
		// scalar, a string, an array, a pointer and a union are arms, and
		// what refuses them OUTSIDE a table closure is the closure rule —
		// such a union has no packet wire yet (§11, §15)
		{name: "an enum arm outside a table closure", want: "no table reaches U",
			src: "package t\nunion U {\n    e E\n}\nenum E { A }\n"},
		{name: "a union arm outside a table closure", want: "no table reaches U",
			src: "package t\nunion U {\n    v V\n}\nunion V { }\n"},
		{name: "a scalar arm outside a table closure", want: "no table reaches U",
			src: "package t\nunion U {\n    count int32\n}\n"},
		{name: "a table-closure union held in a type body", want: "a union in a `type` body takes `type` payloads only",
			src: "package t\nunion U {\n    count int32\n}\ntype Holder { u U }\ntable Root { h Holder }\n"},
		// what an ARM may not carry, each refused at the arm (§2.6, §11)
		{name: "a default on an arm", want: "ZERO-ESTABLISHES at selection",
			src: "package t\nunion U {\n    count int32 = 3\n}\ntable Root { u U }\n"},
		{name: "an optional arm", want: "SELECTION IS THE ARM'S PRESENCE",
			src: "package t\nunion U {\n    count ?int32\n}\ntable Root { u U }\n"},
		{name: "was on an arm", want: "was on an arm is a named follow-on",
			src: "package t\nunion U {\n    count int32 | was = \"tally\"\n}\ntable Root { u U }\n"},
		{name: "json on an arm", want: "json on an arm is a named follow-on",
			src: "package t\nunion U {\n    count int32 | json = \"n\"\n}\ntable Root { u U }\n"},
		{name: "an enum-keyed array arm", want: "an enum-keyed array is not an arm",
			src: "package t\nenum E { A, B }\nunion U {\n    slots [E]int32\n}\ntable Root { u U }\n"},
		{name: "an arm whose range excludes zero", want: "the value it establishes at selection is outside it",
			src: "package t\nunion U {\n    count int32 | min = 1, max = 10\n}\ntable Root { u U }\n"},
		{name: "union payload undefined", want: "undefined type",
			src: "package t\nunion U {\n    b Box\n}\n"},
		{name: "union composition cycle", want: "type composition cycle",
			src: "package t\nunion U {\n    b Box\n}\ntype Box { u U }\n"},

		{name: "a decl colliding with a union's generated tag enum", want: "generated tag enum",
			src: "package t\nunion U { }\ntype UType { x uint8 }\n"},

		// ---- objects ----
		// an unbounded fixed component is already illegal by §4.3 (bounds are
		// part of the wire format) — the narrowing rule never sees one alone;
		// this documents the error the author gets, and that the composite
		// rule survives the multi-error pass beside it

		// ---- namespace, names, claims ----
		// valued attributes written bare. FOUND BY THE FUZZER (internal/fuzz)
		// in 3 seconds: ` | min, max` reached expression evaluation as a nil
		// and panicked the compiler with a stack trace instead of pointing at
		// the typo. One case per valued attribute — the guard is a table, and
		// a table with an untested entry is a table that will lose one.
		{name: "min written without a value", want: "requires a value",
			src: "package t\ntype T { n int32 | min, max = 10 }\n"},
		{name: "max written without a value", want: "requires a value",
			src: "package t\ntype T { n int32 | min = 0, max }\n"},
		{name: "resolution written without a value", want: "requires a value",
			src: "package t\ntype T { x float32 | min = 0, max = 1, resolution }\n"},
		{name: "was written without a value", want: "requires a value",
			src: "package t\ntable T { x int32 | was }\n"},

		// cycle guards: every resolver that can re-enter itself must REJECT,
		// not recurse. Before the enum guard these crashed the compiler with
		// a raw "fatal error: stack overflow" — no diagnostic, no position.
		{name: "enum | max = E.Max self-cycle", want: "reference cycle",
			src: "package t\nenum Alpha | max = Alpha.Max\n{ One }\n"},
		{name: "enum | max = E.Max mutual cycle across files", want: "reference cycle",
			srcs: map[string]string{
				"A_e.schema": "package t\nenum Alpha | max = Zed.Max\n{ One, Two }\n",
				"Z_e.schema": "package t\nenum Zed | max = Alpha.Max\n{ Three }\n"},
		},
		{name: "enum | max = E.Max three-enum cycle", want: "reference cycle",
			src: "package t\nenum A | max = B.Max\n{ One }\nenum B | max = C.Max\n{ Two }\nenum C | max = A.Max\n{ Three }\n"},
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
		{name: "a decl collides with an enum's flat variant constant (Go)", want: "variant constant",
			src: "package t\nenum Team { Red }\ntype TeamRed { x uint8 }\n"},
		{name: "a decl collides with a flags mask constant", want: "mask constant",
			src: "package t\nflags Caps { Fast }\nconst CapsFast = 1\n"},
		{name: "a decl collides with a flags name function", want: "name functions",
			src: "package t\nflags Caps { Fast }\ntype FlagNamesCaps { x uint8 }\n"},
		{name: "two flags whose Rust name-function spellings collide", want: "both generate the symbol flag_name_ab_caps",
			src: "package t\nflags ABCaps { Fast }\nflags AbCaps { Slow }\ntype T { a ABCaps\n b AbCaps }\n"},
		{name: "two decls whose GENERATED symbols collide", want: "both generate the symbol WriteFooMaxBits",
			src: "package t\ntype WriteFoo { x uint8 }\ntype FooMaxBits { y uint8 }\n"},
		{name: "a decl shadows a name generated Rust references unqualified", want: "references unqualified",
			src: "package t\ntype Default { x uint8 }\n"},
		{name: "two enum variants collide as Rust associated constants", want: "associated constant FOO_BAR",
			src: "package t\nenum E { FOOBar, FooBar }\ntype T { e E }\n"},
		{name: "field collides with its sibling's length companion", want: "length companion",
			src: "package t\ntype T {\n    text string(8)\n    text_length uint8\n}\n"},
		{name: "field collides with its sibling's count companion", want: "count companion",
			src: "package t\ntype T {\n    items [..4]uint8\n    items_count uint8\n}\n"},
		{name: "field export equals its declaring type's name (C# CS0542)", want: "enclosing type",
			src: "package t\ntype Timescale { timescale float64 }\n"},
		{name: "float triple degenerate at float32", want: "degenerate at float32",
			src: "package t\ntype T { x float32 | min = 1.0, max = 1.00000001, resolution = 0.000000001 }\n"},
		{name: "resolution collapses to zero at float32", want: "collapses to zero at float32",
			src: "package t\ntype T { x float32 | min = 0.0, max = 1.0, resolution = 1e-46 }\n"},

		// ---- package names that cannot compile (the compile fuzzer's
		// `package exit` specimen). Without the rule, the
		// checker said `ok: package exit` and clang rejected the generated
		// `namespace exit` against <cstdlib> with "redefinition of 'exit' as
		// different kind of symbol"; `package for` checked clean and generated
		// keyword namespaces; `package main` generated Go that fails with
		// "function main is undeclared in the main package". ----
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

		// ---- non-finite compressed-float parameters ("attempting to send NaN or INF or anything else through compressed
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
			src: "package t\ntype T { x float32 | min = -1e39, max = 1e39, resolution = 1e30 }\n"},
		{name: "compressed float min +Inf at float32", want: "min = 1e+39 overflows float32",
			src: "package t\ntype T { x float32 | min = 1e39, max = 2e39, resolution = 1e30 }\n"},
		{name: "compressed float max +Inf at float32", want: "max = 1e+39 overflows float32",
			src: "package t\ntype T { x float32 | min = 0.0, max = 1e39, resolution = 1e30 }\n"},
		// (a -Inf max cannot be its own diagnostic: min < max puts min below
		// it, so the min arm always fires first — the parameter is still
		// covered, by ordering rather than by a separate message)
		{name: "compressed float resolution +Inf at float32", want: "resolution = 1e+39 overflows float32",
			src: "package t\ntype T { x float32 | min = 0.0, max = 1.0, resolution = 1e39 }\n"},
		{name: "compressed float min +Inf at float64, integer literal", want: "does not fit float64",
			src: "package t\ntype T { x float32 | min = 1" + strings.Repeat("0", 400) + ", max = 1.0, resolution = 0.1 }\n"},
		{name: "compressed float min -Inf at float64, negated integer literal", want: "does not fit float64",
			src: "package t\ntype T { x float32 | min = -1" + strings.Repeat("0", 400) + ", max = 1.0, resolution = 0.1 }\n"},
		// The through-a-const vehicle is refused EARLIER since the extremes guard
		// landed: an implicitly-typed const past int64 fails at its own
		// declaration, so the float64-finiteness arm can no longer be reached
		// through a const — the schema is refused either way, and the float64
		// arm keeps its coverage via the integer-literal cases beside this one.
		{name: "compressed float max +Inf at float64, through a const", want: "does not fit int64, the default constant storage",
			src: "package t\nconst Big = 1" + strings.Repeat("0", 400) + "\ntype T { x float32 | min = 0.0, max = Big, resolution = 0.1 }\n"},
		{name: "compressed float resolution +Inf at float64, integer literal", want: "does not fit float64",
			src: "package t\ntype T { x float32 | min = 0.0, max = 1.0, resolution = 1" + strings.Repeat("0", 400) + " }\n"},
		{name: "enum max above the 32-bit tag wire", want: "32-bit tag wire",
			src: "package t\nenum E | max = 2147483648\n{ A }\ntype T { e E }\n"},
		{name: "a decl named Schema collides with the C# Schema class", want: "C# Schema class",
			src: "package t\ntype Schema { x uint8 }\n"},
		{name: "a decl collides with a type's generated Zero helper (C#)", want: "both generate the symbol ZeroChat",
			src: "package t\ntype Chat { }\ntype ZeroChat { x uint8 }\n"},
		{name: "two types whose Rust snake spellings collide", want: "both generate the symbol write_ab_test",
			src: "package t\ntype ABTest { x uint8 }\ntype AbTest { y uint8 }\n"},
		{name: "two consts whose Rust constant spellings collide", want: "both generate the symbol AB_MAX",
			src: "package t\nconst ABMax = 1\nconst AbMax = 2\n"},
		{name: "a const collides with an enum's C variant #define", want: "variant constant Ludicrous (C form)",
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
			src: "package t\ntype T { h int16 = 99 | min = 0, max = 10 }\n"},
		{name: "default on an array", want: "array takes no specified default",
			src: "package t\ntype T { a [4]uint8 = 1 }\n"},
		{name: "enum default is not a variant", want: "names a variant",
			src: "package t\nenum E { A }\ntype T { e E = Zzz }\n"},
		{name: "bool default is not a bool", want: "true or false",
			src: "package t\ntype T { b bool = 3 }\n"},

		// ---- native type mapping (SPEC §4.2) ----
		{name: "cpp_native without cpp_include", want: "cpp_native and cpp_include go together",
			src: "package t\ntype V | cpp_native = VMath { x float64 }\n"},
		{name: "cpp_include without cpp_native", want: "cpp_native and cpp_include go together",
			src: "package t\ntype V | cpp_include = \"v.h\" { x float64 }\n"},
		{name: "cpp_native takes an identifier", want: "cpp_native takes an identifier",
			src: "package t\ntype V | cpp_native = \"VMath\", cpp_include = \"v.h\" { x float64 }\n"},
		{name: "cpp_include takes a string", want: "cpp_include takes a quoted header path",
			src: "package t\ntype V | cpp_native = VMath, cpp_include = vh { x float64 }\n"},
		{name: "a valued type attr that is not a binding is still rejected", want: "bare identifier",
			src: "package t\ntype V | vec3 = 4 { x float64 }\n"},
		// ---- §11's claimed runtime names, including the GO port's LOWERCASE
		// ---- family. Unexported is not private: a Go package is one namespace,
		// ---- so `const tableJsonMaxDepth = 5` beside a table generates a
		// ---- redeclaration and the unit does not compile. This is the blind
		// ---- reader's own repro on #338. ----
		{name: "a const spelling the Go text walk's depth cap, beside a table",
			src:  "package probe\n\nconst tableJsonMaxDepth = 5\n\ntable Thing\n{\n    n int32\n}\n",
			want: "tableJsonMaxDepth"},
		{name: "a type spelling the Go text walk's reader cursor, beside a table",
			src:  "package probe\n\ntype tableJsonIn { n int32 }\n\ntable Thing\n{\n    n int32\n}\n",
			want: "unexported names at package scope"},
		{name: "a table spelling the Go cook's descriptor graph",
			src:  "package probe\n\ntable tableCookRecords\n{\n    n int32\n}\n",
			want: "tableCookRecords"},

		// ---- the C target's PREPROCESSOR namespace (SPEC §6.1's C column) ----
		//
		// A schema's constants, enum variants and flag masks are #defines in the
		// C target, and the generated sources define macros of their own beside
		// them. A collision is a SILENT REWRITE, not a redeclaration error: the
		// generator's #ifndef sees the user's definition standing and skips its
		// own, so every later use expands to something else and nothing in the
		// build says so. These are the shapes that reach one.
		{name: "an enum variant folding to the packet emitter's own macro", want: "SILENT REWRITE",
			src: "package t\nenum Schema { Unused }\ntype H { g Schema }\n"},
		{name: "an enum variant folding to the table backend's alignof macro", want: "SILENT REWRITE",
			src: "package t\nenum SchemaTable { Alignof }\ntype H { g SchemaTable }\n"},
		{name: "a constant folding to the table backend's force-inline macro", want: "SILENT REWRITE",
			src: "package t\nconst SchemaTTableInline = 1\ntype H { x uint8 }\n"},
		{name: "a declaration spelling a generated include guard", want: "SILENT REWRITE",
			srcs: map[string]string{"T.schema": "package t\ntype SCHEMA_T_T_H { x uint8 }\n"}},
		{name: "a declaration carrying the reserved lowercase prefix", want: "reserves for the names the",
			src: "package t\ntype schema_thing { x uint8 }\n"},
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
		{name: "the Go runtime's lowercase names in a TABLE-FREE unit (the negative control)",
			src: "package t\nconst tableJsonMaxDepth = 5\ntype tableJsonIn { n int32 }\ntype tableCookRecords { n int32 }\n"},
		{name: "nested if with cond in the same branch",
			src: "package t\ntype T {\n    a bool\n    if a {\n        b bool\n        if b { x uint8 }\n    }\n}\n"},
		{name: "fixed default in whole units, exactly representable (the reopened door)",
			src: "package t\ntype Q { w fixed(2, 30) = 1.0 | min = -1, max = 1\n x fixed(16, 16) = 0.5 | min = 0, max = 100\n y fixed(48, 16) = 3 | min = -10, max = 10 }\n"},
		{name: "field named flags at block scope",
			src: "package t\nflags F { A }\ntype T { flags F }\n"},
		{name: "64 flags variants fill uint64 storage exactly",
			src: "package t\nflags F { " + strings.Replace(flags65, ", V64", "", 1) + " }\n"},
		{name: "F.Count in a constant expression",
			src: "package t\nflags F { A, B }\nconst N = F.Count\n"},
		{name: "E.Count in a constant expression, headroom included",
			src: "package t\nenum E | max = 15\n{ A, B }\nconst N = E.Count\n"},
		{name: "E.Count as an array bound",
			src: "package t\nenum E | max = 15\n{ A, B }\ntype T { xs [..E.Count]uint8 }\n"},
		{name: "a // comment terminates the qualification section",
			src: "package t\ntype T { h int16 | min = 0, max = 5 // the section ends here\n}\n"},
		{name: "a qualified declaration's body opens on the next line",
			src: "package t\nenum E | max = 5\n{ A, B }\ntype Q | tagged\n{\n    e E\n}\n"},
		{name: "both brace placements parse (Allman canonical, same-line tolerated)",
			src: "package t\ntype A {\n    x uint8\n}\ntype B\n{\n    a bool\n    if a {\n        y uint8\n    }\n    if !a\n    {\n        z uint8\n    }\n}\n"},
		{name: "empty type",
			src: "package t\ntype T { }\n"},
		{name: "full-range uint64",
			src: "package t\ntype T { n uint64 | min = 0, max = 18446744073709551615 }\n"},
		{name: "fixed at every storage width, F = 0, and the sign-bit-only corner",
			src: "package t\ntype T {\n    a fixed(8, 8) | min = -100, max = 100\n    b fixed(16, 16) | min = -180, max = 180\n    c fixed(32, 0) | min = 0, max = 1000000\n    d fixed(48, 16) | min = -30000, max = 30000\n    e fixed(112, 16) | min = -1000000, max = 1000000\n    f fixed(1, 63) | min = -1, max = 0\n}\n"},
		{name: "ufixed at every storage width, the full unsigned domains, and the one-bit corner",
			src: "package t\ntype T {\n    a ufixed(8, 8) | min = 0, max = 255\n    b ufixed(16, 16) | min = 0, max = 360\n    c ufixed(32, 0) | min = 0, max = 4294967295\n    d ufixed(48, 16) | min = 0, max = 281474976710655\n    e ufixed(112, 16) | min = 0, max = 2000000\n    f ufixed(1, 63) | min = 0, max = 1\n    g ufixed(16, 16) = 3 | min = 3, max = 3\n}\n"},
		{name: "ufixed default in whole units, exactly representable",
			src: "package t\ntype T { x ufixed(2, 30) = 1.0 | min = 0, max = 1\n y ufixed(16, 16) = 0.5 | min = 0, max = 100 }\n"},
		{name: "a ufixed composite as an ordinary field type",
			src: "package t\ntype V { x ufixed(48, 16) | min = 0, max = 100\n y ufixed(48, 16) | min = 0, max = 100 }\ntype T { p V\n b bool }\n"},
		{name: "int128 with a range only 128 bits can hold, and raw uint128",
			src: "package t\ntype T {\n    flux int128 | min = -1267650600228229401496703205376, max = 1267650600228229401496703205376\n    id   uint128\n}\n"},
		// issue #346's legal side: a range that REACHES zero at either end
		// needs nothing, and a range that excludes zero is legal the moment a
		// default inside it is declared — on every range-carrying kind
		{name: "a range touching zero at either end takes no default",
			src: "package t\ntype T {\n    a uint8 | min = 0, max = 10\n    b int8  | min = -10, max = 0\n    c int8  | min = -10, max = 10\n    d int8  | min = 0, max = 0\n}\n"},
		{name: "a range that excludes zero, with a default in range",
			src: "package t\ntype T {\n    a uint8   = 1  | min = 1, max = 255\n    b int8    = -7 | min = -100, max = -1\n    c int32   = 7  | min = 7, max = 7\n    d fixed(16, 16) = 3 | min = 3, max = 8\n    e ufixed(16, 16) = 3 | min = 3, max = 3\n    f int128  = 1  | min = 1, max = 1267650600228229401496703205376\n    v float32 = 1.5 | min = 1, max = 2, resolution = 0.01\n}\n"},
		{name: "a table field's excluding range, with a default in range",
			src: "package t\ntable T { hp uint8 = 100 | min = 1, max = 100 }\n"},
		{name: "an array whose range reaches zero stays legal",
			src: "package t\ntype T { xs [4]uint8 | min = 0, max = 255 }\n"},
		{name: "128-bit defaults inside their range",
			src: "package t\ntype T {\n    a int128 = -5 | min = -10, max = 10\n    b uint128 = 7\n}\n"},
		{name: "headroom enum with non-variant wire values",
			src: "package t\nenum E | max = 15\n{ A, B }\ntype T { e E }\n"},
		{name: "field named message_type on a plain type",
			src: "package t\ntype T { message_type uint8 }\n"},
		{name: "a type named WriteFoo with no type Foo anywhere",
			src: "package t\ntype WriteFoo { x uint8 }\n"},
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
// §4.6). The grammar cannot spell NaN — a float literal
// beyond the double's range is a parse error, every constant arithmetic
// result is overflow-guarded, and division by zero is refused — so the arm is
// exercised the only way a NaN can exist in the checker's input: planted
// directly in the AST. Defense in depth, proven rather than assumed.
func TestNonFiniteTripleNaN(t *testing.T) {
	src := "package t\ntype T { x float32 | min = 0.5, max = 1.0, resolution = 0.25 }\n"
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

// THE NEGATIVE CONTROL for the C preprocessor reservation: a refusal that
// cannot be shown to LEAVE NEIGHBOURS ALONE is a refusal nobody can size.
//
// The set is enumerated rather than a bare prefix test, and this is what that
// buys: `enum SchemaKind` folds to SCHEMA_KIND_ALPHA, `const SchemaVersion` to
// SCHEMA_VERSION, and neither is a macro the generated C defines — so both stay
// legal. A prefix test would have taken all of them, which is a claim nothing
// needs taking a name away from every schema for free.
func TestCReservedMacroNeighborsAccepted(t *testing.T) {
	for _, src := range []string{
		"package t\nenum SchemaKind { Alpha, Beta }\ntype H { g SchemaKind }\n",
		"package t\nconst SchemaVersion = 3\ntype H { x uint8 }\n",
		"package t\ntype SchemaPoint { x float32 }\n",
		"package t\nflags SchemaPerks { Fast, Slow }\ntype H { p SchemaPerks }\n",
		// and the one that only LOOKS like the lowercase reservation: the
		// prefix is `schema_`, so a name that merely starts with `schema` and
		// carries no underscore is untouched
		"package t\ntype schematic { x uint8 }\n",
	} {
		if errs := runUnit(t, map[string]string{"a.schema": src}); len(errs) > 0 {
			t.Errorf("a declaration that collides with no generated macro must stay legal: %v\n%s", errs, src)
		}
	}
}

// ONE MISTAKE, ONE DIAGNOSTIC (#447 F-01, #521 G-02). A variant that IS one of
// the three names an enum generates for itself used to draw four errors: the
// rule, then the same fact restated as a Go-symbol collision, a Rust
// associated-const collision and a C #define collision. The actionable line
// was one of four, and it was a beginner's first enum mistake.
//
// Reverting reservedEnumVariant's two uses in checkClaimedNames turns this red
// at four errors for None and Count and four for Max.
func TestReservedEnumVariantDrawsOneDiagnostic(t *testing.T) {
	for _, reserved := range []string{"None", "Max", "Count"} {
		errs := runUnit(t, map[string]string{
			"Bad.schema": "package t\nenum E { " + reserved + ", Fighter }\n",
		})
		if len(errs) != 1 {
			t.Errorf("variant %s: want 1 diagnostic, got %d:", reserved, len(errs))
			for _, e := range errs {
				t.Errorf("  %v", e)
			}
			continue
		}
		if !strings.Contains(errs[0].Error(), "is a compile error") {
			t.Errorf("variant %s: the one diagnostic is not the reserved-word rule: %v", reserved, errs[0])
		}
	}
}

// The collision checks stay live for a name that merely COLLIDES with a
// generated one. Nothing else explains those, so their per-target wording is
// the whole answer — and each side of the C-form collision now names itself,
// where one shared label used to print "enum E's generated variant constants
// (C form) collides with enum E's generated variant constants (C form)".
func TestCollidingEnumVariantKeepsItsPerTargetDiagnostics(t *testing.T) {
	errs := runUnit(t, map[string]string{
		"Bad.schema": "package t\nenum E { none, Fighter }\n",
	})
	if len(errs) != 2 {
		t.Fatalf("want the Rust and C collisions, got %d: %v", len(errs), errs)
	}
	var cForm string
	for _, e := range errs {
		if strings.Contains(e.Error(), "(C form)") {
			cForm = e.Error()
		}
	}
	if cForm == "" {
		t.Fatalf("no C-form collision among %v", errs)
	}
	if !strings.Contains(cForm, "generated variant constant none (C form) collides with enum E's generated None constant (C form)") {
		t.Errorf("the C-form collision does not name both sides: %s", cForm)
	}
}

// `?` ON A UNION IS ONE MISTAKE (#521 G-09). Marking a union field optional
// used to draw a second error saying no table reaches the union and telling
// the author to "hold the union in a table body" — which is what the very
// line the first error is about already does. The `?` refusal dropped the
// field, and the reachability walk read the surviving fields alone.
//
// Reverting the unionNamedBy record turns this red at two diagnostics.
func TestOptionalUnionDrawsOneDiagnostic(t *testing.T) {
	errs := runUnit(t, map[string]string{
		"Bad.schema": "package t\ntable A { x int32 }\ntable B { y int32 }\nunion U { a A\n b B }\ntable T { u ?U }\n",
	})
	if len(errs) != 1 {
		t.Errorf("want 1 diagnostic, got %d:", len(errs))
		for _, e := range errs {
			t.Errorf("  %v", e)
		}
		return
	}
	if !strings.Contains(errs[0].Error(), "marks a union optional") {
		t.Errorf("the one diagnostic is not the `?` refusal: %v", errs[0])
	}
}

// The reachability refusal itself stays live where it is TRUE: a union with a
// table arm that no closure names at all, and one held by a `type` body.
func TestUnreachedTableArmUnionIsStillRefused(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"named by nothing", "package t\ntable A { x int32 }\ntable B { y int32 }\nunion U { a A\n b B }\ntable C { z int32 }\n"},
		{"held by a type body", "package t\ntable A { x int32 }\ntable B { y int32 }\nunion U { a A\n b B }\ntype T { u U }\n"},
	} {
		errs := runUnit(t, map[string]string{"Bad.schema": tc.src})
		var found bool
		for _, e := range errs {
			if strings.Contains(e.Error(), "no table reaches U") {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: the reachability refusal went missing: %v", tc.name, errs)
		}
	}
}

// THE FRAMING THE MESSAGES NAME (#521 G-17). The three packet-wire constructs
// a table body refuses are refused for the right reason — a table's wire has no
// bit positions — but the messages described the wire as "field-tagged TLV",
// the framing #507 replaced. A field is now `id reference, kind, payload`
// against a trailing id table, and the tag is a position into that table.
//
// Reverting the wording turns this red on all three.
func TestTableRefusalsNameTheIdTableWire(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"const(value, bits)", "package t\ntable T { const(0xC7, 8)\n x int32 }\n"},
		{"reserved(bits)", "package t\ntable T { x int32\n reserved(4)\n y int32 }\n"},
		{"align", "package t\ntable T { x int32\n align\n y int32 }\n"},
	} {
		errs := runUnit(t, map[string]string{"Bad.schema": tc.src})
		if len(errs) == 0 {
			t.Errorf("%s: the packet-wire construct was accepted in a table body", tc.name)
			continue
		}
		got := errs[0].Error()
		if strings.Contains(got, "field-tagged TLV") {
			t.Errorf("%s: the refusal still names the framing #507 replaced: %s", tc.name, got)
		}
		for _, want := range []string{"`id reference, kind, payload`", "no bit positions", "docs/SPEC-TABLES.md §3"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: the refusal does not carry %q: %s", tc.name, want, got)
			}
		}
	}
}

// A DIAGNOSTIC ABOUT THE VALUE POINTS AT THE VALUE (#447 F-05). The
// float-expression refusals carried the DECLARATION's position, so the caret
// landed on column 1, while a range refusal on the same kind of line points at
// the offending value. Reverting valuePos turns this red at column 1.
func TestConstValueDiagnosticsPointAtTheExpression(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
		col             int
	}{
		{"integer type, float expression", "package t\nconst C int32 = 1.5\n", "but a float expression", 17},
		{"float32 overflow", "package t\nconst D float32 = 1.0e300\n", "does not fit its declared type float32", 19},
	} {
		errs := runUnit(t, map[string]string{"Bad.schema": tc.src})
		if len(errs) != 1 {
			t.Errorf("%s: want 1 diagnostic, got %v", tc.name, errs)
			continue
		}
		got := errs[0].Error()
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: unexpected diagnostic %s", tc.name, got)
			continue
		}
		if !strings.HasPrefix(got, fmt.Sprintf("Bad.schema:2:%d:", tc.col)) {
			t.Errorf("%s: the caret is not on the value (want column %d): %s", tc.name, tc.col, got)
		}
	}
}
