package gotable

// TestFixedRoadmapGoT15GuardChain is the go leg's row of the card
// the-guard-chain-answers-the-outer-tag: docs/roadmap.sexp go/R28, "the guard
// chain: the plan entry carries offset+count into a pool of (guard, arg, argw)
// links, outermost first, the only bound the layout's 64; an arm inside an arm
// answers to the OUTER tag". Law: docs/FIXED-FORM-ALGORITHM.md §4.1 (the guard
// chain) and §5.8 row 6.
//
// ONE FIXTURE, SHARED BY THE FIVE LEGS (t15GuardChainSchema, the same text in
// cstable, javatable, jstable and darttable): a union whose arm is a union
// whose arm is a union whose arm is a table, three tags deep on one path. The
// deepest leaf is written and read back, on the IDENTITY plan and on a plan
// COMPILED from the layout, and it must land only when EVERY enclosing tag
// holds its ordinal: each of the three tags is poisoned in turn and the leaf
// must not land. The plan entry's offset+count into the guard pool and the
// links OUTERMOST FIRST are asserted on both plans; then the bound: 31 nested
// unions (the deepest inside the layout's 64) run a 31-link chain, and 32 are
// refused by the depth bound BY NAME.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// t15GuardChainSchema is THE fixture of the five legs' T15GuardChain tests.
// Outer's arm m is the union Middle, whose arm i is the union Inner, whose arm
// a is the table LeafA. A UNION WHOSE ARM IS A UNION IS A TABLE-CLOSURE
// CONSTRUCT (SPEC-TABLES §2.6), so Middle and Inner ride the body as fields too.
// The record body is [0] Outer's tag, [1] Middle's, [2] Inner's, [3..7) x.
const t15GuardChainSchema = `package probe

type LeafA { x int32 = 0 }
type LeafB { y int32 = 0 }
union Inner
{
    a LeafA
    b LeafB
}
union Middle
{
    i Inner
    n LeafB
}
union Outer
{
    m Middle
    p LeafB
}
fixed table Root
{
    o Outer
    mid Middle
    inn Inner
}
`

// t15DeepUnionSchema lays n unions on one path to a leaf: S0's x sits under
// U0's arm a, which sits under U1's arm a, and so on out to U{n-1}; every union
// has a second arm b. Each union adds two levels to the layout's depth, so 31
// is the deepest chain the layout's 64 carries and 32 is past it (the c leg's
// cT08DeepUnionSchema, the same shape).
func t15DeepUnionSchema(n int) string {
	var b strings.Builder
	b.WriteString("package probe\n")
	b.WriteString("type S0 { x int32 = 0 }\n")
	b.WriteString("type Sb { y int32 = 0 }\n")
	for i := range n {
		fmt.Fprintf(&b, "union U%d {\n    a S%d\n    b Sb\n}\n", i, i)
		fmt.Fprintf(&b, "type S%d { u U%d }\n", i+1, i)
	}
	fmt.Fprintf(&b, "fixed table Root { u U%d }\n", n-1)
	return b.String()
}

// t15GoRunDepth is the longest chain the go/R28 subtest BUILDS and runs end to
// end; the bound of 64 is asserted on the 31- and 32-union units without one.
const t15GoRunDepth = 8

func TestFixedRoadmapGoT15GuardChain(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	rows := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"go/R28", t15GoR28},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			row.run(t)
		})
	}
}

