package jstable

// THE STRICT READ'S RULE FOR THE JS LEG's RETAIN-UNKNOWN AND WIDENING CELLS
// (ROADMAP.md "Variable tables"; docs/SPEC-TABLES.md §3.4, §4, §6.6):
//
//	one read gives either the whole value or a refusal, never a half answer.
//
// The two rows are the ones the cells name:
//
//	vt/widening/js         report on previous_form_wide_blob: WideBlob's record
//	    is past §3.4's ceiling, so it has no file form. The file read refuses
//	    the whole value and writes nothing back.
//	vt/retain-unknown/js   retain and retain-save on retain_node_index: an
//	    unknown field that holds a nested kind-17 node pointer marks the target
//	    node reachable, and the read answers the whole value with the canonical
//	    retained wire.
//
// This is a functional test. It generates the modules and runs them under
// node, so slowtest.Gate skips it unless SCHEMA_SLOW=1 or SCHEMA_REQUIRE_CORPUS
// is set. A missing node fails by name under SCHEMA_REQUIRE_CORPUS; it does
// not skip. The probe takes no real time of its own, opens no socket, and
// makes no network call.

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestJsRetainAndWideningStrict(t *testing.T) {
	slowtest.Gate(t, "node")
	repo := jsStrictRepo(t)
	for _, w := range jsStrictCells(t, repo, nil) {
		t.Error(w)
	}
}

func TestJsRetainAndWideningStrictNegativeControl(t *testing.T) {
	slowtest.Gate(t, "node")
	repo := jsStrictRepo(t)

	t.Run("widening_no_file_form", func(t *testing.T) {
		wrong := jsStrictCells(t, repo, func(demo, _ string) {
			sabotageWideBlobLoad(t, demo)
		})
		want := "report/previous_form_wide_blob: got verdict read, want refused"
		if len(wrong) != 1 || wrong[0] != want {
			t.Fatalf("NEGATIVE CONTROL FAILED: putting WideBlobLoad back on TableVariableLoad produced %v, want [%q]", wrong, want)
		}
	})

	t.Run("retain_unknown_node_refs", func(t *testing.T) {
		wrong := jsStrictCells(t, repo, func(_, rt string) {
			sabotageUnknownNodeReach(t, rt)
		})
		want := []string{
			"retain/retain_node_index: answer differs from read + retain-values/retain_node_index.json",
			"retain-save/retain_node_index: driver wrote nothing",
		}
		if len(wrong) != len(want) || wrong[0] != want[0] || wrong[1] != want[1] {
			t.Fatalf("NEGATIVE CONTROL FAILED: dropping the unknown kind-17 reach mark produced %v, want %v", wrong, want)
		}
	})
}

// jsStrictPlant edits the generated modules before the probe runs. A nil plant
// is the strict read as emitted.
type jsStrictPlant func(demoModule, retainModule string)

