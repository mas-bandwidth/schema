// TestSpecFixedFormEdges pins the three edges of the fixed form the pages
// leave undefined, each stated as a rule (docs/rulings.json fixed-form-edges,
// ruled 2026-11-10):
//
//  1. a fixed-form file holds at least one record — a load refuses a file whose
//     header and layout are followed by no record, and a save refuses an empty
//     batch (FIXED-FORM-ALGORITHM.md §5.3 step 9, SPEC-TABLES.md §3.4 and §22.6);
//  2. a record body of 65536 bytes is lawful and 65537 is not, measured on the
//     body and never on 8 + body (FIXED-FORM-ALGORITHM.md §6);
//  3. the lock refuses two lineage entries with one hash (FIXED-FORM-ALGORITHM.md §5.2).
//
// It also holds the vector complete and honest: testdata/wire/fixed-empty-batch/
// carries a MANIFEST.tsv naming one one-record file (read) and one no-record
// file (refused), and the generator under test/fixed-empty-batch/gen/ reproduces
// both committed files byte for byte from the engine's one-record bytes, with
// the no-record file's integrity trailer recomputed.
package compiler

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestSpecFixedFormEdges(t *testing.T) {
	t.Parallel()

	flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	const algo = "../docs/FIXED-FORM-ALGORITHM.md"
	const tables = "../docs/SPEC-TABLES.md"

	// (1) the three rules are stated, each in the section that owns it.
	load := flat(specSection(t, algo, "### 5.3 LOAD", "### 5.4 "))
	mustState(t, "§5.3 LOAD holds at least one record", load, "A FIXED-FORM FILE HOLDS AT LEAST ONE RECORD")
	mustState(t, "§5.3 LOAD refuses a header and layout followed by no record", load, "a header and a layout followed by no record")
	mustState(t, "§5.3 LOAD refuses n = 0", load, "rest == 0")

	bounds := flat(specSection(t, algo, "## 6. The bounds", "## 7. What a port"))
	mustState(t, "§6 the record-body bound is inclusive", bounds, "a record body of 65536 bytes is lawful and 65537 is not")
	mustState(t, "§6 the bound is measured on the body", bounds, "measured on the BODY, never on `8 + body`")

	compile := flat(specSection(t, algo, "### 5.2 COMPILE", "### 5.3 LOAD"))
	mustState(t, "§5.2 the lock refuses two lineage entries with one hash", compile, "THE LOCK REFUSES TWO LINEAGE ENTRIES WITH ONE HASH")

	// The write side: a save refuses an empty batch (§22.6), and the read side
	// refuses a file with no record (§3.4's "THE RECORDS FILL THE REST").
	save := flat(specSection(t, tables, "### 22.6 Forms", "### 22.7 "))
	mustState(t, "§22.6 a save refuses an empty batch", save, "a save refuses an empty batch")
	records := flat(specSection(t, tables, "**THE RECORDS FILL THE REST OF THE FILE", "**THE INTEGRITY TRAILER"))
	mustState(t, "§3.4 a file whose header and layout are followed by no record is refused", records, "a fixed-form file holds at least one record")

	// (2) the rulings entry: id, date, the one ruling sentence, and a source
	// that names no person.
	raw, err := os.ReadFile("../docs/rulings.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Rulings []rulingEntry `json:"rulings"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	entry := findRuling(t, doc.Rulings, "fixed-form-edges")
	if entry.Date != "2026-11-10" {
		t.Errorf("ruling fixed-form-edges date = %q, want 2026-11-10", entry.Date)
	}
	const rulingText = "ruled 2026-11-10: a fixed-form file holds at least one record; a record body of 65536 bytes is lawful and 65537 is not; the lock refuses two lineage entries with one hash"
	if len(entry.Quotes) != 1 || entry.Quotes[0] != rulingText {
		t.Errorf("ruling fixed-form-edges quotes = %q, want %q", entry.Quotes, rulingText)
	}
	if !strings.Contains(entry.Source, "strict wins") {
		t.Errorf("ruling fixed-form-edges source = %q, want the owner's standing rule that strict wins", entry.Source)
	}

	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	vector := filepath.Join(root, "testdata", "wire", "fixed-empty-batch")

	// (3) the manifest is complete: one read file and one refused file, each
	// naming the shared schema and table and each present on disk.
	manifest := readManifest(t, filepath.Join(vector, "MANIFEST.tsv"))
	if len(manifest) != 2 {
		t.Fatalf("MANIFEST.tsv names %d rows, want 2", len(manifest))
	}
	byName := map[string]manifestRow{}
	for _, r := range manifest {
		byName[r.name] = r
	}
	for name, want := range map[string]manifestRow{
		"one-record.bin": {"one-record.bin", "test/fixed-empty-batch/Batch.schema", "Batch", "read", "-"},
		"no-record.bin":  {"no-record.bin", "test/fixed-empty-batch/Batch.schema", "Batch", "refused", "truncated"},
	} {
		got, ok := byName[name]
		if !ok {
			t.Errorf("MANIFEST.tsv has no row for %s", name)
			continue
		}
		if got != want {
			t.Errorf("MANIFEST.tsv row for %s = %+v, want %+v", name, got, want)
		}
		if _, err := os.Stat(filepath.Join(vector, name)); err != nil {
			t.Errorf("MANIFEST.tsv names %s which is not on disk: %v", name, err)
		}
	}

	// (4) the generator reproduces both files byte for byte.
	out := t.TempDir()
	cmd := exec.Command("go", "run", "./test/fixed-empty-batch/gen",
		"-schema", filepath.Join(root, "test", "fixed-empty-batch", "Batch.schema"),
		"-out", out)
	cmd.Dir = root
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generator: %v\n%s", err, b)
	}
	for _, name := range []string{"one-record.bin", "no-record.bin"} {
		committed, err := os.ReadFile(filepath.Join(vector, name))
		if err != nil {
			t.Fatal(err)
		}
		regenerated, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(committed, regenerated) {
			t.Errorf("generator did not reproduce %s:\n committed  %x\n regenerated %x", name, committed, regenerated)
		}
	}

	// The vector means what the manifest says: the engine reads the one-record
	// file and refuses the no-record file as truncated.
	m := loadBatchModel(t, root)
	for _, r := range manifest {
		data, err := os.ReadFile(filepath.Join(vector, r.name))
		if err != nil {
			t.Fatal(err)
		}
		inst := m.New(m.Lookup("Batch"))
		var report tabletext.Report
		ok, err := tablewire.Decode(m, inst, data, &report)
		switch r.verdict {
		case "read":
			if !ok || err != nil {
				t.Errorf("%s: read refused: ok=%v err=%v report=%+v", r.name, ok, err, report)
			}
		case "refused":
			if ok || tablewire.RefusalReason(err) != r.reason {
				t.Errorf("%s: read ok=%v reason=%q, want the %s refusal", r.name, ok, tablewire.RefusalReason(err), r.reason)
			}
		}
	}
}

type manifestRow struct {
	name, schema, table, verdict, reason string
}

func readManifest(t *testing.T, path string) []manifestRow {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []manifestRow
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 5 {
			t.Fatalf("%s:%d: want 5 tab-separated columns, got %d: %q", path, i+1, len(cols), line)
		}
		rows = append(rows, manifestRow{cols[0], cols[1], cols[2], cols[3], cols[4]})
	}
	return rows
}

func loadBatchModel(t *testing.T, root string) *tabletext.Model {
	t.Helper()
	u, err := New().Load([]string{filepath.Join(root, "test", "fixed-empty-batch", "Batch.schema")})
	if err != nil {
		t.Fatal(err)
	}
	if u.Tables["Batch"] == nil {
		t.Fatal("schema has no fixed table Batch")
	}
	return tabletext.NewModel(u)
}

type rulingEntry struct {
	ID     string   `json:"id"`
	Date   string   `json:"date"`
	Quotes []string `json:"quotes"`
	Source string   `json:"source"`
}

func findRuling(t *testing.T, rules []rulingEntry, id string) rulingEntry {
	t.Helper()
	for _, r := range rules {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("docs/rulings.json has no ruling %q", id)
	return rulingEntry{}
}
