package ctable

// TestFixedRoadmapCT08WritesConformance is the c leg's card
// fixed-c-t08-writes-conformance, rows union-guards, plan-execution and the
// identity half of interoperability of docs/roadmap.sexp's node
// `fixed-tables`. One subtest per task this card proves, named for the task id.
//
// The laws are docs/FIXED-FORM-ALGORITHM.md §4.1 (guards answer to the outer
// tag), §5.4 (the plan's partition and split) and docs/SPEC-TABLES.md §3.4 (the
// fixed message form). The identity==compiled and write-read-write clauses are
// SPEC-TABLES §3.4's "the fixed form reads and writes the same body".

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapCT08WritesConformance(t *testing.T) {
	t.Parallel()
	rows := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"c/W7", cT08ArgAndMetaTwoLanes},
		{"c/W16", cT08NestedUnionAnswersOuterTag},
		{"c/R28", cT08GuardChain},
		{"c/W6", cT08PlanPartitionSplit},
		{"c/P1", cT08IdentityEqualsCompiled},
		{"c/P2", cT08WriteReadWriteIdentical},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			row.run(t)
		})
	}
}

// cT08UnionSchema is a union whose first arm carries a text field, so a leaf
// under that arm carries BOTH a guard's tag (Arg) and the text op's flavour
// (Meta) at once.
const cT08UnionSchema = `package probe
type Payload { label string(8) = "" }
type Other { n int32 = 0 }
union Pick {
    q Other
    p Payload
}
fixed table Root { pick Pick }
`

// c/W7 — "arg and meta are two lanes". FIXED-FORM-ALGORITHM §4.1: the guard's
// tag rides in the plan entry's arg lane; SPEC-TABLES §3.4: a text field's
// flavour is the op's own meta lane. A text field under a union arm is the one
// entry where both lanes are live, so the test builds exactly that and asserts
// each lane carries its own fact: Arg is the arm's ordinal and Meta the text
// flavour. A port that stamped the flavour over the tag (or the reverse) fails.
func cT08ArgAndMetaTwoLanes(t *testing.T) {
	u := unitFrom(t, cT08UnionSchema)
	st := u.Tables["Root"]
	plan, _ := ir.TableFixedBuildPlan(u, st)
	var leaf *ir.TableFixedLeaf
	for i := range plan {
		if plan[i].Op == ir.TableFixedOpText {
			leaf = &plan[i]
			break
		}
	}
	if leaf == nil {
		t.Fatal("the plan lost the arm's text op")
	}
	if leaf.Guard == ir.TableFixedNoGuard {
		t.Fatal("the text field under a union arm is unguarded: the guard lane was dropped")
	}
	if leaf.Arg != 2 {
		t.Fatalf("arg is the guard's tag and nothing else: want second arm ordinal 2, got %d", leaf.Arg)
	}
	if leaf.Meta != 1 {
		t.Fatalf("meta is the op's own argument: want utf8 flavour 1, got %d", leaf.Meta)
	}
}

// cT08NestedUnionSchema nests a union inside an arm of an outer union, so the
// inner arm's leaves carry both tags: the inner one on guard/arg and the outer
// one on guard2/arg2.
const cT08NestedUnionSchema = `package probe
type InnerA { x int32 = 0 }
type InnerB { n int32 = 0 }
union Inner {
    ia InnerA
    ib InnerB
}
type OuterA { inner Inner }
type OuterB { m int32 = 0 }
union Outer {
    oa OuterA
    ob OuterB
}
fixed table Root { o Outer }
`

// c/W16 [owed] — "nested union answers outer tag". FIXED-FORM-ALGORITHM §4.1
// and §5.8 row 6: an arm inside an arm answers to the OUTER tag as well as its
// own, on a second lane that is a CONJUNCTION, not a replacement. The inner
// arm's leaf therefore carries guard/arg for the inner tag and guard2/arg2 for
// the outer. A port that stamped the outer tag over the inner one (so every
// inner arm fired the moment the outer rode) leaves guard2 unset.
func cT08NestedUnionAnswersOuterTag(t *testing.T) {
	u := unitFrom(t, cT08NestedUnionSchema)
	st := u.Tables["Root"]
	plan, _ := ir.TableFixedBuildPlan(u, st)
	var nested *ir.TableFixedLeaf
	for i := range plan {
		if plan[i].Guard != ir.TableFixedNoGuard && plan[i].Guard2 != ir.TableFixedNoGuard {
			nested = &plan[i]
			break
		}
	}
	if nested == nil {
		t.Fatal("no entry answers to two tags: the outer tag was stamped over the inner one")
	}
	if nested.Arg != 1 || nested.Arg2 != 1 {
		t.Fatalf("the inner arm's own ordinal and the outer arm's ordinal must both be 1: arg=%d arg2=%d", nested.Arg, nested.Arg2)
	}
	if nested.ArgW == 0 || nested.ArgW2 == 0 {
		t.Fatalf("each tag is compared at its own width: argw=%d argw2=%d", nested.ArgW, nested.ArgW2)
	}
	if nested.Guard == nested.Guard2 {
		t.Fatal("the outer tag was stamped at the inner tag's offset: the two conditions are not independent")
	}
}

