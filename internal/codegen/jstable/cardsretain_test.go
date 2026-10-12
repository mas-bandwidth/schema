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
