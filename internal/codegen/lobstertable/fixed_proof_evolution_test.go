package lobstertable

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
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The shared evolution suite supplies real C++ files, prior locked layouts,
// retired entries and corrupted controls. The adapter only projects the native
// result: it does not translate a successful read into a refusal.
func TestLobsterFixedProofEvolution(t *testing.T) {
	fixedproof.RunEvolutionSuite(t, filepath.Join("..", "..", ".."), lobsterFixedProofRunner{})
}

var _ fixedproof.EvoRunner = lobsterFixedProofRunner{}

func (lobsterFixedProofRunner) LoadLineage(t *testing.T, u *ir.Unit, root string, lineage map[string][]fixedproof.LineageEntry, wire []byte, capacity int) (int, string, map[string]string, []byte) {
	t.Helper()
	files, err := lobster.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	var tables map[string][]byte
	if len(lineage) == 0 {
		tables, err = Generate(u)
	} else {
		locked := map[string][]FixedLineageEntry{}
		for name, entries := range lineage {
			for _, e := range entries {
				locked[name] = append(locked[name], FixedLineageEntry{
					Wire: e.Wire, Layout: e.Layout, Record: e.Record, Retired: e.Retired, Reason: e.Reason,
				})
			}
		}
		tables, err = GenerateLineage(u, locked)
	}
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if capacity < 0 {
		// The native API takes caller-owned records; the shared fixtures need
		// at most a handful. An explicit capacity is used unchanged.
		capacity = 16
	}
	pkg := u.Package
	st := fixedproof.RootStruct(t, u, root)
	var dump strings.Builder
	lobsterProofDumpFields(&dump, st, "r[i]", "", "    ")
	script := lobsterProofImports(u) + fmt.Sprintf(`let data = read_file("in.bin")
assert(data)
let r:[%[1]s.%[2]s] = []
for(%[3]d): push(r, %[1]s.New%[2]s())
let report = schema_rt.TableReport {}
let plan:[schema_rt.TableFixedEntry] = []
for(4096): push(plan, schema_rt.TableFixedEntry {})
let count = %[1]s.%[2]sFixedLoad(r, data, plan, report)
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
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, line := range lines {
		if after, ok := strings.CutPrefix(line, "REFUSED "); ok {
			return -1, strings.TrimSpace(after), fields, nil
		}
		if after, ok := strings.CutPrefix(line, "OK "); ok {
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
		return count, reason, fields, resaved
	}
	t.Fatalf("native evolution probe omitted its result: %s", out)
	return -1, "", nil, nil
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

// Project the scalar fields the evolution contract checks, using the checked
// schema for native member spellings. The file oracle separately checks all
// array, text and wide-number storage by complete byte-identical write-back.
func lobsterProofDumpFields(out *strings.Builder, st *ir.Struct, expr, path, indent string) {
	for _, f := range st.Fields {
		lobsterProofDumpField(out, f, expr+"."+ir.GoExportName(f.Name), path+"."+f.Name, indent)
	}
}

func lobsterProofDumpField(out *strings.Builder, f *ir.Field, expr, path, indent string) {
	printField := func(key, value string) {
		fmt.Fprintf(out, "%sprint(\"FIELD r\" + string(i) + %q + string(%s))\n", indent, key+"=", value)
	}
	if f.Type.Optional {
		fmt.Fprintf(out, "%sprint(\"FIELD r\" + string(i) + %q + if %s: \"true\" else: \"false\")\n", indent, path+"_present=", slotExpr(expr, f, "Present"))
	}
	if f.Array != ir.ArrayNone || f.KeyEnumRef != nil {
		return
	}
	switch f.Type.Kind {
	case ir.TInt, ir.TBits, ir.TFixed:
		if f.Type.Width <= 64 {
			printField(path, expr)
		}
	case ir.TBool:
		fmt.Fprintf(out, "%sprint(\"FIELD r\" + string(i) + %q + if %s: \"true\" else: \"false\")\n", indent, path+"=", expr)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			lobsterProofDumpFields(out, ref, expr, path, indent)
		case *ir.Union:
			for _, arm := range ref.Variants {
				if arm.F != nil {
					lobsterProofDumpField(out, arm.F, expr+"."+ir.GoExportName(arm.Name), path+"."+arm.Name, indent)
				}
			}
		case *ir.Enum, *ir.Flags:
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
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	count, setup := 1, ""
	switch scenario {
	case "batch_over_4096":
		count = 4097
	case "optional_absent_zeros":
		setup = `r[0].Plain.A = 1
r[0].Plain.B = 2
r[0].OptPresent = false
r[0].Opt.A = 0x77777777
r[0].Opt.B = 0x66666666
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
	files, err := Generate(u)
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
