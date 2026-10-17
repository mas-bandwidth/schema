package gotable

// THE ROADMAP'S OPTIONAL-VALUES, FIELD-EVOLUTION AND REPORTS ROWS ON THE GO LEG
// (docs/roadmap.sexp node `fixed-tables`, ROADMAP.md "NEW Fixed Tables"). Law:
// docs/FIXED-FORM-ALGORITHM.md §3.1 (optional values and the present byte), §5
// (COMPILE and the one read loop), docs/FIXED-FORM-VERSIONING-TESTS.md (the
// rows and §5.4's counters), and docs/SPEC-TABLES.md §3.4 (the fixed form) and
// §5 (renames ride under `was`). One subtest per task id, table-driven,
// t.Parallel() first, and the assertion under each is the clause the task's own
// title states, read off the Go this leg emits, off the ir walk it renders, or
// off the emitted runtime the plan runs on.
//
// EVERY SUBTEST EXCEPT go/R19 IS A PURE-Go ASSERTION, so it RUNS under a bare
// `go test ./internal/codegen/gotable/` and under
// `-run TestFixedRoadmapGoT04VersionsEvolution`. go/R19 is the one clause that
// needs the emitted code to RUN as a NEW-READS-OLD pair: it generates the OLD
// and the NEW unit with the table emitter, hands the NEW unit the OLD unit's
// locked lineage entry (§5.2), and writes with the old plan then reads with the
// compiled one — all through the package's own generated files, which for a
// fixed table of scalar fields carry no sibling `serialize.go` import, so the
// subtest runs without the corpus and without the sibling. The row's cell "x,y,z
// exact; w = default" and the title's "moves no counter at all" are asserted on
// the running read.
//
// EVERY TASK IN THIS ROW IS SUBTESTED. go/R16's last clause —
// "copy/const/present/ordinal move nothing" — is held: the `ordinal` and `const`
// ops count no clamp, and the forged ordinal's count lives in the bounds pass
// (§5.4, fix 27, schema#1254).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapGoT04VersionsEvolution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"go/E5", t04GoE5},
		{"go/W2", t04GoW2},
		{"go/E6", t04GoE6},
		{"go/E8", t04GoE8},
		{"go/E9", t04GoE9},
		{"go/R19", t04GoR19},
		{"go/R16", t04GoR16},
		{"go/R18", t04GoR18},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

const t04Flat = `package probe

fixed table T
{
    x int32
    y int32
}
`

const t04PlainNest = `package probe

type Inner { a int32 }

fixed table T
{
    n Inner
}
`

const t04OptionalNest = `package probe

type Inner { a int32 }

fixed table T
{
    n ?Inner
}
`

const t04Rename = `package probe

fixed table T
{
    y int32 | was = "x"
}
`

const t04Deprecated = `package probe

fixed table T
{
    a int32
    b int32 | deprecated
    c int32
}
`

const t04AppendOld = `package probe

fixed table T
{
    x int32
    y int32
    z int32
}
`

const t04AppendNew = `package probe

fixed table T
{
    x int32
    y int32
    z int32
    w int32 = 7
}
`

const t04Ranged = `package probe

fixed table T
{
    x int32 | min = -5, max = 5
}
`

// ---- go/E5 ------------------------------------------------------------------

// t04GoE5 holds E5 to §3.4's ONE kind the fixed LAYOUT adds to §3's closed set:
// `?T` is TableKindOptional (35), a present byte then the payload, and it is NOT
// the plain nesting's Table kind. A plain `Inner` nests by value with no flag.
func t04GoE5(t *testing.T) {
	opt := unitFrom(t, t04OptionalNest)
	ow := ir.TableFixedWalkRoot(opt.Tables["T"])
	inner := unitFrom(t, t04PlainNest).Tables["T"].Fields[0].Type.Ref.(*ir.Struct)
	payload := fixedTypeBytes(inner)
	found := false
	for _, e := range ow {
		if e.Note != "n ?" {
			continue
		}
		found = true
		if e.Kind != ir.TableKindOptional {
			t.Errorf("E5: `?Inner` walks as kind %d, want TableKindOptional = %d", e.Kind, ir.TableKindOptional)
		}
		if e.Size != payload+fixedPresentBytes {
			t.Errorf("E5: `?Inner` size = %d, want present byte + payload = %d + %d", e.Size, fixedPresentBytes, payload)
		}
	}
	if !found {
		t.Fatal("E5: the optional field has no walk entry")
	}

	pw := ir.TableFixedWalkRoot(unitFrom(t, t04PlainNest).Tables["T"])
	plain := false
	for _, e := range pw {
		if e.Kind == ir.TableKindOptional {
			t.Error("E5: plain nesting emitted the optional kind (35)")
		}
		if e.Note == "n" {
			plain = true
			if e.Kind != ir.TableKindTable {
				t.Errorf("E5: plain `Inner` walks as kind %d, want TableKindTable = %d", e.Kind, ir.TableKindTable)
			}
			if e.Size != payload {
				t.Errorf("E5: plain `Inner` size = %d, want the payload alone = %d", e.Size, payload)
			}
		}
	}
	if !plain {
		t.Fatal("E5: the plainly nested field has no walk entry")
	}
	if ir.TableKindOptional == ir.TableKindTable {
		t.Error("E5: `?T` must not share the plain nesting's kind")
	}
}

