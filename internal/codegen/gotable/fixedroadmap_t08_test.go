package gotable

// THE ROADMAP'S UNION-GUARDS ROW ON THE GO LEG (docs/roadmap.sexp node
// `fixed-tables`, ROADMAP.md "NEW Fixed Tables"). Law: docs/FIXED-FORM-ALGORITHM.md
// §4.1 (nested union tags are CONJOINED, never overwritten) and §5.8 row 6, and
// docs/SPEC-TABLES.md §3.4. One subtest per task id this card proves,
// table-driven, t.Parallel() first, and the assertion under each is the clause
// the task's own title states, read off the ir plan walk this leg emits.

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapGoT08WritesConformance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"go/W7", t08GoW7},
		{"go/W16", t08GoW16},
		{"go/R28", t08GoR28},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t08GoPlan is this leg's ir plan for a fixed root, the walk TableFixedBuildPlan
// lays down.
func t08GoPlan(t *testing.T, schema string) []ir.TableFixedLeaf {
	t.Helper()
	u := unitFrom(t, schema)
	st := u.Tables["T"]
	if st == nil {
		t.Fatal("the fixture declares no fixed table T")
	}
	plan, _ := ir.TableFixedBuildPlan(u, st)
	if len(plan) == 0 {
		t.Fatal("the ir plan is empty")
	}
	return plan
}

// ---- go/W7 ------------------------------------------------------------------

// t08GoW7 holds W7's clause: "arg and meta are two lanes". §5.2 EMIT and the
// TableFixedLeaf doc: "Meta is THE OP'S OWN ARGUMENT, on the ops that have one: a
// text entry's flavour today. It has its own lane because Arg's is the guard's".
// A text field under union arm 2 carries BOTH the guard's ordinal in Arg and the
// text flavour in Meta, and the two do not collide.
func t08GoW7(t *testing.T) {
	plan := t08GoPlan(t, `package probe

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
	found := false
	for _, e := range plan {
		if e.Op != ir.TableFixedOpText {
			continue
		}
		found = true
		if e.Guard == ir.TableFixedNoGuard {
			t.Error("W7: the text entry under a union arm carries no guard")
		}
		if e.Arg != 2 {
			t.Errorf("W7: the guard lane's Arg = %d, want the arm ordinal 2", e.Arg)
		}
		if e.Meta != 1 {
			t.Errorf("W7: the Meta lane = %d, want the text flavour 1 (utf8)", e.Meta)
		}
		if e.Arg == e.Meta {
			t.Error("W7: Arg and Meta share one lane; the guard ordinal and the op's argument must be two")
		}
	}
	if !found {
		t.Fatal("W7: the plan carries no text entry to read the two lanes off")
	}
}

// ---- go/W16 and go/R28 ------------------------------------------------------

// t08Nested is an arm inside an arm: Outer's arm p holds Inner, so the entry
// inside Inner's arm x answers to the OUTER tag as well as its own.
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
// TableFixedLeaf doc: "Guard2 / Arg2 / ArgW2 ARE THE OUTER TAG AN ARM INSIDE AN
// ARM ALSO ANSWERS TO ... the entry runs when BOTH tags hold their ordinal". The
// entry inside the inner arm carries its own tag on the first lane AND the outer
// tag on the second, and the two tag offsets differ.
func t08GoW16(t *testing.T) {
	plan := t08GoPlan(t, t08Nested)
	found := false
	for _, e := range plan {
		if e.Guard == ir.TableFixedNoGuard || e.Guard2 == ir.TableFixedNoGuard {
			continue
		}
		found = true
		if e.Arg < 1 || e.Arg2 < 1 {
			t.Errorf("W16: a tag lane names no arm (Arg=%d Arg2=%d); both name the arm each tag must hold", e.Arg, e.Arg2)
		}
		if e.Guard == e.Guard2 {
			t.Errorf("W16: both tag lanes read offset %d; the outer tag is a distinct tag", e.Guard)
		}
		if e.ArgW <= 0 || e.ArgW2 <= 0 {
			t.Errorf("W16: a guard width is zero (ArgW=%d ArgW2=%d); the tag width is at least one", e.ArgW, e.ArgW2)
		}
	}
	if !found {
		t.Fatal("W16: no entry carries an outer tag lane; the nested union was overwritten or dropped")
	}
}

// t08GoR28 holds the part of R28's clause the two-lane walk carries: "an arm
// inside an arm answers to the OUTER tag" and the entry carries (guard, arg,
// argw) links on both lanes. The OUTER tag's offset is the one nearer the record
// base, and it is stamped on the SECOND lane without erasing the inner tag.
func t08GoR28(t *testing.T) {
	plan := t08GoPlan(t, t08Nested)
	found := false
	for _, e := range plan {
		if e.Guard == ir.TableFixedNoGuard || e.Guard2 == ir.TableFixedNoGuard {
			continue
		}
		found = true
		if e.Guard2 > e.Guard {
			t.Errorf("R28: the inner tag offset %d is above the outer tag offset %d; the outer tag is nearer the base", e.Guard, e.Guard2)
		}
		for _, w := range []int{e.ArgW, e.ArgW2} {
			if w <= 0 || w > 8 {
				t.Errorf("R28: a guard width %d is outside one to eight bytes", w)
			}
		}
	}
	if !found {
		t.Fatal("R28: no entry carries both tag lanes")
	}
}
