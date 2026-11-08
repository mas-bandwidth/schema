package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

// mapJsonRoundTrip reads each input's text into a fresh root and writes it
// back, printing the text as hex and the §4 report on one line.
const mapJsonRoundTrip = `
local roundtrip = function(unit, mod, root, key) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local J = schema.require("schema_table_json");
  local r = fresh();
  local v = J.fromJsonWalk(T, root, inputs[key + ".json"], r);
  local w = r.bodyDamaged ? null : J.ToJson(T, root, v);
  print("row " + key + " " + hexOf(w) + " " + reportLine(r) + "\n");
};
local strict = function(unit, mod, root, key) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local J = schema.require("schema_table_json");
  local v = J.FromJson(T, root, inputs[key + ".json"], {});
  print("row " + key + " - " + (v == null ? "refused" : "read") + "\n");
};
`

// TestSquirrelMapsInJson carries docs/SPEC-TABLES.md §2.8's map through §16.2's
// text mapping: every json corpus row on mapdemo's carried tables, FromJson
// then ToJson, must equal the corpus text byte for byte with a clean report;
// and the key-domain text of internal/codegen/gotable's TestMapJsonKeyDomains
// reads with the same report counters and writes the same key spellings the
// Go oracle (internal/codegen/gotable's TestMapJsonKeyDomains) does.
func TestSquirrelMapsInJson(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u, _ := mapdemoCarried(t, root)
	dir := t.TempDir()
	mod := emitUnit(t, dir, "mapdemo", u)

	var p strings.Builder
	p.WriteString(jsonPrelude)
	p.WriteString(mapJsonRoundTrip)
	inputs := map[string][]byte{}
	var names []string
	for _, in := range readInstanceRows(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")) {
		// a `no-text` row has no spelling by the form's own rule (§16.7), so
		// the corpus carries no text for it
		if in.unit != "mapdemo" || in.noText {
			continue
		}
		text, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json", in.name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[in.name+".json"] = text
		names = append(names, in.name)
		fmt.Fprintf(&p, "roundtrip(%q, %q, %q, %q);\n", in.unit, mod, in.root, in.name)
	}
	if len(names) == 0 {
		t.Fatal("the mapdemo corpus names no json row Squirrel can carry")
	}

	// THE KEY-DOMAIN TEXTS of internal/codegen/gotable's TestKeys
	// (docs/SPEC-TABLES.md §2.8, §16.2): a key is an identity, so an integer
	// key spelled 1.0 is the key 1 and 1e0 after it is the same key given
	// twice, damage; a key outside the kind's range or with a fraction, and a
	// key past its storage bound, refuse the whole text (strict-only).
	keyUnit := loadSource(t, `package probemap
table Root { keys map[int8]int32
 wide map[uint64]int32
 names map[string(300)]int32
 tiny map[string(3)]int32 }
`)
	keyMod := emitUnit(t, dir, "probemap", keyUnit)
	long := strings.Repeat("a", 280)
	keyText := `{"keys":{"1.0":3,"-1e+2":4},"wide":{"18446744073709551615":9,"-0":2},"names":{"` + long + `":5},"tiny":{"a":2}}`
	inputs["keydomains.json"] = []byte(keyText)
	fmt.Fprintf(&p, "roundtrip(%q, %q, %q, %q);\n", "probemap", keyMod, "Root", "keydomains")
	badKeys := []string{`{"keys":{"128":1}}`, `{"keys":{"-129":1}}`, `{"keys":{"0.5":1}}`, `{"keys":{"1.0":2,"1e0":3}}`,
		`{"wide":{"18446744073709551616":1}}`, `{"wide":{"-1":1}}`, `{"tiny":{"😀":1}}`}
	for i, bad := range badKeys {
		key := fmt.Sprintf("badkey%d", i)
		inputs[key+".json"] = []byte(bad)
		fmt.Fprintf(&p, "strict(%q, %q, %q, %q);\n", "probemap", keyMod, "Root", key)
	}

	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	got := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) != 4 || f[0] != "row" {
			t.Fatalf("probe line %q is not a row", line)
		}
		got[f[1]] = f[2:]
	}

	compared := 0
	for _, name := range names {
		g, ok := got[name]
		if !ok {
			t.Errorf("%s: the probe printed no row", name)
			continue
		}
		want, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		have, ok := unhex(g[0])
		switch {
		case g[1] != "0,0,0,0,0,false,read":
			t.Errorf("%s: FromJson of the corpus text reported %s, want a clean read", name, g[1])
		case !ok:
			t.Errorf("%s: ToJson refused", name)
		case string(have) != string(want):
			t.Errorf("%s: FromJson then ToJson differs from the corpus text\n got %q\nwant %q", name, have, want)
		default:
			compared++
		}
	}

	// every bad key refuses its text (the Go oracle's TestKeys)
	for i, bad := range badKeys {
		key := fmt.Sprintf("badkey%d", i)
		if g, ok := got[key]; !ok || g[1] != "refused" {
			t.Errorf("%s: the strict FromJson of %s answered %v, want refused", key, bad, g)
		}
	}

	// THE GO ORACLE'S ANSWER for the clean text (docs/SPEC-TABLES.md §2.8,
	// §16.2): the key spellings are the key identities: 1.0 and -1e+2 are
	// integers, -0 is zero, and a uint64 key past the signed half writes as
	// its full unsigned decimal.
	const wantReport = "0,0,0,0,0,false,read"
	want := fmt.Sprintf(`{
  "keys": {
    "-100": 4,
    "1": 3
  },
  "wide": {
    "0": 2,
    "18446744073709551615": 9
  },
  "names": {
    "%s": 5
  },
  "tiny": {
    "a": 2
  }
}
`, long)
	g, ok := got["keydomains"]
	switch {
	case !ok:
		t.Errorf("keydomains: the probe printed no row")
	case g[1] != wantReport:
		t.Errorf("keydomains: Squirrel reports %s, the Go oracle %s", g[1], wantReport)
	default:
		have, ok := unhex(g[0])
		switch {
		case !ok:
			t.Errorf("keydomains: ToJson refused")
		case string(have) != want:
			t.Errorf("keydomains: ToJson differs from the Go oracle\n got %q\nwant %q", have, want)
		default:
			compared++
		}
	}
	if compared != len(names)+1 {
		t.Errorf("compared %d of %d json rows; the corpus and the key-domain text are not both reached", compared, len(names)+1)
	}
	t.Logf("compared %d mapdemo json rows and the key-domain text with Go's oracle", compared)
}
