package lobstertable_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The shared evolution suite supplies real C++ files, prior locked layouts,
// retired entries and corrupted controls. The adapter only projects the native
// result: it does not translate a successful read into a refusal.
func TestLobsterFixedProofEvolution(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Lobster toolchain and the C++ fixed-form corpus")
	fixedproof.RunEvolutionSuite(t, filepath.Join("..", "..", ".."), lobsterFixedProofRunner{})
}

var _ fixedproof.EvoRunner = lobsterFixedProofRunner{}

func lobsterGenerateWithLineage(t *testing.T, u *ir.Unit, lineage map[string][]fixedproof.LineageEntry) map[string][]byte {
	t.Helper()
	files, err := lobster.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	var tables map[string][]byte
	if len(lineage) == 0 {
		tables, err = lobstertable.Generate(u)
	} else {
		locked := map[string][]lobstertable.FixedLineageEntry{}
		for name, entries := range lineage {
			for _, e := range entries {
				locked[name] = append(locked[name], lobstertable.FixedLineageEntry{
					Wire: e.Wire, Layout: e.Layout, Record: e.Record, Retired: e.Retired, Reason: e.Reason,
				})
			}
		}
		tables, err = lobstertable.GenerateLineage(u, locked)
	}
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	return files
}

func (lobsterFixedProofRunner) CheckLineage(t *testing.T, u *ir.Unit, root string, lineage map[string][]fixedproof.LineageEntry) string {
	t.Helper()
	files := lobsterGenerateWithLineage(t, u, lineage)
	pkg := u.Package
	script := lobsterProofImports(u) + fmt.Sprintf(`let _, why = schema_rt.fixed_parse_layout(%[1]s.%[2]sFixedLayoutBytes())
if why != "":
    print("REFUSED " + why)
    return
for(%[1]s.%[2]sFixedKnown) kl:
    if read_uint32_le(kl.layout, 0) > 4096:
        print("REFUSED " + schema_rt.TableRefusePlanTooLarge)
        return
for(%[1]s.%[2]sFixedLineagePlans) lane:
    if lane.why != "":
        print("REFUSED " + lane.why)
        return
if %[1]s.%[2]sFixedPlanMax > 4096:
    print("REFUSED " + schema_rt.TableRefusePlanTooLarge)
    return
print("OK")
`, pkg, root)
	out, _ := lobsterEvolutionRun(t, files, script, nil)
	out = strings.TrimSpace(out)
	if after, ok := strings.CutPrefix(out, "REFUSED "); ok {
		return strings.TrimSpace(after)
	}
	if out == "OK" {
		return ""
	}
	return out
}

