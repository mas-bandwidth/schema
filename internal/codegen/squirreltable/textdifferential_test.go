package squirreltable_test

// I13 (docs/PORTING.md, "The text differential against a third
// implementation") on the Squirrel leg. The leg draws N random float instances
// and writes each as (wire, text) through the emitted runtime's save and
// ToJson; the compiler's own engine (internal/tabletext and internal/tablepack,
// written from the spec and from neither backend) reads the same wire and
// writes its text, and the two texts are byte-compared; then the engine packs
// the leg's text back and that wire is byte-compared with the leg's wire.
//
// The pinned corpus (TestSquirrelJsonAgreesWithTheCorpus) never reaches a
// float tie, and a float tie is where text writers disagree, so the instances
// are drawn here from a splitmix64 stream, in Squirrel. Non-finite floats have
// no JSON spelling and both writers refuse one, so the probe redraws a
// non-finite pattern. Instance 0 is the all-defaults value. The schema and the
// seed are the Lua and Elixir legs', so a divergence replays across legs.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const squirrelTextDifferentialSchema = `package textdiff
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
	squirrelTextDifferentialSeed = 24845619678
	squirrelTextDifferentialN    = 200
	squirrelTextDifferentialCost = "the pinned Squirrel interpreter over 200 random float instances"
)

// squirrelTextDifferentialProbe draws the instances and prints one line per
// instance: its index, the saved wire as hex, the ToJson text as hex.
const squirrelTextDifferentialProbe = `local schema = dofile("textdiff/schema_module.nut", true);
local T = schema.require(%q);
local J = schema.require("schema_table_json");
local state = %d;

// splitmix64 over Squirrel's wrapping 64-bit integers; >>> is the logical shift
local next64 = function() {
  state = state + 0x9E3779B97F4A7C15;
  local z = state;
  z = (z ^ (z >>> 30)) * 0xBF58476D1CE4E5B9;
  z = (z ^ (z >>> 27)) * 0x94D049BB133111EB;
  return z ^ (z >>> 31);
};
local scratch = blob(8);
// an all-ones exponent is an infinity or a NaN: neither has a JSON spelling
local f32 = function() {
  for (;;) {
    local bits = next64() & 0xFFFFFFFF;
    if ((bits & 0x7F800000) == 0x7F800000) continue;
    scratch.seek(0); scratch.writen(bits, 'i'); scratch.seek(0);
    return scratch.readn('f');
  }
};
local f64 = function() {
  for (;;) {
    local z = next64();
    if ((z & 0x7FF0000000000000) == 0x7FF0000000000000) continue;
    scratch.seek(0); scratch.writen(z, 'l'); scratch.seek(0);
    return scratch.readn('d');
  }
};
local many = function(k, draw) {
  local out = [];
  for (local i = 0; i < k; i++) out.append(draw());
  return out;
};
local hexOf = function(b) {
  local h = "";
  for (local i = 0; i < b.len(); i++) h += format("%%02x", b[i] & 0xff);
  return h.len() == 0 ? "-" : h;
};
for (local i = 0; i < %d; i++) {
  local v = T.Root();
  if (i != 0) {
    v.A = f32();
    v.B = f64();
    local p = T.Sample();
    p.S = f32();
    p.D = f64();
    v.Pair = p;
    v.Many = many(8, f32);
    v.Wide = many(8, f32);
    v.Deep = many(8, f64);
  }
  local wire = T.RootSave(v);
  local text = J.ToJson(T, "Root", v);
  if (wire == null || text == null) throw "instance " + i + ": the save or ToJson refused";
  print("" + i + " " + hexOf(wire) + " " + hexOf(text) + "\n");
}
`

