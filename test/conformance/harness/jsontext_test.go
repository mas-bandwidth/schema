package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// THE BUILDER'S SORT IS ASKED OF EVERY LEG (jsontext.go, docs/SPEC-TABLES.md
// §22.7): the corpus's json-text row reaches a leg's json-read manifest as an
// instance line, and the json-read surface (and only it) owes the row's wire.
func TestJSONTextIsAskedOnJSONReadAlone(t *testing.T) {
	m, pinned, _ := corpus(t)
	var jt *JSONTextCase
	for i := range m.JSONTexts {
		if m.JSONTexts[i].Name == "edge_map_keys_unsorted" {
			jt = &m.JSONTexts[i]
		}
	}
	if jt == nil {
		t.Fatal("the manifest carries no json-text row edge_map_keys_unsorted")
	}
	line := "instance edge_map_keys_unsorted mapdemo Fleet testdata/wire/tables/edge_map_keys.bin\n"
	if got := jsonReadManifest("unit mapdemo x.schema\n", m, nil); !strings.Contains(got, line) {
		t.Errorf("the leg's json-read manifest lacks %q:\n%s", line, got)
	}
	if got := jsonReadManifest("unit mapdemo x.schema\n", m, map[string]bool{"mapdemo": true}); strings.Contains(got, "edge_map_keys_unsorted") {
		t.Errorf("a leg not asked unit mapdemo is handed its json-text row:\n%s", got)
	}

	wire, err := os.ReadFile(jt.Wire)
	if err != nil {
		t.Fatal(err)
	}
	owes := func(surface string) ([]byte, bool) {
		exps, err := expectations(m, surface, pinned, defaultJSONDir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range exps {
			if e.name == jt.Name {
				return e.want, true
			}
		}
		return nil, false
	}
	if got, ok := owes("json-read"); !ok || !bytes.Equal(got, wire) {
		t.Errorf("json-read owes %d bytes for edge_map_keys_unsorted (asked %t), want the %d of %s", len(got), ok, len(wire), jt.Wire)
	}
	if _, ok := owes("wire"); ok {
		t.Error("the wire surface is asked a json-text row, which has no wire case")
	}
}

// A JSON-TEXT ROW MUST BE A TRAP (jsontext.go, checkJSONText): the oracle
// reads it to the row's wire, and it is not already the canonical text. The
// corpus row passes; the same row pointed at the canonical text, or at
// another wire, is refused by name.
func TestJSONTextCheckRefusesARowThatTestsNothing(t *testing.T) {
	m, _, u := corpus(t)
	var jt JSONTextCase
	for _, c := range m.JSONTexts {
		if c.Name == "edge_map_keys_unsorted" {
			jt = c
		}
	}
	if err := checkJSONText(u, jt); err != nil {
		t.Fatalf("the corpus row: %v", err)
	}
	canonical := jt
	canonical.Text = "testdata/conformance/tables/rewrite/edge_map_keys.json"
	if err := checkJSONText(u, canonical); err == nil || !strings.Contains(err.Error(), "already the canonical text") {
		t.Errorf("a canonical text as a json-text row: got %v, want the refusal naming it canonical", err)
	}
	other := jt
	other.Wire = "testdata/wire/tables/edge_text_keys.bin"
	if err := checkJSONText(u, other); err == nil || !strings.Contains(err.Error(), "not the") {
		t.Errorf("a json-text row naming another wire: got %v, want the refusal naming the byte counts", err)
	}
}
