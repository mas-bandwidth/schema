// The multi-file dispatch test: messages and objects spread across schema
// files are legal (SPEC §2 — the aspect layout is never compiler-enforced),
// so the unit-level dispatch surface must be emitted exactly ONCE per unit in
// every target, or the generated package/TU cannot compile.
package golang

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/internal/check"
	cgen "github.com/mas-bandwidth/schema/internal/codegen/c"
	"github.com/mas-bandwidth/schema/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/internal/codegen/js"
	"github.com/mas-bandwidth/schema/internal/codegen/rust"
	"github.com/mas-bandwidth/schema/internal/parser"
	"github.com/mas-bandwidth/schema/ir"
)

func multiFileUnit(t *testing.T) *ir.Unit {
	t.Helper()
	sources := map[string]string{
		"MessagesA.schema": "package t\nmessage Ping { x uint8 }\nobject Rock { size uint8 [interpolate, min = 0, max = 100] }\n",
		"MessagesB.schema": "package t\nmessage Pong { y uint8 }\nobject Tree { size uint8 [interpolate, min = 0, max = 100] }\n",
	}
	var files []check.SourceFile
	for name, src := range sources {
		f, perrs := parser.Parse(name, []byte(src))
		if len(perrs) > 0 {
			t.Fatalf("parse: %v", perrs[0])
		}
		files = append(files, check.SourceFile{
			Path: name, Name: name, Base: strings.TrimSuffix(name, ".schema"),
			Bytes: []byte(src), AST: f,
		})
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

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
// (issue #120). An untyped Go constant is a different exported type: it
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
// §4.2 — the exported-extent rule, issue #121): declared enums (headroom
// included) and the generated MessageType/ObjectType tag enums alike, in
// each target's own convention. Call sites then state ranges against
// E.Max's generated twin instead of a hand-declared count constant.
func TestEnumExtentEmitted(t *testing.T) {
	src := "package t\n\n" +
		"enum Weapon [max = 15] { Laser, Missile }\n\n" +
		"message Ping { w Weapon }\n\n" +
		"message Pong { y uint8 }\n\n" +
		"object Rock { size uint8 [interpolate, min = 0, max = 100] }\n\n" +
		"object Tree { size uint8 [interpolate, min = 0, max = 100] }\n"
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
		expectation{"MessageTypeMax", 1},
		expectation{"ObjectTypeMax", 1},
		expectation{"= 15 // the exported extent (SPEC §4.2)", 1},
		expectation{"= 2 // the exported extent (SPEC §4.2)", 2})
	rustFiles, rustErr := rust.Generate(u)
	addTarget("Rust", rustFiles, rustErr,
		expectation{"pub const MAX: Weapon = Weapon(15);", 1},
		expectation{"pub const MAX: MessageType = MessageType(2);", 1},
		expectation{"pub const MAX: ObjectType = ObjectType(2);", 1})
	csFiles, csErr := csharp.Generate(u)
	addTarget("C#", csFiles, csErr,
		expectation{"Max = 15,", 1}, // Weapon
		expectation{"Max = 2,", 2})  // MessageType + ObjectType
	jsFiles, jsErr := js.Generate(u)
	addTarget("JS", jsFiles, jsErr,
		expectation{"Max: 15,", 1},
		expectation{"Max: 2,", 2})
	cFiles, cErr := cgen.Generate(u)
	addTarget("C", cFiles, cErr,
		expectation{"#define WEAPON_MAX 15", 1},
		expectation{"#define MESSAGE_TYPE_MAX 2", 1})
	for _, mode := range []string{"union", "variant"} {
		cppFiles, cppErr := cpp.Generate(u, cpp.Options{MessageRepr: mode})
		addTarget("C++ ("+mode+")", cppFiles, cppErr,
			expectation{"Max = 15,", 1},
			expectation{"Max = 2,", 2})
	}

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
	for _, mode := range []string{"union", "variant"} {
		cppFiles, cppErr := cpp.Generate(u, cpp.Options{MessageRepr: mode})
		addTarget("C++ ("+mode+")", cppFiles, cppErr,
			expectation{"enum class ShapeType : uint8_t {", 1},
			expectation{"struct Shape\n{", 1},
			expectation{"SCHEMA_WRITE_INLINE bool WriteShape( serialize::WriteStream & stream, const Shape & value )", 1},
			expectation{"SCHEMA_READ_INLINE bool ReadShape( serialize::ReadStream & stream, Shape & value )", 1},
			expectation{"inline constexpr int64_t ShapeMaxBits = 18;", 1})
	}

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

func TestDispatchSurfaceEmittedOnce(t *testing.T) {
	u := multiFileUnit(t)

	goFiles, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{
		"type MessageType ", "type Message interface", "type MessageStorage struct",
		"func WriteMessage(", "func ReadMessage(", "func WriteMessageType(",
		"type ObjectType ", "func WriteObjectType(",
	} {
		if got := countAcross(goFiles, needle); got != 1 {
			t.Errorf("Go: %q emitted %d times across the unit — the package cannot compile unless it is exactly once", needle, got)
		}
	}
	// both messages' dispatch methods live with the surface, in the owner file
	if got := countAcross(goFiles, ") MessageType() MessageType {"); got != 2 {
		t.Errorf("Go: expected 2 dispatch methods (one per message), got %d", got)
	}

	rustFiles, err := rust.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{
		"pub struct MessageType(", "pub enum Message {", "pub struct ObjectType(",
		// the inline taxonomy rides on the dispatch-once gate, as the C++
		// twin's does below. Rust's line is NOT C++'s: the WRITE spines and
		// tag writers demand, and every READ surface plus write_message keeps
		// the plain hint — the C++ blanket read demand was ported, measured,
		// and refused (see rust/inline.go's read-half note).
		"#[inline(always)]\npub fn write_message_type(", "#[inline(always)]\npub fn write_object_type(",
		"#[inline]\npub fn write_message(", "#[inline]\npub fn read_message(",
		"#[inline]\npub fn read_message_into(", "#[inline]\npub fn read_message_type(",
		"#[inline]\npub fn read_object_type(",
	} {
		if got := countAcross(rustFiles, needle); got != 1 {
			t.Errorf("Rust: %q emitted %d times across the unit — the crate cannot compile unless it is exactly once", needle, got)
		}
	}
	// both messages ride the one dispatch enum, in the owner file
	if got := countAcross(rustFiles, "(value) => {"); got != 2 {
		t.Errorf("Rust: expected 2 write dispatch arms (one per message), got %d", got)
	}

	csFiles, err := csharp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{
		"public enum MessageType", "public abstract class Message", "public sealed class MessageStorage",
		"public static bool WriteMessage(", "public static bool ReadMessage(", "public static bool WriteMessageType(",
		"public enum ObjectType", "public static bool WriteObjectType(",
	} {
		if got := countAcross(csFiles, needle); got != 1 {
			t.Errorf("C#: %q emitted %d times across the unit — the compilation cannot succeed unless it is exactly once", needle, got)
		}
	}
	// both messages carry the dispatch property, beside their own class
	if got := countAcross(csFiles, "public override MessageType Type =>"); got != 2 {
		t.Errorf("C#: expected 2 Type dispatch overrides (one per message), got %d", got)
	}

	for _, mode := range []string{"union", "variant"} {
		cppFiles, err := cpp.Generate(u, cpp.Options{MessageRepr: mode})
		if err != nil {
			t.Fatal(err)
		}
		for _, needle := range []string{
			// WriteMessage is the dispatch surface — deliberately plain `inline`,
			// outside the write-spine demand (see cpp emitWriteDispatchComment)
			"enum class MessageType", "inline bool WriteMessage(", "SCHEMA_READ_INLINE bool ReadMessage(",
			"enum class ObjectType", "SCHEMA_WRITE_INLINE bool WriteObjectType(", "SCHEMA_READ_INLINE bool ReadObjectType(",
		} {
			if got := countAcross(cppFiles, needle); got != 1 {
				t.Errorf("C++ (%s): %q emitted %d times across the unit — a TU including both headers cannot compile unless it is exactly once", mode, needle, got)
			}
		}
		// the owner file must include the other message file: the dispatch
		// value holds every message by value
		owner := ir.MessageOwner(u) + ".h"
		other := "MessagesA"
		if ir.MessageOwner(u) == "MessagesA" {
			other = "MessagesB"
		}
		if !strings.Contains(string(cppFiles[owner]), "#include \""+other+".h\"") {
			t.Errorf("C++ (%s): the dispatch owner %s does not include %s.h", mode, owner, other)
		}
	}
}
