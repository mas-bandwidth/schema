package compiler_test

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v3/compiler"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/unitytable"
	"github.com/mas-bandwidth/schema/v3/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"

	"github.com/mas-bandwidth/schema/v3/internal/testproc"
)

// The lineage the Unity fixed form is held to: an older writer of Row, the
// reader that widens both of its fields and adds a third, and an older writer
// whose first field is another signedness (no lawful widening).
const (
	unityFixedOldWriter = `package probe
fixed table Row {
    a uint16
    b int32 | min = -50, max = 50
}
`
	unityFixedReader = `package probe
fixed table Row {
    a uint32
    b int64 | min = -1000, max = 1000
    c int16 = 99
}
`
	unityFixedUnlawfulWriter = `package probe
fixed table Row {
    a int16
    b int32 | min = -50, max = 50
}
`
	unityFixedUncarried = `package probe
fixed table Named {
    n    int32
    name bytes(70000)
}
fixed table Flags {
    on bool
}
`
)

// TestUnityFixedFormMatchesCpp is the Unity leg's fixed form, form 3
// (docs/FIXED-FORM-ALGORITHM.md, strict-only): the plan and the lineage, the
// selection by hash, the bounds pass, file save and load and measurement, a
// batch that is total, and a refusal that leaves the caller's rows untouched.
//
// The surface and the generator's lineage refusals are checked in process.
// Then (slow) every proof row whose root this leg carries is saved by the live
// C++ reference, and the Unity build — every row in one assembly, compiled in
// the Unity dialect — must load it, re-save it byte for byte, save the same
// values from scratch to the same bytes, have its own bytes accepted by the C++
// reference, and refuse every damaged file by name with the rows untouched. A
// lineage reader must read an older writer's file through its plan to the
// bytes the C++ reference writes for the same values, and refuse a retired, an
// unknown and an unlawful layout by name. Each refusal is sabotaged in a copy
// of the generated code, and the named probe must go red.
func TestUnityFixedFormMatchesCpp(t *testing.T) {
	t.Parallel()
	old := compiler.UnitFromSourceForTest(t, unityFixedOldWriter)
	reader := compiler.UnitFromSourceForTest(t, unityFixedReader)
	unlawful := compiler.UnitFromSourceForTest(t, unityFixedUnlawfulWriter)
	oldEntry, _ := unitytable.FixedLineageOf(old, "Row")
	ownEntry, _ := unitytable.FixedLineageOf(reader, "Row")
	unlawfulEntry, _ := unitytable.FixedLineageOf(unlawful, "Row")

	// THE SURFACE, and the hash and layout are the compiler's.
	plain := compiler.UnityGenerateForTest(t, unityFixedReader)
	tables := string(plain["Probe.Tables.cs"])
	layout, hash, body := fixedproof.RootFacts(reader.Tables["Row"])
	for _, want := range []string{
		fmt.Sprintf("public const ulong RowFixedHash = 0x%016xUL;", hash),
		fmt.Sprintf("public const int RowFixedBodyBytes = %d;", body),
		"public const int RowFixedRecordBytes = 8 + RowFixedBodyBytes;",
		"public static long RowFixedMeasure(long count)",
		"public static long RowFixedSave(ReadOnlySpan<Row> values, Span<byte> buffer)",
		"public static long RowFixedLoad(Span<Row> values, ReadOnlySpan<byte> data, ref TableReport report)",
		"public static long RowFixedLoad(Span<Row> values, ReadOnlySpan<byte> data, ref TableReport report, int planCapacity)",
		`Debug.Assert(value.B >= -1000 && value.B <= 1000, "b is outside its declared range [-1000, 1000]");`,
	} {
		if !strings.Contains(tables, want) {
			t.Errorf("Probe.Tables.cs lacks %q", want)
		}
	}
	if !bytes.Equal(ownEntry.Layout, layout) || ownEntry.Wire != hash {
		t.Errorf("the leg's own lineage entry is not the compiler's layout and hash")
	}
	// a root a codec does not carry whole has its struct and no fixed form
	uncarried := string(compiler.UnityGenerateForTest(t, unityFixedUncarried)["Probe.Tables.cs"])
	if !strings.Contains(uncarried, "public struct Named") || strings.Contains(uncarried, "NamedFixed") || !strings.Contains(uncarried, "FlagsFixedLoad") {
		t.Errorf("only the carried fixed root has the fixed form:\n%s", uncarried)
	}

	// THE LINEAGE, generated: the plan, the writer's bounds, the floor, the
	// unlawful entry, and the two refusals that stop generation.
	lineageTables := func(lineage []unitytable.FixedLineageEntry) (string, error) {
		out, err := unitytable.GenerateLineage(reader, map[string][]unitytable.FixedLineageEntry{"Row": lineage})
		return string(out["Probe.Tables.cs"]), err
	}
	planned, err := lineageTables([]unitytable.FixedLineageEntry{oldEntry, ownEntry})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		// a widens unsigned 2 -> 4, b widens signed 4 -> 8, c is prefilled
		fmt.Sprintf("new TableWire.FixedLane(0x%016xUL, 14, 0, false, true, 2, 3,", oldEntry.Wire),
		"new TableWire.FixedOp(3, 0, 0, 2, 4, 0x0000000000000000UL, 0x0000000000000000UL)",
		"new TableWire.FixedOp(2, 2, 4, 4, 8, 0x0000000000000000UL, 0x0000000000000000UL)",
		// the check pass holds b to the WRITER's [-50, 50] at the writer's width
		"new TableWire.FixedOp(5, 2, 0, 4, 4, 0xffffffffffffffceUL, 0x0000000000000032UL)",
		"public const int RowFixedPlanMax = 3;",
		"private static readonly byte[] RowFixedDefaults",
		"if (lane.Prefill) { RowFixedDefaults.CopyTo(image); }",
	} {
		if !strings.Contains(planned, want) {
			t.Errorf("the lineage reader lacks %q", want)
		}
	}
	retired := oldEntry
	retired.Retired = true
	if got, err := lineageTables([]unitytable.FixedLineageEntry{retired, ownEntry}); err != nil || !strings.Contains(got, "private const int RowFixedFloor = 1;") || strings.Contains(got, "FixedApply") {
		t.Errorf("a retired entry moves the floor and lays no plan (err %v)", err)
	}
	if got, err := lineageTables([]unitytable.FixedLineageEntry{unlawfulEntry, ownEntry}); err != nil || !strings.Contains(got, fmt.Sprintf("new TableWire.FixedLane(0x%016xUL, 14, TableReport.LayoutMalformed,", unlawfulEntry.Wire)) {
		t.Errorf("an entry of another signedness carries layout_malformed (err %v)", err)
	}
	noWriter := oldEntry
	noWriter.Writer = nil
	if _, err := lineageTables([]unitytable.FixedLineageEntry{noWriter, ownEntry}); err == nil || !strings.Contains(err.Error(), "writer declaration snapshot") {
		t.Errorf("an older entry with no writer snapshot must stop generation, got %v", err)
	}
	wrongWriter := oldEntry
	wrongWriter.Writer = unlawful.Tables["Row"]
	if _, err := lineageTables([]unitytable.FixedLineageEntry{wrongWriter, ownEntry}); err == nil || !strings.Contains(err.Error(), "does not state the entry's own layout") {
		t.Errorf("a writer snapshot that is not the entry's must stop generation, got %v", err)
	}

	slowtest.Gate(t, "dotnet and the C++ compiler")
	dotnet := compiler.FindDotnetForTest()
	if dotnet == "" {
		t.Fatal("dotnet is not on PATH")
	}
	fixedproof.SetMemoryCeiling(t)
	h := compiler.NewUnityHarnessForTest(t, dotnet)

	// THE ROWS this leg carries, renamed into one namespace each, and the C++
	// reference's file for each, four compiles at once.
	var rows []*unityFixedRow
	for i, tc := range fixedproof.Cases() {
		src := strings.Replace(tc.Schema, "package probe", fmt.Sprintf("package r%02d", i), 1)
		u := compiler.UnitFromSourceForTest(t, src)
		files, err := compiler.New().Generate(u, "unity", compiler.Options{})
		if err != nil || !unitytable.FixedCarried(u, u.Tables[tc.Root]) || !unityFlatScalarProbeRow(u.Tables[tc.Root]) {
			continue
		}
		rows = append(rows, newUnityFixedRow(t, tc, u, files))
	}
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.tc.ID)
	}
	for _, want := range []string{"bool-values", "signed-integers", "unsigned-integers", "ranged-integers", "bits-values"} {
		if !slices.Contains(ids, want) {
			t.Fatalf("the Unity leg does not carry proof row %s (carried: %v)", want, ids)
		}
	}
	t.Logf("the Unity leg carries %d proof rows: %v", len(rows), ids)
	lineageCase := fixedproof.RowCase{ID: "lineage-reader", Root: "Row", Count: 2, Schema: unityFixedReader, CppSetup: `
    value[0].a = 123;
    value[0].b = -20;
    value[1].a = 65535;
    value[1].b = -50;
`}
	var lineageOracle []byte
	slots := make(chan struct{}, min(max(4, runtime.GOMAXPROCS(0)/2), 8))
	t.Run("oracles", func(t *testing.T) {
		for _, r := range rows {
			t.Run(r.tc.ID, func(t *testing.T) {
				t.Parallel()
				slots <- struct{}{}
				defer func() { <-slots }()
				r.oracle, _ = fixedproof.CppOracle(t, r.tc, fixedproof.ParseCheckUnit(t, r.tc.Schema))
			})
		}
		t.Run(lineageCase.ID, func(t *testing.T) {
			t.Parallel()
			slots <- struct{}{}
			defer func() { <-slots }()
			lineageOracle, _ = fixedproof.CppOracle(t, lineageCase, fixedproof.ParseCheckUnit(t, lineageCase.Schema))
		})
	})
	if t.Failed() {
		t.FailNow()
	}

	// THE ASSEMBLY: every row, the four lineage readers, and the probe.
	assembly := map[string][]byte{}
	var namespaces []string
	add := func(files map[string][]byte) string {
		ns := csNamespace(t, files)
		for name, data := range files {
			assembly[ns+"_"+name] = data
		}
		namespaces = append(namespaces, ns)
		return ns
	}
	for _, r := range rows {
		r.ns = add(r.files)
		if got := r.files[r.tablesFile()]; !strings.Contains(string(got), fmt.Sprintf("public const ulong %sFixedHash = 0x%016xUL;", r.tc.Root, binary.LittleEndian.Uint64(r.oracle[8:16]))) {
			t.Fatalf("%s: the renamed unit's hash is not the C++ reference's 0x%016x", r.tc.ID, binary.LittleEndian.Uint64(r.oracle[8:16]))
		}
	}
	lineageNS := map[string]string{}
	for _, l := range []struct {
		key     string
		lineage []unitytable.FixedLineageEntry
	}{
		{"planned", []unitytable.FixedLineageEntry{oldEntry, ownEntry}},
		{"retired", []unitytable.FixedLineageEntry{retired, ownEntry}},
		{"none", nil},
		{"unlawful", []unitytable.FixedLineageEntry{unlawfulEntry, ownEntry}},
	} {
		u := compiler.UnitFromSourceForTest(t, strings.Replace(unityFixedReader, "package probe", "package l"+l.key, 1))
		files, err := compiler.New().Generate(u, "unity", compiler.Options{})
		if err != nil {
			t.Fatal(err)
		}
		var lin map[string][]unitytable.FixedLineageEntry
		if l.lineage != nil {
			lin = map[string][]unitytable.FixedLineageEntry{"Row": l.lineage}
		}
		tables, err := unitytable.GenerateLineage(u, lin)
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(files, tables)
		lineageNS[l.key] = add(files)
		rows = append(rows, &unityFixedRow{ns: lineageNS[l.key], tc: lineageCase, st: u.Tables["Row"], files: files, lineageOnly: true})
		rows[len(rows)-1].facts()
	}
	assembly["FixedProbe.cs"] = []byte(unityFixedProbeSource(rows, namespaces))

	// THE PROBES: one script line each, and what the line must answer.
	p := &unityFixedProbes{dir: filepath.Join(h.Tmp(), "probe")}
	for _, r := range rows {
		if r.lineageOnly {
			continue
		}
		r.probes(t, p)
	}
	writer := func(u *ir.Unit, recs ...[]byte) []byte {
		layout, hash, _ := fixedproof.RootFacts(u.Tables["Row"])
		return fixedproof.MakeFixedFile(hash, layout, recs...)
	}
	le := func(v int64, n int) []byte { return putLEBytes(uint64(v), n) }
	oldFile := writer(old, slices.Concat(le(123, 2), le(-20, 4)), slices.Concat(le(65535, 2), le(-50, 4)))
	oldPast := writer(old, slices.Concat(le(123, 2), le(-20, 4)), slices.Concat(le(1, 2), le(60, 4)))
	unlawfulFile := writer(unlawful, slices.Concat(le(-1, 2), le(5, 4)))
	planned2 := lineageNS["planned"]
	p.add("lineage-read", fmt.Sprintf("%s load %s", planned2, p.file(oldFile)), "2 accepted 4 0000000000000000 | 123 -20 99;65535 -50 99", lineageOracle)
	p.add("lineage-own", fmt.Sprintf("%s load %s", planned2, p.file(lineageOracle)), "2 accepted 0 0000000000000000 | 123 -20 99;65535 -50 99", lineageOracle)
	p.add("lineage-writer-bounds", fmt.Sprintf("%s mut %s %s 4 3", planned2, p.file(lineageOracle), p.file(oldPast)), "-1 1 out_of_range 0 0 0000000000000000 untouched", nil)
	p.add("lineage-plan-capacity", fmt.Sprintf("%s mut %s %s 4 2", planned2, p.file(lineageOracle), p.file(oldFile)), "-1 1 plan_too_large 0 0 0000000000000000 untouched", nil)
	p.add("lineage-retired", fmt.Sprintf("%s mut %s %s 4 3", lineageNS["retired"], p.file(lineageOracle), p.file(oldFile)), fmt.Sprintf("-1 1 layout_unsupported 0 0 %016x untouched", oldEntry.Wire), nil)
	p.add("lineage-retired-own", fmt.Sprintf("%s load %s", lineageNS["retired"], p.file(lineageOracle)), "2 accepted 0 0000000000000000 | 123 -20 99;65535 -50 99", lineageOracle)
	p.add("lineage-unknown", fmt.Sprintf("%s mut %s %s 4 3", lineageNS["none"], p.file(lineageOracle), p.file(oldFile)), fmt.Sprintf("-1 1 layout_unknown 0 0 %016x untouched", oldEntry.Wire), nil)
	p.add("lineage-unlawful", fmt.Sprintf("%s mut %s %s 4 3", lineageNS["unlawful"], p.file(lineageOracle), p.file(unlawfulFile)), "-1 1 layout_malformed 0 0 0000000000000000 untouched", nil)

	// THE BUILDS: the one under test and each sabotage, four at once.
	sabotages := []unityFixedSabotage{
		{"form byte", "form-1", "if (data[0] != FixedForm)", "if (data[0] == 255)", nil, ""},
		{"check pass over every record", "last-record-hash", "for (int k = 0; k < n; k++)", "for (int k = 0; k < 1; k++)", nil, unitytable.RuntimeFile},
		{"bool byte", "bool-2", "if (body[c.Src] > 1)", "if (body[c.Src] > 2)", nil, ""},
		{"range check", "out-of-range", "if (v < unchecked((long)c.Lo) || v > unchecked((long)c.Hi))", "if (v < unchecked((long)c.Lo) && v > unchecked((long)c.Hi))", nil, ""},
		{"writer store", "save", "unchecked((ulong)(long)value.", "unchecked((ulong)(uint)value.", nil, ""},
		{"widen sign", "lineage-read", "unchecked((ulong)FixedSigned(FixedGet(body, o.Src, o.Size), o.Size))", "FixedGet(body, o.Src, o.Size)", nil, ""},
		{"widened count", "lineage-read", "report.Widened = lane.Widens * n;", "report.Widened = 0 * n;", nil, ""},
		{"floor", "lineage-retired", "if (selected < floor)", "if (selected < 0)", nil, ""},
		{"land before the check pass", "untouched", "", "", regexp.MustCompile(`int why = TableWire\.FixedOpen\(data, (\w+)FixedLanes`), ""},
		{"short header", "short", "if (data.Length < FixedHead) { return TableReport.Truncated; }", "", nil, ""},
		{"reserved bytes", "reserved", "if (data[i] != 0) { return TableReport.ReservedNotZero; }", "if (data[i] == 255) { return TableReport.ReservedNotZero; }", nil, ""},
		{"layout length", "layout-length", "if ((ulong)FixedHead + layoutBytes > (ulong)data.Length)", "if ((ulong)FixedHead + layoutBytes > (ulong)data.Length + 0xFFFFFFFFUL)", nil, ""},
		{"unknown hash", "unknown-hash", "if (selected < 0) { return TableReport.LayoutUnknown; }", "if (selected < -1) { return TableReport.LayoutUnknown; }", nil, ""},
		{"layout compare", "layout-flip", "if (!data.Slice(FixedHead, (int)layoutBytes).SequenceEqual(new ReadOnlySpan<byte>(lane.Layout)))", "if (layoutBytes != (ulong)lane.Layout.Length)", nil, ""},
		{"unlawful entry", "lineage-unlawful", "if (lane.Why != 0) { return lane.Why; }", "if (lane.Why == -1) { return lane.Why; }", nil, ""},
		{"plan capacity", "plan-capacity", "if (lane.PlanCount > planCapacity)", "if (lane.PlanCount > planCapacity + 1000)", nil, ""},
		{"whole records", "ragged", "if (rest == 0 || record <= 8 || rest % record != 0)", "if (rest < 0)", nil, ""},
		{"batch capacity", "capacity", "if (n > capacity) { return TableReport.BatchTooLarge; }", "if (n > capacity + 1000000) { return TableReport.BatchTooLarge; }", nil, ""},
		{"record hash", "last-record-hash", "if (FixedGet(one, 0, 8) != hash)", "if (FixedGet(one, 0, 8) != FixedGet(one, 0, 8))", nil, ""},
		{"writer contract", "assert-", "Debug.Assert(value.", "Debug.Assert(body.Length >= 0 || value.", nil, ""},
	}
	gen := filepath.Join(h.Tmp(), "fixed-gen")
	compiler.WriteFilesForTest(t, gen, assembly)
	host, err := buildUnityFixedHost(t, h)
	if err != nil {
		t.Fatal(err)
	}
	units := make([]string, len(sabotages)+1)
	errs := make([]error, len(sabotages)+1)
	var wg sync.WaitGroup
	for i := range len(sabotages) + 1 {
		dir := gen
		if i > 0 {
			dir = filepath.Join(h.Tmp(), fmt.Sprintf("fixed-planted%d", i))
			compiler.WriteFilesForTest(t, dir, sabotages[i-1].plant(t, assembly))
		}
		units[i] = filepath.Join(h.Tmp(), fmt.Sprintf("fixed-units%d", i))
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			errs[i] = h.BuildUnit(dir, units[i], fmt.Sprintf("fixed%d", i))
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("build %d: %v", i, err)
		}
	}

	got := p.run(t, h, host, units[0], "build")
	bad := 0
	for i, want := range p.want {
		if got[i] != want {
			bad++
			if bad <= 12 {
				t.Errorf("probe %s (%s):\n unity %q\n want  %q", p.names[i], p.lines[i], got[i], want)
			}
		}
	}
	if bad > 0 {
		t.Fatalf("unity disagrees on %d of %d probes", bad, len(p.want))
	}
	t.Logf("unity answers all %d probes: every carried row's C++ bytes loaded, re-saved and saved from scratch byte for byte, every damaged file refused by name with the rows untouched, the lineage read through its plan", len(p.want))

	// THE OTHER DIRECTION needs no second run: every byte Unity wrote above is
	// the C++ reference's own save byte for byte (the probes compare the hex),
	// and the reference has already loaded its own save clean and re-saved it
	// to the same bytes (fixedproof.CppOracle refuses to answer otherwise).

	plantedRuns := make([][]string, len(sabotages))
	var runWG sync.WaitGroup
	for i := range sabotages {
		runWG.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			plantedRuns[i] = p.run(t, h, host, units[i+1], fmt.Sprintf("planted%d", i+1))
		})
	}
	runWG.Wait()
	for i, s := range sabotages {
		planted := plantedRuns[i]
		red := ""
		for j, want := range p.want {
			if s.red(p.names[j], want, planted[j]) {
				red = p.names[j]
				break
			}
		}
		if red == "" {
			t.Errorf("NEGATIVE CONTROL: with the %s sabotaged, every %s probe still passes", s.name, s.decides)
			continue
		}
		t.Logf("negative control: the %s sabotage turns probe %s red, as it must", s.name, red)
	}
}

