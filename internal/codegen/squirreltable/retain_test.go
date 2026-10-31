package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// retainUnitFrom parses and checks one schema source into a unit, the way the
// Go oracle's unitFrom does (internal/codegen/gotable/retain_test.go).
func retainUnitFrom(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// TestSquirrelRetainMatchesIndependentRewrite is the Squirrel leg of
// docs/SPEC-TABLES.md §6.6: a newer writer's file, read by an older Squirrel
// build with retention on and saved back, must equal Go's independent retain
// rewrite byte for byte, and a retention buffer too small for a record must
// refuse that record exactly as Go's does.
func TestSquirrelRetainMatchesIndependentRewrite(t *testing.T) {
	t.Parallel()
	old := `package probe
enum Key { A, B }
fixed table Child { n int32 }
union Inner { child Child }
union Choice { child Child
 nested [..2]Inner
 empty }
table Root { link *Child
 direct Inner
 child Child
 optional ?Child
 array [..3]Child
 list []Child
 choices [..2]Choice
 keyed [Key]Child
 choice Choice }
`
	newer := strings.Replace(old, "table Child { n int32 }", "table Child { n int32\n extra string(16) }", 1)
	newer = strings.Replace(newer, " choice Choice }", " choice Choice\n future Choice }", 1)

	// THE PEER WRITES. It is the Go oracle's own text, one map row removed
	// because the Squirrel table surface does not map a map yet.
	model := tabletext.NewModel(retainUnitFrom(t, newer))
	v := model.New(model.Lookup("Root"))
	var report tabletext.Report
	text := `{"link":{"extra":"node"},"child":{"extra":"child"},"optional":{"extra":"optional"},"array":[{"extra":"array0"},{"n":3,"extra":"array1"}],"list":[{"extra":"list"}],"choices":[{"child":{"extra":"arm0"}},{"nested":[{"child":{"extra":"nested0"}},{"child":{"extra":"nested1"}}]}],"keyed":{"A":{"extra":"keyA"},"B":{"extra":"keyB"}},"choice":{"child":{"extra":"arm"}},"future":{"child":{"extra":"unknown union"}}}`
	if !model.Read(v, []byte(text), &report) {
		t.Fatal(report)
	}
	wire, err := tablewire.Encode(model, v)
	if err != nil {
		t.Fatal(err)
	}

	// THE OLDER READER, WITH GO'S RETAIN ORACLE.
	reader := tabletext.NewModel(retainUnitFrom(t, old))
	value := reader.New(reader.Lookup("Root"))
	store := &tablewire.Retain{Capacity: 1 << 20, IdCapacity: 1024}
	report = tabletext.Report{}
	ok, err := tablewire.DecodeRetain(reader, value, wire, store, &report)
	if !ok || err != nil || report.Malformed {
		t.Fatalf("oracle read %v %v %+v", ok, err, report)
	}
	want, err := tablewire.EncodeRetain(reader, value, store, &report)
	if err != nil {
		t.Fatal(err)
	}

	// THE SAME OLDER READER IN SQUIRREL.
	u := retainUnitFrom(t, old)
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "probe")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}

	main := `local I = dofile("inputs.nut", true);
local schema = dofile("probe/schema_module.nut", true);
local T = schema.require("ProbeTables");
local R = schema.require("schema_table_retain");
local rep = function() { return { unknown=0, kindMismatch=0, widened=0, clamped=0, duplicate=0, malformed=false, refused=false }; };
local r = rep();
local store = { capacity = 1048576, idCapacity = 1024 };
local v = R.LoadRetain(T, "Root", I.wire, store, r);
if (v == null) { print("loadnull\n"); return; }
local m = R.MeasureRetain(T, "Root", v, store);
local out = blob(m);
local n = R.SaveRetain(T, "Root", v, store, out, r);
print("measure " + m + "\n");
print("save " + n + "\n");
print("report " + r.retained + "," + r.retainLost + "," + r.unknown + "," + (r.malformed ? 1 : 0) + "\n");
::hexline(out);
local r2 = rep();
local s2 = { capacity = 8, idCapacity = 1024 };
local v2 = R.LoadRetain(T, "Root", I.wire, s2, r2);
print("bounds " + r2.retained + "," + r2.retainLost + "\n");
`
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(main))
	stdout := string(sqrun.Run(t, dir, "main.nut", map[string][]byte{"wire": wire}))
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("the retain probe printed %d lines, want 5:\n%s", len(lines), stdout)
	}
	var measure, save int
	var retained, lost, unknown, malformed int
	if _, err := fmt.Sscanf(lines[0], "measure %d", &measure); err != nil {
		t.Fatalf("probe line %q: %v", lines[0], err)
	}
	if _, err := fmt.Sscanf(lines[1], "save %d", &save); err != nil {
		t.Fatalf("probe line %q: %v", lines[1], err)
	}
	if _, err := fmt.Sscanf(lines[2], "report %d,%d,%d,%d", &retained, &lost, &unknown, &malformed); err != nil {
		t.Fatalf("probe line %q: %v", lines[2], err)
	}
	have := lines[3]
	if wantHex := fmt.Sprintf("%x", want); have != wantHex {
		t.Errorf("the Squirrel retain rewrite is not Go's rewrite\n got %s\nwant %s", have, wantHex)
	}
	if save != len(want) || measure != len(want) {
		t.Errorf("measure %d save %d, want %d", measure, save, len(want))
	}
	gotMalformed := malformed != 0
	if retained != report.Retained || lost != report.RetainLost || unknown != report.Unknown || gotMalformed != report.Malformed {
		t.Errorf("the Squirrel report retained=%d lost=%d unknown=%d malformed=%v, Go's is retained=%d lost=%d unknown=%d malformed=%v",
			retained, lost, unknown, gotMalformed, report.Retained, report.RetainLost, report.Unknown, report.Malformed)
	}

	// THE CAPTURE BOUND: the same union where Go's does, one record at a time.
	small := &tablewire.Retain{Capacity: 8, IdCapacity: 1024}
	var bounds tabletext.Report
	smallReader := tabletext.NewModel(retainUnitFrom(t, old))
	smallValue := smallReader.New(smallReader.Lookup("Root"))
	if _, err := tablewire.DecodeRetain(smallReader, smallValue, wire, small, &bounds); err != nil {
		t.Fatal(err)
	}
	var bRetained, bLost int
	if _, err := fmt.Sscanf(lines[4], "bounds %d,%d", &bRetained, &bLost); err != nil {
		t.Fatalf("probe line %q: %v", lines[4], err)
	}
	if bRetained != bounds.Retained || bLost != bounds.RetainLost {
		t.Errorf("the Squirrel capture bound retained=%d lost=%d, Go's is retained=%d lost=%d",
			bRetained, bLost, bounds.Retained, bounds.RetainLost)
	}
	if bRetained != 0 || bLost == 0 {
		t.Errorf("the capture bound kept %d and lost %d, want 0 kept and some lost", bRetained, bLost)
	}
	t.Logf("compared %d retained fields; the rewrite is %d bytes; the bound lost %d", retained, len(want), bLost)
}
