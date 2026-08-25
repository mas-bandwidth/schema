package check_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// build compiles a one-file unit and returns it, failing the test on any error.
func build(t *testing.T, source string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	f, perrs := parser.Parse(path, []byte(source))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: path, Name: "Probe.schema", Base: "Probe", Bytes: []byte(source), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs)
	}
	return u
}

const baseSchema = `package probe

const MaxHealth = 1000

enum Team { Red, Blue }

type Vec3 {
    x float64
    y float64
    z float64
}

type State {
    team     Team
    position Vec3
    health   int32 | min = 0, max = MaxHealth
    at_rest  bool
    if !at_rest {
        velocity Vec3
    }
}
`

// The point of the projection: an edit that moves no bytes must not move the
// protocol id. Each of these used to force a coordinated redeploy for nothing.
func TestIdIsStableUnderNonWireEdits(t *testing.T) {
	base := build(t, baseSchema).ProtocolId

	cases := []struct {
		name   string
		source string
	}{
		{"a trailing comment", baseSchema + "\n// a comment costs no bits\n"},
		{"an interior comment", strings.Replace(baseSchema, "type State {", "// describes a ship\ntype State {", 1)},
		{"blank lines", strings.Replace(baseSchema, "enum Team", "\n\nenum Team", 1)},
		{"an enum variant renamed", strings.Replace(baseSchema, "{ Red, Blue }", "{ Crimson, Blue }", 1)},
		{"a const renamed", strings.NewReplacer("MaxHealth", "HealthCeiling").Replace(baseSchema)},
	}

	for _, tc := range cases {
		got := build(t, tc.source).ProtocolId
		if got != base {
			t.Errorf("%s moved the protocol id (0x%016x -> 0x%016x); no wire byte changed",
				tc.name, base, got)
		}
	}
}

// Float const inference is id-neutral (SPEC §4.2): a bare float
// const infers float64, so dropping the annotation resolves to the same
// value reaching the same wire facts — the id must not move in either
// direction, whether the const feeds an attribute or sits unused.
func TestIdIsStableUnderFloatConstAnnotation(t *testing.T) {
	const annotated = `package probe

const Half float64 = 180.0

type Sample {
    orientation float32 | min = -Half, max = Half, resolution = 0.01
}
`
	bare := strings.Replace(annotated, "const Half float64 = 180.0", "const Half = 180.0", 1)
	a := build(t, annotated).ProtocolId
	b := build(t, bare).ProtocolId
	if a != b {
		t.Errorf("removing a float const's annotation moved the protocol id (0x%016x -> 0x%016x); the inferred type is the annotated type and no wire byte changed", a, b)
	}
}

// The other direction, and the one that must never regress: an edit that DOES
// move bytes must move the id. A missed case here is two incompatible builds
// claiming compatibility.
func TestIdMovesUnderWireEdits(t *testing.T) {
	base := build(t, baseSchema).ProtocolId

	cases := []struct {
		name   string
		source string
	}{
		{"a widened bound", strings.Replace(baseSchema, "const MaxHealth = 1000", "const MaxHealth = 2000", 1)},
		{"a field renamed (field names are projected facts)",
			strings.Replace(baseSchema, "health   int32", "hp       int32", 1)},
		{"a field reordered", strings.Replace(baseSchema,
			"    team     Team\n    position Vec3", "    position Vec3\n    team     Team", 1)},
		{"a new enum variant (the tag range widens)",
			strings.Replace(baseSchema, "{ Red, Blue }", "{ Red, Blue, Green }", 1)},
		{"a field added", strings.Replace(baseSchema, "    at_rest  bool", "    shield   int32 | min = 0, max = 100\n    at_rest  bool", 1)},
		{"a branch inverted", strings.Replace(baseSchema, "if !at_rest {", "if at_rest {", 1)},
		{"a type widened", strings.Replace(baseSchema, "    x float64", "    x float32", 1)},
		{"the package renamed", strings.Replace(baseSchema, "package probe", "package other", 1)},
	}

	for _, tc := range cases {
		got := build(t, tc.source).ProtocolId
		if got == base {
			t.Errorf("%s did NOT move the protocol id (0x%016x) — two incompatible builds would claim compatibility",
				tc.name, base)
		}
	}
}

