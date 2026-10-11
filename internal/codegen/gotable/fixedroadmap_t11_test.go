package gotable

import "testing"

// THE ROADMAP'S UNION AND OPTIONAL ROWS ON THE GO LEG (docs/roadmap.sexp node
// `fixed-tables`, ROADMAP.md "NEW Fixed Tables"). Law: docs/SPEC-TABLES.md §3.4
// — "A FIXED-TABLE RECORD IS AN EIGHT-BYTE HASH OF THE WRITER'S LAYOUT, AND
// THEN THE VALUES IN DECLARED ORDER, EVERY FIELD AT ITS BOUND" — and
// docs/FIXED-FORM-ALGORITHM.md §3.1 (the write) and §4.4 (the run). One subtest
// per task this card proves, table-driven, t.Parallel() first. Each subtest
// WRITES a record with its construct at non-default and boundary values and
// READS it back through the leg's own emitted `RootFixedSave`/`RootFixedLoad`,
// so the capability is proved by a value round-trip. The harness is the leg's
// own: runGenerated emits the packet half and the table half together against
// the sibling `serialize.go` runtime, and t10Run's table-only path is used for a
// case whose packet half needs that absent sibling — the probe then declares the
// value structs the table module splices (the t10 harness's own narrowing).
//
// A root whose closure holds a payload-free (void) union arm is not proved here:
// ir.TableFixedSupported refuses a void arm (ir/fixedform.go:259-260) and that
// is outside this card's PATHS, so `payload-free-arms/go/valid-data` is
// reported unknown.
func TestFixedRoadmapGoT11UnionsOptionals(t *testing.T) {
	t.Parallel()
	for _, tc := range t11Cases {
		tc := tc
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			if tc.tableOnly {
				t10Run(t, tc.schema, tc.probe, true)
				return
			}
			runGenerated(t, tc.schema, tc.probe)
		})
	}
}

// t11Prelude is the probe's one round-trip helper: write ONE Root through the
// leg's combined writer, read it back through the leg's combined reader, and
// insist the load was clean. docs/SPEC-TABLES.md §3.4 record form.
const t11Prelude = `package probe
import "testing"

func roundtrip(t *testing.T, v Root) Root {
	t.Helper()
	buf := make([]byte, RootFixedMeasure(1))
	if n := RootFixedSave([]Root{v}, buf); n != int64(len(buf)) {
		t.Fatalf("save returned %d, want %d", n, len(buf))
	}
	back := make([]Root, 1)
	plan := make([]TableFixedEntry, 256)
	var r TableReport
	if n := RootFixedLoad(back, buf, plan, &r); n != 1 || r != (TableReport{}) {
		t.Fatalf("load returned %d, report %+v", n, r)
	}
	return back[0]
}
`

type t11Case struct {
	id        string
	schema    string
	probe     string
	tableOnly bool
}

