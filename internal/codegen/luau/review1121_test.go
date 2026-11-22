package luau

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/luau/luaurun"
)

// review1121Dir is the committed corpus of the 2026-11-21 Luau review
// (LUAU-1 to LUAU-4): the inputs and verdicts the regressions below read.
var review1121Dir = filepath.Join("testdata", "review1121")

// review1121Rows reads a corpus index: one tab-separated row of fields
// columns per line, # lines and blank lines skipped.
func review1121Rows(t *testing.T, path string, fields int) [][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows [][]string
	for n, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != fields {
			t.Fatalf("%s line %d: %d fields, want %d", path, n+1, len(f), fields)
		}
		rows = append(rows, f)
	}
	if len(rows) == 0 {
		t.Fatalf("%s holds no rows", path)
	}
	return rows
}

// TestLuauPacketWriteRefusesRatherThanRaises pins LUAU-4 (review
// 2026-11-21): the packet writer does what its documented contract says. A
// value of the wrong Luau type, or a float that is not finite at its width,
// makes Write<Name> return false and never raise: the guard runs before the
// serialize runtime, which traps on such a value. The lawful value writes.
// The schema, the probe and every case are the committed corpus
// testdata/review1121/packet-write.
func TestLuauPacketWriteRefusesRatherThanRaises(t *testing.T) {
	t.Parallel()
	corpus := filepath.Join(review1121Dir, "packet-write")
	schema, err := os.ReadFile(filepath.Join(corpus, "Contract.schema"))
	if err != nil {
		t.Fatal(err)
	}
	probe, err := os.ReadFile(filepath.Join(corpus, "probe.luau"))
	if err != nil {
		t.Fatal(err)
	}
	rows := review1121Rows(t, filepath.Join(corpus, "cases.tsv"), 4)
	u := unitFromSchemas(t, map[string]string{"Contract.schema": string(schema)})
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeAll(t, dir, files)
	writeFile(t, dir, "main.luau", string(probe))
	var cases strings.Builder
	cases.WriteString("return {\n")
	written, refused := 0, 0
	for _, r := range rows {
		id, field, value, want := r[0], r[1], r[2], r[3]
		switch field {
		case "-":
			cases.WriteString("\t{ \"" + id + "\", function(v) return v end },\n")
		case "*":
			cases.WriteString("\t{ \"" + id + "\", function(v) return " + value + " end },\n")
		default:
			cases.WriteString("\t{ \"" + id + "\", function(v) v." + field + " = " + value + "; return v end },\n")
		}
		switch want {
		case "written":
			written++
		case "refused":
			refused++
		}
	}
	cases.WriteString("}\n")
	if written == 0 || refused == 0 {
		t.Fatalf("the corpus holds %d written and %d refused rows: a refusal is only proven beside a lawful write", written, refused)
	}
	writeFile(t, dir, "cases.luau", cases.String())
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", nil))), "\n") {
		k, v, _ := strings.Cut(line, " ")
		got[k] = v
	}
	for _, r := range rows {
		if got[r[0]] != r[3] {
			t.Errorf("%s (%s = %s): %q, want %q", r[0], r[1], r[2], got[r[0]], r[3])
		}
	}
}
