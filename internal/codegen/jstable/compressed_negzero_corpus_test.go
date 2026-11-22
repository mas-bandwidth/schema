package jstable

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// THE COMPRESSED -0 WRITER CORPUS ON THE JAVASCRIPT LEG (docs/SPEC-TABLES.md
// §3.3, docs/rulings.json fixed-compressed-negzero): a writer stores +0 for a
// compressed float's -0. The corpus is the Go reference engine's
// (testdata/conformance/feature-witnesses/compressed-negzero-go, pinned by
// internal/tablewire's TestCompressedNegZeroGoCorpus); this leg runs every row
// through the generated JavaScript unit: load <base>, set <path> to -0 in the
// native value, save <form>, and owe <expect>.<form>.bin byte for byte.
//
// Beside each row, every other JavaScript writer the row's root has is held to
// the row's +0 twin (the same base saved untouched): the variable save, the
// retaining save, the JSON writer and the cook writer over the variable class
// (loaded from <base>.json by the JSON read), and the fixed save and the
// message form over a fixed table's storage class (loaded from
// <base>.fixed.bin by the fixed read). A compressed row's -0 output equals its
// twin's; the plain `p` rows are the control, whose -0 rides bit-exact, so
// their output differs from the twin's.
//
// It runs node, so it is a slow test; the conformance lane runs it
// (make/js-compressed-negzero.mk). TestJsCompressedFloatStoresAreGuarded is its
// fast twin.

const jsNegZeroCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-negzero-go"

type jsNegZeroRow struct {
	name, form, root, base, path, expect string
}

