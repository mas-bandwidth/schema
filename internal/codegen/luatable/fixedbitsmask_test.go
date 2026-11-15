package luatable

// A BITS(N) SLOT PAST ITS MASK REFUSES, WHATEVER ITS HIGH BITS
// (docs/FIXED-FORM-ALGORITHM.md §4.6). A bits(N) with 32 < N < 64 rides in an
// eight-byte unsigned slot, and the bounds pass compares it UNSIGNED: a stored
// value with bit 63 set is past 2^N - 1 like any other, never a negative number
// under the mask. Both fixed reads, ReadFixed (one record) and FixedLoad (the
// file), are held to it.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

func TestLuaFixedBitsMaskComparesUnsigned(t *testing.T) {
	t.Parallel()
	schemaDir := t.TempDir()
	writeFile(t, filepath.Join(schemaDir, "bitsmask.schema"), `package fixed_bits_mask

fixed table BitsMask
{
    lead  uint32 = 1
    v     bits(48)
    trail uint32 = 2
}
`)
	unit := luarun.LoadUnit(t, filepath.Join(schemaDir, "bitsmask.schema"))
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}

	// A lawful record and a lawful file, written by the leg itself, carrying a
	// sentinel in the bits(48) slot so the slot can be found in the bytes.
	const sentinel = uint64(0x0000_1234_5678_9abc)
	writeDir := t.TempDir()
	luarun.WriteFiles(t, writeDir, files)
	writeFile(t, filepath.Join(writeDir, "main.lua"), fmt.Sprintf(`
local T = require(%q)
local v = T.NewBitsMask()
v.lead = 7; v.v = %d; v.trail = 9
local function put(name, b)
  local f = io.open(name, "wb")
  f:write(b)
  f:close()
end
put("record.bin", T.WriteFixed("BitsMask", v))
put("file.bin", T.FixedSave("BitsMask", { v }))
`, ModuleName(unit), sentinel))
	luarun.Run(t, writeDir, "main.lua", nil)

	var slot [8]byte
	binary.LittleEndian.PutUint64(slot[:], sentinel)
	patch := func(name string, value uint64) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(writeDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if n := bytes.Count(b, slot[:]); n != 1 {
			t.Fatalf("%s: the bits(48) slot appears %d times, want 1", name, n)
		}
		at := bytes.Index(b, slot[:])
		out := append([]byte(nil), b...)
		binary.LittleEndian.PutUint64(out[at:at+8], value)
		return out
	}

	rows := []struct {
		name  string
		value uint64
	}{
		{"mask", 1<<48 - 1},
		{"one_past_mask", 1 << 48},
		{"bit63", 1 << 63},
		{"all_ones", ^uint64(0)},
	}
	inputs := map[string][]byte{}
	for _, r := range rows {
		inputs["record_"+r.name] = patch("record.bin", r.value)
		inputs["file_"+r.name] = patch("file.bin", r.value)
	}

	readDir := t.TempDir()
	luarun.WriteFiles(t, readDir, files)
	writeFile(t, filepath.Join(readDir, "main.lua"), fmt.Sprintf(`
local T = require(%q)
local I = require("inputs")
local function verdict(f, buf)
  local ok, got = pcall(f, buf)
  if not ok then
    return "refused"
  end
  return got
end
local function readFixed(buf)
  return T.ReadFixed("BitsMask", buf)
end
local function fixedLoad(buf)
  local rows = T.FixedLoad("BitsMask", buf)
  assert(#rows == 1, "one row was saved")
  return rows[1]
end
for _, name in ipairs({ "mask", "one_past_mask", "bit63", "all_ones" }) do
  for _, leg in ipairs({ { "ReadFixed", readFixed, "record_" }, { "FixedLoad", fixedLoad, "file_" } }) do
    local got = verdict(leg[2], I[leg[3] .. name])
    if got == "refused" then
      print(leg[1] .. " " .. name .. " refused")
    else
      assert(got.lead == 7 and got.trail == 9, leg[1] .. " " .. name .. ": the neighbours moved")
      print(leg[1] .. " " .. name .. " read " .. string.format("%%x", got.v))
    end
  end
end
`, ModuleName(unit)))
	out := string(luarun.Run(t, readDir, "main.lua", inputs))

	want := map[string]string{
		"mask":          "read ffffffffffff",
		"one_past_mask": "refused",
		"bit63":         "refused",
		"all_ones":      "refused",
	}
	for _, r := range rows {
		for _, leg := range []string{"ReadFixed", "FixedLoad"} {
			line := leg + " " + r.name + " " + want[r.name]
			if !strings.Contains(out, line+"\n") {
				t.Errorf("want %q; the probe printed:\n%s", line, out)
			}
		}
	}
}
