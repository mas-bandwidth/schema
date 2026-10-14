package gotable

// THE ROADMAP'S UNION-GUARDS ROW ON THE GO LEG (docs/roadmap.sexp node
// `fixed-tables`, ROADMAP.md "NEW Fixed Tables"). Law: docs/FIXED-FORM-ALGORITHM.md
// §4.1 (nested union tags are CONJOINED, never overwritten) and §5.2 (Arg and
// Meta are two lanes), and docs/SPEC-TABLES.md §3.4. One subtest per task id this
// card proves, table-driven, t.Parallel() first. Every assertion reads the plan
// THIS leg's emitter lays down — the GENERATED TFixedLeaves source — and not the
// canonical ir.TableFixedBuildPlan, so a planted break in the emitter turns it
// red. W16 goes further: it also RUNS the emitted plan through the leg's own
// generated TFixedSave/TFixedLoad (t09Run), so the two-lane conjunction the
// runtime's read loop holds is exercised and a dropped Guard2 check turns it red,
// which a source-text assertion alone cannot do.

import (
	"strings"
	"testing"
)

func TestFixedRoadmapGoT08WritesConformance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"go/W7", t08GoW7},
		{"go/W16", t08GoW16},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t08GoSource is the whole emitted package for a fixture: the walk this leg's
// emitter lays down, as Go source. A planted edit to the emitter's plan walk
// changes this text and turns every clause below red.
func t08GoSource(t *testing.T, schema string) string {
	t.Helper()
	out := generate(t, schema)
	var b strings.Builder
	for _, data := range out {
		b.Write(data)
	}
	if !strings.Contains(b.String(), "FixedLeaves") {
		t.Fatal("the emitter laid down no fixed leaf walk")
	}
	return b.String()
}

// ---- go/W7 ------------------------------------------------------------------

// t08GoW7 holds W7's clause: "arg and meta are two lanes". §5.2 EMIT and the
// TableFixedEntry doc: Meta is THE OP'S OWN ARGUMENT, on the ops that have one —
// a text entry's flavour today — and it has its own lane because Arg's is the
// guard's. A text field under union arm 2 must carry the guard's ordinal in Arg
// AND the text flavour in Meta, on two fields that do not collide.
func t08GoW7(t *testing.T) {
	src := t08GoSource(t, `package probe

type ArmA { n int32 }
type ArmB { s string(8) }
union Pick
{
    a ArmA
    b ArmB
}
fixed table T
{
    pick Pick
}
`)
	if !strings.Contains(src, "Op: tableFixedText, Meta: 1") {
		t.Error("W7: the text entry's Meta lane (the text flavour) is missing from the emitted plan")
	}
	// The arm's guard is stamped as a link of the entry's guard chain
	// (FIXED-FORM-ALGORITHM §4.1), which tableFixedBuildPlan lays on the Arg lane.
	if !strings.Contains(src, "Arg: 2, ArgW: ") {
		t.Error("W7: the guard lane's Arg is not stamped with the arm ordinal 2 in the emitted plan")
	}
	// TWO LANES, NOT ONE: the flavour rides Meta on the entry literal and the
	// ordinal rides the arm's guard stamp. If the emitter folded Meta into Arg
	// the literal would lose Meta, and a break in either turns this red.
	if strings.Contains(src, "Meta: 0, Op: tableFixedText") {
		t.Error("W7: the text entry carries no Meta lane")
	}
}

// ---- go/W16 and go/R28 ------------------------------------------------------

// t08Nested is an arm inside an arm: Outer's arm p holds Inner, so every entry
// inside Inner's arm answers to the OUTER tag as well as its own. Inner is also
// a table field of T on its own, so this leg's table emitter lays down Inner's
// and Outer's own type declarations — the generated fixed unit compiles without
// the message-form emitter (the one that reads through the sibling
// serialize.go runtime), which is what lets the conjunction be RUN here.
const t08Nested = `package probe

union Inner
{
    x int32
    y int32
}
union Outer
{
    p Inner
    q int32
}
fixed table T
{
    pick Outer
    other Inner
}
`

