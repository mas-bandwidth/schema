package goosetable

// TestGooseSkippedValueIsValidated holds the Goose leg to the skipped-value
// rule (docs/SPEC-TABLES.md §4, §3.3, §16.3; docs/rulings.json
// skipped-value-is-validated): a value the reader SKIPS is held to what a read
// of it would hold, and damage inside it refuses the whole read. The vectors
// under testdata/wire/skipped-value/ are read with the reader unit
// (test/skipped-value/reader), so every writer field other than `known` takes
// the skip path: the message rows decode against the writer's announcement and
// the JSON rows read the text form. Each row's verdict is the manifest's.

import (
	"bufio"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type skippedValueRow struct {
	name, form, verdict string
}

func skippedValueRows(t *testing.T) []skippedValueRow {
	t.Helper()
	const dir = "testdata/wire/skipped-value"
	f, err := os.Open(filepath.Join(fixedRoot, dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []skippedValueRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fs := strings.Split(line, "\t")
		if len(fs) != 3 {
			t.Fatalf("MANIFEST.tsv: a row is <file> TAB <form> TAB <verdict>, got %q", line)
		}
		if fs[1] != "json" && fs[1] != "message" {
			t.Fatalf("MANIFEST.tsv: %s has form %q, want json or message", fs[0], fs[1])
		}
		if fs[2] != "read" && fs[2] != "refused" {
			t.Fatalf("MANIFEST.tsv: %s has verdict %q, want read or refused", fs[0], fs[2])
		}
		rows = append(rows, skippedValueRow{name: fs[0], form: fs[1], verdict: fs[2]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return rows
}

func skippedValueReader(t *testing.T) *ir.Unit {
	t.Helper()
	u, err := loadSchemaPaths([]string{filepath.Join(fixedRoot, "test/skipped-value/reader/Reader.schema")})
	if err != nil {
		t.Fatalf("load the reader unit: %v", err)
	}
	return u
}

// skippedValueJSONInputs numbers the probe's inputs: a manifest row reads its
// vector, an inline row carries its text in the map.
func skippedValueJSONInputs(t *testing.T, rows []jsonRow, inline map[string][]byte) map[string][]byte {
	t.Helper()
	in := map[string][]byte{}
	for i, r := range rows {
		key := "in" + strconv.Itoa(i) + ".json"
		if data, ok := inline[r.name]; ok {
			in[key] = data
			continue
		}
		data, err := os.ReadFile(filepath.Join(fixedRoot, filepath.FromSlash(r.path)))
		if err != nil {
			t.Fatal(err)
		}
		in[key] = data
	}
	return in
}

func TestGooseSkippedValueIsValidated(t *testing.T) {
	t.Parallel()
	const dir = "testdata/wire/skipped-value"
	rows := skippedValueRows(t)
	u := skippedValueReader(t)
	announcement, err := os.ReadFile(filepath.Join(fixedRoot, dir, "announcement.bin"))
	if err != nil {
		t.Fatal(err)
	}

	var jsonRows []jsonRow
	for _, row := range rows {
		if row.form == "json" {
			jsonRows = append(jsonRows, jsonRow{
				name: row.name,
				root: "Root",
				path: dir + "/" + row.name,
				want: row.verdict,
			})
		}
	}
	// The shared vectors carry a zero in a skipped string and in a key, but
	// not a name the read meets twice. A skipped object's own keys and an
	// unknown field's key are held to §16.2's one-name rule as a placed key
	// is, two spellings that decode equal included, so these refuse too.
	inline := []struct{ name, text string }{
		{"unknown-field-key-repeat.json", `{"known":1,"extra":1,"extra":2}`},
		{"skipped-object-key-repeat.json", `{"known":1,"extra_nested":{"a":1,"a":2}}`},
		{"skipped-object-key-equal-spelling.json", `{"known":1,"extra_nested":{"a":1,"\u0061":2}}`},
	}
	inlineText := map[string][]byte{}
	for _, c := range inline {
		inlineText[c.name] = []byte(c.text)
		jsonRows = append(jsonRows, jsonRow{name: c.name, root: "Root", want: "refused"})
	}
	if len(jsonRows) > 0 {
		files, err := goose.Generate(u)
		if err != nil {
			t.Fatalf("goose type surface: %v", err)
		}
		tables, err := Generate(u)
		if err != nil {
			t.Fatalf("goose table surface: %v", err)
		}
		maps.Copy(files, tables)
		probeDir := t.TempDir()
		jsonProbe(t, u, files, probeDir, jsonRows, false)
		out := string(gooserun.Run(t, probeDir, "main.goose", skippedValueJSONInputs(t, jsonRows, inlineText)))
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		if len(lines) != len(jsonRows) {
			t.Fatalf("the JSON probe printed %d lines for %d rows:\n%s", len(lines), len(jsonRows), out)
		}
		for i, row := range jsonRows {
			if lines[i] != row.want {
				t.Errorf("%s: the JSON read answered %q, want %q", row.name, lines[i], row.want)
			}
		}
	}

	for _, row := range rows {
		if row.form != "message" {
			continue
		}
		row := row
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			data := announcement
			if row.name != "announcement.bin" {
				var err error
				data, err = os.ReadFile(filepath.Join(fixedRoot, dir, row.name))
				if err != nil {
					t.Fatal(err)
				}
			}
			r := gooseMsgProbe(t, gooseMsgCase{unit: u, root: "Root", data: data, announce: announcement, capacity: 1})
			if !strings.HasPrefix(r.announced, "1 ") {
				t.Fatalf("AnnounceRead refused %s: %q", row.name, r.announced)
			}
			if row.name == "announcement.bin" {
				// the row is the announcement itself: its verdict is
				// AnnounceRead's, read above.
				if row.verdict != "read" {
					t.Fatalf("MANIFEST.tsv gives the announcement verdict %q", row.verdict)
				}
				return
			}
			got := "refused"
			if r.loadN >= 1 && !r.loadMalformed {
				got = "read"
			}
			if got != row.verdict {
				t.Errorf("%s: the message load answered %q, want %q:\n%s", row.name, got, row.verdict, r.raw)
			}
		})
	}
}
