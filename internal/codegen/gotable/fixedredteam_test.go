package gotable

// THE RED TEAM AGAINST §5, BY COMPOSITION AND BY HISTORY.
//
// docs/FIXED-FORM-VERSIONING-TESTS.md gives one row per definition change and
// proves each alone. This file attacks what the rows cannot: TWO OR MORE
// lawful widenings in ONE step, THREE generations at once, a widening on a
// table reached from two places, and the histories the lock records.
//
// The harness is self-contained — no C++ corpus. Stage one generates the OLD
// unit and WRITES a file with its own `FixedSave`, which is the lawful old
// file by construction; stage two generates the NEW unit with the older units'
// locked entries as its lineage and READS that file. A FINDING is a lawful old
// file refused, read with a wrong value, or read differently from the
// reference.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// redRuntime is the one thing this suite needs from outside the repository:
// the Go runtime the generated package replaces `serialize.go` with, as a
// SIBLING checkout — the same path `runVersionProbeRetired` compiles against.
// A tree without it cannot build a probe at all, so the suite says so and
// skips, the way the versioning suite skips a missing corpus; under
// SCHEMA_REQUIRE_CORPUS something promised the build and a skip would report
// green over a suite that never ran.
func redRuntime(t *testing.T) {
	t.Helper()
	path, err := filepath.Abs("../../../../serialize.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the Go runtime is not a sibling checkout at %s: %v", path, err)
		}
		t.Skipf("the Go runtime is not a sibling checkout at %s", path)
	}
}

// redWrite generates `schema` alone and runs `body` in its package, which must
// write the file at the path the body is handed as `out`. It returns the path.
func redWrite(t *testing.T, dir, name, schema, body string) string {
	t.Helper()
	redRuntime(t)
	out := filepath.Join(dir, name+".bin")
	src := fmt.Sprintf(`package probe

import ("os"; "testing")

func TestWrite(t *testing.T) {
	out := %q
	_ = out
%s
}
`, out, body)
	o, err := runVersionProbe(t, schema, nil, src)
	if err != nil {
		t.Fatalf("the OLD writer did not write its own file: %v\n%s", err, o)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("the OLD writer wrote nothing: %v", err)
	}
	return out
}

// redRead generates `schema` with `older`'s locked entries as its lineage and
// runs `body` in its package.
func redRead(t *testing.T, schema string, older []string, body string) {
	t.Helper()
	src := fmt.Sprintf(`package probe

import ("encoding/binary"; "os"; "testing")

var _ = binary.LittleEndian
var _ = os.ReadFile

func TestRead(t *testing.T) {
%s
}
`, body)
	o, err := runVersionProbe(t, schema, older, src)
	if err != nil {
		t.Fatalf("the NEW reader failed its read: %v\n%s", err, o)
	}
}

// ---- case 1: THREE lawful widenings in ONE step -----------------------------
//
// A field appended AND an enum grown AND an array bound grown, between one
// locked layout and the next. Every row of the versioning page proves one of
// these alone; a plan is built by ONE walk over both layouts, so three changes
// at once is the first thing that can put a destination offset in the wrong
// place.

const redComposeOld = `package redcompose

enum Tier { bronze, silver }

fixed table Lineage
{
    t Tier
    a [..2]int32
    x int32 = 0
}
`

const redComposeNew = `package redcomposenew

enum Tier { bronze, silver, gold }

fixed table Lineage
{
    t Tier
    a [..4]int32
    x int32 = 0
    w int32 = 77
}
`

