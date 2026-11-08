package luatable

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// BenchmarkLuaL4VariableLoadCook exercises populated variable records, nested
// text-list extents and wide values. Every subprocess first checks wire and
// cook bytes against the Go engine; setup and generation are outside timing.
// lua-ns/op is Lua process CPU time; ns/op also includes interpreter startup.
func BenchmarkLuaL4VariableLoadCook(b *testing.B) {
	for _, mode := range []string{"load", "cook_write", "cook_open_read"} {
		b.Run(mode, func(b *testing.B) {
			b.StopTimer()
			dir := b.TempDir()
			schema := `package l4bench
 table Row { names [..8]string
 tag uint128
 value int32 }
 table Root { rows [..16]Row }
 `
			path := filepath.Join(dir, "L4bench.schema")
			if err := os.WriteFile(path, []byte(schema), 0o600); err != nil {
				b.Fatal(err)
			}
			u := luarun.LoadUnit(b, path)
			files, err := Generate(u)
			if err != nil {
				b.Fatal(err)
			}
			luarun.WriteFiles(b, dir, files)
			rows := make([]map[string]any, 16)
			for i := range rows {
				rows[i] = map[string]any{"names": []string{fmt.Sprintf("row-%d-north-sector", i), "south-sector-supply", "east-sector-patrol", "west-sector-return"}, "tag": json.Number(fmt.Sprintf("%d", i+1)), "value": i * 7}
			}
			text, err := json.Marshal(map[string]any{"rows": rows})
			if err != nil {
				b.Fatal(err)
			}
			model := tabletext.NewModel(u)
			value := model.New(model.Lookup("Root"))
			var report tabletext.Report
			if !model.Read(value, text, &report) || !report.Silent() {
				b.Fatalf("fixture text refused: %+v", report)
			}
			wire, err := tablewire.Encode(model, value)
			if err != nil {
				b.Fatal(err)
			}
			cooked, err := tablecook.Cook(model, value, tablecook.Options{})
			if err != nil {
				b.Fatal(err)
			}
			script := fmt.Sprintf(`local T = require("L4benchTables")
local L, S = require("load"), require("save")
local W, C, U = require("cookwrite"), require("cook"), require("uncook")
local input = require("inputs")
local function load() return assert(L.Load(T, "Root", input.wire, {})) end
local v = load()
assert(S.Save(T, "Root", v) == input.wire, "wire differs from Go")
local image = assert(W.Cook(T, "Root", v, nil, 1))
assert(image == input.cook, "cook differs from Go")
assert(W.CookMeasure(T, "Root", v) == #image, "cook measure differs")
local opened = assert(C.Open(T, "Root", image))
assert(C.Check(T, "Root", image) ~= nil, "cook check refused")
assert(S.Save(T, "Root", assert(U.Read(T, opened))) == input.wire, "uncook differs")
assert(#v.rows == 16 and #v.rows[16].names == 4, "empty benchmark fixture")
local n, mode = %d, %q
local last
collectgarbage("collect")
local start = os.clock()
for i = 1, n do
 if mode == "load" then last = load()
 elseif mode == "cook_write" then last = assert(W.Cook(T, "Root", v, nil, 1))
 else last = assert(U.Read(T, assert(C.Open(T, "Root", image)))) end
end
local elapsed = os.clock() - start
assert(last ~= nil)
print(string.format("%%.9f", elapsed))
`, b.N, mode)
			if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(script), 0o600); err != nil {
				b.Fatal(err)
			}
			b.SetBytes(int64(len(wire)))
			b.StartTimer()
			output := luarun.Run(b, dir, "main.lua", map[string][]byte{"wire": wire, "cook": cooked})
			b.StopTimer()
			seconds, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
			if err != nil || seconds <= 0 {
				b.Fatalf("invalid Lua timing %q: %v", output, err)
			}
			b.ReportMetric(seconds*1e9/float64(b.N), "lua-ns/op")
		})
	}
}

// BenchmarkLuaL4Block checks a fully populated fixed-width block's bytes
// against the canonical IR layout before timing either entry point.
func BenchmarkLuaL4Block(b *testing.B) {
	for _, mode := range []string{"open", "build"} {
		b.Run(mode, func(b *testing.B) {
			b.StopTimer()
			dir := b.TempDir()
			path := filepath.Join(dir, "Blockbench.schema")
			if err := os.WriteFile(path, []byte("package blockbench\nfixed table Row { value uint128 }\nfixed table Frame { rows [..16]Row }\n"), 0o600); err != nil {
				b.Fatal(err)
			}
			u := luarun.LoadUnit(b, path)
			files, err := Generate(u)
			if err != nil {
				b.Fatal(err)
			}
			luarun.WriteFiles(b, dir, files)
			layout := ir.Blocks(u).Block("Frame")
			oracle := make([]byte, layout.MaxBytes)
			binary.LittleEndian.PutUint64(oracle, ir.BlockMagic)
			binary.LittleEndian.PutUint64(oracle[8:], ir.BuildVersion(u))
			binary.LittleEndian.PutUint64(oracle[16:], 1)
			offset := layout.Projection.Size
			for _, a := range layout.Arrays {
				offset = (offset + 63) &^ 63
				binary.LittleEndian.PutUint64(oracle[a.OffsetOfOffset:], uint64(offset))
				binary.LittleEndian.PutUint32(oracle[a.CountOffset:], uint32(a.Max))
				binary.LittleEndian.PutUint32(oracle[a.StrideOffset:], uint32(a.Stride))
				offset += a.Max * a.Stride
			}
			script := fmt.Sprintf(`local T = require("BlockbenchTables")
local B, W = require("block"), require("blockbuild")
local input = require("inputs")
local block = assert(W.Begin(T, "Frame", {rows=16}))
assert(block.bytes == input.block, "block differs from Go IR layout")
local h = assert(B.BlockOpen(T, "Frame", block.bytes))
assert(h.arrays[1].count == 16 and h.arrays[1].stride == 16, "block is not populated")
local counts = {rows=16}
local last
collectgarbage("collect")
local start = os.clock()
for i=1,%d do
 if %q == "open" then last = assert(B.BlockOpen(T, "Frame", block.bytes))
 else last = assert(W.Begin(T, "Frame", counts)) end
end
local elapsed = os.clock()-start
assert(last ~= nil)
print(string.format("%%.9f", elapsed))
`, b.N, mode)
			if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(script), 0o600); err != nil {
				b.Fatal(err)
			}
			b.SetBytes(int64(len(oracle)))
			b.StartTimer()
			output := luarun.Run(b, dir, "main.lua", map[string][]byte{"block": oracle})
			b.StopTimer()
			seconds, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
			if err != nil || seconds <= 0 {
				b.Fatalf("invalid Lua timing %q: %v", output, err)
			}
			b.ReportMetric(seconds*1e9/float64(b.N), "lua-ns/op")
		})
	}
}
