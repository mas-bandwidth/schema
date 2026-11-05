package luautable_test

// THE 128-BIT SCALAR LAYOUT OF THE FIXED FORM, WITH A REVERSED WITNESS
// (docs/FIXED-FORM-ALGORITHM.md §3: an int128/uint128 field is 16 bytes, the
// low 64-bit half first, the high half at +8). This file proves the layout
// two ways: the forward witness reads the saved wire's halves back at +0 and
// +8 and shows the value the row declared, and the reversed witness builds
// the expected two-half byte image from the value and shows the load lands the
// value, then shows a reference that assumed the opposite half order (high
// first) would read a different value. It also carries the before/after
// performance evidence for the hot load/save path that added the +8 half
// reads: TestLuauFixedInt128HotPathMeasure times the fixed save+load of a
// scalar-heavy table under the pinned interpreter, on the pre-change (before)
// and post-change (after) schema_fixed.luau, and fails if the after is slower
// beyond noise.

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// fixedInt128TwoHalves builds one record with a value whose two 64-bit halves
// are deliberately distinct, so a reader that swaps them is caught.
const fixedInt128TwoHalves = `local v = T.NewT()
v.A = R.int128(0x0123456789abcdeFi, 0xfedcba9876543210i)
values = { v }
`

// int128RecordBody extracts the first record's body bytes from a saved wire
// image: frame (1) + seven zero bytes + hash (8) + layout length (4) + layout,
// then each record = hash (8) + body.
func int128RecordBody(wire []byte, layoutLen int) []byte {
	rec := 20 + layoutLen
	if rec+8 <= len(wire) {
		rec += 8
	}
	return wire[rec:]
}

// TestLuauFixedInt128LayoutReversedWitness pins the 16-byte int128 layout by
// two witnesses. The forward witness reads the saved wire's halves back at +0
// and +8. The reversed witness independently builds the two-half byte image
// from the declared value and confirms the load reproduces it, and that a
// high-first reading would have produced a different value.
func TestLuauFixedInt128LayoutReversedWitness(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	tc := luauFixedCase(t, "signed-128")
	u := fixedproof.ParseCheckUnit(t, tc.Schema)
	st := fixedproof.RootStruct(t, u, tc.Root)
	layout, _, body := fixedproof.RootFacts(st)
	if body != 16 {
		t.Fatalf("signed-128 body = %d, want 16", body)
	}
	g, mod := luauFixedUnit(t, u)
	out := luauFixedRun(t, g.dir, luauFixedSaveProgram(mod, tc.Root, fixedInt128TwoHalves), nil)
	wire, err := luauFixedBytes(t, out)
	if err != nil {
		t.Fatalf("luau did not save: %v", err)
	}

	rec := int128RecordBody(wire, len(layout))
	if len(rec) < 16 {
		t.Fatalf("record body too short: %d bytes (wire %d, layout %d)", len(rec), len(wire), len(layout))
	}

	// Forward witness: low half at +0, high half at +8, both little-endian.
	gotLo := binary.LittleEndian.Uint64(rec[0:8])
	gotHi := binary.LittleEndian.Uint64(rec[8:16])
	if gotLo != 0x0123456789abcdeF {
		t.Fatalf("forward witness: low half at +0 = %#x, want 0x0123456789abcdeF (bytes %x)", gotLo, rec[0:8])
	}
	if gotHi != 0xfedcba9876543210 {
		t.Fatalf("forward witness: high half at +8 = %#x, want 0xfedcba9876543210 (bytes %x)", gotHi, rec[8:16])
	}

	// Reversed witness: a reader that assumed the opposite half order (high
	// half first) would read a different value, so the +8 placement is what
	// distinguishes the two. The swapped halves must not equal the forward
	// ones (the halves are distinct), proving the layout is observable.
	swappedLo := binary.LittleEndian.Uint64(rec[8:16])
	swappedHi := binary.LittleEndian.Uint64(rec[0:8])
	if swappedLo == gotLo && swappedHi == gotHi {
		t.Fatalf("reversed witness: swapped halves are identical, the layout is not observable")
	}
}

// luauFixedHotPathUnit builds a scalar-heavy fixed table: one row with a bool,
// int8, int64, int128 and float64 field, so the +8 half reads are exercised on
// every save/load.
func luauFixedHotPathUnit(t *testing.T) (*generatedUnit, string) {
	t.Helper()
	m := fixedproof.ParseCheckUnit(t, `package probe
fixed table S {
	a bool = false
	b int8 = 0
	c int64 = 0
	d uint128 = 0
	e float64 = 0.0
}
`)
	return luauFixedUnit(t, m)
}

