package squirreltable_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

// THE COMPRESSED -0 WRITER CORPUS ON THE SQUIRREL LEG (docs/SPEC-TABLES.md
// §3.3, docs/rulings.json fixed-compressed-negzero): a writer stores +0 for a
// compressed float's -0. The corpus is the Go reference engine's
// (testdata/conformance/feature-witnesses/compressed-negzero-go, pinned by
// internal/tablewire's TestCompressedNegZeroGoCorpus); this leg runs every row
// through the generated Squirrel unit: load <base>.json with the Squirrel text
// read, set <path> to -0 in the native value, save <form>, and owe
// <expect>.<form>.bin byte for byte.
//
// Beside each row, every other Squirrel writer the row's root has is held to
// the row's +0 twin (the same base saved untouched): the JSON writer and the
// cook writer of either root, the variable save and the retaining save of a
// variable root, and the Save (which writes the fixed form), the fixed save
// and the message form of a fixed table. A compressed row's -0
// output equals its twin's; the plain `p` rows are the control, whose -0 rides
// bit-exact, so their output differs from the twin's.
//
// It runs the pinned interpreter, so it is a slow test; the conformance lane
// runs it (make/squirrel-compressed-negzero.mk).
// TestSquirrelCompressedFloatStoresAreGuarded is its fast twin.

const sqNegZeroCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-negzero-go"

// sqNegZeroNoWriter names the corpus rows whose <form> writer the Squirrel
// surface does not have, and why. The row's root still answers every twin
// its writers owe.
var sqNegZeroNoWriter = map[string]string{
	"var_fixed_table_leaf": "the Squirrel Save of a fixed table writes the fixed form (save.nut save calls saveFixed): the leg has no variable-form writer for a fixed root",
}

type sqNegZeroRow struct {
	name, form, root, base, path, expect string
}

