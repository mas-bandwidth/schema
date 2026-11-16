package tablewire_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestEngineReadsRefuseNonCanonical holds the engine's read entries to the
// verdict of every row of testdata/wire/non-canonical/MANIFEST.tsv
// (docs/rulings.json reads-refuse-non-canonical): the variable-form read (the
// strict decode and the retaining decode) and the message-form read refuse a
// non-canonical spelling of a good value and read every canonical file. The
// verdict is the contract; the reason is per-language detail and is not
// compared.
func TestEngineReadsRefuseNonCanonical(t *testing.T) {
	t.Parallel()

	const vector = "../../testdata/wire/non-canonical"
	manifestData, err := os.ReadFile(filepath.Join(vector, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}

	type row struct {
		file    string
		form    string
		verdict string
	}
	var rows []row
	for i, line := range strings.Split(string(manifestData), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want file, form, case, verdict", i+1, len(f))
		}
		if f[0] == "file" && f[1] == "form" && f[2] == "case" && f[3] == "verdict" {
			continue
		}
		rows = append(rows, row{file: f[0], form: f[1], verdict: f[3]})
	}

	nc := loadNonCanonicalModel(t, filepath.Join("..", "..", "test", "non-canonical", "NonCanonical.schema"))
	one := loadNonCanonicalModel(t, filepath.Join("..", "..", "test", "message-one-encoding", "One.schema"))

	ncAnnouncement := readNonCanonicalFile(t, filepath.Join(vector, "announcement.bin"))
	oneAnnouncement := readNonCanonicalFile(t, filepath.Join(vector, "..", "message-one-encoding", "announcement.bin"))

	var ncVocab tablewire.Vocabulary
	if err := ncVocab.AnnounceRead(ncAnnouncement, &tabletext.Report{}); err != nil {
		t.Fatalf("non-canonical announcement: %v", err)
	}
	var oneVocab tablewire.Vocabulary
	if err := oneVocab.AnnounceRead(oneAnnouncement, &tabletext.Report{}); err != nil {
		t.Fatalf("message-one-encoding announcement: %v", err)
	}

	for _, r := range rows {
		data := readNonCanonicalFile(t, filepath.Join(vector, r.file))
		got := readVerdict(t, nc, one, &ncVocab, &oneVocab, r, data)
		if got != r.verdict {
			t.Errorf("%s: read verdict %q, want %q", r.file, got, r.verdict)
		}
	}
}

// readVerdict runs one manifest row through the read entry its form names and
// answers "read" or "refused".
func readVerdict(t *testing.T, nc, one *tabletext.Model, ncVocab, oneVocab *tablewire.Vocabulary, r struct {
	file    string
	form    string
	verdict string
}, data []byte) string {
	t.Helper()
	switch r.form {
	case "variable":
		verdict := "read"
		for _, retain := range []bool{false, true} {
			inst := nc.New(nc.Lookup("Root"))
			var ok bool
			var err error
			if retain {
				ok, err = tablewire.DecodeRetain(nc, inst, data, &tablewire.Retain{Capacity: 1024, IdCapacity: 64}, &tabletext.Report{})
			} else {
				ok, err = tablewire.Decode(nc, inst, data, &tabletext.Report{})
			}
			got := "read"
			if !ok || err != nil {
				got = "refused"
			}
			if got != r.verdict {
				t.Errorf("%s retain=%v: got %s, want %s", r.file, retain, got, r.verdict)
			}
			if !ok || err != nil {
				verdict = "refused"
			}
		}
		return verdict
	case "message":
		model, vocab, root := nc, ncVocab, "Message"
		if strings.HasPrefix(r.file, "../message-one-encoding/") {
			model, vocab, root = one, oneVocab, "One"
		}
		verdict := "read"
		for _, batch := range []bool{false, true} {
			inst := model.New(model.Lookup(root))
			v := *vocab
			var ok bool
			var err error
			if batch {
				_, ok, err = tablewire.DecodeMessages(model, []*tabletext.Instance{inst}, data, &v, &tabletext.Report{})
			} else {
				ok, err = tablewire.DecodeMessage(model, inst, data, &v, &tabletext.Report{})
			}
			got := "read"
			if !ok || err != nil {
				got = "refused"
			}
			if got != r.verdict {
				t.Errorf("%s batch=%v: got %s, want %s", r.file, batch, got, r.verdict)
			}
			if !ok || err != nil {
				verdict = "refused"
			}
		}
		return verdict
	case "announcement":
		var vocab tablewire.Vocabulary
		if err := vocab.AnnounceRead(data, &tabletext.Report{}); err != nil {
			return "refused"
		}
		return "read"
	}
	t.Fatalf("manifest names form %q", r.form)
	return ""
}

func loadNonCanonicalModel(t *testing.T, path string) *tabletext.Model {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f, perrs := parser.Parse(filepath.Base(path), data)
	if len(perrs) > 0 {
		t.Fatalf("parse %s: %v", path, perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: filepath.Base(path), Name: filepath.Base(path), Base: "probe", Bytes: data, AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", path, cerrs[0])
	}
	return tabletext.NewModel(u)
}

func readNonCanonicalFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