// t15GoChainProbe is the generated-module test that proves a chain of `depth`
// links on BOTH plans: the deepest entry carries GuardCount == depth at an
// offset into the pool, its links are OUTERMOST FIRST (guards ascending, each
// arm 1 at width 1), and the leaf lands as written and never while any of the
// first three tags names another arm. `set` writes the value v, `leaf` is the
// deepest leaf read off a Root named r.
func t15GoChainProbe(depth int, set, leaf string) string {
	return fmt.Sprintf(`package probe

import (
	"testing"
	"unsafe"
)

const depth = %d

func leafOf(r *Root) int32 { return %s }

func chainOf(t *testing.T, plan []TableFixedEntry, count int32) []TableFixedGuardLink {
	t.Helper()
	for i := int32(0); i < count; i++ {
		e := plan[i]
		if e.GuardCount != depth {
			continue
		}
		links := make([]TableFixedGuardLink, e.GuardCount)
		for j := range links {
			links[j] = e.guardLink(plan, uint32(j))
		}
		// THE INNERMOST TWO STILL RIDE THE TWO LANES (the special case).
		if e.Guard != links[depth-1].Guard || e.Guard2 != links[depth-2].Guard {
			t.Fatalf("the lanes are not the chain's innermost two: guard=%%d guard2=%%d chain=%%+v", e.Guard, e.Guard2, links)
		}
		return links
	}
	t.Fatalf("no plan entry carries a %%d-link chain (offset+count into the pool)", depth)
	return nil
}

func outermostFirst(t *testing.T, links []TableFixedGuardLink) {
	t.Helper()
	for j, l := range links {
		if l.Guard != uint32(j) || l.Arg != 1 || l.ArgW != 1 {
			t.Fatalf("link %%d is not (guard %%d, arg 1, argw 1), so the chain is not OUTERMOST FIRST: %%+v", j, j, l)
		}
	}
}

func TestGuardChain(t *testing.T) {
	var v Root
	RootReset(&v)
%s
	buf := make([]byte, RootFixedMeasure(1))
	if n := RootFixedSave([]Root{v}, buf); n != int64(len(buf)) {
		t.Fatalf("save %%d want %%d", n, len(buf))
	}
	rec := len(buf) - RootFixedRecordBytes + 8
	for j := 0; j < 3; j++ {
		if buf[rec+j] != 1 {
			t.Fatalf("the writer did not put tag %%d where the chain's link says it is", j)
		}
	}

	// THE IDENTITY PLAN.
	outermostFirst(t, chainOf(t, RootFixedPlan.Entries, RootFixedPlan.Count))
	for poison := 0; poison <= 3; poison++ {
		file := append([]byte(nil), buf...)
		if poison > 0 {
			file[rec+poison-1] = 2
		}
		got := make([]Root, 1)
		var r TableReport
		if n := RootFixedLoad(got, file, make([]TableFixedEntry, 1<<16), &r); n != 1 || r.Reason != "" {
			t.Fatalf("identity poison %%d: load %%d %%+v", poison, n, r)
		}
		if poison == 0 && leafOf(&got[0]) != 42 {
			t.Fatalf("identity: the record as written did not land its deepest leaf")
		}
		if poison > 0 && leafOf(&got[0]) == 42 {
			t.Fatalf("identity: the deepest leaf landed while tag %%d names another arm: an arm inside an arm answers to EVERY outer tag", poison-1)
		}
	}

	// THE COMPILED PLAN: the same layout walked by the plan compiler.
	parsed, why := tableFixedParseLayout(RootFixedLayout)
	if why != "" {
		t.Fatalf("the layout does not parse: %%s", why)
	}
	plan := make([]TableFixedEntry, 1<<16)
	var cr TableReport
	made := tableFixedCompile(parsed, RootFixedLayout, RootFixedDst, plan, &cr)
	if made <= 0 {
		t.Fatalf("the compiled plan refused a %%d-link chain (%%d): the chain is bounded by something other than the layout's 64", depth, made)
	}
	outermostFirst(t, chainOf(t, plan, made))
	for poison := 0; poison <= 3; poison++ {
		body := append([]byte(nil), buf[rec:]...)
		if poison > 0 {
			body[poison-1] = 2
		}
		var got Root
		RootReset(&got)
		var r TableReport
		if why := tableFixedRun(plan, made, body, tableFixedOverlay(unsafe.Pointer(&got), unsafe.Sizeof(got)), &r); why != "" {
			t.Fatalf("compiled poison %%d: %%s", poison, why)
		}
		if poison == 0 && leafOf(&got) != 42 {
			t.Fatalf("compiled: the record as written did not land its deepest leaf")
		}
		if poison > 0 && leafOf(&got) == 42 {
			t.Fatalf("compiled: the deepest leaf landed while tag %%d names another arm", poison-1)
		}
	}
}
`, depth, leaf, set)
}

// go/R28 — the chain on the three-deep fixture, then the bound.
func t15GoR28(t *testing.T) {
	runGenerated(t, t15GuardChainSchema, t15GoChainProbe(3, `	v.O.Type = OuterTypeM
	v.O.M.Type = MiddleTypeI
	v.O.M.I.Type = InnerTypeA
	v.O.M.I.A.X = 42`, "r.O.M.I.A.X"))

	// A LONG CHAIN RUNS WHOLE: t15GoRunDepth nested unions build a chain of as
	// many links, laid outermost first, and the deepest leaf answers to every one
	// of them on both plans. The generated unit grows with the depth, so the run
	// stops at t15GoRunDepth; the bound itself is asserted below without a build.
	var set strings.Builder
	path := "v.U"
	for i := t15GoRunDepth - 1; i >= 0; i-- {
		fmt.Fprintf(&set, "\t%s.Type = U%dTypeA\n", path, i)
		path += ".A"
		if i > 0 {
			path += ".U"
		}
	}
	fmt.Fprintf(&set, "\t%s.X = 42", path)
	runGenerated(t, t15DeepUnionSchema(t15GoRunDepth), t15GoChainProbe(t15GoRunDepth, set.String(), "r"+strings.TrimPrefix(path, "v")+".X"))

	// THE ONLY BOUND IS THE LAYOUT'S 64: 31 nested unions are inside it — the
	// fixed form is emitted for them and the plan carries a 31-link chain — and
	// 32 are past it, refused by the depth bound BY NAME, with no fixed form
	// emitted for the table.
	const deepest = 31
	in := unitFrom(t, t15DeepUnionSchema(deepest))
	if !ir.TableFixedWithinDepth(in.Tables["Root"]) {
		t.Fatalf("%d nested unions are inside the 64-entry depth bound", deepest)
	}
	_, _, pool := ir.TableFixedBuildPlanNormalisedWithPool(in, in.Tables["Root"])
	if len(pool) < deepest {
		t.Fatalf("%d nested unions lay a pool of %d links: the chain is bounded by something other than the layout's 64", deepest, len(pool))
	}
	emitted := false
	for _, data := range generate(t, t15DeepUnionSchema(deepest)) {
		emitted = emitted || strings.Contains(string(data), "func RootFixedLoad(")
	}
	if !emitted {
		t.Fatalf("%d nested unions: this leg emits no fixed form inside the depth bound", deepest)
	}
	past := unitFrom(t, t15DeepUnionSchema(deepest+1))
	if ir.TableFixedWithinDepth(past.Tables["Root"]) {
		t.Fatalf("%d nested unions are past the 64-entry depth bound", deepest+1)
	}
	named := false
	for _, w := range ir.TableFixedDepthBounds(past) {
		named = named || strings.Contains(w, "table Root:")
	}
	if !named {
		t.Fatalf("%d nested unions: the depth bound turned the table away without naming it", deepest+1)
	}
	for name, data := range generate(t, t15DeepUnionSchema(deepest+1)) {
		if strings.Contains(string(data), "func RootFixedLoad(") {
			t.Fatalf("%d nested unions: %s emits the fixed form past the depth bound", deepest+1, name)
		}
	}
}