// ---- go/W2 ------------------------------------------------------------------

// t04GoW2 holds W2 to §3.1's present byte and fix 13's writer: the flag always
// rides (a `p := uint8(0)` then `if <v>.<Member>Present { p = 1 }` and ONE put),
// and the payload is written ONLY under the flag — a plain nesting has no such
// guard and writes its payload unconditionally.
func t04GoW2(t *testing.T) {
	opt := t02GoEmit(t, unitFrom(t, t04OptionalNest))
	save := t02GoNorm(funcSource(opt, "func TFixedWriteBody("))
	if save == "" {
		t.Fatal("W2: TFixedWriteBody is not emitted")
	}
	t02GoHas(t, save, "if value.NPresent { p = 1 }", "W2 the present byte is 1 only when the value is present")
	// The present-byte put is unconditional, and the payload store is the one
	// guarded block: the save opens exactly two `if value.NPresent {` regions.
	if n := strings.Count(save, "if value.NPresent {"); n != 2 {
		t.Errorf("W2: the save opens %d present guards, want the byte's 1 and the one payload store", n)
	}
	if !strings.Contains(save, "tableFixedPut8(b[0:], p)") {
		t.Error("W2: the present byte is not written unconditionally; an absent optional still rides its flag")
	}

	plain := t02GoEmit(t, unitFrom(t, t04PlainNest))
	if strings.Contains(t02GoNorm(funcSource(plain, "func TFixedWriteBody(")), ".NPresent") {
		t.Error("W2: plain nesting emitted a present guard; only `?T` carries the flag")
	}
}

// ---- go/E6 ------------------------------------------------------------------

// t04GoE6 holds E6 to §5's declared identity: a field renamed under `was` keeps
// the OLD name's wire id, so wire identity survives the rename. The emitted
// layout names that id, and it is not the id of the field's new name.
func t04GoE6(t *testing.T) {
	u := unitFrom(t, t04Rename)
	f := u.Tables["T"].Fields[0]
	if got, want := ir.TableFieldWireId(f), ir.TableWireId("x"); got != want {
		t.Errorf("E6: the renamed field's wire id = %#x, want the old name's %#x", got, want)
	}
	if ir.TableFieldWireId(f) == ir.TableWireId("y") {
		t.Error("E6: the renamed field rides under its NEW name; `was` must keep the old identity")
	}
	walk := ir.TableFixedWalkRoot(u.Tables["T"])
	found := false
	for _, e := range walk {
		if e.Note != "y" {
			continue
		}
		found = true
		if e.ID != ir.TableWireId("x") {
			t.Errorf("E6: the emitted layout id = %#x, want the declared old name's %#x", e.ID, ir.TableWireId("x"))
		}
	}
	if !found {
		t.Fatal("E6: the renamed field has no walk entry")
	}
}

// ---- go/E8 ------------------------------------------------------------------

