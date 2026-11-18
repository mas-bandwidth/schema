package jstable

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestJsPointerTargetBlobKindRefused holds every JavaScript read of the
// variable form to docs/SPEC-TABLES.md §3.1 and §6.5's blob-kind rule
// (docs/rulings.json blob-target-outside-closure-refused): the two reserved
// blob ids are types every reader can name, whatever its root reaches. Each
// row of testdata/wire/pointer-target-type/blobkind/MANIFEST.tsv runs through
// Load and LoadRetain, and each must answer the row's verdict. A declared slot
// naming a blob record of the other kind refuses the whole read wire_damaged;
// a blob record named only from a field this reader does not declare is skipped
// and counted unknown; a blob record no pointer reaches is damage.
//
// The same test holds the leg's writers to never emitting a blob record no
// pointer reaches: a save and a retaining save of each root's blob set, empty
// and null is decoded by the engine's own read, which must accept it with
// nothing unknown. Rows 8, 9 and 10 of the pointer-target feature witnesses (a
// table, a bytes and a string record outside the root's closure, named from a
// declared slot) are encoded by the engine and must be refused wire_damaged by
// Load and LoadRetain, as the engine refuses them. The negative control plants the old root-closure "can name" rule in
// a copy of the generated reader, and text-names-bytes must then read.
func TestJsPointerTargetBlobKindRefused(t *testing.T) {
	t.Parallel()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(repo, "testdata", "wire", "pointer-target-type", "blobkind")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		Case    string `json:"case"`
		Root    string `json:"root"`
		Wire    string `json:"wire"`
		Read    bool   `json:"read"`
		Unknown int    `json:"unknown"`
		Measure int64  `json:"measure"`
	}
	var order []string
	rowsByUnit := map[string][]row{}
	filesByUnit := map[string]map[string][]byte{}
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		if f[4] != "read" && f[4] != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, f[4])
		}
		r := row{Case: f[0], Root: f[2], Wire: f[3], Read: f[4] == "read", Unknown: -1}
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil {
				t.Fatalf("MANIFEST.tsv:%d: unknown %q: %v", i+1, f[5], err)
			}
			r.Unknown = n
		}
		wire, err := os.ReadFile(filepath.Join(dir, f[3]))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := filesByUnit[f[1]]; !ok {
			order = append(order, f[1])
			filesByUnit[f[1]] = map[string][]byte{}
		}
		filesByUnit[f[1]][f[3]] = wire
		rowsByUnit[f[1]] = append(rowsByUnit[f[1]], r)
	}
	if len(rowsByUnit) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	for _, unit := range order {
		rows := rowsByUnit[unit]
		u := loadUnitForMeasure(t, repo, unit)
		for i := range rows {
			want, _, refused := tablewire.FileRegionMeasure(u, findTable(t, u, rows[i].Root), filesByUnit[unit][rows[i].Wire])
			if refused {
				want = -1
			}
			rows[i].Measure = want
		}
		encoded, err := json.Marshal(rows)
		if err != nil {
			t.Fatal(err)
		}
		probe := jsLoadVerdictProbe(encoded)
		t.Run("every MANIFEST row answers its verdict", func(t *testing.T) {
			if out, err := jsBlobKindProbe(t, u, filesByUnit[unit], probe, nil); err != nil {
				t.Fatalf("%s: node probe failed: %v\n%s", unit, err, out)
			}
		})

		t.Run("negative control: the root-closure rule reads text-names-bytes", func(t *testing.T) {
			hasRow := false
			for _, r := range rows {
				if r.Case == "text-names-bytes" {
					hasRow = true
				}
			}
			if !hasRow {
				t.Fatal("MANIFEST.tsv has no text-names-bytes row")
			}
			out, runErr := jsBlobKindProbe(t, u, filesByUnit[unit], probe, func(src []byte) []byte {
				const old = "TableVariableDeclaredTypes.has(tid) || tid === TableStringBlobId || tid === TableBytesBlobId"
				const planted = "TableVariableDeclaredTypes.has(tid) || (tid === TableStringBlobId && st.rootCodec.place.str) || (tid === TableBytesBlobId && st.rootCodec.place.bytes)"
				text := strings.Replace(string(src), old, planted, 1)
				if text == string(src) {
					t.Fatalf("the planted root-closure rule did not apply to the generated reader")
				}
				return []byte(text)
			})
			if runErr == nil {
				t.Fatalf("the planted root-closure rule left text-names-bytes accepted:\n%s", out)
			}
			if !strings.Contains(out, "FAIL: text-names-bytes: Load expected wire_damaged") {
				t.Fatalf("the planted root-closure rule did not read text-names-bytes:\n%s", out)
			}
		})
	}

	t.Run("the flipped feature-witness rows refuse", func(t *testing.T) {
		const witnesses = "testdata/conformance/feature-witnesses/pointer-targets"
		data, err := os.ReadFile(filepath.Join(repo, witnesses, "cases.json"))
		if err != nil {
			t.Fatal(err)
		}
		type witnessCase struct {
			Name, Writer, Reader string
			Input                json.RawMessage
			KindMismatch         int `json:"kind_mismatch"`
		}
		var cases []witnessCase
		if err := json.Unmarshal(data, &cases); err != nil {
			t.Fatal(err)
		}
		u := loadUnitForMeasure(t, repo, witnesses)
		m := tabletext.NewModel(u)
		files := map[string][]byte{}
		var rows []row
		for _, name := range []string{"table outside root closure", "bytes outside root closure", "string outside root closure"} {
			i := slices.IndexFunc(cases, func(c witnessCase) bool { return c.Name == name })
			if i < 0 {
				t.Fatalf("cases.json has no row %q", name)
			}
			c := cases[i]
			if c.KindMismatch == 0 {
				t.Fatalf("%s: cases.json reads this row; the ruling refuses it", name)
			}
			v := m.New(m.Lookup(c.Writer))
			var r tabletext.Report
			if !m.Read(v, c.Input, &r) || !r.Silent() {
				t.Fatalf("%s: the writer's input does not read: %+v", name, r)
			}
			wire, err := tablewire.Encode(m, v)
			if err != nil {
				t.Fatalf("%s: Encode: %v", name, err)
			}
			// The engine is the oracle: it refuses the row wire_damaged.
			var report tabletext.Report
			if ok, _ := tablewire.Decode(m, m.New(m.Lookup(c.Reader)), wire, &report); ok || !report.Refused || !strings.HasPrefix(report.Reason, tablewire.ReasonWireDamaged+":") {
				t.Fatalf("%s: the engine does not refuse wire_damaged: %+v", name, report)
			}
			measure, _, refused := tablewire.FileRegionMeasure(u, findTable(t, u, c.Reader), wire)
			if refused {
				measure = -1
			}
			file := strings.ReplaceAll(name, " ", "-") + ".bin"
			files[file] = wire
			rows = append(rows, row{Case: name, Root: c.Reader, Wire: file, Unknown: -1, Measure: measure})
		}
		encoded, err := json.Marshal(rows)
		if err != nil {
			t.Fatal(err)
		}
		if out, err := jsBlobKindProbe(t, u, files, jsLoadVerdictProbe(encoded), nil); err != nil {
			t.Fatalf("node probe failed: %v\n%s", err, out)
		}
	})

	t.Run("the leg's writers emit no unreachable blob record", func(t *testing.T) {
		unit := order[0]
		u := loadUnitForMeasure(t, repo, unit)
		m := tabletext.NewModel(u)
		// Set false leaves the blob null; Set true stores Value, as a string
		// for Text and as hex-decoded bytes for Bin ("" is the empty blob).
		type writerCase struct {
			Name  string `json:"name"`
			Root  string `json:"root"`
			Field string `json:"field"`
			Set   bool   `json:"set"`
			Value string `json:"value"`
		}
		cases := []writerCase{
			{Name: "Text-set", Root: "Text", Field: "body", Set: true, Value: "hello"},
			{Name: "Text-empty", Root: "Text", Field: "body", Set: true},
			{Name: "Text-null", Root: "Text", Field: "body"},
			{Name: "Bin-set", Root: "Bin", Field: "data", Set: true, Value: "68656c6c6f"},
			{Name: "Bin-empty", Root: "Bin", Field: "data", Set: true},
			{Name: "Bin-null", Root: "Bin", Field: "data"},
		}
		out := runJSConstructProbe(t, mustGenerateVariable(t, u), runtimeHome(u)+"Variable.js", jsBlobKindWriterProbe, cases)
		saved := map[string][2]string{}
		for line := range strings.SplitSeq(out, "\n") {
			f := strings.Fields(line)
			if len(f) == 4 && f[0] == "SAVE" {
				saved[f[1]] = [2]string{f[2], f[3]}
			}
			if strings.HasPrefix(line, "FAIL") {
				t.Error(line)
			}
		}
		for _, c := range cases {
			wires, ok := saved[c.Name]
			if !ok {
				t.Fatalf("%s: the probe saved no wire:\n%s", c.Name, out)
			}
			for i, name := range []string{"save", "retaining save"} {
				if wires[i] == "null" || wires[i] == "" {
					t.Fatalf("%s: the %s refused", c.Name, name)
				}
				wire, err := hex.DecodeString(wires[i])
				if err != nil {
					t.Fatalf("%s: the %s is not hex: %v", c.Name, name, err)
				}
				var report tabletext.Report
				if ok, err := tablewire.Decode(m, m.New(m.Lookup(c.Root)), wire, &report); !ok || err != nil || report.Refused || report.Unknown != 0 {
					t.Errorf("%s: the %s does not read back clean: ok=%v err=%v %+v", c.Name, name, ok, err, report)
				}
			}
		}
		for _, root := range []string{"Text", "Bin"} {
			if saved[root+"-empty"][0] == saved[root+"-null"][0] {
				t.Errorf("%s: the empty blob saved the same wire as null; the probe did not set it", root)
			}
		}
	})
}

