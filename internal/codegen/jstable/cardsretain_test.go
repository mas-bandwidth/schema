package jstable

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cardsretain_test.go holds the card constructs' retain-unknown and descriptor
// tests (docs/SPEC-TABLES.md §6.6, §8): the retained load and save, the named
// loss, and the generated descriptors every field is pinned by.

// descField and descTable are the descriptor surface a node probe prints back
// (docs/SPEC-TABLES.md §8.1): field name, wire kind, storage shape and declared
// bound, in declaration order.
type descField struct {
	Name  string `json:"name"`
	Kind  int    `json:"kind"`
	Shape string `json:"shape"`
	Bound int64  `json:"bound"`
}

type descTable struct {
	Name   string      `json:"name"`
	Fields []descField `json:"fields"`
}

// dumpDescriptors generates the variable module for a unit, runs a node probe
// that prints its unit registry as JSON, and returns the parsed tables.
func dumpDescriptors(t *testing.T, u *ir.Unit) []descTable {
	t.Helper()
	node := jsNode(t)
	out, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatalf("generateVariableFiles: %v", err)
	}
	dir := t.TempDir()
	for name, content := range out {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	module := runtimeHome(u) + "Variable.js"
	script := filepath.Join(dir, "dump.mjs")
	probe := `import { ` + capitalize(u.Package) + `UnitRegistry } from "./` + module + `";
const out = ` + capitalize(u.Package) + `UnitRegistry.tables.map((t) => ({
  name: t.name,
  fields: t.fields.map((f) => ({ name: f.name, kind: f.kind, shape: f.shape, bound: f.bound })),
}));
console.log(JSON.stringify(out));
`
	if err := os.WriteFile(script, []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script)
	cmd.Dir = dir
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("descriptor dump failed: %v\n%s", err, raw)
	}
	var tables []descTable
	if err := json.Unmarshal(raw, &tables); err != nil {
		t.Fatalf("parse descriptors: %v\n%s", err, raw)
	}
	return tables
}

// pinnedField and pinnedTable are the VOCABULARY the descriptor test writes by
// hand, never derived from the descriptors: a field added to a declaration
// without a matching row here must fail the comparison.
type pinnedField struct {
	name  string
	kind  int
	shape string
	bound int64
}

type pinnedTable struct {
	name   string
	fields []pinnedField
}

// TestCardDescriptorsNameEveryField pins the generated descriptors of
// tables/cards against an explicit vocabulary written in this test
// (docs/SPEC-TABLES.md §8.1, §8.3): every table and every field, in declaration
// order, with its wire kind, storage shape and declared bound. A field added to
// a declaration without a matching row fails.
func TestCardDescriptorsNameEveryField(t *testing.T) {
	t.Parallel()

	vocab := []pinnedTable{
		{name: "PathText", fields: []pinnedField{
			{"s", 12, "value", 192},
		}},
		{name: "CardID", fields: []pinnedField{
			{"s", 12, "value", 64},
		}},
		{name: "KeyValue", fields: []pinnedField{
			{"key", 12, "value", 32},
			{"value", 17, "blob", 0},
		}},
		{name: "Consumer", fields: []pinnedField{
			{"kind", 12, "value", 16},
			{"who", 12, "value", 32},
			{"route", 12, "value", 48},
			{"model", 12, "value", 64},
			{"started", 5, "value", 0},
			{"ended", 5, "value", 0},
			{"outcome", 12, "value", 16},
			{"line", 12, "value", 200},
			{"cost_micro_usd", 5, "value", 0},
		}},
		{name: "Card", fields: []pinnedField{
			{"id", 12, "value", 64},
			{"sha", 12, "value", 40},
			{"tier", 30, "value", 0},
			{"repo", 12, "value", 96},
			{"_base", 12, "value", 96},
			{"kind", 12, "value", 24},
			{"priority", 30, "value", 0},
			{"stream", 12, "value", 32},
			{"paths", 14, "list", 0},
			{"_new", 14, "list", 0},
			{"shared", 14, "list", 0},
			{"needs", 14, "list", 0},
			{"test", 12, "value", 192},
			{"done", 17, "blob", 0},
			{"deadline_minutes", 4, "value", 0},
			{"source", 17, "blob", 0},
			{"epoch", 4, "value", 0},
			{"column", 30, "value", 0},
			{"holder", 12, "value", 32},
			{"attempt", 4, "value", 0},
			{"head", 12, "value", 40},
			{"branch", 12, "value", 128},
			{"body", 17, "blob", 0},
			{"extra", 14, "list", 0},
			{"consumers", 14, "list", 0},
		}},
	}

	u := loadUnit(t, "../../../tables/cards/Card.schema")
	tables := dumpDescriptors(t, u)
	if len(tables) != len(vocab) {
		t.Fatalf("descriptor table count = %d, vocabulary pins %d", len(tables), len(vocab))
	}
	for i, want := range vocab {
		got := tables[i]
		if got.Name != want.name {
			t.Errorf("table %d name = %q, want %q", i, got.Name, want.name)
		}
		if len(got.Fields) != len(want.fields) {
			t.Errorf("%s field count = %d, vocabulary pins %d", want.name, len(got.Fields), len(want.fields))
			continue
		}
		for j, wf := range want.fields {
			gf := got.Fields[j]
			if gf.Name != wf.name || gf.Kind != wf.kind || gf.Shape != wf.shape || gf.Bound != wf.bound {
				t.Errorf("%s.%s = {%q %d %q %d}, vocabulary pins {%q %d %q %d}",
					want.name, gf.Name, gf.Name, gf.Kind, gf.Shape, gf.Bound, wf.name, wf.kind, wf.shape, wf.bound)
			}
		}
	}

	// THE NEGATIVE CONTROL, held in the test rather than beside it: a
	// declaration that adds a field without a matching vocabulary row must not
	// compare equal (docs/SPEC-TABLES.md §8.7 — a listing that goes stale is a
	// red, never a silent pass).
	probe := jsUnitOf(t, `package probe
table Thing
{
    a string(8)
    b int32
    c *string
}
`)
	vocabTwo := []pinnedTable{{name: "Thing", fields: []pinnedField{
		{"a", 12, "value", 8},
		{"b", 4, "value", 0},
		{"c", 17, "blob", 0},
	}}}
	if tables := dumpDescriptors(t, probe); !matchVocab(tables, vocabTwo) {
		t.Errorf("the three-field declaration did not match its own vocabulary")
	}
	probeAdded := jsUnitOf(t, `package probe
table Thing
{
    a string(8)
    b int32
    c *string
    d int64
}
`)
	if tables := dumpDescriptors(t, probeAdded); matchVocab(tables, vocabTwo) {
		t.Errorf("a field added without a vocabulary row still compared equal")
	}
}