// t04GoE8 holds E8 to §3.4's append-only rule: "a fixed table evolves
// APPEND-ONLY: a field is deprecated in place, never removed". A deprecated
// field keeps its slot and its declared wire id, and an appended field extends
// the declaration and compiles against the older locked layout.
func t04GoE8(t *testing.T) {
	dep := unitFrom(t, t04Deprecated)
	st := dep.Tables["T"]
	if len(st.Fields) != 3 {
		t.Fatalf("E8: the deprecated field was removed; the table declares %d fields, want 3", len(st.Fields))
	}
	b := st.Fields[1]
	if !b.Deprecated {
		t.Error("E8: the `| deprecated` marker did not reach the IR")
	}
	found := false
	for _, e := range ir.TableFixedWalkRoot(st) {
		if e.Note == "b" {
			found = true
			if e.ID != ir.TableWireId("b") {
				t.Errorf("E8: the deprecated field's wire id = %#x, want %#x", e.ID, ir.TableWireId("b"))
			}
		}
	}
	if !found {
		t.Fatal("E8: deprecation removed the field's slot from the layout")
	}

	older := unitFrom(t, t04AppendOld)
	entry, ok := FixedLineageOf(older, "T")
	if !ok {
		t.Fatal("E8: the old table has no lineage entry")
	}
	newer := unitFrom(t, t04AppendNew)
	if _, err := GenerateLineage(newer, map[string][]FixedLineageEntry{"T": {entry}}); err != nil {
		t.Fatalf("E8: an appended field was refused against the older locked layout: %v", err)
	}
	nf := newer.Tables["T"].Fields
	if nf[len(nf)-1].Name != "w" {
		t.Errorf("E8: the appended field is not last: %v", nf[len(nf)-1].Name)
	}
}

// ---- go/R19 -----------------------------------------------------------------

// t04GoR19 holds go/R19's clean-pair clause — "a clean NEW-READS-OLD of an
// appended field, variant, arm, flag or keyed slot moves no counter at all" — by
// RUNNING the pair, one row of docs/FIXED-FORM-VERSIONING-TESTS.md per subtest.
//
// The corpus `TestFixedVersioningNewReadsOld` reads is the C++ reference's and
// its build needs the `../serialize` sibling that is not staged, so it SKIPS at
// fixedversioning_test.go:811, and a skip is not a run. This harness instead
// generates BOTH sides with the table emitter: the OLD unit's identity plan
// writes the file and the NEW unit, handed the OLD unit's locked lineage entry
// (§5.2), reads it back with its COMPILED plan. The generated files for a fixed
// table of scalars, enums, flags and keyed slots carry no sibling `serialize.go`
// import (a union arm does, so `union_append` is not among the runnable rows).
// The row's own cell says what must land — `field_append`: "x,y,z exact; w =
// default" — and the title's "moves no counter at all" is asserted on the
// running read.
func t04GoR19(t *testing.T) {
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated clean pair)")
	rows := []string{"field_append", "enum_append", "flags_append", "keyed_array_enum_append"}
	root := t.TempDir()
	var imports, requires, replaces, bodies []string
	for _, row := range rows {
		oldPkg := "old_" + row
		newPkg := "new_" + row
		oldU := unitFrom(t, readSchema(t, "VOLD_"+row))
		newU := unitFrom(t, readSchema(t, "VNEW_"+row))
		lineage := map[string][]FixedLineageEntry{}
		for _, st := range ir.TableFixedRoots(oldU) {
			e, ok := FixedLineageOf(oldU, st.Name)
			if !ok {
				t.Fatalf("R19 %s: no lineage entry for %s", row, st.Name)
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
		oldFiles, err := Generate(oldU)
		if err != nil {
			t.Fatalf("R19 %s: old Generate: %v", row, err)
		}
		newFiles, err := GenerateLineage(newU, lineage)
		if err != nil {
			t.Fatalf("R19 %s: new GenerateLineage: %v", row, err)
		}
		t04MergeGo(t, oldFiles, oldU)
		t04MergeGo(t, newFiles, newU)
		t04WriteModule(t, filepath.Join(root, oldPkg), oldPkg, oldFiles)
		t04WriteModule(t, filepath.Join(root, newPkg), newPkg, newFiles)
		oa, na := "o_"+row, "n_"+row
		imports = append(imports, fmt.Sprintf("\t%s %q", oa, oldPkg), fmt.Sprintf("\t%s %q", na, newPkg))
		requires = append(requires, fmt.Sprintf("\t%s v0.0.0", oldPkg), fmt.Sprintf("\t%s v0.0.0", newPkg))
		replaces = append(replaces, fmt.Sprintf("replace %s => ../%s", oldPkg, oldPkg), fmt.Sprintf("replace %s => ../%s", newPkg, newPkg))
		bodies = append(bodies, t04CleanBody(t, row, oa, na))
	}
	testDir := filepath.Join(root, "cleanpair")
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mod := "module cleanpair\n\ngo 1.26\n\nrequire (\n" + strings.Join(requires, "\n") + "\n)\n\n" + strings.Join(replaces, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(testDir, "go.mod"), []byte(mod), 0o600); err != nil {
		t.Fatal(err)
	}
	src := "package cleanpair\n\nimport (\n\t\"testing\"\n\n" + strings.Join(imports, "\n") + "\n)\n\n" + strings.Join(bodies, "\n")
	if err := os.WriteFile(filepath.Join(testDir, "clean_test.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", "-run", "TestClean", ".")
	cmd.Dir = testDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("R19: the clean NEW-READS-OLD pair did not hold (§5.4):\n%s", out)
	}
}

// t04MergeGo folds the packet emitter's declared types (the enum, flags and
// union values the table rows alias) into the table emitter's files, so a
// scalar/enum/flags/keyed unit compiles with no sibling runtime.
func t04MergeGo(t *testing.T, files map[string][]byte, u *ir.Unit) {
	t.Helper()
	decl, err := golang.Generate(u)
	if err != nil {
		t.Fatalf("R19: golang.Generate: %v", err)
	}
	for name, data := range decl {
		if _, ok := files[name]; !ok {
			files[name] = data
		}
	}
}

func t04WriteModule(t *testing.T, dir, mod string, files map[string][]byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if filepath.Ext(name) != ".go" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(name)), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gm := fmt.Sprintf("module %s\n\ngo 1.26\n", mod)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gm), 0o600); err != nil {
		t.Fatal(err)
	}
}

