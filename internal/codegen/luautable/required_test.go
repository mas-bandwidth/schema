package luautable_test

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The two required-field shapes named by the card: `| required` inside an if
// guard (TestGoRequiredInOffBranch) and a required range-excluding-zero field
// with a nested required field (TestGoRequiredExactFieldHasNoDefault),
// together with older shapes whose writer elides the required field so the
// same bytes can be read at the root and two levels down.
const requiredSchema = `package probe
table Current
{
    active bool
    if active
    {
        req int32 | required
    }
}
table Legacy
{
    active bool
    if active
    {
        x int32
    }
}
table Exact
{
    ver uint8 | min = 7, max = 7, required
}
table OldExact
{
}
table Other
{
    ver uint8
}
table Inner
{
    id uint8 | required
}
table Outer
{
    id uint8
    inner Inner
}
table InnerOld
{
    spare int32
}
table OuterOld
{
    id uint8
    inner InnerOld
}
`

// requiredCase is one wire the reader tests, named by its root.
type requiredCase struct {
	name string
	root string
	wire []byte
}

// requiredWires writes each case's wire with schema-go's own engine
// (internal/tabletext and internal/tablewire), never by hand: a required
// field at its default is elided by that writer, which is exactly the older
// record the read must refuse.
func requiredWires(t *testing.T, u *ir.Unit) []requiredCase {
	t.Helper()
	m := tabletext.NewModel(u)
	build := func(table string, set func(*tabletext.Instance)) []byte {
		inst := m.New(m.Lookup(table))
		set(inst)
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: tablewire.Encode: %v", table, err)
		}
		return wire
	}
	field := func(inst *tabletext.Instance, name string) *tabletext.Field {
		f, ok := inst.FieldByKey(name)
		if !ok {
			t.Fatalf("no field %q", name)
		}
		return f
	}
	return []requiredCase{
		{"off", "Current", build("Current", func(i *tabletext.Instance) { field(i, "active").Cell.B = false })},
		{"on-missing", "Current", build("Current", func(i *tabletext.Instance) { field(i, "active").Cell.B = true })},
		{"on-present", "Current", build("Current", func(i *tabletext.Instance) {
			field(i, "active").Cell.B = true
			field(i, "req").Cell.I = 7
		})},
		{"exact-missing", "Exact", build("Exact", func(*tabletext.Instance) {})},
		{"exact-present", "Exact", build("Exact", func(i *tabletext.Instance) { field(i, "ver").Cell.U = 7 })},
		{"nested-missing", "Outer", build("OuterOld", func(i *tabletext.Instance) { field(i, "inner").Cell.Tab.Fields[0].Cell.I = 5 })},
		{"nested-present", "Outer", build("Outer", func(i *tabletext.Instance) { field(i, "inner").Cell.Tab.Fields[0].Cell.U = 5 })},
	}
}

// requiredFiles emits both surfaces for the unit into a temp dir.
func requiredFiles(t *testing.T, u *ir.Unit) *generatedUnit {
	t.Helper()
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	g := &generatedUnit{loaded: true, files: files, dir: t.TempDir()}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

// requiredGoOracle writes a generated Go probe over the same unit and runs it
// under the Go toolchain. It prints one "@<name>|<verdict>" line per read and
// per save, so the Luau probe's answers are compared with schema-go's
// generated reader and writer, never with a hand-typed expectation.
func requiredGoOracle(t *testing.T, u *ir.Unit, cases []requiredCase) map[string]string {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	var b strings.Builder
	b.WriteString("package probe\n\nimport (\n\t\"encoding/hex\"\n\t\"fmt\"\n\t\"testing\"\n)\n\nfunc TestRequiredOracle(t *testing.T) {\n")
	for _, c := range cases {
		fmt.Fprintf(&b, "\t{ b, _ := hex.DecodeString(%q); _, err := %sReadStrict(b); if err != nil { fmt.Printf(\"@%s|%%s\\n\", err) } else { fmt.Printf(\"@%s|ok\\n\") } }\n",
			hex.EncodeToString(c.wire), c.root, c.name, c.name)
	}
	b.WriteString(`
saved := func(name string, n int64, b []byte) {
	if n < 0 { fmt.Printf("@save-%s|REFUSED\n", name); return }
	fmt.Printf("@save-%s|%s\n", name, hex.EncodeToString(b[:n]))
}
var buf [256]byte
{ var v Current; n := CurrentSave(&v, buf[:]); saved("current-default", n, buf[:]) }
{ var v Current; v.Active = true; n := CurrentSave(&v, buf[:]); saved("current-required-default", n, buf[:]) }
{ var v Exact; n := ExactSave(&v, buf[:]); saved("exact-fresh", n, buf[:]) }
{ var v Exact; v.Ver = 7; n := ExactSave(&v, buf[:]); if ExactMeasure(&v) != n { fmt.Printf("@save-exact-set|MEASURE\n") } else { saved("exact-set", n, buf[:]) } }
}
`)
	dir := t.TempDir()
	runtime := tfSerializeGoRuntime(t)
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	files["oracle_test.go"] = []byte(b.String())
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-run", "^TestRequiredOracle$", "-v", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go oracle failed: %v\n%s", err, out)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		if after, ok := strings.CutPrefix(line, "@"); ok {
			f := strings.SplitN(after, "|", 2)
			if len(f) == 2 {
				got[f[0]] = f[1]
			}
		}
	}
	return got
}

