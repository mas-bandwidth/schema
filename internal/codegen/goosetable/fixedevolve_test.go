package goosetable

// THE FIXED FORM'S EVOLUTION (docs/FIXED-FORM-VERSIONING-TESTS.md;
// docs/FIXED-FORM-ALGORITHM.md §5): a NEW build reads an OLD file through the
// lineage it was handed, and every refusal carries the Go engine's own name.
//
// The corpus files are the C++ reference's (make tables-fixedform-corpus) and
// the schema pairs are the ones it was dumped from, so the bytes under test
// are the reference's and the counters are docs/FIXED-FORM-VERSIONING-TESTS.md
// §5.4's. The reader is the NEW generation; the OLD layout is handed in with
// GenerateLineage, the lock played in one line.

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// evolutionProgram emits the NEW unit's surfaces and a main that reads each
// input through <Root>FixedEvolveLoad, printing the verdict and the counters,
// then the record's fields through print (a Goose expression list).
func evolutionProgram(t *testing.T, u *evolutionUnit, root string, dir string, capacity int, inputs []string, print []string) string {
	t.Helper()
	files, err := goose.Generate(u.unit)
	if err != nil {
		t.Fatalf("goose type surface: %v", err)
	}
	tables, err := GenerateLineage(u.unit, u.lineage)
	if err != nil {
		t.Fatalf("goose table surface: %v", err)
	}
	maps.Copy(files, tables)
	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	sort.Strings(mods)
	ns := goose.Namespace(u.unit)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	b.WriteString("\n" + gooserun.HexProbe + "\n")
	fmt.Fprintf(&b, "fn probe(name: const u8[:]) {\n")
	b.WriteString("    var bytes: u8[>..] = [];\n    if !read_file(name, bytes) { print(\"read-failed\"); return; }\n")
	fmt.Fprintf(&b, "    var values: %s::%s[%d] = [%s::%s { .. }; %d];\n", ns, root, capacity, ns, root, capacity)
	b.WriteString("    var report = schema_rt::FixedEvolveReport { .. };\n")
	fmt.Fprintf(&b, "    let n = %s::%sFixedEvolveLoad(values[0..%d], bytes, report);\n", ns, root, capacity)
	b.WriteString("    print(n, \" \", schema_rt::fixed_evolve_refusal_name(report.reason), \" \", report.clamped, \" \", report.widened, \" \", report.unknown);\n")
	b.WriteString("    if n < 0 { return; }\n")
	for _, p := range print {
		fmt.Fprintf(&b, "    print(\" \", %s);\n", p)
	}
	b.WriteString("}\n\nfn main() {\n")
	for _, in := range inputs {
		fmt.Fprintf(&b, "    probe(%q);\n", in)
	}
	b.WriteString("}\n")
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return "main.goose"
}

// evolutionUnit is a unit and the lineage the build is handed for it.
type evolutionUnit struct {
	unit    *ir.Unit
	lineage map[string][]FixedEvolutionEntry
}

func evolutionLoad(t *testing.T, schema string) *ir.Unit {
	t.Helper()
	path := filepath.Join(fixedRoot, filepath.FromSlash(schema))
	u, err := loadSchemaPaths([]string{path})
	if err != nil {
		t.Fatalf("load %s: %v", schema, err)
	}
	return u
}

// joinedProbe folds a probe's one input into one line: the verdict line then
// the field lines it printed (one print per field).
func joinedProbe(t *testing.T, out []byte) string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) == 0 {
		t.Fatalf("the probe printed nothing")
	}
	var parts []string
	for _, line := range lines {
		parts = append(parts, strings.TrimSpace(line))
	}
	return strings.Join(parts, " ")
}

// evolutionLines splits a probe's output into its one line per input.
func evolutionLines(t *testing.T, out []byte, want int) []string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != want {
		t.Fatalf("the probe printed %d lines, want %d:\n%s", len(lines), want, out)
	}
	return lines
}

