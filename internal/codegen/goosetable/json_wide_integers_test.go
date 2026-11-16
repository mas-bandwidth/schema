package goosetable

// THE JSON FORM'S WIDE INTEGERS ARE STRINGS, AND EVERY INTEGER TAKES AN
// INTEGER TOKEN (docs/SPEC-TABLES.md §16.2, §16.14 W10/W11;
// docs/rulings.json json-wide-integers-are-strings and
// json-integer-token-only). An integer whose storage is wider than 32 bits is
// written and read as a JSON string of decimal digits; every integer field takes an integer token only —
// digits with an optional minus, no fraction, no exponent, not -0.
//
// The shared vectors under testdata/wire/json-integers name a verdict per
// row. Every reading row is read through the variable class's text walk
// (runtime/json_value.goose: the unit holds a map, a 128-bit kind and a
// fixed-point kind) and written back, and the text must be the committed
// canonical text byte for byte. A second unit in the same schema exercises
// the same rules at an array element, an optional, a union arm and a map
// value, through BOTH the typed walk (runtime/schema_json.goose) and the
// variable walk, each with a hand-authored canonical text.

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// jsonIntegersSchema is the vectors' unit plus a typed and a variable table
// over the same kinds, so every integer site — a field, an array element, a
// map value and a map key, an optional and an arm — is touched by one
// generation.
const jsonIntegersSchema = `package jsonintegers

enum Key { A, B }

union Choice
{
    wide  int64
    small int32
}

table Typed
{
    nums  [..4]int64
    maybe ?uint64
    pick  Choice
    tail  int32
    keyed [Key]int64
}

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

table WideKinds
{
    huge uint128
    mask bits(40)
    amount fixed(112,16) | min = -10, max = 10
    smallfixed fixed(16,16) | min = -10, max = 10
}

table Sites
{
    nums  [..4]int64
    maybe ?uint64
    pick  Choice
    bykey map[uint64]int64
    keyed [Key]int64
}
`

type jsonIntegerRow struct {
	name, root, file, verdict string
	numberToken               bool
}

// readJSONIntegerVectors reads the shared manifest: name, root, json file,
// verdict, what it holds. The reason is per-language detail
// (docs/rulings.json refusal-verdict-only), so only the verdict is held.
func readJSONIntegerVectors(t *testing.T) []jsonIntegerRow {
	t.Helper()
	var rows []jsonIntegerRow
	for _, manifest := range []string{"MANIFEST.tsv", "NUMBER-TOKEN.tsv"} {
		path := filepath.Join(fixedRoot, "testdata", "wire", "json-integers", manifest)
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		before := len(rows)
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			w := strings.Split(line, "\t")
			if len(w) != 5 {
				t.Fatalf("%s: malformed row %q", path, line)
			}
			rows = append(rows, jsonIntegerRow{name: w[0], root: w[1], file: w[2], verdict: w[3], numberToken: manifest == "NUMBER-TOKEN.tsv"})
		}
		if err := sc.Err(); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		if manifest == "NUMBER-TOKEN.tsv" && len(rows)-before != 13 {
			t.Fatalf("NUMBER-TOKEN.tsv: got %d rows, want 13", len(rows)-before)
		}
	}
	return rows
}

