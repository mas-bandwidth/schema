package luatable

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The storage cap vectors (docs/SPEC-TABLES.md §11, §6.5) live in
// testdata/wire/tables/storage-cap beside the unit they are read under. Big's
// storage is 2^27 bytes and one element or record of it costs two wire bytes,
// so 8191 of them measure under the 2^40-byte cap and 8192 measure over it.
const storageCapDir = "../../../testdata/wire/tables/storage-cap"

// storageCapProbe measures every vector, and reads each over vector through
// the strict read, the plain Load and the tolerant load. Big's constructor
// raises, so a read that builds even one Big before it refuses fails by name
// instead of building 2^24-entry tables. A Huge list probe (hugeCount empty bodies
// of a record just under 2^40 bytes) holds the term check, and a Huge node
// probe (as many node records of it) the running node total: either sum is 2^63, which wraps
// a Lua integer, so only a check made before the sum answers
// storage_over_cap.
const storageCapProbe = `local T = require(%q)
local load = require("load")
local refusal = require("refusal")
local inputs = require("inputs")
-- a read that builds what it should refuse raises here, well before the
-- machine runs out of memory
debug.sethook(function()
  if collectgarbage("count") > 8 * 1024 * 1024 then error("more than 8 GiB built", 0) end
end, "", 100000)
local function report()
  return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, blobShared = 0, missingRequired = 0, wireDamaged = false, reason = "" }
end
for _, c in ipairs({ "list_at", "list_over", "nodes_at", "nodes_over" }) do
  local n, why = load.LoadMeasure(T, "Root", inputs[c])
  print(c .. ".measure=" .. tostring(n) .. "," .. tostring(why))
  if c:sub(-5) == "_over" then
    local ok, err = pcall(refusal.ReadStrict, T, "Root", inputs[c])
    print(c .. ".strict=" .. (ok and "read" or tostring(err)))
    local r = report()
    local lok, root = pcall(load.Load, T, "Root", inputs[c], r)
    print(c .. ".load=" .. (lok and (tostring(root == nil) .. "," .. tostring(r.reason)) or tostring(root)))
    r = report()
    lok, root = pcall(load.LoadTolerant, T, "Root", inputs[c], r)
    print(c .. ".tolerant=" .. (lok and (tostring(root == nil) .. "," .. tostring(r.reason) .. "," .. tostring(r.refused) .. "," .. tostring(r.malformed)) or tostring(root)))
  end
end
for _, c in ipairs({ "huge_list", "huge_nodes" }) do
  local n, why = load.LoadMeasure(T, "Holder", inputs[c])
  print(c .. ".measure=" .. tostring(n) .. "," .. tostring(why))
end
`

// hugeCount Huge bodies (2^40 - 2^27 bytes each) command just past 2^63
// bytes, so their sum wraps a Lua integer.
const hugeCount = 1<<23 + 1<<11

// storageCapHugeWire is a form-1 Holder holding n empty Huge bodies, as its
// items list or as node records.
func storageCapHugeWire(u *ir.Unit, nodes bool, n uint64) []byte {
	var body []byte
	var id uint64
	kind := byte(ir.TableKindArray)
	if nodes {
		body = binary.AppendUvarint(nil, n)
		for range n {
			body = append(body, 2, 1, 0)
		}
		id, kind = math.MaxUint64, byte(ir.TableKindString)
	} else {
		body = binary.AppendUvarint([]byte{byte(ir.TableKindTable)}, n)
		for range n {
			body = append(body, 1, 0)
		}
		id = ir.TableFieldWireId(u.Tables["Holder"].Fields[0])
	}
	wire := []byte{1, 1, kind}
	wire = binary.AppendUvarint(wire, uint64(len(body)))
	wire = append(wire, body...)
	wire = append(wire, 0)
	wire = binary.LittleEndian.AppendUint64(wire, id)
	count := uint64(1)
	if nodes {
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId(u.Tables["Huge"].WireName()))
		count = 2
	}
	return binary.LittleEndian.AppendUint64(wire, count)
}