// unityFixedRow is one proof row as the Unity assembly holds it.
type unityFixedRow struct {
	tc          fixedproof.RowCase
	st          *ir.Struct
	u           *ir.Unit
	files       map[string][]byte
	ns          string
	lineageOnly bool
	oracle      []byte

	members []unityFixedMember
	offs    []int64
	sizes   []int64
	kinds   []int
	body    int64
	layout  int
}

type unityFixedMember struct{ name, typ string }

func newUnityFixedRow(t *testing.T, tc fixedproof.RowCase, u *ir.Unit, files map[string][]byte) *unityFixedRow {
	t.Helper()
	r := &unityFixedRow{tc: tc, u: u, st: u.Tables[tc.Root], files: files}
	r.facts()
	return r
}

func (r *unityFixedRow) tablesFile() string {
	for name := range r.files {
		if strings.HasSuffix(name, ".Tables.cs") {
			return name
		}
	}
	return ""
}

// facts reads the root's members off the generated struct and its leaves off
// the compiler's walk.
func (r *unityFixedRow) facts() {
	text := string(r.files[r.tablesFile()])
	block := regexp.MustCompile(`public struct ` + r.st.Name + `\s*\{([^}]*)\}`).FindStringSubmatch(text)
	for _, m := range regexp.MustCompile(`public (\w+) (\w+);`).FindAllStringSubmatch(block[1], -1) {
		r.members = append(r.members, unityFixedMember{name: m[2], typ: m[1]})
	}
	walk := ir.TableFixedWalkRoot(r.st)
	off := int64(0)
	for _, e := range walk[1:] {
		r.offs = append(r.offs, off)
		r.sizes = append(r.sizes, e.Size)
		r.kinds = append(r.kinds, e.Kind)
		off += e.Size
	}
	r.body = off
	r.layout = len(ir.TableFixedLayoutBytes(walk))
}

