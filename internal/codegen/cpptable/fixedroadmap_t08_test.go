package cpptable

// fixedroadmap_t08 — the cpp leg's union-guards and plan-execution rows of
// docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"): the
// assertions no other test of this leg makes. Law: docs/FIXED-FORM-ALGORITHM.md
// §5.8 row 6 and §5.9 #19, docs/FIXED-FORM-VERSIONING-TESTS.md (the guard
// rows), and docs/SPEC-TABLES.md §3.3/§3.4. One subtest per task id, and the
// assertion under each is the clause the task's own title states, read off the
// plan the schema compiler lays down (ir/fixedform.go) or off the C++ runtime
// this leg emits with it (fixedruntime.go).
//
// NOTHING HERE READS THE C++ REFERENCE'S BYTE ORACLE OR RUNS A PROBE: every
// clause is the compiler's static data or the runtime body this leg emits with
// it, so the suite runs on a tree that never built build/fixedform-corpus —
// which is how a green verdict is not a skip over an unrun row.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t08Union is one fixed table whose single field is a union with an arm that
// carries text: the shape W7's clause needs — a guarded entry whose op has an
// argument of its own.
const t08Union = `package t08

type Alpha { s string(8) = "" }
type Beta  { n int32 = 0 }

union Pick
{
    alpha Alpha
    beta  Beta
}

fixed table Root
{
    pick Pick
}
`

// t08Nested is an arm inside an arm: `Outer.i` holds `Inner`, and `Inner.a`
// holds a scalar, so the inner arm's entries answer to both tags.
const t08Nested = `package t08n

type Leaf { n int32 = 0 }

union Inner
{
    a Leaf
}

union Outer
{
    i Inner
    b Leaf
}

fixed table Root
{
    o Outer
}
`

// t08Unit parses and checks one inline schema.
func t08Unit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t08.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return loadUnit(t, path)
}

// t08Root is the fixture's one fixed root. The walk here follows cppFixedRoot:
// the outer fixed table no other fixed table of the unit names by value.
func t08Root(t *testing.T, u *ir.Unit) *ir.Struct {
	t.Helper()
	roots := ir.TableFixedRoots(u)
	if len(roots) == 0 {
		t.Fatal("t08: the fixture declares no fixed root")
	}
	named := map[string]bool{}
	for _, st := range roots {
		for _, f := range st.Fields {
			named[f.Type.Name] = true
		}
	}
	for _, st := range roots {
		if !named[st.Name] {
			return st
		}
	}
	return roots[len(roots)-1]
}

// t08Emit is [Generate]'s source for a unit, all files joined, so a clause can
// read any emitted spelling without knowing which file carries it.
func t08Emit(t *testing.T, u *ir.Unit) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var b strings.Builder
	for name, data := range files {
		fmt.Fprintf(&b, "// ---- %s ----\n", name)
		b.Write(data)
	}
	return b.String()
}

func TestFixedRoadmapCppT08WritesConformance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"cpp/W7", t08W7},
		{"cpp/W16", t08W16},
		{"cpp/R28", t08R28},
		{"cpp/W6", t08W6},
		{"cpp/P1", t08P1},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t08W7: cpp/W7 "arg and meta are two lanes". A guarded entry's Arg is the