func jsStrictRepo(t *testing.T) string {
	t.Helper()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

type jsStrictAnswer struct {
	Verdict   string  `json:"verdict"`
	ValueNull bool    `json:"valueNull"`
	Saved     bool    `json:"saved"`
	JSON      *string `json:"json"`
	SaveHex   *string `json:"saveHex"`
}

// jsStrictCells generates the two modules, optionally plants a fault in the
// emitted JavaScript, and returns the findings. Empty means both cells hold
// the rule. A wrong widening verdict is the only widening finding: the body
// and the save wire are checked only when the verdict is already refused, so
// the widening negative control stays one line.
func jsStrictCells(t *testing.T, repo string, plant jsStrictPlant) []string {
	t.Helper()
	node := jsNode(t)
	work := t.TempDir()
	demo := variableModuleFor(t, loadUnitForMeasure(t, repo, "tables/examples"), work)
	rt := variableModuleFor(t, loadUnitForMeasure(t, repo, "test/tables/RT1.schema"), work)
	if plant != nil {
		plant(demo, rt)
	}
	demoRel, err := filepath.Rel(work, demo)
	if err != nil {
		t.Fatal(err)
	}
	rtRel, err := filepath.Rel(work, rt)
	if err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(work, "strict.mjs")
	src := strings.NewReplacer(
		"DEMO_MODULE", filepath.ToSlash(demoRel),
		"RT_MODULE", filepath.ToSlash(rtRel),
	).Replace(jsStrictProbe)
	if err := os.WriteFile(probe, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(work, "answer.json")
	cmd := exec.Command(node, probe,
		filepath.Join(repo, "testdata/wire/tables/wide_blob.bin"),
		filepath.Join(repo, "testdata/wire/tables/retain_excluded_2.bin"),
		outPath,
	)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node strict probe failed: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("probe wrote no answer: %v\n%s", err, out)
	}
	var ans jsStrictAnswer
	if err := json.Unmarshal(raw, &ans); err != nil {
		t.Fatalf("probe answer %q: %v", raw, err)
	}
	wantJSON, err := os.ReadFile(filepath.Join(repo, "testdata/conformance/tables/retain-values/retain_node_index.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantSave, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables/retain_excluded_2_save.bin"))
	if err != nil {
		t.Fatal(err)
	}
	wrong := jsStrictFindings(ans, wantJSON, wantSave)
	if len(wrong) > 0 {
		t.Logf("probe %s", raw)
	}
	return wrong
}

func jsStrictFindings(ans jsStrictAnswer, wantJSON, wantSave []byte) []string {
	var wrong []string
	if ans.Verdict != "refused" {
		wrong = append(wrong, "report/previous_form_wide_blob: got verdict "+ans.Verdict+", want refused")
	} else {
		if !ans.ValueNull {
			wrong = append(wrong, "report/previous_form_wide_blob: refused read wrote value bytes")
		}
		if ans.Saved {
			wrong = append(wrong, "report/previous_form_wide_blob: refused read wrote save wire")
		}
	}
	if !jsStrictJSONMatches(ans.JSON, wantJSON) {
		wrong = append(wrong, "retain/retain_node_index: answer differs from read + retain-values/retain_node_index.json")
	}
	if ans.SaveHex == nil {
		wrong = append(wrong, "retain-save/retain_node_index: driver wrote nothing")
		return wrong
	}
	gotSave, err := hex.DecodeString(*ans.SaveHex)
	if err != nil || !bytes.Equal(gotSave, wantSave) {
		n := 0
		if err == nil {
			n = len(gotSave)
		}
		wrong = append(wrong, "retain-save/retain_node_index: saved wire ("+strconv.Itoa(n)+" bytes) differs from the canonical save ("+strconv.Itoa(len(wantSave))+" bytes)")
	}
	return wrong
}

// jsStrictJSONMatches holds the §16 text to the retain-values oracle. The
// oracle file ends in a newline the walker may omit.
func jsStrictJSONMatches(got *string, want []byte) bool {
	if got == nil {
		return false
	}
	b := []byte(*got)
	if bytes.Equal(b, want) {
		return true
	}
	if len(b) == 0 || b[len(b)-1] == '\n' {
		return false
	}
	return bytes.Equal(append(b, '\n'), want)
}

// sabotageWideBlobLoad replaces the emitted refusing WideBlobLoad with the
// variable walk. The fault is in the module node executes, not in a string the
// emitter writes and then ignores.
func sabotageWideBlobLoad(t *testing.T, module string) {
	t.Helper()
	src := readModule(t, module)
	const sig = "export function WideBlobLoad(bytes, report) {\n"
	if strings.Count(src, sig) != 1 {
		t.Fatalf("WideBlobLoad occurs %d times in %s, want 1", strings.Count(src, sig), module)
	}
	start := strings.Index(src, sig)
	rel := strings.Index(src[start+len(sig):], "\nexport function ")
	if rel < 0 {
		t.Fatal("WideBlobLoad has no following export")
	}
	end := start + len(sig) + rel
	fn := src[start:end]
	if !strings.Contains(fn, "previous_form") || !strings.Contains(fn, "return null;") || strings.Contains(fn, "TableVariableLoad") {
		t.Fatalf("WideBlobLoad is not the refusing file read:\n%s", fn)
	}
	replacement := sig + "  return TableVariableLoad(TableVariableCodecs.WideBlob, bytes, report, null);\n}"
	if err := os.WriteFile(module, []byte(src[:start]+replacement+src[end:]), 0o600); err != nil {
		t.Fatal(err)
	}
}

// sabotageUnknownNodeReach deletes the reach mark CheckUnknown writes for a
// kind-17 pointer nested in an unknown field. The known-slot mark in
// TableVariableResolve (index - 2) stays, so the fault is the unknown pointer
// and not every reach mark.
func sabotageUnknownNodeReach(t *testing.T, module string) {
	t.Helper()
	src := readModule(t, module)
	const mark = "      if (st.reached !== undefined) { st.reached.add(idx - 2); }\n"
	if strings.Count(src, mark) != 1 {
		t.Fatalf("unknown kind-17 reach mark occurs %d times in %s, want 1", strings.Count(src, mark), module)
	}
	if !strings.Contains(src, "st.reached.add(index - 2)") {
		t.Fatal("the known-slot reach mark is absent; the control would be deleting the wrong line")
	}
	if err := os.WriteFile(module, []byte(strings.Replace(src, mark, "", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readModule(t *testing.T, module string) string {
	t.Helper()
	b, err := os.ReadFile(module)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const jsStrictProbe = `
import { readFileSync, writeFileSync } from "node:fs";
import { WideBlobLoad, WideBlobSave, TableVariableReport } from "./DEMO_MODULE";
import { NodeLoadRetain, NodeSaveRetain, NodeToJson, TableVariableRetain, TableVariableReport as RetainReport } from "./RT_MODULE";

const wide = new Uint8Array(readFileSync(process.argv[2]));
const rep = new TableVariableReport();
const value = WideBlobLoad(wide, rep);
const verdict = rep.refused ? "refused" : "read";
let saved = false;
if (value !== null) {
  const wire = WideBlobSave(value);
  saved = wire !== null && wire.length > 0;
}
const retainWire = new Uint8Array(readFileSync(process.argv[3]));
const store = new TableVariableRetain(new Uint8Array(1 << 20), new Array(1 << 17));
const rrep = new RetainReport();
const root = NodeLoadRetain(retainWire, store, rrep);
let json = null;
let saveHex = null;
if (root !== null && !rrep.damaged) {
  json = NodeToJson(root);
  const out = NodeSaveRetain(root, store, new RetainReport());
  if (out !== null) { saveHex = Buffer.from(out).toString("hex"); }
}
writeFileSync(process.argv[4], JSON.stringify({
  verdict,
  valueNull: value === null,
  saved,
  json,
  saveHex,
}));
`
