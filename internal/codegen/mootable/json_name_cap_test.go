package mootable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// jsonNameCapSchema is the unit testdata/wire/json-read-closed reads: a flags
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

// TestMooJsonNameCap holds the JSON read to §16.2's schema-name cap: every
// name string is at most 255 bytes after decoding, at every name site,
// including inside a skipped value, and a longer one refuses the whole read.
// A map key and a string value are data and are never refused for their
// length. The verdict is read or refused; the reason is not compared.
func TestMooJsonNameCap(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the emitted text form)")

	dir := filepath.Join(root, "testdata", "wire", "json-read-closed")
	data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	type vector struct {
		root, file, verdict string
	}
	named := map[string]vector{}
	for i, line := range strings.Split(string(data), "\n") {
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

	u := unitFromSource(t, jsonNameCapSchema)
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	for _, name := range jsonNameCapVectors {
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
	}

	g := writeUnit(t, u, files)
	mods := map[string]string{}
	for _, name := range jsonNameCapVectors {
		mod := u.DeclFile[named[name].root] + "Table"
		mods[mod] = named[name].root
	}
	var modNames []string
	for mod := range mods {
		modNames = append(modNames, mod)
	}
	sort.Strings(modNames)

	var b strings.Builder
	b.WriteString("--!strict\nlocal inputs = require(\"./inputs\")\n")
	for _, mod := range modNames {
		fmt.Fprintf(&b, "local %s: any = require(\"./%s\")\n", mod, mod)
	}
	b.WriteString("local function run(mod: any, root: string, name: string)\n")
	b.WriteString("\tlocal v = mod[\"New\" .. root]()\n")
	b.WriteString("\tlocal r: any = {}\n")
	b.WriteString("\tlocal ok = mod[root .. \"FromJson\"](v, inputs[name], r)\n")
	b.WriteString("\tprint(name .. \" \" .. (if ok then \"read\" else \"refused\"))\nend\n")
	for _, name := range jsonNameCapVectors {
		v := named[name]
		fmt.Fprintf(&b, "run(%s, %q, %q)\n", u.DeclFile[v.root]+"Table", v.root, name)
	}

	out := strings.Split(g.run(t, b.String(), inputs), "\n")
	if len(out) != len(jsonNameCapVectors) {
		t.Fatalf("the probe printed %d lines for %d rows:\n%s", len(out), len(jsonNameCapVectors), strings.Join(out, "\n"))
	}
	for i, name := range jsonNameCapVectors {
		want := name + " " + named[name].verdict
		if out[i] != want {
			t.Errorf("%s: got %q, want %q", name, out[i], want)
		}
	}
}