func TestFixedRedTeamComposedWidenings(t *testing.T) {
	dir := t.TempDir()
	file := redWrite(t, dir, "compose", redComposeOld, `
	var v Lineage
	LineageReset(&v)
	v.T = Tiersilver
	v.A[0], v.A[1] = 5, 6
	v.ACount = 2
	v.X = 9
	buf := make([]byte, LineageFixedMeasure(1))
	if LineageFixedSave([]Lineage{v}, buf) < 0 {
		t.Fatal("save")
	}
	if err := os.WriteFile(out, buf, 0o600); err != nil {
		t.Fatal(err)
	}`)
	redRead(t, redComposeNew, []string{redComposeOld}, fmt.Sprintf(`
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]Lineage, 4)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport
	n := LineageFixedLoad(back, data, plan, &r)
	if n != 1 || r.Reason != "" || r.Malformed {
		t.Fatalf("three lawful widenings at once and the new reader refused: n=%%d %%+v", n, r)
	}
	if back[0].T != Tiersilver {
		t.Fatalf("the grown enum lost the old ordinal: %%+v", back[0])
	}
	if back[0].ACount != 2 || back[0].A[0] != 5 || back[0].A[1] != 6 {
		t.Fatalf("the grown array lost the old elements: %%+v", back[0])
	}
	if back[0].A[2] != 0 || back[0].A[3] != 0 {
		t.Fatalf("the grown array's new slots are not their default: %%+v", back[0])
	}
	if back[0].X != 9 {
		t.Fatalf("a field after the grown array moved: %%+v", back[0])
	}
	if back[0].W != 77 {
		t.Fatalf("the appended field is not its declared default: %%+v", back[0])
	}
	if r.Widened != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 {
		t.Fatalf("three appends are not events: %%+v", r)
	}`, file))
}

// ---- case 2: a widened table reached from TWO places at once ----------------
//
// `Vec` is both a field and an ARRAY ELEMENT of the same root. One widening
// moves BOTH of the outer's layouts: the field's own bytes and the array's
// stride. The versioning page's `nested_append` row proves the field; nothing
// proves the element, whose destination stride is the READER's and whose source
// stride is the WRITER's.

const redTwoPlacesOld = `package redtwoplaces

fixed table Vec
{
    x int32 = 0
    y int32 = 0
}

fixed table Lineage
{
    v   Vec
    vs  [..2]Vec
    seq int32 = 0
}
`

const redTwoPlacesNew = `package redtwoplacesnew

fixed table Vec
{
    x int32 = 0
    y int32 = 0
    z int32 = 99
}

fixed table Lineage
{
    v   Vec
    vs  [..2]Vec
    seq int32 = 0
}
`

func TestFixedRedTeamWidenedTableInTwoPlaces(t *testing.T) {
	dir := t.TempDir()
	file := redWrite(t, dir, "twoplaces", redTwoPlacesOld, `
	var v Lineage
	LineageReset(&v)
	v.V.X, v.V.Y = 1, 2
	v.Vs[0].X, v.Vs[0].Y = 3, 4
	v.Vs[1].X, v.Vs[1].Y = 5, 6
	v.VsCount = 2
	v.Seq = 7
	buf := make([]byte, LineageFixedMeasure(1))
	if LineageFixedSave([]Lineage{v}, buf) < 0 {
		t.Fatal("save")
	}
	if err := os.WriteFile(out, buf, 0o600); err != nil {
		t.Fatal(err)
	}`)
	redRead(t, redTwoPlacesNew, []string{redTwoPlacesOld}, fmt.Sprintf(`
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]Lineage, 4)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport
	n := LineageFixedLoad(back, data, plan, &r)
	if n != 1 || r.Reason != "" || r.Malformed {
		t.Fatalf("the widened element's reader refused a lawful old file: n=%%d %%+v", n, r)
	}
	g := back[0]
	if g.V.X != 1 || g.V.Y != 2 || g.V.Z != 99 {
		t.Fatalf("the FIELD place: %%+v", g.V)
	}
	if g.VsCount != 2 {
		t.Fatalf("the array count: %%+v", g)
	}
	if g.Vs[0].X != 3 || g.Vs[0].Y != 4 || g.Vs[0].Z != 99 {
		t.Fatalf("element 0 under the grown stride: %%+v", g.Vs[0])
	}
	if g.Vs[1].X != 5 || g.Vs[1].Y != 6 || g.Vs[1].Z != 99 {
		t.Fatalf("element 1 under the grown stride: %%+v", g.Vs[1])
	}
	if g.Seq != 7 {
		t.Fatalf("a field after the grown array moved: %%+v", g)
	}
	if r.Widened != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 {
		t.Fatalf("an append is not an event: %%+v", r)
	}`, file))
}