// The hand-authored rows for the typed and variable tables. Each reading row
// names the canonical text its instance writes back, byte for byte; every
// refusing row is refused whole.
var jsonIntegerSiteRows = []struct {
	name, root, class, text, want string
}{
	{
		name: "typed-sites", root: "Typed", class: "typed",
		text: `{"nums":["-9223372036854775808","3"],"maybe":"18446744073709551615","pick":{"wide":"9007199254740993"},"tail":7,"keyed":{"A":"7"}}`,
		want: "{\n  \"nums\": [\n    \"-9223372036854775808\",\n    \"3\"\n  ],\n  \"maybe\": \"18446744073709551615\",\n  \"pick\": {\n    \"wide\": \"9007199254740993\"\n  },\n  \"tail\": 7,\n  \"keyed\": {\n    \"A\": \"7\",\n    \"B\": \"0\"\n  }\n}\n",
	},
	{
		name: "variable-sites", root: "Sites", class: "variable",
		text: `{"nums":["-9223372036854775808","3","4"],"maybe":"18446744073709551615","pick":{"wide":"9007199254740993"},"bykey":{"9007199254740993":"5","0":"-6"},"keyed":{"A":"7"}}`,
		want: "{\n  \"nums\": [\n    \"-9223372036854775808\",\n    \"3\",\n    \"4\"\n  ],\n  \"maybe\": \"18446744073709551615\",\n  \"pick\": {\n    \"wide\": \"9007199254740993\"\n  },\n  \"bykey\": {\n    \"0\": \"-6\",\n    \"9007199254740993\": \"5\"\n  },\n  \"keyed\": {\n    \"A\": \"7\",\n    \"B\": \"0\"\n  }\n}\n",
	},
	{name: "wide-kinds-strings", root: "WideKinds", class: "variable", text: `{"huge":"7","mask":"7","amount":"1.5","smallfixed":1.5}`, want: "{\n  \"huge\": \"7\",\n  \"mask\": \"7\",\n  \"amount\": \"1.5\",\n  \"smallfixed\": 1.5\n}\n"},
	{name: "uint128-number", root: "WideKinds", class: "variable", text: `{"huge":7}`},
	{name: "bits-number", root: "WideKinds", class: "variable", text: `{"mask":7}`},
	{name: "fixed128-number", root: "WideKinds", class: "variable", text: `{"amount":1}`},
	{name: "typed-array-number", root: "Typed", class: "typed", text: `{"nums":[7]}`},
	{name: "typed-optional-number", root: "Typed", class: "typed", text: `{"maybe":7}`},
	{name: "typed-arm-number", root: "Typed", class: "typed", text: `{"pick":{"wide":7}}`},
	{name: "typed-keyed-number", root: "Typed", class: "typed", text: `{"keyed":{"A":7}}`},
	{name: "variable-array-number", root: "Sites", class: "variable", text: `{"nums":[7]}`},
	{name: "variable-optional-number", root: "Sites", class: "variable", text: `{"maybe":7}`},
	{name: "variable-arm-number", root: "Sites", class: "variable", text: `{"pick":{"wide":7}}`},
	{name: "variable-map-number", root: "Sites", class: "variable", text: `{"bykey":{"1":7}}`},
	{name: "variable-keyed-number", root: "Sites", class: "variable", text: `{"keyed":{"A":7}}`},
	{name: "typed-array-fraction", root: "Typed", class: "typed", text: `{"nums":[2.0]}`},
	{name: "typed-optional-neg-zero", root: "Typed", class: "typed", text: `{"maybe":-0}`},
	{name: "typed-arm-exponent", root: "Typed", class: "typed", text: `{"pick":{"wide":1e3}}`},
	{name: "typed-arm-string-neg-zero", root: "Typed", class: "typed", text: `{"pick":{"wide":"-0"}}`},
	{name: "typed-narrow-fraction", root: "Typed", class: "typed", text: `{"tail":2.0}`},
	{name: "variable-array-fraction", root: "Sites", class: "variable", text: `{"nums":[2.0]}`},
	{name: "variable-optional-neg-zero", root: "Sites", class: "variable", text: `{"maybe":-0}`},
	{name: "variable-arm-exponent", root: "Sites", class: "variable", text: `{"pick":{"wide":1e3}}`},
	{name: "variable-arm-string-neg-zero", root: "Sites", class: "variable", text: `{"pick":{"wide":"-0"}}`},
	{name: "variable-map-value-fraction", root: "Sites", class: "variable", text: `{"bykey":{"1":"2.0"}}`},
	{name: "variable-map-key-fraction", root: "Sites", class: "variable", text: `{"bykey":{"1.0":2}}`},
	{name: "variable-array-one-past", root: "Sites", class: "variable", text: `{"nums":["9223372036854775808"]}`},
}