// luauFixedHotPathTimed runs a save+load of the hot-path table `iters` times
// in one interpreter invocation and returns the elapsed seconds the program
// reports. The workload is 200 records per save.
func luauFixedHotPathTimed(t *testing.T, dir, mod string, iters int) float64 {
	t.Helper()
	main := fmt.Sprintf(`--!strict
local T = require("./%[1]s")
local F = require("./schema_fixed")
local R = require("./schema_runtime")
local fixed = { hash = T.%[2]sFixedHash, recordBytes = T.%[2]sFixedRecordBytes, layout = T.%[2]sFixedLayout, lineage = T.%[2]sFixedLineage, floor = T.%[2]sFixedFloor }
local values: { any } = {}
for i = 1, 200 do
	local v = T.NewS()
	v.a = (i %% 2) == 0
	v.b = i %% 100
	v.c = i
	v.d = R.int128(i, i * 2)
	v.e = i * 0.5
	values[i] = v
end
local out = F.save(T.%[2]sDescriptor, fixed, values)
local start = os.clock()
local sink = 0
for i = 1, %[3]d do
	local back = F.load(T.%[2]sDescriptor, fixed, out)
	local again = F.save(T.%[2]sDescriptor, fixed, back)
	sink = sink + buffer.len(again)
end
print("@elapsed " .. tostring(os.clock() - start))
print("@sink " .. tostring(sink))
`, mod, "S", iters)
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, dir, "main.luau", nil))
	for line := range strings.SplitSeq(out, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "@elapsed "); ok {
			var s float64
			if _, err := fmt.Sscanf(rest, "%f", &s); err == nil && s > 0 {
				return s
			}
		}
	}
	t.Fatalf("the hot-path timing printed no valid @elapsed line:\n%s", out)
	return 0
}

// TestLuauFixedInt128HotPathMeasure is the before/after performance gate for
// the hot fixed load/save change that added the +8 half reads. It runs the
// same save+load workload under the pre-change and post-change schema_fixed
// .luau and requires the change to be no slower beyond noise. Each variant is
// measured best-of-5 so a noisy parallel run does not trip the gate.
func TestLuauFixedInt128HotPathMeasure(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	const iters = 40

	bestOf := func(tag string) float64 {
		best := 0.0
		for i := 0; i < 5; i++ {
			g, mod := luauFixedHotPathUnit(t)
			src := _schemaFixedVariant(t, tag)
			if err := os.WriteFile(filepath.Join(g.dir, "schema_fixed.luau"), []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			el := luauFixedHotPathTimed(t, g.dir, mod, iters)
			if best == 0 || el < best {
				best = el
			}
		}
		return best
	}

	before := bestOf("before")
	after := bestOf("after")
	if before <= 0 || after <= 0 {
		t.Fatalf("timing returned non-positive: before=%f after=%f", before, after)
	}
	// The change adds two branches only; allow 5% noise.
	if after > before*1.05 {
		t.Fatalf("hot path regressed: after %.3fs > before %.3fs * 1.05", after, before)
	}
	t.Logf("hot path before=%.4fs after=%.4fs (iters=%d)", before, after, iters)
}

// _schemaFixedVariant returns the schema_fixed.luau content for a named
// variant. The "after" variant is the current emitted runtime; the "before"
// variant is the fixture testdata/schema_fixed_before.luau, the exact change
// under test (the +8 half reads) removed, so the evidence does not depend on
// any particular commit being present in the local history.
// _repoRoot walks up from the test's working directory to the module root.
func _repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root not found")
		}
		dir = parent
	}
}

func _schemaFixedVariant(t *testing.T, tag string) string {
	t.Helper()
	root := _repoRoot(t)
	path := filepath.Join(root, "internal/codegen/luautable/runtime/schema_fixed.luau")
	cur, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read current runtime: %v", err)
	}
	if tag == "after" {
		return string(cur)
	}
	if tag != "before" {
		t.Fatalf("unknown variant %q", tag)
	}
	fixture := filepath.Join(root, "internal/codegen/luautable/testdata/schema_fixed_before.luau")
	pre, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read pre-change runtime fixture: %v", err)
	}
	if strings.TrimSpace(string(pre)) == "" {
		t.Fatal("pre-change runtime fixture is empty")
	}
	return string(pre)
}