// requiredLuauRead runs ReadStrict under the pinned interpreter and prints
// the same "@<name>|<verdict>" lines.
func requiredLuauRead(t *testing.T, g *generatedUnit, cases []requiredCase) map[string]string {
	t.Helper()
	inputs := map[string][]byte{}
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	var b strings.Builder
	b.WriteString("local inputs = require(\"./inputs\")\nlocal R = require(\"./schema_refusal\")\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "require(\"./%s\")\n", m)
	}
	b.WriteString("local roots = {\n")
	for _, c := range cases {
		inputs[c.name] = c.wire
		fmt.Fprintf(&b, "\t[%q] = %q,\n", c.name, c.root)
	}
	b.WriteString("}\nlocal T = require(\"./ReqTable\")\n")
	b.WriteString(`for name, root in pairs(roots) do
	local rep: any = {}
	local v = R.ReadStrict(T, root, inputs[name], rep)
	print("@" .. name .. "|" .. (rep.reason or "ok"))
end
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, g.dir, "main.luau", inputs)
	got := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		if after, ok := strings.CutPrefix(line, "@"); ok {
			f := strings.SplitN(after, "|", 2)
			if len(f) == 2 {
				got[f[0]] = f[1]
			}
		}
	}
	return got
}

// TestLuauRequiredRead pins §2.3's read rule against schema-go's generated
// ReadStrict over the same bytes: a missing required field refuses the whole
// record by name at the root and below it, a guard-off field is not demanded,
// and a guard-on field is.
func TestLuauRequiredRead(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")
	t.Parallel()
	u := requiredLoad(t)
	cases := requiredWires(t, u)
	g := requiredFiles(t, u)
	want := requiredGoOracle(t, u, cases)
	got := requiredLuauRead(t, g, cases)
	for _, c := range cases {
		if want[c.name] != got[c.name] {
			t.Errorf("%s (%s): Luau ReadStrict %q, generated Go %q", c.name, c.root, got[c.name], want[c.name])
		}
	}
	if want["on-missing"] != "missing_required: req" {
		t.Fatalf("the oracle did not refuse the guard-on missing field by name: %q", want["on-missing"])
	}
	if want["off"] != "ok" || want["on-present"] != "ok" || want["exact-present"] != "ok" || want["nested-present"] != "ok" {
		t.Fatalf("the oracle refused a clean record: %+v", want)
	}
	if !strings.Contains(want["nested-missing"], "missing_required: id") {
		t.Fatalf("the oracle did not name the nested missing field: %q", want["nested-missing"])
	}
}

// TestLuauRequiredSave pins §2.3's write rule: a required field at its default
// is written byte for byte as the generated Go writer writes it, a fresh
// required range-excluding-zero value refuses, and once set it saves and
// round-trips while measure equals the saved length.
func TestLuauRequiredSave(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")
	t.Parallel()
	u := requiredLoad(t)
	g := requiredFiles(t, u)
	want := requiredGoOracle(t, u, requiredWires(t, u))
	got := requiredLuauSave(t, g)
	for _, name := range []string{"save-current-default", "save-current-required-default", "save-exact-fresh", "save-exact-set"} {
		if want[name] != got[name] {
			t.Errorf("%s: Luau %q, generated Go %q", name, got[name], want[name])
		}
	}
	if want["save-current-required-default"] == "REFUSED" {
		t.Fatal("the Go writer refused a required field at its default")
	}
	if want["save-exact-fresh"] != "REFUSED" || got["save-exact-fresh"] != "REFUSED" {
		t.Fatalf("a fresh range-excluding-zero required value did not refuse: Go %q, Luau %q", want["save-exact-fresh"], got["save-exact-fresh"])
	}
}

// requiredLuauSave runs the same save cases through schema_save.luau and prints the
// "@save-<name>|<hex or REFUSED>" lines; measure is compared with the saved
// length inside the probe.
func requiredLuauSave(t *testing.T, g *generatedUnit) map[string]string {
	t.Helper()
	main := `local save = require("./schema_save")
local load = require("./schema_load")
local St = require("./schema_tables")
require("./ReqTable")
local function emit(name: string, d: any, v: any)
	local out = save.save(d, v)
	local m = save.measure(d, v)
	if out == nil or m == nil then print("@save-" .. name .. "|REFUSED"); return end
	if m ~= buffer.len(out) then print("@save-" .. name .. "|MEASURE"); return end
	local parts = {}
	for i = 0, buffer.len(out) - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(out, i)) end
	print("@save-" .. name .. "|" .. table.concat(parts))
end
local c = St.lookup("Current").new()
emit("current-default", St.lookup("Current"), c)
c.Active = true
emit("current-required-default", St.lookup("Current"), c)
local e = St.lookup("Exact").new()
emit("exact-fresh", St.lookup("Exact"), e)
e.Ver = 7
emit("exact-set", St.lookup("Exact"), e)
local ok = save.save(St.lookup("Exact"), e)
if ok ~= nil then
	local rep: any = {}
	local back = load.load("Exact", ok, rep)
	if back == nil or back.Ver ~= 7 then print("@save-exact-roundtrip|BAD") else print("@save-exact-roundtrip|ok") end
end
`
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, g.dir, "main.luau", nil)
	got := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		if after, ok := strings.CutPrefix(line, "@"); ok {
			f := strings.SplitN(after, "|", 2)
			if len(f) == 2 {
				got[f[0]] = f[1]
			}
		}
	}
	if got["save-exact-roundtrip"] != "ok" {
		t.Errorf("the set required value did not round-trip through schema_load.luau: %q", got["save-exact-roundtrip"])
	}
	return got
}

// requiredLoad loads the card's two schemas as one unit.
func requiredLoad(t *testing.T) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Req.schema")
	if err := os.WriteFile(path, []byte(requiredSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the required schema does not load: %v", err)
	}
	return u
}
