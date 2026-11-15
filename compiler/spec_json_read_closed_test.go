// TestSpecJsonReadIsClosed pins docs/SPEC-TABLES.md §16.2's three read-closed
// rules (docs/rulings.json `json-read-closed`): a schema-name string — an
// object key naming a field, an enum variant name, a flags element, a union arm
// key, an `&node` name — is capped at 255 bytes and a longer one refuses, never
// a map key or a string value; the depth cap counts every container with the
// root at depth 0; and every read is handed bytes and validates UTF-8 itself.
// It also pins the vector corpus under testdata/wire/json-read-closed/.
package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpecJsonReadIsClosed(t *testing.T) {
	t.Parallel()

	t.Run("§16.2 states the three read-closed rules", func(t *testing.T) {
		t.Parallel()
		page := readSpecTables(t)
		section := specTablesSection(t, page, "### 16.2 ", "### 16.3 ")

		mustState(t, "§16.2 the 255-byte schema-name cap",
			section, "A SCHEMA-NAME STRING IS CAPPED AT 255 BYTES")
		mustState(t, "§16.2 the name sites",
			section, "an object key that names a field, an enum variant name, a flags element")
		mustState(t, "§16.2 the union arm and ampersand sites",
			section, "union arm key and an `&node` name")
		mustState(t, "§16.2 the cap reaches a skipped value",
			section, "in every object and inside a skipped value")
		mustState(t, "§16.2 an unknown enum name over 255 refuses and never reads None",
			section, "an unknown enum name over 255 bytes refuses")
		mustState(t, "§16.2 an unknown enum name never reads None",
			section, "and never reads `None`")
		mustState(t, "§16.2 the cap never reaches a map key or string value",
			section, "The cap never reaches a map key or a string value")
		mustState(t, "§16.2 the map row's no-key-refused-for-length stands",
			section, "row's \"no")
		mustState(t, "§16.2 the read's depth cap counts every object and array",
			section, "The read's cap counts every object and array the reader walks, placed or skipped")
		mustState(t, "§16.2 the depth cap's root sits at depth 0",
			section, "the root at depth 0")
		mustState(t, "§16.2 every JSON read is handed bytes",
			section, "Every JSON read is handed BYTES and validates UTF-8 itself")
		mustState(t, "§16.2 a string-taking read still offers a bytes entry",
			section, "also offers a bytes entry")
		mustNotState(t, "§16.2 no longer says the read counts table depth like the write",
			section, "and so does the read of an object that deep")
	})

	t.Run("docs/rulings.json carries json-read-closed", func(t *testing.T) {
		t.Parallel()
		r, ok := readRulings(t)["json-read-closed"]
		if !ok {
			t.Fatal("docs/rulings.json carries no ruling \"json-read-closed\"")
		}
		if r.Date != "2026-11-10" {
			t.Errorf("json-read-closed date is %q, want 2026-11-10", r.Date)
		}
		const rulingText = "ruled 2026-11-10: the JSON read refuses a schema name over 255 bytes, counts depth over every container, and is handed bytes"
		if len(r.Quotes) != 1 || r.Quotes[0] != rulingText {
			t.Errorf("json-read-closed quotes are %q, want exactly the ruling text %q", r.Quotes, rulingText)
		}
		const source = "the owner's standing rule of 2026-11-10 that where the standard is silent or lenient, strict wins, applied by the coordinator"
		if r.Source != source {
			t.Errorf("json-read-closed source is %q, want %q", r.Source, source)
		}
	})

	t.Run("the vectors exist and the manifest names each with its verdict", func(t *testing.T) {
		t.Parallel()
		const dir = "../testdata/wire/json-read-closed"
		want := []struct {
			name    string
			root    string
			verdict string
		}{
			{"field-key-255", "Closed", "read"},
			{"field-key-256", "Closed", "refused"},
			{"skipped-key-255", "Closed", "read"},
			{"skipped-key-256", "Closed", "refused"},
			{"enum-name-255", "Closed", "read"},
			{"enum-name-256", "Closed", "refused"},
			{"flags-element-255", "Closed", "read"},
			{"flags-element-256", "Closed", "refused"},
			{"union-arm-255", "Closed", "read"},
			{"union-arm-256", "Closed", "refused"},
			{"ampersand-255", "Scene", "read"},
			{"ampersand-256", "Scene", "refused"},
			{"map-key-long", "Closed", "read"},
			{"string-value-long", "Closed", "read"},
			{"depth-arrays-128", "Closed", "read"},
			{"depth-arrays-129", "Closed", "refused"},
			{"depth-objects-128", "Closed", "read"},
			{"depth-objects-129", "Closed", "refused"},
			{"lone-surrogate", "Closed", "refused"},
			{"illformed-utf8", "Closed", "refused"},
		}

		data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
		if err != nil {
			t.Fatalf("the vectors' manifest: %v", err)
		}
		type row struct {
			root    string
			json    string
			verdict string
		}
		named := make(map[string]row)
		for i, line := range strings.Split(string(data), "\n") {
			line = strings.TrimRight(line, "\r")
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Split(line, "\t")
			if len(fields) != 5 || fields[0] == "" || fields[1] == "" || fields[2] == "" || fields[3] == "" {
				t.Fatalf("%s/MANIFEST.tsv:%d: a vector line is <name> TAB <root> TAB <json> TAB <verdict> TAB <what it holds>, got %q", dir, i+1, line)
			}
			name, root, jsonFile, verdict := fields[0], fields[1], fields[2], fields[3]
			if verdict != "read" && verdict != "refused" {
				t.Errorf("%s/MANIFEST.tsv:%d: %q has verdict %q, want read or refused", dir, i+1, name, verdict)
			}
			if _, dup := named[name]; dup {
				t.Errorf("%s/MANIFEST.tsv:%d: %q is named twice", dir, i+1, name)
			}
			named[name] = row{root: root, json: jsonFile, verdict: verdict}
		}

		if len(named) != len(want) {
			t.Errorf("%s/MANIFEST.tsv names %d vectors, want %d", dir, len(named), len(want))
		}
		for _, v := range want {
			r, ok := named[v.name]
			if !ok {
				t.Errorf("%s/MANIFEST.tsv names no vector %q", dir, v.name)
				continue
			}
			if r.root != v.root {
				t.Errorf("%s: root %q, want %q", v.name, r.root, v.root)
			}
			wantFile := v.name + "." + v.verdict + ".json"
			if r.json != wantFile {
				t.Errorf("%s: json file %q, want %q", v.name, r.json, wantFile)
			}
			if r.verdict != v.verdict {
				t.Errorf("%s: verdict %q, want %q", v.name, r.verdict, v.verdict)
			}
			if info, err := os.Stat(filepath.Join(dir, wantFile)); err != nil {
				t.Errorf("the vector %q: %v", wantFile, err)
			} else if info.IsDir() || info.Size() == 0 {
				t.Errorf("the vector %q is not a non-empty file", wantFile)
			}
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("the vectors' directory: %v", err)
		}
		files := make(map[string]bool, len(named))
		for _, r := range named {
			files[r.json] = true
		}
		for _, e := range entries {
			if e.IsDir() || e.Name() == "MANIFEST.tsv" {
				continue
			}
			if !files[e.Name()] {
				t.Errorf("the vector %q is not named in MANIFEST.tsv", e.Name())
			}
		}
	})
}