// ---- case 3: an appended union arm whose payload is the WIDEST --------------
//
// The tag sits AFTER the widest arm, so an arm bigger than every existing one
// MOVES THE TAG — in the reader's storage and in the lock's layout bytes both.
// The guard is read at THE WRITER'S tag offset and the `const` lands at the
// READER'S; the `union_append` row appends an arm that fits, so nothing yet
// proves the two offsets stay apart.

const redUnionGrowOld = `package reduniongrow

type A { p int32 = 0 }
type B { q int32 = 0 }

union Pick
{
    a A
    b B
}

fixed table Lineage
{
    pick Pick
    seq  int32 = 0
}
`

const redUnionGrowNew = `package reduniongrownew

type A { p int32 = 0 }
type B { q int32 = 0 }
type C
{
    r int64 = 0
    s int64 = 0
    u int64 = 0
}

union Pick
{
    a A
    b B
    c C
}

fixed table Lineage
{
    pick Pick
    seq  int32 = 0
}
`

func TestFixedRedTeamUnionArmWidensTheSlot(t *testing.T) {
	dir := t.TempDir()
	file := redWrite(t, dir, "uniongrow", redUnionGrowOld, `
	var v Lineage
	LineageReset(&v)
	v.Pick.Type = PickTypeB
	v.Pick.B.Q = 42
	v.Seq = 11
	var none Lineage
	LineageReset(&none)
	none.Seq = 12
	buf := make([]byte, LineageFixedMeasure(2))
	if LineageFixedSave([]Lineage{v, none}, buf) < 0 {
		t.Fatal("save")
	}
	if err := os.WriteFile(out, buf, 0o600); err != nil {
		t.Fatal(err)
	}`)
	redRead(t, redUnionGrowNew, []string{redUnionGrowOld}, fmt.Sprintf(`
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]Lineage, 4)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport
	n := LineageFixedLoad(back, data, plan, &r)
	if n != 2 || r.Reason != "" || r.Malformed {
		t.Fatalf("the grown union slot refused a lawful old file: n=%%d %%+v", n, r)
	}
	if back[0].Pick.Type != PickTypeB {
		t.Fatalf("the old tag did not land under the MOVED tag offset: %%+v", back[0].Pick)
	}
	if back[0].Pick.B.Q != 42 {
		t.Fatalf("the old arm's payload: %%+v", back[0].Pick)
	}
	if back[0].Seq != 11 {
		t.Fatalf("the field after the grown union moved: %%+v", back[0])
	}
	// The UNGUARDED None stands when no arm matches, at the MOVED tag offset.
	if back[1].Pick.Type != PickTypeNone || back[1].Seq != 12 {
		t.Fatalf("the None tag did not land under the moved tag offset: %%+v", back[1])
	}
	if r.Widened != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 {
		t.Fatalf("an arm append is not an event: %%+v", r)
	}`, file))
}

// ---- case 4: a `was =` rename COMPOSED with an append -----------------------
//
// A rename through `was` alone moves no layout byte, so the pair takes the
// IDENTITY plan and the compiled plan's id match is never exercised on a
// renamed field. Compose it with an append and the hash moves: now the plan
// must match the renamed reader field to the writer's field BY THE OLD NAME.

const redRenameOld = `package redrename

fixed table Lineage
{
    a   int32 = 0
    seq int32 = 0
}
`

const redRenameNew = `package redrenamenew

fixed table Lineage
{
    b   int32 = 0 | was = "a"
    seq int32 = 0
    w   int32 = 77
}
`

