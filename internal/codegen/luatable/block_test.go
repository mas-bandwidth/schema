package luatable

// THE BLOCK FORM'S READ HALF on the Lua leg (docs/SPEC-TABLES.md §19.2,
// §19.3). The block images are the Go builder's own fixtures from the tables
// manifest, and the expected text is the canonical ROW DUMP the Go descriptors
// produce (testdata/conformance/tables/FORMAT.md), so this leg reads every
// field of every row through runtime/block.lua's descriptors and is compared
// value for value with Go. A damaged image refuses under the same
// TableRefuseReason name a Go Open reports (§7, §19.2).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestLuaBlockRead(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	rows := blockManifestRows(t, manifest)
	if len(rows) == 0 {
		t.Fatal("the manifest names no block image")
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			paths := units[row.unit]
			if len(paths) == 0 {
				t.Fatalf("block row %s names unknown unit %s", row.name, row.unit)
			}
			var full []string
			for _, p := range paths {
				full = append(full, filepath.Join(repo, p))
			}
			u := luarun.LoadUnit(t, full...)
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("unit %s: %v", row.unit, err)
			}
			if _, ok := files["block.lua"]; !ok {
				t.Fatal("the Lua table runtime carries no block.lua")
			}
			dir := t.TempDir()
			luarun.WriteFiles(t, dir, files)
			image, err := os.ReadFile(filepath.Join(repo, row.wire))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join(repo, row.dump))
			if err != nil {
				t.Fatal(err)
			}
			root := blockRoot(t, want)
			probe := fmt.Sprintf(`local T = require(%q)
local B = require("block")
local inputs = require("inputs")
local h, why = B.BlockOpen(T, %q, inputs.block, 0x%016x)
assert(h ~= nil, "BlockOpen refused: " .. tostring(why))
io.write(B.Dump(h))
`, ModuleName(u), root, ir.BuildVersion(u))
			if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
				t.Fatal(err)
			}
			got := string(luarun.Run(t, dir, "main.lua", map[string][]byte{"block": image}))
			if got != string(want) {
				t.Errorf("%s: the reflective read differs from Go's dump:\n--- lua ---\n%s\n--- go ---\n%s", row.name, got, want)
			}
		})
	}

	t.Run("damaged", func(t *testing.T) {
		t.Parallel()
		out, stderr, err := luaBlockRefusals(t, nil)
		if err != nil || out != "block-refusals-ok\n" {
			t.Fatalf("damaged-block probe: %q %s %v", out, stderr, err)
		}
	})

	// NATIVE ORDER is this build's order (§19.1: the producer stamps its own),
	// so the read is native-endian on every host and not a little-endian
	// assumption. The probe also holds the runtime to the host's order word.
	t.Run("native_order", func(t *testing.T) {
		t.Parallel()
		row, ok := blockRow(rows, "block_render")
		if !ok {
			t.Fatal("the manifest has no block_render image")
		}
		paths := units[row.unit]
		var full []string
		for _, p := range paths {
			full = append(full, filepath.Join(repo, p))
		}
		u := luarun.LoadUnit(t, full...)
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		image, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(repo, row.dump))
		if err != nil {
			t.Fatal(err)
		}
		root := blockRoot(t, want)
		probe := fmt.Sprintf(`local T = require(%q)
local B = require("block")
local inputs = require("inputs")
local host = string.pack("I2", 1):byte(1) == 1 and 1 or 2
assert(B.ByteOrder == host, "the block runtime's byte order " .. tostring(B.ByteOrder) .. " is not this host's " .. host)
local real = string.unpack
string.unpack = function(fmt, ...)
  assert(type(fmt) == "string" and fmt:sub(1, 1) == "=", "block.lua decodes with a fixed byte order: " .. tostring(fmt))
  return real(fmt, ...)
end
local h, why = B.BlockOpen(T, %q, inputs.block, 0x%016x)
assert(h ~= nil, "BlockOpen refused a block in this build's order: " .. tostring(why))
local got = B.Dump(h)
string.unpack = real
assert(got == inputs.dump, "the native-order read differs from Go's dump")
print("native-order-ok")
`, ModuleName(u), root, ir.BuildVersion(u))
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		out := string(luarun.Run(t, dir, "main.lua", map[string][]byte{"block": image, "dump": want}))
		if out != "native-order-ok\n" {
			t.Fatalf("native-order probe: %q", out)
		}
	})

	// OPPOSITE ORDER is the one refusal the prologue exists for: a block whose
	// bytes are in the other order reads back the byte-reversed magic, and a
	// prologue whose order word disagrees with this build is not a block at all
	// (§19.1, §19.2).
	t.Run("opposite_order", func(t *testing.T) {
		t.Parallel()
		row, ok := blockRow(rows, "block_render")
		if !ok {
			t.Fatal("the manifest has no block_render image")
		}
		paths := units[row.unit]
		var full []string
		for _, p := range paths {
			full = append(full, filepath.Join(repo, p))
		}
		u := luarun.LoadUnit(t, full...)
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		image, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(repo, row.dump))
		if err != nil {
			t.Fatal(err)
		}
		root := blockRoot(t, want)
		probe := fmt.Sprintf(`local T = require(%q)
local B = require("block")
local inputs = require("inputs")
local V = 0x%016x
local host = string.pack("I2", 1):byte(1) == 1 and 1 or 2
local other = host == 1 and ">" or "<"
local MAGIC = 0x4b4c42414d484353
local swapped = string.pack(other .. "I8", MAGIC) .. inputs.block:sub(9)
local h, why = B.BlockOpen(T, %q, swapped, V)
assert(h == nil, "accepted a block in the opposite order")
assert(why == "foreign_order", "opposite order: want foreign_order, got " .. tostring(why))
local wrong = string.pack("=I8", MAGIC) .. string.pack("=I8", V)
  .. string.pack("=I8", host == 1 and 2 or 1) .. inputs.block:sub(25)
local h2, why2 = B.BlockOpen(T, %q, wrong, V)
assert(h2 == nil, "accepted a block whose byte order word disagrees")
assert(why2 == "not_a_cook", "wrong order word: want not_a_cook, got " .. tostring(why2))
print("opposite-order-ok")
`, ModuleName(u), ir.BuildVersion(u), root, root)
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		out := string(luarun.Run(t, dir, "main.lua", map[string][]byte{"block": image}))
		if out != "opposite-order-ok\n" {
			t.Fatalf("opposite-order probe: %q", out)
		}
	})
}