// t04CleanBody is the row's own NEW-READS-OLD case, read off the row cell and
// the table emitter's generated API: the OLD identity plan writes, the NEW
// compiled plan reads, and the report must be SILENT (§5.4, §5.9 #30).
func t04CleanBody(t *testing.T, row, oa, na string) string {
	t.Helper()
	clean := `	if r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 || r.Duplicate != 0 || r.Widened != 0 {
		t.Fatalf("R19: counters moved on a clean backward read: %+v", r)
	}
	if r.Reason != "" || r.Malformed || r.LayoutHash != 0 {
		t.Fatalf("R19: a clean NEW-READS-OLD is not a refusal: %+v", r)
	}`
	var checks string
	switch row {
	case "field_append":
		checks = fmt.Sprintf(`	if got[0].X != 11 || got[0].Y != 22 || got[0].Z != 33 {
		t.Fatalf("R19 field_append: the old writer's values did not land: %%+v", got[0])
	}
	if got[0].W != 77 {
		t.Fatalf("R19 field_append: the appended field is not its declared default: %%+v", got[0])
	}`)
	case "enum_append":
		checks = fmt.Sprintf(`	if got[0].Tier != %s.TierGold {
		t.Fatalf("R19 enum_append: the old variant did not land: %%v", got[0].Tier)
	}
	if got[0].Seq != 9 {
		t.Fatalf("R19 enum_append: the scalar after the enum moved: %%+v", got[0])
	}`, na)
	case "flags_append":
		checks = fmt.Sprintf(`	if got[0].Caps != %s.CapsJump|%s.CapsCrouch {
		t.Fatalf("R19 flags_append: the old mask did not land: %%b", got[0].Caps)
	}
	if got[0].Seq != 5 {
		t.Fatalf("R19 flags_append: the scalar after the flags moved: %%+v", got[0])
	}`, na, na)
	case "keyed_array_enum_append":
		checks = fmt.Sprintf(`	if got[0].Slots[0].N != 100 || got[0].Slots[1].N != 101 || got[0].Slots[2].N != 102 {
		t.Fatalf("R19 keyed_array_enum_append: the old slots did not land: %%+v", got[0].Slots)
	}
	if got[0].Slots[3].N != 7 {
		t.Fatalf("R19 keyed_array_enum_append: the appended keyed slot is not its declared default: %%+v", got[0].Slots)
	}
	if got[0].Seq != 3 {
		t.Fatalf("R19 keyed_array_enum_append: the scalar after the array moved: %%+v", got[0])
	}`)
	}
	// The fill/seed is the same shape for every row: the OLD unit constructs and
	// saves, the NEW unit loads through the lineage.
	seed := map[string]string{
		"field_append":            fmt.Sprintf("o := %s.Lineage{}\n\to.X = 11\n\to.Y = 22\n\to.Z = 33", oa),
		"enum_append":             fmt.Sprintf("o := %s.Lineage{}\n\to.Tier = %s.TierGold\n\to.Seq = 9", oa, oa),
		"flags_append":            fmt.Sprintf("o := %s.Lineage{}\n\to.Caps = %s.CapsJump | %s.CapsCrouch\n\to.Seq = 5", oa, oa, oa),
		"keyed_array_enum_append": fmt.Sprintf("o := %s.Lineage{}\n\to.Slots[0].N = 100\n\to.Slots[1].N = 101\n\to.Slots[2].N = 102\n\to.Seq = 3", oa),
	}[row]
	return fmt.Sprintf(`func TestClean_%s(t *testing.T) {
	%s
	buf := make([]byte, %s.LineageFixedMeasure(1))
	if n := %s.LineageFixedSave([]%s.Lineage{o}, buf); n != int64(len(buf)) {
		t.Fatalf("R19 %s: the old writer did not save its own layout: n=%%d want %%d", n, len(buf))
	}
	got := make([]%s.Lineage, 1)
	var r %s.TableReport
	plan := make([]%s.TableFixedEntry, 4096)
	if n := %s.LineageFixedLoad(got, buf, plan, &r); n != 1 {
		t.Fatalf("R19 %s: the newer reader refused the older writer's file: n=%%d %%+v", n, r)
	}
%s
%s
}
`, row, seed, oa, oa, oa, row, na, na, na, na, row, clean, checks)
}

