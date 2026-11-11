// TestSpecMessageFormHasOneEncoding pins docs/SPEC-TABLES.md §3.3's rule that a
// message-form batch has one encoding for each value: a body is damage when it
// writes a field at the default the writer elides, writes fields out of the
// writer's declaration order, or writes a union with the None arm. It also pins
// the docs/rulings.json entry and the conformance vectors the engine and port
// cards consume.
package compiler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpecMessageFormHasOneEncoding(t *testing.T) {
	t.Parallel()

	pageData, err := os.ReadFile("../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageData)
	message := specTablesSection(t, page,
		"### 3.3 The message form", "### 3.4 The fixed form")
	flat := strings.Join(strings.Fields(message), " ")

	rows := []struct {
		row    string
		wants  []string
		stales []string
	}{
		{
			row: "§3.3 one encoding for each value",
			wants: []string{
				"A BODY HAS EXACTLY ONE ENCODING FOR ITS VALUE",
				"A BODY THAT IS NOT THE ONE A CONFORMING WRITER EMITS IS DAMAGE",
				"A FIELD WRITTEN AT THE DEFAULT THE WRITER ELIDES",
				"same announced shape and the same build version",
				"every field's specified default is digested into the build version (§20.1)",
				"A field whose sender default this reader cannot know",
				"is read as today",
				"lawful version evolution",
				"A BODY'S FIELD REFERENCES OUT OF THE WRITER'S DECLARATION ORDER",
				"the order the announcement gives each record's fields",
				"a field this reader cannot name still has its place in that order",
				"A UNION FIELD WRITTEN WITH THE NONE REFERENCE",
				"an explicit arm `0` is not the one encoding a conforming writer emits",
				"is not part of the contract",
				"is the variable form's and is neither changed nor carried here",
				"or a union written with the None reference (above)",
			},
			stales: []string{
				"Field ORDER within a body is not part of the contract",
			},
		},
	}
	for _, r := range rows {
		for _, want := range r.wants {
			if !strings.Contains(flat, want) {
				t.Errorf("%s: §3.3 no longer states %q", r.row, want)
			}
		}
		for _, stale := range r.stales {
			if strings.Contains(flat, stale) {
				t.Errorf("%s: §3.3 carries the variable form's sentence %q", r.row, stale)
			}
		}
	}

	type entry struct {
		ID     string   `json:"id"`
		Date   string   `json:"date"`
		Quotes []string `json:"quotes"`
		Source string   `json:"source"`
	}
	rulingsData, err := os.ReadFile("../docs/rulings.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Rulings []entry `json:"rulings"`
	}
	if err := json.Unmarshal(rulingsData, &doc); err != nil {
		t.Fatalf("docs/rulings.json does not parse: %v", err)
	}
	const ruling = "ruled 2026-11-10: a message-form batch has one encoding for each value; " +
		"an explicit default the writer elides, fields out of the writer's order, and an explicit None arm are refused"
	var found *entry
	for i := range doc.Rulings {
		if doc.Rulings[i].ID == "message-one-encoding" {
			found = &doc.Rulings[i]
			break
		}
	}
	if found == nil {
		t.Fatal("docs/rulings.json has no message-one-encoding entry")
	}
	if found.Date != "2026-11-10" {
		t.Errorf("message-one-encoding date = %q, want 2026-11-10", found.Date)
	}
	if len(found.Quotes) == 0 || found.Quotes[0] != ruling {
		t.Errorf("message-one-encoding ruling = %q, want %q", found.Quotes, ruling)
	}
	if !strings.Contains(strings.Join(found.Quotes, " "), "same announced shape and the same build version") {
		t.Error("message-one-encoding does not carry the narrowed explicit-default rule")
	}
	if !strings.Contains(found.Source, "strict wins") || !strings.Contains(found.Source, "coordinator") {
		t.Errorf("message-one-encoding source = %q, want the standing strict-wins rule applied by the coordinator", found.Source)
	}

	const vectors = "../testdata/wire/message-one-encoding"
	files := []struct {
		name    string
		verdict string
	}{
		{"announcement.bin", "read"},
		{"canonical.bin", "read"},
		{"explicit-default.bin", "refused"},
		{"fields-reordered.bin", "refused"},
		{"union-none.bin", "refused"},
	}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(vectors, f.name)); err != nil {
			t.Errorf("message-one-encoding vector %s: %v", f.name, err)
		}
	}
	if _, err := os.Stat("../test/message-one-encoding/One.schema"); err != nil {
		t.Errorf("message-one-encoding unit: %v", err)
	}
	manifestData, err := os.ReadFile(filepath.Join(vectors, "README.md"))
	if err != nil {
		t.Fatalf("message-one-encoding manifest: %v", err)
	}
	manifest := string(manifestData)
	for _, f := range files {
		named, verdicted := false, false
		for _, line := range strings.Split(manifest, "\n") {
			if !strings.Contains(line, "`"+f.name+"`") {
				continue
			}
			named = true
			if strings.Contains(line, "| "+f.verdict+" |") {
				verdicted = true
			}
		}
		if !named {
			t.Errorf("message-one-encoding manifest does not name %s", f.name)
		}
		if !verdicted {
			t.Errorf("message-one-encoding manifest does not give %s the verdict %s", f.name, f.verdict)
		}
	}
}
