package luatable

import (
	"bytes"
	"encoding/hex"
	"fmt"
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
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
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
local report = { damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0, widened = 0, repeatedKey = 0, retained = 0, retainLost = 0 }
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

type retainConformanceRow struct {
	name, unit, root, wire string
	capacity, ids, save    string
}

func loadRetainConformanceRows(t *testing.T, manifestPath string) []retainConformanceRow {
	t.Helper()
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var out []retainConformanceRow
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || f[0] != "retain" {
			continue
		}
		// Accept both the 7-field derived manifest line:
		//   retain <case> <unit> <root> <wire> <capacity> <ids>
		// and the 8-field MANIFEST.txt line:
		//   retain <case> <unit> <root> <wire> <capacity> <ids> <save|->
		if len(f) != 7 && len(f) != 8 {
			t.Fatalf("retain row must have 7 or 8 fields: %s", line)
		}
		save := "-"
		if len(f) == 8 {
			save = f[7]
		}
		out = append(out, retainConformanceRow{
			name:     f[1],
			unit:     f[2],
			root:     f[3],
			wire:     f[4],
			capacity: f[5],
			ids:      f[6],
			save:     save,
		})
	}
	return out
}

func runLuaRetainConformanceRow(t *testing.T, repo string, u *ir.Unit, files map[string][]byte, r retainConformanceRow) (verdict string, jsonText []byte, saved []byte, reloadJSON []byte) {
	t.Helper()
	wire, err := os.ReadFile(filepath.Join(repo, r.wire))
	if err != nil {
		t.Fatal(err)
	}
	ids := "131072"
	if r.ids != "full" {
		ids = r.ids
	}
	short := "false"
	if r.capacity == "short" {
		short = "true"
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	probe := fmt.Sprintf(`local T = require(%q)
local load = require("load")
local json = require("json")
local retain = require("retain")
local inputs = require("inputs")

local function hex(s)
  local out = {}
  for i = 1, #s do out[i] = string.format("%%02x", s:byte(i)) end
  return table.concat(out)
end

local function newReport()
  return {
    damaged = false,
    refused = 0,
    unknown = 0,
    kindChanged = 0,
    outOfRange = 0,
    widened = 0,
    repeatedKey = 0,
    retained = 0,
    retainLost = 0,
  }
end

local ROOMY = 1 << 20
local ids = %s
local store = { list = {}, used = 0, capacity = ROOMY, idCapacity = ids }
if %s then
  if retain.LoadRetain(T, %q, inputs.wire, store, newReport()) == nil then
    error("capacity probe refused")
  end
  store = { list = {}, used = 0, capacity = math.max(0, store.used - 1), idCapacity = ids }
end
local report = newReport()
local v = retain.LoadRetain(T, %q, inputs.wire, store, report)
if v == nil or report.damaged then
  print("refused")
  print("")
  print("")
  print("")
  os.exit(0)
end
local size = retain.MeasureRetain(T, %q, v, store)
local saveRep = newReport()
local out = retain.SaveRetain(T, %q, v, store, saveRep)
assert(out ~= nil and size == #out, "MeasureRetain/SaveRetain mismatch")
local again = newReport()
local second = retain.SaveRetain(T, %q, v, store, again)
assert(second == out and again.retainLost == saveRep.retainLost, "SaveRetain not repeatable")
local text = assert(json.ToJson(T, %q, v))
local v2 = assert(load.Load(T, %q, out, newReport()))
local text2 = assert(json.ToJson(T, %q, v2))
print("read")
print(hex(text))
print(hex(out))
print(hex(text2))
`, ModuleName(u), ids, short, r.root, r.root, r.root, r.root, r.root, r.root, r.root, r.root)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": wire})), "\n")
	lines := strings.Split(raw, "\n")
	if len(lines) != 4 {
		t.Fatalf("%s: unexpected output %q", r.name, raw)
	}
	verdict = lines[0]
	if lines[1] != "" {
		jsonText, err = hex.DecodeString(lines[1])
		if err != nil {
			t.Fatal(err)
		}
	}
	if lines[2] != "" {
		saved, err = hex.DecodeString(lines[2])
		if err != nil {
			t.Fatal(err)
		}
	}
	if lines[3] != "" {
		reloadJSON, err = hex.DecodeString(lines[3])
		if err != nil {
			t.Fatal(err)
		}
	}
	return verdict, jsonText, saved, reloadJSON
}

// TestLuaRetainConformanceSurface exercises all eight retain conformance rows
// (both the 7-field derived manifest line format and the 8-field MANIFEST.txt
// line format) through LoadRetain, ToJson, MeasureRetain, SaveRetain, and
// reload via Load, holding the Lua output to the Go tablewire reference and
// the pinned retain-values and retain-save goldens.
func TestLuaRetainConformanceSurface(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned lua")
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	rows := loadRetainConformanceRows(t, manifest)
	if len(rows) != 8 {
		t.Fatalf("got %d retain rows, want 8", len(rows))
	}
	// Also verify parsing the 7-field derived manifest lines (where the save
	// column is withheld by the conformance harness).
	var derivedBuf strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&derivedBuf, "retain %s %s %s %s %s %s\n", r.name, r.unit, r.root, r.wire, r.capacity, r.ids)
	}
	derivedPath := filepath.Join(t.TempDir(), "DERIVED_MANIFEST.txt")
	if err := os.WriteFile(derivedPath, []byte(derivedBuf.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	derivedRows := loadRetainConformanceRows(t, derivedPath)
	if len(derivedRows) != len(rows) {
		t.Fatalf("derived manifest parsed %d rows, want %d", len(derivedRows), len(rows))
	}

	var paths []string
	for _, p := range units["tblrt1"] {
		paths = append(paths, filepath.Join(repo, p))
	}
	u := luarun.LoadUnit(t, paths...)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			verdict, jsonText, saved, reloadJSON := runLuaRetainConformanceRow(t, repo, u, files, r)
			if verdict != "read" {
				t.Fatalf("%s: verdict = %q, want read", r.name, verdict)
			}
			wantJSONPath := filepath.Join(repo, "testdata", "conformance", "tables", "retain-values", r.name+".json")
			wantJSON, err := os.ReadFile(wantJSONPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(jsonText, wantJSON) {
				t.Errorf("%s: ToJson differs from %s:\ngot:  %s\nwant: %s", r.name, wantJSONPath, jsonText, wantJSON)
			}
			if !bytes.Equal(reloadJSON, wantJSON) {
				t.Errorf("%s: reloaded ToJson differs from %s", r.name, wantJSONPath)
			}

			// Compare against the Go tablewire reference engine.
			wire, err := os.ReadFile(filepath.Join(repo, r.wire))
			if err != nil {
				t.Fatal(err)
			}
			m := tabletext.NewModel(u)
			idCap := 1 << 17
			if r.ids != "full" {
				idCap, err = strconv.Atoi(r.ids)
				if err != nil {
					t.Fatal(err)
				}
			}
			capBytes := 1 << 20
			if r.capacity == "short" {
				probeInst := m.New(m.Lookup(r.root))
				probeStore := &tablewire.Retain{Capacity: 1 << 20, IdCapacity: idCap}
				var probeRep tabletext.Report
				if ok, err := tablewire.DecodeRetain(m, probeInst, wire, probeStore, &probeRep); err != nil || !ok {
					t.Fatalf("%s: reference capacity probe failed: %v", r.name, err)
				}
				capBytes = probeStore.Used() - 1
			}
			inst := m.New(m.Lookup(r.root))
			store := &tablewire.Retain{Capacity: capBytes, IdCapacity: idCap}
			var rep tabletext.Report
			if ok, err := tablewire.DecodeRetain(m, inst, wire, store, &rep); err != nil || !ok {
				t.Fatalf("%s: reference DecodeRetain failed: %v", r.name, err)
			}
			wantSaved, err := tablewire.EncodeRetain(m, inst, store, &rep)
			if err != nil {
				t.Fatalf("%s: reference EncodeRetain failed: %v", r.name, err)
			}
			if !bytes.Equal(saved, wantSaved) {
				t.Errorf("%s: saved bytes differ from tablewire.EncodeRetain:\ngot:  %x\nwant: %x", r.name, saved, wantSaved)
			}
			if r.save != "-" {
				pinned, err := os.ReadFile(filepath.Join(repo, r.save))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(saved, pinned) {
					t.Errorf("%s: saved bytes differ from pinned %s:\ngot:  %x\nwant: %x", r.name, r.save, saved, pinned)
				}
			}
		})
	}
}