func sqNegZeroRows(t *testing.T) []sqNegZeroRow {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(sqNegZeroCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []sqNegZeroRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("cases.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		rows = append(rows, sqNegZeroRow{f[0], f[1], f[2], f[3], f[4], f[5]})
	}
	return rows
}

const sqNegZeroProbe = `
local I = dofile("inputs.nut", true);
local T = dofile("probe/schema_module.nut", true).require(%q);
local NZ = -0.0;
if (!(NZ == 0 && 1.0 / NZ < 0)) throw "the probe's -0 is not -0";

local hex = function(b, n) {
  if (b == null) return "nil";
  local s = "";
  for (local i = 0; i < n; i++) s += format("%%02x", b[i] & 0xff);
  return s;
};
local texthex = function(s) {
  if (s == null) return "nil";
  local out = "";
  foreach (c in s) out += format("%%02x", c & 0xff);
  return out;
};

// set writes -0 into the cell a corpus path names: field, field[index],
// field[Variant] for an enum-keyed slot, or field.arm for a union's set arm
local set = function(root, v, path) {
  local name = path, rest = "";
  foreach (i, c in path) {
    if (c == '[' || c == '.') { name = path.slice(0, i); rest = path.slice(i); break; }
  }
  local fd = null;
  foreach (f in T.Descriptors[root].fields) if (f.name == name) fd = f;
  if (fd == null) throw "no field " + name;
  if (rest == "") { v[fd.member] = NZ; return; }
  if (rest[0] == '.') {
    local arm = rest.slice(1);
    foreach (a in T[fd.table + "Descriptor"].arms) {
      if (a.name == arm) {
        if (v[fd.member].Type != a.tag) throw "the base does not set arm " + arm;
        v[fd.member][a.member] = NZ;
        return;
      }
    }
    throw "no arm " + arm;
  }
  local index = rest.slice(1, rest.len() - 1);
  local slot = null;
  if (index[0] >= '0' && index[0] <= '9') slot = index.tointeger();
  else if (fd.keyvariants != null) foreach (i, kv in fd.keyvariants) if (kv[0] == index) slot = i;
  if (slot == null) throw "no slot " + index;
  v[fd.member][slot] = NZ;
};

local load = function(root, stem) {
  local r = {};
  local v = T[root + "FromJson"](I[stem + ".json"], r);
  if (v == null) throw stem + ".json: the Squirrel text read refused it";
  return v;
};

local rep = function() { return { unknown = 0, mismatch = 0, widened = 0, oor = 0, repeated = 0, bodyDamaged = false, refused = false }; };

// each writer the root has, over a native value
local writers = function(root, stem) {
  local out = [
    ["json", function(v) { return texthex(T[root + "ToJson"](v)); }],
    ["cook", function(v) {
      local n = T.CookMeasure(root, v);
      if (n < 0) return null;
      local b = blob(n);
      return T.Cook(root, v, b, 1) ? hex(b, n) : null;
    }],
  ];
  if (T.Descriptors[root].fixed) {
    // <Root>Save of a fixed table writes the fixed form (save.nut saveFixed),
    // so it is the "fixed" writer's twin, not a variable writer
    out.append(["save", function(v) { local b = T[root + "Save"](v); return b == null ? null : hex(b, b.len()); }]);
    out.append(["fixed", function(v) {
      local b = blob(T[root + "FixedMeasure"](1));
      return T[root + "FixedSave"](b, [v]) < 0 ? null : hex(b, b.len());
    }]);
    out.append(["message", function(v) {
      local b = blob(4096);
      local n = T[root + "SaveMessages"]([v], b, {});
      return n < 0 ? null : hex(b, n);
    }]);
  } else {
    out.append(["variable", function(v) { local b = T[root + "Save"](v); return b == null ? null : hex(b, b.len()); }]);
    out.append(["retain", function(v) {
      // the retaining save over the store its load kept (§6.6)
      local store = { capacity = 65536, idCapacity = 1024 };
      local r = rep();
      if (T[root + "LoadRetain"](I[stem + ".variable.bin"], store, r) == null) throw stem + ": the retaining load refused";
      local m = T[root + "MeasureRetain"](v, store);
      if (m < 0) return null;
      local b = blob(m);
      local n = T[root + "SaveRetain"](v, store, b, r);
      return n < 0 ? null : hex(b, n);
    }]);
  }
  return out;
};

foreach (row in ROWS) {
  local name = row[0], root = row[2], stem = row[3], path = row[4];
  foreach (w in writers(root, stem)) {
    local plus = load(root, stem);
    local minus = load(root, stem);
    set(root, minus, path);
    local bm = null, bp = null;
    try { bm = w[1](minus); } catch (e) { bm = "error"; }
    try { bp = w[1](plus); } catch (e) { bp = "error"; }
    print(name + " " + w[0] + " " + (bm == null ? "nil" : bm) + " " + (bp == null ? "nil" : bp) + "\n");
  }
}
`

// TestSquirrelCompressedNegZeroCorpus runs the corpus on the Squirrel leg.
func TestSquirrelCompressedNegZeroCorpus(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	paths, err := compiler.GatherPaths([]string{filepath.Join(sqNegZeroCorpusDir, "Probe.schema")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mod := emitUnit(t, dir, "probe", u)

	rows := sqNegZeroRows(t)
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(sqNegZeroCorpusDir, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	inputs := map[string][]byte{}
	var main strings.Builder
	main.WriteString("local ROWS = [\n")
	for _, r := range rows {
		inputs[r.base+".json"] = read(r.base + ".json")
		if r.root == "Var" {
			inputs[r.base+".variable.bin"] = read(r.base + ".variable.bin")
		}
		fmt.Fprintf(&main, "  [%q, %q, %q, %q, %q],\n", r.name, r.form, r.root, r.base, r.path)
	}
	main.WriteString("];\n")
	fmt.Fprintf(&main, sqNegZeroProbe, mod)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(main.String()))
	out := sqrun.Run(t, dir, "main.nut", inputs)

	type pair struct{ minus, plus string }
	got := map[string]map[string]pair{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) != 4 {
			t.Fatalf("the probe printed %q, not a row, a writer and two outputs", line)
		}
		if got[f[0]] == nil {
			got[f[0]] = map[string]pair{}
		}
		got[f[0]][f[1]] = pair{f[2], f[3]}
	}

	held, twins, exempt := 0, 0, 0
	for _, r := range rows {
		ws := got[r.name]
		if ws == nil {
			t.Errorf("%s: the probe did not answer the row", r.name)
			continue
		}
		plain := r.path == "p"
		// THE CORPUS ROW: the -0 save of <form> is <expect>.<form>.bin
		want := hex.EncodeToString(read(r.expect + "." + r.form + ".bin"))
		if why, ok := sqNegZeroNoWriter[r.name]; ok {
			if _, has := ws[r.form]; has {
				t.Errorf("%s: the Squirrel leg now has a %s writer for %s; drop the exemption (%s)", r.name, r.form, r.root, why)
			}
			t.Logf("%s: not carried on the Squirrel leg: %s", r.name, why)
			exempt++
		} else if p, ok := ws[r.form]; !ok {
			t.Errorf("%s: the Squirrel leg has no %s writer for %s", r.name, r.form, r.root)
		} else if p.minus != want {
			t.Errorf("%s: the Squirrel %s save of -0 at %s is not %s.%s.bin:\n have %s\n want %s", r.name, r.form, r.path, r.expect, r.form, p.minus, want)
		} else {
			held++
		}
		// THE TWINS: every writer the root has
		for w, p := range ws {
			if p.minus == "error" || p.plus == "error" || p.minus == "nil" || p.plus == "nil" {
				t.Errorf("%s/%s: a writer refused: -0 %s, +0 %s", r.name, w, p.minus, p.plus)
				continue
			}
			switch {
			case plain && p.minus == p.plus:
				t.Errorf("%s/%s: a plain float's -0 did not ride: %s", r.name, w, p.minus)
			case !plain && p.minus != p.plus:
				t.Errorf("%s/%s: a compressed float's -0 was not written as +0:\n -0 %s\n +0 %s", r.name, w, p.minus, p.plus)
			default:
				twins++
			}
		}
	}
	if len(rows) != 15 {
		t.Errorf("cases.tsv has %d rows, want 15", len(rows))
	}
	if held+exempt != len(rows) || exempt != len(sqNegZeroNoWriter) {
		t.Errorf("%d rows held and %d exempt of %d; %d exemptions named", held, exempt, len(rows), len(sqNegZeroNoWriter))
	}
	t.Logf("%d of %d corpus rows held (%d not carried, by name); %d writer twins held", held, len(rows), exempt, twins)
}
