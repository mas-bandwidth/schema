// TestSpecSkippedValueIsValidated pins the skipped-value-is-validated rule
// (docs/rulings.json): a value a reader SKIPS is held to what a read of it
// would hold, in every form, and damage inside it refuses the whole read.
// docs/SPEC-TABLES.md §16.3 states the JSON half — a skipped value and every
// object key are held to the text rules of a placed value, so a skipped string
// or key that decodes to U+0000 refuses the read — and retires the sentence
// that let a skipped zero pass. §4 names the three forms together. The shared
// vectors under testdata/wire/skipped-value/ are the port cards' input.
package compiler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpecSkippedValueIsValidated(t *testing.T) {
	t.Parallel()

	t.Run("§16.3 states the refusal and drops the stale sentence", func(t *testing.T) {
		t.Parallel()
		page := readSpecTables(t)
		section := specTablesSection(t, page, "### 16.3 What the writer refuses", "### 16.4 ")
		flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }
		text := flat(section)
		for _, want := range []string{
			"A string the read only SKIPS — the value of an unknown key, an object key, whether it names a field or none — is held to the grammar and text rules a placed value is held to",
			"one that decodes to `U+0000` refuses the read as a placed one does",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("§16.3 no longer states %q", want)
			}
		}
		if strings.Contains(text, "stores nothing and is not this event") {
			t.Error("§16.3 still states the stale sentence \"stores nothing and is not this event\"")
		}
	})

	t.Run("§4 names the three forms together", func(t *testing.T) {
		t.Parallel()
		page := readSpecTables(t)
		section := specTablesSection(t, page, "## 4. Versioning, and the one read", "## 5. Identity")
		flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }
		text := flat(section)
		for _, want := range []string{
			"held to what a read of it would hold in every form",
			"the message form's skipped payload (§3.3)",
			"the JSON form's skipped value and every object key (§16.3)",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("§4 no longer states %q", want)
			}
		}
	})

	t.Run("docs/rulings.json carries skipped-value-is-validated", func(t *testing.T) {
		t.Parallel()
		data, err := os.ReadFile("../docs/rulings.json")
		if err != nil {
			t.Fatalf("docs/rulings.json: %v", err)
		}
		var file struct {
			Rulings []struct {
				ID         string   `json:"id"`
				Date       string   `json:"date"`
				Quotes     []string `json:"quotes"`
				Source     string   `json:"source"`
				Supersedes []string `json:"supersedes"`
			} `json:"rulings"`
		}
		if err := json.Unmarshal(data, &file); err != nil {
			t.Fatalf("docs/rulings.json: %v", err)
		}
		const rulingText = "ruled 2026-11-10: a value a reader skips is held to what a read of it would hold, and damage inside it refuses the read, in every form"
		found := false
		for _, r := range file.Rulings {
			if r.ID != "skipped-value-is-validated" {
				continue
			}
			found = true
			if r.Date != "2026-11-10" {
				t.Errorf("skipped-value-is-validated: date %q, want 2026-11-10", r.Date)
			}
			if strings.Join(r.Quotes, "\n") != rulingText {
				t.Errorf("skipped-value-is-validated: quotes %q, want exactly %q", r.Quotes, rulingText)
			}
			if !strings.Contains(r.Source, "strict wins") || !strings.Contains(r.Source, "coordinator") {
				t.Errorf("skipped-value-is-validated: source does not state the standing rule and the coordinator: %q", r.Source)
			}
			supersedes := strings.Join(r.Supersedes, "\n")
			if !strings.Contains(supersedes, "§16.3") || !strings.Contains(supersedes, "stores nothing and is not this event") {
				t.Errorf("skipped-value-is-validated: supersedes does not name §16.3's stale sentence: %q", supersedes)
			}
		}
		if !found {
			t.Error("docs/rulings.json carries no ruling skipped-value-is-validated")
		}
	})

	t.Run("the manifest names every vector and every file is present", func(t *testing.T) {
		t.Parallel()
		const dir = "../testdata/wire/skipped-value"
		manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{
			"unknown-string-zero.json":        "refused",
			"unknown-string-control.json":     "read",
			"skipped-object-key-zero.json":    "refused",
			"skipped-object-key-control.json": "read",
			"announcement.bin":                "read",
			"canonical.bin":                   "read",
			"string-zero.bin":                 "refused",
			"string-illformed.bin":            "refused",
			"float-nan.bin":                   "refused",
			"quant-index-over.bin":            "refused",
			"array-count-over.bin":            "refused",
			"keyed-repeat.bin":                "refused",
			"keyed-payload-entry.bin":         "refused",
			"nested-id-twice.bin":             "refused",
		}
		seen := map[string]bool{}
		for i, line := range strings.Split(string(manifest), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			f := strings.Split(line, "\t")
			if len(f) != 3 {
				t.Fatalf("MANIFEST.tsv:%d: a vector line is <file> TAB <form> TAB <verdict>, got %q", i+1, line)
			}
			name, form, verdict := f[0], f[1], f[2]
			if form != "json" && form != "message" {
				t.Errorf("MANIFEST.tsv:%d: form %q, want json or message", i+1, form)
			}
			expected, ok := want[name]
			if !ok {
				t.Errorf("MANIFEST.tsv names an unexpected vector %q", name)
				continue
			}
			seen[name] = true
			if verdict != expected {
				t.Errorf("MANIFEST.tsv gives %q verdict %q, want %q", name, verdict, expected)
			}
			if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
				t.Errorf("MANIFEST.tsv names %s: %v", name, err)
			}
		}
		for name := range want {
			if !seen[name] {
				t.Errorf("MANIFEST.tsv omits vector %q", name)
			}
		}
		// Every vector file in the directory is named, and nothing else is.
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() || e.Name() == "MANIFEST.tsv" {
				continue
			}
			if !seen[e.Name()] {
				t.Errorf("the file %q is not named in MANIFEST.tsv", e.Name())
			}
		}
	})
}
