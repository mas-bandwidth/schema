package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// lobsterJsonIntegersSchema is the unit testdata/wire/json-integers/ reads:
// an int32, an int64, a uint64, a ranged int128, a ranged int64, a wide
// fixed-point field and a map keyed by uint64 — every integer storage width
// the two JSON integer rules name (docs/SPEC-TABLES.md §16.2).
const lobsterJsonIntegersSchema = `package jsonintegers

table Integers
{
    small   int32
    wide    int64
    uwide   uint64
    wide128 int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    ranged  int64 | min = 0, max = 9007199254740993
    frac    fixed(48, 16) | min = -10, max = 10
    counts  map[uint64]int32
}
`

// TestLobsterJsonWideIntegersAreStrings holds every row of
// testdata/wire/json-integers/ through the generated Lobster JSON reader and
// writer: a wide integer is written as a STRING of decimal digits and reads
// as a string or a number; every integer field takes an integer token only —
// no fraction, no exponent, no negative zero — the value never passes through
// a double, and its range is tested on the exact value (docs/SPEC-TABLES.md
// §16.2, §16.14; docs/rulings.json json-wide-integers-are-strings and
// json-integer-token-only).
func TestLobsterJsonWideIntegersAreStrings(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lobster toolchain (it runs the generated JSON reader and writer)")

	scratch := t.TempDir()
	schemaPath := filepath.Join(scratch, "Integers.schema")
	if err := os.WriteFile(schemaPath, []byte(lobsterJsonIntegersSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "testdata", "wire", "json-integers")
	data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := os.ReadFile(filepath.Join(dir, "canonical.read.json"))
	if err != nil {
		t.Fatal(err)
	}

	type row struct {
		name, root, file, verdict string
	}
	var rows []row
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s/MANIFEST.tsv:%d: want name, root, json, verdict, description, got %d columns", dir, i+1, len(fields))
		}
		if fields[3] != "read" && fields[3] != "refused" {
			t.Fatalf("%s: verdict %q, want read or refused", fields[0], fields[3])
		}
		rows = append(rows, row{name: fields[0], root: fields[1], file: fields[2], verdict: fields[3]})
	}
	if len(rows) == 0 {
		t.Fatal("no manifest rows read; the gate would pass over an empty set")
	}

	var program strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&program, "import .%s\n", mod)
	}
	program.WriteString(`import .json
def check(name:string, verdict:string, path:string):
    let text = read_file(path)
    assert text
    let report = schema_rt.LoadReport {}
    let value = schema_rt.json_read("Integers", text, report)
    if verdict == "read":
        if not value:
            print("BAD " + name + " refused " + report.reason)
            return
        let out, why = schema_rt.json_write(value)
        if why != "":
            print("BAD " + name + " write " + why)
            return
        assert write_file("out-" + name, out)
    elif value:
        print("BAD " + name + " accepted")
`)
	inputs := map[string][]byte{}
	for _, r := range rows {
		body, err := os.ReadFile(filepath.Join(dir, r.file))
		if err != nil {
			t.Fatal(err)
		}
		name := "in-" + r.name
		inputs[name] = body
		fmt.Fprintf(&program, "check(%q, %q, %q)\n", r.name, r.verdict, name)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(lobsterrun.Run(t, g.dir, "main.lobster", inputs))
	if strings.Contains(out, "BAD ") {
		t.Fatalf("the generated Lobster JSON surface failed its vectors:\n%s", out)
	}
	for _, r := range rows {
		if r.verdict != "read" {
			continue
		}
		got, err := os.ReadFile(filepath.Join(g.dir, "out-"+r.name))
		if err != nil {
			t.Fatalf("%s: the writer produced no text: %v", r.name, err)
		}
		// the vectors hold the reference engine's Write, which carries no
		// closing newline; the JSON surface's ToJson closes the text with one
		// exactly (§16.1), and the bytes before it are the canonical text
		if !strings.HasSuffix(string(got), "\n") {
			t.Errorf("%s: the writer did not close the text with one newline:\n%s", r.name, got)
			continue
		}
		if trimmed := strings.TrimSuffix(string(got), "\n"); trimmed != string(canonical) {
			t.Errorf("%s: read then write did not produce the canonical text\nwrote:\n%s\nwant:\n%s", r.name, got, canonical)
		}
	}
}