var t11Cases = []t11Case{
	{
		id:        "union-values/go/valid-data",
		tableOnly: true,
		schema: `package probe
type Vec
{
    x int32
    y int32
}
union Body
{
    v Vec
    n int32
}
fixed table Root { body Body }
`,
		// §3.4 "EVERY FIELD AT ITS BOUND": a union is its tag then its widest
		// arm. The by-value type payload at a non-default value and the scalar
		// arm at a boundary value. The value struct the table module splices is
		// declared here because the packet half of a `type` needs the absent
		// sibling runtime.
		probe: t11Prelude + `
type Vec struct {
	X int32
	Y int32
}

func TestUnionValues(t *testing.T) {
	var v Root
	RootReset(&v)
	v.Body.Type = BodyTypeV
	v.Body.V.X = 11
	v.Body.V.Y = -22
	got := roundtrip(t, v)
	if got.Body.Type != BodyTypeV || got.Body.V.X != 11 || got.Body.V.Y != -22 {
		t.Fatalf("type arm: %+v", got.Body)
	}
	v = Root{}
	RootReset(&v)
	v.Body.Type = BodyTypeN
	v.Body.N = (1 << 30) - 3
	got = roundtrip(t, v)
	if got.Body.Type != BodyTypeN || got.Body.N != (1<<30)-3 {
		t.Fatalf("scalar arm: %+v", got.Body)
	}
}
`,
	},
	{
		id:        "union-field-arms/go/valid-data",
		tableOnly: true,
		schema: `package probe
type ScalarArm { n int32 }
type TextArm { s string(8) }
type ArrayArm
{
    a [..3]int32
}
union Body
{
    sa ScalarArm
    ta TextArm
    aa ArrayArm
}
fixed table Root { body Body }
`,
		// §3.4: an arm's payload is a by-value record at its bound, so a
		// scalar field, a bounded text field and a bounded array each land
		// under their own tag. The spliced value structs, the union and its
		// tag are declared here because the packet half of a `type` needs the
		// absent sibling runtime.
		probe: t11Prelude + `
type ScalarArm struct {
	N int32
}
type TextArm struct {
	S       [8]byte
	SLength int32
}
type ArrayArm struct {
	A      [3]int32
	ACount int32
}
type BodyType uint32

const (
	BodyTypeNone BodyType = iota
	BodyTypeSa
	BodyTypeTa
	BodyTypeAa
	BodyTypeCount
	BodyTypeMax = BodyTypeCount - 1
)

type Body struct {
	Type BodyType
	Sa   ScalarArm
	Ta   TextArm
	Aa   ArrayArm
}

func TestUnionFieldArms(t *testing.T) {
	var v Root
	RootReset(&v)
	v.Body.Type = BodyTypeSa
	v.Body.Sa.N = -5
	got := roundtrip(t, v)
	if got.Body.Type != BodyTypeSa || got.Body.Sa.N != -5 {
		t.Fatalf("scalar arm: %+v", got.Body)
	}
	v = Root{}
	RootReset(&v)
	v.Body.Type = BodyTypeTa
	v.Body.Ta.SLength = 6
	copy(v.Body.Ta.S[:], "abcdef")
	got = roundtrip(t, v)
	if got.Body.Type != BodyTypeTa || string(got.Body.Ta.S[:got.Body.Ta.SLength]) != "abcdef" {
		t.Fatalf("text arm: %+v", got.Body)
	}
	v = Root{}
	RootReset(&v)
	v.Body.Type = BodyTypeAa
	v.Body.Aa.ACount = 3
	v.Body.Aa.A[0], v.Body.Aa.A[1], v.Body.Aa.A[2] = -1, 0, 1
	got = roundtrip(t, v)
	if got.Body.Type != BodyTypeAa || got.Body.Aa.ACount != 3 || got.Body.Aa.A[2] != 1 {
		t.Fatalf("array arm: %+v", got.Body)
	}
}
`,
	},
	{
		id: "union-arrays/go/valid-data",
		schema: `package probe
union Pick
{
    a int32
    b int64
}
fixed table Root { picks [..3]Pick }
`,
		// §3.4: an array of unions is each element at its union bound, the tag
		// then the widest arm; the wire ignores the slack behind the narrow one.
		probe: t11Prelude + `
func TestUnionArrays(t *testing.T) {
	var v Root
	RootReset(&v)
	v.PicksCount = 3
	v.Picks[0].Type = PickTypeA
	v.Picks[0].A = 3
	v.Picks[1].Type = PickTypeB
	v.Picks[1].B = -(1 << 40)
	v.Picks[2].Type = PickTypeA
	v.Picks[2].A = 0
	got := roundtrip(t, v)
	if got.PicksCount != 3 || got.Picks[0].A != 3 || got.Picks[1].B != -(1<<40) || got.Picks[2].Type != PickTypeA {
		t.Fatalf("union array: %+v", got.Picks[:got.PicksCount])
	}
}
`,
	},
	{
		id: "optional-scalars/go/valid-data",
		schema: `package probe
enum Grade { Gold, Silver }
fixed table Root
{
    n ?int32
    g ?Grade
}
`,
		// §3.4: an optional carries a present byte, and its payload only when
		// present. Both states of a scalar and an enum, absent then present.
		probe: t11Prelude + `
func TestOptionalScalars(t *testing.T) {
	var v Root
	RootReset(&v)
	v.NPresent = false
	v.GPresent = false
	got := roundtrip(t, v)
	if got.NPresent || got.GPresent {
		t.Fatalf("absent: %+v", got)
	}
	v = Root{}
	RootReset(&v)
	v.NPresent = true
	v.N = -123456
	v.GPresent = true
	v.G = GradeSilver
	got = roundtrip(t, v)
	if !got.NPresent || got.N != -123456 || !got.GPresent || got.G != GradeSilver {
		t.Fatalf("present: %+v", got)
	}
}
`,
	},
	{
		id:        "optional-nesting/go/valid-data",
		tableOnly: true,
		schema: `package probe
type Vec
{
    x int32
    y int32
}
fixed table Root { p ?Vec }
`,
		// §3.4: an optional nested value is a present byte then the by-value
		// record at its bound. Absent and present at boundary values. The
		// spliced value struct is declared here because the packet half of a
		// `type` needs the absent sibling runtime.
		probe: t11Prelude + `
type Vec struct {
	X int32
	Y int32
}

func TestOptionalNesting(t *testing.T) {
	var v Root
	RootReset(&v)
	v.PPresent = false
	got := roundtrip(t, v)
	if got.PPresent {
		t.Fatalf("absent: %+v", got)
	}
	v = Root{}
	RootReset(&v)
	v.PPresent = true
	v.P.X = 99
	v.P.Y = -99
	got = roundtrip(t, v)
	if !got.PPresent || got.P.X != 99 || got.P.Y != -99 {
		t.Fatalf("present: %+v", got)
	}
}
`,
	},
	{
		id: "optional-arrays/go/valid-data",
		schema: `package probe
fixed table Root { xs ?[..3]int32 }
`,
		// §3.4: an optional array is a present byte then the bounded array's
		// count and elements. Absent and present at the full bound.
		probe: t11Prelude + `
func TestOptionalArrays(t *testing.T) {
	var v Root
	RootReset(&v)
	v.XsPresent = false
	got := roundtrip(t, v)
	if got.XsPresent {
		t.Fatalf("absent: %+v", got)
	}
	v = Root{}
	RootReset(&v)
	v.XsPresent = true
	v.XsCount = 3
	v.Xs[0], v.Xs[1], v.Xs[2] = 7, -8, 9
	got = roundtrip(t, v)
	if !got.XsPresent || got.XsCount != 3 || got.Xs[2] != 9 {
		t.Fatalf("present: %+v", got)
	}
}
`,
	},
	{
		id: "scalar-defaults/go/valid-data",
		schema: `package probe
enum Grade { Gold, Silver, Bronze }
fixed table Root
{
    x int32 = 7
    g Grade = Silver
    f float32 = 1.5
}
`,
		// §3.4 and docs/FIXED-FORM-ALGORITHM.md §3.1: Reset lands the declared
		// default, and a non-default value round-trips. Scalar, enum and float.
		probe: t11Prelude + `
func TestScalarDefaults(t *testing.T) {
	var v Root
	RootReset(&v)
	if v.X != 7 || v.G != GradeSilver || v.F != 1.5 {
		t.Fatalf("reset defaults: %+v", v)
	}
	v.X = -9
	v.G = GradeBronze
	v.F = -2.25
	got := roundtrip(t, v)
	if got.X != -9 || got.G != GradeBronze || got.F != -2.25 {
		t.Fatalf("roundtrip: %+v", got)
	}
}
`,
	},
	{
		id: "text-bytes-flags-defaults/go/valid-data",
		schema: `package probe
flags Caps { Jump, Fly, Swim }
fixed table Root
{
    s string(8) = "hi"
    b bytes(4) = "ab"
    c Caps = { Jump, Fly }
}
`,
		// §3.4 and §3.1: Reset lands the declared text, byte-buffer and flags
		// defaults; non-default values round-trip.
		probe: t11Prelude + `
func TestTextBytesFlagsDefaults(t *testing.T) {
	var v Root
	RootReset(&v)
	if string(v.S[:v.SLength]) != "hi" || string(v.B[:v.BLength]) != "ab" || v.C != CapsJump|CapsFly {
		t.Fatalf("reset defaults: %+v", v)
	}
	v.SLength = 3
	copy(v.S[:], "xyz")
	v.BLength = 4
	copy(v.B[:], "\x00\xff\x5a\x01")
	v.C = CapsSwim
	got := roundtrip(t, v)
	if string(got.S[:got.SLength]) != "xyz" || string(got.B[:got.BLength]) != "\x00\xff\x5a\x01" || got.C != CapsSwim {
		t.Fatalf("roundtrip: %+v", got)
	}
}
`,
	},
}
