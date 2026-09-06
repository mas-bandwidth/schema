package check_test

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
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

// VARIANT ORDER is a wire fact (SPEC §3.1). An enum value rides as its
// declaration ordinal and a flags variant as its bit position, so a reorder
// changes what every stored ordinal and every set bit MEANS while the shape
// stays put. The ordered variant names are the only record of that mapping,
// so they project; the price is that a RENAME moves the id too, which the
// ship-together rule makes free. This is the direction that must never
// regress — without the names, two builds either side of an alphabetized enum
// hold one id and read each other's values as the wrong variant.
func TestIdMovesUnderVariantOrder(t *testing.T) {
	const source = `package probe

enum Grade { Bronze, Silver, Gold }

flags Perks { Fast, Quiet, Tough }

type State {
    grade Grade
    perks Perks
}
`
	base := build(t, source).ProtocolId

	cases := []struct {
		name   string
		source string
	}{
		{"an enum reordered", strings.Replace(source, "{ Bronze, Silver, Gold }", "{ Gold, Silver, Bronze }", 1)},
		{"an enum variant renamed (declaration order is spelled in the names)",
			strings.Replace(source, "{ Bronze, Silver, Gold }", "{ Copper, Silver, Gold }", 1)},
		{"a flags declaration reordered", strings.Replace(source, "{ Fast, Quiet, Tough }", "{ Tough, Quiet, Fast }", 1)},
		{"a flags variant renamed", strings.Replace(source, "{ Fast, Quiet, Tough }", "{ Rapid, Quiet, Tough }", 1)},
	}
	for _, tc := range cases {
		if got := build(t, tc.source).ProtocolId; got == base {
			t.Errorf("%s did NOT move the protocol id (0x%016x) — every ordinal changed meaning and two incompatible builds would claim compatibility",
				tc.name, base)
		}
	}
}

// THE CODEC LAW (SPEC §3.1): a second version line beside the rendering's own,
// for the class of change the rendering cannot see — a compiler that alters
// the bytes, the accepted inputs, the rejections, the materialized defaults or
// a numeric conversion for the same schema and values. Held here in two parts:
// the id IS the digest over the projection text, so nothing outside the text
// can reach it, and a different law line therefore gives a different id.
func TestWireLawLineMovesTheId(t *testing.T) {
	u := build(t, baseSchema)
	text := ir.WireProjection(u)

	lawLine := fmt.Sprintf("schema-wire-law %d\n", ir.WireLaw)
	if !strings.HasPrefix(text, fmt.Sprintf("schema-wire-projection %d\n%s", ir.ProjectionVersion, lawLine)) {
		t.Fatalf("the projection must open with its rendering version and then its codec law; it opens:\n%s", text)
	}

	if got := digest(text); got != u.ProtocolId {
		t.Fatalf("the protocol id is not the digest over the projection text (0x%016x vs 0x%016x) — §3.1's procedure has moved", u.ProtocolId, got)
	}
	bumped := strings.Replace(text, lawLine, fmt.Sprintf("schema-wire-law %d\n", ir.WireLaw+1), 1)
	if digest(bumped) == u.ProtocolId {
		t.Error("bumping the codec law left the id where it was — a rounding-rule change would ship as a false match")
	}
}

// digest is §3.1's procedure, spelled out where a test can see it: the low 64
// bits of SHA-256 over the projection, the final eight bytes big-endian.
func digest(projection string) uint64 {
	sum := sha256.Sum256([]byte(projection))
	return binary.BigEndian.Uint64(sum[24:])
}

// Union id behavior (SPEC §4.8, §3.1): a union is wire structure — arm order,
// count, arm NAMES and payload types all project. The names are what the
// payload types cannot carry alone: two arms of ONE payload type reorder
// invisibly without them (#491), the enum case exactly, so an arm rename
// moves the id like an enum variant's.
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
		{"an arm renamed (arm order is spelled in names)", strings.Replace(unionSchema, "ring Ring", "hoop Ring", 1)},
	}
	for _, tc := range moves {
		if got := build(t, tc.source).ProtocolId; got == base {
			t.Errorf("%s did NOT move the protocol id (0x%016x) — two incompatible builds would claim compatibility", tc.name, base)
		}
	}
}

