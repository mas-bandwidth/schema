package luatable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLuaRetainUnknown is the Lua leg of RETAIN-UNKNOWN (docs/SPEC-TABLES.md
// §6.6): the manifest's retain_round_trip pairs RT2 (the newer writer) with
// RT1 (the older unit that cannot name what it wrote). LoadRetain keeps the
// eleven fields RT1 cannot name — nine whole, the two node-index classes
// excluded — and the SaveRetain it writes must equal the generated Go
// reference's SaveRetain byte for byte: the id table is ordered by the newer
// writer's first use, so a raw copy of the skipped bytes is not enough. The
// fixed-class root Inner is refused by the same sentence the Go const carries.
func TestLuaRetainUnknown(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "go and the pinned lua")
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	var row manifestRow
	for _, r := range manifestRows(t, manifest, "retain") {
		if r.caseName == "retain_round_trip" {
			row = r
		}
	}
	if row.caseName == "" {
		t.Fatal("the manifest has no retain_round_trip row")
	}
	var paths []string
	for _, p := range units[row.unit] {
		paths = append(paths, filepath.Join(repo, p))
	}
	u := luarun.LoadUnit(t, paths...)
	wirePath, err := filepath.Abs(filepath.Join(repo, row.wire))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(wirePath)
	if err != nil {
		t.Fatal(err)
	}

	// THE GO ORACLE (docs/SPEC-TABLES.md §6.6; internal/codegen/gotable/
	// retain.go): the same unit generated for Go, loading this wire into a
	// region and saving it back with retention. Its bytes and its report are
	// the expectation, never a fixture.
	goSaved, goRetained, goLost, goRefusal := goRetainOracle(t, u, wirePath)

	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate Lua: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	probe := `local T = require("` + ModuleName(u) + `")
local retain_mod = require("retain")
local inputs = require("inputs")
local function hex(s)
  local out = {}
  for i = 1, #s do out[i] = string.format("%02x", s:byte(i)) end
  return table.concat(out)
end
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, retained = 0, retainLost = 0 }
local retain = { list = {}, used = 0 }
local v = retain_mod.LoadRetain(T, "` + row.root + `", inputs.wire, retain, report)
if v == nil then print("load refused") os.exit(1) end
local saved = retain_mod.SaveRetain(T, "` + row.root + `", v, retain, report)
if saved == nil then print("save refused") os.exit(1) end
local measure = retain_mod.MeasureRetain(T, "` + row.root + `", v, retain)
local verdict = (saved == inputs.go) and "same" or hex(saved)
print(verdict .. ":" .. measure .. ":" .. report.retained .. ":" .. report.retainLost)
local ok, msg = pcall(retain_mod.LoadRetain, T, "Inner", inputs.wire, retain, report)
if ok then print("fixed root loaded") os.exit(1) end
print("refusal:" .. tostring(msg))
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": wire, "go": goSaved})), "\n")
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("probe output %q, want two lines", out)
	}
	parts := strings.SplitN(lines[0], ":", 4)
	if len(parts) != 4 {
		t.Fatalf("round trip line %q", lines[0])
	}
	if parts[0] != "same" {
		t.Fatalf("SaveRetain differs from generated Go SaveRetain (Lua hex %s, Go hex %x)", parts[0], goSaved)
	}
	if parts[1] != fmt.Sprint(len(goSaved)) {
		t.Errorf("MeasureRetain %s, Go save %d bytes", parts[1], len(goSaved))
	}
	// THE REPORT ACCUMULATES ACROSS THE PAIR (§6.6): nine retained at the
	// load, two excluded classes counted, and the save places all nine.
	if parts[2] != fmt.Sprint(goRetained) || parts[3] != fmt.Sprint(goLost) {
		t.Errorf("retained,retain_lost = %s,%s, Go = %d,%d", parts[2], parts[3], goRetained, goLost)
	}
	want := "refusal:" + goRefusal
	if lines[1] != want {
		t.Errorf("fixed root refusal %q, want %q", lines[1], want)
	}
}

// goRetainOracle generates the Go reference for u and runs one retaining round
// trip over wirePath, answering the bytes its SaveRetain wrote, its accumulated
// report counters, and the fixed-root refusal's sentence from the generated
// const. The const is what the Lua refusal must name, so it is read from the
// generated Go source rather than restated here.
func goRetainOracle(t *testing.T, u *ir.Unit, wirePath string) ([]byte, int, int, string) {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatalf("generate Go: %v", err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatalf("generate Go tables: %v", err)
	}
	maps.Copy(files, tables)
	files["go.mod"] = []byte("module probe\n\ngo 1.26\n")
	dir := t.TempDir()
	saved := filepath.Join(dir, "go_save.bin")
	counters := filepath.Join(dir, "go_report.txt")
	files["retain_probe_test.go"] = []byte(fmt.Sprintf(`package %s
import("os";"strconv";"testing";"unsafe")
func TestGoRetainSave(t *testing.T){
 wire,err:=os.ReadFile(%q);if err!=nil{t.Fatal(err)}
 n:=NodeLoadMeasure(wire);if n<0{t.Fatal("measure")}
 raw:=make([]byte,n+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;region:=raw[off:off+uintptr(n)]
 retain:=TableRetain{Bytes:make([]byte,1<<20),Ids:make([]TableRetainID,1024)};var report TableReport
 v:=NodeLoadRetain(region,wire,&retain,&report);if v==nil||report.Malformed{t.Fatalf("load %%+v",report)}
 size:=NodeMeasureRetain(v,&retain);out:=make([]byte,size)
 if NodeSaveRetain(v,&retain,out,&report)!=size{t.Fatalf("save %%+v",report)}
 if err:=os.WriteFile(%q,out,0o644);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(%q,[]byte(strconv.Itoa(int(report.Retained))+" "+strconv.Itoa(int(report.RetainLost))),0o644);err!=nil{t.Fatal(err)}
}
`, u.Package, wirePath, saved, counters))
	for name, data := range files {
		target := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-run", "^TestGoRetainSave$", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Go retain reference: %v\n%s", err, out)
	}
	data, err := os.ReadFile(saved)
	if err != nil {
		t.Fatal(err)
	}
	report, err := os.ReadFile(counters)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(report))
	if len(fields) != 2 {
		t.Fatalf("generated Go report %q", report)
	}
	retained, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatal(err)
	}
	lost, err := strconv.Atoi(fields[1])
	if err != nil {
		t.Fatal(err)
	}
	return data, retained, lost, goConst(t, files, "InnerLoadRetain")
}

// goConst answers the unquoted string of `const <name> = "..."` in the
// generated Go source: the fixed-class root's refusal is emitted by name as a
// const (internal/codegen/gotable/retain.go, docs/SPEC-TABLES.md §6.6, §11).
func goConst(t *testing.T, files map[string][]byte, name string) string {
	t.Helper()
	needle := "const " + name + " = "
	for _, data := range files {
		for line := range strings.SplitSeq(string(data), "\n") {
			if rest, ok := strings.CutPrefix(line, needle); ok {
				quoted, err := strconv.Unquote(strings.TrimSpace(rest))
				if err != nil {
					t.Fatalf("generated const %s: %v", name, err)
				}
				return quoted
			}
		}
	}
	t.Fatalf("generated Go has no const %s", name)
	return ""
}
