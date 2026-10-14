package cstable

// fixedroadmap_t03 — the cs leg's plans-selection, fixed-closure, retirement and
// retired-runtime rows of docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md
// "NEW Fixed Tables"): the assertions no other test of this leg makes. Law:
// docs/FIXED-FORM-ALGORITHM.md §5.2 (COMPILE(lock, T)) and §5.3 (LOAD and its
// refusal table), docs/FIXED-FORM-VERSIONING-TESTS.md §5.7/#7 (the floor),
// docs/SPEC-TABLES.md §3.4 (the record ceiling and the closure rule). One
// subtest per task id, and the assertion under each is the clause the task's
// own title states, read off the C# this leg emits or off the ir walk it
// renders.
//
// A clause already held by an existing test of this leg is named by that test
// in the card's verdicts and is not repeated here; this file carries the direct
// assertion of every clause that had none.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapCSharpT03PlansVersions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"cs/R12", t03R12},
		{"cs/R6", t03R6},
		{"cs/R22", t03R22},
		{"cs/R3", t03R3},
		{"cs/R10", t03R10},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t03First is the index of the first occurrence of want in got, or -1. It names
// a rule by the text it must see so a moved statement is red by name.
func t03First(t *testing.T, got, want, what string) int {
	t.Helper()
	i := strings.Index(got, want)
	if i < 0 {
		t.Fatalf("%s: the generated source does not carry %q", what, want)
	}
	return i
}

// t03LoadBody is the first TFixedLoad method, from its signature to the next
// overload. The run-time walk and any hash recompute would live in this body;
// the static initializer's lineage walk does not.
func t03LoadBody(t *testing.T, src, name string) string {
	t.Helper()
	sig := "public static long " + name + "FixedLoad("
	i := strings.Index(src, sig)
	if i < 0 {
		t.Fatalf("the generated source has no %s", sig)
	}
	rest := src[i+len(sig):]
	if j := strings.Index(rest, sig); j >= 0 {
		return rest[:j]
	}
	return rest
}

// t03CheckErrors parses and checks a fixture the way the compiler does, and
// returns the checker's diagnostics as text. A fixture the check refuses is
// what the closure rule's own names are read from.
func t03CheckErrors(t *testing.T, src string) []string {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	_, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	out := make([]string, 0, len(cerrs))
	for _, e := range cerrs {
		out = append(out, e.Error())
	}
	return out
}

// t03R12: R12 [owed] "the per-record hash check is before the prefill: no_layout
// writes nothing" (docs/FIXED-FORM-ALGORITHM.md §5.3 step 11, §5.8 row 9). The
// record loop tests the record's own hash FIRST, before it allocates a
// destination or prefills a hole, so a no_layout refusal has written not one
// destination byte.
func t03R12(t *testing.T) {
	_, src := t02Table(t, t02Flat)
	iCheck := t03First(t, src, "if (BinaryPrimitives.ReadUInt64LittleEndian(at) != hash)", "R12 the per-record hash check")
	iNo := t03First(t, src, `report.Reason = "no_layout"`, "R12 the no_layout refusal")
	iAlloc := t03First(t, src, "values[k] = new ", "R12 the destination allocation")
	iFill := t03First(t, src, "TableFixedWire.FillRun(", "R12 the prefill")
	if iCheck >= iNo || iNo >= iAlloc || iNo >= iFill {
		t.Errorf("R12: the hash check (%d) / no_layout (%d) must stand before the allocation (%d) and the prefill (%d)", iCheck, iNo, iAlloc, iFill)
	}
	// No destination write stands between the check and its refusal: the two
	// statements are adjacent, so nothing lands before the return.
	between := src[iCheck:iNo]
	if strings.Contains(between, ") = new ") || strings.Contains(between, "FillRun(") || strings.Contains(between, "TableFixedWire.Run(") {
		t.Errorf("R12: a destination write stands between the hash check and the no_layout refusal:\n%s", between)
	}
}

