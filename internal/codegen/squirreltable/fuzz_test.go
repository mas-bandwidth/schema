package squirreltable_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// splitmix64 is the differential's random source (as internal/codegen/gotable/
// textdifferential_test.go). It mutates every instance wire of the corpus.
type splitmix64 struct{ state uint64 }

func (r *splitmix64) next() uint64 {
	r.state += 0x9E3779B97F4A7C15
	z := r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

func mutateWire(r *splitmix64, base []byte) []byte {
	w := make([]byte, len(base))
	for i := range base {
		w[i] = byte(r.next())
	}
	return w
}

const fuzzUnit = "tblv1"
const fuzzRoot = "Cfg"
const fuzzMutants = 200
const fuzzSeed = 0x5EED5EED5EED5EED

// fuzzPrelude loads one mutant through the emitted load runtime, saves it
// through the emitted save runtime, and prints the loaded flag, the six
// counters, the malformed and refused flags, and the saved bytes' hex.
const fuzzPrelude = `local inputs = dofile("inputs.nut", true);
local hexof = function(b) {
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%02x", b[i] & 0xff);
  return s;
};
local run = function(unit, mod, root, key) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local L = schema.require("schema_table_load");
  local S = schema.require("schema_table_save");
  local report = {
    unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
    duplicate = 0, malformed = false, refused = false
  };
  // The Go engine always resaves the instance state it is left with, and a
  // throw must never kill the run: the emitted load/save can only report.
  // A read past the buffer is framing damage (docs/SPEC-TABLES.md §3, §4),
  // so a throw is malformed, and the instance is then the empty one the save
  // draws from, exactly as the engine resaves its fresh instance.
  local v = null;
  local b = null;
  try {
    v = L.load(T, root, inputs[key], report);
  } catch (e) {
    report.malformed = true;
    v = null;
  }
  if (v == null) v = T.Descriptors[root].make();
  try {
    b = S.save(T, root, v);
  } catch (e) {
    report.malformed = true;
    b = null;
  }
  local loaded = (report.refused || report.malformed) ? 0 : 1;
  local h = b == null ? "-" : hexof(b);
  print(key + " " + loaded + " " + report.unknown.tostring() + "," +
        report.kindMismatch.tostring() + "," + report.widened.tostring() + "," +
        report.clamped.tostring() + "," + report.duplicate.tostring() + " " +
        (report.malformed ? "1" : "0") + " " + (report.refused ? "1" : "0") +
        " " + h + "\n");
};
`

type fuzzRow struct {
	loaded     int
	unknown    int
	kindMismatch int
	widened    int
	clamped    int
	duplicate  int
	malformed  int
	refused    int
	savedHex   string
}

func parseFuzzLines(t *testing.T, out []byte) map[string]fuzzRow {
	t.Helper()
	rows := map[string]fuzzRow{}
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) != 6 {
			t.Fatalf("fuzz line %q is not key loaded u,k,w,c,d malformed refused hex", line)
		}
		var r fuzzRow
		fmt.Sscanf(f[1], "%d", &r.loaded)
		fmt.Sscanf(f[2], "%d,%d,%d,%d,%d", &r.unknown, &r.kindMismatch, &r.widened, &r.clamped, &r.duplicate)
		fmt.Sscanf(f[3], "%d", &r.malformed)
		fmt.Sscanf(f[4], "%d", &r.refused)
		if f[5] != "-" {
			r.savedHex = f[5]
		}
		rows[f[0]] = r
	}
	return rows
}

// engineRow runs the Go engine on a mutant and returns the same shape.
func engineRow(t *testing.T, m *tabletext.Model, root string, wire []byte) fuzzRow {
	t.Helper()
	var r fuzzRow
	inst := m.New(m.Lookup(root))
	var report tabletext.Report
	loaded, err := tablewire.Decode(m, inst, wire, &report)
	if err != nil {
		r.loaded = 0
	} else if loaded {
		r.loaded = 1
	}
	r.unknown = report.Unknown
	r.kindMismatch = report.KindMismatch
	r.widened = report.Widened
	r.clamped = report.Clamped
	r.duplicate = report.Duplicate
	if report.Malformed {
		r.malformed = 1
	}
	if report.Refused {
		r.refused = 1
	}
	// resave: text round-trip through the engine (write JSON, pack to wire).
	dir := t.TempDir()
	if txt, werr := m.Write(inst); werr == nil && len(txt) > 0 {
		if err := os.WriteFile(filepath.Join(dir, root+".json"), txt, 0o600); err == nil {
			if wire, _, _, perr := tablepack.Pack(m, root, dir); perr == nil {
				r.savedHex = hex.EncodeToString(wire)
			}
		}
	}
	return r
}

func rowsEqual(a, b fuzzRow) bool {
	return a.loaded == b.loaded && a.unknown == b.unknown &&
		a.kindMismatch == b.kindMismatch && a.widened == b.widened &&
		a.clamped == b.clamped && a.duplicate == b.duplicate &&
		a.malformed == b.malformed && a.refused == b.refused &&
		a.savedHex == b.savedHex
}