// guard's tag and nothing else, while Meta is the op's own argument (a text
// entry's flavour); §5.8 row 6's second lane and the ir/fixedform.go comment
// that "they shared one, and a string(N) under a union's arm then had to be
// either guarded correctly or read with the right flavour and could not be
// both". The fixture's `pick.alpha.s` is exactly that collision: one leaf that
// carries BOTH a guard's ordinal and a text flavour, on two fields.
func t08W7(t *testing.T) {
	u := t08Unit(t, t08Union)
	st := t08Root(t, u)
	plan, _ := ir.TableFixedBuildPlanNormalised(u, st)

	found := false
	for _, e := range plan {
		if e.Guard == ir.TableFixedNoGuard || e.Op != ir.TableFixedOpText {
			continue
		}
		found = true
		if e.Arg == 0 {
			t.Errorf("W7: the guarded text leaf carries no guard ordinal: %+v", e)
		}
		if e.Meta == 0 {
			t.Errorf("W7: the guarded text leaf carries no text flavour on Meta: %+v", e)
		}
		if e.Arg != 1 {
			t.Errorf("W7: the arm's ordinal is %d, want the first arm's 1: %+v", e.Arg, e)
		}
	}
	if !found {
		t.Fatal("W7: no guarded text leaf in the plan; the fixture proves nothing")
	}

	// The runtime lanes the plan is laid into are the two facts kept apart.
	for _, want := range []string{
		"uint64_t arg = 0;",
		"uint8_t meta = 0;",
	} {
		if !strings.Contains(tableFixedRuntime, want) {
			t.Errorf("W7: the emitted entry does not carry the lane %q", want)
		}
	}
	if i, j := strings.Index(tableFixedRuntime, "uint64_t arg = 0;"), strings.Index(tableFixedRuntime, "uint8_t meta = 0;"); i < 0 || j < 0 || j < i {
		t.Error("W7: meta does not stand after arg on the entry")
	}
}

// t08W16: cpp/W16 "nested union answers outer tag". An entry inside an arm
// inside an arm carries the OUTER tag on the second lane as a CONJUNCTION
// (ir/fixedform.go: "an arm nested inside an arm answers to the OUTER tag,
// which is the one that decides whether any of it is there at all"), so
// Guard2/Arg2/ArgW2 name the outer condition while Guard/Arg/ArgW keep the
// inner arm's own selection (§4.1, §5.8 row 6).
func t08W16(t *testing.T) {
	u := t08Unit(t, t08Nested)
	st := t08Root(t, u)
	plan, _ := ir.TableFixedBuildPlanNormalised(u, st)

	found := false
	for _, e := range plan {
		if e.Guard == ir.TableFixedNoGuard || e.Guard2 == ir.TableFixedNoGuard {
			continue
		}
		found = true
		if e.Arg == 0 || e.Arg2 == 0 {
			t.Errorf("W16: a doubly guarded entry is missing an ordinal: %+v", e)
		}
		if e.Guard2 == e.Guard {
			t.Errorf("W16: the outer and inner guards are the same offset %d: %+v", e.Guard2, e)
		}
	}
	if !found {
		t.Fatal("W16: no entry answers to two tags; the nested fixture proves nothing")
	}

	// Both tags are tested, and the second is a conjunction, not a replacement.
	want := "if ( p.guard2 != kTableFixedNoGuard && TableFixedTagAt( src, p.guard2, p.argw2 ) != p.arg2 ) { continue; }"
	if !strings.Contains(tableFixedRuntime, want) {
		t.Errorf("W16: the run loop does not test the outer tag as a conjunction; want %q", want)
	}
}

