package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// sqNameCapSchema is the unit testdata/wire/json-read-closed reads: a flags
// field, an enum, a union, a string-keyed map and a string field, plus a
// pointered root for the &node name's site.
const sqNameCapSchema = `package closed

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

// TestSquirrelJsonNameCap pins docs/SPEC-TABLES.md §16.2 and §16.12 for the
// Squirrel leg: a schema-name string is at most 255 bytes after decoding at
// every name site, a skipped object's keys included, and a longer one refuses
// the whole read. A map key and a string value are data, never refused for
// their length. The verdict is read or refused; the reason is the leg's own
// (docs/rulings.json refusal-verdict-only) and is not compared.
func TestSquirrelJsonNameCap(t *testing.T) {
	t.Parallel()
	const dir = "../../../testdata/wire/json-read-closed"
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	type vector struct {
		root, file, verdict string
	}
	named := map[string]vector{}
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("MANIFEST.tsv:%d: want 5 columns, got %d", i+1, len(fields))
		}
		named[fields[0]] = vector{root: fields[1], file: fields[2], verdict: fields[3]}
	}

	// The name-cap rows only. Depth and UTF-8 rows belong to other cards.
	want := []string{
		"field-key-255", "field-key-256",
		"skipped-key-255", "skipped-key-256",
		"enum-name-255", "enum-name-256",
		"flags-element-255", "flags-element-256",
		"union-arm-255", "union-arm-256",
		"ampersand-255", "ampersand-256",
		"map-key-long", "string-value-long",
	}

	schemaPath := filepath.Join(t.TempDir(), "closed.schema")
	writeFile(t, schemaPath, []byte(sqNameCapSchema))
	paths, err := compiler.GatherPaths([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	unit, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}

	files, err := squirrel.Generate(unit)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(unit)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	dir2 := t.TempDir()
	out := filepath.Join(dir2, "closed")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}

	inputs := map[string][]byte{}
	cases := make([]string, len(want))
	expectRead := make([]bool, len(want))
	for i, name := range want {
		v, ok := named[name]
		if !ok {
			t.Fatalf("MANIFEST.tsv names no name-cap vector %q", name)
		}
		if v.verdict != "read" && v.verdict != "refused" {
			t.Fatalf("%s: verdict %q, want read or refused", name, v.verdict)
		}
		body, err := os.ReadFile(filepath.Join(dir, v.file))
		if err != nil {
			t.Fatal(err)
		}
		inputs[name] = body
		cases[i] = fmt.Sprintf("[%q, %q]", name, v.root)
		expectRead[i] = v.verdict == "read"
	}

	probe := fmt.Sprintf(`local inputs = dofile("inputs.nut", true);
local schema = dofile("closed/schema_module.nut", true);
local T = schema.require(%q);
local J = schema.require("schema_table_json");
local cases = [%s];
foreach (c in cases) {
  local r = { unknown = 0, mismatch = 0, widened = 0, oor = 0, repeated = 0,
    bodyDamaged = false, refused = false };
  local v = J.FromJson(T, c[1], inputs[c[0]], r);
  print(c[0] + " " + (v == null ? "refused" : "read") + "\n");
}
`, squirreltable.ModuleName(unit), strings.Join(cases, ", "))
	writeFile(t, filepath.Join(dir2, "main.nut"), []byte(probe))

	out2 := string(sqrun.Run(t, dir2, "main.nut", inputs))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out2, "\n"), "\n") {
		name, verdict, ok := strings.Cut(line, " ")
		if !ok {
			t.Fatalf("probe line %q is not a row", line)
		}
		got[name] = verdict
	}
	for i, name := range want {
		verdict, ok := got[name]
		if !ok {
			t.Errorf("%s: the probe printed no row", name)
			continue
		}
		wantWord := "refused"
		if expectRead[i] {
			wantWord = "read"
		}
		if verdict != wantWord {
			t.Errorf("%s: Squirrel says %s, want %s", name, verdict, wantWord)
		}
	}
}