// THE SAME-TYPED ARMS (#491), the shape the payload types cannot describe: two
// arms of one payload type reorder with every projected type unmoved, so tag 1
// means `left` on one build and `right` on the other under a single id. The
// arm names are the whole of the difference, and this is the case that names
// why they project.
func TestUnionIdMovesUnderSameTypedArmReorder(t *testing.T) {
	const source = `package probe

type Arm {
    v uint8
}

union Held {
    left  Arm
    right Arm
}

type Holder {
    held Held
}
`
	reordered := strings.Replace(source, "    left  Arm\n    right Arm", "    right Arm\n    left  Arm", 1)
	if reordered == source {
		t.Fatal("the edit patched nothing — the fixture drifted")
	}
	base, got := build(t, source).ProtocolId, build(t, reordered).ProtocolId
	if got == base {
		t.Errorf("two arms of one payload type reordered and the id did not move (0x%016x) — tag 1 means a different arm on each build and both claim compatibility", base)
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

// E.Count is the DECLARED variant count of an enum, excluding the implicit
// None (SPEC §4.2). It equals E.Max when nothing widens the enum and stays
// the count under | max = K headroom, where Max is the extent — so the two
// words mean one thing each, in enums and flags alike, and a bound written
// over either folds to the number the author asked for.
func TestEnumCountValue(t *testing.T) {
	u := build(t, `package probe

enum Plain { Laser, Missile }

enum Wide | max = 15
{
    Laser,
    Missile,
    Railgun,
}

const PlainN = Plain.Count
const PlainM = Plain.Max
const WideN  = Wide.Count
const WideM  = Wide.Max

type Loadout {
    slots [..Wide.Count]uint8
    keyed [Wide.Max]uint8
}
`)
	for _, tc := range []struct {
		name string
		want int64
	}{{"PlainN", 2}, {"PlainM", 2}, {"WideN", 3}, {"WideM", 15}} {
		c := u.Consts[tc.name]
		if c == nil || c.Int == nil || c.Int.Int64() != tc.want {
			t.Errorf("const %s: want %d, got %v — Count is the declared count and Max the extent (SPEC §4.2)", tc.name, tc.want, c)
		}
	}
	fields := u.Structs["Loadout"].Fields
	if got := fields[0].ArrayBound; got != 3 {
		t.Errorf("[..Wide.Count]uint8 bound = %d, want 3 — E.Count must fold in an array bound", got)
	}
	if got := fields[1].ArrayBound; got != 15 {
		t.Errorf("[Wide.Max]uint8 bound = %d, want 15 — E.Max stays the extent beside E.Count", got)
	}
}

// The generated Count is a CLAIMED name (SPEC §11): a user symbol that would
// collide with it is refused, and the diagnostic names the enum it belongs to
// — the same claim E.Max's generated twin has carried since it existed.
func TestEnumCountIsClaimed(t *testing.T) {
	source := "package probe\n\nenum Weapon { Laser, Missile }\n\nconst WeaponCount = 3\n"
	f, perrs := parser.Parse("Probe.schema", []byte(source))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs)
	}
	_, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(source), AST: f,
	}})
	if len(cerrs) == 0 {
		t.Fatal("const WeaponCount compiled beside enum Weapon — the generated Count would be silently overwritten")
	}
	for _, e := range cerrs {
		text := e.Error()
		if strings.Contains(text, "generated Count constant") && strings.Contains(text, "Weapon") {
			return
		}
	}
	t.Fatalf("no diagnostic names enum Weapon's generated Count constant; got: %v", cerrs)
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
// pinned as literals. Every declaration in it is REACHED, which is what makes
// the pin a rendering pin rather than a scoping one. Any rendering change that
// moves this unit's id is a compatibility break for every protocol-free unit
// in the wild and must be a deliberate ProjectionVersion or WireLaw bump.
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

type Holder {
    shape Shape
    team  Team
}
`)
	const pinnedId = uint64(0x973fe01026957464) // WireLaw 2: selected payloads start at construction defaults
	if u.ProtocolId != pinnedId {
		t.Fatalf("the neutrality probe's id moved: 0x%016x, pinned 0x%016x", u.ProtocolId, pinnedId)
	}
	const pinnedProjection = `schema-wire-projection 3
schema-wire-law 2
package probe
enum Team max=2 storage=8 variants=2
  variant 1 name=Red
  variant 2 name=Blue
flags ProbeFlags wirebits=3
  bit 0 name=Armed
  bit 1 name=Cloaked
  bit 2 name=Damaged
type Attitude table=false message=false
  field orientation kind=3 floatrange=[0,360] res=0.1 steps=3600 round=nearest
  field health kind=0 width=32 signed=true intrange=[0,1000]
type Holder table=false message=false
  field shape kind=8 type=Shape
  field team kind=8 type=Team