// values is the records of a valid file as the probe prints them, read here
// from the bytes: bools as 0 or 1, integers in decimal at their signedness.
func (r *unityFixedRow) values(file []byte) string {
	at := 20 + r.layout
	var recs []string
	for k := 0; at+int(8+r.body)*(k+1) <= len(file); k++ {
		body := file[at+int(8+r.body)*k+8:]
		var vals []string
		for i := range r.offs {
			raw := uint64(0)
			for b := r.sizes[i] - 1; b >= 0; b-- {
				raw = raw<<8 | uint64(body[r.offs[i]+b])
			}
			switch {
			case r.kinds[i] == ir.TableKindBool:
				vals = append(vals, strconv.FormatUint(raw, 10))
			case ir.TableKindSigned(r.kinds[i]):
				shift := 64 - 8*r.sizes[i]
				vals = append(vals, strconv.FormatInt(int64(raw<<shift)>>shift, 10))
			default:
				vals = append(vals, strconv.FormatUint(raw, 10))
			}
		}
		recs = append(recs, strings.Join(vals, " "))
	}
	return strings.Join(recs, ";")
}

// probes lays one row's probes: the load and re-save of the C++ bytes, the save
// of the same values from scratch, the save of the defaults, and every damaged
// file of §5.3, each refused by name with the rows untouched.
func (r *unityFixedRow) probes(t *testing.T, p *unityFixedProbes) {
	t.Helper()
	f := r.oracle
	id, ns := r.tc.ID, r.ns
	n := r.tc.Count
	vals := r.values(f)
	p.add("load-"+id, fmt.Sprintf("%s load %s", ns, p.file(f)), fmt.Sprintf("%d accepted 0 0000000000000000 | %s", n, vals), f)
	p.add("save-"+id, fmt.Sprintf("%s save %s", ns, p.file([]byte(strings.ReplaceAll(vals, ";", "\n")+"\n"))), strconv.Itoa(len(f)), f)
	m := tabletext.NewModel(r.u)
	if defaults, err := tablewire.EncodeFixed(m, m.New(r.st)); err == nil {
		p.add("defaults-"+id, fmt.Sprintf("%s defaults", ns), strconv.Itoa(len(defaults)), defaults)
	}
	head := 20 + r.layout
	rec := int(8 + r.body)
	last := len(f) - rec
	hash := binary.LittleEndian.Uint64(f[8:16])
	mutant := func(name string, file []byte, capacity, plan int, reason string, reportHash uint64) {
		p.add(name+"-"+id, fmt.Sprintf("%s mut %s %s %d %d", ns, p.file(f), p.file(file), capacity, plan),
			fmt.Sprintf("-1 1 %s 0 0 %016x untouched", reason, reportHash), nil)
	}
	edit := func(fn func(b []byte) []byte) []byte { return fn(append([]byte(nil), f...)) }
	for _, form := range []struct {
		b      byte
		reason string
	}{{0, "newer_form"}, {1, "previous_form"}, {2, "message_form_as_file"}, {4, "newer_form"}} {
		mutant(fmt.Sprintf("form-%d", form.b), edit(func(b []byte) []byte { b[0] = form.b; return b }), n, 1, form.reason, 0)
	}
	mutant("short", f[:19], n, 1, "truncated", 0)
	mutant("reserved", edit(func(b []byte) []byte { b[5] = 1; return b }), n, 1, "reserved_not_zero", 0)
	mutant("layout-length", edit(func(b []byte) []byte { binary.LittleEndian.PutUint32(b[16:], uint32(len(b))); return b }), n, 1, "layout_malformed", 0)
	mutant("unknown-hash", edit(func(b []byte) []byte { b[8] ^= 1; return b }), n, 1, "layout_unknown", hash^1)
	mutant("layout-flip", edit(func(b []byte) []byte { b[head-3] ^= 0x40; return b }), n, 1, "layout_malformed", 0)
	mutant("last-record-hash", edit(func(b []byte) []byte { b[last] ^= 1; return b }), n, 1, "no_layout", 0)
	mutant("ragged", append(append([]byte(nil), f...), 0), n, 1, "truncated", 0)
	mutant("header-only", f[:head], n, 1, "truncated", 0)
	mutant("capacity", f, n-1, 1, "batch_too_large", 0)
	mutant("plan-capacity", f, n, 0, "plan_too_large", 0)
	for i, k := range r.kinds {
		if k == ir.TableKindBool {
			mutant("bool-2", edit(func(b []byte) []byte { b[last+8+int(r.offs[i])] = 2; return b }), n, 1, "wire_damaged", 0)
			break
		}
	}
	for i, fd := range r.st.Fields {
		v, ok := outOfRange(fd, r.kinds[i], r.sizes[i])
		if !ok {
			continue
		}
		mutant("out-of-range-"+fd.Name, edit(func(b []byte) []byte {
			copy(b[last+8+int(r.offs[i]):], putLEBytes(v, int(r.sizes[i])))
			return b
		}), n, 1, "out_of_range", 0)
		// the writer's contract is a Debug.Assert (Unity is an assert leg): the
		// same value handed to the save stops it in this Debug build
		first := strings.Fields(strings.Split(vals, ";")[0])
		first[i] = strconv.FormatUint(v, 10)
		if ir.TableKindSigned(r.kinds[i]) {
			shift := 64 - 8*r.sizes[i]
			first[i] = strconv.FormatInt(int64(v<<shift)>>shift, 10)
		}
		p.add("assert-"+fd.Name+"-"+id, fmt.Sprintf("%s save %s %s", ns, p.file([]byte(strings.Join(first, " ")+"\n")), filepath.Join(p.dir, "assert.bin")), "exception InvalidOperationException", nil)
	}
}