// TestLuaRetainConformanceSurfaceNegativeControl patches emit_tail in
// retain.lua so retained records are not written to the output buffer, and
// verifies that retain_round_trip's saved bytes diverge from the reference.
func TestLuaRetainConformanceSurfaceNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned lua")
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	rows := loadRetainConformanceRows(t, manifest)
	var row retainConformanceRow
	for _, r := range rows {
		if r.name == "retain_round_trip" {
			row = r
			break
		}
	}
	var paths []string
	for _, p := range units[row.unit] {
		paths = append(paths, filepath.Join(repo, p))
	}
	u := luarun.LoadUnit(t, paths...)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := os.ReadFile(filepath.Join(repo, row.save))
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "      if ref ~= nil and out_payload(o, rec.kind, 0) and o.r.at == o.r.lim then\n        w_raw(w, table.concat(probe.parts))\n"
	src := string(files["retain.lua"])
	if n := strings.Count(src, anchor); n != 1 {
		t.Fatalf("retain.lua matched anchor %d times, want 1", n)
	}
	sabotaged := maps.Clone(files)
	sabotaged["retain.lua"] = []byte(strings.Replace(src, anchor, "      if false then\n        w_raw(w, table.concat(probe.parts))\n", 1))
	_, _, saved, _ := runLuaRetainConformanceRow(t, repo, u, sabotaged, row)
	if bytes.Equal(saved, pinned) {
		t.Fatal("negative control: retain_round_trip saved bytes stayed equal with emit_tail disabled")
	}
}

