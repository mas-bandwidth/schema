package luautable_test

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The storage cap vectors (docs/SPEC-TABLES.md §6.5, §11) live in
// testdata/wire/tables/storage-cap beside Cap.schema. Big's storage is 2^27
// bytes and one element or node record of it costs two wire bytes, so 8191
// of them measure under the 2^40-byte cap and 8192 measure over it, through
// the list extent (list_*) or the node scan (nodes_*). list_term is 8193
// elements in one list, a single extent term past the cap.
const storageCapDir = "../../../testdata/wire/tables/storage-cap"

// storageCapListWire is a form-1 file whose root holds count empty Big bodies
// as its items list, the shape of list_at.bin and list_over.bin.
func storageCapListWire(u *ir.Unit, count uint64) []byte {
	items := ir.TableFieldWireId(u.Tables["Root"].Fields[0])
	list := binary.AppendUvarint([]byte{byte(ir.TableKindTable)}, count)
	for range count {
		list = append(list, 1, 0)
	}
	wire := []byte{1, 1, byte(ir.TableKindArray)}
	wire = binary.AppendUvarint(wire, uint64(len(list)))
	wire = append(wire, list...)
	wire = append(wire, 0)
	wire = binary.LittleEndian.AppendUint64(wire, items)
	return binary.LittleEndian.AppendUint64(wire, 1)
}

func storageCapUnit(t *testing.T) (*ir.Unit, string) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(storageCapDir, "Cap.schema"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "Cap.schema")
	if err := os.WriteFile(path, src, 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	return u, string(src)
}

// storageCapSizes is the measure each at vector owes: the root's record and
// each node record rounded to the region alignment, the list extent, and 16
// bytes of attribution per record and for the root.
func storageCapSizes(u *ir.Unit) map[string]int64 {
	round := func(n, a int64) int64 { return (n + a - 1) / a * a }
	ra := ir.TableRegionAlign(u)
	root, big := ir.RecordLayout(u, u.Tables["Root"]), ir.RecordLayout(u, u.Tables["Big"])
	return map[string]int64{
		"list_at":  round(round(root.Size, ra)+round(0, big.Align)+8191*big.Size, ra) + 16,
		"nodes_at": round(root.Size, ra) + 8191*round(big.Size, ra) + 8192*16,
	}
}

const storageCapProbe = `local inputs = require("./inputs")
local T = require("./MODULE")
local L = require("./schema_load")
local R = require("./schema_refusal")
local function nn(x: any): string
	if x == nil then return "-" end
	if type(x) == "number" then return string.format("%.0f", x) end
	return tostring(x)
end
for _, k in { "list_at", "nodes_at", "list_over", "nodes_over", "list_term" } do
	local n, why = L.LoadMeasure("Root", inputs[k])
	local line = k .. " " .. nn(n) .. " " .. nn(why)
	-- the strict read runs on the over vectors alone: an at vector would
	-- build a terabyte
	if string.sub(k, -3) ~= "_at" then
		local report: any = {}
		local v = R.ReadStrict(T, "Root", inputs[k], report)
		line ..= " " .. tostring(v == nil) .. " " .. nn(report.reason)
	end
	print(line)
end
`

// TestLuauStorageCapRefusedAtMeasure holds the storage cap at load on the
// shared vectors: LoadMeasure answers each at vector's exact size, under
// 2^40, and refuses each over vector storage_over_cap, as ReadStrict does
// before it builds anything.
func TestLuauStorageCapRefusedAtMeasure(t *testing.T) {
	t.Parallel()
	u, src := storageCapUnit(t)
	g := generateFiles(t, map[string]string{"Cap.schema": src})
	module := ""
	for name, data := range g.files {
		if strings.HasSuffix(name, "Table.luau") && strings.Contains(string(data), "_T.RootDescriptor = ") {
			module = strings.TrimSuffix(name, ".luau")
		}
	}
	if module == "" {
		t.Fatal("no emitted module declares RootDescriptor")
	}
	inputs := map[string][]byte{"list_term": storageCapListWire(u, 8193)}
	for _, k := range []string{"list_at", "list_over", "nodes_at", "nodes_over"} {
		data, err := os.ReadFile(filepath.Join(storageCapDir, k+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[k] = data
	}
	if want := storageCapListWire(u, 8191); string(want) != string(inputs["list_at"]) {
		t.Fatal("storageCapListWire does not write list_at.bin")
	}
	sizes := storageCapSizes(u)
	want := []string{
		fmt.Sprintf("list_at %d -", sizes["list_at"]),
		fmt.Sprintf("nodes_at %d -", sizes["nodes_at"]),
		"list_over - storage_over_cap true storage_over_cap",
		"nodes_over - storage_over_cap true storage_over_cap",
		"list_term - storage_over_cap true storage_over_cap",
	}
	for k, n := range sizes {
		if n <= 0 || n > 1<<40 {
			t.Fatalf("%s: %d is not under the cap", k, n)
		}
	}
	got := strings.Split(runProbe(t, g, strings.ReplaceAll(storageCapProbe, "MODULE", module), inputs), "\n")
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the storage cap answers\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