func TestFixedRedTeamRenameComposedWithAppend(t *testing.T) {
	dir := t.TempDir()
	file := redWrite(t, dir, "rename", redRenameOld, `
	var v Lineage
	LineageReset(&v)
	v.A = 31
	v.Seq = 32
	buf := make([]byte, LineageFixedMeasure(1))
	if LineageFixedSave([]Lineage{v}, buf) < 0 {
		t.Fatal("save")
	}
	if err := os.WriteFile(out, buf, 0o600); err != nil {
		t.Fatal(err)
	}`)
	redRead(t, redRenameNew, []string{redRenameOld}, fmt.Sprintf(`
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]Lineage, 4)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport
	n := LineageFixedLoad(back, data, plan, &r)
	if n != 1 || r.Reason != "" || r.Malformed {
		t.Fatalf("rename plus append refused a lawful old file: n=%%d %%+v", n, r)
	}
	if back[0].B != 31 {
		t.Fatalf("the renamed field did not match by the OLD name: %%+v", back[0])
	}
	if back[0].Seq != 32 || back[0].W != 77 {
		t.Fatalf("%%+v", back[0])
	}
	if r.Unknown != 0 {
		t.Fatalf("a renamed field is not an unknown field: %%+v", r)
	}`, file))
}

// ---- case 5: THREE generations, and the two-step skip ------------------------
//
// V1 -> V2 appends `b`, V2 -> V3 appends `c`. V3's plan for V1 is built from
// V1's locked layout bytes against V3's own — V2 is never in between — so the
// V1 file must land `a` exactly and BOTH tails at their declared defaults. The
// destinations are POISONED first: a prefill that does not run shows here.

const redGenV1 = `package redgen1

fixed table Lineage
{
    a int32 = 0
}
`

const redGenV2 = `package redgen2

fixed table Lineage
{
    a int32 = 0
    b int32 = 22
}
`

const redGenV3 = `package redgen3

fixed table Lineage
{
    a int32 = 0
    b int32 = 22
    c int32 = 33
}
`

func TestFixedRedTeamThreeGenerations(t *testing.T) {
	dir := t.TempDir()
	v1 := redWrite(t, dir, "gen1", redGenV1, `
	var v Lineage
	LineageReset(&v)
	v.A = 1
	buf := make([]byte, LineageFixedMeasure(1))
	if LineageFixedSave([]Lineage{v}, buf) < 0 {
		t.Fatal("save")
	}
	if err := os.WriteFile(out, buf, 0o600); err != nil {
		t.Fatal(err)
	}`)
	v2 := redWrite(t, dir, "gen2", redGenV2, `
	var v Lineage
	LineageReset(&v)
	v.A, v.B = 2, 99
	buf := make([]byte, LineageFixedMeasure(1))
	if LineageFixedSave([]Lineage{v}, buf) < 0 {
		t.Fatal("save")
	}
	if err := os.WriteFile(out, buf, 0o600); err != nil {
		t.Fatal(err)
	}`)

	// Nothing retired: V3 reads V1 (the two-step skip) and V2.
	redRead(t, redGenV3, []string{redGenV1, redGenV2}, fmt.Sprintf(`
	poison := func(back []Lineage) {
		for k := range back {
			back[k] = Lineage{A: 0x5A5A5A5A, B: 0x5A5A5A5A, C: 0x5A5A5A5A}
		}
	}
	one, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	two, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]Lineage, 4)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport

	poison(back)
	if n := LineageFixedLoad(back, one, plan, &r); n != 1 || r.Reason != "" {
		t.Fatalf("V3 refused V1, two generations back: n=%%d %%+v", n, r)
	}
	if back[0].A != 1 || back[0].B != 22 || back[0].C != 33 {
		t.Fatalf("the two-step skip did not prefill both tails: %%+v", back[0])
	}

	poison(back)
	r = TableReport{}
	if n := LineageFixedLoad(back, two, plan, &r); n != 1 || r.Reason != "" {
		t.Fatalf("V3 refused V2: n=%%d %%+v", n, r)
	}
	if back[0].A != 2 || back[0].B != 99 || back[0].C != 33 {
		t.Fatalf("V2's own values plus ONE tail: %%+v", back[0])
	}`, v1, v2))

	// V1 retired: the floor is 1. V1 is layout_unsupported BY NAME and V2, at
	// the raised floor, still reads.
	src := fmt.Sprintf(`package probe

import ("os"; "testing")

func TestFloorRaised(t *testing.T) {
	one, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	two, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]Lineage, 4)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport
	if n := LineageFixedLoad(back, one, plan, &r); n != -1 || r.Reason != "layout_unsupported" {
		t.Fatalf("a retired generation owes layout_unsupported: n=%%d %%+v", n, r)
	}
	if r.LayoutHash == 0 {
		t.Fatal("layout_unsupported reports THE FILE'S hash")
	}
	if r.Malformed || r.Widened != 0 || r.Unknown != 0 || r.Clamped != 0 {
		t.Fatalf("REFUSE is total: %%+v", r)
	}
	r = TableReport{}
	if n := LineageFixedLoad(back, two, plan, &r); n != 1 || r.Reason != "" {
		t.Fatalf("the generation AT the raised floor must still read: n=%%d %%+v", n, r)
	}
	if back[0].A != 2 || back[0].B != 99 || back[0].C != 33 {
		t.Fatalf("%%+v", back[0])
	}
}
`, v1, v2)
	if out, err := runVersionProbeRetired(t, redGenV3, []string{redGenV1, redGenV2}, 1, src); err != nil {
		t.Fatalf("the raised floor: %v\n%s", err, out)
	}
}

