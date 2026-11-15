package lobstertable_test

// TestLobsterReadsRefuseNonCanonical holds the Lobster variable-form and
// message-form reads to the 2026-11-10 rule (docs/rulings.json
// reads-refuse-non-canonical): the shared vectors under
// testdata/wire/non-canonical/ decide the verdict. A canonical file reads
// through every read entry and re-saves to the same bytes; every other row is
// refused by the plain load, the strict load, the retaining load, the measure
// and the message load alike. The refusal's name is per-language detail, so
// this test asserts the verdict alone.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

type nonCanonicalVectorRow struct {
	file     string
	form     string
	spelling string
	verdict  string
	data     []byte
	input    string
}

type nonCanonicalVectorUnit struct {
	key          string
	schema       string
	announcement string
	rows         []nonCanonicalVectorRow
}

const nonCanonicalProgramHead = `import .schema_save
import .strict_and_refusals
import .retain_unknown
import .region_measure
import .message

def nc_variable(file:string, verdict:string):
    let data = read_file(file)
    assert data != nil
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.load("Root", data, rep)
    if verdict == "read":
        assert rec
        assert schema_rt.save(rec) == data
        let srep = schema_rt.LoadReport {}
        let strict, reason = schema_rt.load_strict("Root", data, srep)
        assert strict and reason == ""
        schema_rt.release(strict)
        let rrep = schema_rt.LoadReport {}
        let retained = schema_rt.Retain { capacity: 4096, id_capacity: 256, used: 0, ids: [], known: [] }
        let rrec = schema_rt.load_retain("Root", data, retained, rrep)
        assert rrec
        assert schema_rt.save(rrec) == data
        schema_rt.release(rrec)
        let n, whole, refused, why = schema_rt.load_measure("Root", data)
        assert n > 0 and whole and not refused and why == ""
    else:
        assert not rec
        let srep = schema_rt.LoadReport {}
        let strict, reason = schema_rt.load_strict("Root", data, srep)
        assert not strict and reason != ""
        let rrep = schema_rt.LoadReport {}
        let retained = schema_rt.Retain { capacity: 4096, id_capacity: 256, used: 0, ids: [], known: [] }
        let rrec = schema_rt.load_retain("Root", data, retained, rrep)
        assert not rrec
        let n, whole, refused, why = schema_rt.load_measure("Root", data)
        assert n == -1 and not whole and refused and why != ""
    if rec:
        schema_rt.release(rec)

def nc_message(root:string, announcement:string, file:string, verdict:string):
    let v = schema_rt.new_vocabulary(schema_rt.message_entries_here())
    let ann = read_file(announcement)
    assert ann != nil
    assert schema_rt.announce_read(v, ann, schema_rt.LoadReport {})
    let data = read_file(file)
    assert data != nil
    let rep = schema_rt.LoadReport {}
    let recs, count, ok = schema_rt.load_messages(root, v, data, rep, 256)
    if verdict == "read":
        assert ok and count >= 1 and length(recs) == count
        let saved, used = schema_rt.save_messages(recs, schema_rt.LoadReport {})
        assert used > 0 and saved == data
    else:
        assert not ok and count == 0 and length(recs) == 0

def nc_announcement(file:string):
    let data = read_file(file)
    assert data != nil
    let v = schema_rt.new_vocabulary(4096)
    assert schema_rt.announce_read(v, data, schema_rt.LoadReport {})
    assert schema_rt.announce() == data
`

func nonCanonicalManifest(t *testing.T, path string) []nonCanonicalVectorRow {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []nonCanonicalVectorRow
	for i, line := range strings.Split(string(data), "\n") {
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
		row := nonCanonicalVectorRow{file: f[0], form: f[1], spelling: f[2], verdict: f[3]}
		switch row.form {
		case "variable", "message", "announcement":
		default:
			t.Fatalf("MANIFEST.tsv:%d: form %q, want variable, message or announcement", i+1, row.form)
		}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, row.verdict)
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv is empty")
	}
	return rows
}

func TestLobsterReadsRefuseNonCanonical(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lobster toolchain (it runs the generated non-canonical reader)")

	vectorDir := filepath.Join(root, "testdata", "wire", "non-canonical")
	messageOneDir := filepath.Join(root, "testdata", "wire", "message-one-encoding")
	rows := nonCanonicalManifest(t, filepath.Join(vectorDir, "MANIFEST.tsv"))

	units := map[string]*nonCanonicalVectorUnit{
		"noncanonical": {
			key:          "noncanonical",
			schema:       filepath.Join(root, "test", "non-canonical", "NonCanonical.schema"),
			announcement: filepath.Join(vectorDir, "announcement.bin"),
		},
		"messageone": {
			key:          "messageone",
			schema:       filepath.Join(root, "test", "message-one-encoding", "One.schema"),
			announcement: filepath.Join(messageOneDir, "announcement.bin"),
		},
	}
	for i := range rows {
		dir := vectorDir
		unit := "noncanonical"
		if strings.HasPrefix(rows[i].file, "../message-one-encoding/") {
			dir = messageOneDir
			unit = "messageone"
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.Base(rows[i].file)))
		if err != nil {
			t.Fatalf("MANIFEST.tsv names %s which is not on disk: %v", rows[i].file, err)
		}
		rows[i].data = data
		rows[i].input = fmt.Sprintf("row%d", i)
		units[unit].rows = append(units[unit].rows, rows[i])
	}

	handled := 0
	for _, key := range []string{"noncanonical", "messageone"} {
		u := units[key]
		if len(u.rows) == 0 {
			continue
		}
		g, err := generateLoadUnit(t, []string{u.schema})
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		inputs := map[string][]byte{"announcement": mustReadFile(t, u.announcement)}
		var program strings.Builder
		for _, mod := range g.mods {
			fmt.Fprintf(&program, "import .%s\n", mod)
		}
		program.WriteString(nonCanonicalProgramHead)
		for _, row := range u.rows {
			inputs[row.input] = row.data
			switch row.form {
			case "variable":
				fmt.Fprintf(&program, "nc_variable(%s, %s)\n", lobster.StringLit([]byte(row.input)), lobster.StringLit([]byte(row.verdict)))
			case "message":
				rootName := "Message"
				if key == "messageone" {
					rootName = "One"
				}
				fmt.Fprintf(&program, "nc_message(%s, \"announcement\", %s, %s)\n",
					lobster.StringLit([]byte(rootName)), lobster.StringLit([]byte(row.input)), lobster.StringLit([]byte(row.verdict)))
			case "announcement":
				fmt.Fprintf(&program, "nc_announcement(%s)\n", lobster.StringLit([]byte(row.input)))
			}
			handled++
		}
		program.WriteString("print(\"non-canonical passed\")\n")
		if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		out := strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)))
		if out != "non-canonical passed" {
			t.Fatalf("%s: %s", key, out)
		}
	}
	if handled != len(rows) {
		t.Fatalf("reached %d of %d manifest rows", handled, len(rows))
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
