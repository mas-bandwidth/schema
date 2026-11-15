// TestCorpusNonCanonicalSpellings pins the shared vectors for the rule that a
// read of the variable form or the message form refuses a non-canonical
// spelling of a good value (docs/SPEC-TABLES.md §3, §3.1, §3.3, §4;
// docs/rulings.json reads-refuse-non-canonical). The vectors live under
// testdata/wire/non-canonical, beside test/non-canonical/NonCanonical.schema
// and its generator. The engine's verdict on each case is not asserted here:
// that is the engine card's test.
package compiler

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestCorpusNonCanonicalSpellings(t *testing.T) {
	t.Parallel()

	const (
		schemaDir = "../test/non-canonical"
		vectorDir = "../testdata/wire/non-canonical"
	)

	manifest, err := os.ReadFile(filepath.Join(vectorDir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("vectors manifest: %v", err)
	}
	rows := parseNonCanonicalRows(t, string(manifest))

	t.Run("the manifest names every file with a verdict", func(t *testing.T) {
		t.Parallel()
		named := map[string]bool{}
		for _, row := range rows {
			named[row.file] = true
			if _, err := os.Stat(filepath.Join(vectorDir, row.file)); err != nil {
				t.Errorf("MANIFEST.tsv names %s: %v", row.file, err)
			}
			switch row.verdict {
			case "read", "refused":
			default:
				t.Errorf("%s: verdict %q, want read or refused", row.file, row.verdict)
			}
		}
		entries, err := os.ReadDir(vectorDir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".bin" {
				continue
			}
			if !named[entry.Name()] {
				t.Errorf("%s is not named by MANIFEST.tsv", entry.Name())
			}
		}
	})

	t.Run("the generator reproduces every file byte for byte", func(t *testing.T) {
		t.Parallel()
		root, err := filepath.Abs("..")
		if err != nil {
			t.Fatal(err)
		}
		work := t.TempDir()
		run := exec.Command("go", "run", "./test/non-canonical/gen", "--out", work)
		run.Dir = root
		if out, err := run.CombinedOutput(); err != nil {
			t.Fatalf("running the generator: %v\n%s", err, out)
		}
		for _, row := range rows {
			if filepath.Dir(row.file) != "." {
				continue // a case that lives in another corpus directory
			}
			committed, err := os.ReadFile(filepath.Join(vectorDir, row.file))
			if err != nil {
				t.Fatalf("%s: %v", row.file, err)
			}
			regenerated, err := os.ReadFile(filepath.Join(work, row.file))
			if err != nil {
				t.Fatalf("the generator wrote no %s: %v", row.file, err)
			}
			if !bytes.Equal(committed, regenerated) {
				t.Errorf("%s: the generator no longer reproduces the committed vector:\n committed %x\ngenerated %x", row.file, committed, regenerated)
			}
		}
		committed, err := os.ReadFile(filepath.Join(vectorDir, "MANIFEST.tsv"))
		if err != nil {
			t.Fatal(err)
		}
		regenerated, err := os.ReadFile(filepath.Join(work, "MANIFEST.tsv"))
		if err != nil {
			t.Fatalf("the generator wrote no MANIFEST.tsv: %v", err)
		}
		if !bytes.Equal(committed, regenerated) {
			t.Errorf("MANIFEST.tsv is not the generator's own text:\n committed %q\ngenerated %q", committed, regenerated)
		}
	})

	t.Run("each canonical file reads and re-saves to the same bytes", func(t *testing.T) {
		t.Parallel()
		m := loadNonCanonicalModel(t, schemaDir)
		canonical := 0
		for _, row := range rows {
			if row.caseName != "canonical" || row.verdict != "read" {
				continue
			}
			canonical++
			data, err := os.ReadFile(filepath.Join(vectorDir, row.file))
			if err != nil {
				t.Fatalf("%s: %v", row.file, err)
			}
			var saved []byte
			switch row.form {
			case "variable":
				inst := m.New(m.Lookup("Root"))
				var report tabletext.Report
				if ok, err := tablewire.Decode(m, inst, data, &report); !ok || err != nil {
					t.Fatalf("%s: the canonical variable file did not read: ok=%v err=%v report=%+v", row.file, ok, err, report)
				}
				saved, err = tablewire.Encode(m, inst)
			case "message":
				inst := m.New(m.Lookup("Batch"))
				var vocab tablewire.Vocabulary
				var report tabletext.Report
				if err := vocab.AnnounceRead(tablewire.Announce(m.Unit), &report); err != nil {
					t.Fatalf("%s: the unit's announcement did not read: %v %+v", row.file, err, report)
				}
				if ok, err := tablewire.DecodeMessage(m, inst, data, &vocab, &report); !ok || err != nil {
					t.Fatalf("%s: the canonical message file did not read: ok=%v err=%v report=%+v", row.file, ok, err, report)
				}
				saved, err = tablewire.EncodeMessage(m, inst)
			default:
				t.Fatalf("%s: form %q, want variable or message", row.file, row.form)
			}
			if err != nil {
				t.Fatalf("%s: re-save: %v", row.file, err)
			}
			if !bytes.Equal(data, saved) {
				t.Errorf("%s: the canonical file does not re-save to its own bytes:\n read %x\nsaved %x", row.file, data, saved)
			}
		}
		if canonical != 2 {
			t.Errorf("MANIFEST.tsv names %d canonical files, want one per form (2)", canonical)
		}
	})

	t.Run("every other file differs from its canonical file", func(t *testing.T) {
		t.Parallel()
		for _, row := range rows {
			if row.caseName == "canonical" {
				continue
			}
			canonicalFile := nonCanonicalCompanion(row)
			want, err := os.ReadFile(filepath.Join(vectorDir, canonicalFile))
			if err != nil {
				t.Fatalf("%s: canonical companion %s: %v", row.file, canonicalFile, err)
			}
			got, err := os.ReadFile(filepath.Join(vectorDir, row.file))
			if err != nil {
				t.Fatalf("%s: %v", row.file, err)
			}
			if bytes.Equal(got, want) {
				t.Errorf("%s is byte for byte its canonical file %s", row.file, canonicalFile)
			}
		}
	})
}

type nonCanonicalRow struct {
	file     string
	form     string
	caseName string
	verdict  string
}

func parseNonCanonicalRows(t *testing.T, manifest string) []nonCanonicalRow {
	t.Helper()
	var rows []nonCanonicalRow
	for i, line := range strings.Split(manifest, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want 4 (file, form, case, verdict)", i+1, len(f))
		}
		if f[1] != "variable" && f[1] != "message" {
			t.Fatalf("MANIFEST.tsv:%d: form %q, want variable or message", i+1, f[1])
		}
		rows = append(rows, nonCanonicalRow{file: f[0], form: f[1], caseName: f[2], verdict: f[3]})
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	return rows
}

// nonCanonicalCompanion names the canonical file a row differs from: this
// directory's per-form canonical, or the canonical beside a case that lives in
// another corpus directory.
func nonCanonicalCompanion(row nonCanonicalRow) string {
	if filepath.Dir(row.file) != "." {
		return filepath.Join(filepath.Dir(row.file), "canonical.bin")
	}
	if row.form == "message" {
		return "message-canonical.bin"
	}
	return "variable-canonical.bin"
}

func loadNonCanonicalModel(t *testing.T, dir string) *tabletext.Model {
	t.Helper()
	paths, err := GatherPaths([]string{dir})
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	c := New()
	c.FormatInPlace = false
	u, err := c.Load(paths)
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	return tabletext.NewModel(u)
}