// t03R6: R6 [owed] "a table past §3.4's 65536 ceiling is not a fixed-form root:
// the refusal names the table, no form is emitted, and no lineage entry is
// parsed for it even when the lock carries one" (docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-ALGORITHM.md §5.2). The table is excluded from the form roots,
// the size bound names it and the size, and a lock entry for it reaches no
// emitted form.
func t03R6(t *testing.T) {
	const src = `package probe

fixed table T
{
    x [16385]int32
}
`
	u := unitFrom(t, src)
	if len(ir.TableFixedRoots(u)) != 1 {
		t.Fatalf("R6: the fixture declares %d fixed roots, want one", len(ir.TableFixedRoots(u)))
	}
	if len(ir.TableFixedFormRoots(u)) != 0 {
		t.Errorf("R6: a table past the 65536 ceiling is a fixed-form root")
	}
	warnings, errs := ir.TableFixedRecordBounds(u, 0)
	if len(errs) != 0 {
		t.Errorf("R6: the ceiling is not a hard refusal; errs = %v", errs)
	}
	named := false
	for _, w := range warnings {
		if strings.Contains(w, "table T") && strings.Contains(w, "65536-byte fixed-form ceiling") {
			named = true
		}
	}
	if !named {
		t.Errorf("R6: the ceiling must name the table and the bound; warnings = %v", warnings)
	}

	// A lock entry for T reaches no emitted form: no known array, no floor.
	own, _ := FixedLineageOf(u, "T")
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": {own}})
	if err != nil {
		t.Fatalf("R6: GenerateLineage: %v", err)
	}
	gen := string(files[t02File])
	if strings.Contains(gen, "TFixedKnown") || strings.Contains(gen, "TFixedFloor") || strings.Contains(gen, "TFixedLayout") {
		t.Error("R6: the table is past the ceiling, so no fixed form may be emitted for it, yet the source carries one")
	}
}

// t03R22: R22 [owed] "the closure rule: every table or type reached by value is
// itself fixed; a pointer, map or unbounded array in the closure is a compile
// refusal; T is never in its own closure" (docs/SPEC-TABLES.md §2.2, §11;
// docs/FIXED-FORM-BILL-READS-BACKWARD.md §2a). A plain nested table, a pointer,
// a map and an unbounded array are each refused by the construct's own name,
// and the closure walk stops at T so T is not walked as its own member.
func t03R22(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"pointer", "package t\ntable Node { x int32 }\nfixed table Scene { head *Node }\n", "Scene.head"},
		{"map", "package t\nfixed table Fleet { ships map[uint32]int32 }\n", "Fleet.ships"},
		{"unbounded array", "package t\nfixed table Log { entries []int32 }\n", "Log.entries"},
		{"plain nested table", "package t\ntable Child { x int32 }\nfixed table Root { child Child }\n", "Root.child"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := t03CheckErrors(t, tc.src)
			if len(errs) == 0 {
				t.Fatalf("R22: %s in a fixed table's closure is a compile refusal, and the check admitted it", tc.name)
			}
			joined := strings.Join(errs, "\n")
			if !strings.Contains(joined, tc.want) || !strings.Contains(joined, "docs/SPEC-TABLES.md") {
				t.Errorf("R22: the refusal must name %q and cite the spec: %s", tc.want, joined)
			}
		})
	}

	// T is never in its own closure: the walk marks T seen before its fields,
	// so a fixed table that names itself by value stops at the already-seen
	// name rather than re-entering it.
	node := &ir.Struct{Name: "Node", IsTable: true, FixedDeclared: true}
	node.Fields = []*ir.Field{{Name: "next", Type: ir.FieldType{Kind: ir.TNamed, Name: "Node", Ref: node}}}
	calls := 0
	breaks := ir.FixedClosureBreaks(func(name string) *ir.Struct {
		calls++
		if name == "Node" {
			return node
		}
		return nil
	}, "Node")
	if calls != 1 {
		t.Errorf("R22: the walk resolved Node %d times, want once: T is never in its own closure", calls)
	}
	for _, b := range breaks {
		if b.Owner == "Node" && b.Field == "next" {
			t.Errorf("R22: the walk reported Node's own self-reference as a break: %+v", b)
		}
	}
}

