package jstable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// A COMPRESSED FLOAT'S RANGE IS HELD AT FLOAT32 (docs/SPEC-TABLES.md §3.3),
// on the JavaScript leg, run on the cross-port corpus
// testdata/conformance/feature-witnesses/compressed-f32-ends: every row of its
// cases.tsv runs through the generated JavaScript under the leg's node.
// internal/tablewire's TestCompressedF32EndsCorpus is the reference runner;
// the row logic is its.
//
// The fixed root's JSON is read through the variable library's FFromJson and
// carried into the fixed library's F field by field, so the fixed save is the
// fixed writer's.
const jsF32EndsDir = "../../../testdata/conformance/feature-witnesses/compressed-f32-ends"

const jsF32EndsDriver = `import { readFileSync } from "node:fs";
import * as v from "./F32endsVariable.js";
import * as t from "./F32endsTable.js";
import * as p from "./ProbeTable.js";

const dir = process.argv[2];
let reason = "";

const file = (name) => new Uint8Array(readFileSync(dir + "/" + name));
const hex = (b) => b === null || b === undefined ? "null" : Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");

function same(got, want) {
  const w = file(want);
  return hex(got) === hex(w) ? "" : "saved " + hex(got) + ", want " + want + " " + hex(w);
}

function saveFixed(value) {
  const out = new Uint8Array(p.FFixedMeasure(1));
  const n = p.FFixedSave([value], 1, out);
  return n < 0 ? null : out.subarray(0, n);
}

function carry(from) {
  const out = new t.F();
  out.r = from.r;
  for (let i = 0; i < out.xs.length && i < from.xs.length; i++) { out.xs[i] = from.xs[i]; }
  return out;
}

function jsonOf(read, stem) {
  const r = new v.TableVariableReport();
  const got = read(readFileSync(dir + "/" + stem + ".json", "utf8"), r);
  const e = r.err();
  reason = String(e);
  return got === null || got === undefined || e !== null ? null : got;
}

const jsonV = (stem) => jsonOf(v.VFromJson, stem);
function jsonF(stem) {
  const got = jsonOf(v.FFromJson, stem);
  return got === null ? null : carry(got);
}

function readVariable(stem) {
  const r = new v.TableVariableReport();
  const got = v.VLoad(file(stem + ".variable.bin"), r);
  const e = r.err();
  reason = String(e);
  return got === null || got === undefined || e !== null ? null : got;
}

function readFixed(stem) {
  const data = file(stem + ".fixed.bin");
  const value = new t.F();
  const r = new t.TableFixedReport();
  const n = p.FFixedLoad([value], 1, data, data.length, p.FFixedNewPlan(4096, 4096), r);
  reason = r.refused === t.TableFixedRefusal.OutOfRange ? "out_of_range" : "fixed refusal " + r.refused;
  return n === 1 && !r.refused && !r.damaged ? value : null;
}

function read(form, root, input, expect) {
  let saved = null;
  let got;
  if (root === "V") {
    const value = form === "json" ? jsonV(input) : readVariable(input);
    got = value !== null;
    if (got && expect !== "refused") { saved = v.VSave(value); }
  } else {
    const value = form === "json" ? jsonF(input) : readFixed(input);
    got = value !== null;
    if (got && expect !== "refused") { saved = saveFixed(value); }
  }
  if (expect === "refused") {
    if (got) { return "the " + form + " read was not refused"; }
    return reason.startsWith("out_of_range") ? "" : "refused, but not out_of_range: " + reason;
  }
  if (!got) { return "the " + form + " read refused the value at the float32 end"; }
  if (saved === null || saved === undefined) { return "saving again what was just read was refused"; }
  return same(saved, expect + "." + (root === "V" ? "variable" : "fixed") + ".bin");
}

const index = (path) => Number(path.slice(path.indexOf("[") + 1, path.indexOf("]")));

function write(form, root, input, path, bits, expect) {
  const s = new DataView(new ArrayBuffer(4));
  s.setUint32(0, parseInt(bits, 16));
  const x = s.getFloat32(0);
  let saved = null;
  try {
    const value = form === "variable" ? jsonV(input) : jsonF(input);
    if (value === null) { return input + ".json is refused"; }
    if (path === "r") { value.r = x; } else { value.xs[index(path)] = x; }
    saved = form === "variable" ? v.VSave(value) : saveFixed(value);
  } catch (e) {
    saved = null;
  }
  if (saved === undefined) { saved = null; }
  if (expect === "refused") { return saved === null ? "" : "the " + form + " save was not refused: " + hex(saved); }
  if (saved === null) { return "the " + form + " save was refused"; }
  return same(saved, expect + "." + form + ".bin");
}

for (const line of readFileSync(dir + "/cases.tsv", "utf8").split("\n")) {
  if (line === "" || line.startsWith("#")) { continue; }
  const c = line.split("\t");
  let why;
  try {
    why = c[1] === "read" ? read(c[2], c[3], c[4], c[7]) : write(c[2], c[3], c[4], c[5], c[6], c[7]);
  } catch (e) {
    why = "threw " + e;
  }
  console.log("CHECK " + c[0] + (why === "" ? " ok" : " FAIL " + why));
}
`

// TestJavaScriptCompressedF32EndsCorpus runs every row of the cross-port
// corpus under node (gated: jsNode).
func TestJavaScriptCompressedF32EndsCorpus(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	src, err := os.ReadFile(filepath.Join(jsF32EndsDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(src))
	files := generatedTree(u)
	for _, name := range []string{"F32endsVariable.js", "F32endsTable.js", "ProbeTable.js"} {
		if files[name] == nil {
			t.Fatalf("the generator wrote no %s", name)
		}
	}
	corpus, err := filepath.Abs(jsF32EndsDir)
	if err != nil {
		t.Fatal(err)
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
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f32ends_driver.mjs"), []byte(jsF32EndsDriver), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := testproc.Command(t, node, "f32ends_driver.mjs", corpus)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	text, err := os.ReadFile(filepath.Join(jsF32EndsDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for line := range strings.SplitSeq(string(out), "\n") {
		rest, ok := strings.CutPrefix(line, "CHECK ")
		if !ok {
			continue
		}
		label, verdict, _ := strings.Cut(rest, " ")
		seen[label] = true
		if verdict != "ok" {
			t.Errorf("%s: %s", label, strings.TrimPrefix(verdict, "FAIL "))
		}
	}
	for line := range strings.SplitSeq(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if name, _, _ := strings.Cut(line, "\t"); !seen[name] {
			t.Errorf("the driver made no %s check\n%s", name, out)
		}
	}
}