// t08R28: cpp/R28 "the guard chain: ... outermost first, the only bound the
// layout's 64; an arm inside an arm answers to the OUTER tag". On this leg the
// chain rides as two guard lanes on the entry (guard/arg/argw and
// guard2/arg2/argw2), each compared at its own width, and the walk's one bound
// is the layout's depth cap of 64 (§5.8 row 6; the depth guard refuses a
// stranger's deep chain rather than recursing).
func t08R28(t *testing.T) {
	u := t08Unit(t, t08Nested)
	st := t08Root(t, u)
	plan, _ := ir.TableFixedBuildPlanNormalised(u, st)

	// Every guarded entry carries an OFFSET (guard) plus its tag (arg) and the
	// tag's width (argw); the outer condition is carried on the second lane.
	for _, e := range plan {
		if e.Guard == ir.TableFixedNoGuard {
			continue
		}
		if e.ArgW <= 0 {
			t.Errorf("R28: a guarded entry carries no tag width: %+v", e)
		}
		if e.Guard2 != ir.TableFixedNoGuard && e.ArgW2 <= 0 {
			t.Errorf("R28: a doubly guarded entry carries no outer tag width: %+v", e)
		}
	}
	// The plan entry's aggregate names the guard lanes in the emitted order.
	if !strings.Contains(tableFixedRuntime, "uint32_t guard = kTableFixedNoGuard;") {
		t.Error("R28: the entry carries no guard offset lane")
	}
	if !strings.Contains(tableFixedRuntime, "uint8_t argw = 1;") {
		t.Error("R28: the entry carries no guard width lane")
	}
	// The tag is compared at its own width, not its low byte.
	if !strings.Contains(tableFixedRuntime, "inline uint64_t TableFixedTagAt( const uint8_t * src, uint32_t guard, uint8_t argw )") {
		t.Error("R28: the tag is not read at its own width")
	}
	// The layout's only depth bound is 64, and the walk refuses past it.
	if !strings.Contains(tableFixedRuntime, "constexpr int32_t kTableFixedMaxDepth = 64;") {
		t.Error("R28: the layout's depth cap is not 64")
	}
	if !strings.Contains(tableFixedRuntime, "if ( c.depth > 64 ) { c.hostile = true; return; }") {
		t.Error("R28: the walk does not refuse a chain past 64")
	}
}

// t08W6: cpp/W6 "plan partition / split". The plan is PARTITIONED — every
// unguarded entry first, then every guarded one — and `guarded` is where the
// second half starts (ir/fixedform.go TableFixedBuildPlan, the runtime's "A
// PLAN IS PARTITIONED"). The run loop walks the unguarded half with no guard
// test at all, then the guarded half with the conjunction.
func t08W6(t *testing.T) {
	u := t08Unit(t, t08Union)
	st := t08Root(t, u)
	plan, guarded := ir.TableFixedBuildPlanNormalised(u, st)

	if guarded <= 0 || guarded >= len(plan) {
		t.Fatalf("W6: the split %d of %d leaves is not a partition of both halves", guarded, len(plan))
	}
	for i, e := range plan {
		unguarded := e.Guard == ir.TableFixedNoGuard
		if unguarded != (i < guarded) {
			t.Fatalf("W6: leaf %d is on the wrong side of the split at %d: guard=%d", i, guarded, e.Guard)
		}
	}

	src := t08Emit(t, u)
	if !strings.Contains(src, "RootFixedPlanGuarded = ") {
		t.Error("W6: the emitted plan does not lay the split down as a constant")
	}
	for _, want := range []string{
		"for ( int32_t i = 0; i < guarded; ++i )",
		"for ( int32_t i = guarded; i < count; ++i )",
	} {
		if !strings.Contains(tableFixedRuntime, want) {
			t.Errorf("W6: the run loop does not carry the partition loop %q", want)
		}
	}
}

// t08P1: cpp/P1 "identity == compiled". Identity is a plan walked by the SAME
// loop as a plan compiled from a stranger's layout (fixedform.go: "Identity is
// a PLAN walked by the same loop"). The emitted load selects the identity plan
// by the file's own hash and the compiled plan by a known-older hash, and both
// reach the ONE TableFixedRun call site — there is no second identity path.
func t08P1(t *testing.T) {
	u := t08Unit(t, t08Union)
	src := t08Emit(t, u)

	if n := strings.Count(src, "TableFixedRun( entries, entry_count, entry_guarded,"); n != 1 {
		t.Fatalf("P1: the load has %d run call sites, want the one shared loop", n)
	}
	for _, want := range []string{
		"const TableFixedEntry * entries = RootFixedPlan;",
		"entries = dest;",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("P1: the load does not reach the shared loop through %q", want)
		}
	}
	// The shared loop is the partition walk, defined once.
	if n := strings.Count(tableFixedRuntime, "inline void TableFixedRun( const TableFixedEntry * plan, int32_t count, int32_t guarded,"); n != 1 {
		t.Fatalf("P1: the runtime defines %d TableFixedRun loops, want one", n)
	}
}