// t08GoW16 holds W16's clause: "nested union answers outer tag". §4.1 and the
// TableFixedEntry doc: an arm inside an arm answers to the outer tag too, on a
// SECOND lane, so the entry runs only when BOTH tags hold their ordinal. The
// emitted walk must stamp the outer union's tag on Guard2/Arg2/ArgW2 for the
// entries an inner union already guarded with its own tag.
func t08GoW16(t *testing.T) {
	src := t08GoSource(t, t08Nested)
	// THE OUTER TAG LANDS BESIDE THE INNER, NEVER OVER IT: the emitted walk
	// records ONE MORE LINK of the entry's guard chain per enclosing union
	// (§4.1, the guard chain), and the plan builder lays the chain's innermost
	// two on the Guard and Guard2 lanes.
	if !strings.Contains(src, "links[q] = append(links[q], TableFixedGuardLink{Guard: ") {
		t.Error("W16: the emitted walk does not record the outer tag as a link of the entry's guard chain")
	}
	if !strings.Contains(src, "e.Guard2, e.Arg2, e.ArgW2 = chain[k-2].Guard, chain[k-2].Arg, chain[k-2].ArgW") {
		t.Error("W16: the plan builder does not lay the outer tag on the second lane; an arm inside an arm answers the outer tag on a second lane")
	}
	// AND THE CONJUNCTION IS EXERCISED, not merely laid down: the emitted
	// plan's two lanes are RUN — the leg builds, WRITES and READS a nested
	// union (an outer arm holding an inner union) through its own generated
	// TFixedSave/TFixedLoad. This is the clause the source text cannot prove:
	// §4.1's "the entry runs when BOTH tags hold their ordinal"
	// (docs/FIXED-FORM-ALGORITHM.md:246-251). t09Run is this package's existing
	// fixed-form harness: it generates the table unit and runs it in a
	// throwaway module (it shells out, so it runs where the gate promises).
	t09Run(t, t08Nested, t08NestedProbe)
}

// t08NestedProbe builds, writes and reads the nested fixture end-to-end and
// bites the second lane: (a) when both the outer tag and the inner tag hold,
// the inner arm lands; (b) when only the inner tag holds — the outer tag names
// a different arm — the inner arm's bytes must NOT land beneath an outer arm
// that never rode, which is the bug §4.1 describes. A plan whose reader drops
// the Guard2 check lands the bytes in (b) and this test goes red.
const t08NestedProbe = `package probe

import "testing"

func TestOuterTagConjunction(t *testing.T) {
	// (a) BOTH TAGS HOLD: the outer tag says p and the inner tag says x, so the
	// x arm's entry runs and its bytes land (§4.1).
	v := T{}
	TReset(&v)
	v.Pick.Type = OuterTypeP
	v.Pick.P.Type = InnerTypeX
	v.Pick.P.X = 0x2a
	buf := make([]byte, TFixedMeasure(1))
	if n := TFixedSave([]T{v}, buf); n != int64(len(buf)) {
		t.Fatalf("save %d want %d", n, len(buf))
	}
	plan := make([]TableFixedEntry, 256)
	var r TableReport
	both := make([]T, 1)
	if n := TFixedLoad(both, buf, plan, &r); n != 1 || r != (TableReport{}) {
		t.Fatalf("load %d report %+v", n, r)
	}
	if both[0].Pick.Type != OuterTypeP || both[0].Pick.P.Type != InnerTypeX || both[0].Pick.P.X != 0x2a {
		t.Fatalf("both tags held but the inner arm did not land: %+v", both[0].Pick)
	}

	// (b) ONLY THE INNER TAG HOLDS: the record's outer tag says q while its
	// inner tag byte still says x. The x entry must not run, so no inner-arm
	// bytes land beneath outer arm p, which never rode (§4.1).
	body := buf[len(buf)-TFixedRecordBytes:]
	for i := range body[8:] {
		body[8+i] = 0
	}
	body[8+0] = byte(OuterTypeQ)
	body[8+1] = byte(InnerTypeX)
	body[8+2] = 0xde
	body[8+3] = 0xad
	body[8+4] = 0xbe
	body[8+5] = 0xef
	only := make([]T, 1)
	r = TableReport{}
	if n := TFixedLoad(only, buf, plan, &r); n != 1 {
		t.Fatalf("load %d report %+v", n, r)
	}
	if only[0].Pick.P.X != 0 {
		t.Fatalf("the inner tag alone landed an inner arm's bytes beneath an outer arm that never rode: %+v", only[0].Pick)
	}
	if only[0].Pick.Type != OuterTypeQ || only[0].Pick.Q == 0 {
		t.Fatalf("the outer q arm did not land: %+v", only[0].Pick)
	}
}
`