// ---- case 6: ONE nested table, TWO fixed roots, ONE schema file -------------
//
// One lock, two lineages. `Vec` widens once and BOTH roots' layouts move; each
// root owes its OWN plan for its own older entry, and a plan shared by name
// would land the second root's values at the first root's offsets.

const redTwoRootsOld = `package redtworoots

fixed table Vec
{
    x int32 = 0
}

fixed table Alpha
{
    v   Vec
    seq int32 = 0
}

fixed table Beta
{
    tag int32 = 0
    v   Vec
}
`

const redTwoRootsNew = `package redtworootsnew

fixed table Vec
{
    x int32 = 0
    y int32 = 44
}

fixed table Alpha
{
    v   Vec
    seq int32 = 0
}

fixed table Beta
{
    tag int32 = 0
    v   Vec
}
`

func TestFixedRedTeamTwoRootsShareANestedTable(t *testing.T) {
	dir := t.TempDir()
	files := redWrite(t, dir, "tworoots", redTwoRootsOld, `
	var a Alpha
	AlphaReset(&a)
	a.V.X, a.Seq = 7, 8
	ab := make([]byte, AlphaFixedMeasure(1))
	if AlphaFixedSave([]Alpha{a}, ab) < 0 {
		t.Fatal("save alpha")
	}
	if err := os.WriteFile(out, ab, 0o600); err != nil {
		t.Fatal(err)
	}
	var b Beta
	BetaReset(&b)
	b.Tag, b.V.X = 9, 10
	bb := make([]byte, BetaFixedMeasure(1))
	if BetaFixedSave([]Beta{b}, bb) < 0 {
		t.Fatal("save beta")
	}
	if err := os.WriteFile(out+".beta", bb, 0o600); err != nil {
		t.Fatal(err)
	}`)
	redRead(t, redTwoRootsNew, []string{redTwoRootsOld}, fmt.Sprintf(`
	ab, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	bb, err := os.ReadFile(%[1]q + ".beta")
	if err != nil {
		t.Fatal(err)
	}
	plan := make([]TableFixedEntry, 4096)
	var r TableReport
	as := make([]Alpha, 2)
	if n := AlphaFixedLoad(as, ab, plan, &r); n != 1 || r.Reason != "" {
		t.Fatalf("the first root refused its own old file: n=%%d %%+v", n, r)
	}
	if as[0].V.X != 7 || as[0].V.Y != 44 || as[0].Seq != 8 {
		t.Fatalf("the first root: %%+v", as[0])
	}
	r = TableReport{}
	bs := make([]Beta, 2)
	if n := BetaFixedLoad(bs, bb, plan, &r); n != 1 || r.Reason != "" {
		t.Fatalf("the SECOND root refused its own old file: n=%%d %%+v", n, r)
	}
	if bs[0].Tag != 9 || bs[0].V.X != 10 || bs[0].V.Y != 44 {
		t.Fatalf("the second root: %%+v", bs[0])
	}`, files))
}

// ---- case 7: a keyed array's KEY grows and its ELEMENT widens, at once ------
//
// `[Tier]Qty` sizes itself from the enum, so appending a variant GROWS THE SLOT
// RUN, and widening `Qty`'s field grows the STRIDE. Both at once: the old
// file's three slots must land under the new stride and the fourth slot must be
// the element's own default.

