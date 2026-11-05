package luautable_test

// THE QUIET-BENCH EVIDENCE, attempt 8 (linux x86-64, pinned Luau 0.741). This
// is the committed form of the numbers the card report must carry: the exact
// command, the host load before and during, and both before/after tables.
//
// Command:
//   nice -n 19 go test -count=1 -timeout 600s -v \
//     ./internal/codegen/luautable/ -run TestLuauFixedHotPathBenchEvidence
// Load average (1m/5m/15m): before 11.56/10.37/20.10; during 8.11/9.66/19.50.
//
// fixed8 (signed-integers): the baseline fixture schema_fixed_before.luau
// versus the current runtime, median ops/s across 8 paired samples,
//   read:       before 293074 -> after 291685
//   write:      before 273370 -> after 268638
//   round_trip: before 137972 -> after 136321
// The differences (-0.5%..-1.7%) sit inside the sample spreads (1.9%..11.9%)
// on a loaded host, so the width dispatch is not measurably slower.
//
// fixed16 (signed-128 / unsigned-128): absolute ops/s, median of 8 runs; the
// old half-width read is not a valid 16-byte baseline, so these stand alone,
//   signed-128    read 539718  write 640055  round_trip 282824
//   unsigned-128  read 538371  write 641073  round_trip 286368

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestLuauFixedHotPathBenchEvidence measures the actual fixed runtime under
// the pinned interpreter. The 8-byte comparison includes the new readScalar
// width dispatch on bytes both runtimes prove against C++; corrected 128-bit
// throughput is absolute because the old half-width behavior is not a valid
// 16-byte baseline.
func TestLuauFixedHotPathBenchEvidence(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and live C++ fixed-form oracle")

	const iters = 100000
	const pairs = 8
	baselineRuntime, err := os.ReadFile("testdata/schema_fixed_before.luau")
	if err != nil {
		t.Fatal(err)
	}
	const baselineSHA256 = "60ed6ecb3545dcb57cd7547946a4a2aaa09dce92db3b2ade8a2217ceb217274e"
	if got := fmt.Sprintf("%x", sha256.Sum256(baselineRuntime)); got != baselineSHA256 {
		t.Fatalf("8-byte baseline is not the pre-width-16 runtime fixture: sha256=%s", got)
	}
	tc8 := luauFixedCase(t, "signed-integers")
	u8 := fixedproof.ParseCheckUnit(t, tc8.Schema)
	cpp8, verify8 := fixedproof.CppOracle(t, tc8, u8)
	if err := verify8(cpp8); err != nil {
		t.Fatalf("8-byte reference corpus did not verify: %v", err)
	}
	row8 := luauFixedRowFor(t, tc8.ID)
	before8, beforeMod := luauFixedUnit(t, u8)
	if err := os.WriteFile(filepath.Join(before8.dir, "schema_fixed.luau"), baselineRuntime, 0o644); err != nil {
		t.Fatal(err)
	}
	after8, afterMod := luauFixedUnit(t, u8)
	before := make(map[string][]float64, 3)
	after := make(map[string][]float64, 3)
	for i := 0; i < pairs; i++ {
		if i%2 == 0 {
			fixedHotPathAppend(t, before, before8, beforeMod, tc8, row8.check, cpp8, iters, "before8")
			fixedHotPathAppend(t, after, after8, afterMod, tc8, row8.check, cpp8, iters, "after8")
		} else {
			fixedHotPathAppend(t, after, after8, afterMod, tc8, row8.check, cpp8, iters, "after8")
			fixedHotPathAppend(t, before, before8, beforeMod, tc8, row8.check, cpp8, iters, "before8")
		}
	}
	for _, mode := range []string{"read", "write", "round_trip"} {
		bm, bmin, bmax, bsp := fixedHotPathStats(before[mode], iters)
		am, amin, amax, asp := fixedHotPathStats(after[mode], iters)
		t.Logf("fixed8 %s ops/s, all %d paired samples: before median=%.0f min=%.0f max=%.0f spread=%.2f%%; after median=%.0f min=%.0f max=%.0f spread=%.2f%%", mode, pairs, bm, bmin, bmax, bsp, am, amin, amax, asp)
	}
	for _, id := range []string{"signed-128", "unsigned-128"} {
		tc := luauFixedCase(t, id)
		u := fixedproof.ParseCheckUnit(t, tc.Schema)
		cpp, verify := fixedproof.CppOracle(t, tc, u)
		if err := verify(cpp); err != nil {
			t.Fatalf("%s reference corpus did not verify: %v", id, err)
		}
		row := luauFixedRowFor(t, id)
		g, mod := luauFixedUnit(t, u)
		samples := make(map[string][]float64, 3)
		for i := 0; i < pairs; i++ {
			m := fixedHotPathMeasure(t, g, mod, tc, row.check, cpp, iters, id)
			for _, mode := range []string{"read", "write", "round_trip"} {
				elapsed := m[mode]
				samples[mode] = append(samples[mode], elapsed)
				t.Logf("fixed16 %s run=%d %s seconds=%.6f ops/s=%.0f", id, i+1, mode, elapsed, float64(iters)/elapsed)
			}
		}
		for _, mode := range []string{"read", "write", "round_trip"} {
			median, min, max, spread := fixedHotPathStats(samples[mode], iters)
			t.Logf("fixed16 %s %s absolute ops/s, all %d runs: median=%.0f min=%.0f max=%.0f spread=%.2f%%", id, mode, pairs, median, min, max, spread)
		}
	}
}