// jsLoadVerdictProbe is the node probe for verdict rows (case, root, wire,
// read, unknown, measure): each wire file runs through Load, LoadRetain and
// LoadMeasure, and a refused row must be refused wire_damaged with the
// retaining store left empty.
func jsLoadVerdictProbe(rows []byte) string {
	return `
for (const row of ` + string(rows) + `) {
  const wire = new Uint8Array(readFileSync(row.wire));
  const r = new m.TableVariableReport();
  const v = m[row.root + "Load"](wire, r);
  const retain = new m.TableVariableRetain(new Uint8Array(1 << 16), new Array(256));
  const rr = new m.TableVariableReport();
  const rv = m[row.root + "LoadRetain"](wire, retain, rr);
  if (row.read) {
    if (v === null || r.err() !== null) { fail(row.case + ": Load expected read, got refusal: " + JSON.stringify(r)); }
    if (rv === null || rr.err() !== null) { fail(row.case + ": LoadRetain expected read, got refusal: " + JSON.stringify(rr)); }
    if (row.unknown >= 0 && r.unknown !== row.unknown) { fail(row.case + ": Load unknown " + r.unknown + ", want " + row.unknown); }
    if (row.unknown >= 0 && rr.unknown !== row.unknown) { fail(row.case + ": LoadRetain unknown " + rr.unknown + ", want " + row.unknown); }
  } else {
    if (v !== null || r.err() !== "wire_damaged") { fail(row.case + ": Load expected wire_damaged, got " + JSON.stringify(r)); }
    if (rv !== null || rr.err() !== "wire_damaged" || retain.used !== 0 || retain.count !== 0) { fail(row.case + ": LoadRetain expected wire_damaged with its store empty: " + JSON.stringify(rr)); }
  }
  const measure = Number(m[row.root + "LoadMeasure"](wire));
  if (measure !== row.measure) { fail(row.case + ": LoadMeasure " + measure + ", want " + row.measure); }
}
`
}

