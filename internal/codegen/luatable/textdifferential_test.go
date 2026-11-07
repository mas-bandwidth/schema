package luatable

// I13 (docs/PORTING.md, "The text differential against a third
// implementation") on the Lua leg. The leg emits N random float instances as
// (wire, text) through the emitted runtime's save.Save and json.ToJson; the
// compiler's own engine (internal/tabletext and internal/tablepack, written
// from the spec and from neither backend) reads the same wire and writes its
// text, and the two texts are byte-compared; then the engine packs the leg's
// text back and that wire is byte-compared with the leg's wire.
//
// The pinned corpus (TestLuaJsonAgreesWithTheCorpus) reaches eighteen
// instances and never a float tie, and a float tie is where text writers
// disagree, so the instances are drawn here from a splitmix64 stream, in Lua.
// Non-finite floats have no JSON spelling and both writers refuse one, so the
// probe redraws a non-finite pattern. Instance 0 is the all-defaults value. The
// schema is the Elixir leg's (internal/codegen/elixirtable/
// textdifferential_test.go): two floats, a nested type of two, a bounded list of
// float32, an unbounded list of each width.
//
// It runs the pinned interpreter, so it is a functional test behind the slow
// gate: make tables-lua-text-differential runs it through test/slowgate/proof.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const luaTextDifferentialSchema = `package textdiff
type Sample {
 s float32
 d float64
}
table Root {
 a float32
 b float64
 pair Sample
 many [..8]float32
 wide []float32
 deep []float64
}
`

const (
	// luaTextDifferentialSeed is the Elixir leg's, so a divergence replays
	// across the two legs from the same stream.
	luaTextDifferentialSeed = 24845619678
	luaTextDifferentialN    = 200
	luaTextDifferentialCost = "the pinned Lua interpreter (dist/lua-5.4.8, make dist-lua) over 200 random float instances"
)

// luaTextDifferentialProbe draws the instances and writes inst-<i>.wire and
// inst-<i>.json into the directory the first argument names.
const luaTextDifferentialProbe = `local T = require("TextdiffTables")
local save = require("save")
local J = require("json")

local dir, seed, n = arg[1], tonumber(arg[2]), tonumber(arg[3])
local state = seed

-- splitmix64 over Lua's native wrapping 64-bit integers; >> is logical
local function next_u64()
  state = state + 0x9E3779B97F4A7C15
  local z = state
  z = (z ~ (z >> 30)) * 0xBF58476D1CE4E5B9
  z = (z ~ (z >> 27)) * 0x94D049BB133111EB
  return z ~ (z >> 31)
end

-- an all-ones exponent is an infinity or a NaN: neither has a JSON spelling
local function f32()
  while true do
    local bits = next_u64() & 0xFFFFFFFF
    if bits & 0x7F800000 ~= 0x7F800000 then
      return (string.unpack("<f", string.pack("<I4", bits)))
    end
  end
end

local function f64()
  while true do
    local z = next_u64()
    if z & 0x7FF0000000000000 ~= 0x7FF0000000000000 then
      return (string.unpack("<d", string.pack("<i8", z)))
    end
  end
end

local function many(k, draw)
  local out = {}
  for i = 1, k do out[i] = draw() end
  return out
end

for i = 0, n - 1 do
  local v = T.NewRoot()
  if i ~= 0 then
    v.a = f32()
    v.b = f64()
    v.pair = { s = f32(), d = f64() }
    v.many = many(8, f32)
    v.wide = many(8, f32)
    v.deep = many(8, f64)
  end
  local wire = assert(save.Save(T, "Root", v))
  local text = assert(J.ToJson(T, "Root", v))
  local w = assert(io.open(dir .. "/inst-" .. i .. ".wire", "wb"))
  w:write(wire)
  w:close()
  local j = assert(io.open(dir .. "/inst-" .. i .. ".json", "wb"))
  j:write(text)
  j:close()
