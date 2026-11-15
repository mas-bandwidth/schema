// TestCorpusNonCanonicalSpellings pins the shared vectors for the 2026-11-10
// rule (docs/rulings.json reads-refuse-non-canonical): one canonical file and
// one file per non-canonical spelling the standard names, beside it in the
// vector directory. The test holds the manifest to the files, the generator to
// the committed bytes, and the engine's canonical read to a byte-identical
// save. It does not assert the engine's verdict on the non-canonical cases;
// the engine card does that.
package compiler

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

type nonCanonicalRow struct {
	file     string
	form     string
	spelling string
	verdict  string
}

func TestCorpusNonCanonicalSpellings(t *testing.T) {
	t.Parallel()

	const vector = "../testdata/wire/non-canonical"
	manifestData, err := os.ReadFile(filepath.Join(vector, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []nonCanonicalRow
	named := map[string]bool{}
	for i, line := range strings.Split(string(manifestData), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want file, form, case, verdict", i+1, len(f))
		}
		if f[0] == "file" && f[1] == "form" && f[2] == "case" && f[3] == "verdict" {
			continue
		}
		row := nonCanonicalRow{file: f[0], form: f[1], spelling: f[2], verdict: f[3]}
		switch row.form {
		case "variable", "message", "announcement":
		default:
			t.Fatalf("MANIFEST.tsv:%d: form %q, want variable, message or announcement", i+1, row.form)
		}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, row.verdict)
		}
		if named[row.file] {
			t.Fatalf("MANIFEST.tsv:%d: %s is named twice", i+1, row.file)
		}
		named[row.file] = true
		if _, err := os.Stat(filepath.Join(vector, row.file)); err != nil {
			t.Errorf("MANIFEST.tsv names %s which is not on disk: %v", row.file, err)
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv is empty")
	}

	// Every local file is named by the manifest, and every manifest row that
	// names a local file is a file. Rows may also reference a vector this unit
	// does not own (the message-one-encoding fixture).
	entries, err := os.ReadDir(vector)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == "MANIFEST.tsv" {
			continue
		}
		if !named[e.Name()] {
			t.Errorf("%s is not named by MANIFEST.tsv", e.Name())
		}
	}

	u, err := New().Load([]string{"../test/non-canonical/NonCanonical.schema"})
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	var vocabulary tablewire.Vocabulary

	canonicalFor := func(row nonCanonicalRow) string {
		switch row.form {
		case "variable":
			return "variable-canonical.bin"
		case "message":
			if strings.HasPrefix(row.file, "../message-one-encoding/") {
				return "../message-one-encoding/canonical.bin"
			}
			return "message-canonical.bin"
		}
		return row.file
	}

	for _, row := range rows {
		data, err := os.ReadFile(filepath.Join(vector, row.file))
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case row.form == "variable" && row.verdict == "read":
			inst := m.New(m.Lookup("Root"))
			var report tabletext.Report
			ok, err := tablewire.Decode(m, inst, data, &report)
			if !ok || err != nil || report.Refused {
				t.Errorf("%s: canonical read: ok=%v err=%v report=%+v", row.file, ok, err, report)
				continue
			}
			saved, err := tablewire.Encode(m, inst)
			if err != nil {
				t.Errorf("%s: canonical save: %v", row.file, err)
				continue
			}
			if !bytes.Equal(saved, data) {
				t.Errorf("%s: canonical save is not the same bytes", row.file)
			}
		case row.form == "message" && row.verdict == "read":
			announcement, err := os.ReadFile(filepath.Join(vector, "announcement.bin"))
			if err != nil {
				t.Fatal(err)
			}
			var read tablewire.Vocabulary
			var report tabletext.Report
			if err := read.AnnounceRead(announcement, &report); err != nil {
				t.Errorf("%s: announcement: %v (%+v)", row.file, err, report)
				continue
			}
			inst := m.New(m.Lookup("Message"))
			ok, err := tablewire.DecodeMessage(m, inst, data, &read, &report)
			if !ok || err != nil || report.Refused {
				t.Errorf("%s: canonical read: ok=%v err=%v report=%+v", row.file, ok, err, report)
				continue
			}
			saved, err := tablewire.EncodeMessage(m, inst)
			if err != nil {
				t.Errorf("%s: canonical save: %v", row.file, err)
				continue
			}
			if !bytes.Equal(saved, data) {
				t.Errorf("%s: canonical save is not the same bytes", row.file)
			}
		case row.form == "announcement":
			var report tabletext.Report
			if err := vocabulary.AnnounceRead(data, &report); err != nil {
				t.Errorf("%s: announcement read: %v (%+v)", row.file, err, report)
				continue
			}
			if !bytes.Equal(tablewire.Announce(u), data) {
				t.Errorf("%s: the unit's announcement is not the same bytes", row.file)
			}
		}
		if row.verdict != "refused" {
			continue
		}
		canonical, err := os.ReadFile(filepath.Join(vector, canonicalFor(row)))
		if err != nil {
			t.Errorf("%s: canonical counterpart: %v", row.file, err)
			continue
		}
		if bytes.Equal(data, canonical) {
			t.Errorf("%s: the non-canonical vector is byte-identical to %s", row.file, canonicalFor(row))
		}
	}

	// The generator reproduces every file, the manifest included, byte for
	// byte. It starts from the engine's canonical bytes and recomputes every
	// LEB128, length and count it moves.
	t.Run("the generator reproduces the committed files", func(t *testing.T) {
		t.Parallel()
		slowtest.Gate(t, "the Go toolchain (it builds and runs the vector generator)")
		root, err := filepath.Abs("..")
		if err != nil {
			t.Fatal(err)
		}
		out := t.TempDir()
		cmd := exec.Command("go", "run", "./test/non-canonical/gen",
			"-schema", filepath.Join(root, "test", "non-canonical", "NonCanonical.schema"),
			"-out", out)
		cmd.Dir = root
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("generator: %v\n%s", err, b)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			committed, err := os.ReadFile(filepath.Join(vector, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			regenerated, err := os.ReadFile(filepath.Join(out, e.Name()))
			if err != nil {
				t.Errorf("the generator wrote no %s: %v", e.Name(), err)
				continue
			}
			if !bytes.Equal(committed, regenerated) {
				t.Errorf("the generator did not reproduce %s:\n committed   %x\n regenerated %x", e.Name(), committed, regenerated)
			}
		}
	})
}
