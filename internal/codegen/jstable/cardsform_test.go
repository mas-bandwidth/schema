package jstable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cardsform_test.go tests the VARIABLE FORM (form byte 1) support for
// card constructs (docs/SPEC-TABLES.md §3.1). The card class uses only plain
// `table` roots and list elements with bool, int32, int64, enum, string(N),
// *string, and []T. Other variable-class constructs (maps, unions, optional
// ?T, wide kinds) are refused by name at generation time (docs/SPEC-TABLES.md
// §11).

// unitWith builds a one-table unit holding the given table, so a test can ask
// the VARIABLE FORM'S OWN generation entry point whether it accepts or refuses
// exactly those constructs (docs/SPEC-TABLES.md §11).
func unitWith(st *ir.Struct) *ir.Unit {
	return &ir.Unit{
		Package: "cards",
		Tables:  map[string]*ir.Struct{st.Name: st},
	}
}

// TestOtherVariableConstructsAreRefusedByName holds the BOUNDARY of the card
// constructs: every variable-class construct the card set excludes is named as
// refused with its table, its field and the construct, and a field set made
// only of card constructs is not refused (docs/SPEC-TABLES.md §11, §2.1-2.9).
func TestOtherVariableConstructsAreRefusedByName(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		field *ir.Field
		want  string
	}{
		{
			field: &ir.Field{Name: "words", Type: ir.FieldType{Kind: ir.TString, Size: 8}, Array: ir.ArrayList},
			want:  "an array of strings or byte strings (docs/SPEC-TABLES.md §2.9)",
		},
		// a wstring(N) is CARRIED (docs/SPEC-TABLES.md §3, SPEC.md §4.12;
		// widescalarstext_test.go TestFixedPointAndWideTextOnTheTableWireJavaScript),
		// as fixed/ufixed are, so neither is a case here any more
		{
			field: &ir.Field{Name: "titles", Type: ir.FieldType{Kind: ir.TWString, Size: 8}, Array: ir.ArrayList},
			want:  "an array of strings or byte strings (docs/SPEC-TABLES.md §2.9)",
		},
	} {
		st := &ir.Struct{Name: "Probe", IsTable: true, Fields: []*ir.Field{tc.field}}
		got := tableVariableRefusals(st)
		if len(got) != 1 {
			t.Errorf("refusals for Probe.%s = %q, want exactly one naming the construct", tc.field.Name, got)
			continue
		}
		if !strings.Contains(got[0], "Probe."+tc.field.Name) {
			t.Errorf("refusal %q does not name the field Probe.%s", got[0], tc.field.Name)
		}
		if !strings.Contains(got[0], tc.want) {
			t.Errorf("refusal %q does not name the construct %q", got[0], tc.want)
		}
		// THE BOUNDARY IS THE BACKEND'S, not only the helper's: the variable
		// form's own generation entry point refuses the unit REGARDLESS of
		// whether the fixed form could also serve the table (docs/SPEC-TABLES.md
		// §11). A variable table is the VARIABLE wire by declaration (§2.2), so
		// a hook gated on the fixed form's service (fixedRefusal == "") is a
		// green helper over a backend that still silently drops the construct.
		if _, err := generateVariableFiles(unitWith(st), nil); err == nil {
			t.Errorf("generateVariableFiles accepted Probe.%s, a construct outside the card slice", tc.field.Name)
		} else if !strings.Contains(err.Error(), "Probe."+tc.field.Name) {
			t.Errorf("refusal %q does not name the field Probe.%s", err.Error(), tc.field.Name)
		}
	}

	cardish := &ir.Struct{Name: "Card", IsTable: true, Fields: []*ir.Field{
		{Name: "ok", Type: ir.FieldType{Kind: ir.TBool}},
		{Name: "n", Type: ir.FieldType{Kind: ir.TInt, Signed: true, Width: 64}},
		{Name: "tier", Type: ir.FieldType{Kind: ir.TNamed, Ref: &ir.Enum{Name: "Tier"}}},
		{Name: "s", Type: ir.FieldType{Kind: ir.TString, Size: 64}},
		{Name: "note", Type: ir.FieldType{Kind: ir.TString, Pointer: true}},
		{Name: "paths", Type: ir.FieldType{Kind: ir.TNamed, Ref: &ir.Struct{Name: "PathText", IsTable: true}}, Array: ir.ArrayList},
	}}
	if got := tableVariableRefusals(cardish); len(got) != 0 {
		t.Errorf("the card construct set must not be refused, got: %q", got)
	}
	if _, err := generateVariableFiles(unitWith(cardish), nil); err != nil {
		t.Errorf("generateVariableFiles refused the card construct set: %v", err)
	}

	// THE CLASS BEYOND THE CARD CONSTRUCTS is carried, not refused: a pointer
	// to a table, a *bytes, a table nested by value, a bounded array, the rest
	// of the scalar kinds and bytes(N) (docs/SPEC-TABLES.md §2.1, §2.5, §2.9, §3)
	node := &ir.Struct{Name: "Node", IsTable: true}
	beyond := &ir.Struct{Name: "Beyond", IsTable: true, Fields: []*ir.Field{
		{Name: "next", Type: ir.FieldType{Kind: ir.TNamed, Pointer: true, Ref: node}},
		{Name: "blob", Type: ir.FieldType{Kind: ir.TBytes, Pointer: true}},
		{Name: "byval", Type: ir.FieldType{Kind: ir.TNamed, Ref: node}},
		{Name: "some", Type: ir.FieldType{Kind: ir.TNamed, Ref: node}, Array: ir.ArrayCounted, ArrayBound: 4},
		// a declared minimum moves nothing on the table wire (internal/tablewire
		// reads ArrayMin nowhere): [1..4]T is [..4]T's bytes
		{Name: "least", Type: ir.FieldType{Kind: ir.TInt, Width: 8}, Array: ir.ArrayCounted, ArrayBound: 4, ArrayMin: 1},
		{Name: "ptrs", Type: ir.FieldType{Kind: ir.TNamed, Pointer: true, Ref: node}, Array: ir.ArrayList},
		{Name: "u", Type: ir.FieldType{Kind: ir.TInt, Width: 16}},
		{Name: "f", Type: ir.FieldType{Kind: ir.TFloat32}},
		{Name: "b", Type: ir.FieldType{Kind: ir.TBits, Width: 10}},
		{Name: "raw", Type: ir.FieldType{Kind: ir.TBytes, Size: 6}},
		{Name: "nums", Type: ir.FieldType{Kind: ir.TFloat64}, Array: ir.ArrayList},
		// a fixed-length array [N]T (§3) and a nested `type` by value (§2) are
		// carried (TestNestedTypesAndFixedArraysOnTheTableWireJavaScript)
		{Name: "trio", Type: ir.FieldType{Kind: ir.TInt, Signed: true, Width: 32}, Array: ir.ArrayFixed, ArrayBound: 3},
		{Name: "grid", Type: ir.FieldType{Kind: ir.TNamed, Ref: &ir.Struct{Name: "Grid"}}},
	}}
	if got := tableVariableRefusals(beyond); len(got) != 0 {
		t.Errorf("the variable class beyond the card constructs must not be refused, got: %q", got)
	}

	// The committed card corpus end to end: the card set generates its codec and
	// names no refused construct; a refused unit names the construct.
	u := loadUnit(t, "../../../tables/cards/Card.schema")
	out, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatalf("generateVariableFiles on tables/cards: %v", err)
	}
	found := false
	for name, content := range out {
		if strings.Contains(string(content), "refused:") {
			t.Errorf("%s names a refused construct in the card corpus:\n%s", name, content)
		}
		if strings.Contains(string(content), "export function CardSave") {
			found = true
		}
	}
	if !found {
		t.Errorf("no emitted module carries the Card Save codec for the card constructs")
	}
}

