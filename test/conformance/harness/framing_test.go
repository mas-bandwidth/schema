package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// framingDir is where the hand-built framing vectors live (docs/LAYERS/1-framing.md).
const framingDir = "testdata/wire/tables/framing"

// TestFramingVectorBytesArePinned holds every vector's file to its byte script:
// the script in framing_vectors_test.go is the readable form and the file is
// what every leg reads, and the two may not drift. FRAMING_WRITE=1 writes the
// files from the scripts, which is how a new vector lands.
func TestFramingVectorBytesArePinned(t *testing.T) {
	_, _, u := corpus(t)
	write := os.Getenv("FRAMING_WRITE") == "1"
	seen := map[string]bool{}
	for _, v := range framingVectors {
		if seen[v.name] {
			t.Errorf("%s: two vectors share the name", v.name)
		}
		seen[v.name] = true
		want, err := v.bytes(u)
		if err != nil {
			t.Fatalf("%s: %v", v.name, err)
		}
		path := filepath.Join(framingDir, v.name+".bin")
		if write {
			if err := os.MkdirAll(framingDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v — run with FRAMING_WRITE=1 to write it", v.name, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: the file is not the script's bytes — run with FRAMING_WRITE=1 to rewrite it", v.name)
		}
	}
}

// TestFramingRowsNameEveryVector holds the manifest and the scripts to one
// list: every vector not held out has a `framing` row, every row names a
// vector, and a held-out vector has none.
func TestFramingRowsNameEveryVector(t *testing.T) {
	m, _, _ := corpus(t)
	rows := map[string]FramingCase{}
	for _, fc := range m.Framings {
		rows[fc.Name] = fc
	}
	for _, v := range framingVectors {
		row, ok := rows[v.name]
		switch {
		case v.held && ok:
			t.Errorf("%s: held out on a finding (docs/LAYERS/1-framing.md) and still a manifest row", v.name)
		case !v.held && !ok:
			t.Errorf("%s: the manifest has no framing row for it", v.name)
		case ok:
			if row.Wire != filepath.ToSlash(filepath.Join(framingDir, v.name+".bin")) {
				t.Errorf("%s: the row reads %s, the vector is %s/%s.bin", v.name, row.Wire, framingDir, v.name)
			}
			if row.Unit != v.unit || row.Root != v.root {
				t.Errorf("%s: the row reads %s.%s, the vector is %s.%s", v.name, row.Unit, row.Root, v.unit, v.root)
			}
			delete(rows, v.name)
		}
	}
	for name := range rows {
		t.Errorf("%s: a framing row with no vector script", name)
	}
}

// framingStrictRefusals are the framing rows whose manifest line pins an
// answer the one read retires (docs/plans/strict-only.md (b), framing rows),
// each with the rule that refuses it. The one read refuses every one of them
// (docs/SPEC-TABLES.md §4, §6.5): an inert body, a header that crosses its L,
// a repeated id, a kind mismatch and a count past the bound each refuse, a
// node record no pointer reaches is damage, and the conditions decidable from
// the wire alone apply inside an unknown field as everywhere else. Layer 3
// re-pins their manifest lines; until it does, this list is the verdict they
// owe.
var framingStrictRefusals = map[string]string{
	"frame_kind_mismatch_skips_by_wire_kind": "R4: a named field under another kind refuses",
	"frame_repeat_last_wins":                 "R5: a repeated id is damage",
	"array_L0_inert":                         "H2: an array body with L = 0 is damage",
	"array_L1_inert":                         "H2: an array body with L = 1 is damage",
	"array_count_bound_plus_one":             "H6: a count past the bound refuses",
	"array_header_crosses_L_kind_mismatch":   "H3: a header crossing its L is damage",
	"array_elem_kind_mismatch":               "H4: an element kind mismatch refuses",
	"map_header_crosses_L_kind_mismatch":     "H3: a header crossing its L is damage",
	"map_L1_inert":                           "H2: a map body with L = 1 is damage",
	"keyed_header_crosses_L_kind_mismatch":   "H3: a header crossing its L is damage",
	"keyed_elem_kind_mismatch":               "H4: an element kind mismatch refuses",
	"keyed_L1_inert":                         "H2: an enum-keyed body with L = 1 is damage",
	"node_table_twice":                       "R5, T1: a repeated node table is damage",
	"node_index_null":                        "T1: a node record no pointer reaches is damage",
	"frame_unknown_id_length":                "R2: an unknown field's bytes are checked, and AA BB CC is no body",
	"frame_unknown_id_reference":             "R2, T7: an unknown pointer's index past the node table is damage",
	"node_unknown_to_fixed_reader":           "R2, T4: an unknown node table is checked, and its record's type ref is 0",
}