func (lobsterFixedProofRunner) LoadLineage(t *testing.T, u *ir.Unit, root string, lineage map[string][]fixedproof.LineageEntry, wire []byte, capacity int) (int, string, map[string]string, []byte, fixedproof.LoadCounters) {
	t.Helper()
	files := lobsterGenerateWithLineage(t, u, lineage)
	if capacity < 0 {
		// Every fixed record contains an eight-byte hash, so this file-size
		// bound supplies enough caller storage without imposing a hidden cap.
		// Explicit capacities are passed through unchanged.
		capacity = len(wire) / 8
	}
	pkg := u.Package
	st := fixedproof.RootStruct(t, u, root)
	var dump strings.Builder
	lobsterProofDumpFields(&dump, pkg, st, "r[i]", "", "    ")
	script := lobsterProofImports(u) + fmt.Sprintf(`let data = read_file("in.bin")
assert(data)
let r:[%[1]s.%[2]s] = []
for(%[3]d): push(r, %[1]s.New%[2]s())
let report = schema_rt.TableReport { unknown: 99, kind_mismatch: 99, clamped: 99, widened: 99, duplicate: 99, malformed: true, reason: "stale" }
let plan:[schema_rt.TableFixedEntry] = []
for(4096): push(plan, schema_rt.TableFixedEntry {})
let count = %[1]s.%[2]sFixedLoad(r, data, plan, report)
print("COUNTERS " + string(report.unknown) + " " + string(report.kind_mismatch) + " " + string(report.clamped) + " " + string(report.widened) + " " + string(report.duplicate))
if count < 0:
    print("REFUSED " + report.reason)
else:
    print("OK " + string(count))
    let decoded:[%[1]s.%[2]s] = []
    for(count) i:
        push(decoded, r[i])
%[4]s    assert write_file("out.bin", %[1]s.%[2]sFixedSave(decoded))
`, pkg, root, capacity, indentLobsterProof(dump.String(), "    "))
	out, resaved := lobsterEvolutionRun(t, files, script, map[string][]byte{"in.bin": wire})
	fields := map[string]string{}
	count, reason := -1, ""
	var counters fixedproof.LoadCounters
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, line := range lines {
		if after, ok := strings.CutPrefix(line, "COUNTERS "); ok {
			parts := strings.Fields(after)
			if len(parts) == 5 {
				counters.Unknown, _ = strconv.Atoi(parts[0])
				counters.KindMismatch, _ = strconv.Atoi(parts[1])
				counters.Clamped, _ = strconv.Atoi(parts[2])
				counters.Widened, _ = strconv.Atoi(parts[3])
				counters.Duplicate, _ = strconv.Atoi(parts[4])
			}
		}
		if after, ok := strings.CutPrefix(line, "REFUSED "); ok {
			return -1, strings.TrimSpace(after), fields, nil, counters
		}
		if after, ok := strings.CutPrefix(line, "OK "); ok {
			var err error
			count, err = strconv.Atoi(after)
			if err != nil {
				t.Fatalf("native count %q: %v", after, err)
			}
			reason = "ok"
		}
		if after, ok := strings.CutPrefix(line, "FIELD "); ok {
			key, val, ok := strings.Cut(after, "=")
			if !ok {
				t.Fatalf("invalid native field %q", line)
			}
			fields[key] = val
		}
	}
	if count >= 0 && resaved != nil {
		return count, reason, fields, resaved, counters
	}
	t.Fatalf("native evolution probe omitted its result: %s", out)
	return -1, "", nil, nil, counters
}

func lobsterProofImports(u *ir.Unit) string {
	var out strings.Builder
	for _, f := range u.Files {
		fmt.Fprintf(&out, "import .%sTable\n", f.Base)
	}
	return out.String()
}

func indentLobsterProof(src, indent string) string {
	if src == "" {
		return ""
	}
	return indent + strings.ReplaceAll(strings.TrimSuffix(src, "\n"), "\n", "\n"+indent) + "\n"
}

// Project the fields the evolution contract checks, using the checked
// schema for native member spellings. The file oracle separately checks all
// array, text and wide-number storage by complete byte-identical write-back.
func lobsterProofDumpFields(out *strings.Builder, pkg string, st *ir.Struct, expr, path, indent string) {
	for _, f := range st.Fields {
		lobsterProofDumpField(out, pkg, f, expr+"."+ir.GoExportName(f.Name), path+"."+f.Name, indent)
	}
}

func lobsterProofDumpField(out *strings.Builder, pkg string, f *ir.Field, expr, path, indent string) {
	trueLit, falseLit := "1", "0"
	if pkg == "probe" {
		trueLit, falseLit = "true", "false"
	}
	if f.Type.Optional {
		pres := lobsterCompanionExpr(expr, f, "_present")
		fmt.Fprintf(out, "%sprint(\"FIELD r\" + string(i) + %s + \"=\" + if %s: %q else: %q)\n", indent, pathExpr(path+"_present"), pres, trueLit, falseLit)
		fmt.Fprintf(out, "%sif %s:\n", indent, pres)
		indent += "    "
	}
	if f.Array != ir.ArrayNone || f.KeyEnumRef != nil {
		loopV := fmt.Sprintf("idx_%d", len(indent))
		boundExpr := strconv.FormatInt(f.ArrayBound, 10)
		if f.KeyEnumRef != nil {
			boundExpr = strconv.FormatInt(f.KeyEnumRef.Max, 10)
		} else if f.Array == ir.ArrayCounted {
			cnt := lobsterCompanionExpr(expr, f, "_count")
			fmt.Fprintf(out, "%sprint(\"FIELD r\" + string(i) + %s + \"=\" + string(%s))\n", indent, pathExpr(path+"_count"), cnt)
			boundExpr = cnt
		}
		fmt.Fprintf(out, "%sfor(%s) %s:\n", indent, boundExpr, loopV)
		elemF := *f
		elemF.Array = ir.ArrayNone
		elemF.KeyEnumRef = nil
		elemF.Type.Optional = false
		elemPath := path + "[\" + string(" + loopV + ") + \"]"
		lobsterProofDumpScalarOrComposite(out, pkg, &elemF, fmt.Sprintf("%s[%s]", expr, loopV), elemPath, "", indent+"    ")
		return
	}
	lenExpr := lobsterCompanionExpr(expr, f, "_length")
	lobsterProofDumpScalarOrComposite(out, pkg, f, expr, path, lenExpr, indent)
}

