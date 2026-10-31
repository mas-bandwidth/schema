package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE SOAK, GATED ON THE ALLOCATION COUNT (docs/PORTING.md I9). A soak that
// gates only on heap drift cannot see a path that allocates and frees the same
// bytes every iteration: it reads +0 forever. This one loads through ONE state
// into ONE value, pass after pass, and at every sample re-measures the
// allocation COUNT of the rows that license nothing (it must be exactly zero,
// the collector stopped over the window) and of the row that licenses a floor
// (it must stay inside it), with the live heap after a full collection, the
// floor a carrier cannot move, read beside it as the weaker instrument that
// sees retention. The soak's length is a PASS COUNT, never a clock:
// TABLES_LUA_SOAK_ITERATIONS passes, 4000 by default so a short count rides
// `make test`, and the release act runs the long one.

const (
	soakDefaultPasses = 4000
	soakSample        = 500
	soakDriftSlackKB  = 8.0
	soakBudgetCeiling = 4096.0 // RootConfig root_full's licensed floor, bytes a load
)

func soakProbe(module string, passes int) string {
	return fmt.Sprintf(`local T = require(%q)
local load = require("load")
local inputs = require("inputs")
local PASSES, SAMPLE = %d, %d
local function report()
  return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
-- the rows: the roots that license nothing, then the one that licenses a floor
local rows = {
  { "KeyedConfig", "w1" },
  { "RootConfig", "w2" },
  { "RootConfig", "w3" },
}
for _, row in ipairs(rows) do
  row.wire = inputs[row[2]]
  row.report, row.state, row.into = report(), load.NewState(), T["New" .. row[1]]()
  assert(load.Load(T, row[1], row.wire, row.report, row.state, row.into) ~= nil, row[1] .. " does not load")
end
local function pass()
  for _, row in ipairs(rows) do load.Load(T, row[1], row.wire, row.report, row.state, row.into) end
end
collectgarbage("collect")
for _ = 1, 20 do pass() end
local floor0, floorN
local zero_max, budget_max, samples = 0.0, 0.0, 0
local done = 0
while done < PASSES do
  collectgarbage("collect")
  local floor = collectgarbage("count")
  if floor0 == nil then floor0 = floor end
  floorN = floor
  for _ = 1, 20 do pass() end
  collectgarbage("stop")
  local before = { }
  for i, row in ipairs(rows) do before[i] = 0.0 end
  -- each row measured by itself over the sample
  for i, row in ipairs(rows) do
    local b = collectgarbage("count")
    for _ = 1, SAMPLE do load.Load(T, row[1], row.wire, row.report, row.state, row.into) end
    local a = collectgarbage("count")
    local per = (a - b) * 1024 / SAMPLE
    if i <= 2 then
      if per > zero_max then zero_max = per end
    elseif per > budget_max then
      budget_max = per
    end
  end
  collectgarbage("restart")
  samples = samples + 1
  done = done + SAMPLE
end
print(samples, string.format("%%.1f", zero_max), string.format("%%.1f", budget_max), string.format("%%.2f", floorN - floor0))
`, module, passes, soakSample)
}

func soakPasses(t *testing.T) int {
	t.Helper()
	passes := soakDefaultPasses
	if v := os.Getenv("TABLES_LUA_SOAK_ITERATIONS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < soakSample {
			t.Fatalf("TABLES_LUA_SOAK_ITERATIONS=%q: a pass count of at least %d", v, soakSample)
		}
		passes = n
	}
	return passes
}

// soakRun runs the soak over files and answers its four numbers: the samples,
// the most any no-licence row allocated a load in any sample, the most the
// licensed row did, and the drift of the live heap between the first sample and
// the last, in KB.
func soakRun(t *testing.T, b builtUnit, files map[string][]byte, passes int) (samples int, zeroMax, budgetMax, driftKB float64) {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	inputs := map[string][]byte{}
	for i, w := range []string{"keyed_config", "root_default", "root_full"} {
		data, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", w+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs["w"+strconv.Itoa(i+1)] = data
	}
	out, stderr, err := try(t, files, soakProbe(b.module, passes), inputs)
	if err != nil {
		t.Fatalf("soak: %v\n%s", err, stderr)
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(out), "%d %f %f %f", &samples, &zeroMax, &budgetMax, &driftKB); err != nil {
		t.Fatalf("soak printed %q", out)
	}
	return samples, zeroMax, budgetMax, driftKB
}

func TestLuaLoadSoak(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (the pinned release)")
	if !pinned(t) {
		return
	}
	passes := soakPasses(t)
	b := buildManifestUnit(t, "tabledemo")
	samples, zeroMax, budgetMax, drift := soakRun(t, b, b.files, passes)
	t.Logf("%d passes, %d samples: no-licence rows %.1f bytes a load at most, licensed row %.1f (ceiling %.0f), live heap drift %.2f KB", passes, samples, zeroMax, budgetMax, soakBudgetCeiling, drift)
	if zeroMax != 0 {
		t.Errorf("a row that licenses nothing allocated %.1f bytes a load in a sample: the soak's count gate is red", zeroMax)
	}
	if budgetMax > soakBudgetCeiling {
		t.Errorf("the licensed row allocated %.1f bytes a load against %.0f", budgetMax, soakBudgetCeiling)
	}
	if drift > soakDriftSlackKB {
		t.Errorf("the live heap grew %.2f KB over the soak: a retained value", drift)
	}
}

// TestLuaLoadSoakNegativeControl plants a MATCHED ALLOCATE/FREE PAIR per load
// (a table built and dropped at the top of every load): the drift gate stays
// silent, because every sample's collection takes it back, and the count gate
// goes red. A soak that watched drift alone would pass it.
func TestLuaLoadSoakNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (the pinned release)")
	if !pinned(t) {
		return
	}
	b := buildManifestUnit(t, "tabledemo")
	anchor := "  local desc = T.Tables[name]\n  state = state or L.NewState()\n"
	files := b.patched(t, "load.lua", anchor, anchor+"  local pair = { name, bytes }\n  pair = nil\n")
	_, zeroMax, _, drift := soakRun(t, b, files, soakSample*2)
	if zeroMax == 0 {
		t.Errorf("the count gate stayed green with a table built and dropped per load")
	}
	if drift > soakDriftSlackKB {
		t.Errorf("the drift gate went red (%.2f KB) with a matched pair: the control no longer separates the two gates", drift)
	}
}