// outOfRange is a raw value the field's storage holds and its declared range
// or bits(N) does not, as the storage's two's complement.
func outOfRange(f *ir.Field, kind int, size int64) (uint64, bool) {
	var lo, hi *big.Int
	switch f.Type.Kind {
	case ir.TBits:
		lo, hi = new(big.Int), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
	case ir.TInt:
		l, h, ok := ir.TableRawRange(f)
		if !ok {
			return 0, false
		}
		lo, hi = l, h
	default:
		return 0, false
	}
	bits := uint(8 * size)
	one := big.NewInt(1)
	smin, smax := new(big.Int), new(big.Int).Sub(new(big.Int).Lsh(one, bits), one)
	if ir.TableKindSigned(kind) {
		smin = new(big.Int).Neg(new(big.Int).Lsh(one, bits-1))
		smax = new(big.Int).Sub(new(big.Int).Lsh(one, bits-1), one)
	}
	var v *big.Int
	switch {
	case hi.Cmp(smax) < 0:
		v = new(big.Int).Add(hi, one)
	case lo.Cmp(smin) > 0:
		v = new(big.Int).Sub(lo, one)
	default:
		return 0, false
	}
	if v.Sign() < 0 {
		v.Add(v, new(big.Int).Lsh(one, bits))
	}
	return v.Uint64(), true
}