// c/R28 [owed] — "the guard chain: the plan entry carries offset+count into a
// pool of (guard, arg, argw) links, outermost first". FIXED-FORM-ALGORITHM
// §5.8 row 6: the entry's guard is the offset of the tag that gates it, Arg the
// ordinal it must hold and ArgW the tag's width; a nested arm carries the outer
// link too. The chain's depth bound is the layout's own 64 (§5.8 row 6): the
// walk refuses a type deeper than it.
func cT08GuardChain(t *testing.T) {
	u := unitFrom(t, cT08NestedUnionSchema)
	st := u.Tables["Root"]
	if !ir.TableFixedWithinDepth(st) {
		t.Fatal("the nested type is within the layout's depth bound")
	}
	plan, _ := ir.TableFixedBuildPlan(u, st)
	links := 0
	for i := range plan {
		e := plan[i]
		if e.Guard == ir.TableFixedNoGuard {
			continue
		}
		if e.Arg < 1 {
			t.Fatalf("a guarded entry carries an arg (its tag's ordinal): got %d", e.Arg)
		}
		if e.ArgW < 1 {
			t.Fatalf("a guarded entry carries an argw (its tag's width in bytes): got %d", e.ArgW)
		}
		if e.Guard < 0 {
			t.Fatalf("a guarded entry carries the tag's offset: got %d", e.Guard)
		}
		links++
		if e.Guard2 != ir.TableFixedNoGuard && e.Arg2 >= 1 {
			links++
		}
	}
	if links == 0 {
		t.Fatal("the guard chain is empty: no entry carries a guard link")
	}
	if links < 2 {
		t.Fatal("an arm inside an arm answers to the OUTER tag too: only one link was stamped")
	}
}

// c/W6 [owed] — "plan partition / split". FIXED-FORM-ALGORITHM §5.4: the plan
// is PARTITIONED, every unguarded entry first and every guarded one after, and
// `guarded` is where the second half starts; neighbours are never merged across
// the split. The test asserts the index is in range, that the halves hold
// exactly the entries they claim, and that the coalescer did not merge an
// unguarded copy into a guarded one.
func cT08PlanPartitionSplit(t *testing.T) {
	u := unitFrom(t, cT08NestedUnionSchema)
	st := u.Tables["Root"]
	plan, guarded := ir.TableFixedBuildPlan(u, st)
	if guarded < 0 || guarded > len(plan) {
		t.Fatalf("the guarded split index %d is out of range for %d entries", guarded, len(plan))
	}
	for i := range plan {
		unguarded := plan[i].Guard == ir.TableFixedNoGuard
		if i < guarded && !unguarded {
			t.Fatalf("entry %d before the split is guarded", i)
		}
		if i >= guarded && unguarded {
			t.Fatalf("entry %d after the split is unguarded", i)
		}
	}
	// the split is real: the nested schema carries both halves
	if guarded == 0 || guarded == len(plan) {
		t.Fatalf("the partition has an empty half: guarded=%d of %d", guarded, len(plan))
	}
}

// c/P1 [owed] — "identity == compiled". SPEC-TABLES §3.4: the fixed form reads
// and writes one body, so the identity plan and a plan compiled against the
// reader's own layout are the same data walked by the same loop. The emitted C
// proves it: the load is the one table_fixed_run loop with no separate identity
// scatter and no whole-body memcpy.
func cT08IdentityEqualsCompiled(t *testing.T) {
	files := generate(t, cT08UnionSchema)
	var header string
	for name, data := range files {
		if strings.HasSuffix(name, "Table.h") {
			header += string(data)
		}
	}
	if !strings.Contains(header, "table_fixed_run") {
		t.Fatal("the load is not the one loop: identity and compiled plans are not one path")
	}
	if strings.Contains(header, "fixed_scatter") {
		t.Fatal("a scatter function survives: identity is a second path")
	}
	if strings.Contains(header, "memcpy( (void *) ( values + k ), at + 8") {
		t.Fatal("the load memcpys the whole body: identity is not the plan")
	}
}

// c/P2 [owed] — "write-read-write byte-identical". SPEC-TABLES §3.4: a saved
// fixed image read back and saved again is the same bytes. The probe writes a
// value with a non-default count and text, reads it into a dirty destination,
// writes again, and memcmps the two images byte for byte.
func cT08WriteReadWriteIdentical(t *testing.T) {
	runCGenerated(t, identityHeldSchema, `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
int main(void)
{
    Held value, loaded;
    TableReport report;
    TableFixedEntry plan[64];
    uint8_t first[512], second[512];
    int64_t need, n;
    memset(&value, 0, sizeof(value));
    value.marks[0] = 7;
    value.marks_count = 1;
    value.label[0] = 'h';
    value.label[1] = 'i';
    value.label_length = 2;
    need = held_fixed_measure(1);
    CHECK(need > 0 && need <= (int64_t)sizeof(first));
    CHECK(held_fixed_save(&value, 1, first, (int64_t)sizeof(first)) == need);
    memset(&loaded, 0xAA, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = held_fixed_load(&loaded, 1, first, need, plan, 64, NULL, &report);
    CHECK(n == 1);
    CHECK(!report.malformed && !report.refused && report.clamped == 0);
    CHECK(held_fixed_save(&loaded, 1, second, (int64_t)sizeof(second)) == need);
    CHECK(memcmp(first, second, (size_t)need) == 0);
    return 0;
}
`, identityCCFlags()...)
}