func pathExpr(p string) string {
	return "\"" + p + "\""
}

func lobsterProofDumpScalarOrComposite(out *strings.Builder, pkg string, f *ir.Field, expr, path, lenExpr, indent string) {
	printField := func(key, value string) {
		fmt.Fprintf(out, "%sprint(\"FIELD r\" + string(i) + %s + \"=\" + string(%s))\n", indent, pathExpr(key), value)
	}
	trueLit, falseLit := "1", "0"
	if pkg == "probe" {
		trueLit, falseLit = "true", "false"
	}
	switch f.Type.Kind {
	case ir.TInt, ir.TBits:
		if f.Type.Width <= 64 {
			if (!f.Type.Signed || f.Type.Kind == ir.TBits) && f.Type.Width == 64 {
				// Avoid signed 64-bit formatting mismatches on high-bit u64/bits64
				return
			}
			printField(path, expr)
		}
	case ir.TFixed:
		if f.Type.Width <= 64 {
			fmt.Fprintf(out, "%sprint(\"FIELD r\" + string(i) + %s + \"=raw=\" + string(%s))\n", indent, pathExpr(path), expr)
		}
	case ir.TBool:
		fmt.Fprintf(out, "%sprint(\"FIELD r\" + string(i) + %s + \"=\" + if %s: %q else: %q)\n", indent, pathExpr(path), expr, trueLit, falseLit)
	case ir.TString:
		if lenExpr != "" {
			fmt.Fprintf(out, "%sprint(\"FIELD r\" + string(i) + %s + \"=\\\"\" + substring(%s, 0, %s) + \"\\\"\")\n", indent, pathExpr(path), expr, lenExpr)
			printField(path+"_length", lenExpr)
		}
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			lobsterProofDumpFields(out, pkg, ref, expr, path, indent)
		case *ir.Union:
			printField(path+".type", "int("+expr+".Type)")
			for idx, arm := range ref.Variants {
				if arm.F != nil {
					fmt.Fprintf(out, "%sif int(%s.Type) == %d:\n", indent, expr, idx+1)
					af := *arm.F
					af.Name = arm.Name
					lobsterProofDumpField(out, pkg, &af, expr+"."+ir.GoExportName(arm.Name), path+"."+arm.Name, indent+"    ")
				}
			}
		case *ir.Enum:
			printField(path, "int("+expr+")")
		case *ir.Flags:
			printField(path, "int("+expr+")")
		}
	}
}

