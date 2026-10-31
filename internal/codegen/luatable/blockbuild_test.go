package luatable

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// 2026-10-18: the Lua leg had no block builder. docs/SPEC-TABLES.md §19.1 is
// the contract and internal/codegen/gotable/blockbuild.go is the oracle: one
// 64-byte aligned extent whose projection opens with the three-word prologue
// and carries, per out-of-line array, the (offset_of, count, stride) triple.
// This test builds the blockdemo block roots in Lua through runtime/
// blockbuild.lua, compares every byte against the GENERATED Go builder, and
// opens the Lua bytes with the generated Go reader.
func TestLuaBlockBuilder(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "go and the pinned lua")
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	paths := units["blockdemo"]
	if len(paths) == 0 {
		t.Fatal("the manifest has no blockdemo unit")
	}
	var schemaPaths []string
	for _, p := range paths {
		schemaPaths = append(schemaPaths, filepath.Join(repo, p))
	}
	u := luarun.LoadUnit(t, schemaPaths...)

	dir := t.TempDir()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate the Lua unit: %v", err)
	}
	luarun.WriteFiles(t, dir, files)
	luarun.WriteHexProbe(t, dir)

	roots := []string{"RenderFrame", "PaddedFrame", "PaddedRow"}
	lua := fmt.Sprintf(`local T = require(%q)
local B = require("blockbuild")
local hexline = require("hexline")
T.BuildVersion = 0x%016x
local plan = {
  { "RenderFrame", { cameras = 1, ships = 3, turrets = 2, missiles = 1,
                     dynamic_props = 2, static_props = 4, cosmetic_props = 1,
                     lasers = 3, explosions = 2 } },
  { "PaddedFrame", { rows = 3 } },
  { "PaddedRow", {} },
}
for _, p in ipairs(plan) do
  local block, refusal = B.Begin(T, p[1], p[2])
  if block == nil then error("Begin(" .. p[1] .. "): " .. tostring(type(refusal) == "table" and refusal.array or refusal)) end
  if B.BlockBytes(block) ~= #block.bytes then error("BlockBytes disagrees with the extent") end
  hexline(block.bytes)
end
local block, refusal = B.Begin(T, "RenderFrame", { ships = 4097 })
if block ~= nil then error("Begin accepted a count past the declared maximum") end
hexline(tostring(refusal.array) .. "|" .. tostring(refusal.count) .. "|" .. tostring(refusal.maximum))
`, ModuleName(u), ir.BuildVersion(u))
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(lua), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luarun.Run(t, dir, "main.lua", nil)
	lines := luarun.HexLines(t, out)
	if len(lines) != len(roots)+1 {
		t.Fatalf("the Lua probe returned %d lines, want %d", len(lines), len(roots)+1)
	}
	for i, root := range roots {
		if err := os.WriteFile(filepath.Join(dir, "lua_"+root+".bin"), lines[i], 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := string(lines[len(roots)]); got != "ships|4097|4096" {
		t.Fatalf("the refusal names %q, want ships|4097|4096", got)
	}

	// The generated Go builder writes the oracle bytes and opens the Lua ones.
	goFiles, err := golang.Generate(u)
	if err != nil {
		t.Fatalf("generate the Go unit: %v", err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatalf("generate the Go block surface: %v", err)
	}
	maps.Copy(goFiles, tables)
	serialize := slowtest.SerializeGo(t, "../../../")
	goFiles["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", serialize))
	goFiles["block_test.go"] = []byte(blockOpenProbe)
	for name, data := range goFiles {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("nice", "-n", "19", "go", "test", "-count=1", "-timeout", "600s", "-run", "^TestLuaBlockOpen$", ".")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated Go reader: %v\n%s", err, output)
	}

	for _, root := range roots {
		oracle, err := os.ReadFile(filepath.Join(dir, "go_"+root+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(oracle, lines[rootsIndex(roots, root)]) {
			t.Errorf("%s: the Lua builder's %d bytes differ from the generated Go builder's %d", root, len(lines[rootsIndex(roots, root)]), len(oracle))
		}
	}
}

func rootsIndex(roots []string, want string) int {
	for i, r := range roots {
		if r == want {
			return i
		}
	}
	return -1
}

const blockOpenProbe = `package blockdemo

import (
	"os"
	"testing"
	"unsafe"
)

func aligned(data []byte) unsafe.Pointer {
	raw := make([]byte, len(data)+64)
	base := uintptr(unsafe.Pointer(&raw[0]))
	off := (64 - base%64) % 64
	copy(raw[off:], data)
	return unsafe.Pointer(&raw[off])
}

func TestLuaBlockOpen(t *testing.T) {
	t.Run("RenderFrame", func(t *testing.T) {
		var storage RenderFrameBlockStorage
		if !storage.Create(TableBlockDefaultAllocator()) {
			t.Fatal("create")
		}
		defer storage.Destroy()
		var block RenderFrameBlock
		var refusal TableBlockRefusal
		counts := RenderFrameCounts{Cameras: 1, Ships: 3, Turrets: 2, Missiles: 1,
			DynamicProps: 2, StaticProps: 4, CosmeticProps: 1, Lasers: 3, Explosions: 2}
		if !RenderFrameBlockBegin(&block, &storage, counts, &refusal) {
			t.Fatalf("begin: %+v", refusal)
		}
		if err := os.WriteFile("go_RenderFrame.bin", unsafe.Slice((*byte)(block.Base), block.Bytes), 0o644); err != nil {
			t.Fatal(err)
		}
		lua, err := os.ReadFile("lua_RenderFrame.bin")
		if err != nil {
			t.Fatal(err)
		}
		var opened RenderFrameBlock
		if !RenderFrameBlockOpen(&opened, aligned(lua), int64(len(lua))) {
			t.Fatal("the Lua-built block does not open")
		}
		if opened.Bytes != int64(len(lua)) ||
			opened.Projection.Ships.OffsetOf != block.Projection.Ships.OffsetOf ||
			opened.Projection.Ships.Count != 3 || opened.Projection.Ships.Stride != block.Projection.Ships.Stride ||
			opened.Projection.Explosions.OffsetOf != block.Projection.Explosions.OffsetOf {
			t.Fatalf("opened layout differs: %+v", opened.Projection)
		}
	})
	t.Run("PaddedFrame", func(t *testing.T) {
		var storage PaddedFrameBlockStorage
		if !storage.Create(TableBlockDefaultAllocator()) {
			t.Fatal("create")
		}
		defer storage.Destroy()
		var block PaddedFrameBlock
		var refusal TableBlockRefusal
		if !PaddedFrameBlockBegin(&block, &storage, PaddedFrameCounts{Rows: 3}, &refusal) {
			t.Fatalf("begin: %+v", refusal)
		}
		if err := os.WriteFile("go_PaddedFrame.bin", unsafe.Slice((*byte)(block.Base), block.Bytes), 0o644); err != nil {
			t.Fatal(err)
		}
		lua, err := os.ReadFile("lua_PaddedFrame.bin")
		if err != nil {
			t.Fatal(err)
		}
		var opened PaddedFrameBlock
		if !PaddedFrameBlockOpen(&opened, aligned(lua), int64(len(lua))) {
			t.Fatal("the Lua-built block does not open")
		}
		if opened.Bytes != int64(len(lua)) || opened.Projection.Rows.OffsetOf != block.Projection.Rows.OffsetOf ||
			opened.Projection.Rows.Stride != block.Projection.Rows.Stride {
			t.Fatalf("opened layout differs: %+v", opened.Projection)
		}
	})
	t.Run("PaddedRow", func(t *testing.T) {
		var storage PaddedRowBlockStorage
		if !storage.Create(TableBlockDefaultAllocator()) {
			t.Fatal("create")
		}
		defer storage.Destroy()
		var block PaddedRowBlock
		if !PaddedRowBlockBegin(&block, &storage, PaddedRowCounts{}, nil) {
			t.Fatal("begin")
		}
		if err := os.WriteFile("go_PaddedRow.bin", unsafe.Slice((*byte)(block.Base), block.Bytes), 0o644); err != nil {
			t.Fatal(err)
		}
		lua, err := os.ReadFile("lua_PaddedRow.bin")
		if err != nil {
			t.Fatal(err)
		}
		var opened PaddedRowBlock
		if !PaddedRowBlockOpen(&opened, aligned(lua), int64(len(lua))) {
			t.Fatal("the Lua-built block does not open")
		}
		if opened.Bytes != int64(len(lua)) || string(lua) != string(lua) {
			t.Fatalf("opened extent %d, want %d", opened.Bytes, len(lua))
		}
	})
}
`