// framingOutcome is what the engine answered one framing vector: the error
// Decode returned, the report, and whether the instance it left is the root
// at its declared defaults.
type framingOutcome struct {
	err       error
	report    tabletext.Report
	atDefault bool
}

// framingVerdictFault is the check the oracle test applies to one row: nil
// when the engine answered as the row's rule says, or what it got wrong. A
// form refusal is `refused`; every other refusal (damage, a kind mismatch, a
// count past the bound) is a read's refusal and hands back nothing it read; a
// read returns the row's `unknown` and `widened`.
func framingVerdictFault(fc FramingCase, strictRule string, got framingOutcome) error {
	switch {
	case fc.Refused:
		if !tablewire.Refused(got.err) {
			return fmt.Errorf("the rule refuses the form and the engine answers %v", got.err)
		}
		return nil
	case fc.Damaged || strictRule != "":
		rule := strictRule
		if rule == "" {
			rule = "damage discards the whole load"
		}
		if !tablewire.ReadRefusal(got.err) || tablewire.Refused(got.err) {
			return fmt.Errorf("the rule refuses (%s) and the engine answers %v with %+v", rule, got.err, got.report)
		}
		if !got.report.Refused {
			return fmt.Errorf("the engine refused and its report is not a refusal: %+v", got.report)
		}
		if !got.atDefault {
			return errors.New("the engine handed back a value from a refused load")
		}
		return nil
	}
	if got.err != nil {
		return fmt.Errorf("the rule reads and the engine refuses: %v", got.err)
	}
	if got.report.Refused || got.report.Unknown != fc.Counts.Unknown || got.report.Widened != fc.Counts.Widened {
		return fmt.Errorf("the engine reads %+v and the rule says unknown %d, widened %d", got.report, fc.Counts.Unknown, fc.Counts.Widened)
	}
	return nil
}

// framingVariableUnit is a framing row's unit as the variable form reads it.
// The framing vectors are form-1 files and the rules they pin are the
// variable form's (docs/LAYERS/1-framing.md), but a root declared `fixed
// table` reads form 3 alone and refuses every form-1 file as previous_form
// before any framing rule runs (docs/SPEC-TABLES.md §3.4). Such a root is
// read here through a fresh load of its unit with the root's class variable,
// the same declaration with the same fields and ids, so the battery reaches
// the rules it was written for. The returned unit is never shared.
func framingVariableUnit(t *testing.T, m *Manifest, key, root string) *ir.Unit {
	t.Helper()
	unit, err := newUnits(m).get(key)
	if err != nil {
		t.Fatal(err)
	}
	st := unit.Tables[root]
	if st == nil {
		t.Fatalf("%s declares no table %s", key, root)
	}
	st.FixedDeclared = false
	return unit
}