// ---- go/E9 ------------------------------------------------------------------

// t04GoE9 holds E9 to the report's own word: "Fixed-class wire fields do not
// raise it ... A wire read always leaves it zero" (the Duplicate field's
// comment, §5.4). The fixed module never writes report.Duplicate and never
// carries a duplicate refusal reason.
func t04GoE9(t *testing.T) {
	src := t02GoEmit(t, unitFrom(t, t04Flat))
	if !strings.Contains(src, "Duplicate int32") {
		t.Fatal("E9: the report carries no Duplicate counter to hold at zero")
	}
	for _, bad := range []string{"Duplicate++", "report.Duplicate =", "Duplicate++"} {
		if strings.Contains(src, bad) {
			t.Errorf("E9: the fixed module raises the duplicate counter (%q); a wire read leaves it zero", bad)
		}
	}
	if strings.Contains(src, `"duplicate"`) {
		t.Error("E9: the fixed module carries a duplicate refusal reason; the fixed form never raises one")
	}
}

// ---- go/R16 -----------------------------------------------------------------

// t04GoR16 asserts §5.4's counters exactly. The COMPILE census is once per peer
// and never per record: §5.4's "once per peer and never per record" is emitted as
// `censusUnknown, censusKind = lane.Unknown, lane.KindMismatch` (one lane read)
// and ONE `report.Unknown += censusUnknown` after the record loop, so no record
// and no second read of the same peer moves it. `widened` is once per entry per
// record: the widen and float-widen op cases each carry exactly one
// `report.Widened++` inside the per-entry loop, and §5.4's "a folded element run
// is ONE" is the copy/present fold (`tableFixedRuns` folds only
// `tableFixedCopy`/`tableFixedBool`), so a widen run is never folded. `count`
// and `text` clamp once per entry. AND THE TITLE'S LAST CLAUSE: §5.4 says
// "`copy`, `const`, `present`, `ordinal` ... the op lands its value and moves
// nothing; the remap to `None` is the BOUNDS PASS's to count, on BOTH plans"
// (docs/FIXED-FORM-ALGORITHM.md §5.4, fix 27). The `ordinal` and `const` op
// cases therefore carry NO `report.Clamped++`; the forged ordinal/union tag is
// counted by `tableFixedForgedBounds`, the bounds pass the compiled read runs
// over the record's source, while the identity plan — which carries no
// `ordinal` or unguarded `const` entry — has its own forged tag counted by the
// storage clamp (§5.8 row 12, schema#1254).
func t04GoR16(t *testing.T) {
	run := funcSource(tableFixedRuntime, "func tableFixedRun(")
	if run == "" {
		t.Fatal("R16: tableFixedRun is not emitted")
	}
	if n := strings.Count(run, "report.Widened++"); n != 2 {
		t.Errorf("R16: the read loop widens at %d sites, want the one widen op and the one float widen — once per entry per record", n)
	}
	if n := strings.Count(run, "report.KindMismatch++"); n != 0 {
		t.Errorf("R16: the record loop counts KindMismatch %d times; that census is COMPILE's, once per peer", n)
	}
	refusal := funcSource(tableFixedRuntime, "func tableFixedTextRefusal(")
	if refusal == "" {
		t.Fatal("R16: the total bounds/text pre-scan is not emitted")
	}
	for _, want := range []string{"case tableFixedCount:", "return TableFixedReasonCountOverBound", "case tableFixedText:", "return TableFixedReasonLengthOverBound"} {
		if !strings.Contains(refusal, want) {
			t.Errorf("R16: pre-scan does not carry %q", want)
		}
	}
	if !strings.Contains(run, "if why := tableFixedTextRefusal(plan, count, src); why != \"\" {") {
		t.Error("R16: direct runner does not pre-scan before writes")
	}
	// THE OPS MOVE NOTHING (§5.4): the ordinal and const ops land their value and
	// count no clamp; the forged ordinal's count is the bounds pass's. The const
	// case is last under the switch, so its block runs to the function's return.
	for _, op := range [][2]string{
		{"case tableFixedOrdinal:", "case tableFixedWiden:"},
		{"case tableFixedConst:", "return \"\""},
	} {
		if strings.Contains(t02GoBlock(t, run, op[0], op[1]), "report.Clamped++") {
			t.Errorf("R16: %s counts clamped in the op; §5.4 puts the forged count in the bounds pass, not the op", op[0])
		}
	}
	if !strings.Contains(tableFixedRuntime, "func tableFixedForgedBounds(") {
		t.Error("R16: the bounds pass that counts a forged ordinal is not emitted")
	}
	load := t02GoNorm(funcSource(t02GoEmit(t, unitFrom(t, t04Flat)), "func TFixedLoad("))
	if !strings.Contains(load, "tableFixedForgedBounds(entries, entryCount, at[8:recordBytes], report)") {
		t.Error("R16: the compiled read does not run the bounds pass that counts a forged ordinal")
	}
	if n := strings.Count(load, "report.Unknown += censusUnknown"); n != 1 {
		t.Errorf("R16: the load adds the unknown census %d times, want once per peer", n)
	}
	if n := strings.Count(load, "censusUnknown, censusKind = lane.Unknown, lane.KindMismatch"); n != 1 {
		t.Errorf("R16: the census lane is read %d times, want once per peer", n)
	}
}