func jsNegZeroRows(t *testing.T) []jsNegZeroRow {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(jsNegZeroCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []jsNegZeroRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("cases.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		rows = append(rows, jsNegZeroRow{f[0], f[1], f[2], f[3], f[4], f[5]})
	}
	return rows
}

const jsNegZeroProbe = `
import { readFileSync } from "node:fs";
import * as V from "./NzgoVariable.js";
import * as N from "./NzgoTable.js";
import * as T from "./ProbeTable.js";

const NZ = -0;
if (!Object.is(NZ, -0)) { throw new Error("the probe's -0 is not -0"); }
const read = (name) => new Uint8Array(readFileSync(name));
const hex = (b) => {
  if (b === null || b === undefined) { return "nil"; }
  if (typeof b === "string") { b = new TextEncoder().encode(b); }
  return Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
};
const take = (n, b) => (n < 0 ? null : b.subarray(0, n));

// set writes -0 into the cell a corpus path names: field, field[index],
// field[Variant] for an enum-keyed slot, or field.arm for a union's set arm.
// fixed is true for a fixed table's storage class, whose union is {type, arm
// members} and whose counted array carries a count beside it.
const LEVEL = { Low: 0, High: 1 };
function set(v, path, fixed) {
  let m = path.match(/^(\w+)\.(\w+)$/);
  if (m) {
    const u = v[m[1]];
    if (fixed) {
      if (u.type !== 1 || m[2] !== "v") { throw new Error("the base does not set arm " + m[2]); }
      u[m[2]] = NZ;
    } else {
      if (u === null || u.arm !== m[2]) { throw new Error("the base does not set arm " + m[2]); }
      u.value = NZ;
    }
    return;
  }
  m = path.match(/^(\w+)\[(\w+)\]$/);
  if (m) {
    const slot = /^\d+$/.test(m[2]) ? Number(m[2]) : LEVEL[m[2]];
    if (slot === undefined || v[m[1]].length <= slot) { throw new Error("no slot " + m[2]); }
    v[m[1]][slot] = NZ;
    return;
  }
  if (!(path in v)) { throw new Error("no field " + path); }
  v[path] = NZ;
}

function loadVariable(root, base) {
  const r = new V.TableVariableReport();
  const v = V[root + "FromJson"](readFileSync(base + ".json", "utf8"), r);
  if (v === null) { throw new Error(base + ".json: the JavaScript text read refused it: " + JSON.stringify(r)); }
  return v;
}
function loadFixed(root, base) {
  const data = read(base + ".fixed.bin");
  const out = [new N[root]()];
  const r = new N.TableFixedReport();
  if (T[root + "FixedLoad"](out, 1, data, data.length, T[root + "FixedNewPlan"](4096, 4096), r) !== 1) {
    throw new Error(base + ".fixed.bin: the fixed read refused it");
  }
  return out[0];
}

// each writer the root has: [name, load, write]
function writers(root, base) {
  const out = [
    ["json", loadVariable, (v) => V[root + "ToJson"](v)],
    ["cook", loadVariable, (v) => {
      const n = V[root + "CookMeasure"](v);
      if (n < 0) { return null; }
      const buf = new Uint8Array(n);
      return V[root + "CookFrom"](v, buf, 1) === false ? null : buf;
    }],
    ["variable", loadVariable, (v) => V[root + "Save"](v)],
    ["retain", loadVariable, (v) => {
      // the retaining save over the store its load kept (§6.6)
      const store = new V.TableVariableRetain(new Uint8Array(1 << 16), new Array(1 << 10));
      const r = new V.TableVariableReport();
      if (V[root + "LoadRetain"](read(base + ".variable.bin"), store, r) === null) { throw new Error(base + ": the retaining load refused"); }
      return V[root + "SaveRetain"](v, store, r);
    }],
  ];
  if (root === "Fix") {
    out.push(["fixed", loadFixed, (v) => {
      const buf = new Uint8Array(T.FixFixedMeasure(1));
      return take(T.FixFixedSave([v], 1, buf), buf);
    }]);
    out.push(["message", loadFixed, (v) => {
      const n = T.FixMeasureMessages([v], {});
      if (n < 0) { return null; }
      const buf = new Uint8Array(n);
      return take(T.FixSaveMessages([v], buf, {}), buf);
    }]);
  }
  return out;
}

for (const [name, form, root, base, path] of ROWS) {
  for (const [w, load, write] of writers(root, base)) {
    const plus = load(root, base);
    const minus = load(root, base);
    set(minus, path, load === loadFixed);
    let bp, bm;
    try { bp = hex(write(plus)); } catch (e) { bp = "error"; }
    try { bm = hex(write(minus)); } catch (e) { bm = "error"; }
    console.log(name + " " + w + " " + bm + " " + bp);
  }
}
`

// TestJsCompressedNegZeroCorpus runs the corpus on the JavaScript leg.
func TestJsCompressedNegZeroCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node (it runs the generated writers over the corpus)")
	node := jsNode(t)
	schemaText, err := os.ReadFile(filepath.Join(jsNegZeroCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files := generatedTree(unitFrom(t, string(schemaText)))
	maps.DeleteFunc(files, func(name string, _ []byte) bool { return strings.Contains(name, "/") })

	rows := jsNegZeroRows(t)
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(jsNegZeroCorpusDir, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	var probe strings.Builder
	probe.WriteString("const ROWS = [\n")
	for _, r := range rows {
		files[r.base+".json"] = read(r.base + ".json")
		files[r.base+".variable.bin"] = read(r.base + ".variable.bin")
		if r.root == "Fix" {
			files[r.base+".fixed.bin"] = read(r.base + ".fixed.bin")
		}
		fmt.Fprintf(&probe, "  [%q, %q, %q, %q, %q],\n", r.name, r.form, r.root, r.base, r.path)
	}
	probe.WriteString("];\n")
	probe.WriteString(jsNegZeroProbe)
	// an ES module hoists its imports, so ROWS may sit above them
	files["probe.mjs"] = []byte(probe.String())

	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := testproc.Command(t, node, filepath.Join(dir, "probe.mjs"))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the probe died: %v\n%s", err, out)
	}

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

	held, twins := 0, 0
	for _, r := range rows {
		ws := got[r.name]
		if ws == nil {
			t.Errorf("%s: the probe did not answer the row", r.name)
			continue
		}
		plain := r.path == "p"
		// THE CORPUS ROW: the -0 save of <form> is <expect>.<form>.bin
		want := hex.EncodeToString(read(r.expect + "." + r.form + ".bin"))
		if p, ok := ws[r.form]; !ok {
			t.Errorf("%s: the JavaScript leg has no %s writer for %s", r.name, r.form, r.root)
		} else if p.minus != want {
			t.Errorf("%s: the JavaScript %s save of -0 at %s is not %s.%s.bin:\n have %s\n want %s", r.name, r.form, r.path, r.expect, r.form, p.minus, want)
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
	if held != len(rows) {
		t.Errorf("%d rows held of %d", held, len(rows))
	}
	t.Logf("%d of %d corpus rows held; %d writer twins held", held, len(rows), twins)
}