// blockManifestRow is one manifest `block` line: the fixture's name, unit,
// image path and the Go-pinned canonical dump path. A block line names no
// root; the dump's first line does.
type blockManifestRow struct {
	name string
	unit string
	wire string
	dump string
}

func blockManifestRows(t *testing.T, path string) []blockManifestRow {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []blockManifestRow
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 5 && f[0] == "block" {
			rows = append(rows, blockManifestRow{name: f[1], unit: f[2], wire: f[3], dump: f[4]})
		}
	}
	return rows
}

func blockRoot(t *testing.T, dump []byte) string {
	t.Helper()
	line, _, _ := strings.Cut(string(dump), "\n")
	f := strings.Fields(line)
	if len(f) < 2 || f[0] != "projection" {
		t.Fatalf("the dump's first line names no projection: %q", line)
	}
	return f[1]
}

func blockRow(rows []blockManifestRow, name string) (blockManifestRow, bool) {
	for _, r := range rows {
		if r.name == name {
			return r, true
		}
	}
	return blockManifestRow{}, false
}

// luaBlockRefusals runs the damaged-block probe over block_render: every
// refusal of BlockOpen in §19.2's order, by its TableRefuseReason name,
// including an intact magic with the other order word (I7). patch, when set,
// rewrites the emitted runtime files before the probe runs, so a negative
// control can take a clause out and require the probe to go red.
func luaBlockRefusals(t *testing.T, patch func(files map[string][]byte)) (stdout, stderr string, err error) {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	rows := blockManifestRows(t, manifest)
	row, ok := blockRow(rows, "block_render")
	if !ok {
		t.Fatal("the manifest has no block_render image")
	}
	paths := units[row.unit]
	var full []string
	for _, p := range paths {
		full = append(full, filepath.Join(repo, p))
	}
	u := luarun.LoadUnit(t, full...)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if patch != nil {
		patch(files)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	image, err := os.ReadFile(filepath.Join(repo, row.wire))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(repo, row.dump))
	if err != nil {
		t.Fatal(err)
	}
	root := blockRoot(t, want)
	probe := fmt.Sprintf(`local T = require(%q)
local B = require("block")
local inputs = require("inputs")
local V = 0x%016x
local base = inputs.block
local L = B.Layout(T, %q, true)
local function field(name)
  for _, fl in ipairs(L.fields) do
    if fl.f.name == name then return fl end
  end
  error("no field " .. name)
end
local function patch(s, off, data)
  return s:sub(1, off) .. data .. s:sub(off + 1 + #data)
end
local function expect(reason, s, base_offset)
  local h, why = B.BlockOpen(T, %q, s, V, base_offset)
  assert(h == nil, "accepted a damaged block: wanted " .. reason)
  assert(why == reason, "want %%s, got " .. tostring(why))
end
expect("truncated", base:sub(1, 23))
expect("not_a_cook", string.rep("\0", 8) .. base:sub(9))
expect("foreign_order", string.pack("<I8", 0x5343484d41424c4b) .. base:sub(9))
expect("wrong_build_version", patch(base, 8, string.pack("<I8", V + 1)))
expect("not_a_cook", patch(base, 16, string.pack("<I8", 2)))
local cameras = field("cameras")
expect("bad_layout", patch(base, cameras.offset + 12, string.pack("<I4", 0)))
expect("bad_layout", patch(base, cameras.offset + 8, string.pack("<I4", cameras.bound + 1)))
expect("bad_layout", patch(base, cameras.offset, string.pack("<I8", 0x7fffffffffffffc0)))
expect("bad_layout", patch(base, cameras.offset + 8, string.pack("<I4", 1000)))
expect("truncated", base:sub(1, #base - 1))
expect("unaligned_base", base, 1)
print("block-refusals-ok")
`, ModuleName(u), ir.BuildVersion(u), root, root)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	return luarun.Try(t, dir, "main.lua", map[string][]byte{"block": image})
}
