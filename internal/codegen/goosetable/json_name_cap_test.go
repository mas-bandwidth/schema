package goosetable_test

// THE SCHEMA-NAME CAP IN THE JSON READ (docs/SPEC-TABLES.md §16.2): every
// schema-name string — a field key, an enum variant name, a flags element, a
// union arm key and an `&node` name, in every object and inside a skipped
// value — is at most 255 bytes after decoding, and a longer one refuses the
// read. A map key and a string value are data and are never refused for
// their length. Each row's verdict is read from the shared vectors under
// testdata/wire/json-read-closed/ (their MANIFEST.tsv), never from a reason.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// jsonNameCapSchema is the small unit the read-closed vectors read: a flags
// field, an enum, a union, a map keyed by string and a string field, plus a
// pointered root for the `&node` name's site.
const jsonNameCapSchema = `package closed

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

table Carried
{
    kind   Kind
    perms  Perms
    effect Effect
    note   string(300)
}
`

// jsonNameCapVectors are the manifest's name-cap rows: the 255-byte reads,
// the 256-byte refusals at each name site, and the two data rows that stay
// reads. The depth and UTF-8 rows belong to other rules of the corpus.
var jsonNameCapVectors = []string{
	"field-key-255", "field-key-256",
	"skipped-key-255", "skipped-key-256",
	"enum-name-255", "enum-name-256",
	"flags-element-255", "flags-element-256",
	"union-arm-255", "union-arm-256",
	"ampersand-255", "ampersand-256",
	"map-key-long", "string-value-long",
}

// jsonNameCapTyped are the rows a carried root reads too: the field, skipped
// object, enum, flags, union and string-value sites. The `&` name and the
// map key need the variable class's storage, so those rows stay there.
var jsonNameCapTyped = map[string]bool{
	"field-key-255": true, "field-key-256": true,
	"skipped-key-255": true, "skipped-key-256": true,
	"enum-name-255": true, "enum-name-256": true,
	"flags-element-255": true, "flags-element-256": true,
	"union-arm-255": true, "union-arm-256": true,
	"string-value-long": true,
}

// jsonNameCapRow is one wanted manifest line: the root and JSON file it
// names, and its verdict.
type jsonNameCapRow struct {
	name, root, path, want string
}

// jsonNameCapRows reads the wanted manifest rows, in name order.
func jsonNameCapRows(t *testing.T) []jsonNameCapRow {
	t.Helper()
	dir := filepath.Join(root, "testdata", "wire", "json-read-closed")
	data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]jsonNameCapRow{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s/MANIFEST.tsv:%d: want name, root, json, verdict, description, got %d columns", dir, i+1, len(fields))
		}
		byName[fields[0]] = jsonNameCapRow{
			name: fields[0],
			root: fields[1],
			path: filepath.Join("testdata", "wire", "json-read-closed", fields[2]),
			want: fields[3],
		}
	}
	rows := make([]jsonNameCapRow, 0, len(jsonNameCapVectors))
	for _, name := range jsonNameCapVectors {
		r, ok := byName[name]
		if !ok {
			t.Fatalf("%s/MANIFEST.tsv names no vector %q", dir, name)
		}
		rows = append(rows, r)
	}
	return rows
}

// TestGooseJsonNameCap reads every name-cap row through the strict variable
// text read (json_load_table) and holds its verdict: the 255-byte names and
// the long data read, the 256-byte names refuse.
func TestGooseJsonNameCap(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Goose toolchain (it compiles and runs the text read)")
	rows := jsonNameCapRows(t)

	dir := t.TempDir()
	schema := filepath.Join(dir, "Closed.schema")
	if err := os.WriteFile(schema, []byte(jsonNameCapSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, files := cookUnitFiles(t, []string{schema}, nil)
	ns := u.Package

	var b strings.Builder
	b.WriteString("import os;\nimport .all;\n\n")
	b.WriteString(`fn probe(name: const u8[:], root: const u8[:], file: const u8[:], typed: bool) {
    var text: u8[>..] = [];
    if !read_file(file, text) { print(name, " unreadable"); return; }
    var r = schema_rt::json_report();
    var ok = false;
    if typed {
        var v = ` + ns + `::NewCarried();
        ok = ` + ns + `::CarriedFromJson(v, text, r);
    } else {
        var data: u8[>..] = [];
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        let at = schema_rt::json_load_table(` + ns + `::TableUnit, ` + ns + `::TextUnit, root, text, data, vals, nodes, r);
        ok = at >= 0;
    }
    if ok && !r.damaged { print(name, " read"); } else { print(name, " refused"); }
}

fn main() {
`)
	verdicts := make([]jsonNameCapRow, 0, len(rows)*2)
	for _, r := range rows {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(r.path)))
		if err != nil {
			t.Fatal(err)
		}
		file := r.name + ".json"
		files[file] = data
		b.WriteString("    probe(\"" + r.name + "\", \"" + r.root + "\", \"" + file + "\", false);\n")
		verdicts = append(verdicts, r)
		if jsonNameCapTyped[r.name] {
			b.WriteString("    probe(\"typed " + r.name + "\", \"Carried\", \"" + file + "\", true);\n")
			verdicts = append(verdicts, jsonNameCapRow{name: "typed " + r.name, want: r.want})
		}
	}
	b.WriteString("}\n")
	files["main.goose"] = []byte(b.String())

	out := strings.Split(strings.TrimSuffix(string(gooserun.Run(t, dir, "main.goose", files)), "\n"), "\n")
	if len(out) != len(verdicts) {
		t.Fatalf("%d answers for %d rows:\n%s", len(out), len(verdicts), strings.Join(out, "\n"))
	}
	for i, r := range verdicts {
		if out[i] != r.name+" "+r.want {
			t.Errorf("%s: got %q, want %q", r.name, out[i], r.name+" "+r.want)
		}
	}
}
