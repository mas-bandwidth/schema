package golang

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixir"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rust"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func countAcross(files map[string][]byte, needle string) int {
	n := 0
	for _, src := range files {
		n += strings.Count(string(src), needle)
	}
	return n
}

// collapseColumns squeezes each line's whitespace runs to one space, so a
// needle can name a whole emitted line — symbol, value and trailing comment
// — without pinning the column go/format aligned it into.
func collapseColumns(files map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(files))
	for name, src := range files {
		lines := strings.Split(string(src), "\n")
		for i, line := range lines {
			lines[i] = strings.Join(strings.Fields(line), " ")
		}
		out[name] = []byte(strings.Join(lines, "\n"))
	}
	return out
}

// A bare float const infers float64 (SPEC §4.2, Go's literal rule): the Go
// target must export it exactly as the explicit annotation would — a TYPED
// float64 constant, the same surface every other target already emits
// . An untyped Go constant is a different exported type: it
// converts where float64 does not, so consumer code written against one
// form breaks against the other.
func TestBareFloatConstExportsTypedFloat64(t *testing.T) {
	src := "package t\n\nconst Bare      = 1.5\nconst Annotated float64 = 1.5\n"
	f, perrs := parser.Parse("Consts.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Consts.schema", Name: "Consts.schema", Base: "Consts",
		Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"const Bare float64 = 1.5",
		"const Annotated float64 = 1.5",
	} {
		if got := countAcross(files, want); got != 1 {
			t.Errorf("Go: %q emitted %d times — a bare float const must export the same typed float64 the annotation spells", want, got)
		}
	}
}

// Every generated enum surface carries its extent as the member Max (SPEC
// §4.2 — the exported-extent rule): declared enums (headroom
// included) and the generated <Union>Type tag enum alike, in each target's
// own convention. Call sites then state ranges against E.Max's generated
// twin instead of a hand-declared count constant.
func TestEnumExtentEmitted(t *testing.T) {
	src := "package t\n\n" +
		"enum Weapon | max = 15\n{ Laser, Missile }\n\n" +
		"type Box { w Weapon }\n\n" +
		"type Ball { y uint8 }\n\n" +
		"union Shape {\n    box  Box\n    ball Ball\n}\n"
	f, perrs := parser.Parse("Extent.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Extent.schema", Name: "Extent.schema", Base: "Extent",
		Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}

	type expectation struct {
		needle string
		count  int
	}
	type target struct {
		name    string
		files   map[string][]byte
		genErr  error
		expects []expectation
	}
	var targets []target
	addTarget := func(name string, files map[string][]byte, err error, expects ...expectation) {
		targets = append(targets, target{name, files, err, expects})
	}

	goFiles, goErr := Generate(u)
	// go/format aligns const blocks, so the symbol and its value are asserted
	// as separate needles rather than one whitespace-sensitive line
	addTarget("Go", goFiles, goErr,
		expectation{"WeaponMax", 1},
		expectation{"ShapeTypeMax", 1},
		expectation{"= 15 // the exported extent (SPEC §4.2)", 1},
		expectation{"= 2 // the exported extent (SPEC §4.2)", 1})
	rustFiles, rustErr := rust.Generate(u)
	addTarget("Rust", rustFiles, rustErr,
		expectation{"pub const MAX: Weapon = Weapon(15);", 1},
		expectation{"pub const MAX: ShapeType = ShapeType(2);", 1})
	csFiles, csErr := csharp.Generate(u)
	addTarget("C#", csFiles, csErr,
		expectation{"Max = 15,", 1}, // Weapon
		expectation{"Max = 2,", 1})  // ShapeType
	jsFiles, jsErr := js.Generate(u)
	addTarget("JS", jsFiles, jsErr,
		expectation{"Max: 15,", 1},
		expectation{"Max: 2,", 1})
	cFiles, cErr := cgen.Generate(u)
	addTarget("C", cFiles, cErr,
		expectation{"#define WEAPON_MAX 15", 1},
		expectation{"#define SHAPE_TYPE_MAX 2", 1})
	cppFiles, cppErr := cpp.Generate(u)
	addTarget("C++", cppFiles, cppErr,
		expectation{"Max = 15,", 1},
		expectation{"Max = 2,", 1})

	for _, tgt := range targets {
		if tgt.genErr != nil {
			t.Errorf("%s: generate: %v", tgt.name, tgt.genErr)
			continue
		}
		for _, e := range tgt.expects {
			if got := countAcross(tgt.files, e.needle); got != e.count {
				t.Errorf("%s: %q emitted %d times, want %d — the enum extent must ride every generated enum surface", tgt.name, e.needle, got, e.count)
			}
		}
	}
}

