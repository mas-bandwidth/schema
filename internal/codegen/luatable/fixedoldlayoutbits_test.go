package luatable

// A BITS(N) FIELD LANDS FROM AN OLDER LAYOUT LIKE ANY OTHER FIELD
// (docs/FIXED-FORM-ALGORITHM.md §4.2, §5): the fixed read of a record an older
// locked layout wrote lands every field that layout carries and this build
// still declares, with its written value. A bits(N) rides at its declared
// storage width under the unsigned integer kind its bit count picks, so the
// older layout names kinds 6, 7 or 8 at four bytes and kind 9 at eight bytes;
// the read accepts each of them. A value past the field's own mask is refused
// by the bounds pass (§4.6), never clamped.

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const oldLayoutBitsOlder = `package vold_old_layout_bits

fixed table Bits
{
    lead  uint32 = 1
    small bits(12)
    large bits(48)
    trail uint32 = 2
}
`

const oldLayoutBitsNewer = `package vnew_old_layout_bits

fixed table Bits
{
    lead  uint32 = 1
    small bits(12)
    large bits(48)
    trail uint32 = 2
    tail  uint32 = 77
}
`

// Functional (luaBinary): run with SCHEMA_SLOW=1.
func TestLuaFixedOlderLayoutLandsBitsField(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (it runs an older and a newer emitted module of one fixed-form lineage)")
	src := t.TempDir()
	oldPath := filepath.Join(src, "VOLD_old_layout_bits.schema")
	newPath := filepath.Join(src, "VNEW_old_layout_bits.schema")
	writeFile(t, oldPath, oldLayoutBitsOlder)
	writeFile(t, newPath, oldLayoutBitsNewer)
	older := luarun.LoadUnit(t, oldPath)
	newer := luarun.LoadUnit(t, newPath)
	lineage := map[string][]FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(older) {
		e, ok := FixedLineageOf(older, st.Name)
		if !ok {
			t.Fatalf("no lineage entry for %s", st.Name)
		}
		lineage[st.Name] = append(lineage[st.Name], e)
	}
	oldFiles, err := Generate(older)
	if err != nil {
		t.Fatalf("generate the older unit: %v", err)
	}
	newFiles, err := GenerateLineage(newer, lineage)
	if err != nil {
		t.Fatalf("generate the newer unit with its lineage: %v", err)
	}

	// The older writer writes the lawful record and one variant per bits field;
	// each variant differs from the lawful record in that field's storage
	// alone, which is how the patch finds the bytes it rewrites.
	oldDir := t.TempDir()
	luarun.WriteFiles(t, oldDir, oldFiles)
	writeFile(t, filepath.Join(oldDir, "main.lua"), fmt.Sprintf(`
local T = require(%q)
local function put(name, small, large)
  local v = T.NewBits()
  v.lead = 0x11111111
  v.small = small
  v.large = large
  v.trail = 0x22222222
  local f = io.open(name, "wb")
  f:write(T.WriteFixed("Bits", v))
  f:close()
end
put("lawful.bin", 0x123, 0x123)
put("small.bin", 0xfff, 0x123)
put("large.bin", 0x123, 0xffffffffffff)
`, ModuleName(older)))
	luarun.Run(t, oldDir, "main.lua", nil)
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(oldDir, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	lawful := read("lawful.bin")
	offset := func(variant string, width int) int {
		other := read(variant)
		if len(other) != len(lawful) {
			t.Fatalf("%s: %d bytes, the lawful record has %d", variant, len(other), len(lawful))
		}
		first, last := -1, -1
		for i := range lawful {
			if lawful[i] != other[i] {
				if first < 0 {
					first = i
				}
				last = i
			}
		}
		if first < 0 || last >= first+width {
			t.Fatalf("%s: the field's storage is not one %d-byte span (bytes %d..%d differ)", variant, width, first, last)
		}
		return first
	}
	smallAt := offset("small.bin", 4)
	largeAt := offset("large.bin", 8)
	patch := func(at int, b []byte) []byte {
		out := append([]byte(nil), lawful...)
		copy(out[at:], b)
		return out
	}
	u32 := func(v uint32) []byte {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, v)
		return b
	}
	u64 := func(v uint64) []byte {
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, v)
		return b
	}
	inputs := map[string][]byte{
		"lawful":     lawful,
		"small_past": patch(smallAt, u32(1<<12)),
		"large_past": patch(largeAt, u64(1<<48)),
	}

	newDir := t.TempDir()
	luarun.WriteFiles(t, newDir, newFiles)
	writeFile(t, filepath.Join(newDir, "main.lua"), fmt.Sprintf(`
local T = require(%q)
local I = require("inputs")
local v = T.ReadFixed("Bits", I.lawful)
assert(v.lead == 0x11111111 and v.trail == 0x22222222, "the neighbours moved")
assert(v.small == 0x123, "the four-byte bits(12) did not land")
assert(v.large == 0x123, "the eight-byte bits(48) did not land")
assert(v.tail == 77, "the appended field is not its declared default")
for _, name in ipairs({ "small_past", "large_past" }) do
  local ok = pcall(T.ReadFixed, "Bits", I[name])
  print(name .. " " .. (ok and "accepted" or "refused"))
end
print("bits-ok")
`, ModuleName(newer)))
	out := string(luarun.Run(t, newDir, "main.lua", inputs))
	if !strings.Contains(out, "bits-ok\n") {
		t.Fatalf("the older layout's bits fields did not land: %q", out)
	}
	for _, name := range []string{"small_past", "large_past"} {
		if !strings.Contains(out, name+" refused\n") {
			t.Errorf("%s: a bits field past its mask was not refused (got %q)", name, out)
		}
	}
}
