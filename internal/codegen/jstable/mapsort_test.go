package jstable

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestMapWriterSortJavaScript pins docs/PORTING.md M19's "the WRITER holds the
// sort" on the JavaScript variable form (docs/SPEC-TABLES.md §2.8, §3.2): a map
// is a JavaScript Map, which iterates in INSERTION order, so a Map the caller
// builds out of key order must still save, measure and write as text in
// ascending key order, byte for byte the Go oracle's.
//
// Every other map test reaches the writer through a load or a text read of the
// oracle's own (already sorted) bytes, so the Map it saves is in key order
// before the writer looks at it, and dropping the sort from
// TableVariableMapEntries left them all green. Here each case is loaded from
// the oracle's wire, then EVERY Map in the value, at every depth, is rebuilt in
// REVERSED insertion order before Save, Measure and ToJson run; the oracle's
// wire and text are the expected output. The probe's cases hold string keys
// (UTF-8 order, "é" after "z"), unsigned 64-bit keys at the extremes, signed
// keys, pointer values a second slot names, and a map inside a map value.
//
// THE REVERSED WITNESS generates the same module with the sort removed and
// requires the save to differ from the oracle on at least one case, so this
// test cannot pass over a writer that does not sort.
func TestMapWriterSortJavaScript(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node (it runs the generated modules)")
	node := jsNode(t)
	work := t.TempDir()

	probe := unitFrom(t, mapsArmsProbeSrc)
	pm := tabletext.NewModel(probe)
	var cases []*vcCase
	for _, pc := range mapsArmsProbeTexts {
		if !strings.HasPrefix(pc.name, "maps_") && pc.name != "everything" {
			continue
		}
		inst := pm.New(pm.Lookup("Probe"))
		var rep tabletext.Report
		if !pm.Read(inst, []byte(pc.text), &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("%s: the oracle did not read the probe text cleanly: %+v", pc.name, rep)
		}
		wire, err := tablewire.Encode(pm, inst)
		if err != nil {
			t.Fatalf("%s: the oracle did not encode: %v", pc.name, err)
		}
		text, err := pm.Write(inst)
		if err != nil {
			t.Fatalf("%s: the oracle did not write: %v", pc.name, err)
		}
		cases = append(cases, &vcCase{Name: pc.name, Root: "Probe", wire: wire, text: append(text, '\n')})
	}

	files, err := generateVariableFiles(probe, nil)
	if err != nil {
		t.Fatalf("the variable form was refused: %v", err)
	}
	home := runtimeHome(probe) + "Variable.js"
	module, ok := files[home]
	if !ok {
		t.Fatal("no variable module was generated")
	}
	const sortLine = "  out.sort((a, b) => TableVariableKeyOrder(fd, a.key, b.key));\n"
	if bytes.Count(module, []byte(sortLine)) != 1 {
		t.Fatalf("the module does not carry the entry sort exactly once (%q): the witness below would plant nothing", strings.TrimSpace(sortLine))
	}
	unsorted := bytes.Replace(module, []byte(sortLine), nil, 1)

	run := func(name string, data []byte) map[string]string {
		t.Helper()
		dir := filepath.Join(work, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, home)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			c.Module = path
			c.Wire = filepath.Join(dir, c.Name+".wire")
			c.Text = filepath.Join(dir, c.Name+".json")
			if err := os.WriteFile(c.Wire, c.wire, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(c.Text, c.text, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		spec, err := json.Marshal(map[string]any{"cases": cases})
		if err != nil {
			t.Fatal(err)
		}
		specPath := filepath.Join(dir, "spec.json")
		if err := os.WriteFile(specPath, spec, 0o600); err != nil {
			t.Fatal(err)
		}
		script := filepath.Join(dir, "probe.mjs")
		if err := os.WriteFile(script, []byte(mapSortDriver), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(node, script, specPath)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: the map sort probe did not run: %v\n%s", name, err, out)
		}
		got := map[string]string{}
		for line := range strings.SplitSeq(string(out), "\n") {
			f := strings.Fields(line)
			if len(f) >= 3 {
				got[f[0]+" "+f[1]] = f[2]
			}
		}
		t.Logf("%s probe output:\n%s", name, out)
		return got
	}

	got := run("sorted", module)
	reversed := 0
	for _, c := range cases {
		for _, k := range []string{"SAVE", "MEASURE", "JSONW"} {
			if v := got[k+" "+c.Name]; v != "OK" {
				t.Errorf("%s %s: %q, want OK (a Map built out of key order must write the oracle's ascending bytes)", k, c.Name, v)
			}
		}
		if got["REVERSED "+c.Name] == "YES" {
			reversed++
		}
	}
	if reversed == 0 {
		t.Fatal("no case held a map of two or more entries: the reversal changed nothing and the test proves nothing")
	}

	witness := run("unsorted", unsorted)
	caught := 0
	for _, c := range cases {
		if witness["SAVE "+c.Name] != "OK" {
			caught++
		}
	}
	if caught == 0 {
		t.Error("REVERSED WITNESS: with the entry sort removed every save still matched the oracle, so this test does not pin the writer's sort")
	}
}

// mapSortDriver loads each case, rebuilds every Map in reversed insertion
// order, and saves, measures and writes the text of the rebuilt value.
const mapSortDriver = `
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

const spec = JSON.parse(readFileSync(process.argv[2], "utf8"));
const same = (a, b) => a !== null && a.length === b.length && a.every((x, i) => x === b[i]);

function reverseMaps(x, seen, flag) {
  if (x === null || typeof x !== "object" || ArrayBuffer.isView(x)) { return x; }
  if (x instanceof Map) {
    if (x.size > 1) { flag.hit = true; }
    return new Map([...x].reverse().map(([k, v]) => [k, reverseMaps(v, seen, flag)]));
  }
  if (seen.has(x)) { return x; }
  seen.add(x);
  if (Array.isArray(x)) {
    for (let i = 0; i < x.length; i++) { x[i] = reverseMaps(x[i], seen, flag); }
    return x;
  }
  for (const k of Object.keys(x)) { x[k] = reverseMaps(x[k], seen, flag); }
  return x;
}

for (const c of spec.cases) {
  const m = await import(pathToFileURL(c.module).href);
  const wire = new Uint8Array(readFileSync(c.wire));
  const rep = new m.TableVariableReport();
  const loaded = m[c.root + "Load"](wire, rep);
  if (loaded === null) { console.log("SAVE " + c.name + " REFUSED"); continue; }
  const flag = { hit: false };
  const v = reverseMaps(loaded, new Set(), flag);
  console.log("REVERSED " + c.name + " " + (flag.hit ? "YES" : "NO"));
  const saved = m[c.root + "Save"](v);
  console.log("SAVE " + c.name + " " + (same(saved, wire) ? "OK" : "DIFF"));
  const measured = m[c.root + "Measure"](v);
  console.log("MEASURE " + c.name + " " + (measured === wire.length ? "OK" : "DIFF"));
  const want = readFileSync(c.text, "utf8");
  console.log("JSONW " + c.name + " " + (m[c.root + "ToJson"](v) === want ? "OK" : "DIFF"));
}
`