type Ring table=false message=false
  field radius kind=0 width=16 signed=false
union Shape max=2
  variant 1 name=ring payload=Ring
  variant 2 name=attitude payload=Attitude
`
	if got := ir.WireProjection(u); got != pinnedProjection {
		t.Fatalf("the neutrality probe's projection moved:\n%s\npinned:\n%s", got, pinnedProjection)
	}
}

// A UNION ONLY A TABLE CLOSURE REACHES CONTRIBUTES NOTHING TO THIS ID
// (SPEC §3.1, §3.2). The projection is the closure over the unit's `type`
// declarations, so a union a table holds and no type names has no packet byte
// to describe: adding an arm to it, moving an arm's bound, retyping an arm and
// renaming an arm all leave the id where it was. The same union under a `type`
// field is in the closure, and every one of those edits moves the id there —
// which is the pair that says the scoping is reachability and not a blanket
// exclusion of unions a table happens to hold.
func TestUnionOutsideTheClosureMovesNoId(t *testing.T) {
	const tableHeld = `package probe

type Ring {
    radius uint16
}

union Shape {
    ring  Ring
    count int32 | min = 0, max = 100
}

table Holder {
    shape Shape
}
`
	base := build(t, tableHeld).ProtocolId

	still := []struct {
		name   string
		source string
	}{
		{"a scalar arm added", strings.Replace(tableHeld, "    count int32 | min = 0, max = 100\n",
			"    count int32 | min = 0, max = 100\n    tally int32 | min = 0, max = 100\n", 1)},
		{"an arm's declared maximum moved", strings.Replace(tableHeld, "max = 100", "max = 200", 1)},
		{"an arm's type moved under one width", strings.Replace(tableHeld,
			"    count int32 | min = 0, max = 100\n", "    count float32\n", 1)},
		{"a payload-free arm added", strings.Replace(tableHeld, "    ring  Ring\n", "    ring  Ring\n    idle\n", 1)},
		{"an arm renamed", strings.Replace(tableHeld, "    ring  Ring\n", "    hoop  Ring\n", 1)},
	}
	for _, tc := range still {
		if got := build(t, tc.source).ProtocolId; got != base {
			t.Errorf("%s moved the protocol id (0x%016x -> 0x%016x) — no `type` reaches this union, so it writes no packet byte and buys no redeploy",
				tc.name, base, got)
		}
	}
	if strings.Contains(ir.WireProjection(build(t, tableHeld)), "union Shape") {
		t.Error("a union no `type` reaches is in the projection — the scoping is not the closure SPEC §3.1 states")
	}

	// the same union inside the closure: a `type` body takes `type` payloads
	// only (docs/SPEC-TABLES.md §2.6), so the reached form is the all-type
	// union, and every arm edit it can express moves the id
	const typeHeld = `package probe

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
	reachedBase := build(t, typeHeld).ProtocolId
	if !strings.Contains(ir.WireProjection(build(t, typeHeld)), "union Shape") {
		t.Fatal("a union a `type` field names is absent from the projection — the closure lost edge 1")
	}
	moves := []struct {
		name   string
		source string
	}{
		{"an arm added", strings.Replace(typeHeld, "    slab Slab\n", "    slab Slab\n    slab_b Slab\n", 1)},
		{"the arms reordered", strings.Replace(typeHeld, "    ring Ring\n    slab Slab", "    slab Slab\n    ring Ring", 1)},
		{"an arm renamed", strings.Replace(typeHeld, "    ring Ring\n", "    hoop Ring\n", 1)},
		{"a payload-free arm added", strings.Replace(typeHeld, "    ring Ring\n", "    ring Ring\n    idle\n", 1)},
	}
	for _, tc := range moves {
		if got := build(t, tc.source).ProtocolId; got == reachedBase {
			t.Errorf("%s did NOT move the protocol id (0x%016x) — the union is in the closure and its arms are wire facts",
				tc.name, reachedBase)
		}
	}
}