func TestGooseJsonWideIntegersAreStrings(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Goose toolchain (it reads and writes every shared json-integers vector)")
	rows := readJSONIntegerVectors(t)
	if len(rows) == 0 {
		t.Fatal("the shared json-integers vectors are empty")
	}
	canonical, err := os.ReadFile(filepath.Join(fixedRoot, "testdata", "wire", "json-integers", "canonical.read.json"))
	if err != nil {
		t.Fatal(err)
	}

	src := t.TempDir()
	schema := filepath.Join(src, "Integers.schema")
	if err := os.WriteFile(schema, []byte(jsonIntegersSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := loadSchemaPaths([]string{schema})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatalf("goose: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("goosetable: %v", err)
	}
	maps.Copy(files, tables)

	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	slices.Sort(mods)
	ns := goose.Namespace(u)

	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	b.WriteString("\n" + gooserun.HexProbe + "\n")
	b.WriteString(strings.ReplaceAll(`// variable reads one text through the flat value table's walk and prints
// the canonical text its instance writes back, or refused.
fn variable(name: const u8[:], root: const u8[:]) {
    var text: u8[>..] = [];
    if !read_file(name, text) { print("read-failed"); return; }
    var data: u8[>..] = [];
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var r = schema_rt::json_report();
    let at = schema_rt::json_load_table(NS::TableUnit, NS::TextUnit, root, text, data, vals, nodes, r);
    if at < 0 || r.damaged { if r.mismatch > 0 { print("refused_mismatch"); } else { print("refused"); } return; }
    var out: u8[>..] = [];
    let n = schema_rt::json_save_table(NS::TableUnit, NS::TextUnit, root, data, vals, nodes, at, out);
    if n < 0 || n != (out.len as! i64) { print("short"); return; }
    hex(out);
}

fn typed(name: const u8[:]) {
    var text: u8[>..] = [];
    if !read_file(name, text) { print("read-failed"); return; }
    var v = NS::NewTyped();
    var report = schema_rt::json_report();
    if !NS::TypedFromJson(v, text, report) || report.damaged { if report.mismatch > 0 { print("refused_mismatch"); } else { print("refused"); } return; }
    let m = NS::TypedToJsonMeasure(v);
    var out: u8[>..] = [];
    let n = NS::TypedToJson(v, out);
    if m < 0 || n < 0 || m != n || n != (out.len as! i64) { print("short"); return; }
    hex(out);
}

fn main() {
`, "NS", ns))

	inputs := map[string][]byte{}
	type probe struct {
		name, class, root string
		want              []byte
		refused           bool
		mismatch          bool
	}
	var probes []probe
	for i, row := range rows {
		data, err := os.ReadFile(filepath.Join(fixedRoot, "testdata", "wire", "json-integers", row.file))
		if err != nil {
			t.Fatal(err)
		}
		name := fmt.Sprintf("in%d.json", i)
		inputs[name] = data
		p := probe{name: name, class: "variable", root: row.root, mismatch: row.numberToken}
		if row.verdict == "refused" {
			p.refused = true
		} else {
			// the shared file is the document without its final newline; the
			// canonical writer ends the text with exactly one (§16.14 W2)
			p.want = append(append([]byte{}, canonical...), '\n')
		}
		probes = append(probes, p)
		fmt.Fprintf(&b, "    variable(%q, %q);\n", name, row.root)
	}
	for i, row := range jsonIntegerSiteRows {
		name := fmt.Sprintf("site%d.json", i)
		inputs[name] = []byte(row.text)
		p := probe{name: name, class: row.class, root: row.root, refused: row.want == "", mismatch: strings.HasSuffix(row.name, "-number")}
		if !p.refused {
			p.want = []byte(row.want)
		}
		probes = append(probes, p)
		if row.class == "typed" {
			fmt.Fprintf(&b, "    typed(%q);\n", name)
		} else {
			fmt.Fprintf(&b, "    variable(%q, %q);\n", name, row.root)
		}
	}
	b.WriteString("}\n")

	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(gooserun.Run(t, dir, "main.goose", inputs)), "\n"), "\n")
	if len(lines) != len(probes) {
		t.Fatalf("the probe answered %d rows for %d texts:\n%s", len(lines), len(probes), strings.Join(lines, "\n"))
	}
	for i, p := range probes {
		got := lines[i]
		if p.refused {
			if p.mismatch && got != "refused_mismatch" {
				t.Errorf("%s (%s): read %q, want kind_mismatch", p.name, p.class, got)
			}
			if !p.mismatch && got != "refused" && got != "refused_mismatch" {
				t.Errorf("%s (%s): read %q, want refused", p.name, p.class, got)
			}
			continue
		}
		want := hex.EncodeToString(p.want)
		if got != want {
			raw, err := hex.DecodeString(got)
			if err != nil {
				t.Errorf("%s (%s): the probe printed %q: %v", p.name, p.class, got, err)
				continue
			}
			t.Errorf("%s (%s): wrote\n%s\nwant\n%s", p.name, p.class, raw, p.want)
		}
	}
}
