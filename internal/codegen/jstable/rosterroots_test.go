package jstable

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestJsCodecOpensEveryRosterRootJavaScript pins that the JavaScript variable
// leg opens the five form-1 roster roots the gate card found without a JS
// codec (docs/SPEC-TABLES.md §4.2): tbla2.Root, tblg1.Guarded, tblk1.Root,
// tblk2.Root and tblp2.Chain. Each unit is generated through the public table
// emitter, its module loads in node, and every manifest instance and report
// wire for those roots answers the Go oracle (internal/tablewire): the same
// report, the same saved bytes, and the same region measure.
//
// The fixture set is the MANIFEST's, not a hand-written one, so this test
// covers exactly the bytes the wire fuzzer feeds those roots.
func TestJsCodecOpensEveryRosterRootJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()

	unitSchemas, fixtures := readRosterManifest(t, filepath.Join(repo, "testdata/conformance/tables/MANIFEST.txt"))

	roots := []struct {
		unit string
		root string
	}{
		{"tbla2", "Root"},
		{"tblg1", "Guarded"},
		{"tblk1", "Root"},
		{"tblk2", "Root"},
		{"tblp2", "Chain"},
	}

	type vcCase struct {
		Name   string `json:"name"`
		Root   string `json:"root"`
		Module string `json:"module"`
		Wire   string `json:"wire"`
	}
	var all []*vcCase
	// expected is the oracle's answer, keyed by case name.
	type oracleCase struct {
		measureRefused bool
		exact          bool
		measure        int64
		loaded         bool
		report         tabletext.Report
		saved          []byte
	}
	expect := map[string]oracleCase{}

	for _, rc := range roots {
		paths, ok := unitSchemas[rc.unit]
		if !ok {
			t.Fatalf("%s: the manifest names no such unit", rc.unit)
		}
		var schemaPaths []string
		for _, p := range paths {
			schemaPaths = append(schemaPaths, filepath.Join(repo, p))
		}
		u := loadUnit(t, schemaPaths...)
		vc := newVariableClass(u)
		if !vc.carried(rc.root) {
			t.Fatalf("%s.%s: the variable wire does not carry it: %s", rc.unit, rc.root, vc.reason[rc.root])
		}
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("%s: the emitter refused the unit: %v", rc.unit, err)
		}
		module := runtimeHome(u) + "Variable.js"
		data, ok := files[module]
		if !ok {
			t.Fatalf("%s: the emitter omitted %s", rc.unit, module)
		}
		dir := filepath.Join(work, rc.unit)
		for name, bytes := range files {
			path := filepath.Join(dir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, bytes, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if len(data) == 0 {
			t.Fatalf("%s: %s is empty", rc.unit, module)
		}
		m := tabletext.NewModel(u)
		def := m.Lookup(rc.root)
		if def == nil {
			t.Fatalf("%s: the unit declares no table %s", rc.unit, rc.root)
		}
		if len(fixtures[rc.unit+"."+rc.root]) == 0 {
			t.Fatalf("%s.%s: the manifest names no instance or report wire", rc.unit, rc.root)
		}
		for _, f := range fixtures[rc.unit+"."+rc.root] {
			wire, err := os.ReadFile(filepath.Join(repo, f.Wire))
			if err != nil {
				t.Fatal(err)
			}
			c := &vcCase{Name: f.Name, Root: rc.root, Module: filepath.Join(dir, filepath.FromSlash(module)), Wire: filepath.Join(repo, f.Wire)}
			all = append(all, c)
			expect[f.Name] = oracleRosterCase(t, u, m, def, wire)
		}
	}

	spec, err := json.Marshal(map[string]any{"cases": all})
	if err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(work, "spec.json")
	if err := os.WriteFile(specPath, spec, 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(work, "probe.mjs")
	if err := os.WriteFile(script, []byte(rosterRootsDriver), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script, specPath)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the roster roots probe did not run: %v\n%s", err, out)
	}

	type lever struct {
		Name    string `json:"name"`
		Measure int64  `json:"measure"`
		Loaded  bool   `json:"loaded"`
		Report  struct {
			Unknown      int  `json:"unknown"`
			KindMismatch int  `json:"kindMismatch"`
			Widened      int  `json:"widened"`
			Clamped      int  `json:"clamped"`
			Duplicate    int  `json:"duplicate"`
			Malformed    bool `json:"malformed"`
			Refused      int  `json:"refused"`
		} `json:"report"`
		Saved string `json:"saved"`
	}
	got := map[string]lever{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		var l lever
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			t.Fatalf("the probe printed a line that is not a case: %q", line)
		}
		got[l.Name] = l
	}
	for _, c := range all {
		want := expect[c.Name]
		l, ok := got[c.Name]
		if !ok {
			t.Errorf("%s: the probe did not answer the case", c.Name)
			continue
		}
		if !l.Loaded {
			if l.Measure >= 0 {
				t.Errorf("%s: the leg returned no root, but its LoadMeasure sized a region", c.Name)
				continue
			}
			if !want.measureRefused && !want.report.Malformed && !want.report.Refused {
				t.Errorf("%s: the leg's LoadMeasure refused the framing; the oracle read the same bytes", c.Name)
			}
			continue
		}
		if want.measureRefused {
			t.Errorf("%s: the leg loaded a wire whose container framing requires refusal", c.Name)
			continue
		}
		if want.exact && l.Measure != want.measure {
			t.Errorf("%s: LoadMeasure asks for %d region bytes; the framing commands exactly %d", c.Name, l.Measure, want.measure)
		}
		gotReport := tabletext.Report{
			Unknown:      l.Report.Unknown,
			KindMismatch: l.Report.KindMismatch,
			Widened:      l.Report.Widened,
			Clamped:      l.Report.Clamped,
			Duplicate:    l.Report.Duplicate,
			Malformed:    l.Report.Malformed,
			Refused:      l.Report.Refused != 0,
		}
		if gotReport != want.report {
			t.Errorf("%s: the report differs: the leg says %+v, the oracle says %+v", c.Name, gotReport, want.report)
		}
		if l.Saved == "" {
			if want.saved != nil {
				t.Errorf("%s: the leg saved nothing; the oracle saved %d bytes", c.Name, len(want.saved))
			}
			continue
		}
		saved, err := hex.DecodeString(l.Saved)
		if err != nil {
			t.Errorf("%s: the probe's saved hex is malformed: %v", c.Name, err)
			continue
		}
		if !bytes.Equal(saved, want.saved) {
			t.Errorf("%s: the decoded value differs: the leg saves %d bytes, the oracle %d", c.Name, len(saved), len(want.saved))
		}
	}
	if t.Failed() {
		t.Logf("probe output:\n%s", out)
	} else {
		t.Logf("probe output: %d manifest wires over 5 roots, all equal to the Go oracle", len(all))
	}
}