func putLEBytes(v uint64, n int) []byte {
	out := make([]byte, n)
	for i := range n {
		out[i] = byte(v >> (8 * i))
	}
	return out
}

// csNamespace is the namespace a generated unit's files declare.
func csNamespace(t *testing.T, files map[string][]byte) string {
	t.Helper()
	for name, data := range files {
		if strings.HasSuffix(name, ".Tables.cs") {
			if m := regexp.MustCompile(`(?m)^namespace (\w+)`).FindSubmatch(data); m != nil {
				return string(m[1])
			}
		}
	}
	t.Fatal("no namespace in the generated tables file")
	return ""
}

// unityFixedProbes is the script the probe host runs, one line per probe.
type unityFixedProbes struct {
	dir   string
	n     int
	names []string
	lines []string
	want  []string
	files []string // the output file a probe writes, or ""
}

func (p *unityFixedProbes) file(data []byte) string {
	p.n++
	path := filepath.Join(p.dir, fmt.Sprintf("in%04d.bin", p.n))
	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		panic(err)
	}
	return path
}

// add lays one probe. out, when set, is the bytes the probe must write: its
// hex is part of the answer.
func (p *unityFixedProbes) add(name, line, want string, out []byte) {
	file := ""
	if out != nil {
		file = fmt.Sprintf("out%04d.bin", len(p.lines))
		line += " " + file
		want += " #" + hex.EncodeToString(out)
	}
	p.names = append(p.names, name)
	p.lines = append(p.lines, line)
	p.want = append(p.want, want)
	p.files = append(p.files, file)
}