const redKeyedOld = `package redkeyed

enum Tier { bronze, silver, gold }

fixed table Qty
{
    n int16 = 7
}

fixed table Lineage
{
    slots [Tier]Qty
    seq   int32 = 0
}
`

const redKeyedNew = `package redkeyednew

enum Tier { bronze, silver, gold, platinum }

fixed table Qty
{
    n int32 = 7
}

fixed table Lineage
{
    slots [Tier]Qty
    seq   int32 = 0
}
`

func TestFixedRedTeamKeyedKeyGrowsAndElementWidens(t *testing.T) {
	dir := t.TempDir()
	file := redWrite(t, dir, "keyed", redKeyedOld, `
	var v Lineage
	LineageReset(&v)
	v.Slots[0].N = 11
	v.Slots[1].N = 22
	v.Slots[2].N = 33
	v.Seq = 44
	buf := make([]byte, LineageFixedMeasure(1))
	if LineageFixedSave([]Lineage{v}, buf) < 0 {
		t.Fatal("save")
	}
	if err := os.WriteFile(out, buf, 0o600); err != nil {
		t.Fatal(err)
	}`)
	redRead(t, redKeyedNew, []string{redKeyedOld}, fmt.Sprintf(`
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]Lineage, 2)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport
	n := LineageFixedLoad(back, data, plan, &r)
	if n != 1 || r.Reason != "" || r.Malformed {
		t.Fatalf("the grown keyed array refused a lawful old file: n=%%d %%+v", n, r)
	}
	g := back[0]
	if g.Slots[0].N != 11 || g.Slots[1].N != 22 || g.Slots[2].N != 33 {
		t.Fatalf("the old slots did not land under the grown stride: %%+v", g.Slots)
	}
	if g.Slots[3].N != 7 {
		t.Fatalf("the APPENDED slot is not the element's declared default: %%+v", g.Slots)
	}
	if g.Seq != 44 {
		t.Fatalf("the field after the grown slot run moved: %%+v", g)
	}
	if r.Widened != 3 {
		t.Fatalf("three widened elements, not %%d: %%+v", r.Widened, r)
	}
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 {
		t.Fatalf("%%+v", r)
	}`, file))
}

// ---- case 8: `T` becomes `?T` AND the payload gains a field ------------------
//
// The optional wrapper lands a CONSTANT `1` at the present byte and then
// recurses into the payload (bill §12.8). Composed with an append inside the
// payload, the recursion carries the wrapper's guard AND the payload's own
// prefill: `optional_add` alone never has a tail to prefill.

const redOptOld = `package redopt

fixed table Vec
{
    x int32 = 0
}

fixed table Lineage
{
    v   Vec
    seq int32 = 0
}
`

const redOptNew = `package redoptnew

fixed table Vec
{
    x int32 = 0
    y int32 = 55
}

fixed table Lineage
{
    v   ?Vec
    seq int32 = 0
}
`

func TestFixedRedTeamOptionalAddedAndPayloadAppended(t *testing.T) {
	dir := t.TempDir()
	file := redWrite(t, dir, "opt", redOptOld, `
	var v Lineage
	LineageReset(&v)
	v.V.X = 13
	v.Seq = 14
	buf := make([]byte, LineageFixedMeasure(1))
	if LineageFixedSave([]Lineage{v}, buf) < 0 {
		t.Fatal("save")
	}
	if err := os.WriteFile(out, buf, 0o600); err != nil {
		t.Fatal(err)
	}`)
	redRead(t, redOptNew, []string{redOptOld}, fmt.Sprintf(`
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]Lineage, 2)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport
	n := LineageFixedLoad(back, data, plan, &r)
	if n != 1 || r.Reason != "" || r.Malformed {
		t.Fatalf("T into ?T with an appended payload field refused: n=%%d %%+v", n, r)
	}
	if !back[0].VPresent {
		t.Fatalf("T into ?T lands present: %%+v", back[0])
	}
	if back[0].V.X != 13 {
		t.Fatalf("the payload's old value: %%+v", back[0].V)
	}
	if back[0].V.Y != 55 {
		t.Fatalf("the payload's APPENDED field is not its declared default: %%+v", back[0].V)
	}
	if back[0].Seq != 14 {
		t.Fatalf("the field after the wrapper moved: %%+v", back[0])
	}`, file))
}

