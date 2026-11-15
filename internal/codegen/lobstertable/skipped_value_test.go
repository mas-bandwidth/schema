package lobstertable_test

// TestLobsterSkippedValueIsValidated holds the Lobster leg to
// docs/rulings.json's skipped-value-is-validated rule (docs/SPEC-TABLES.md §4,
// §3.3, §16.3): a value the reader SKIPS is held to what a read of it would
// hold, and damage inside it refuses the whole read. The shared vectors under
// testdata/wire/skipped-value/ are the cases, each with its verdict in the
// manifest; this test runs every JSON row through the leg's JSON read and
// every message row through the leg's message load under the writer's
// announcement.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

type skippedValueRow struct {
	file, form, verdict string
}

// readSkippedValueManifest reads testdata/wire/skipped-value/MANIFEST.tsv:
// <file> TAB <form> TAB <verdict>, the file named relative to the manifest.
func readSkippedValueManifest(t *testing.T) []skippedValueRow {
	t.Helper()
	manifestPath := filepath.Join(root, "testdata", "wire", "skipped-value", "MANIFEST.tsv")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var rows []skippedValueRow
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			t.Fatalf("%s:%d: a vector line is <file> TAB <form> TAB <verdict>, got %q", manifestPath, i+1, line)
		}
		if f[1] != "json" && f[1] != "message" {
			t.Fatalf("%s:%d: form %q, want json or message", manifestPath, i+1, f[1])
		}
		if f[2] != "read" && f[2] != "refused" {
			t.Fatalf("%s:%d: verdict %q, want read or refused", manifestPath, i+1, f[2])
		}
		rows = append(rows, skippedValueRow{file: f[0], form: f[1], verdict: f[2]})
	}
	if len(rows) == 0 {
		t.Fatal("the skipped-value manifest names no rows")
	}
	return rows
}

// skippedValueProgram reads every row as the reader unit's Root: a JSON row
// through json_read, a message row as a batch of one under the writer's
// announcement (input "ann"), and the announcement row by announce_read
// alone. It prints one `<file> <verdict>` line per row. The vocabulary is
// declared at the default 4096 entries: the writer announces more entries
// than the reader unit declares.
func skippedValueProgram(g *loadUnit, rows []skippedValueRow) string {
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\nimport .schema_save\nimport .strict_and_refusals\nimport .json\nimport .message\n")
	b.WriteString(`def jrow(file:string):
    let text = read_file(file)
    assert text
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.json_read("Root", text, rep)
    print(file + " " + (if rec: "read" else: "refused"))
def mrow(file:string):
    assert schema_rt.is_message_root("Root")
    let v = schema_rt.new_vocabulary(4096)
    let announcement = read_file("ann")
    assert announcement
    let aok = schema_rt.announce_read(v, announcement, schema_rt.LoadReport {})
    if file == "announcement.bin" or not aok:
        print(file + " " + (if aok: "read" else: "refused"))
        return
    let data = read_file(file)
    assert data
    let rep = schema_rt.LoadReport {}
    let recs, count, ok = schema_rt.load_messages("Root", v, data, rep, schema_rt.message_batch_max)
    print(file + " " + (if ok and count == 1 and length(recs) == 1: "read" else: "refused"))
`)
	for _, r := range rows {
		call := "mrow"
		if r.form == "json" {
			call = "jrow"
		}
		fmt.Fprintf(&b, "%s(%s)\n", call, lobster.StringLit([]byte(r.file)))
	}
	return b.String()
}

func TestLobsterSkippedValueIsValidated(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lobster toolchain (it runs the generated modules)")

	rows := readSkippedValueManifest(t)
	dir := filepath.Join(root, "testdata", "wire", "skipped-value")
	inputs := map[string][]byte{}
	for _, r := range rows {
		data, err := os.ReadFile(filepath.Join(dir, r.file))
		if err != nil {
			t.Fatalf("%s: %v", r.file, err)
		}
		inputs[r.file] = data
	}
	inputs["ann"] = inputs["announcement.bin"]
	if inputs["ann"] == nil {
		t.Fatal("the skipped-value manifest names no announcement.bin")
	}

	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "test", "skipped-value", "reader")})
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("the reader unit is refused: %v", err)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(skippedValueProgram(g, rows)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)), "\n"), "\n")
	if len(out) != len(rows) {
		t.Fatalf("the program printed %d lines for %d rows:\n%s", len(out), len(rows), strings.Join(out, "\n"))
	}
	for i, r := range rows {
		want := r.file + " " + r.verdict
		if out[i] != want {
			t.Errorf("%s (%s): got %q, want %q", r.file, r.form, out[i], want)
		}
	}
}