// run executes the script against one build and answers each probe's line,
// with the bytes it wrote appended.
func (p *unityFixedProbes) run(t *testing.T, h compiler.UnityHarnessForTest, host, units, label string) []string {
	t.Helper()
	outdir := filepath.Join(h.Tmp(), "probe-out-"+label)
	if err := os.MkdirAll(outdir, 0o755); err != nil {
		t.Fatal(err)
	}
	var script strings.Builder
	for i, line := range p.lines {
		if p.files[i] != "" {
			line = strings.TrimSuffix(line, p.files[i]) + filepath.Join(outdir, p.files[i])
		}
		script.WriteString(line + "\n")
	}
	scriptPath := filepath.Join(outdir, "script.txt")
	answers := filepath.Join(outdir, "answers.txt")
	if err := os.WriteFile(scriptPath, []byte(script.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := testproc.Command(t, h.Dotnet(), host, filepath.Join(units, "schemaunit.dll"), scriptPath, answers)
	cmd.Dir = h.Tmp()
	cmd.Env = h.Env()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the probe host (%s): %v\n%s", label, err, out)
	}
	data, err := os.ReadFile(answers)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(got) != len(p.lines) {
		t.Fatalf("the probe host (%s) answered %d lines for %d probes", label, len(got), len(p.lines))
	}
	for i := range got {
		if p.files[i] == "" {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(outdir, p.files[i])); err == nil {
			got[i] += " #" + hex.EncodeToString(b)
		}
	}
	return got
}

// unityFixedSabotage is one refusal or contract planted out of a copy of the
// whole assembly. decides prefixes the probes it must turn red.
type unityFixedSabotage struct {
	name, decides string
	from, to      string
	re            *regexp.Regexp
	only          string // when set, only the files whose name ends in it
}

// red reports whether a probe's answer in the planted build shows the
// sabotage: a probe it decides answers otherwise, or, for the landing control,
// a refusal leaves a row touched.
func (s unityFixedSabotage) red(name, want, got string) bool {
	if s.decides == "untouched" {
		return strings.HasSuffix(want, " untouched") && strings.HasSuffix(got, " touched")
	}
	return got != want && strings.HasPrefix(name, s.decides)
}

func (s unityFixedSabotage) plant(t *testing.T, files map[string][]byte) map[string][]byte {
	t.Helper()
	out := maps.Clone(files)
	patched := 0
	for name, data := range files {
		if s.only != "" && !strings.HasSuffix(name, s.only) {
			continue
		}
		var next string
		if s.re != nil {
			// land the first record before the check pass has answered
			next = s.re.ReplaceAllString(string(data), "if (values.Length > 0 && data.Length >= TableWire.FixedHead + ${1}FixedLayout.Length + ${1}FixedRecordBytes) { ${1}FixedDecode(data.Slice(TableWire.FixedHead + ${1}FixedLayout.Length + 8, ${1}FixedBodyBytes), ref values[0]); }\n            int why = TableWire.FixedOpen(data, ${1}FixedLanes")
		} else {
			next = strings.ReplaceAll(string(data), s.from, s.to)
		}
		if next != string(data) {
			patched++
			out[name] = []byte(next)
		}
	}
	if patched == 0 {
		t.Fatalf("sabotage %s patched nothing", s.name)
	}
	return out
}

// unityFixedProbeSource is the probe compiled into the assembly: one class per
// namespace that loads, saves and refuses, and the dispatcher the host calls.
func unityFixedProbeSource(rows []*unityFixedRow, namespaces []string) string {
	var b strings.Builder
	b.WriteString("// The fixed-form probe (compiler/unityfixedform_test.go).\n#pragma warning disable CA1031, CA1305, CA1307, CA1310, CA1050\n\nusing System;\nusing System.Globalization;\nusing System.IO;\nusing System.Text;\n\n")
	for _, r := range rows {
		root := r.st.Name
		fmt.Fprintf(&b, "namespace %s\n{\n    internal static class FixedProbe\n    {\n", r.ns)
		b.WriteString("        private static readonly CultureInfo CI = CultureInfo.InvariantCulture;\n\n")
		fmt.Fprintf(&b, "        private static %s[] Fresh(int n)\n        {\n            %s[] rows = new %s[n];\n            for (int i = 0; i < n; i++) { rows[i] = Schema.New%s(); }\n            return rows;\n        }\n\n", root, root, root, root)
		fmt.Fprintf(&b, "        private static string Values(%s[] v, long n)\n        {\n            StringBuilder s = new StringBuilder();\n            for (int k = 0; k < n; k++)\n            {\n                if (k > 0) { s.Append(';'); }\n", root)
		for i, m := range r.members {
			if i > 0 {
				b.WriteString("                s.Append(' ');\n")
			}
			if m.typ == "bool" {
				fmt.Fprintf(&b, "                s.Append(v[k].%s ? \"1\" : \"0\");\n", m.name)
			} else {
				fmt.Fprintf(&b, "                s.Append(v[k].%s.ToString(CI));\n", m.name)
			}
		}
		b.WriteString("            }\n            return s.ToString();\n        }\n\n")
		fmt.Fprintf(&b, "        private static void Save(%s[] v, string path)\n        {\n            long need = Schema.%sFixedMeasure(v.Length);\n            byte[] buf = new byte[need];\n            long n = Schema.%sFixedSave(v, buf);\n            if (n != need) { throw new InvalidOperationException(\"save answered \" + n.ToString(CI)); }\n            File.WriteAllBytes(path, buf);\n        }\n\n", root, root, root)
		b.WriteString("        internal static string Run(string[] a)\n        {\n            Schema.TableReport r = default;\n            switch (a[1])\n            {\n")
		// load <in> <out>
		fmt.Fprintf(&b, "                case \"load\":\n                {\n                    %s[] rows = Fresh(1024);\n                    long n = Schema.%sFixedLoad(rows, File.ReadAllBytes(a[2]), ref r);\n", root, root)
		b.WriteString("                    string line = n.ToString(CI) + \" \" + Schema.TableReport.ReasonName(r.Reason) + \" \" + r.Widened.ToString(CI) + \" \" + r.LayoutHash.ToString(\"x16\", CI);\n")
		fmt.Fprintf(&b, "                    if (n >= 0) { %s[] got = new %s[n]; Array.Copy(rows, got, n); Save(got, a[3]); line += \" | \" + Values(rows, n); }\n                    return line;\n                }\n", root, root)
		// save <values> <out>
		fmt.Fprintf(&b, "                case \"save\":\n                {\n                    string[] lines = File.ReadAllLines(a[2]);\n                    %s[] rows = Fresh(lines.Length);\n                    for (int k = 0; k < lines.Length; k++)\n                    {\n                        string[] p = lines[k].Split(' ');\n", root)
		for i, m := range r.members {
			switch m.typ {
			case "bool":
				fmt.Fprintf(&b, "                        rows[k].%s = p[%d] == \"1\";\n", m.name, i)
			case "sbyte", "short", "int", "long":
				fmt.Fprintf(&b, "                        rows[k].%s = unchecked((%s)long.Parse(p[%d], CI));\n", m.name, m.typ, i)
			default:
				fmt.Fprintf(&b, "                        rows[k].%s = unchecked((%s)ulong.Parse(p[%d], CI));\n", m.name, m.typ, i)
			}
		}
		b.WriteString("                    }\n                    Save(rows, a[3]);\n                    return new FileInfo(a[3]).Length.ToString(CI);\n                }\n")
		// defaults <out>
		b.WriteString("                case \"defaults\":\n                {\n                    Save(Fresh(1), a[2]);\n                    return new FileInfo(a[2]).Length.ToString(CI);\n                }\n")
		// mut <valid> <mutant> <capacity> <plan capacity>
		fmt.Fprintf(&b, "                case \"mut\":\n                {\n                    %s[] all = Fresh(1024);\n                    long vn = Schema.%sFixedLoad(all, File.ReadAllBytes(a[2]), ref r);\n", root, root)
		fmt.Fprintf(&b, "                    int cap = int.Parse(a[4], CI);\n                    %s[] rows = Fresh(cap);\n                    // each row holds the record after its own, so a landing shows\n                    for (int i = 0; i < cap && vn > 0; i++) { rows[i] = all[(i + 1) %% vn]; }\n                    %s[] before = (%s[])rows.Clone();\n", root, root, root)
		b.WriteString("                    r = default;\n                    r.Unknown = 7;\n                    r.Widened = 7;\n                    r.LayoutHash = 7;\n")
		fmt.Fprintf(&b, "                    long n = Schema.%sFixedLoad(rows, File.ReadAllBytes(a[3]), ref r, int.Parse(a[5], CI));\n", root)
		b.WriteString("                    bool same = true;\n                    for (int i = 0; i < cap; i++) { if (!rows[i].Equals(before[i])) { same = false; } }\n")
		b.WriteString("                    return n.ToString(CI) + \" \" + (r.Refused ? \"1\" : \"0\") + \" \" + Schema.TableReport.ReasonName(r.Reason) + \" \" + r.Widened.ToString(CI) + \" \" + r.Unknown.ToString(CI) + \" \" + r.LayoutHash.ToString(\"x16\", CI) + \" \" + (same ? \"untouched\" : \"touched\");\n                }\n")
		b.WriteString("            }\n            return \"no mode \" + a[1];\n        }\n    }\n}\n\n")
	}
	b.WriteString("namespace FixedProbeHost\n{\n    public static class FixedProbeMain\n    {\n        public static int Run(string[] args)\n        {\n            StringBuilder outp = new StringBuilder();\n            foreach (string line in File.ReadAllLines(args[0]))\n            {\n                string[] a = line.Split(' ');\n                string got;\n                try { got = Dispatch(a); }\n                catch (Exception e) { got = \"exception \" + e.GetType().Name; }\n                outp.Append(got).Append('\\n');\n            }\n            File.WriteAllText(args[1], outp.ToString());\n            return 0;\n        }\n\n        private static string Dispatch(string[] a)\n        {\n            switch (a[0])\n            {\n")
	for _, ns := range namespaces {
		fmt.Fprintf(&b, "                case \"%s\": return %s.FixedProbe.Run(a);\n", ns, ns)
	}
	b.WriteString("            }\n            return \"no namespace \" + a[0];\n        }\n    }\n}\n")
	return b.String()
}

// buildUnityFixedHost builds the .NET host that loads one built assembly and
// runs its probe script, and answers the host's dll.
func buildUnityFixedHost(t testing.TB, h compiler.UnityHarnessForTest) (string, error) {
	dir := filepath.Join(h.Tmp(), "fixed-host")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	proj := fmt.Sprintf(`<Project Sdk="Microsoft.NET.Sdk">
  <Import Project="%s" />
  <PropertyGroup>
    <TargetFramework>net10.0</TargetFramework>
    <AssemblyName>fixedhost</AssemblyName>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="Program.cs" />
  </ItemGroup>
</Project>
`, filepath.Join(h.Root(), "test", "unity", "HostDialect.props"))
	prog := `using System;
using System.Diagnostics;
using System.Reflection;

// A failed Debug.Assert throws here instead of ending the process, so a
// probe can answer that the writer's contract stopped a save.
internal sealed class ThrowingListener : TraceListener
{
    public override void Write(string message) { }
    public override void WriteLine(string message) { }
    public override void Fail(string message, string detailMessage) { throw new InvalidOperationException(message); }
}

internal static class Program
{
    private static int Main(string[] args)
    {
        Trace.Listeners.Clear();
        Trace.Listeners.Add(new ThrowingListener());
        Assembly unit = Assembly.LoadFrom(args[0]);
        MethodInfo run = unit.GetType("FixedProbeHost.FixedProbeMain", true).GetMethod("Run");
        return (int)run.Invoke(null, new object[] { new[] { args[1], args[2] } });
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "fixedhost.csproj"), []byte(proj), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "Program.cs"), []byte(prog), 0o644); err != nil {
		return "", err
	}
	out := filepath.Join(dir, "bin")
	cmd := testproc.Command(t, h.Dotnet(), "build", filepath.Join(dir, "fixedhost.csproj"), "-c", "Debug", "-o", out,
		"--artifacts-path", filepath.Join(h.Tmp(), "artifacts", "fixedhost"), "--nologo", "-v", "q", "-p:RestoreLockedMode=false")
	cmd.Dir = dir
	cmd.Env = h.Env()
	if msg, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("dotnet build the probe host: %w\n%s", err, msg)
	}
	return filepath.Join(out, "fixedhost.dll"), nil
}

func unityFlatScalarProbeRow(st *ir.Struct) bool {
	if st == nil {
		return false
	}
	for _, f := range st.Fields {
		if f.Type.Optional || f.Array != ir.ArrayNone || f.KeyEnum != "" || f.Type.Width == 128 {
			return false
		}
		if f.Type.Kind != ir.TBool && f.Type.Kind != ir.TInt && f.Type.Kind != ir.TBits {
			return false
		}
	}
	return true
}