// ---- case 9: a string's capacity grows, a field FOLLOWS it, and one is
// appended ---------------------------------------------------------------------
//
// The text op writes the length at `dst` and the WHOLE SPAN at `aux`, and the
// span it copies is the WRITER's. Grow the capacity, keep a field after the
// string, and append one: the reader's slack past the writer's span must stay
// the prefill's and the following field must not move.

const redTextOld = `package redtext

fixed table Lineage
{
    s   string(8)
    seq int32 = 0
}
`

const redTextNew = `package redtextnew

fixed table Lineage
{
    s   string(16)
    seq int32 = 0
    w   int32 = 77
}
`

func TestFixedRedTeamStringGrowsWithAFollowingField(t *testing.T) {
	dir := t.TempDir()
	file := redWrite(t, dir, "text", redTextOld, `
	var v Lineage
	LineageReset(&v)
	copy(v.S[:], "abcd")
	v.SLength = 4
	v.Seq = 21
	buf := make([]byte, LineageFixedMeasure(1))
	if LineageFixedSave([]Lineage{v}, buf) < 0 {
		t.Fatal("save")
	}
	if err := os.WriteFile(out, buf, 0o600); err != nil {
		t.Fatal(err)
	}`)
	redRead(t, redTextNew, []string{redTextOld}, fmt.Sprintf(`
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]Lineage, 2)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport
	n := LineageFixedLoad(back, data, plan, &r)
	if n != 1 || r.Reason != "" || r.Malformed {
		t.Fatalf("a grown string with a field behind it refused: n=%%d %%+v", n, r)
	}
	if back[0].SLength != 4 || string(back[0].S[:4]) != "abcd" {
		t.Fatalf("the old text: %%+v", back[0])
	}
	for i := 4; i < len(back[0].S); i++ {
		if back[0].S[i] != 0 {
			t.Fatalf("the grown capacity's slack is not zero at %%d: %%+v", i, back[0])
		}
	}
	if back[0].Seq != 21 {
		t.Fatalf("the field after the grown string moved: %%+v", back[0])
	}
	if back[0].W != 77 {
		t.Fatalf("the appended field is not its default: %%+v", back[0])
	}
	if r.Clamped != 0 {
		t.Fatalf("a grown capacity clamps nothing: %%+v", r)
	}`, file))
}

// ---- case 10: the ONLY field of a table widens ------------------------------
//
// No neighbour, and the record body size changes by the widening alone. The
// plan's single entry is the whole record.

const redOnlyOld = `package redonly

fixed table Lineage
{
    a int16 = 0
}
`

const redOnlyNew = `package redonlynew

fixed table Lineage
{
    a int32 = 0
}
`

func TestFixedRedTeamOnlyFieldWidens(t *testing.T) {
	dir := t.TempDir()
	file := redWrite(t, dir, "only", redOnlyOld, `
	vals := []Lineage{{A: -1}, {A: -32768}, {A: 32767}}
	buf := make([]byte, LineageFixedMeasure(int64(len(vals))))
	if LineageFixedSave(vals, buf) < 0 {
		t.Fatal("save")
	}
	if err := os.WriteFile(out, buf, 0o600); err != nil {
		t.Fatal(err)
	}`)
	redRead(t, redOnlyNew, []string{redOnlyOld}, fmt.Sprintf(`
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]Lineage, 8)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport
	n := LineageFixedLoad(back, data, plan, &r)
	if n != 3 || r.Reason != "" || r.Malformed {
		t.Fatalf("a table whose only field widened refused: n=%%d %%+v", n, r)
	}
	want := []int32{-1, -32768, 32767}
	for k := range want {
		if back[k].A != want[k] {
			t.Fatalf("record %%d sign-extended wrong: %%+v", k, back[k])
		}
	}
	if r.Widened != 3 {
		t.Fatalf("one widen per record: %%+v", r)
	}`, file))
}