// THE NEGATIVE CONTROL IS SEVEN ISOLABLE CASES (SPEC §3.1), one for every edge
// kind a case can isolate, and that is what the reachability obligation costs.
// A missed edge is the dangerous direction, a declaration a packet byte reaches
// that the walk does not, which is two incompatible builds shaking hands, so a
// single control over a single edge proves nothing about the rest.
//
// Each case holds ONE enum reachable only through one edge kind, and edits it
// by REORDERING its variants: every folded number in the projection stays
// exactly where it was, so the id moves only if the walk reached the enum and
// put its ordered variant names in the text. Removing that edge from the walk
// takes the enum out of the projection and the case goes red here.
//
// A reorder is a byte-moving edit for every one of them. Where the enum is a
// field type or an array element, a value rides as its declaration ordinal, so
// a reorder changes what every stored ordinal means. Where it is an extent,
// `[E.Max]T`, `[E]T`, or a `const` standing in for either, the array is
// positional and slot i holds the key i + 1 (docs/SPEC-TABLES.md §2.4), so a
// reorder changes what every element is.
//
// EDGE 6 IS THE ONE CASE THIS LEG DOES NOT ISOLATE, and it is stated rather
// than left for a reader to find. A union in a `type` body takes `type`
// payloads only (docs/SPEC-TABLES.md §2.6), and EVERY `type` IS A ROOT, so a
// projected union's arm payload is a root by construction and is in the closure
// before the arm is walked: deleting the arm descent from the walk leaves case
// 6 green. The descent is implemented because the rule is the rule, and the
// case is held here as the path the page names, red through edge 1 reaching the
// union and edge 8 descending into the payload type. What holds edge 6 is
// TestUnionOutsideTheClosureMovesNoId's arm-edit pair (SPEC §3.1).
func TestReachabilityEdgeControls(t *testing.T) {
	cases := []struct {
		edge   int
		what   string
		source string
	}{
		{1, "an enum named only as a field type", `package probe

enum Only { Alpha, Beta, Gamma }

type Root {
    e Only
}
`},
		{2, "an enum named only as an array element", `package probe

enum Only { Alpha, Beta, Gamma }

type Root {
    a [4]Only
}
`},
		{3, "an enum named only by an [E.Max] bound", `package probe

enum Only { Alpha, Beta, Gamma }

type Root {
    a [Only.Max]uint8
}
`},
		{4, "an enum named only as an [E]T key", `package probe

enum Only { Alpha, Beta, Gamma }

type Root {
    a [Only]uint8
}
`},
		{5, "an enum named only through a const", `package probe

enum Only { Alpha, Beta, Gamma }

const Slots = Only.Max

type Root {
    a [Slots]uint8
}
`},
		{6, "an enum named only inside a union arm's payload type", `package probe

enum Only { Alpha, Beta, Gamma }

type Arm {
    e Only
}

union Held {
    arm Arm
}

type Root {
    held Held
}
`},
		{7, "an enum named only inside an else body", `package probe

enum Only { Alpha, Beta, Gamma }

type Root {
    live bool
    if live {
        n uint8
    } else {
        e Only
    }
}
`},
		{8, "an enum named only through a type two steps away", `package probe

enum Only { Alpha, Beta, Gamma }

type Deep {
    e Only
}

type Middle {
    d Deep
}

type Root {
    m Middle
}
`},
	}

	for _, tc := range cases {
		u := build(t, tc.source)
		if !strings.Contains(ir.WireProjection(u), "enum Only ") {
			t.Errorf("edge %d, %s: the enum is absent from the projection — the walk does not carry this edge, and a build that reorders Only shakes hands with one that did not",
				tc.edge, tc.what)
			continue
		}
		reordered := strings.Replace(tc.source, "{ Alpha, Beta, Gamma }", "{ Gamma, Beta, Alpha }", 1)
		if reordered == tc.source {
			t.Fatalf("edge %d: the edit patched nothing — the fixture drifted", tc.edge)
		}
		if got, base := build(t, reordered).ProtocolId, u.ProtocolId; got == base {
			t.Errorf("edge %d, %s: reordering Only did NOT move the protocol id (0x%016x) — every ordinal changed meaning and two incompatible builds would claim compatibility",
				tc.edge, tc.what, base)
		}
	}
}