func (lobsterFixedProofRunner) SaveCustom(t *testing.T, u *ir.Unit, root, scenario string) ([]byte, string) {
	t.Helper()
	files, err := lobster.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := lobstertable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	count, setup := 1, ""
	switch scenario {
	case "batch_over_4096":
		count = 4097
	case "optional_absent_zeros":
		setup = `r[0].Plain.A = 11
r[0].Plain.B = 22
r[0].OptPresent = false
r[0].Opt.A = 0x12345678
r[0].Opt.B = 0x23456789
`
	case "live_extents_base":
		setup = fmt.Sprintf(`r[0].ArrCount = 1
r[0].Arr[0] = %s.SmallOne
r[0].OptPresent = false
r[0].Tail = 77
`, u.Package)
	case "writing_slack_zeros":
		setup = fmt.Sprintf(`r[0].ArrCount = 1
r[0].Arr[0] = 0x1122
r[0].S = "A"
r[0].SLength = 1
r[0].B = "B"
r[0].BLength = 1
r[0].Pick.Type = %s.PickTypeNarrow
r[0].Pick.Narrow = 7
`, u.Package)
	case "writing_slack_batch", "writing_slack_unzeroed":
		count = 2
		setup = fmt.Sprintf(`r[0].ArrCount = 3
r[0].Arr[0] = 0x9999
r[0].Arr[1] = 0x9999
r[0].Arr[2] = 0x8888
r[0].S = "ZZZZ"
r[0].SLength = 4
r[0].B = "\xAA\xAA\xAA\xAA"
r[0].BLength = 4
r[0].Pick.Type = %[1]s.PickTypeWide
r[0].Pick.Wide = -1
r[1].ArrCount = 1
r[1].Arr[0] = 0x1122
r[1].Arr[1] = 0x9999
r[1].Arr[2] = 0x8888
r[1].S = "A"
r[1].SLength = 1
r[1].B = "\x42\xAA\xAA\xAA"
r[1].BLength = 1
r[1].Pick.Type = %[1]s.PickTypeNarrow
r[1].Pick.Narrow = 7
r[1].Pick.Wide = -1
`, u.Package)
		if scenario == "writing_slack_unzeroed" {
			target := "    var body = \"\"\n    for(values) v: body += schema_rt.fixed_record(TFixedHash, TFixedPut(schema_rt.zeros(35), 0, v))"
			repl := "    var body = \"\"\n    var scratch = schema_rt.zeros(35)\n    for(values) v:\n        scratch = TFixedPut(scratch, 0, v)\n        body += schema_rt.fixed_record(TFixedHash, scratch)"
			for name, data := range files {
				if strings.Contains(string(data), target) {
					files[name] = []byte(strings.ReplaceAll(string(data), target, repl))
				}
			}
		}
	case "writing_overcount":
		setup = `r[0].ArrCount = 4
`
	case "closure_small_42":
		setup = `r[0].X = 42
`
	case "outer_nested":
		setup = `r[0].Head = 7
r[0].Inner.A = 11
r[0].Inner.B = "one"
r[0].Inner.BLength = 3
r[0].Items[0].B = "hi"
r[0].Items[0].BLength = 2
r[0].Items[1].A = 33
r[0].Items[1].B = "six"
r[0].Items[1].BLength = 3
r[0].MaybePresent = true
r[0].Maybe.A = 44
r[0].Tail = 9
`
	case "w14_writer_offsets":
		setup = `r[0].A = 0xA1
r[0].B = 0x0B0B0B0B
r[0].Inner.P = 0x1E1E1E1E
r[0].Inner.Q = 0x2E2E
r[0].OptPresent = true
r[0].Opt = 0x0D0D0D0D
r[0].ArrCount = 2
r[0].Arr[0] = 0x3E3E
r[0].Arr[1] = 0x4E4E
r[0].S = "seven"
r[0].SLength = 5
r[0].Tail = 0x7A7A7A7A
`
	default:
		t.Fatalf("unknown SaveCustom scenario %q", scenario)
	}
	script := lobsterProofImports(u) + fmt.Sprintf(`let r:[%[1]s.%[2]s] = []
for(%[3]d): push(r, %[1]s.New%[2]s())
%[4]slet written, wire, reason = %[1]s.%[2]sFixedSaveChecked(r, schema_rt.zeros(%[1]s.%[2]sFixedMeasure(%[3]d)))
if written < 0:
    print("REFUSED " + reason)
else:
    assert write_file("out.bin", substring(wire, 0, written))
    print("OK")
`, u.Package, root, count, setup)
	out, wire := lobsterEvolutionRun(t, files, script, nil)
	out = strings.TrimSpace(out)
	if reason, ok := strings.CutPrefix(out, "REFUSED "); ok {
		return nil, reason
	}
	if out != "OK" || wire == nil {
		t.Fatalf("native save omitted its wire: %q", out)
	}
	return wire, ""
}

func (lobsterFixedProofRunner) HasFixedRoot(t *testing.T, u *ir.Unit, root string) bool {
	t.Helper()
	files, err := lobstertable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	needle := "def " + root + "FixedSave("
	for _, data := range files {
		if strings.Contains(string(data), needle) {
			return true
		}
	}
	return false
}

func lobsterEvolutionRun(t *testing.T, files map[string][]byte, script string, inputs map[string][]byte) (string, []byte) {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.lobster"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(lobsterrun.Run(t, dir, "main.lobster", inputs))
	wire, err := os.ReadFile(filepath.Join(dir, "out.bin"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return out, wire
}

func lobsterCompanionExpr(expr string, f *ir.Field, suffix string) string {
	name := ir.GoExportName(f.Name)
	slot := ir.GoExportName(f.Name + suffix)
	if i := strings.LastIndex(expr, name); i >= 0 {
		return expr[:i] + slot + expr[i+len(name):]
	}
	return slot
}