// matchVocab reports whether a dumped descriptor set equals a pinned vocabulary.
func matchVocab(tables []descTable, vocab []pinnedTable) bool {
	if len(tables) != len(vocab) {
		return false
	}
	for i, want := range vocab {
		got := tables[i]
		if got.Name != want.name || len(got.Fields) != len(want.fields) {
			return false
		}
		for j, wf := range want.fields {
			gf := got.Fields[j]
			if gf.Name != wf.name || gf.Kind != wf.kind || gf.Shape != wf.shape || gf.Bound != wf.bound {
				return false
			}
		}
	}
	return true
}

// variableModuleBytes generates a unit's variable module and returns its bytes.
func variableModuleBytes(t *testing.T, u *ir.Unit) []byte {
	t.Helper()
	out, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatalf("generateVariableFiles: %v", err)
	}
	for _, content := range out {
		return content
	}
	t.Fatal("no variable module generated")
	return nil
}

// retainNodeProbe runs a node probe script from `dir`, where newer and older
// modules were written as CardsVariable.js and OldVariable.js.
func retainNodeProbe(t *testing.T, dir, script string) string {
	t.Helper()
	node := jsNode(t)
	path := filepath.Join(dir, "probe.mjs")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, path)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("retain probe failed: %v\n%s", err, out)
	}
	return string(out)
}

