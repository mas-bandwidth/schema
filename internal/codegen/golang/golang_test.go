package golang

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
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
