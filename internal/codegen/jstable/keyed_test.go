package jstable

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestKeyedArrayAccessorsJavaScript holds docs/PORTING.md M5 on every
// JavaScript form that stores a keyed array [E]T (docs/SPEC-TABLES.md §2.4):
// the variable class's storage, the fixed class's storage and the block row.
// Key k (E's variant value, 1..E.Max) lives at slot k-1; the accessors take the
// KEY; None (0), a key past E.Max, a fraction and a non-number are refused with
// a RangeError that names the field, on read and on write, and the array never
// grows; the entries yield each key beside its element in key order.
//
//   - variable class (Holder): Holder.leavesAt / setLeavesAt / leavesEntries
//     over a table element and Holder.ranksAt over an enum element; a value
//     written by key saves the bytes of the same value written by slot.
//   - fixed class (Config): Config.scoresAt over int32, Config.turretsAt over
//     a fixed table; the same save comparison through the fixed form.
//   - block row (ConfigBlock): ScoresByKey(block, k) reads Scores(block, k-1),
//     TurretsAtKey(k) is TurretsAt(k-1), both ends refused.
//
// THREE REVERSED WITNESSES plant the shapes M5 names back into every guard and
// shift the generator wrote: the None-only guard (a key past Max reads past the
// array), the Max-only guard (None reads the slot before 0), and the missing
// shift (key k reads slot k). Each must turn the probe red with a named
// failure.
func TestKeyedArrayAccessorsJavaScript(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node (it runs the generated modules)")
	node := jsNode(t)
	work := t.TempDir()

	u := unitFrom(t, keyedProbeSrc)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	const guard = "key < 1 || key > 3)"
	total := 0
	for _, data := range files {
		total += bytes.Count(data, []byte(guard))
	}
	// Two guards (read and write) per keyed field per storage class: Holder's
	// two fields in the variable module, Config's two in the fixed module and
	// again in the variable module (a fixed-class table carries a form-1 codec
	// and class there too), and one per keyed accessor of the block row.
	if total != 14 {
		t.Fatalf("the generated unit carries %d keyed guards, want 14: the witnesses below would not plant what they claim", total)
	}

	run := func(name string, edit func([]byte) []byte) string {
		t.Helper()
		dir := filepath.Join(work, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		for file, data := range files {
			if err := os.WriteFile(filepath.Join(dir, file), edit(data), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		script := filepath.Join(dir, "probe.mjs")
		if err := os.WriteFile(script, []byte(keyedDriver), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(node, script)
		cmd.Dir = dir
		out, _ := cmd.CombinedOutput()
		return string(out)
	}

	out := run("keyed", func(b []byte) []byte { return b })
	t.Logf("probe output:\n%s", out)
	if !strings.Contains(out, "KEYED OK") {
		t.Errorf("the keyed accessors do not hold M5:\n%s", out)
	}

	witnesses := []struct {
		name string
		edit func([]byte) []byte
	}{
		{"none-only", func(b []byte) []byte { return bytes.ReplaceAll(b, []byte(guard), []byte("key < 1)")) }},
		{"max-only", func(b []byte) []byte { return bytes.ReplaceAll(b, []byte(guard), []byte("key > 3)")) }},
		{"no-shift", func(b []byte) []byte {
			b = bytes.ReplaceAll(b, []byte("[key - 1]"), []byte("[key]"))
			return bytes.ReplaceAll(b, []byte("(key - 1) *"), []byte("key *"))
		}},
	}
	for _, w := range witnesses {
		got := run(w.name, w.edit)
		if !strings.Contains(got, "KEYED FAIL") {
			t.Errorf("REVERSED WITNESS %s: the probe did not name a failure over the planted accessors:\n%s", w.name, got)
		}
	}
}

// keyedProbeSrc: a fixed table with a scalar and a table keyed array (fixed
// class and block row), and a variable table with a table and an enum keyed
// array (variable class: the pointer makes it variable).
const keyedProbeSrc = `package keyedprobe

enum Grade { Bronze, Silver, Gold }

fixed table Turret
{
    damage float32 = 10.0
}

fixed table Config
{
    scores  [Grade]int32
    turrets [Grade]Turret
}

table Holder
{
    leaves [Grade]Turret
    ranks  [Grade]Grade
    head   *Turret
}
`

// keyedDriver: Grade has three variants, Bronze = 1, Silver = 2, Gold = 3.
const keyedDriver = `
import * as V from "./KeyedprobeVariable.js";
import * as F from "./KeyedprobeTable.js";
import { ConfigFixedMeasure, ConfigFixedSave } from "./ProbeTable.js";
import { ConfigBlock } from "./ProbeBlock.js";

const fail = [];
const check = (ok, what) => { if (!ok) { fail.push(what); } };
const refuses = (f, what) => {
  try { f(); fail.push(what + " was accepted"); }
  catch (e) { check(e instanceof RangeError && /: key /.test(e.message), what + " threw " + e); }
};
const bad = [0, 4, -1, 1.5, NaN, "1", null, undefined, 2n];
const same = (a, b) => a !== null && b !== null && a.length === b.length && a.every((x, i) => x === b[i]);

// THE VARIABLE CLASS
const h = new V.Holder();
const t2 = new V.Turret(); t2.damage = 2;
const t3 = new V.Turret(); t3.damage = 3;
V.Holder.setLeavesAt(h, 2, t2); V.Holder.setLeavesAt(h, 3, t3);
V.Holder.setRanksAt(h, 1, "Gold");
check(h.leaves[1] === t2 && h.leaves[2] === t3 && h.ranks[0] === "Gold", "variable: key k lands at slot k-1");
check(V.Holder.leavesAt(h, 3) === t3 && V.Holder.ranksAt(h, 1) === "Gold", "variable: key k reads slot k-1");
for (const k of bad) {
  refuses(() => V.Holder.leavesAt(h, k), "variable leavesAt(" + String(k) + ")");
  refuses(() => V.Holder.setRanksAt(h, k, "Gold"), "variable setRanksAt(" + String(k) + ")");
}
check(h.leaves.length === 3 && h.ranks.length === 3, "variable: a refused write grew the array");
const ve = [...V.Holder.leavesEntries(h)].map(([k, x]) => [k, x.damage]);
check(JSON.stringify(ve) === "[[1,10],[2,2],[3,3]]", "variable: entries in key order: " + JSON.stringify(ve));
const hs = new V.Holder(); hs.leaves[1] = t2; hs.leaves[2] = t3; hs.ranks[0] = "Gold";
check(same(V.HolderSave(h), V.HolderSave(hs)), "variable: by key saves the bytes of by slot");

// THE FIXED CLASS
const c = new F.Config();
F.Config.setScoresAt(c, 1, 10); F.Config.setScoresAt(c, 2, 20); F.Config.setScoresAt(c, 3, 30);
F.Config.turretsAt(c, 2).damage = 5;
check(c.scores[0] === 10 && c.scores[1] === 20 && c.scores[2] === 30 && c.turrets[1].damage === 5, "fixed: key k at slot k-1");
check(F.Config.scoresAt(c, 3) === 30, "fixed: key k reads slot k-1");
for (const k of bad) {
  refuses(() => F.Config.scoresAt(c, k), "fixed scoresAt(" + String(k) + ")");
  refuses(() => F.Config.setTurretsAt(c, k, new F.Turret()), "fixed setTurretsAt(" + String(k) + ")");
}
check(c.scores.length === 3 && c.turrets.length === 3, "fixed: a refused write grew the array");
const fe = [...F.Config.scoresEntries(c)];
check(JSON.stringify(fe) === "[[1,10],[2,20],[3,30]]", "fixed: entries in key order: " + JSON.stringify(fe));
const cs = new F.Config(); cs.scores[0] = 10; cs.scores[1] = 20; cs.scores[2] = 30; cs.turrets[1].damage = 5;
const fixedSave = (v) => { const out = new Uint8Array(ConfigFixedMeasure(1)); ConfigFixedSave([v], 1, out); return out; };
check(same(fixedSave(c), fixedSave(cs)), "fixed: by key saves the bytes of by slot");

// THE BLOCK ROW
const bytes = new Uint8Array(ConfigBlock.MaxBytes);
const used = ConfigBlock.Begin(bytes);
for (let i = 24; i < bytes.length; i++) { bytes[i] = (i * 7) & 0xff; }
const block = ConfigBlock.Open(bytes.subarray(0, used));
check(block !== null, "block: the leg's own block did not open");
if (block !== null) {
  const seen = new Set();
  for (let k = 1; k <= 3; k++) {
    seen.add(ConfigBlock.ScoresByKey(block, k));
    check(ConfigBlock.ScoresByKey(block, k) === ConfigBlock.Scores(block, k - 1), "block: ScoresByKey(" + k + ") is Scores(" + (k - 1) + ")");
    check(ConfigBlock.TurretsAtKey(k) === ConfigBlock.TurretsAt(k - 1), "block: TurretsAtKey(" + k + ") is TurretsAt(" + (k - 1) + ")");
  }
  check(seen.size === 3, "block: the three slots read the same bytes");
  for (const k of bad) {
    refuses(() => ConfigBlock.ScoresByKey(block, k), "block ScoresByKey(" + String(k) + ")");
    refuses(() => ConfigBlock.TurretsAtKey(k), "block TurretsAtKey(" + String(k) + ")");
  }
}

console.log(fail.length === 0 ? "KEYED OK" : "KEYED FAIL\n" + fail.join("\n"));
`