// oracleRosterCase is the harness's oracle answer for one fixture
// (test/conformance/harness/wirefuzz.go, wireRoot.oracle), reduced to the
// parts this test pins: the region measure, the report, and the saved bytes.
func oracleRosterCase(t *testing.T, u *ir.Unit, m *tabletext.Model, def *ir.Struct, wire []byte) (out struct {
	measureRefused bool
	exact          bool
	measure        int64
	loaded         bool
	report         tabletext.Report
	saved          []byte
}) {
	t.Helper()
	measured, whole, refused := tablewire.FileRegionMeasure(u, def, wire)
	if refused {
		out.measureRefused = true
		return out
	}
	if whole {
		out.exact, out.measure = true, measured
	}
	inst := m.New(def)
	var rep tabletext.Report
	ok, err := tablewire.DecodeRegion(m, inst, wire, &rep)
	if tablewire.Refused(err) {
		out.report = tabletext.Report{Refused: true}
	} else if err != nil {
		t.Fatalf("the oracle refused the root itself: %v", err)
	} else {
		out.loaded = ok
		out.report = rep
		out.report.Malformed = rep.Malformed || !ok
	}
	saved, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the oracle could not encode the fixture: %v", err)
	}
	out.saved = saved
	return out
}

// rosterFixture is one instance or report row of the conformance manifest.
type rosterFixture struct {
	Name string
	Wire string
}

// readRosterManifest reads the unit schema paths and the instance/report wires
// this test depends on, keyed by unit and by unit.root.
func readRosterManifest(t *testing.T, path string) (map[string][]string, map[string][]rosterFixture) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	units := map[string][]string{}
	fixtures := map[string][]rosterFixture{}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		switch f[0] {
		case "unit":
			units[f[1]] = append([]string(nil), f[2:]...)
		case "instance", "report":
			fixtures[f[2]+"."+f[3]] = append(fixtures[f[2]+"."+f[3]], rosterFixture{Name: f[1], Wire: f[4]})
		}
	}
	return units, fixtures
}

// rosterRootsDriver loads, measures, saves and reports each manifest wire, the
// same four answers the wire fuzzer's driver gives.
const rosterRootsDriver = `
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

const spec = JSON.parse(readFileSync(process.argv[2], "utf8"));
for (const c of spec.cases) {
  const m = await import(pathToFileURL(c.module).href);
  const wire = new Uint8Array(readFileSync(c.wire));
  const rep = new m.TableVariableReport();
  const measure = m[c.root + "LoadMeasure"](wire);
  let loaded = false;
  let saved = null;
  if (measure >= 0) {
    const v = m[c.root + "Load"](wire, rep);
    loaded = v !== null;
    if (loaded) { saved = m[c.root + "Save"](v); }
  }
  console.log(JSON.stringify({
    name: c.name, measure, loaded,
    report: {
      unknown: rep.unknown, kindMismatch: rep.kindMismatch, widened: rep.widened,
      clamped: rep.clamped, duplicate: rep.duplicate, malformed: rep.malformed,
      refused: rep.refused,
    },
    saved: saved === null ? "" : Buffer.from(saved).toString("hex"),
  }));
}
`