// TestGooseFixedFormNamesEveryRefusal holds every refusal the evolution load
// can answer to the Go engine's own name (internal/codegen/gotable's
// TableRefuseReason): a hash in no entry, a retired entry below the floor, a
// known hash whose bytes differ, a damaged framing, and a short capacity.
func TestGooseFixedFormNamesEveryRefusal(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	old := evolutionLoad(t, "test/tables/VOLD_field_append.schema")
	entry, ok := FixedEvolutionOf(old, "Lineage")
	if !ok {
		t.Fatal("no fixed root Lineage in the old schema")
	}
	data, err := os.ReadFile(filepath.Join(corpus, "old_field_append.bin"))
	if err != nil {
		t.Fatal(err)
	}
	newer := evolutionLoad(t, "test/tables/VNEW_field_append.schema")
	live := evolutionUnit{unit: newer, lineage: map[string][]FixedEvolutionEntry{"Lineage": {entry}}}
	retiredEntry := entry
	retiredEntry.Retired = true
	retiredEntry.Reason = "the test retires the old layout"
	retired := evolutionUnit{unit: newer, lineage: map[string][]FixedEvolutionEntry{"Lineage": {retiredEntry}}}

	unknown := append([]byte(nil), data...)
	unknown[8] ^= 0x01
	malformed := append([]byte(nil), data...)
	// A layout byte under a KNOWN hash is layout_malformed (§1.1).
	malformed[20+4] ^= 0x01
	reserved := append([]byte(nil), data...)
	reserved[1] = 1
	truncated := data[:10]
	previous := append([]byte(nil), data...)
	previous[0] = 1
	newerForm := append([]byte(nil), data...)
	newerForm[0] = 9
	messageForm := append([]byte(nil), data...)
	messageForm[0] = 2
	noLayout := append([]byte(nil), data...)
	noLayout[20+len(entry.Layout)] ^= 0x01
	short := append(append([]byte(nil), data...), data[len(data)-20:]...)

	dir := t.TempDir()
	inputs := []string{"unknown.bin", "malformed.bin", "reserved.bin", "truncated.bin", "previous.bin", "newer.bin", "message.bin", "nolayout.bin"}
	main := evolutionProgram(t, &live, "Lineage", dir, 1, inputs, nil)
	lines := evolutionLines(t, gooserun.Run(t, dir, main, map[string][]byte{
		"unknown.bin": unknown, "malformed.bin": malformed, "reserved.bin": reserved, "truncated.bin": truncated,
		"previous.bin": previous, "newer.bin": newerForm, "message.bin": messageForm, "nolayout.bin": noLayout,
	}), len(inputs))

	retireDir := t.TempDir()
	retireMain := evolutionProgram(t, &retired, "Lineage", retireDir, 1, []string{"old.bin"}, nil)
	retireLine := evolutionLines(t, gooserun.Run(t, retireDir, retireMain, map[string][]byte{"old.bin": data}), 1)

	shortDir := t.TempDir()
	shortMain := evolutionProgram(t, &live, "Lineage", shortDir, 1, []string{"old.bin"}, nil)
	shortLine := evolutionLines(t, gooserun.Run(t, shortDir, shortMain, map[string][]byte{"old.bin": short}), 1)

	want := []struct {
		line, reason string
	}{
		{lines[0], "layout_unknown"},
		{lines[1], "layout_malformed"},
		{lines[2], "reserved_not_zero"},
		{lines[3], "truncated"},
		{lines[4], "previous_form"},
		{lines[5], "newer_form"},
		{lines[6], "message_form_as_file"},
		{lines[7], "no_layout"},
		{retireLine[0], "layout_unsupported"},
		{shortLine[0], "batch_too_large"},
	}
	for i, w := range want {
		fields := strings.Fields(w.line)
		if len(fields) < 2 || fields[1] != w.reason {
			t.Fatalf("refusal %d answered %q, want the Go name %q", i, w.line, w.reason)
		}
		t.Logf("refusal %d: %s", i, w.reason)
	}
}

