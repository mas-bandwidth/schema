package luatable

// AN OLDER LAYOUT'S READ HOLDS THE SAME BOUNDS (docs/FIXED-FORM-ALGORITHM.md
// §4.6): the strict pass runs over STORAGE, so one pass covers both plans. A
// record an older locked layout wrote, read by the newer module of the same
// lineage, refuses a compressed float past either end of [min, max] and a
// ranged 128-bit value past its range, at the root and in a nested table,
// exactly as the read under this build's own layout refuses them; the lawful
// record reads.

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const oldLayoutBoundsOlder = `package vold_old_layout_bounds

fixed table Inner
{
    cf float32 | min = 0.0, max = 10.0, resolution = 0.01
    n  int32 = 0 | min = 0, max = 100
}

fixed table Bounds
{
    cf    float32 | min = 0.0, max = 10.0, resolution = 0.01
    big   int128 = 0 | min = -5000000000, max = 5000000000
    n     int32 = 0 | min = 0, max = 100
    inner Inner
}
`

const oldLayoutBoundsNewer = `package vnew_old_layout_bounds

fixed table Inner
{
    cf float32 | min = 0.0, max = 10.0, resolution = 0.01
    n  int32 = 0 | min = 0, max = 100
}

fixed table Bounds
{
    cf    float32 | min = 0.0, max = 10.0, resolution = 0.01
    big   int128 = 0 | min = -5000000000, max = 5000000000
    n     int32 = 0 | min = 0, max = 100
    inner Inner
    tail  int32 = 77
}
`

// Functional (luaBinary): run with SCHEMA_SLOW=1.
func TestLuaFixedOlderLayoutHoldsBounds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (it runs an older and a newer emitted module of one fixed-form lineage)")
	src := t.TempDir()
	oldPath := filepath.Join(src, "VOLD_old_layout_bounds.schema")
	newPath := filepath.Join(src, "VNEW_old_layout_bounds.schema")
	writeFile(t, oldPath, oldLayoutBoundsOlder)
	writeFile(t, newPath, oldLayoutBoundsNewer)
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

	// The older writer writes the lawful record and one variant per field;
	// each variant differs from the lawful record in that field's storage
	// alone, which is how the patch finds the bytes it rewrites.
	oldDir := t.TempDir()
	luarun.WriteFiles(t, oldDir, oldFiles)
	writeFile(t, filepath.Join(oldDir, "main.lua"), fmt.Sprintf(`
local T = require(%q)
local function put(name, setup)
  local v = T.NewBounds()
  v.cf = 3.33
  v.n = 7
  v.inner.cf = 3.33
  v.inner.n = 9
  setup(v)
  local f = io.open(name, "wb")
  f:write(T.WriteFixed("Bounds", v))
  f:close()
end
put("lawful.bin", function(v) end)
put("cf.bin", function(v) v.cf = 0.0 end)
put("big.bin", function(v) v.big = { lo = -1, hi = -1 } end)
put("innercf.bin", function(v) v.inner.cf = 0.0 end)
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
	span := func(variant string, width int) int {
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
		if first < 0 || last-first+1 != width {
			t.Fatalf("%s: the field's storage is not one %d-byte span (bytes %d..%d differ)", variant, width, first, last)
		}
		return first
	}
	cfAt := span("cf.bin", 4)
	bigAt := span("big.bin", 16)
	innerAt := span("innercf.bin", 4)
	patch := func(at int, b []byte) []byte {
		out := append([]byte(nil), lawful...)
		copy(out[at:], b)
		return out
	}
	big := func(v int64) []byte {
		b := make([]byte, 16)
		binary.LittleEndian.PutUint64(b, uint64(v))
		hi := uint64(0)
		if v < 0 {
			hi = math.MaxUint64
		}
		binary.LittleEndian.PutUint64(b[8:], hi)
		return b
	}
	inputs := map[string][]byte{
		"lawful":        lawful,
		"cf_above":      patch(cfAt, f32bytes(11.0)),
		"cf_below":      patch(cfAt, f32bytes(-0.5)),
		"inner_above":   patch(innerAt, f32bytes(10.5)),
		"inner_below":   patch(innerAt, f32bytes(-1.0)),
		"big_above":     patch(bigAt, big(5000000001)),
		"big_below":     patch(bigAt, big(-5000000001)),
		"cf_top_lawful": patch(cfAt, f32bytes(10.0)),
	}
	// The control: the older module's own identity read refuses every patched
	// record, so each patch lands on the field's storage.
	writeFile(t, filepath.Join(oldDir, "check.lua"), fmt.Sprintf(`
local T = require(%q)
local I = require("inputs")
T.ReadFixed("Bounds", I.lawful)
T.ReadFixed("Bounds", I.cf_top_lawful)
for _, name in ipairs({ "cf_above", "cf_below", "inner_above", "inner_below", "big_above", "big_below" }) do
  assert(not pcall(T.ReadFixed, "Bounds", I[name]), name .. ": the older module's own read accepted it")
end
print("control-ok")
`, ModuleName(older)))
	if got := string(luarun.Run(t, oldDir, "check.lua", inputs)); got != "control-ok\n" {
		t.Fatalf("control: got %q", got)
	}
	newDir := t.TempDir()
	luarun.WriteFiles(t, newDir, newFiles)
	writeFile(t, filepath.Join(newDir, "main.lua"), fmt.Sprintf(`
local T = require(%q)
local I = require("inputs")
local v = T.ReadFixed("Bounds", I.lawful)
assert(math.abs(v.cf - 3.33) < 1e-5 and v.n == 7, "the lawful root did not land")
assert(math.abs(v.inner.cf - 3.33) < 1e-5 and v.inner.n == 9, "the lawful nested table did not land")
assert(v.tail == 77, "the appended field is not its declared default")
local top = T.ReadFixed("Bounds", I.cf_top_lawful)
assert(top.cf == 10.0, "a float at its declared max did not land")
for _, name in ipairs({ "cf_above", "cf_below", "inner_above", "inner_below", "big_above", "big_below" }) do
  local ok = pcall(T.ReadFixed, "Bounds", I[name])
  print(name .. " " .. (ok and "accepted" or "refused"))
end
`, ModuleName(newer)))
	out := string(luarun.Run(t, newDir, "main.lua", inputs))
	for _, name := range []string{"cf_above", "cf_below", "inner_above", "inner_below", "big_above", "big_below"} {
		if !strings.Contains(out, name+" refused\n") {
			t.Errorf("%s: an older layout's record past the declared bound was not refused (got %q)", name, out)
		}
	}
}

func f32bytes(v float32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, math.Float32bits(v))
	return b
}