// ---- go/R18 -----------------------------------------------------------------

// t04GoR18 holds R18 to §5's clamp emission: a clamp that cannot fire is not
// emitted and nothing moves. A table that declares nothing bounded has no
// TableFixedClampNeeded and no clamp function or call in its module; a ranged
// table has both, and the pass adds what it clamped to the report once.
func t04GoR18(t *testing.T) {
	unbounded := unitFrom(t, t04Flat)
	if ir.TableFixedClampNeeded(unbounded.Tables["T"]) {
		t.Error("R18: an unbounded table reports a clamp is needed")
	}
	usrc := t02GoEmit(t, unbounded)
	if strings.Contains(usrc, "TFixedClamp") {
		t.Error("R18: a clamp that cannot fire is emitted for an unbounded table")
	}
	if strings.Contains(t02GoNorm(funcSource(usrc, "func TFixedLoad(")), "TFixedClamp(") {
		t.Error("R18: the load calls a clamp for a table that declares nothing bounded")
	}

	ranged := unitFrom(t, t04Ranged)
	if !ir.TableFixedClampNeeded(ranged.Tables["T"]) {
		t.Error("R18: a ranged table reports no clamp is needed")
	}
	rsrc := t02GoEmit(t, ranged)
	if !strings.Contains(rsrc, "func TFixedClamp(") {
		t.Error("R18: a ranged table emitted no clamp pass")
	}
	if !strings.Contains(t02GoNorm(funcSource(rsrc, "func TFixedLoad(")), "TFixedClamp(&values[k], report)") {
		t.Error("R18: the load does not run the clamp pass for a ranged table")
	}
	if !strings.Contains(rsrc, "report.Clamped += clamped") {
		t.Error("R18: the clamp pass does not move the report once with what it clamped")
	}
}