// TestFixedUnitWithRefusedVariableConstructStillGenerates pins the SCOPE of the
// boundary (docs/SPEC-TABLES.md §11): a unit whose closure declares a `fixed
// table` (§2.2) that also declares a variable table carrying a construct outside
// the card slice still generates — the refusal binds a unit the variable form
// OWNS, not one whose block, cook and fixed surfaces this leg emits. Red if the
// refusal is applied unit-wide again: tables/pointers (graphdemo) is exactly
// this shape, and its generation is what the leg's table gate reads.
func TestFixedUnitWithRefusedVariableConstructStillGenerates(t *testing.T) {
	t.Parallel()

	unit := &ir.Unit{
		Package: "cards",
		Tables: map[string]*ir.Struct{
			"Probe": {Name: "Probe", IsTable: true, Fields: []*ir.Field{
				// an array of strings, still outside this leg's variable wire
				{Name: "words", Type: ir.FieldType{Kind: ir.TString, Size: 8}, Array: ir.ArrayList},
			}},
			"Stamp": {Name: "Stamp", IsTable: true, FixedDeclared: true, Fields: []*ir.Field{
				{Name: "id", Type: ir.FieldType{Kind: ir.TInt, Signed: true, Width: 32}},
			}},
		},
	}

	if _, err := generateVariableFiles(unit, nil); err != nil {
		t.Fatalf("a fixed unit with a refused variable table was refused whole: %v", err)
	}
}

