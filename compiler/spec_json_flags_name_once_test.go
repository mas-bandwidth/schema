// TestSpecJsonFlagsNameOnce pins docs/SPEC-TABLES.md §16.2's rule for the
// JSON form of a flags value: a flags array that names one KNOWN variant twice
// refuses the read `malformed`, as a repeated key does. An unknown name keeps
// its treatment, skipped and counted `unknown`; this rule does not reach one.
// The vectors under testdata/wire/json-flags-name-once state the rule for the
// port cards, and the Go engine answers each with the verdict the manifest
// names.
package compiler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func TestSpecJsonFlagsNameOnce(t *testing.T) {
	t.Parallel()

	page, err := os.ReadFile("../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	mapping := specTablesSection(t, string(page),
		"### 16.2 The mapping, field kind by field kind", "### 16.3 ")
	for _, want := range []string{
		"a KNOWN variant named twice in one array refuses `malformed`, as a repeated key does",
		"a conforming writer writes each set bit's name once",
	} {
		mustState(t, "§16.2 the flags row", mapping, want)
	}
	mustNotState(t, "§16.2 the flags row", mapping,
		"a KNOWN variant named twice in one array is skipped")

	// THE RULING (docs/rulings.json): the entry the standard card carries, in
	// the file's own quote form. The date and the text are the contract.
	data, err := os.ReadFile("../docs/rulings.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Rulings []struct {
			ID     string   `json:"id"`
			Date   string   `json:"date"`
			Quotes []string `json:"quotes"`
			Source string   `json:"source"`
		} `json:"rulings"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("docs/rulings.json: %v", err)
	}
	const wantRuling = "ruled 2026-11-10: a JSON flags array that names one known variant twice is refused, as a repeated key is"
	var found bool
	for _, r := range file.Rulings {
		if r.ID != "json-flags-name-once" {
			continue
		}
		found = true
		if r.Date != "2026-11-10" {
			t.Errorf("docs/rulings.json json-flags-name-once: date %q, want 2026-11-10", r.Date)
		}
		if len(r.Quotes) != 1 || r.Quotes[0] != wantRuling {
			t.Errorf("docs/rulings.json json-flags-name-once: quotes %q, want exactly %q", r.Quotes, wantRuling)
		}
		if r.Source == "" {
			t.Error("docs/rulings.json json-flags-name-once: no source")
		}
	}
	if !found {
		t.Fatal("docs/rulings.json carries no ruling json-flags-name-once")
	}

	// THE VECTORS: one text per case, and the manifest names each with the
	// schema, the root and the verdict.
	const dir = "../testdata/wire/json-flags-name-once"
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	rows := flagsNameOnceRows(t, string(manifest))
	wantCases := []string{
		"known-name",
		"two-known-names",
		"known-name-twice",
		"known-name-twice-separated",
		"known-name-twice-in-unknown-key",
		"unknown-beside-known",
	}
	if len(rows) != len(wantCases) {
		t.Fatalf("MANIFEST.tsv names %d cases, want %d", len(rows), len(wantCases))
	}
	for i, want := range wantCases {
		if rows[i].name != want {
			t.Errorf("MANIFEST.tsv row %d is %q, want %q", i+1, rows[i].name, want)
		}
	}

	// Every text in the directory is named by the manifest, and every name
	// resolves to a text.
	texts, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	named := map[string]bool{}
	for _, row := range rows {
		named[row.text] = true
		if _, err := os.Stat(filepath.Join(dir, row.text)); err != nil {
			t.Errorf("MANIFEST.tsv names %s: %v", row.text, err)
		}
	}
	for _, text := range texts {
		if !named[filepath.Base(text)] {
			t.Errorf("%s is not named by MANIFEST.tsv", text)
		}
	}

	// The Go engine gives each text the verdict the manifest names.
	models := map[string]*tabletext.Model{}
	for _, row := range rows {
		model := models[row.unit]
		if model == nil {
			paths, err := GatherPaths([]string{filepath.Join("..", row.unit)})
			if err != nil {
				t.Fatalf("%s: %v", row.unit, err)
			}
			u, err := New().Load(paths)
			if err != nil {
				t.Fatalf("%s: %v", row.unit, err)
			}
			model = tabletext.NewModel(u)
			models[row.unit] = model
		}
		root := model.Lookup(row.root)
		if root == nil {
			t.Fatalf("%s declares no root %s", row.unit, row.root)
		}
		text, err := os.ReadFile(filepath.Join(dir, row.text))
		if err != nil {
			t.Fatalf("%s: %v", row.text, err)
		}
		var report tabletext.Report
		read := model.Read(model.New(root), text, &report)
		got := "read"
		if !read {
			got = "refused"
		}
		if got != row.verdict {
			t.Errorf("%s: verdict %s, want %s (%+v)", row.name, got, row.verdict, report)
		}
		if read == report.Refused {
			t.Errorf("%s: verdict %v disagrees with report.Refused=%v", row.name, read, report.Refused)
		}
		if row.unknown != nil && report.Unknown != *row.unknown {
			t.Errorf("%s: unknown %d, want %d", row.name, report.Unknown, *row.unknown)
		}
	}
}

type flagsNameOnceRow struct {
	name    string
	unit    string
	root    string
	text    string
	verdict string
	unknown *int
}

func flagsNameOnceRows(t *testing.T, manifest string) []flagsNameOnceRow {
	t.Helper()
	var rows []flagsNameOnceRow
	for i, line := range strings.Split(manifest, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		row := flagsNameOnceRow{
			name:    f[0],
			unit:    f[1],
			root:    f[2],
			text:    f[3],
			verdict: f[4],
		}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, row.verdict)
		}
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil {
				t.Fatalf("MANIFEST.tsv:%d: unknown %q: %v", i+1, f[5], err)
			}
			row.unknown = &n
		}
		rows = append(rows, row)
	}
	return rows
}
