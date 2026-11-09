package luautable_test

// THE WIDENING CELL (docs/SPEC-TABLES.md §4, §4.1; the variable-tables row
// "widening on read, and the refusal reasons"). The strict read is ONE read:
// the whole value or a refusal (§6.5). The named gap is list_scalar_widen:
// the committed older-writer witness writes a list whose region uses the
// declared width while its elements ride narrower, so the array's L holds
// bytes past its elements — the reference engine refuses it as damage, and
// the Luau runtime must hold the same verdict instead of widening the list
// scalar and recovering.

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

var luauVtWideningCell = luauVTCell{
	row: "widening",
	cases: []string{
		"field_scalar_widen",
		"k1_as_k2",
		"list_scalar_widen",
		"map_key_widen",
		"previous_form_wide_blob",
		"scalars_full_as_2",
	},
}

// genListdemoUnit emits the Luau table surface for the committed tables/lists
// unit and returns the directory holding it and the unit itself. The emitted
// schema_load.luau is the real artifact the runtime's strict read rides.
func genListdemoUnit(t *testing.T) (string, *ir.Unit) {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "tables", "lists")})
	if err != nil {
		t.Fatalf("gather listdemo schema paths: %v", err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load listdemo unit: %v", err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatalf("luau type generate: %v", err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatalf("luautable generate: %v", err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, u
}

func readEmittedRuntime(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "schema_load.luau"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// luauArrayTrailingBytesRefused is the source-rule the widening cell holds: an
// array whose L holds bytes past its elements is refused, not recovered. In
// schema_load.luau's array body this is the single line that marks the read
// malformed when every declared element was read yet the body still holds
// bytes — the clause that makes list_scalar_widen a refusal.
func luauArrayTrailingBytesRefused(src string) bool {
	const anchor = "got == keep and keep == count and sub.at < sub.e"
	for line := range strings.SplitSeq(src, "\n") {
		if strings.Contains(line, anchor) && strings.Contains(line, "report.malformed = true") {
			return true
		}
	}
	return false
}

// luauScalarElementWidens is the widening-on-read half: a scalar element
// decodes at the wire kind the array header carried, the widening rungs exist
// (a signed kind up its ladder), and the element-kind widening is counted per
// field (§4, §4.1).
func luauScalarElementWidens(src string) bool {
	return strings.Contains(src, "scalar_at(r, e, ek, report)") &&
		strings.Contains(src, "return kind >= 2 and kind < declared") &&
		strings.Contains(src, "report.widened = report.widened + 1")
}

// refuseListScalarWiden pins the reference engine's verdict: the committed
// list_scalar_widen.bin wire is a refusal (array body bytes past its
// elements), because the strict read is one read.
func refuseListScalarWiden(t *testing.T, u *ir.Unit) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "fuzz-vectors", "list_scalar_widen.bin"))
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	st := m.Lookup("Save")
	if st == nil {
		t.Fatal("the listdemo unit has no root Save")
	}
	var report tabletext.Report
	if ok, _ := tablewire.Decode(m, m.New(st), data, &report); ok {
		t.Fatal("the reference engine read list_scalar_widen; the strict read must refuse")
	}
	if !report.Refused || !strings.Contains(report.Reason, "bytes left inside an array") {
		t.Fatalf("reference refusal reason: %q", report.Reason)
	}
}

func TestLuauVtWideningCell(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "luau (it runs the emitted table runtime)")

	dir, u := genListdemoUnit(t)
	refuseListScalarWiden(t, u)

	src := readEmittedRuntime(t, dir)
	if !luauArrayTrailingBytesRefused(src) {
		t.Fatal("the emitted runtime does not refuse an array body with bytes past its elements")
	}
	if !luauScalarElementWidens(src) {
		t.Fatal("the emitted runtime does not widen a scalar element on read")
	}

	fails, held := vtCellFailures(t, luauVtWideningCell, nil)
	for _, f := range fails {
		t.Error(f)
	}
	if held != len(luauVtWideningCell.cases) {
		t.Fatalf("widening cell held %d rows, want %d", held, len(luauVtWideningCell.cases))
	}
}

func TestLuauVtWideningCellNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "luau (it runs the emitted table runtime)")

	dir, _ := genListdemoUnit(t)
	src := readEmittedRuntime(t, dir)

	const anchor = "got == keep and keep == count and sub.at < sub.e"
	if !strings.Contains(src, anchor) {
		t.Fatal("NEGATIVE CONTROL FAILED: the real artifact has no trailing-bytes refusal to plant over")
	}
	patchedLines := strings.Split(src, "\n")
	replaced := false
	for i, line := range patchedLines {
		if !replaced && strings.Contains(line, anchor) && strings.Contains(line, "report.malformed = true") {
			patchedLines[i] = strings.Replace(line, "report.malformed = true", "report.malformed = false", 1)
			replaced = true
		}
	}
	patched := strings.Join(patchedLines, "\n")
	if !replaced {
		t.Fatal("NEGATIVE CONTROL FAILED: the trailing-bytes refusal line was not found")
	}
	if patched == src {
		t.Fatal("NEGATIVE CONTROL FAILED: planting the fault did not change the real artifact")
	}
	if luauArrayTrailingBytesRefused(patched) {
		t.Fatal("NEGATIVE CONTROL FAILED: removing the trailing-bytes refusal left the strict-read check green")
	}

	controls := []struct {
		name    string
		patches []vtPatch
	}{
		{
			name: "allow-array-trailing-bytes",
			patches: []vtPatch{
				{
					file: "schema_load.luau",
					from: "\tif got == keep and keep == count and sub.at < sub.e then report.malformed = true end\n",
					to:   "\tif false then report.malformed = true end\n",
				},
				{
					file: "schema_load.luau",
					from: "\t\t-- bytes left inside an array's or map's L after its elements are damage\n\t\treturn p ~= e\n",
					to:   "\t\treturn false\n",
				},
			},
		},
		{
			name: "disable-signed-widen-ladder",
			patches: []vtPatch{{
				file: "schema_load.luau",
				from: "\tif declared == 3 or declared == 4 or declared == 5 then return kind >= 2 and kind < declared end\n",
				to:   "\tif declared == 3 or declared == 4 or declared == 5 then return false end\n",
			}},
		},
	}
	for _, ctl := range controls {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			fails, _ := vtCellFailures(t, luauVtWideningCell, ctl.patches)
			if len(fails) == 0 {
				t.Fatalf("NEGATIVE CONTROL FAILED: %s left the widening row green", ctl.name)
			}
			t.Logf("%s caught: %s", ctl.name, fails[0])
		})
	}
}
