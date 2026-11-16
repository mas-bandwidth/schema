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
  local report = {};
  local v = J.FromJson(T, root, inputs[key + ".json"], report);
  if (v == null) { print("jwide " + key + " refused " + (("reason" in report) ? report.reason : "unknown") + "\n"); return; }
  local text = J.ToJson(T, root, v);
  local measure = J.ToJsonMeasure(T, root, v);
  print("jwide " + key + " " + (text == null ? "null" : hexOf(text)) + " " + measure + "\n");
};
`

// TestSquirrelJsonWideIntegersAreStrings holds the Squirrel leg to both JSON
// integer rules of docs/SPEC-TABLES.md §16.2: an integer kind wider than 32
// bits is WRITTEN as a string of decimal digits and reads as a string only; every integer field takes an integer token only — no fraction, no
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
		numberToken         bool
		body                []byte
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
	numberManifest, err := os.ReadFile(filepath.Join(dir, "NUMBER-TOKEN.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	numberCount := 0
	for line := range strings.SplitSeq(string(numberManifest), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 || f[1] != "Integers" || f[3] != "refused" {
			t.Fatalf("invalid NUMBER-TOKEN row: %q", line)
		}
		vectors = append(vectors, vector{name: f[0], json: f[2], verdict: f[3], numberToken: true})
		numberCount++
	}
	if numberCount != 13 {
		t.Fatalf("NUMBER-TOKEN.tsv: got %d rows, want 13", numberCount)
	}

	// A NUMBER TOKEN IS READ AT ANY LENGTH (docs/SPEC-TABLES.md §16.2
	// Numbers): a wide fixed-point string's long in-range spelling is the same
	// value as its canonical "0.5", and the writer spells the canonical text.
	longFrac := "0.5" + strings.Repeat("0", 600)
	vectors = append(vectors, vector{
		name:    "long-number",
		verdict: "read",
		body: []byte(`{
  "small": 7,
  "wide": "-42",
  "uwide": "18446744073709551615",
  "wide128": "170141183460469231731687303715884105727",
  "ranged": "9007199254740993",
  "frac": "` + longFrac + `",
  "counts": {
    "0": 1,
    "9007199254740993": 2
  }
}`),
	})

	unit := loadSource(t, jsonIntegersSchema)
	out := t.TempDir()
	mod := emitUnit(t, out, "jsonintegers", unit)

	var probe strings.Builder
	probe.WriteString(jsonPrelude)
	probe.WriteString(jsonIntegersProbe)
	inputs := map[string][]byte{}
	for _, v := range vectors {
		body := v.body
		if body == nil {
			body, err = os.ReadFile(filepath.Join(dir, v.json))
			if err != nil {
				t.Fatalf("%s: %v", v.name, err)
			}
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
			if v.numberToken && (len(g) < 2 || g[1] != "kind_mismatch") {
				t.Errorf("%s: got %v, want kind_mismatch", v.name, g)
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
	t.Run("integer sites", func(t *testing.T) { t.Parallel(); squirrelWideIntegerSites(t) })
}

const squirrelWideSitesSchema = `package jsonsites

enum Slot { A, B }
union Pick
{
    wide int64
    narrow int32
}
table Sites
{
    wide int64
    bits33 bits(33)
    bits64 bits(64)
    u128 uint128
    fixed64 fixed(48, 16) | min = -10, max = 10
    fixed128 fixed(64, 64) | min = -10, max = 10
    counts map[uint64]int64
    list [2]int64
    keyed [Slot]int64
    opt ?int64
    pick Pick
    narrow int32
}
`

func squirrelWideIntegerSites(t *testing.T) {
	t.Helper()
	cases := []struct{ name, number, quoted, value string }{
		{"plain", `{"wide":42}`, `{"wide":"42"}`, `"42"`},
		{"array", `{"list":[42,"0"]}`, `{"list":["42","0"]}`, `"42"`},
		{"optional", `{"opt":42}`, `{"opt":"42"}`, `"42"`},
		{"union", `{"pick":{"wide":42}}`, `{"pick":{"wide":"42"}}`, `"42"`},
		{"map", `{"counts":{"1":42}}`, `{"counts":{"1":"42"}}`, `"42"`},
		{"keyed", `{"keyed":{"A":42}}`, `{"keyed":{"A":"42"}}`, `"42"`},
		{"bits33", `{"bits33":42}`, `{"bits33":"42"}`, `"42"`},
		{"bits64", `{"bits64":42}`, `{"bits64":"42"}`, `"42"`},
		{"uint128", `{"u128":42}`, `{"u128":"42"}`, `"42"`},
		{"fixed64", `{"fixed64":0.5}`, `{"fixed64":"0.5"}`, `"0.5"`},
		{"fixed128", `{"fixed128":0.5}`, `{"fixed128":"0.5"}`, `"0.5"`},
	}
	u := loadSource(t, squirrelWideSitesSchema)
	out := t.TempDir()
	mod := emitUnit(t, out, "jsonsites", u)
	var probe strings.Builder
	probe.WriteString(jsonPrelude)
	probe.WriteString(jsonIntegersProbe)
	inputs := map[string][]byte{"narrow.json": []byte(`{"narrow":42}`)}
	fmt.Fprintf(&probe, "jwide(%q, %q, %q, %q);\n", "jsonsites", mod, "Sites", "narrow")
	for _, c := range cases {
		for _, variant := range []struct{ name, body string }{{"number", c.number}, {"quoted", c.quoted}} {
			key := c.name + "-" + variant.name
			inputs[key+".json"] = []byte(variant.body)
			fmt.Fprintf(&probe, "jwide(%q, %q, %q, %q);\n", "jsonsites", mod, "Sites", key)
		}
	}
	writeFile(t, filepath.Join(out, "main.nut"), []byte(probe.String()))
	got := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, out, "main.nut", inputs)), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[0] != "jwide" {
			t.Fatalf("unexpected probe line %q", line)
		}
		got[f[1]] = f[2:]
	}
	for _, c := range cases {
		v := got[c.name+"-number"]
		if len(v) < 2 || v[0] != "refused" || v[1] != "kind_mismatch" {
			t.Errorf("%s number: got %v, want kind_mismatch", c.name, v)
		}
		v = got[c.name+"-quoted"]
		if len(v) < 2 || v[0] == "refused" || v[0] == "null" {
			t.Errorf("%s quoted: got %v, want accepted", c.name, v)
			continue
		}
		text, ok := unhex(v[0])
		if !ok || !strings.Contains(string(text), c.value) {
			t.Errorf("%s quoted: output %q lacks %s", c.name, text, c.value)
		}
	}
	v := got["narrow"]
	if len(v) < 2 || v[0] == "refused" || v[0] == "null" {
		t.Errorf("narrow: got %v, want accepted", v)
		return
	}
	text, ok := unhex(v[0])
	if !ok || !strings.Contains(string(text), `"narrow": 42`) {
		t.Errorf("narrow: output %q does not retain 42", text)
	}
}