// TestFramingVectorsReadThroughTheEngine is THE ORACLE CHECK
// (docs/LAYERS/1-framing.md): the Go engine, which every leg is fuzzed
// against, answers every framing vector as the rule text says, READ with the
// value in the row's file or REFUSED (docs/SPEC-TABLES.md §4, §6.5). A
// difference here is an engine defect or a finding, never a vector to re-pin
// from the engine.
func TestFramingVectorsReadThroughTheEngine(t *testing.T) {
	m, _, u := corpus(t)
	rows := map[string]bool{}
	variable := map[string]*ir.Unit{}
	for _, fc := range m.Framings {
		rows[fc.Name] = true
		strictRule := framingStrictRefusals[fc.Name]
		retired := fc.Counts.KindMismatch != 0 || fc.Counts.Clamped != 0 || fc.Counts.Duplicate != 0
		if strictRule == "" && retired {
			t.Errorf("%s: the row pins a retired counter (%s) and framingStrictRefusals does not name it", fc.Name, fc.Counts)
		}
		unit, err := u.get(fc.Unit)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(fc.Wire)
		if err != nil {
			t.Fatal(err)
		}
		// a row pinned as refused by its form is read through the declared
		// unit: a fixed root refuses the form-1 file by its form byte
		if st := unit.Tables[fc.Root]; st != nil && st.FixedDeclared && !fc.Refused {
			key := fc.Unit + "." + fc.Root
			if variable[key] == nil {
				variable[key] = framingVariableUnit(t, m, fc.Unit, fc.Root)
			}
			unit = variable[key]
		}
		model := tabletext.NewModel(unit)
		def := model.Lookup(fc.Root)
		if def == nil {
			t.Fatalf("%s: the unit declares no root %q", fc.Name, fc.Root)
		}
		if !fc.Refused && tablewire.FixedRoot(model, def) {
			t.Fatalf("%s: %s.%s still reads as a fixed root", fc.Name, fc.Unit, fc.Root)
		}
		inst := model.New(def)
		var got framingOutcome
		_, got.err = tablewire.Decode(model, inst, data, &got.report)
		if got.err != nil && !tablewire.ReadRefusal(got.err) {
			t.Errorf("%s: the engine errored: %v", fc.Name, got.err)
			continue
		}
		fresh, _ := tablewire.Encode(model, model.New(def))
		left, _ := tablewire.Encode(model, inst)
		got.atDefault = fresh == nil || bytes.Equal(left, fresh)
		if fault := framingVerdictFault(fc, strictRule, got); fault != nil {
			t.Errorf("%s: %v", fc.Name, fault)
			continue
		}
		if fc.Value == "" || got.err != nil {
			continue
		}
		text, err := model.Write(inst)
		if err != nil {
			t.Errorf("%s: the engine's text writer refused the value: %v", fc.Name, err)
			continue
		}
		if !bytes.HasSuffix(text, []byte("\n")) {
			// the text ends with one newline, which is part of the form rather
			// than a file convention (§16.1); the engine's writer leaves it to
			// the file it lands in, exactly as `harness generate` does
			text = append(text, '\n')
		}
		wantText, err := os.ReadFile(fc.Value)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(text, wantText) {
			t.Errorf("%s: the engine's value is not the rule's\n--- engine\n%s--- rule\n%s", fc.Name, text, wantText)
		}
	}
	for name := range framingStrictRefusals {
		if !rows[name] {
			t.Errorf("%s: framingStrictRefusals names no manifest row", name)
		}
	}
}

// TestFramingVerdictCheckCatchesTheWrongAnswer is the oracle check's NEGATIVE
// CONTROL: an engine that reads what the rule refuses, refuses what the rule
// reads, miscounts an unknown, hands back a value from a refused load, or
// confuses a form refusal with damage is caught by the check the oracle test
// applies, and the right answer passes it.
func TestFramingVerdictCheckCatchesTheWrongAnswer(t *testing.T) {
	damage := &tablewire.Refusal{Reason: tablewire.ReasonWireDamaged}
	refused := tabletext.Report{Refused: true, Reason: tablewire.ReasonWireDamaged}
	read := FramingCase{Name: "read", Counts: Counts{Unknown: 1}}
	wrong := []struct {
		name   string
		fc     FramingCase
		strict string
		got    framingOutcome
	}{
		{"an inert body read", FramingCase{Name: "inert"}, "H2", framingOutcome{atDefault: true}},
		{"damage read", FramingCase{Name: "damaged", Damaged: true}, "", framingOutcome{atDefault: true}},
		{"a value handed back", FramingCase{Name: "damaged", Damaged: true}, "", framingOutcome{err: damage, report: refused}},
		{"a refusal without a refused report", FramingCase{Name: "inert"}, "H2", framingOutcome{err: damage, atDefault: true}},
		{"a read refused", read, "", framingOutcome{err: damage, report: refused, atDefault: true}},
		{"an unknown not counted", read, "", framingOutcome{atDefault: true}},
		{"a form refusal answered as damage", FramingCase{Name: "form", Refused: true}, "", framingOutcome{err: damage, report: refused, atDefault: true}},
		{"a repeat answered as a form refusal", FramingCase{Name: "repeat"}, "R5", framingOutcome{err: &tablewire.FormRefusal{Form: 9}, report: tabletext.Report{Refused: true}, atDefault: true}},
	}
	for _, c := range wrong {
		if framingVerdictFault(c.fc, c.strict, c.got) == nil {
			t.Errorf("%s: the check passes a wrong answer", c.name)
		}
	}
	right := []struct {
		name   string
		fc     FramingCase
		strict string
		got    framingOutcome
	}{
		{"an inert body refused", FramingCase{Name: "inert"}, "H2", framingOutcome{err: damage, report: refused, atDefault: true}},
		{"a read with its unknown", read, "", framingOutcome{report: tabletext.Report{Unknown: 1}}},
		{"a form refusal", FramingCase{Name: "form", Refused: true}, "", framingOutcome{err: &tablewire.FormRefusal{Form: 9}, report: tabletext.Report{Refused: true}, atDefault: true}},
	}
	for _, c := range right {
		if fault := framingVerdictFault(c.fc, c.strict, c.got); fault != nil {
			t.Errorf("%s: the right answer fails the check: %v", c.name, fault)
		}
	}
}