// The other direction of the same rule, and the churn the scoping exists to
// end: a declaration ONLY a table body reaches is out of the projection, so
// the content enum a studio grows weekly buys no coordinated redeploy
// (SPEC §3.2). `flags` is the ONE exception and is held here beside it.
func TestUnreachedDeclarationsAreOutOfTheProjection(t *testing.T) {
	const source = `package probe

enum ItemKind { Sword, Shield }

enum Carried { Alpha, Beta }

flags Perks { Fast, Quiet }

type Packet {
    e Carried
}

table Bag {
    kind  ItemKind
    perks Perks
}
`
	u := build(t, source)
	proj := ir.WireProjection(u)
	if strings.Contains(proj, "enum ItemKind") {
		t.Error("an enum only a table body reaches is in the projection — every variant added to a content enum buys a coordinated redeploy for a byte no packet carries")
	}
	if !strings.Contains(proj, "enum Carried") {
		t.Error("an enum a `type` reaches is absent from the projection — the closure lost edge 1")
	}
	if !strings.Contains(proj, "flags Perks") {
		t.Fatal("a flags declaration no `type` reaches left the projection — the connect gate is the only runtime frame that refuses two peers holding different bit assignments (SPEC §3.1)")
	}

	// the edits that must NOT move the id, because no packet byte moves
	for _, tc := range []struct {
		name   string
		source string
	}{
		{"a variant added to a table-only enum", strings.Replace(source, "{ Sword, Shield }", "{ Sword, Shield, Potion }", 1)},
		{"a table-only enum reordered", strings.Replace(source, "{ Sword, Shield }", "{ Shield, Sword }", 1)},
		{"a table-only enum renamed at a variant", strings.Replace(source, "{ Sword, Shield }", "{ Sabre, Shield }", 1)},
	} {
		if got := build(t, tc.source).ProtocolId; got != u.ProtocolId {
			t.Errorf("%s moved the protocol id (0x%016x -> 0x%016x) — the table wire reads that vocabulary by name and reports every move in it (docs/SPEC-TABLES.md §4)",
				tc.name, u.ProtocolId, got)
		}
	}
	// and the one that must, because no read report can see a bit reassigned
	for _, tc := range []struct {
		name   string
		source string
	}{
		{"a flags declaration reordered", strings.Replace(source, "{ Fast, Quiet }", "{ Quiet, Fast }", 1)},
		{"a flags variant renamed", strings.Replace(source, "{ Fast, Quiet }", "{ Rapid, Quiet }", 1)},
	} {
		if got := build(t, tc.source).ProtocolId; got == u.ProtocolId {
			t.Errorf("%s did NOT move the protocol id (0x%016x) — flags is the table wire's one positional vocabulary and the connect gate is the only frame that refuses it",
				tc.name, u.ProtocolId)
		}
	}
}

// A REACHABILITY MOVE NEVER ARRIVES ALONE (SPEC §3.2): removing the last
// type-side use of an enum takes its lines out of the projection, and the use
// that moved is a `type` edit in the same commit, so it costs no id move that
// was not owed already. Held as the pair it is.
func TestReachabilityMoveRidesWithItsTypeEdit(t *testing.T) {
	const both = `package probe

enum Kind { Alpha, Beta }

type Packet {
    k    Kind
    seq  uint32
}

table Bag {
    kind Kind
}
`
	dropped := strings.Replace(both, "    k    Kind\n", "", 1)
	withUse, without := build(t, both), build(t, dropped)
	if withUse.ProtocolId == without.ProtocolId {
		t.Fatal("dropping the last type-side use of an enum moved no id — a field left the wire")
	}
	if !strings.Contains(ir.WireProjection(withUse), "enum Kind") {
		t.Error("the enum a type field names is out of the projection")
	}
	if strings.Contains(ir.WireProjection(without), "enum Kind") {
		t.Error("the enum survived in the projection with no type-side use left — the closure is not scoped")
	}
}

// A BOUND MAY NAME A UNION'S GENERATED TAG SET, `<Union>Type.Max` (SPEC §4.2,
// §4.8), and that is edge 3 through a name no declaration spells. The array is
// positional over the arms, so slot i belongs to arm i + 1 and a reorder
// changes what every element is — the union has to be in the closure, and a
// walk that resolved only declared names would miss it.
func TestAGeneratedSetBoundReachesItsUnion(t *testing.T) {
	const source = `package probe

type Arm { v uint8 }

union Held
{
    left  Arm
    right Arm
}

type Root
{
    slots [HeldType.Max]uint8
}
`
	u := build(t, source)
	if !strings.Contains(ir.WireProjection(u), "union Held") {
		t.Fatal("a union named only by a [HeldType.Max] bound is out of the projection — a reorder of its arms would change what every slot means under one id")
	}
	reordered := strings.Replace(source, "    left  Arm\n    right Arm", "    right Arm\n    left  Arm", 1)
	if reordered == source {
		t.Fatal("the edit patched nothing — the fixture drifted")
	}
	if got := build(t, reordered).ProtocolId; got == u.ProtocolId {
		t.Errorf("reordering the arms did NOT move the protocol id (0x%016x) — every slot of the positional array changed meaning", u.ProtocolId)
	}
}
