package luatable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLuaRetainUnknown is the Lua leg of RETAIN-UNKNOWN (docs/SPEC-TABLES.md
// §6.6): RT1 (the older reader) loads RT2's wire with LoadRetain, and the
// SaveRetain it writes must equal the generated Go reference's SaveRetain byte
// for byte — the id table is reordered by the newer writer's field order, so a
// raw copy of the skipped bytes is not enough. The fixed-class root Inner is
// refused by the same sentence the Go const carries.
func TestLuaRetainUnknown(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	wirePath, err := filepath.Abs(filepath.Join(repo, "testdata", "wire", "tables", "retain_rt2.bin"))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(wirePath)
	if err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "RT1.schema"))

	// THE GO ORACLE (docs/SPEC-TABLES.md §6.6; internal/codegen/gotable/retain.go):
	// the same unit generated for Go, loading this wire into a region and saving
	// it back with retention. Its bytes are the expectation, never a fixture.
	goSaved := goRetainSave(t, u, wirePath)

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
local v = retain_mod.LoadRetain(T, "Node", inputs.wire, retain, report)
if v == nil then print("load refused") os.exit(1) end
local saved = retain_mod.SaveRetain(T, "Node", v, retain, report)
if saved == nil then print("save refused") os.exit(1) end
local measure = retain_mod.MeasureRetain(T, "Node", v, retain)
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
	// RT2's wire carries eleven fields RT1 cannot name, nine retained and the
	// two kind-17 fields the resolving walk excludes (§6.6).
	if parts[2] != "9" || parts[3] != "2" {
		t.Errorf("retained,retain_lost = %s,%s, want 9,2", parts[2], parts[3])
	}
	want := "refusal:Inner: retention requires a variable root and its region directory"
	if lines[1] != want {
		t.Errorf("fixed root refusal %q, want %q", lines[1], want)
	}
}

// goRetainSave generates the Go reference for u and runs one retaining round
// trip over wirePath, returning the bytes its SaveRetain wrote.
func goRetainSave(t *testing.T, u *ir.Unit, wirePath string) []byte {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatalf("generate Go: %v", err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatalf("generate Go tables: %v", err)
	}
	for name, data := range tables {
		files[name] = data
	}
	files["go.mod"] = []byte("module probe\n\ngo 1.26\n")
	dir := t.TempDir()
	saved := filepath.Join(dir, "go_save.bin")
	files["retain_probe_test.go"] = []byte(fmt.Sprintf(`package %s
import("os";"testing";"unsafe")
func TestGoRetainSave(t *testing.T){
 wire,err:=os.ReadFile(%q);if err!=nil{t.Fatal(err)}
 n:=NodeLoadMeasure(wire);if n<0{t.Fatal("measure")}
 raw:=make([]byte,n+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;region:=raw[off:off+uintptr(n)]
 retain:=TableRetain{Bytes:make([]byte,1<<20),Ids:make([]TableRetainId,1024)};var report TableReport
 v:=NodeLoadRetain(region,wire,&retain,&report);if v==nil||report.Malformed{t.Fatalf("load %%+v",report)}
 size:=NodeMeasureRetain(v,&retain);out:=make([]byte,size);report=TableReport{}
 if NodeSaveRetain(v,&retain,out,&report)!=size{t.Fatalf("save %%+v",report)}
 if err:=os.WriteFile(%q,out,0o644);err!=nil{t.Fatal(err)}
}
`, u.Package, wirePath, saved))
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
	return data
}
