package mootable_test

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/memceiling"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The storage cap vectors (docs/SPEC-TABLES.md §6.5, §11) live in
// testdata/wire/tables/storage-cap beside Cap.schema. Big's storage is 2^27
// bytes and one element or node record of it costs two wire bytes, so 8191
// of them measure under the 2^40-byte cap and 8192 measure over it, through
// the list extent (list_*) or the node scan (nodes_*). Two more lists are
// built here: list_term, 8193 elements, one extent term past the cap, and
// list_wide, 2^20 elements, whose term (2^47 bytes) is past the largest
// integer a moo number holds exactly, so only the check before the multiply
// refuses it by name.
var storageCapDir = filepath.Join("..", "..", "..", "testdata", "wire", "tables", "storage-cap")

// storageCapList is a form-1 file whose root holds count empty Big bodies as
// its items list, split around the elements: the bytes before the first, and
// the bytes after the last. list_at.bin and list_over.bin are this shape.
func storageCapList(u *ir.Unit, count uint64) (pre, post []byte) {
	items := ir.TableFieldWireId(u.Tables["Root"].Fields[0])
	list := binary.AppendUvarint([]byte{byte(ir.TableKindTable)}, count)
	pre = []byte{1, 1, byte(ir.TableKindArray)}
	pre = binary.AppendUvarint(pre, uint64(len(list))+2*count)
	pre = append(pre, list...)
	post = binary.LittleEndian.AppendUint64([]byte{0}, items)
	return pre, binary.LittleEndian.AppendUint64(post, 1)
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

const storageCapProbe = `--!strict
local inputs = require("./inputs")
local load = require("./table_load")
local refusal = require("./table_refusal")
REQUIRES
local T = require("./MODULE")
local function nn(x: any): string
	if x == nil then
		return "-"
	end
	return tostring(x)
end
-- a list of n empty Big bodies, assembled from the bytes around them
local function list(key: string, n: number): buffer
	local pre, post = inputs[key .. "_pre"], inputs[key .. "_post"]
	local b = buffer.create(buffer.len(pre) + 2 * n + buffer.len(post))
	buffer.copy(b, 0, pre)
	for i = 0, n - 1 do
		buffer.writeu8(b, buffer.len(pre) + 2 * i, 1)
	end
	buffer.copy(b, buffer.len(pre) + 2 * n, post)
	return b
end
local wires: { [string]: buffer } = {
	list_at = inputs.list_at, nodes_at = inputs.nodes_at,
	list_over = inputs.list_over, nodes_over = inputs.nodes_over,
	list_term = list("list_term", 8193), list_wide = list("list_wide", 1048576),
}
for _, k in { "list_at", "nodes_at", "list_over", "nodes_over", "list_term", "list_wide" } do
	local n, why = load.LoadMeasure("Root", wires[k])
	local line = k .. " " .. nn(n) .. " " .. nn(why)
	-- the strict read runs on the over vectors alone: an at vector would
	-- build a terabyte
	if string.sub(k, -3) ~= "_at" then
		local report: any = {}
		local v = refusal.ReadStrict(T, "Root", wires[k], report)
		line ..= " " .. tostring(v == nil) .. " " .. nn(report.reason)
	end
	print(line)
end
`

// TestMooStorageCapRefusedAtMeasure holds the storage cap at load on the
// shared vectors: LoadMeasure answers each at vector's exact size, under
// 2^40, and refuses each over vector storage_over_cap, as ReadStrict does
// before it builds anything.
func TestMooStorageCapRefusedAtMeasure(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_load.moo)")
	t.Parallel()
	// a leg that builds what it should refuse is killed at the ceiling
	memceiling.Guard(t, memceiling.Default)
	src, err := os.ReadFile(filepath.Join(storageCapDir, "Cap.schema"))
	if err != nil {
		t.Fatal(err)
	}
	g := generateFiles(t, map[string]string{"Cap.schema": string(src)})
	u := g.unit
	module := ""
	for name, data := range g.files {
		if strings.HasSuffix(name, "Table.moo") && strings.Contains(string(data), ".RootDescriptor = ") {
			module = strings.TrimSuffix(name, ".moo")
		}
	}
	if module == "" {
		t.Fatal("no emitted module declares RootDescriptor")
	}
	inputs := map[string][]byte{}
	for _, k := range []string{"list_at", "list_over", "nodes_at", "nodes_over"} {
		data, err := os.ReadFile(filepath.Join(storageCapDir, k+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[k] = data
	}
	pre, post := storageCapList(u, 8191)
	if string(inputs["list_at"]) != string(pre)+strings.Repeat("\x01\x00", 8191)+string(post) {
		t.Fatal("storageCapList does not write list_at.bin")
	}
	inputs["list_term_pre"], inputs["list_term_post"] = storageCapList(u, 8193)
	inputs["list_wide_pre"], inputs["list_wide_post"] = storageCapList(u, 1<<20)
	sizes := storageCapSizes(u)
	for k, n := range sizes {
		if n <= 0 || n > 1<<40 {
			t.Fatalf("%s: %d is not under the cap", k, n)
		}
	}
	want := strings.Join([]string{
		fmt.Sprintf("list_at %d -", sizes["list_at"]),
		fmt.Sprintf("nodes_at %d -", sizes["nodes_at"]),
		"list_over - storage_over_cap true storage_over_cap",
		"nodes_over - storage_over_cap true storage_over_cap",
		"list_term - storage_over_cap true storage_over_cap",
		"list_wide - storage_over_cap true storage_over_cap",
	}, "\n")
	main := strings.NewReplacer("REQUIRES", g.requireAll(), "MODULE", module).Replace(storageCapProbe)
	if got := g.run(t, main, inputs); got != want {
		t.Fatalf("the storage cap answers\n%s\nwant\n%s", got, want)
	}
	// the measure is typed: the emitted runtime, table_load.moo included,
	// passes moo-analyze with no output
	analyzeSilent(t, g)
}