end
print("wrote " .. n)
`

// runLuaTextDifferential generates the probe unit, applies an optional
// in-memory edit to the emitted files, runs the probe, then runs both
// directions of the engine comparison. It returns the divergence messages and a
// non-nil error when any instance differs, so the negative control asserts on
// the same output the row produces. A probe that fails to run is fatal through
// luarun's own diagnostics, never a divergence.
func runLuaTextDifferential(t *testing.T, edit func(map[string][]byte)) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	schema := filepath.Join(dir, "textdiff.schema")
	if err := os.WriteFile(schema, []byte(luaTextDifferentialSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(files)
	}
	mod := t.TempDir()
	luarun.WriteFiles(t, mod, files)
	if err := os.WriteFile(filepath.Join(mod, "main.lua"), []byte(luaTextDifferentialProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	cmd := luaFuzzCommand(t, mod, out, strconv.Itoa(luaTextDifferentialSeed), strconv.Itoa(luaTextDifferentialN))
	if res, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the probe failed: %v\n%s", err, res)
	}
	m := tabletext.NewModel(u)
	msgs, err := compareLuaTextDifferential(m, out, luaTextDifferentialN)
	if err != nil {
		return nil, err
	}
	if len(msgs) > 0 {
		return []byte(strings.Join(msgs, "\n\n")), fmt.Errorf("%d of %d instance(s) differ", len(msgs), luaTextDifferentialN)
	}
	return nil, nil
}

// TestLuaTextDifferential is the row: the emitted Lua float writer and the
// engine's agree, byte for byte, over 200 random float instances, in both
// directions.
func TestLuaTextDifferential(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, luaTextDifferentialCost)
	out, err := runLuaTextDifferential(t, nil)
	if err != nil {
		t.Fatalf("text differential: %v\n%s", err, out)
	}
}

// luaTextDifferentialAnchor is the float digit range in json.lua's float_text:
// six to nine significant digits for a float32, the §16.7 shortest-round-trip
// loop over %.*g.
const luaTextDifferentialAnchor = "local low, high = single and 6 or 15, single and 9 or 17"

// TestLuaTextDifferentialNegativeControl caps a float32's text at SIX
// significant digits, so a float32 that needs seven, eight or nine renders
// short and only the cross-implementation comparison sees it. The gate must go
// red on the text comparison, not on a probe failure (a failed probe is fatal
// in runLuaTextDifferential and fails this test instead).
func TestLuaTextDifferentialNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, luaTextDifferentialCost)
	out, err := runLuaTextDifferential(t, func(files map[string][]byte) {
		src, ok := files["json.lua"]
		if !ok {
			t.Fatal("the emitted unit has no json.lua")
		}
		if c := bytes.Count(src, []byte(luaTextDifferentialAnchor)); c != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage anchor %q appeared %d times in json.lua, want 1", luaTextDifferentialAnchor, c)
		}
		files["json.lua"] = bytes.Replace(src, []byte(luaTextDifferentialAnchor),
			[]byte("local low, high = single and 6 or 15, single and 6 or 17 -- SABOTAGED"), 1)
	})
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: a float32 writer capped at six significant digits left the gate GREEN\n%s", out)
	}
	if !bytes.Contains(out, []byte("leg text and engine text differ")) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on the text comparison: %v\n%s", err, out)
	}
	t.Logf("control red as required: %v", err)
}

// compareLuaTextDifferential runs both directions for every instance the probe
// wrote. Direction one: the engine reads the leg's wire and writes its text,
// byte-compared with the leg's text. Direction two: the engine packs the leg's
// text back to wire, byte-compared with the leg's wire. Divergences are
// collected, not fatal on the first.
func compareLuaTextDifferential(m *tabletext.Model, dir string, n int) ([]string, error) {
	var msgs []string
	for i := range n {
		wire, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.wire", i)))
		if err != nil {
			return msgs, fmt.Errorf("read inst-%d.wire: %w", i, err)
		}
		legJSON, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.json", i)))
		if err != nil {
			return msgs, fmt.Errorf("read inst-%d.json: %w", i, err)
		}
		d1, err := os.MkdirTemp(dir, "d1-")
		if err != nil {
			return msgs, err
		}
		report, err := tablepack.UnpackOneFile(m, "Root", wire, d1)
		if err != nil {
			return msgs, fmt.Errorf("instance %d: UnpackOneFile: %w", i, err)
		}
		if report != (tabletext.Report{}) {
			return msgs, fmt.Errorf("instance %d: UnpackOneFile report not clean: %+v", i, report)
		}
		engineJSON, err := os.ReadFile(filepath.Join(d1, "Root.json"))
		if err != nil {
			return msgs, fmt.Errorf("instance %d: read engine Root.json: %w", i, err)
		}
		if !bytes.Equal(engineJSON, legJSON) {
			off := 0
			for off < len(engineJSON) && off < len(legJSON) && engineJSON[off] == legJSON[off] {
				off++
			}
			lo := max(off-32, 0)
			msgs = append(msgs, fmt.Sprintf("instance %d: leg text and engine text differ at byte %d\nleg[%d:]=%q\nengine[%d:]=%q",
				i, off, lo, legJSON[lo:min(off+32, len(legJSON))], lo, engineJSON[lo:min(off+32, len(engineJSON))]))
			continue
		}
		d2, err := os.MkdirTemp(dir, "d2-")
		if err != nil {
			return msgs, err
		}
		if err := os.WriteFile(filepath.Join(d2, "Root.json"), legJSON, 0o600); err != nil {
			return msgs, err
		}
		wireBack, _, report2, err := tablepack.Pack(m, "Root", d2)
		if err != nil {
			return msgs, fmt.Errorf("instance %d: Pack: %w", i, err)
		}
		if report2 != (tabletext.Report{}) {
			return msgs, fmt.Errorf("instance %d: Pack report not clean: %+v", i, report2)
		}
		if !bytes.Equal(wireBack, wire) {
			msgs = append(msgs, fmt.Sprintf("instance %d: engine wire packed from leg text differs from leg wire: engine=%x leg=%x", i, wireBack, wire))
		}
	}
	return msgs, nil
}

// luaFuzzCommand is the pinned interpreter running main.lua in dir with its
// arguments, with the whole of package.path luarun.Run gives a probe.
func luaFuzzCommand(t *testing.T, dir string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(luarun.Interpreter(t), append([]string{"main.lua"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LUA_PATH="+filepath.Join(dir, "?.lua")+";;", "LUA_INIT=")
	return cmd
}