// t03R3: R3 [verify] "the floor is 1 + the highest retired index (0 when none);
// below the floor is layout_unsupported, reporting the file's hash"
// (docs/FIXED-FORM-ALGORITHM.md §5.2, docs/FIXED-FORM-VERSIONING-TESTS.md §5.7).
// The emitted floor constant is the retired mark counted from one, and the load
// refuses below it with the file's hash.
func t03R3(t *testing.T) {
	u := unitFrom(t, t02Flat)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R3: FixedLineageOf has no entry for T")
	}
	older := func(wire uint64, retired bool) FixedLineageEntry {
		e := own
		e.Wire = wire
		e.Retired = retired
		e.Reason = "retired by the test's lock"
		return e
	}

	// none retired: the floor is zero.
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("R3: Generate: %v", err)
	}
	if !strings.Contains(string(files[t02File]), "public const int TFixedFloor = 0;") {
		t.Error("R3: with no retired entry the floor is 0")
	}

	// one retired at index 0: the floor is 1.
	files, err = GenerateLineage(u, map[string][]FixedLineageEntry{"T": {older(0x1111, true), older(0x2222, false)}})
	if err != nil {
		t.Fatalf("R3: GenerateLineage: %v", err)
	}
	src := string(files[t02File])
	if !strings.Contains(src, "public const int TFixedFloor = 1;") {
		t.Error("R3: one retired entry at index 0 must set the floor to 1")
	}

	// two retired: the floor is 1 + the highest retired index = 2.
	files, err = GenerateLineage(u, map[string][]FixedLineageEntry{"T": {older(0x1111, true), older(0x2222, true)}})
	if err != nil {
		t.Fatalf("R3: GenerateLineage: %v", err)
	}
	if !strings.Contains(string(files[t02File]), "public const int TFixedFloor = 2;") {
		t.Error("R3: two retired entries must set the floor to 1 + the highest retired index = 2")
	}

	// below the floor: layout_unsupported, reporting the file's hash.
	pick := t03First(t, src, "int pick = TableFixedWire.Select(", "R3 the hash select")
	below := t03First(t, src, "if (pick < TFixedFloor)", "R3 the floor test")
	unsup := t03First(t, src, `report.Reason = "layout_unsupported"`, "R3 the layout_unsupported refusal")
	if pick >= below || below >= unsup {
		t.Errorf("R3: the floor test (%d) must stand after the select (%d) and before the refusal (%d)", below, pick, unsup)
	}
	hash := strings.Index(src[unsup:], "report.LayoutHash = hash;")
	if hash < 0 || hash > 128 {
		t.Error("R3: layout_unsupported reports THE FILE'S hash")
	}
}

// t03R10: R10 [weak] "the run-time walk of a stranger's layout and the recompute
// of the header's hash are retired" (docs/FIXED-FORM-ALGORITHM.md §5.3 "EVERY
// HASH A RUNTIME HOLDS WAS HANDED TO IT"). The load path parses no layout and
// derives no hash from the bytes it reads; the lineage walk and the hash live
// in the build's static data, off every load path.
func t03R10(t *testing.T) {
	_, src := t02Table(t, t02Flat)
	body := t03LoadBody(t, src, "T")
	if strings.Contains(body, "ParseLayout(") {
		t.Error("R10: the load path walks a layout; the run-time walk of a stranger's layout is retired")
	}
	if strings.Contains(body, "HashOf(") {
		t.Error("R10: the load path recomputes the header's hash; it is taken as given")
	}
	if n := strings.Count(src, "HashOf("); n != 1 {
		t.Errorf("R10: HashOf( occurs %d times in the emitted unit, want its one definition and no call", n)
	}
}