func fixedHotPathAppend(t *testing.T, dst map[string][]float64, g *generatedUnit, mod string, tc fixedproof.RowCase, check string, wire []byte, iters int, label string) {
	t.Helper()
	m := fixedHotPathMeasure(t, g, mod, tc, check, wire, iters, label)
	for _, name := range []string{"read", "write", "round_trip"} {
		seconds := m[name]
		dst[name] = append(dst[name], seconds)
		t.Logf("%s %s seconds=%.6f ops/s=%.0f", label, name, seconds, float64(iters)/seconds)
	}
}

func fixedHotPathMeasure(t *testing.T, g *generatedUnit, mod string, tc fixedproof.RowCase, check string, wire []byte, iters int, label string) map[string]float64 {
	t.Helper()
	program := fmt.Sprintf(`--!strict
local inputs = require("./inputs")
local T = require("./%[1]s")
local F = require("./schema_fixed")
local fixed = { hash = T.%[2]sFixedHash, recordBytes = T.%[2]sFixedRecordBytes, layout = T.%[2]sFixedLayout, lineage = T.%[2]sFixedLineage, floor = T.%[2]sFixedFloor }
local back = F.load(T.%[2]sDescriptor, fixed, inputs.wire)
if #back ~= %[3]d then error("reference row count differs", 0) end
if not (%[4]s) then error("fixed load landed wrong values", 0) end
local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do parts[i + 1] = string.format("%%02x", buffer.readu8(b, i)) end
	return table.concat(parts)
end
local reference = inputs.wire
local initial = F.save(T.%[2]sDescriptor, fixed, back)
if hex(initial) ~= hex(reference) then error("fixed load/save differs from C++ reference", 0) end
local sink = 0
local function measure(name: string, op: () -> any)
	local start = os.clock()
	for i = 1, %[5]d do
		local result = op()
		if typeof(result) == "buffer" then sink += buffer.len(result) else sink += #result end
	end
	print("@metric " .. name .. " " .. tostring(os.clock() - start))
end
measure("read", function() return F.load(T.%[2]sDescriptor, fixed, inputs.wire) end)
measure("write", function() return F.save(T.%[2]sDescriptor, fixed, back) end)
measure("round_trip", function() return F.save(T.%[2]sDescriptor, fixed, F.load(T.%[2]sDescriptor, fixed, inputs.wire)) end)
print("@sink " .. tostring(sink))
`, mod, tc.Root, tc.Count, check, iters)
	out := luauFixedRun(t, g.dir, program, map[string][]byte{"wire": wire})
	metrics := make(map[string]float64, 3)
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 3 || fields[0] != "@metric" {
			continue
		}
		seconds, err := strconv.ParseFloat(fields[2], 64)
		if err != nil || seconds <= 0 {
			t.Fatalf("%s: invalid %s timing %q: %v", label, fields[1], fields[2], err)
		}
		metrics[fields[1]] = seconds
	}
	for _, mode := range []string{"read", "write", "round_trip"} {
		if metrics[mode] <= 0 {
			t.Fatalf("%s: missing %s timing in Luau output: %s", label, mode, out)
		}
	}
	if !strings.Contains(out, "@sink ") {
		t.Fatalf("%s: timing sink missing: %s", label, out)
	}
	return metrics
}

func fixedHotPathStats(seconds []float64, iters int) (median, min, max, spreadPct float64) {
	if len(seconds) == 0 {
		return 0, 0, 0, 0
	}
	rates := make([]float64, len(seconds))
	for i, elapsed := range seconds {
		rates[i] = float64(iters) / elapsed
	}
	sort.Float64s(rates)
	median = rates[len(rates)/2]
	if len(rates)%2 == 0 {
		median = (rates[len(rates)/2-1] + rates[len(rates)/2]) / 2
	}
	min, max = rates[0], rates[len(rates)-1]
	if median > 0 {
		spreadPct = 100 * (max - min) / median
	}
	return median, min, max, spreadPct
}
