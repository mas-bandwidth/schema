package tabletext_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestTextIntegersWideAreStringsAndTokensAreIntegers holds every row of
// testdata/wire/json-integers/MANIFEST.tsv to the Go text engine
// (docs/SPEC-TABLES.md §16.2, docs/rulings.json
// `json-wide-integers-are-strings` and `json-integer-token-only`): a kind whose
// storage is wider than 32 bits is written as a string of decimal digits and
// read from either the string or a number, and an integer field takes an
// integer token only — digits with an optional minus, no fraction, no exponent,
// not `-0`.
func TestTextIntegersWideAreStringsAndTokensAreIntegers(t *testing.T) {
	t.Parallel()

	dir := filepath.Join("..", "..", "testdata", "wire", "json-integers")
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	st := m.Lookup("Counts")
	if st == nil {
		t.Fatal("no Counts table")
	}

	rows := integerManifestRows(t, filepath.Join(dir, "MANIFEST.tsv"))
	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			text, err := os.ReadFile(filepath.Join(dir, row.json))
			if err != nil {
				t.Fatal(err)
			}
			inst := m.New(st)
			var report tabletext.Report
			read := m.Read(inst, text, &report)
			got := "read"
			if !read {
				got = "refused"
			}
			if got != row.verdict {
				t.Fatalf("verdict %s, want %s (%+v)", got, row.verdict, report)
			}
			if row.verdict == "refused" {
				if !report.Refused {
					t.Fatalf("a refused read left report.Refused false: %+v", report)
				}
				if row.reason != "-" && !strings.HasPrefix(report.Reason, row.reason) {
					t.Fatalf("reason %q, want prefix %q", report.Reason, row.reason)
				}
				return
			}
			if !report.Silent() {
				t.Fatalf("an accepted read counted or refused: %+v", report)
			}
			out, err := m.Write(inst)
			if err != nil {
				t.Fatalf("the accepted value does not write: %v", err)
			}
			// the value an accepted read lands writes back and reads again, and
			// the wide kinds come out as strings of digits
			back := m.New(st)
			var rr tabletext.Report
			if !m.Read(back, append(append([]byte{}, out...), '\n'), &rr) || !rr.Silent() {
				t.Fatalf("the written value does not read back: %+v\n%s", rr, out)
			}
			if row.name == "canonical" && string(out)+"\n" != string(text) {
				t.Fatalf("the canonical text is not the writer's bytes:\nwrote %q\nwant  %q", out, text)
			}
		})
	}

	// The writer's own text of the default instance is the canonical vector,
	// with every wide field a string of digits and the int32 a number.
	canonical, err := os.ReadFile(filepath.Join(dir, "canonical.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := string(canonical)
	for _, wantBit := range []string{
		`"small": 7`,
		`"wide_signed": "-9223372036854775808"`,
		`"wide_unsigned": "18446744073709551615"`,
		`"huge": "0"`,
		`"ranged": "-5"`,
		`"ratio": "1.5"`,
	} {
		if !strings.Contains(want, wantBit) {
			t.Errorf("the canonical vector lacks %s:\n%s", wantBit, want)
		}
	}
}

type integerManifestRow struct {
	name    string
	json    string
	verdict string
	reason  string
}

func integerManifestRows(t *testing.T, path string) []integerManifestRow {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []integerManifestRow
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want name, schema, root, json, verdict, reason", i+1, len(f))
		}
		row := integerManifestRow{name: f[0], json: f[3], verdict: f[4], reason: f[5]}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, row.verdict)
		}
		rows = append(rows, row)
	}
	return rows
}