// TestCardConstructsRetainUnknownFields is the retain step (docs/SPEC-TABLES.md
// §6.6): the older declaration's retained Load and Save keep a newer build's
// unknown scalar and string fields and re-emit them in the canonical order, the
// newer declaration rereads every retained value equal, and the *string — a
// node index, §6.6's excluded class — is reported as a loss and never retained
// silently.
func TestCardConstructsRetainUnknownFields(t *testing.T) {
	t.Parallel()
	newer := loadUnit(t, "testdata/cards/retain_new.schema")
	older := loadUnit(t, "testdata/cards/retain_old.schema")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CardsVariable.js"), variableModuleBytes(t, newer), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "OldVariable.js"), variableModuleBytes(t, older), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `
import * as newer from "./CardsVariable.js";
import * as older from "./OldVariable.js";

const input = new newer.Entry();
input.id = "retain";
input.future = 42n;
input.future_text = "kept text";
input.future_note = "lost note";
const wire = newer.EntrySave(input);

const retain = new older.TableVariableRetain(new Uint8Array(4096), new Array(64));
const report = new older.TableVariableReport();
const v = older.EntryLoadRetain(wire, retain, report);
if (v === null || report.malformed || report.refused !== 0) {
  console.log("load", JSON.stringify(report)); process.exit(1);
}
if (v.id !== "retain") { console.log("id", v.id); process.exit(1); }
if (report.unknown !== 3 || report.retained !== 2 || report.retainLost !== 1) {
  console.log("load report", JSON.stringify(report)); process.exit(1);
}
const saved = older.EntrySaveRetain(v, retain, report);
if (saved === null) { console.log("save null"); process.exit(1); }
if (report.retained !== 2 || report.retainLost !== 1) {
  console.log("save report", JSON.stringify(report)); process.exit(1);
}
const back = newer.EntryLoad(saved, new newer.TableVariableReport());
if (back === null || back.id !== "retain" || back.future !== 42n || back.future_text !== "kept text") {
  console.log("reread values", JSON.stringify(back)); process.exit(1);
}
if (back.future_note !== null) { console.log("future_note not lost", back.future_note); process.exit(1); }
// the second save is byte-identical to the first (idempotence, §6.6).
const saved2 = older.EntrySaveRetain(v, retain, new older.TableVariableReport());
if (saved2 === null || saved2.length !== saved.length) { console.log("second save drifted"); process.exit(1); }
for (let i = 0; i < saved.length; i++) { if (saved[i] !== saved2[i]) { console.log("second save byte", i); process.exit(1); } }
console.log("retained", saved.length, "bytes, unknown", report.unknown, "retained", report.retained, "lost", report.retainLost);
`
	retainNodeProbe(t, dir, script)
}

// TestRetentionCapacityIsALoss is the named-loss step (docs/SPEC-TABLES.md
// §6.6): a retention store one byte or one id short reports RetainLost and
// places nothing, an unknown enum variant in a known field is reported and
// blocks a lossless claim, and the input bytes are unchanged in every case.
func TestRetentionCapacityIsALoss(t *testing.T) {
	t.Parallel()
	newer := loadUnit(t, "testdata/cards/retain_new.schema")
	older := loadUnit(t, "testdata/cards/retain_old.schema")
	enumNew := loadUnit(t, "testdata/cards/retain_enum_new.schema")
	enumOld := loadUnit(t, "testdata/cards/retain_enum_old.schema")
	dir := t.TempDir()
	write := func(name string, u *ir.Unit) {
		if err := os.WriteFile(filepath.Join(dir, name), variableModuleBytes(t, u), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("CardsVariable.js", newer)
	write("OldVariable.js", older)
	write("EnumNew.js", enumNew)
	write("EnumOld.js", enumOld)
	script := `
import * as newer from "./CardsVariable.js";
import * as older from "./OldVariable.js";
import * as enumNew from "./EnumNew.js";
import * as enumOld from "./EnumOld.js";

const input = new newer.Entry();
input.id = "retain";
input.future = 7n;
input.future_text = "kept";
input.future_note = "note";
const wire = newer.EntrySave(input);
const wireCopy = new Uint8Array(wire);

// (a) the byte store one byte short of the first record places nothing.
{
  const retain = new older.TableVariableRetain(new Uint8Array(0), new Array(64));
  const report = new older.TableVariableReport();
  const v = older.EntryLoadRetain(wire, retain, report);
  if (v === null || report.retained !== 0 || report.retainLost !== 3) {
    console.log("zero bytes", JSON.stringify(report)); process.exit(1);
  }
  const saved = older.EntrySaveRetain(v, retain, report);
  if (saved === null || report.retainLost !== 3) { console.log("zero save", JSON.stringify(report)); process.exit(1); }
  // nothing retained, so the save is the plain save of the known field alone.
  const plain = older.EntrySave(v);
  if (saved.length !== plain.length) { console.log("plain length"); process.exit(1); }
  for (let i = 0; i < plain.length; i++) { if (saved[i] !== plain[i]) { console.log("plain byte", i); process.exit(1); } }
}

// (b) the id store one short of the two retained ids drops at save.
{
  const retain = new older.TableVariableRetain(new Uint8Array(4096), new Array(1));
  const report = new older.TableVariableReport();
  const v = older.EntryLoadRetain(wire, retain, report);
  if (v === null || report.retained !== 2 || report.retainLost !== 1) {
    console.log("id load", JSON.stringify(report)); process.exit(1);
  }
  const saved = older.EntrySaveRetain(v, retain, report);
  if (saved === null) { console.log("id save null"); process.exit(1); }
  // exactly one record rides without its id, so retainLost grows by one.
  if (report.retainLost !== 2) { console.log("id save report", JSON.stringify(report)); process.exit(1); }
}

// (c) an unknown enum variant in a known field is reported, never retained.
{
  const slot = new enumNew.Slot();
  slot.tier = "c";
  const ew = enumNew.SlotSave(slot);
  const retain = new enumOld.TableVariableRetain(new Uint8Array(4096), new Array(16));
  const report = new enumOld.TableVariableReport();
  const v = enumOld.SlotLoadRetain(ew, retain, report);
  if (v === null || report.unknown !== 1 || report.retainLost !== 1 || report.retained !== 0) {
    console.log("enum report", JSON.stringify(report)); process.exit(1);
  }
  if (v.tier !== null) { console.log("enum tier", v.tier); process.exit(1); }
}

// (d) the input bytes are unchanged in every case.
for (let i = 0; i < wire.length; i++) { if (wire[i] !== wireCopy[i]) { console.log("input changed", i); process.exit(1); } }
console.log("loss is named: zero bytes, short ids and an unknown variant all report, input unchanged");
`
	retainNodeProbe(t, dir, script)
}