// runSquirrelTextDifferential generates the probe unit, replaces the emitted
// json runtime with json when it is not empty, runs the probe and both
// directions of the engine comparison, and returns the divergences.
func runSquirrelTextDifferential(t *testing.T, json string) []string {
	t.Helper()
	u := loadSource(t, squirrelTextDifferentialSchema)
	dir := t.TempDir()
	mod := emitUnit(t, dir, "textdiff", u)
	if json != "" {
		writeFile(t, filepath.Join(dir, "textdiff", "schema_table_json.nut"), []byte(json))
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(fmt.Sprintf(squirrelTextDifferentialProbe,
		mod, squirrelTextDifferentialSeed, squirrelTextDifferentialN)))
	out := sqrun.Run(t, dir, "main.nut", nil)
	m := tabletext.NewModel(u)
	var msgs []string
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != squirrelTextDifferentialN {
		t.Fatalf("the probe printed %d instances, want %d", len(lines), squirrelTextDifferentialN)
	}
	unhexOrEmpty := func(s string) []byte {
		if s == "-" {
			return nil
		}
		b, err := hex.DecodeString(s)
		if err != nil {
			t.Fatalf("probe hex %q: %v", s, err)
		}
		return b
	}
	for i, line := range lines {
		f := strings.Fields(line)
		if len(f) != 3 || f[0] != fmt.Sprint(i) {
			t.Fatalf("probe line %q is not instance %d, its wire and its text", line, i)
		}
		wire, legJSON := unhexOrEmpty(f[1]), unhexOrEmpty(f[2])
		d1 := t.TempDir()
		report, err := tablepack.UnpackOneFile(m, "Root", wire, d1)
		if err != nil || report != (tabletext.Report{}) {
			t.Fatalf("instance %d: the engine did not read the leg's wire cleanly: %v %+v", i, err, report)
		}
		engineJSON, err := os.ReadFile(filepath.Join(d1, "Root.json"))
		if err != nil {
			t.Fatal(err)
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
		d2 := t.TempDir()
		writeFile(t, filepath.Join(d2, "Root.json"), legJSON)
		wireBack, _, report2, err := tablepack.Pack(m, "Root", d2)
		if err != nil || report2 != (tabletext.Report{}) {
			t.Fatalf("instance %d: the engine did not pack the leg's text cleanly: %v %+v", i, err, report2)
		}
		if !bytes.Equal(wireBack, wire) {
			msgs = append(msgs, fmt.Sprintf("instance %d: engine wire packed from leg text differs from leg wire: engine=%x leg=%x", i, wireBack, wire))
		}
	}
	return msgs
}

// TestSquirrelTextDifferentialAgainstTheGoEngine is the row: the emitted
// Squirrel float writer and the engine's agree, byte for byte, over 200 random
// float instances, in both directions.
func TestSquirrelTextDifferentialAgainstTheGoEngine(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, squirrelTextDifferentialCost)
	if msgs := runSquirrelTextDifferential(t, ""); len(msgs) > 0 {
		t.Fatalf("%d of %d instances differ:\n%s", len(msgs), squirrelTextDifferentialN, strings.Join(msgs, "\n\n"))
	}
}

// TestSquirrelTextDifferentialNegativeControl caps a float32's text at SIX
// significant digits in a scratch copy of the json runtime (the anchor
// TestSquirrelJsonNegativeControl plants on, matched exactly once), so a
// float32 that needs seven, eight or nine renders short and only the
// cross-implementation comparison sees it. The gate must go red on the text.
func TestSquirrelTextDifferentialNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, squirrelTextDifferentialCost)
	const anchor = "local low = single ? 6 : 15, high = single ? 9 : 17;"
	runtime := string(squirreltable.Runtime()["schema_table_json.nut"])
	if n := strings.Count(runtime, anchor); n != 1 {
		t.Fatalf("the control's anchor appears %d times in the json runtime, want exactly once", n)
	}
	msgs := runSquirrelTextDifferential(t, strings.Replace(runtime, anchor, "local low = single ? 6 : 15, high = single ? 6 : 17;", 1))
	if len(msgs) == 0 {
		t.Fatal("a float32 writer capped at six significant digits left the text differential green")
	}
	if !strings.Contains(msgs[0], "leg text and engine text differ") {
		t.Fatalf("the differential went red, but not on the text comparison: %s", msgs[0])
	}
	t.Logf("control red as required: %d of %d instances differ", len(msgs), squirrelTextDifferentialN)
}
