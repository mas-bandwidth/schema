package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// lobsterNameCapSchema is the unit the read-closed vectors read
// (testdata/wire/json-read-closed/MANIFEST.tsv): a flags field, an enum, a
// union, a map keyed by string and a string field, plus a pointered root for
// the `&node` name's site.
const lobsterNameCapSchema = `package closed

enum Kind { Alpha, Beta }

flags Perms { Read, Write }

type Buff
{
    mult float32 = 1.0
}

union Effect
{
    buff Buff
    ping
}

table Node
{
    value int32 = 0
}

table Scene
{
    head *Node
}

table Closed
{
    kind   Kind
    perms  Perms
    effect Effect
    tags   map[string]int32
    note   string(300)
}
`

// lobsterNameCapRows are the manifest rows of the 255-byte schema-name cap
// (docs/SPEC-TABLES.md §16.2): each name site at 255 and 256 bytes, a field
// key inside a skipped value, and the map key and string value the cap never
// reaches.
var lobsterNameCapRows = map[string]bool{
	"field-key-255": true, "field-key-256": true,
	"skipped-key-255": true, "skipped-key-256": true,
	"enum-name-255": true, "enum-name-256": true,
	"flags-element-255": true, "flags-element-256": true,
	"union-arm-255": true, "union-arm-256": true,
	"ampersand-255": true, "ampersand-256": true,
	"map-key-long": true, "string-value-long": true,
}

// TestLobsterJsonNameCap holds the verdict of every name-cap row of the
// read-closed vectors through the Lobster JSON read: a schema-name string of
// 255 bytes reads and one of 256 refuses the whole load, at every name site
// and inside a skipped value; a long map key and a long string value read.
func TestLobsterJsonNameCap(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("..", "..", "..", "testdata", "wire", "json-read-closed")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		name, root, file, verdict string
	}
	var rows []row
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s/MANIFEST.tsv:%d: want name, root, json, verdict, description, got %d columns", dir, i+1, len(fields))
		}
		if !lobsterNameCapRows[fields[0]] {
			continue
		}
		if fields[3] != "read" && fields[3] != "refused" {
			t.Fatalf("%s: verdict %q, want read or refused", fields[0], fields[3])
		}
		rows = append(rows, row{fields[0], fields[1], fields[2], fields[3]})
	}
	if len(rows) != len(lobsterNameCapRows) {
		t.Fatalf("the manifest holds %d of the %d name-cap rows", len(rows), len(lobsterNameCapRows))
	}

	path := filepath.Join(t.TempDir(), "Closed.schema")
	if err := os.WriteFile(path, []byte(lobsterNameCapSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	inputs := make(map[string][]byte, len(rows))
	var program strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&program, "import .%s\n", mod)
	}
	program.WriteString("import .json\n")
	for _, r := range rows {
		body, err := os.ReadFile(filepath.Join(dir, r.file))
		if err != nil {
			t.Fatal(err)
		}
		inputs[r.file] = body
		fmt.Fprintf(&program, `if true:
    let input = read_file(%q)
    assert input
    let report = schema_rt.LoadReport {}
    let value = schema_rt.json_read(%q, input, report)
    print(%q + (if value: " read" else: " refused"))
`, r.file, r.root, r.name)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := lobsterrun.Run(t, g.dir, "main.lobster", inputs)
	got := make(map[string]string, len(rows))
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name, verdict, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok {
			got[name] = verdict
		}
	}
	for _, r := range rows {
		if got[r.name] != r.verdict {
			t.Errorf("%s (%s): the Lobster JSON read gave %q, want %q", r.name, r.file, got[r.name], r.verdict)
		}
	}
}