// TestGooseFixedFormKindMismatchCensus pins the other half of §5.4's census:
// a named field whose KIND moved outside the widening ladder is dropped and
// counted once per FIELD per peer — like unknown, never once per array element.
// The pair is deliberately unlawful (the lock refuses it), so the lineage
// entry is handed in from the OLD build, exactly as unknown_census does it.
func TestGooseFixedFormKindMismatchCensus(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	old := evolutionLoad(t, "test/tables/VOLD_unknown_census.schema")
	entry, ok := FixedEvolutionOf(old, "Census")
	if !ok {
		t.Fatal("no fixed root Census in the old schema")
	}
	dirSchema := t.TempDir()
	path := filepath.Join(dirSchema, "VNEW_kind_mismatch.schema")
	const text = `package vnew_kind_mismatch

fixed table Item
{
    a    int32 = 0
    drop string(8)
}

fixed table Census
{
    lead  uint32 = 1
    items [4]Item
    trail uint32 = 2
}
`
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	newer, err := loadSchemaPaths([]string{path})
	if err != nil {
		t.Fatalf("load kind_mismatch: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(corpus, "old_unknown_census.bin"))
	if err != nil {
		t.Fatal(err)
	}
	u := evolutionUnit{unit: newer, lineage: map[string][]FixedEvolutionEntry{"Census": {entry}}}
	out := t.TempDir()
	main := evolutionProgram(t, &u, "Census", out, 1, []string{"old.bin"}, []string{
		"values[0].Lead", "values[0].Items[0].A", "values[0].Items[1].A",
		"values[0].Items[2].A", "values[0].Items[3].A", "values[0].Trail", "report.kind_mismatch",
	})
	got := joinedProbe(t, gooserun.Run(t, out, main, map[string][]byte{"old.bin": data}))
	const want = "1 ok 0 0 0 1 10 11 12 13 2 1"
	if got != want {
		t.Fatalf("kind_mismatch: the NEW reader answered %q, want %q", got, want)
	}
	t.Logf("kind_mismatch: %s", got)
}

func refuseSchema(t *testing.T, src string) string {
	t.Helper()
	f, perrs := parser.Parse("T.schema", []byte(src))
	if len(perrs) > 0 {
		return perrs[0].Error()
	}
	_, cerrs := check.Unit([]check.SourceFile{{
		Path: "T.schema", Name: "T.schema", Base: "T", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) == 0 {
		t.Fatalf("expected schema to be refused, but it compiled cleanly:\n%s", src)
	}
	return cerrs[0].Error()
}

func makeFixedFile(hash uint64, layout []byte, bodies ...[]byte) []byte {
	var out []byte
	out = append(out, 3, 0, 0, 0, 0, 0, 0, 0)
	out = binary.LittleEndian.AppendUint64(out, hash)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(layout)))
	out = append(out, layout...)
	for _, b := range bodies {
		out = binary.LittleEndian.AppendUint64(out, hash)
		out = append(out, b...)
	}
	return out
}

// TestGooseFixedClosure proves port/goose/fixed/fixed-closure (go/R6, go/R14,
// go/R22, go/W10):
//   - R6: a declared fixed table past the 65536-byte ceiling warns (not errors),
//     emits no fixed-form surface, and ignores any lineage entry handed in.
//   - R14 & W10: zero-size root, >65536-byte root/entry/child-sum, and any
//     entry whose source reach exceeds oldAll[0].size refuse the plan whole
//     with layout_record_too_large (while an entry ending right at oldAll[0].size
//     loads cleanly).
//   - R22: nested fixed table in closure is valid and round-trips; variable
//     table, pointer, map, unbounded array, and guarded/recursive closure breaks
//     are refused at compile time naming the construct and docs/SPEC-TABLES.md.
func TestGooseFixedClosure(t *testing.T) {
	t.Parallel()

	t.Run("R6", func(t *testing.T) {
		t.Parallel()
		const src = `package probe

fixed table Big
{
    blob bytes(70000)
}

fixed table Small
{
    x int32 = 7
}
`
		u := parseCheckUnit(t, src)
		warns, errs := ir.TableFixedRecordBounds(u, 0)
		if len(errs) != 0 {
			t.Fatalf("R6: 70000-byte fixed table produced hard error without --fixed-record-limit: %v", errs)
		}
		if len(warns) != 1 || !strings.Contains(warns[0], "Big") || !strings.Contains(warns[0], "65536") || !strings.Contains(warns[0], "THE FIXED FORM IS NOT EMITTED FOR IT") {
			t.Fatalf("R6: expected ceiling warning for Big, got %v", warns)
		}
		// Negative control: with --fixed-record-limit=65536, it IS a hard error.
		_, limitErrs := ir.TableFixedRecordBounds(u, 65536)
		if len(limitErrs) != 1 || !strings.Contains(limitErrs[0].Error(), "Big") {
			t.Fatalf("R6: expected hard error with limit=65536, got %v", limitErrs)
		}
		if _, ok := FixedEvolutionOf(u, "Big"); ok {
			t.Fatalf("R6: FixedEvolutionOf succeeded for over-ceiling table Big")
		}
		smallEntry, ok := FixedEvolutionOf(u, "Small")
		if !ok {
			t.Fatalf("R6: FixedEvolutionOf failed for Small")
		}
		// Pass a lineage entry for Big anyway; GenerateLineage must ignore it and emit no fixed surface for Big.
		tables, err := GenerateLineage(u, map[string][]FixedEvolutionEntry{
			"Big":   {smallEntry},
			"Small": {smallEntry},
		})
		if err != nil {
			t.Fatalf("R6: GenerateLineage: %v", err)
		}
		code := string(tables["ProbeTable.goose"])
		for _, forbidden := range []string{"BigFixedSave", "BigFixedLoad", "BigFixedEvolveLoad", "BigFixedBodyBytes"} {
			if strings.Contains(code, forbidden) {
				t.Fatalf("R6: emitted fixed-form symbol %s for over-ceiling table Big", forbidden)
			}
		}
		for _, required := range []string{"SmallFixedSave", "SmallFixedLoad", "SmallFixedEvolveLoad"} {
			if !strings.Contains(code, required) {
				t.Fatalf("R6: missing %s for Small", required)
			}
		}
		out := runCustomFixedGoose(t, u, nil, `
fn main() {
    var s = probe::Small { X: 42 };
    var buf: u8[>..] = [];
    let n = probe::SmallFixedSave([s], buf);
    var got: probe::Small[1] = [probe::Small { .. }; 1];
    var rep = schema_rt::FixedReport { .. };
    let loaded = probe::SmallFixedLoad(got, buf, rep);
    print(n > 0, " ", loaded, " ", schema_rt::fixed_refusal_name(rep.reason), " ", got[0].X);
}
`, nil)
		if out != "true 1 ok 42" {
			t.Fatalf("R6 Small probe got %q, want \"true 1 ok 42\"", out)
		}
	})

	t.Run("R14_W10", func(t *testing.T) {
		t.Parallel()
		const schema = `package probe

fixed table Target
{
    lead  uint32 = 1
    name  string(8) = "ok"
    trail uint32 = 2
}

fixed table Boundary
{
    lead uint32 = 1
    name string(8) = "ok"
}
`
		u := parseCheckUnit(t, schema)
		baseEntry, ok := FixedEvolutionOf(u, "Target")
		if !ok {
			t.Fatal("missing Target fixed root")
		}
		boundEntry, ok := FixedEvolutionOf(u, "Boundary")
		if !ok {
			t.Fatal("missing Boundary fixed root")
		}

		// Positive boundary control: Boundary has lead (0..4) and name string(8) (4..16), ending right at root.size == 16.
		// Mutate boundEntry's hash so it enters lineage pick=1 instead of pick=0.
		boundOld := boundEntry
		boundOld.Wire ^= 0x1111
		var boundBody [16]byte
		binary.LittleEndian.PutUint32(boundBody[0:4], 9)
		binary.LittleEndian.PutUint32(boundBody[4:8], 8)
		copy(boundBody[8:16], "edgecase")
		boundFile := makeFixedFile(boundOld.Wire, boundOld.Layout, boundBody[:])

		// Construct 5 layout_record_too_large entries for Target:
		// 1. zeroRoot: entry 0 size == 0
		eZeroRoot := baseEntry
		eZeroRoot.Wire = baseEntry.Wire ^ 0x0101
		eZeroRoot.Layout = append([]byte(nil), baseEntry.Layout...)
		binary.LittleEndian.PutUint32(eZeroRoot.Layout[4+9:4+13], 0)

		// 2. hugeRoot: entry 0 size == 65537
		eHugeRoot := baseEntry
		eHugeRoot.Wire = baseEntry.Wire ^ 0x0202
		eHugeRoot.Layout = append([]byte(nil), baseEntry.Layout...)
		binary.LittleEndian.PutUint32(eHugeRoot.Layout[4+9:4+13], 65537)

		// 3. hugeField: child entry 1 size == 65537 while root size == 65536
		eHugeField := baseEntry
		eHugeField.Wire = baseEntry.Wire ^ 0x0303
		eHugeField.Layout = append([]byte(nil), baseEntry.Layout...)
		binary.LittleEndian.PutUint32(eHugeField.Layout[4+9:4+13], 65536)
		binary.LittleEndian.PutUint32(eHugeField.Layout[4+17+9:4+17+13], 65537)

		// 4. childSumHuge: root size == 65536, child 1 size == 40000, child 2 size == 30000 (sum 70000 > 65536)
		eChildSum := baseEntry
		eChildSum.Wire = baseEntry.Wire ^ 0x0404
		eChildSum.Layout = append([]byte(nil), baseEntry.Layout...)
		binary.LittleEndian.PutUint32(eChildSum.Layout[4+9:4+13], 65536)
		binary.LittleEndian.PutUint32(eChildSum.Layout[4+17+9:4+17+13], 40000)
		binary.LittleEndian.PutUint32(eChildSum.Layout[4+34+9:4+34+13], 30000)

		// 5. reachPastBody (W10): root size == 15 while children (lead 4B + name 12B + trail 4B = 20B) reach past root.size.
		eReach := baseEntry
		eReach.Wire = baseEntry.Wire ^ 0x0505
		eReach.Layout = append([]byte(nil), baseEntry.Layout...)
		binary.LittleEndian.PutUint32(eReach.Layout[4+9:4+13], 15)

		lineage := map[string][]FixedEvolutionEntry{
			"Boundary": {boundOld},
			"Target":   {eZeroRoot, eHugeRoot, eHugeField, eChildSum, eReach},
		}
		var zeroBody [20]byte
		files := map[string][]byte{
			"bound.bin":     boundFile,
			"zeroroot.bin":  makeFixedFile(eZeroRoot.Wire, eZeroRoot.Layout, zeroBody[:]),
			"hugeroot.bin":  makeFixedFile(eHugeRoot.Wire, eHugeRoot.Layout, zeroBody[:]),
			"hugefield.bin": makeFixedFile(eHugeField.Wire, eHugeField.Layout, zeroBody[:]),
			"childsum.bin":  makeFixedFile(eChildSum.Wire, eChildSum.Layout, zeroBody[:]),
			"reach.bin":     makeFixedFile(eReach.Wire, eReach.Layout, zeroBody[:]),
		}
		out := runCustomFixedGoose(t, u, lineage, `
fn check_target(name: const u8[:]) {
    var bytes: u8[>..] = [];
    if !read_file(name, bytes) { print("read-failed"); return; }
    var values: probe::Target[1] = [probe::Target { Lead: 777, Name: "sentinel", Trail: 888 }; 1];
    var rep = schema_rt::FixedEvolveReport { .. };
    let n = probe::TargetFixedEvolveLoad(values, bytes, rep);
    let untouched = values[0].Lead == 777 && values[0].Name == "sentinel" && values[0].Trail == 888;
    print(name, " ", n, " ", schema_rt::fixed_evolve_refusal_name(rep.reason), " ", untouched);
}

fn main() {
    var bbytes: u8[>..] = [];
    if !read_file("bound.bin", bbytes) { print("read-failed"); return; }
    var bvals: probe::Boundary[1] = [probe::Boundary { .. }; 1];
    var brep = schema_rt::FixedEvolveReport { .. };
    let bn = probe::BoundaryFixedEvolveLoad(bvals, bbytes, brep);
    print("bound.bin ", bn, " ", schema_rt::fixed_evolve_refusal_name(brep.reason), " ", bvals[0].Lead, " ", bvals[0].Name);

    check_target("zeroroot.bin");
    check_target("hugeroot.bin");
    check_target("hugefield.bin");
    check_target("childsum.bin");
    check_target("reach.bin");
}
`, files)
		wantLines := []string{
			"bound.bin 1 ok 9 edgecase",
			"zeroroot.bin -1 layout_record_too_large true",
			"hugeroot.bin -1 layout_record_too_large true",
			"hugefield.bin -1 layout_record_too_large true",
			"childsum.bin -1 layout_record_too_large true",
			"reach.bin -1 layout_record_too_large true",
		}
		if out != strings.Join(wantLines, "\n") {
			t.Fatalf("R14_W10 output:\n got:\n%s\nwant:\n%s", out, strings.Join(wantLines, "\n"))
		}
	})

	t.Run("R22", func(t *testing.T) {
		t.Parallel()
		// Positive control: a fixed table embedding another fixed table by value, array, and optional.
		const posSchema = `package probe

fixed table Inner
{
    a int32 = 10
    b string(4) = "hi"
}

fixed table Outer
{
    head  uint32 = 1
    inner Inner
    items [2]Inner
    maybe ?Inner
    tail  uint32 = 2
}
`
		u := parseCheckUnit(t, posSchema)
		out := runCustomFixedGoose(t, u, nil, `
fn main() {
    var v = probe::Outer {
        Head: 7,
        Inner: probe::Inner { A: 11, B: [111, 110, 101, 0], BLength: 3 },
        Items: [
            probe::Inner { A: 22, B: [116, 119, 111, 0], BLength: 3 },
            probe::Inner { A: 33, B: [115, 105, 120, 0], BLength: 3 },
        ],
        MaybePresent: true,
        Maybe: probe::Inner { A: 44, B: [111, 112, 116, 0], BLength: 3 },
        Tail: 9,
    };
    var bytes: u8[>..] = [];
    let saved = probe::OuterFixedSave([v], bytes);
    var got: probe::Outer[1] = [probe::Outer { .. }; 1];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::OuterFixedLoad(got, bytes, rep);
    var ev: probe::Outer[1] = [probe::Outer { .. }; 1];
    var erep = schema_rt::FixedEvolveReport { .. };
    let en = probe::OuterFixedEvolveLoad(ev, bytes, erep);
    let ok = got[0].Head == 7 && got[0].Inner.A == 11 && got[0].Inner.BLength == 3 && got[0].Inner.B[0] == 111 &&
        got[0].Items[1].A == 33 && got[0].Items[1].BLength == 3 && got[0].Items[1].B[0] == 115 &&
        got[0].MaybePresent && got[0].Maybe.A == 44 && got[0].Tail == 9 &&
        en == 1 && ev[0].Inner.BLength == 3 && ev[0].Inner.B[0] == 111;
    print(saved > 0, " ", n, " ", schema_rt::fixed_refusal_name(rep.reason), " ", ok);
}
`, nil)
		if out != "true 1 ok true" {
			t.Fatalf("R22 positive control got %q, want \"true 1 ok true\"", out)
		}

		// Negative controls: closure breaks inside a declared fixed table are refused at compile time.
		for _, tc := range []struct {
			name, field, want, src string
		}{
			{
				name:  "nested_variable_table",
				field: "Outer.child",
				want:  "holds the variable table VarChild by value",
				src:   "package probe\ntable VarChild { x int32 }\nfixed table Outer { child VarChild }\n",
			},
			{
				name:  "pointer_in_closure",
				field: "Outer.ptr",
				want:  "is a pointer",
				src:   "package probe\ntable Node { x int32 }\nfixed table Outer { ptr *Node }\n",
			},
			{
				name:  "map_in_closure",
				field: "Outer.m",
				want:  "is a map",
				src:   "package probe\nfixed table Outer { m map[uint32]int32 }\n",
			},
			{
				name:  "unbounded_array_in_closure",
				field: "Outer.arr",
				want:  "is an unbounded array",
				src:   "package probe\nfixed table Outer { arr []int32 }\n",
			},
			{
				name:  "guarded_branch_in_type_closure",
				field: "Sub.extra",
				want:  "sits in an `if` branch",
				src:   "package probe\ntype Sub {\n    gate bool\n    if gate {\n        extra int32\n    }\n}\nfixed table Outer { sub Sub }\n",
			},
		} {
			msg := refuseSchema(t, tc.src)
			if !strings.Contains(msg, tc.field) || !strings.Contains(msg, tc.want) || !strings.Contains(msg, "docs/SPEC-TABLES.md") {
				t.Fatalf("R22(%s): unexpected refusal %q (want %q, %q, docs/SPEC-TABLES.md)", tc.name, msg, tc.field, tc.want)
			}
		}
	})
}

// TestGooseFixedCompiledPlans proves port/goose/fixed/compiled-plans (go/R1,
// go/R2, go/R23, go/R25, go/R26, go/W14) under the per-language-design-proof
// ruling:
//   - R1: GenerateLineage emits static constants and straight-line code at build
//     time in oldest-first order with current layout (pick = 0) baked alongside,
//     never parsing layouts or compiling plans at runtime.
//   - R2: record_bytes = 8 + body is added once; FixedEvolutionOf sets Record = 8 + body
//     and FixedEvolveLoad uses en.Record directly.
//   - R23: static root constants (FixedBodyBytes, FixedRecordBytes, FixedHash,
//     FixedLayout, FixedLayoutLength) and FixedReport/FixedEvolveReport with
//     hash: u64 as the last member, zeroed on entry and populated only on
//     layout_unknown and layout_unsupported.
//   - R25 & R26: caller capacity check (batch_too_large) and per-entry refusal
//     lane (FixedEvolveRefuse<k>) carrying layout_malformed, layout_record_too_large,
//     or plan_too_large, refusing by name and never panicking.
//   - W14: constant-offset scatter/gather in _<T>FixedRead, _<T>FixedWrite, and
//     compileEvolvePlan matching ir.TableFixedMemberOffset and ir.TableFixedTypeBytes.
func TestGooseFixedCompiledPlans(t *testing.T) {
	t.Parallel()

	t.Run("R1_R2_R23", func(t *testing.T) {
		t.Parallel()
		const schemaV1 = `package probe

fixed table Lineage
{
    a uint16 = 1
}
`
		const schemaV2 = `package probe

fixed table Lineage
{
    a uint32 = 1
    b int32 = 10
}
`
		const schemaV3 = `package probe

fixed table Lineage
{
    a uint32 = 1
    b int64 = 10
    c int32 = 99
}
`
		u1 := parseCheckUnit(t, schemaV1)
		u2 := parseCheckUnit(t, schemaV2)
		u3 := parseCheckUnit(t, schemaV3)

		e1, ok := FixedEvolutionOf(u1, "Lineage")
		if !ok {
			t.Fatal("missing Lineage v1")
		}
		e2, ok := FixedEvolutionOf(u2, "Lineage")
		if !ok {
			t.Fatal("missing Lineage v2")
		}
		e3, ok := FixedEvolutionOf(u3, "Lineage")
		if !ok {
			t.Fatal("missing Lineage v3")
		}

		// R2: FixedEvolutionOf adds 8 once (Record == 8 + body).
		st1 := fixedRootStruct(t, u1, "Lineage")
		st2 := fixedRootStruct(t, u2, "Lineage")
		st3 := fixedRootStruct(t, u3, "Lineage")
		if e1.Record != 8+ir.TableFixedTypeBytes(st1) || e2.Record != 8+ir.TableFixedTypeBytes(st2) || e3.Record != 8+ir.TableFixedTypeBytes(st3) {
			t.Fatalf("R2: Record != 8 + body: e1=%d e2=%d e3=%d", e1.Record, e2.Record, e3.Record)
		}

		// Also include a retired entry e0 before e1 to prove layout_unsupported sets report.hash.
		e0 := e1
		e0.Wire = e1.Wire ^ 0xbeef
		e0.Retired = true
		e0.Reason = "v0 retired"

		lineage := map[string][]FixedEvolutionEntry{
			"Lineage": {e0, e1, e2},
		}
		tables, err := GenerateLineage(u3, lineage)
		if err != nil {
			t.Fatalf("GenerateLineage: %v", err)
		}
		code := string(tables["ProbeTable.goose"])

		// R1: Verify oldest-first static constants and straight-line branches, with pick = 0 for current layout.
		idx0 := strings.Index(code, fmt.Sprintf("else if h == 0x%016x {\n        pick = 1;", e0.Wire))
		idx1 := strings.Index(code, fmt.Sprintf("else if h == 0x%016x {\n        pick = 2;", e1.Wire))
		idx2 := strings.Index(code, fmt.Sprintf("else if h == 0x%016x {\n        pick = 3;", e2.Wire))
		idxCur := strings.Index(code, "if h == LineageFixedHash {\n        pick = 0;")
		if idxCur < 0 || idx0 < 0 || idx1 < 0 || idx2 < 0 || !(idxCur < idx0 && idx0 < idx1 && idx1 < idx2) {
			t.Fatalf("R1: lineage dispatch not in oldest-first order after pick=0:\n%s", code)
		}
		// R2: Verify record_bytes uses e1.Record (10) and e2.Record (16) directly without adding 8 again.
		if !strings.Contains(code, "let LineageFixedBodyBytes: i64 = 16;") ||
			!strings.Contains(code, "let LineageFixedRecordBytes: i64 = 8 + LineageFixedBodyBytes;") ||
			!strings.Contains(code, fmt.Sprintf("pick = 2;\n        known = LineageFixedEvolveLayout2;\n        record_bytes = %d;", e1.Record)) ||
			!strings.Contains(code, fmt.Sprintf("pick = 3;\n        known = LineageFixedEvolveLayout3;\n        record_bytes = %d;", e2.Record)) {
			t.Fatalf("R2: record_bytes not emitted as 8 + body once:\n%s", code)
		}

		// Build 2-record files for v1 (body 2B each), v2 (body 8B each), and v0 (retired).
		var v1b1, v1b2 [2]byte
		binary.LittleEndian.PutUint16(v1b1[:], 7)
		binary.LittleEndian.PutUint16(v1b2[:], 8)
		fileV1 := makeFixedFile(e1.Wire, e1.Layout, v1b1[:], v1b2[:])

		var v2b1, v2b2 [8]byte
		binary.LittleEndian.PutUint32(v2b1[0:4], 100)
		binary.LittleEndian.PutUint32(v2b1[4:8], ^uint32(19))
		binary.LittleEndian.PutUint32(v2b2[0:4], 200)
		binary.LittleEndian.PutUint32(v2b2[4:8], uint32(int32(30)))
		fileV2 := makeFixedFile(e2.Wire, e2.Layout, v2b1[:], v2b2[:])

		fileV0 := makeFixedFile(e0.Wire, e0.Layout, v1b1[:])
		fileUnknown := makeFixedFile(0x123456789abcdef0, e3.Layout, make([]byte, 16))

		files := map[string][]byte{
			"v1.bin":      fileV1,
			"v2.bin":      fileV2,
			"v0.bin":      fileV0,
			"unknown.bin": fileUnknown,
		}
		out := runCustomFixedGoose(t, u3, lineage, `
fn main() {
    // R2 & R23: verify static constants on Lineage.
    let consts_ok = probe::LineageFixedBodyBytes == 16 &&
        probe::LineageFixedRecordBytes == 24 &&
        probe::LineageFixedLayoutLength == (probe::LineageFixedLayout.len as i64) &&
        probe::LineageFixedHash != 0;
    print("consts ", consts_ok);

    // Current layout (pick = 0) round-trip of 2 records.
    var cur_in: probe::Lineage[2] = [
        probe::Lineage { A: 11, B: -500, C: 77 },
        probe::Lineage { A: 22, B: 600, C: 88 },
    ];
    var cur_bytes: u8[>..] = [];
    let saved = probe::LineageFixedSave(cur_in, cur_bytes);
    var cur_out: probe::Lineage[2] = [probe::Lineage { .. }; 2];
    // Dirty report before call to prove entry zeroing (R23).
    var erep = schema_rt::FixedEvolveReport { reason: 99, clamped: 99, widened: 99, unknown: 99, kind_mismatch: 99, malformed: true, hash: 0xdeadbeef };
    let n_cur = probe::LineageFixedEvolveLoad(cur_out, cur_bytes, erep);
    print("cur ", saved == probe::LineageFixedMeasure(2), " ", n_cur, " ", schema_rt::fixed_evolve_refusal_name(erep.reason), " ", erep.widened, " ", erep.hash, " ", cur_out[0].B, " ", cur_out[1].C);

    // Oldest live layout v1 (pick = 2): 2 records, a widened u16->u32, b & c at defaults (10, 99).
    var v1_bytes: u8[>..] = [];
    if !read_file("v1.bin", v1_bytes) { print("read-failed"); return; }
    var v1_out: probe::Lineage[2] = [probe::Lineage { .. }; 2];
    erep.hash = 0xdeadbeef;
    let n_v1 = probe::LineageFixedEvolveLoad(v1_out, v1_bytes, erep);
    print("v1 ", n_v1, " ", schema_rt::fixed_evolve_refusal_name(erep.reason), " ", erep.widened, " ", erep.hash, " ", v1_out[0].A, " ", v1_out[0].B, " ", v1_out[0].C, " ", v1_out[1].A);

    // Middle layout v2 (pick = 3): 2 records, b widened i32->i64 (sign-extended), c at default 99.
    var v2_bytes: u8[>..] = [];
    if !read_file("v2.bin", v2_bytes) { print("read-failed"); return; }
    var v2_out: probe::Lineage[2] = [probe::Lineage { .. }; 2];
    let n_v2 = probe::LineageFixedEvolveLoad(v2_out, v2_bytes, erep);
    print("v2 ", n_v2, " ", schema_rt::fixed_evolve_refusal_name(erep.reason), " ", erep.widened, " ", v2_out[0].A, " ", v2_out[0].B, " ", v2_out[0].C, " ", v2_out[1].A, " ", v2_out[1].B);

    // R23: layout_unsupported (v0) and layout_unknown populate report.hash; truncated zeroes report.hash.
    var v0_bytes: u8[>..] = [];
    if !read_file("v0.bin", v0_bytes) { print("read-failed"); return; }
    let n_v0 = probe::LineageFixedEvolveLoad(v2_out, v0_bytes, erep);
    let h_v0 = erep.hash;
    let r_v0 = schema_rt::fixed_evolve_refusal_name(erep.reason);

    var unk_bytes: u8[>..] = [];
    if !read_file("unknown.bin", unk_bytes) { print("read-failed"); return; }
    let n_unk = probe::LineageFixedEvolveLoad(v2_out, unk_bytes, erep);
    let h_unk = erep.hash;
    let r_unk = schema_rt::fixed_evolve_refusal_name(erep.reason);

    // Now pass truncated bytes with dirty report.hash: report.hash must be wiped to 0!
    let n_trunc = probe::LineageFixedEvolveLoad(v2_out, cur_bytes[0..10], erep);
    print("hash_rule ", n_v0, " ", r_v0, " ", h_v0 != 0, " ", n_unk, " ", r_unk, " ", h_unk == 0x123456789abcdef0, " ", n_trunc, " ", erep.hash);
}
`, files)
		wantLines := []string{
			"consts true",
			"cur true 2 ok 0 0 -500 88",
			"v1 2 ok 2 0 7 10 99 8",
			"v2 2 ok 2 100 -20 99 200 30",
			"hash_rule -1 layout_unsupported true -1 layout_unknown true -1 0",
		}
		if out != strings.Join(wantLines, "\n") {
			t.Fatalf("R1_R2_R23 output:\n got:\n%s\nwant:\n%s", out, strings.Join(wantLines, "\n"))
		}
	})

	t.Run("R25_R26", func(t *testing.T) {
		t.Parallel()
		const src = `package probe

enum Mode
{
    A
    B
}

fixed table Gate
{
    mode Mode = A
    val  int32 = 5
}
`
		u := parseCheckUnit(t, src)
		st := fixedRootStruct(t, u, "Gate")
		baseEntry, ok := FixedEvolutionOf(u, "Gate")
		if !ok {
			t.Fatal("missing Gate")
		}

		// 1. Direct unit check of compileEvolvePlan with a 65536-variant old enum layout -> evolveRefusePlanTooLarge (39).
		var hugeOld []byte
		hugeCount := uint32(2 + 65536)
		hugeOld = binary.LittleEndian.AppendUint32(hugeOld, hugeCount)
		// Entry 0: root struct (id, kind=13, size=5, children=1)
		hugeOld = append(hugeOld, baseEntry.Layout[4:4+17]...)
		binary.LittleEndian.PutUint32(hugeOld[4+13:4+17], 1)
		// Entry 1: enum field (id, kind=30, size=1, children=65536)
		hugeOld = append(hugeOld, baseEntry.Layout[4+17:4+34]...)
		binary.LittleEndian.PutUint32(hugeOld[4+17+13:4+17+17], 65536)
		var varEntry [17]byte
		varEntry[8] = 32
		for i := range 65536 {
			binary.LittleEndian.PutUint64(varEntry[0:8], uint64(i+1))
			hugeOld = append(hugeOld, varEntry[:]...)
		}
		plan := compileEvolvePlan(st, hugeOld, baseEntry.Layout)
		if plan.supported || plan.refuse != evolveRefusePlanTooLarge {
			t.Fatalf("R25: 65536-variant enum layout did not refuse plan_too_large: %+v", plan)
		}

		// 2. End-to-end Goose execution of all 3 per-entry refusal lanes (layout_malformed,
		// layout_record_too_large, plan_too_large) plus caller capacity batch_too_large.
		eMalformed := baseEntry
		eMalformed.Wire = baseEntry.Wire ^ 0x1001
		eMalformed.Layout = append([]byte(nil), baseEntry.Layout...)
		eMalformed.Layout[4+8] = 99 // unknown kind 99 -> layout_malformed

		eTooLarge := baseEntry
		eTooLarge.Wire = baseEntry.Wire ^ 0x2002
		eTooLarge.Layout = append([]byte(nil), baseEntry.Layout...)
		binary.LittleEndian.PutUint32(eTooLarge.Layout[4+9:4+13], 65537) // root size > 65536 -> layout_record_too_large

		ePlanTooLarge := baseEntry
		ePlanTooLarge.Wire = baseEntry.Wire ^ 0x3003
		ePlanTooLarge.Layout = append([]byte(nil), baseEntry.Layout...)
		// Set enum entry (entry 1) children count to 65536 (> 0xFFFF) -> plan_too_large (39)
		binary.LittleEndian.PutUint32(ePlanTooLarge.Layout[4+17+13:4+17+17], 65536)

		lineage := map[string][]FixedEvolutionEntry{
			"Gate": {eMalformed, eTooLarge, ePlanTooLarge},
		}
		var body [5]byte
		files := map[string][]byte{
			"malformed.bin": makeFixedFile(eMalformed.Wire, eMalformed.Layout, body[:]),
			"toolarge.bin":  makeFixedFile(eTooLarge.Wire, eTooLarge.Layout, body[:]),
			"planlarge.bin": makeFixedFile(ePlanTooLarge.Wire, ePlanTooLarge.Layout, body[:]),
			"batch2.bin":    makeFixedFile(baseEntry.Wire, baseEntry.Layout, body[:], body[:]),
		}
		out := runCustomFixedGoose(t, u, lineage, `
fn check_file(name: const u8[:]) {
    var bytes: u8[>..] = [];
    if !read_file(name, bytes) { print("read-failed"); return; }
    var values: probe::Gate[1] = [probe::Gate { Mode: probe::ModeB, Val: 777 }; 1];
    var rep = schema_rt::FixedEvolveReport { .. };
    let n = probe::GateFixedEvolveLoad(values, bytes, rep);
    let untouched = values[0].Mode == probe::ModeB && values[0].Val == 777;
    print(name, " ", n, " ", schema_rt::fixed_evolve_refusal_name(rep.reason), " ", untouched);
}

fn main() {
    check_file("malformed.bin");
    check_file("toolarge.bin");
    check_file("planlarge.bin");
    check_file("batch2.bin");
}
`, files)
		wantLines := []string{
			"malformed.bin -1 layout_malformed true",
			"toolarge.bin -1 layout_record_too_large true",
			"planlarge.bin -1 plan_too_large true",
			"batch2.bin -1 batch_too_large true",
		}
		if out != strings.Join(wantLines, "\n") {
			t.Fatalf("R25_R26 output:\n got:\n%s\nwant:\n%s", out, strings.Join(wantLines, "\n"))
		}
	})

	t.Run("W14", func(t *testing.T) {
		t.Parallel()
		const src = `package probe

type Pair
{
    x int32
    y int32
}

fixed table Scatter
{
    a uint32 = 1
    b uint32 = 2
    p Pair
    z uint64 = 9
}
`
		u := parseCheckUnit(t, src)
		st := fixedRootStruct(t, u, "Scatter")
		plan, _ := ir.TableFixedBuildPlan(u, st)
		if len(plan) != 1 || plan[0].Dst != ir.TableFixedMemberOffset(u, st, "a") || plan[0].Size != ir.TableFixedTypeBytes(st) {
			t.Fatalf("W14: unexpected build plan for Scatter: %+v", plan)
		}
		// W14: Verify ir.TableFixedMemberOffset and ir.TableFixedTypeBytes match emitted constant offsets in _ScatterFixedRead and _ScatterFixedWrite.
		wantOffs := map[string]int64{
			"a": 0,
			"b": 4,
			"p": 8,
			"z": 16,
		}
		for name, want := range wantOffs {
			if got := ir.TableFixedMemberOffset(u, st, name); got != want {
				t.Fatalf("W14: TableFixedMemberOffset(%q) = %d, want %d", name, got, want)
			}
		}
		if got := ir.TableFixedTypeBytes(st); got != 24 {
			t.Fatalf("W14: TableFixedTypeBytes(Scatter) = %d, want 24", got)
		}
		tables, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		code := string(tables["ProbeTable.goose"])
		for _, snippet := range []string{
			"let ScatterFixedBodyBytes: i64 = 24;",
			"v.A = schema_rt::fixed_get(b, at, 4) as! u32;",
			"v.B = schema_rt::fixed_get(b, at + 4, 4) as! u32;",
			"_PairFixedRead(b, at + 8, v.P);",
			"v.Z = schema_rt::fixed_get(b, at + 16, 8);",
			"v.X = schema_rt::fixed_get(b, at, 4) as! i32;",
			"v.Y = schema_rt::fixed_get(b, at + 4, 4) as! i32;",
		} {
			if !strings.Contains(code, snippet) {
				t.Fatalf("W14: emitted ProbeTable.goose missing constant-offset snippet %q:\n%s", snippet, code)
			}
		}
	})
}
