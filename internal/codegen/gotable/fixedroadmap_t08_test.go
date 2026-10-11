package gotable

// THE ROADMAP'S UNION-GUARDS ROW ON THE GO LEG (docs/roadmap.sexp node
// `fixed-tables`, ROADMAP.md "NEW Fixed Tables"). Law: docs/FIXED-FORM-ALGORITHM.md
// §4.1 (nested union tags are CONJOINED, never overwritten) and §5.2 (Arg and
// Meta are two lanes), and docs/SPEC-TABLES.md §3.4. One subtest per task id this
// card proves, table-driven, t.Parallel() first. Every assertion reads the plan
// THIS leg's emitter lays down — the GENERATED TFixedLeaves source — and not the
// canonical ir.TableFixedBuildPlan, so a planted break in the emitter turns it
// red.

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
	if !strings.Contains(src, "out[q].Arg = 2") {
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
// inside Inner's arm answers to the OUTER tag as well as its own.
const t08Nested = `package probe

type Cell { n int32 }
union Inner
{
    x Cell
    y Cell
}
union Outer
{
    p Inner
    q Cell
}
fixed table T
{
    pick Outer
}
`

// t08GoW16 holds W16's clause: "nested union answers outer tag". §4.1 and the
// TableFixedEntry doc: an arm inside an arm answers to the outer tag too, on a
// SECOND lane, so the entry runs only when BOTH tags hold their ordinal. The
// emitted walk must stamp the outer union's tag on Guard2/Arg2/ArgW2 for the
// entries an inner union already guarded with its own tag.
func t08GoW16(t *testing.T) {
	src := t08GoSource(t, t08Nested)
	for _, want := range []string{
		"out[q].Guard2 = ",
		"out[q].Arg2 = ",
		"out[q].ArgW2 = ",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("W16: the emitted walk never stamps %s; an arm inside an arm answers the outer tag on a second lane", strings.TrimSpace(want))
		}
	}
	// THE OUTER TAG LANDS BESIDE THE INNER, NEVER OVER IT: the stamp is guarded
	// by the first lane, so an entry already guarded keeps its tag and records
	// the outer one on the second.
	if !strings.Contains(src, "if out[q].Guard == tableFixedNoGuard") {
		t.Error("W16: the emitted walk overwrites the first guard lane instead of answering the outer tag on a second")
	}
}