// ---- case 7: THE MERGE COMMIT — both parents' files on the merged build ------
//
// docs/FIXED-FORM-VERSIONING-TESTS.md's `lineage_merge`, on the Go leg and with
// no C++ corpus: two branches append DIFFERENT fields, the merge takes both, and
// BOTH pre-merge writers' files read exactly on the merged reader.
//
// It is the read half of the two-parent lock run (internal/lockfile/merge.go,
// bill §8a.1 and §11.3). The lock run is where the merge is JUDGED; this is why
// the judgement is safe to make: the merged reader pairs a parent's layout to its
// own BY WIRE ID (algorithm §5.9 #41), so neither branch's field order has to be
// a prefix of the merged one and the merged record's new fields may sit anywhere.
// Nothing on the read path knows a merge happened — which is the ruling: the
// name-subset clause is a lock run, never a run-time rule.
//
// The interleaving is DELIBERATELY NOT either parent's: the merge puts B's `y`
// BEFORE A's `x`, so A's own last field is not in A's position in the merged
// record. If anything in the read paired by position, A's file would land `y`'s
// default in `x`.

const redMergeBase = `package redmergebase

fixed table Lineage
{
    a int32 = 0
    b int32 = 0
}
`

const redMergeA = `package redmergea

fixed table Lineage
{
    a int32 = 0
    b int32 = 0
    x int32 = 7
}
`

const redMergeB = `package redmergeb

fixed table Lineage
{
    a int32 = 0
    b int32 = 0
    y int32 = 9
}
`

const redMergeBoth = `package redmergeboth

fixed table Lineage
{
    a int32 = 0
    b int32 = 0
    y int32 = 9
    x int32 = 7
}
`

func TestFixedRedTeamMergeReadsBothParents(t *testing.T) {
	dir := t.TempDir()
	write := func(name, schema string, field string, value int) string {
		return redWrite(t, dir, name, schema, fmt.Sprintf(`
	var v Lineage
	LineageReset(&v)
	v.A, v.B = 11, 22
	v.%s = %d
	buf := make([]byte, LineageFixedMeasure(1))
	if LineageFixedSave([]Lineage{v}, buf) < 0 {
		t.Fatal("save")
	}
	if err := os.WriteFile(out, buf, 0o600); err != nil {
		t.Fatal(err)
	}`, field, value))
	}
	fromA := write("mergeA", redMergeA, "X", 101)
	fromB := write("mergeB", redMergeB, "Y", 202)

	// THE LINEAGE THE MERGED LOCK WROTE, in the order merge.go composes it: the
	// shared base, then A's layout, then B's, then the merge's own.
	redRead(t, redMergeBoth, []string{redMergeBase, redMergeA, redMergeB}, fmt.Sprintf(`
	for _, c := range []struct{ path string; x, y int32 }{
		{%q, 101, 9},
		{%q, 7, 202},
	} {
		data, err := os.ReadFile(c.path)
		if err != nil {
			t.Fatal(err)
		}
		back := make([]Lineage, 4)
		plan := make([]TableFixedEntry, 4096)
		var r TableReport
		n := LineageFixedLoad(back, data, plan, &r)
		if n != 1 || r.Reason != "" || r.Malformed {
			t.Fatalf("%%s: a parent of the merge wrote it and the merged build refused: n=%%d %%+v", c.path, n, r)
		}
		g := back[0]
		if g.A != 11 || g.B != 22 {
			t.Fatalf("%%s: the shared fields did not land: %%+v", c.path, g)
		}
		if g.X != c.x {
			t.Fatalf("%%s: x is %%d, want %%d — the other branch's field is its DECLARED DEFAULT and a branch's own field is EXACT: %%+v", c.path, g.X, c.x, g)
		}
		if g.Y != c.y {
			t.Fatalf("%%s: y is %%d, want %%d: %%+v", c.path, g.Y, c.y, g)
		}
		if r.Widened != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 {
			t.Fatalf("%%s: an append on either branch is not an event: %%+v", c.path, r)
		}
	}`, fromA, fromB))
}