// writeCardsVariable generates tables/cards' variable module and writes it to a
// fresh directory, returning the directory and the module path inside it.
func writeCardsVariable(t *testing.T) string {
	t.Helper()
	u := loadUnit(t, "../../../tables/cards/Card.schema")
	out, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatalf("generateVariableFiles on tables/cards: %v", err)
	}
	dir := t.TempDir()
	for name, content := range out {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestCardConstructsRoundTripTheVariableForm is the wire step (docs/SPEC-TABLES.md
// §3.1): a JavaScript Load and Save that carry the card corpus byte for byte as
// the Go reference does. tables/cards/fuzz/card-populated.wire loads to the
// values in card-populated.json and re-saves byte for byte, with the `was`
// rename (done_when) found by id and the *string blobs round-tripped through
// the node table.
func TestCardConstructsRoundTripTheVariableForm(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	dir := writeCardsVariable(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	wire, err := filepath.Abs(filepath.Join(repo, "tables", "cards", "fuzz", "card-populated.wire"))
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "probe.mjs")
	probe := `
import { readFileSync } from "node:fs";
import { CardLoad, CardSave, TableVariableReport } from "./CardsVariable.js";
const wire = new Uint8Array(readFileSync(process.argv[2]));
const report = new TableVariableReport();
const card = CardLoad(wire, report);
if (card === null) { console.log("load refused"); process.exit(1); }
if (report.malformed || report.refused !== 0 || report.unknown !== 0 || report.kindMismatch !== 0) {
  console.log("load report", JSON.stringify(report)); process.exit(1);
}
if (card.id !== "populated") { console.log("id", card.id); process.exit(1); }
if (card.tier !== "flash" || card.priority !== "critical" || card.column !== "review") {
  console.log("enum", card.tier, card.priority, card.column); process.exit(1);
}
if (card.paths.length !== 2 || card.paths[0].s !== "path/one" || card.paths[1].s !== "path/two") {
  console.log("paths", JSON.stringify(card.paths)); process.exit(1);
}
if (card.done !== "done when") { console.log("done (was rename)", card.done); process.exit(1); }
if (card.source !== "source résumé→世界") { console.log("source", card.source); process.exit(1); }
if (card.deadline_minutes !== 45 || card.epoch !== 7 || card.attempt !== 1) {
  console.log("ints", card.deadline_minutes, card.epoch, card.attempt); process.exit(1);
}
const saved = CardSave(card);
if (saved.length !== wire.length) { console.log("length", saved.length, wire.length); process.exit(1); }
for (let i = 0; i < wire.length; i++) {
  if (saved[i] !== wire[i]) { console.log("byte", i, saved[i], wire[i]); process.exit(1); }
}
console.log("round-trip", wire.length, "bytes, byte for byte");
`
	if err := os.WriteFile(script, []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script, wire)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated JavaScript did not round-trip the card wire: %v\n%s", err, out)
	}
}

// TestCardTextFormMatchesGo is the text step (docs/SPEC-TABLES.md §16) of this
// card. It is claimed by the text-form codec (schema#349's slice): the JSON of
// card-populated.wire equals tables/cards/fuzz/card-populated.json byte for
// byte, and that JSON read back saves to card-populated.wire byte for byte.
func TestCardTextFormMatchesGo(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	dir := writeCardsVariable(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	wire, err := filepath.Abs(filepath.Join(repo, "tables", "cards", "fuzz", "card-populated.wire"))
	if err != nil {
		t.Fatal(err)
	}
	json, err := filepath.Abs(filepath.Join(repo, "tables", "cards", "fuzz", "card-populated.json"))
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "textprobe.mjs")
	probe := `
import { readFileSync } from "node:fs";
import { CardLoad, CardSave, CardToJson, CardFromJson, TableVariableReport } from "./CardsVariable.js";
const wire = new Uint8Array(readFileSync(process.argv[2]));
const wantText = readFileSync(process.argv[3], "utf8");
const report = new TableVariableReport();
const card = CardLoad(wire, report);
const text = CardToJson(card);
if (text !== wantText) {
  console.log("text mismatch:\n--- got ---\n" + text + "\n--- want ---\n" + wantText);
  process.exit(1);
}
const back = CardFromJson(text, new TableVariableReport());
const saved = CardSave(back);
if (saved.length !== wire.length) { console.log("length", saved.length, wire.length); process.exit(1); }
for (let i = 0; i < wire.length; i++) {
  if (saved[i] !== wire[i]) { console.log("byte", i, saved[i], wire[i]); process.exit(1); }
}
console.log("text round-trip", text.length, "chars, wire", wire.length, "bytes, byte for byte");
`
	if err := os.WriteFile(script, []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script, wire, json)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated JavaScript text form did not match the Go leg's JSON: %v\n%s", err, out)
	}
}