// Union id behavior (SPEC §4.8, §3.1): a union is wire structure — variant
// order, count and payload types project — but variant NAMES do not, the
// enum-variant rule exactly.
func TestUnionIdBehavior(t *testing.T) {
	const unionSchema = `package probe

type Ring {
    radius uint16
}

type Slab {
    width uint8
}

union Shape {
    ring Ring
    slab Slab
}

type Holder {
    shape Shape
}
`
	base := build(t, unionSchema).ProtocolId

	moves := []struct {
		name   string
		source string
	}{
		{"a variant added", strings.Replace(unionSchema, "    slab Slab\n", "    slab Slab\n    slab_b Slab\n", 1)},
		{"a variant removed", strings.Replace(unionSchema, "    slab Slab\n", "", 1)},
		{"variants reordered (the tag is positional)", strings.Replace(unionSchema,
			"    ring Ring\n    slab Slab", "    slab Slab\n    ring Ring", 1)},
		{"a payload type changed", strings.Replace(unionSchema, "slab Slab", "slab Ring", 1)},
		{"a payload's field widened", strings.Replace(unionSchema, "width uint8", "width uint16", 1)},
	}
	for _, tc := range moves {
		if got := build(t, tc.source).ProtocolId; got == base {
			t.Errorf("%s did NOT move the protocol id (0x%016x) — two incompatible builds would claim compatibility", tc.name, base)
		}
	}

	stable := []struct {
		name   string
		source string
	}{
		{"a variant renamed (the ordinal is the wire)", strings.Replace(unionSchema, "ring Ring", "hoop Ring", 1)},
	}
	for _, tc := range stable {
		if got := build(t, tc.source).ProtocolId; got != base {
			t.Errorf("%s moved the protocol id (0x%016x -> 0x%016x); no wire byte changed", tc.name, base, got)
		}
	}
}

// The generated tag set resolves in constant expressions (SPEC §4.2, §4.8):
// <Union>Type.Max is the declared variant count.
func TestGeneratedSetMaxInConstExprs(t *testing.T) {
	u := build(t, `package probe

type Arm {
    v uint8
}

union Held {
    left  Arm
    right Arm
}

const Arms = HeldType.Max
`)
	for _, tc := range []struct {
		name string
		want int64
	}{{"Arms", 2}} {
		c := u.Consts[tc.name]
		if c == nil || c.Int == nil || c.Int.Int64() != tc.want {
			t.Errorf("const %s: want %d, got %v — generated-set Max must resolve during const folding", tc.name, tc.want, c)
		}
	}
}

// F.Count is the DECLARED variant count (SPEC §4.2) — under | max = K
// headroom it diverges from the wire width, and Count stays the count.
func TestFlagsCountValue(t *testing.T) {
	u := build(t, `package probe

flags Plain {
    A,
    B,
    C,
}

flags Wide | max = 8
{
    A,
    B,
    C,
}

const PlainN = Plain.Count
const WideN  = Wide.Count
`)
	for _, tc := range []struct {
		name string
		want int64
	}{{"PlainN", 3}, {"WideN", 3}} {
		c := u.Consts[tc.name]
		if c == nil || c.Int == nil || c.Int.Int64() != tc.want {
			t.Errorf("const %s: want %d, got %v — Count is the declared count, never the widened wire width", tc.name, tc.want, c)
		}
	}
	if u.Flags["Wide"].WireBits != 8 {
		t.Errorf("Wide.WireBits = %d, want 8 — the | max = 8 widening is the WIRE width, distinct from Count", u.Flags["Wide"].WireBits)
	}
}

// [..N] is pure sugar for [0..N] (SPEC §4.3): same IR, same wire, same
// protocol id — the respelling of the retired [..N] must be spelling only.
func TestUpToBoundIsSugar(t *testing.T) {
	sugar := build(t, "package probe\n\ntype T {\n    a [..8]uint16\n}\n")
	full := build(t, "package probe\n\ntype T {\n    a [0..8]uint16\n}\n")
	if sugar.ProtocolId != full.ProtocolId {
		t.Errorf("[..8] and [0..8] disagree on the protocol id (0x%016x vs 0x%016x) — the sugar changed the wire", sugar.ProtocolId, full.ProtocolId)
	}
}