// jsBlobKindProbe writes the unit's variable module, optionally patched, and
// runs probe beside it under the leg's own node; probe imports the module as
// `m`. It returns the probe's output and exit error so a negative control can
// require a failure by name.
func jsBlobKindProbe(t *testing.T, u *ir.Unit, files map[string][]byte, probe string, patch func([]byte) []byte) (string, error) {
	t.Helper()
	slowtest.Gate(t, "node (it runs the generated module)")
	gen, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatalf("generateVariableFiles: %v", err)
	}
	module := runtimeHome(u) + "Variable.js"
	data, ok := gen[module]
	if !ok {
		t.Fatalf("no variable module %s generated", module)
	}
	if patch != nil {
		data = patch(data)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "module.js"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	src := `import { readFileSync } from "node:fs";
import * as m from "./module.js";
let failures = 0;
function fail(why) { console.log("FAIL: " + why); failures++; }
` + probe + `
if (failures !== 0) { process.exit(1); }
console.log("ok");
`
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(jsNode(t), "probe.mjs")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func mustGenerateVariable(t *testing.T, u *ir.Unit) map[string][]byte {
	t.Helper()
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

const jsBlobKindWriterProbe = `
import {readFileSync} from "node:fs";
import {pathToFileURL} from "node:url";
const cases=JSON.parse(readFileSync(process.argv[2],"utf8"));
const m=await import(pathToFileURL(process.argv[3]).href);
const hex=b=>b==null?"null":Buffer.from(b).toString("hex");
for(const c of cases){
  const v=new m[c.root]();
  if(c.set){v[c.field]=c.root==="Bin"?new Uint8Array(Buffer.from(c.value,"hex")):c.value;}
  const saved=m[c.root+"Save"](v);
  if(saved===null){console.log("FAIL "+c.name+" Save refused");continue;}
  const store=new m.TableVariableRetain(new Uint8Array(1<<16), new Array(256));
  const lr=new m.TableVariableReport();
  const lv=m[c.root+"LoadRetain"](saved,store,lr);
  if(lv===null){console.log("FAIL "+c.name+" LoadRetain refused its save: "+JSON.stringify(lr));continue;}
  const rs=m[c.root+"SaveRetain"](lv,store,new m.TableVariableReport());
  console.log("SAVE "+c.name+" "+hex(saved)+" "+hex(rs));
}
`