// Every generated enum carries its DECLARED variant count as the member
// Count beside the extent Max (SPEC §4.2), in each target's own idiom and in
// all nine. Without headroom the two numbers coincide; under | max = K they
// part, and that is the case a loop written against Max alone gets wrong —
// so both enums are pinned here, and Count < Max is asserted on the widened
// one.
func TestEnumDeclaredCountEmitted(t *testing.T) {
	src := "package t\n\n" +
		"enum Weapon | max = 15\n{ Laser, Missile }\n\n" +
		"enum Team { Red, Blue, Green }\n\n" +
		"type Box {\n    w Weapon\n    t Team\n}\n"
	f, perrs := parser.Parse("Counts.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Counts.schema", Name: "Counts.schema", Base: "Counts",
		Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	if got, max := len(u.Enums["Weapon"].Variants), u.Enums["Weapon"].Max; int64(got) >= max {
		t.Fatalf("Weapon: Count = %d, Max = %d — the headroom case must have Count < Max", got, max)
	}

	type expectation struct {
		needle string
		count  int
	}
	type target struct {
		name    string
		files   map[string][]byte
		genErr  error
		expects []expectation
	}
	var targets []target
	addTarget := func(name string, files map[string][]byte, err error, expects ...expectation) {
		targets = append(targets, target{name, files, err, expects})
	}

	goFiles, goErr := Generate(u)
	// go/format aligns a const block into columns, so the Go needles are
	// matched against a space-collapsed copy: symbol, value and comment
	// together, without a column position baked into the test
	addTarget("Go", collapseColumns(goFiles), goErr,
		expectation{"WeaponCount Weapon = 2 // the declared variant count (SPEC §4.2)", 1},
		expectation{"TeamCount Team = 3 // the declared variant count (SPEC §4.2)", 1},
		expectation{"WeaponMax Weapon = 15 // the exported extent (SPEC §4.2)", 1})
	rustFiles, rustErr := rust.Generate(u)
	addTarget("Rust", rustFiles, rustErr,
		expectation{"pub const COUNT: Weapon = Weapon(2);", 1},
		expectation{"pub const COUNT: Team = Team(3);", 1},
		expectation{"pub const MAX: Weapon = Weapon(15);", 1})
	csFiles, csErr := csharp.Generate(u)
	addTarget("C#", csFiles, csErr,
		expectation{"Count = 2,", 1},
		expectation{"Count = 3,", 1},
		expectation{"Max = 15,", 1})
	jsFiles, jsErr := js.Generate(u)
	addTarget("JS", jsFiles, jsErr,
		expectation{"Count: 2,", 1},
		expectation{"Count: 3,", 1},
		expectation{"Max: 15,", 1})
	cFiles, cErr := cgen.Generate(u)
	addTarget("C", cFiles, cErr,
		expectation{"#define WEAPON_COUNT 2", 1},
		expectation{"#define TEAM_COUNT 3", 1},
		expectation{"#define WEAPON_MAX 15", 1})
	cppFiles, cppErr := cpp.Generate(u)
	addTarget("C++", cppFiles, cppErr,
		expectation{"Count = 2,", 1},
		expectation{"Count = 3,", 1},
		expectation{"Max = 15,", 1})
	dartFiles, dartErr := dart.Generate(u)
	addTarget("Dart", dartFiles, dartErr,
		expectation{"static const int count = 2;", 1},
		expectation{"static const int count = 3;", 1},
		expectation{"static const int max = 15;", 1})
	javaFiles, javaErr := java.Generate(u)
	addTarget("Java", javaFiles, javaErr,
		expectation{"public static final byte count = 2;", 1},
		expectation{"public static final byte count = 3;", 1},
		expectation{"public static final byte max = 15;", 1})
	elixirFiles, elixirErr := elixir.Generate(u)
	addTarget("Elixir", elixirFiles, elixirErr,
		expectation{"def count, do: 2", 1},
		expectation{"def count, do: 3", 1},
		expectation{"def max, do: 15", 1})

	for _, tgt := range targets {
		if tgt.genErr != nil {
			t.Errorf("%s: generate: %v", tgt.name, tgt.genErr)
			continue
		}
		for _, e := range tgt.expects {
			if got := countAcross(tgt.files, e.needle); got != e.count {
				t.Errorf("%s: %q emitted %d times, want %d — every generated enum carries Count beside Max (SPEC §4.2)", tgt.name, e.needle, got, e.count)
			}
		}
	}
}

// A union declaration emits its full surface in every target (SPEC §4.8):
// the <Union>Type tag enum with None/variants/Max, the storage shape, the
// wire pair, and the bounds. The needles pin each target's idiom — red-first
// against the pre-union compiler, where the C backend refused the IR kind
// loudly and every other backend skipped it silently.
func TestUnionSurfaceEmitted(t *testing.T) {
	src := "package t\n\n" +
		"type Box { x uint8 }\n\n" +
		"type Ball { r uint16 }\n\n" +
		"union Shape {\n    box  Box\n    ball Ball\n}\n\n" +
		"type Holder { shape Shape }\n"
	f, perrs := parser.Parse("Unions.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Unions.schema", Name: "Unions.schema", Base: "Unions",
		Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}

	type expectation struct {
		needle string
		count  int
	}
	type target struct {
		name    string
		files   map[string][]byte
		genErr  error
		expects []expectation
	}
	var targets []target
	addTarget := func(name string, files map[string][]byte, err error, expects ...expectation) {
		targets = append(targets, target{name, files, err, expects})
	}

	goFiles, goErr := Generate(u)
	addTarget("Go", goFiles, goErr,
		expectation{"type ShapeType uint8", 1},
		expectation{"ShapeTypeMax", 1},
		expectation{"type Shape struct", 1},
		expectation{"func WriteShape(stream *serialize.WriteStream, value *Shape) error", 1},
		expectation{"func ReadShape(stream *serialize.ReadStream, value *Shape) error", 1},
		expectation{"const ShapeMaxBits = 18", 1})
	rustFiles, rustErr := rust.Generate(u)
	addTarget("Rust", rustFiles, rustErr,
		expectation{"pub struct ShapeType(pub u8);", 1},
		expectation{"pub const MAX: ShapeType = ShapeType(2);", 1},
		expectation{"pub enum Shape {", 1},
		expectation{"pub fn write_shape(stream: &mut WriteStream<'_>, value: &Shape) -> Result {", 1},
		expectation{"pub fn read_shape(stream: &mut ReadStream<'_>, value: &mut Shape) -> Result {", 1},
		expectation{"pub const SHAPE_MAX_BITS: u64 = 18;", 1})
	csFiles, csErr := csharp.Generate(u)
	addTarget("C#", csFiles, csErr,
		expectation{"public enum ShapeType", 1},
		expectation{"public sealed class Shape", 1},
		expectation{"public static bool WriteShape(WriteStream stream, Shape value)", 1},
		expectation{"public static bool ReadShape(ReadStream stream, Shape value)", 1},
		expectation{"public static void ZeroShape(Shape value)", 1},
		expectation{"public const long ShapeMaxBits = 18;", 1})
	jsFiles, jsErr := js.Generate(u)
	addTarget("JS", jsFiles, jsErr,
		expectation{"export const ShapeType = Object.freeze({", 1},
		expectation{"export class Shape {", 1},
		expectation{"export function WriteShape(stream, value) {", 1},
		expectation{"export function ReadShape(stream, value) {", 1},
		expectation{"export function ZeroShape(value) {", 1},
		expectation{"export const ShapeMaxBits = 18;", 1})
	cFiles, cErr := cgen.Generate(u)
	addTarget("C", cFiles, cErr,
		expectation{"typedef uint8_t ShapeType;", 1},
		expectation{"#define SHAPE_TYPE_MAX 2", 1},
		expectation{"typedef struct Shape {", 1},
		expectation{"int write_shape( serialize_write_stream_t * stream, const Shape * value )", 1},
		expectation{"int read_shape( serialize_read_stream_t * stream, Shape * value )", 1},
		expectation{"#define SHAPE_MAX_BITS 18", 1})
	cppFiles, cppErr := cpp.Generate(u)
	addTarget("C++", cppFiles, cppErr,
		expectation{"enum class ShapeType : uint8_t {", 1},
		expectation{"struct Shape\n{", 1},
		expectation{"SCHEMA_WRITE_INLINE bool WriteShape( serialize::WriteStream & stream, const Shape & value )", 1},
		expectation{"SCHEMA_READ_INLINE bool ReadShape( serialize::ReadStream & stream, Shape & value )", 1},
		expectation{"inline constexpr int64_t ShapeMaxBits = 18;", 1})

	for _, tgt := range targets {
		if tgt.genErr != nil {
			t.Errorf("%s: generate: %v", tgt.name, tgt.genErr)
			continue
		}
		for _, e := range tgt.expects {
			if got := countAcross(tgt.files, e.needle); got != e.count {
				t.Errorf("%s: %q emitted %d times, want %d — the union surface must ride every target (SPEC §4.8)", tgt.name, e.needle, got, e.count)
			}
		}
	}
}

// Flags export their declared variant count as Count (SPEC §4.2 — one name,
// not Max: the variants are independent bits, not a range with a top), and
// the wire spends EXACTLY Count bits when no | max = K widens it (schema
// the flags-width verification): a 4-variant flags field serializes in 4 bits
// in every target, write and read alike.
func TestFlagsCountAndWireBits(t *testing.T) {
	src := "package t\n\n" +
		"flags Caps { A, B, C, D }\n\n" +
		"type Holder { caps Caps }\n"
	f, perrs := parser.Parse("Flags.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Flags.schema", Name: "Flags.schema", Base: "Flags",
		Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	if got := ir.MaxBitsStruct(u.Structs["Holder"]); got != 4 {
		t.Fatalf("MaxBits(Holder) = %d, want 4 — a 4-variant flags spends exactly Count bits", got)
	}

	type expectation struct {
		needle string
		count  int
	}
	type target struct {
		name    string
		files   map[string][]byte
		genErr  error
		expects []expectation
	}
	var targets []target
	addTarget := func(name string, files map[string][]byte, err error, expects ...expectation) {
		targets = append(targets, target{name, files, err, expects})
	}

	goFiles, goErr := Generate(u)
	addTarget("Go", goFiles, goErr,
		expectation{"const CapsCount = 4", 1},
		expectation{"stream.SerializeBits(&flagsValue, 4)", 2}) // write + read
	rustFiles, rustErr := rust.Generate(u)
	addTarget("Rust", rustFiles, rustErr,
		expectation{"pub const CAPS_COUNT: i64 = 4;", 1},
		expectation{"stream.serialize_bits(&mut flags_value, 4)?;", 2})
	csFiles, csErr := csharp.Generate(u)
	addTarget("C#", csFiles, csErr,
		expectation{"public const long CapsCount = 4;", 1},
		expectation{"SerializeBits(ref flagsValue, 4)", 2})
	jsFiles, jsErr := js.Generate(u)
	addTarget("JS", jsFiles, jsErr,
		expectation{"export const CapsCount = 4;", 1},
		expectation{"stream.serializeBits(NUMBER_SCRATCH, 4)", 2})
	cFiles, cErr := cgen.Generate(u)
	addTarget("C", cFiles, cErr,
		expectation{"#define CAPS_COUNT 4", 1},
		expectation{"serialize_write_bits( stream, (serialize_uint32_t) value->caps, 4 )", 1},
		expectation{"serialize_read_bits( stream, &flags_value, 4 )", 1})
	cppFlagFiles, cppFlagErr := cpp.Generate(u)
	addTarget("C++", cppFlagFiles, cppFlagErr,
		expectation{"inline constexpr int64_t CapsCount = 4;", 1},
		expectation{"write_bits( stream, value.caps, 4 );", 1},
		expectation{"read_bits( stream, value.caps, 4 );", 1})

	for _, tgt := range targets {
		if tgt.genErr != nil {
			t.Errorf("%s: generate: %v", tgt.name, tgt.genErr)
			continue
		}
		for _, e := range tgt.expects {
			if got := countAcross(tgt.files, e.needle); got != e.count {
				t.Errorf("%s: %q emitted %d times, want %d — flags spend exactly Count wire bits and export Count (SPEC §4.2)", tgt.name, e.needle, got, e.count)
			}
		}
	}
}

// generateGo compiles one in-test schema through the public door and returns
// the emitted Go, so a property can be stated against a shape the corpus does
// not carry.
func generateGo(t *testing.T, name, src string) map[string][]byte {
	t.Helper()
	f, perrs := parser.Parse(name+".schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: name + ".schema", Name: name + ".schema", Base: name,
		Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// funcBody returns the text of the named generated function, so a property
// about one type's body is not confused by another type's own functions.
func funcBody(t *testing.T, files map[string][]byte, decl string) string {
	t.Helper()
	for _, src := range files {
		text := string(src)
		start := strings.Index(text, decl)
		if start < 0 {
			continue
		}
		end := strings.Index(text[start:], "\n}\n")
		if end < 0 {
			t.Fatalf("%s is not terminated", decl)
		}
		return text[start : start+end]
	}
	t.Fatalf("%s was not emitted", decl)
	return ""
}

// The flat word codec classifies one item into 1..N pieces, so a run that
// falls back to the per-field form must re-emit its ITEMS, never its pieces:
// an unrolled fixed array's pieces are one item, and a flattened nested
// struct's pieces name fields of ANOTHER type at another base expression.
// Emitting pieces produced silent wire corruption (a `[2]float64` written
// twice), generated Go that did not compile (a nested field named on the
// outer type), and — with a name collision — would have serialized the wrong
// field silently. Degenerate.schema carries these shapes into the
// cross-language wire gate; this test states the property directly, against
// maxRunBits itself, so re-tuning that constant cannot quietly retire it.
func TestFlatFallbackReEmitsItemsNotPieces(t *testing.T) {
	// A fixed scalar array whose elements each fill a whole chunk: flattening
	// removes no stream call, so the run falls back — over ONE item.
	files := generateGo(t, "Span", "package t\n\ntype Pair { values [2]float64 }\n")
	if got := countAcross(files, "for i := int(0); i < 2; i++ {"); got != 2 {
		t.Errorf("[2]float64 emitted %d element loops, want 2 (one write, one read) — "+
			"a fallback over pieces emits the loop once per element and doubles the wire", got)
	}

	// The nested-struct case, calibrated to the run cap at test time: a
	// prefix that leaves room for part of a nested struct's fields, so the
	// run splits with the nested type's pieces on both sides.
	var src strings.Builder
	src.WriteString("package t\n\ntype Inner\n{\n    a bits(20)\n    b bits(20)\n    c bits(24)\n}\n\ntype Outer {\n")
	const innerBits = 64
	prefix := maxRunBits - innerBits + 20 // the split lands inside Inner
	for w := prefix; w > 0; {
		n := min(w, 64)
		fmt.Fprintf(&src, "    pad%d bits(%d)\n", w, n)
		w -= n
	}
	src.WriteString("    inner Inner\n}\n")
	files = generateGo(t, "Straddle", src.String())
	for _, fn := range []string{"func WriteOuter(", "func ReadOuter("} {
		body := funcBody(t, files, fn)
		for _, forbidden := range []string{"value.A", "value.B", "value.C"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("a nested struct split by the %d-bit run cap named %q on the OUTER type in %s — "+
					"Inner's fields are reachable only through value.Inner, so the fallback must "+
					"re-emit the ITEM against its own base", maxRunBits, forbidden, fn)
			}
		}
		if !strings.Contains(body, "value.Inner") {
			t.Errorf("%s places none of Inner's fields — the shape under test did not arise", fn)
		}
	}

	// A file whose every float run falls back imports no math: needsMath is
	// set at emission, not during the speculative classification every run
	// accumulator performs.
	files = generateGo(t, "Vec", "package t\n\ntype Vec2\n{\n    x float64\n    y float64\n}\n")
	if got := countAcross(files, `"math"`); got != 0 {
		t.Errorf("a bare two-float unit imported math %d times — nothing in it reaches "+
			"math, so the import does not compile", got)
	}
	if got := countAcross(files, "math."); got != 0 {
		t.Errorf("a bare two-float unit referenced math %d times — its runs fall back "+
			"to serialize's own float calls", got)
	}
}