// storageCapRun generates the unit, plants the raising Big constructor, and
// runs the probe with patch applied to the emitted load.lua (nil for none).
func storageCapRun(t *testing.T, patch func(string) string) map[string]string {
	t.Helper()
	repo, err := filepath.Abs(storageCapDir)
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(repo, "Cap.schema"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	schema := filepath.Join(dir, "Cap.schema")
	extra := "\nfixed table Huge\n{\n    a [8191]Big\n}\n\ntable Holder\n{\n    items []Huge\n    next  *Huge\n}\n"
	if err := os.WriteFile(schema, append(src, extra...), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	module := ModuleName(u)
	ctor := "function _M.NewBig()\n"
	mod := string(files[module+".lua"])
	if !strings.Contains(mod, ctor) {
		t.Fatalf("the emitted module has no %q", ctor)
	}
	files[module+".lua"] = []byte(strings.Replace(mod, ctor, ctor+"  error(\"built a Big\", 0)\n", 1))
	if patch != nil {
		files["load.lua"] = []byte(patch(string(files["load.lua"])))
	}
	inputs := map[string][]byte{
		"huge_list":  storageCapHugeWire(u, false, hugeCount),
		"huge_nodes": storageCapHugeWire(u, true, hugeCount),
	}
	for _, c := range []string{"list_at", "list_over", "nodes_at", "nodes_over"} {
		data, err := os.ReadFile(filepath.Join(repo, c+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[c] = data
	}
	stdout, stderr, err := try(t, files, fmt.Sprintf(storageCapProbe, module), inputs)
	if err != nil {
		t.Fatalf("probe: %v\n%s\n%s", err, stdout, stderr)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(stdout, "\n"), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("probe line %q is not key=value", line)
		}
		got[k] = v
	}
	return got
}

// storageCapFailures answers what the probe's answers break of §11: an at
// vector measures in (0, 2^40], and every over vector and the Huge list
// refuse by name, the loads without building a Big.
func storageCapFailures(got map[string]string) []string {
	var bad []string
	for _, c := range []string{"list_at", "nodes_at"} {
		size, why, _ := strings.Cut(got[c+".measure"], ",")
		n, err := strconv.ParseInt(size, 10, 64)
		if err != nil || n <= 0 || n > ir.MaxSizeBytes || why != "nil" {
			bad = append(bad, c+" measure "+got[c+".measure"])
		}
	}
	for _, c := range []string{"list_over", "nodes_over"} {
		want := map[string]string{
			".measure":  "nil,storage_over_cap",
			".strict":   "storage_over_cap",
			".load":     "true,storage_over_cap",
			".tolerant": "true,storage_over_cap,1,false",
		}
		for k, w := range want {
			if got[c+k] != w {
				bad = append(bad, fmt.Sprintf("%s%s: got %q, want %q", c, k, got[c+k], w))
			}
		}
	}
	for _, c := range []string{"huge_list", "huge_nodes"} {
		if got[c+".measure"] != "nil,storage_over_cap" {
			bad = append(bad, c+" measure "+got[c+".measure"])
		}
	}
	return bad
}

// TestLuaStorageCapRefusedAtMeasure holds the storage cap at load
// (docs/SPEC-TABLES.md §11, §6.5) on the shared vectors: LoadMeasure answers
// a size for each at vector and nil, "storage_over_cap" for each over vector,
// and ReadStrict, Load and LoadTolerant refuse each over vector by name
// before building anything.
func TestLuaStorageCapRefusedAtMeasure(t *testing.T) {
	t.Parallel()
	if bad := storageCapFailures(storageCapRun(t, nil)); len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// TestLuaStorageCapNegativeControl removes each check from a copy of the
// emitted load.lua and requires the probe to go red: the extent term, the
// running node total, the final total, and the load's guard.
func TestLuaStorageCapNegativeControl(t *testing.T) {
	t.Parallel()
	plants := []struct{ name, from, to string }{
		{"term", "if count > 0 and size > 0 and count > (STORAGE_CAP - at) // size then", "if false then"},
		{"node total", "    if total > STORAGE_CAP then return nil, \"storage_over_cap\" end\n  end\n", "  end\n"},
		{"final total", "  total = total + (#raw + 1) * 16\n  if total > STORAGE_CAP then return nil, \"storage_over_cap\" end\n", "  total = total + (#raw + 1) * 16\n"},
		{"load guard", "    if why == \"storage_over_cap\" then\n      report.refused", "    if false then\n      report.refused"},
	}
	for _, p := range plants {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			got := storageCapRun(t, func(src string) string {
				if !strings.Contains(src, p.from) {
					t.Fatalf("the plant's anchor is absent from load.lua: %q", p.from)
				}
				return strings.Replace(src, p.from, p.to, 1)
			})
			if len(storageCapFailures(got)) == 0 {
				t.Fatalf("load.lua with the %s check removed still holds the cap", p.name)
			}
		})
	}
}