// runWireFuzzDifferential builds the tblv1 unit, mutates every instance wire
// fuzzMutants times with splitmix64, runs the emitted load and save under the
// pinned interpreter over all mutants in one process, and compares each
// mutant's loaded flag, six counters, malformed and refused flags, and resave
// with the Go engine. edit patches the emitted runtime files before the run.
func runWireFuzzDifferential(t *testing.T, edit func(map[string][]byte)) ([]byte, error) {
	t.Helper()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	units := manifestUnits(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	var paths []string
	for _, u := range units {
		if u.key == fuzzUnit {
			paths = u.paths
		}
	}
	if paths == nil {
		t.Fatalf("the manifest declares no %s unit", fuzzUnit)
	}
	unit := loadUnit(t, root, paths)
	files, err := squirrel.Generate(unit)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(unit)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	if _, ok := tables["schema_table_load.nut"]; !ok {
		t.Fatalf("the emitted runtime carries no schema_table_load.nut")
	}
	if _, ok := tables["schema_table_save.nut"]; !ok {
		t.Fatalf("the emitted runtime carries no schema_table_save.nut")
	}
	if edit != nil {
		edit(tables)
	}

	dir := t.TempDir()
	out := filepath.Join(dir, fuzzUnit)
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}
	mod := squirreltable.ModuleName(unit)

	instances := readInstanceRows(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	var instWires [][]byte
	for _, in := range instances {
		if in.unit == fuzzUnit && in.root == fuzzRoot {
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(in.wire)))
			if err != nil {
				t.Fatal(err)
			}
			instWires = append(instWires, data)
		}
	}
	if len(instWires) == 0 {
		t.Fatalf("no %s instance wires in the manifest", fuzzUnit)
	}
	rng := &splitmix64{state: fuzzSeed}
	var p strings.Builder
	p.WriteString(fuzzPrelude)
	inputs := map[string][]byte{}
	var keys []string
	mut := 0
	for _, base := range instWires {
		for k := 0; k < fuzzMutants; k++ {
			key := fmt.Sprintf("m%d", mut)
			inputs[key] = mutateWire(rng, base)
			fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", fuzzUnit, mod, fuzzRoot, key)
			keys = append(keys, key)
			mut++
		}
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	leg := parseFuzzLines(t, sqrun.Run(t, dir, "main.nut", inputs))

	m := tabletext.NewModel(unit)
	var divergences []string
	for _, key := range keys {
		legRow, ok := leg[key]
		if !ok {
			return nil, fmt.Errorf("mutant %s: no leg row", key)
		}
		engRow := engineRow(t, m, fuzzRoot, inputs[key])
		if !rowsEqual(legRow, engRow) {
			divergences = append(divergences, fmt.Sprintf(
				"mutant %s: leg{loaded=%d u=%d,k=%d,w=%d,c=%d,d=%d m=%d r=%d save=%s} engine{loaded=%d u=%d,k=%d,w=%d,c=%d,d=%d m=%d r=%d save=%s}",
				key, legRow.loaded, legRow.unknown, legRow.kindMismatch, legRow.widened,
				legRow.clamped, legRow.duplicate, legRow.malformed, legRow.refused, legRow.savedHex,
				engRow.loaded, engRow.unknown, engRow.kindMismatch, engRow.widened,
				engRow.clamped, engRow.duplicate, engRow.malformed, engRow.refused, engRow.savedHex))
		}
	}
	if len(divergences) > 0 {
		return nil, fmt.Errorf("seed %d, unit %s, root %s: %d mutant(s) diverge; first:\n%s", fuzzSeed, fuzzUnit, fuzzRoot, len(divergences), divergences[0])
	}
	return nil, nil
}

// TestSquirrelWireFuzzAgreesWithTheGoEngine is the in-package differential: a
// splitmix64 generator mutates each instance wire of the corpus fuzzMutants
// times, and the Squirrel load's six counters, verdict and resave must equal
// the Go engine's (internal/tablewire) on every mutant. On a red it prints the
// seed, the unit, the root and the mutant.
func TestSquirrelWireFuzzAgreesWithTheGoEngine(t *testing.T) {
	t.Parallel()
	out, err := runWireFuzzDifferential(t, nil)
	if err != nil {
		t.Fatalf("wire fuzz differential: %v\n%s", err, out)
	}
}

// TestSquirrelWireFuzzNegativeControl patches the emitted load in memory, one
// anchor matched exactly once, so a frame length past the body is not counted
// malformed (docs/SPEC-TABLES.md §4.2, §2.9). The differential must then go
// red on the malformed flag.
func TestSquirrelWireFuzzNegativeControl(t *testing.T) {
	t.Parallel()
	// one anchor, matched exactly once: the form-byte gate that refuses a
	// non-variable wire (docs/SPEC-TABLES.md §3). Sabotaging it makes a wire
	// the engine refuses by form be read by the Squirrel side, so the
	// differential goes red on the refused flag.
	const anchor = "  if ((bytes[0] & 0xff) != FORM_VARIABLE) { report.refused = true; return null; }\n"
	_, err := runWireFuzzDifferential(t, func(files map[string][]byte) {
		runtime := squirreltable.Runtime()["schema_table_load.nut"]
		if n := strings.Count(string(runtime), anchor); n != 1 {
			t.Fatalf("the negative control's anchor appears %d times in the emitted runtime, want exactly once", n)
		}
		patched := strings.Replace(string(runtime), anchor, "  if (false) { report.refused = true; return null; }\n", 1)
		files["schema_table_load.nut"] = []byte(patched)
	})
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: a frame length past the body still counted malformed; the differential stayed green")
	}
}