// goRetainOracle generates the Go reference for u and runs one retaining round
// trip over wirePath, answering the bytes its SaveRetain wrote, its accumulated
// report counters, and the fixed-root refusal's sentence from the generated
// const.
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

	dir := t.TempDir()
	saved := filepath.Join(dir, "saved.bin")
	counters := filepath.Join(dir, "counters.txt")
	files["go.mod"] = []byte("module retainprobe\n\ngo 1.26\n")
	files["retain_probe_test.go"] = []byte(fmt.Sprintf(`package %s
import ("fmt";"os";"testing";"unsafe")
func TestGoRetainOracle(t *testing.T){
 wire,err:=os.ReadFile(%q);if err!=nil{t.Fatal(err)}
 n:=NodeLoadMeasure(wire);if n<0{t.Fatalf("measure %%d",n)}
 raw:=make([]byte,n+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;region:=raw[off:off+uintptr(n)]
 retain:=TableRetain{Bytes:make([]byte,1<<20),Ids:make([]TableRetainID,1024)};var report TableReport
 v:=NodeLoadRetain(region,wire,&retain,&report);if v==nil{t.Fatalf("load %%+v",report)}
 size:=NodeMeasureRetain(v,&retain);out:=make([]byte,size)
 if NodeSaveRetain(v,&retain,out,&report)!=size{t.Fatalf("save %%+v",report)}
 if err:=os.WriteFile(%q,out,0o644);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(%q,[]byte(fmt.Sprintf("%%d %%d",report.Retained,report.RetainLost)),0o644);err!=nil{t.Fatal(err)}
}
`, u.Package, wirePath, saved, counters))

	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-run", "^TestGoRetainOracle$", ".")
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