// The projection is a reviewable artifact, so it has to be deterministic:
// same unit, same text, every time and in any map iteration order.
func TestProjectionIsDeterministic(t *testing.T) {
	first := ir.WireProjection(build(t, baseSchema))
	for i := range 20 {
		if got := ir.WireProjection(build(t, baseSchema)); got != first {
			t.Fatalf("projection is not deterministic on run %d", i)
		}
	}
	if !strings.HasPrefix(first, "schema-wire-projection ") {
		t.Error("the projection must carry its version on the first line")
	}
}

// A specified default is a wire fact: both ends must materialize the same
// value for an untaken branch's zeroing and for constructors, and the
// projection includes it (SPEC §3.1). Changing one must move the id.
func TestDefaultsAreWireFacts(t *testing.T) {
	withDefault := `package probe

type Config {
    retries int32 = -1
}
`
	other := strings.Replace(withDefault, "= -1", "= -2", 1)
	if build(t, withDefault).ProtocolId == build(t, other).ProtocolId {
		t.Error("changing a specified default did not move the id — defaults are projected wire facts (SPEC §3.1)")
	}
}

// Tables, messages and the round spelling left the language, and the
// projection keeps THREE FROZEN tokens so each removal moved no id for any
// unit that never declared one: `table=false` and `message=false` on every
// type line, and `round=nearest` on every compressed-float field line (the
// checker defaults it, so it rendered on every such line already). Held
// here so no token can quietly disappear. Dropping one is a
// ProjectionVersion bump, taken deliberately or not at all.
func TestFrozenTableToken(t *testing.T) {
	u := build(t, "package probe\n\ntype P {\n    x int32\n    f float32 | min = 0, max = 1, resolution = 0.1\n}\n")
	proj := ir.WireProjection(u)
	if !strings.Contains(proj, "type P table=false message=false") {
		t.Fatal("a frozen token left the type line — every unit's id just moved without a ProjectionVersion bump")
	}
	if !strings.Contains(proj, "field f kind=3 floatrange=[0,1] res=0.1 steps=10 round=nearest") {
		t.Fatal("the frozen round=nearest token left the compressed-float line — every compressed-float unit's id just moved without a ProjectionVersion bump")
	}
}

// The protocol-free neutrality probe: a unit of types, enums, flags, unions,
// constants and a COMPRESSED FLOAT (the composition matters — a float-free
// probe could not see the round token), with its id and full projection
// pinned as literals. Any rendering change that moves this unit's id is a
// compatibility break for every protocol-free unit in the wild and must be
// a deliberate ProjectionVersion bump.
func TestProtocolFreeUnitIdPinned(t *testing.T) {
	u := build(t, `package probe

const MaxHealth = 1000

enum Team { Red, Blue }

flags ProbeFlags { Armed, Cloaked, Damaged }

type Attitude {
    orientation float32 | min = 0, max = 360, resolution = 0.1
    health      int32   | min = 0, max = MaxHealth
}

type Ring {
    radius uint16
}

union Shape {
    ring     Ring
    attitude Attitude
}
`)
	const pinnedId = uint64(0xad54eaab53f241b4)
	if u.ProtocolId != pinnedId {
		t.Fatalf("the neutrality probe's id moved: 0x%016x, pinned 0x%016x", u.ProtocolId, pinnedId)
	}
	const pinnedProjection = `schema-wire-projection 1
package probe
enum Team max=2 storage=8 variants=2
flags ProbeFlags wirebits=3
type Attitude table=false message=false
  field orientation kind=3 floatrange=[0,360] res=0.1 steps=3600 round=nearest
  field health kind=0 width=32 signed=true intrange=[0,1000]
type Ring table=false message=false
  field radius kind=0 width=16 signed=false
union Shape max=2
  variant 1 payload=Ring
  variant 2 payload=Attitude
`
	if got := ir.WireProjection(u); got != pinnedProjection {
		t.Fatalf("the neutrality probe's projection moved:\n%s\npinned:\n%s", got, pinnedProjection)
	}
}
