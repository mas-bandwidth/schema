package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// jsonIntegersSchema is the unit the json-integers vectors read: an int32, an
// int64, a uint64, a ranged int128, a ranged int64, a wide fixed-point field
// and a map keyed by uint64 — every integer storage width the two JSON integer
// rules name (docs/SPEC-TABLES.md §16.2, §16.14 W10 and W11).
const jsonIntegersSchema = `package jsonintegers

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

// jsonIntegersProbe reads one vector through the strict FromJson and, for an
// accept, writes it back through ToJson and ToJsonMeasure.
const jsonIntegersProbe = `
local jwide = function(unit, mod, root, key) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local J = schema.require("schema_table_json");
  local v = J.FromJson(T, root, inputs[key + ".json"], {});
  if (v == null) { print("jwide " + key + " refused\n"); return; }
  local text = J.ToJson(T, root, v);
  local measure = J.ToJsonMeasure(T, root, v);
  print("jwide " + key + " " + (text == null ? "null" : hexOf(text)) + " " + measure + "\n");
};
`

// TestSquirrelJsonWideIntegersAreStrings holds the Squirrel leg to both JSON
// integer rules of docs/SPEC-TABLES.md §16.2: an integer kind wider than 32
// bits is WRITTEN as a string of decimal digits and reads as a string or a
// number; every integer field takes an integer token only — no fraction, no
// exponent, no `-0` — and the range is tested on the exact value. Every accept
// row writes the committed canonical text byte for byte, and every refuse row
// is refused.
func TestSquirrelJsonWideIntegersAreStrings(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")

	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "testdata", "wire", "json-integers")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := os.ReadFile(filepath.Join(dir, "canonical.read.json"))
	if err != nil {
		t.Fatal(err)
	}

	type vector struct {
		name, json, verdict string
	}
	var vectors []vector
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s/MANIFEST.tsv:%d: want name, root, json, verdict, description, got %d columns", dir, i+1, len(fields))
		}
		if fields[1] != "Integers" {
			t.Fatalf("%s/MANIFEST.tsv:%d: root %q is not the unit's Integers", dir, i+1, fields[1])
		}
		if fields[3] != "read" && fields[3] != "refused" {
			t.Fatalf("%s/MANIFEST.tsv:%d: verdict %q, want read or refused", dir, i+1, fields[3])
		}
		vectors = append(vectors, vector{name: fields[0], json: fields[2], verdict: fields[3]})
	}
	if len(vectors) == 0 {
		t.Fatal("no vector rows read; the gate would pass over an empty set")
	}

	unit := loadSource(t, jsonIntegersSchema)
	out := t.TempDir()
	mod := emitUnit(t, out, "jsonintegers", unit)

	var probe strings.Builder
	probe.WriteString(jsonPrelude)
	probe.WriteString(jsonIntegersProbe)
	inputs := map[string][]byte{}
	for _, v := range vectors {
		body, err := os.ReadFile(filepath.Join(dir, v.json))
		if err != nil {
			t.Fatalf("%s: %v", v.name, err)
		}
		inputs[v.name+".json"] = body
	}
	for _, v := range vectors {
		fmt.Fprintf(&probe, "jwide(%q, %q, %q, %q);\n", "jsonintegers", mod, "Integers", v.name)
	}
	writeFile(t, filepath.Join(out, "main.nut"), []byte(probe.String()))

	got := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, out, "main.nut", inputs)), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[0] != "jwide" {
			t.Fatalf("probe line %q is not a jwide row", line)
		}
		got[f[1]] = f[2:]
	}

	compared := 0
	for _, v := range vectors {
		g, ok := got[v.name]
		if !ok {
			t.Errorf("%s: the probe printed no row", v.name)
			continue
		}
		if v.verdict == "refused" {
			if g[0] != "refused" {
				t.Errorf("%s: FromJson answered %v, want refused", v.name, g)
				continue
			}
			compared++
			continue
		}
		if g[0] == "refused" || g[0] == "null" {
			t.Errorf("%s: FromJson or ToJson refused an accept row: %v", v.name, g)
			continue
		}
		text, ok := unhex(g[0])
		if !ok {
			t.Errorf("%s: ToJson's text is not hex: %q", v.name, g[0])
			continue
		}
		// The json-write surface's canonical text ends with exactly one
		// newline (§16.1); the committed vector is the body.
		if string(text) != string(canonical)+"\n" {
			t.Errorf("%s: ToJson differs from the committed canonical text\n got %q\nwant %q", v.name, text, string(canonical)+"\n")
			continue
		}
		if g[1] != strconv.Itoa(len(text)) {
			t.Errorf("%s: ToJsonMeasure is %s, want %d", v.name, g[1], len(text))
			continue
		}
		compared++
	}
	if compared != len(vectors) {
		t.Errorf("compared %d of %d integer vectors", compared, len(vectors))
	}
}
