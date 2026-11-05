package jstable

// A REFUSED MESSAGE BATCH LEAVES THE CALLER'S BUFFER UNTOUCHED (docs/SPEC.md
// §5.1): SaveMessages measures the whole batch before its first byte, so a
// value its declaration does not admit (the second value's ranged integer,
// count or text length, after a lawful first value) or a buffer too short for
// the batch answers -1 and every byte of the buffer keeps the poison the
// caller put there. The plain and the integrity form are both held, over the
// emitted module under the leg's own node.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const jsMessagePoisonSchema = `package probe
fixed table Plain
{
    label string(8)
    level int32 | min = 0, max = 10
    picks [..4]int32
}
fixed table Sealed | integrity = "crc32c"
{
    label string(8)
    level int32 | min = 0, max = 10
    picks [..4]int32
}
`

const jsMessagePoisonProbe = `import { readdirSync } from "node:fs";
// the batch verbs and the fixed storage classes live in different modules, and a
// variable class of the same name lives in a third: the storage class is the
// one beside its SaveMessageBody
const mods = {}, storage = {};
for (const f of readdirSync(".")) {
  if (!f.endsWith(".js")) { continue; }
  const m = await import("./" + f);
  for (const k of Object.keys(m)) { if (k.endsWith("SaveMessages") || k.endsWith("MeasureMessages")) { mods[k] = m[k]; } }
  for (const name of ["Plain", "Sealed"]) { if (m[name + "SaveMessageBody"] !== undefined) { storage[name] = m[name]; } }
}
function poisoned(n) { return new Uint8Array(n).fill(0xA5); }
function intact(what, b) {
  const i = b.findIndex((c) => c !== 0xA5);
  if (i >= 0) { throw new Error(what + ": the refused batch wrote byte " + i + ": the poison is gone"); }
}
for (const name of ["Plain", "Sealed"]) {
  const make = () => { const v = new storage[name](); v.label.set([0x6f, 0x6b]); v.labelLength = 2; v.level = 3; v.picksCount = 1; return v; };
  const save = mods[name + "SaveMessages"], measure = mods[name + "MeasureMessages"];
  for (const [what, set] of [["level", (v) => { v.level = 11; }], ["picks", (v) => { v.picksCount = 5; }], ["label", (v) => { v.labelLength = 9; }]]) {
    const bad = make();
    set(bad);
    const b = poisoned(4096), report = {};
    const n = save([make(), bad], b, report);
    if (n !== -1 || report.Reason !== "invalid_value") { throw new Error(name + " " + what + ": SaveMessages answered " + n + " (" + report.Reason + "), wants -1 invalid_value"); }
    intact(name + " " + what, b);
  }
  const values = [make(), make()];
  const need = measure(values, {});
  if (!(need > 0)) { throw new Error(name + ": measure " + need); }
  const short = poisoned(need - 1);
  if (save(values, short, {}) !== -1) { throw new Error(name + ": a short buffer was not refused"); }
  intact(name + " short", short);
  const n = save(values, poisoned(need), {});
  if (n !== need) { throw new Error(name + ": an exact buffer answered " + n + ", wants " + need); }
}
console.log("ok");
`

func jsRunMessagePoison(t *testing.T, edit func(map[string][]byte)) ([]byte, error) {
	t.Helper()
	node := jsNode(t)
	files := generatedTree(jsUnitOf(t, jsMessagePoisonSchema))
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(jsMessagePoisonProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, filepath.Join(dir, "probe.mjs"))
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

func TestJSMessageRefusalLeavesTheBufferUntouched(t *testing.T) {
	t.Parallel()
	if out, err := jsRunMessagePoison(t, nil); err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("message poison probe: %v\n%s", err, out)
	}
}

// TestJSMessageRefusalNegativeControl removes the measure that runs before the
// first byte, in the emitted module: the save then writes the form and count
// bytes and the lawful first value before the second value refuses, and the
// probe must go red on the poison.
func TestJSMessageRefusalNegativeControl(t *testing.T) {
	t.Parallel()
	out, err := jsRunMessagePoison(t, func(files map[string][]byte) {
		const old = "if (need < 0 || buffer.length < need) { return -1; }"
		n := 0
		for name, data := range files {
			if strings.Contains(string(data), old) {
				files[name] = []byte(strings.ReplaceAll(string(data), old, "if (false && (need < 0 || buffer.length < need)) { return -1; } /* SABOTAGED */"))
				n++
			}
		}
		if n == 0 {
			t.Fatal("the negative control found no measure-first guard to remove")
		}
	})
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: SaveMessages no longer measures first and the probe stayed GREEN\n%s", out)
	}
	if !strings.Contains(string(out), "the poison is gone") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the probe went red, but not on the poison\n%s", out)
	}
}
