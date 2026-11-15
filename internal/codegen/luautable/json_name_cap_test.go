package luautable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// jsonNameCapSchema is the read-closed unit the vectors under
// testdata/wire/json-read-closed read (docs/SPEC-TABLES.md §16.2, §16.12): a
// flags field, an enum, a union, a map keyed by string and a string field,
// plus a pointered root for the `&node` name's site.
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
`

// jsonNameCapRows is the name-cap half of the vectors' manifest: the 255 and
// 256 spellings at every name site, and the two data strings a long length
// never refuses.
var jsonNameCapRows = []string{
	"field-key-255",
	"field-key-256",
	"skipped-key-255",
	"skipped-key-256",
	"enum-name-255",
	"enum-name-256",
	"flags-element-255",
	"flags-element-256",
	"union-arm-255",
	"union-arm-256",
	"ampersand-255",
	"ampersand-256",
	"map-key-long",
	"string-value-long",
}

// TestLuauJsonNameCap holds the name-cap rows of
// testdata/wire/json-read-closed/: the 255-byte spelling reads, the 256-byte
// spelling refuses, at every name site including a skipped object and an enum
// name, while the long map key and string value read. A schema-name string is
// at most 255 bytes after decoding (docs/SPEC-TABLES.md §16.2, §16.12); a map
// key and a string value are data and are never refused for their length.
func TestLuauJsonNameCap(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")

	const dir = "../../../testdata/wire/json-read-closed"
	data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("the vectors' manifest: %v", err)
	}
	type vector struct {
		root, json, verdict string
	}
	named := make(map[string]vector)
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s/MANIFEST.tsv:%d: want name, root, json, verdict, description, got %d columns", dir, i+1, len(fields))
		}
		named[fields[0]] = vector{root: fields[1], json: fields[2], verdict: fields[3]}
	}
	want := make(map[string]vector, len(jsonNameCapRows))
	for _, name := range jsonNameCapRows {
		row, ok := named[name]
		if !ok {
			t.Fatalf("%s/MANIFEST.tsv names no row %q", dir, name)
		}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("%s: verdict %q, want read or refused", name, row.verdict)
		}
		want[name] = row
	}

	g := generateFiles(t, map[string]string{"Closed.schema": jsonNameCapSchema})
	inputs := make(map[string][]byte, len(jsonNameCapRows))
	var b strings.Builder
	b.WriteString(jsonModules(g))
	b.WriteString(`local json = require("./schema_json")
local inputs = require("./inputs")
local cases = {
`)
	for _, name := range jsonNameCapRows {
		body, err := os.ReadFile(filepath.Join(dir, want[name].json))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		inputs[name] = body
		fmt.Fprintf(&b, "\t{ %q, %q },\n", name, want[name].root)
	}
	b.WriteString(`}
for _, c in cases do
	local name, root = c[1], c[2]
	local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
	local v = json.FromJson(root, inputs[name], report)
	print(name .. "|" .. (if v ~= nil then "read" else "refused"))
end
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, g.dir, "main.luau", inputs))
	got := make(map[string]string, len(jsonNameCapRows))
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		name, verdict, ok := strings.Cut(strings.TrimSpace(line), "|")
		if !ok {
			t.Fatalf("probe line %q: want name|verdict", line)
		}
		got[name] = verdict
	}
	if len(got) != len(jsonNameCapRows) {
		t.Fatalf("probe answered %d of %d rows:\n%s", len(got), len(jsonNameCapRows), out)
	}
	for _, name := range jsonNameCapRows {
		if answer := got[name]; answer != want[name].verdict {
			t.Errorf("%s: Luau verdict = %s, vector says %s", name, answer, want[name].verdict)
		}
	}
}
