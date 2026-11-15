package goosetable

// A REPEATED KEY REFUSES THE READ IN EVERY OBJECT OF THE TEXT
// (docs/SPEC-TABLES.md §16.2, §16.12). The rule reaches an object's keys at
// any position: a name this build knows, one it does not, and one inside a
// value the reader only skips. Keys compare as DECODED bytes, so a key and
// its \u-escaped spelling are the same key, and the same name in SIBLING
// objects stays lawful.
//
// Both Goose JSON readers are held to the rule against the compiler's own
// engine (internal/tabletext) reading the same text: the typed
// <Root>FromJson walk (runtime/schema_json.goose) and the flat
// json_load_table walk (runtime/json_value.goose). The root is
// tables/examples' KeyedConfig, whose [Team]TeamConfig field is an object
// keyed by variant names, so the enum-keyed row is exercised too.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const repeatedKeyRoot = "KeyedConfig"

type repeatedKeyCase struct {
	name string
	text string
}

// repeatedKeyEnumText is one skipped enum-keyed object with 65 distinct
// unknown variant names, then the last name again. Sixty-four hashes no
// longer see the repeat.
func repeatedKeyEnumText() string {
	var b strings.Builder
	b.WriteString(`{"teams":{`)
	for i := 0; i <= 64; i++ {
		fmt.Fprintf(&b, `"u%d":{},`, i)
	}
	b.WriteString(`"u64":{}}}`)
	return b.String()
}

func repeatedKeyCases() []repeatedKeyCase {
	return []repeatedKeyCase{
		{"unknown-key-repeated", `{"bogus":1,"bogus":2}`},
		{"skipped-object-repeated", `{"bogus":{"k":1,"k":2}}`},
		{"skipped-array-repeated", `{"bogus":[{"k":1,"k":2}]}`},
		{"unknown-known-unknown", `{"bogus":1,"teams":{},"bogus":2}`},
		{"skipped-object-escape-repeated", `{"bogus":{"k":1,"\u006b":2}}`},
		{"unknown-escape-repeated", `{"bogus":1,"\u0062ogus":2}`},
		{"unknown-enum-past-64", repeatedKeyEnumText()},
		{"control-one-unknown", `{"bogus":1}`},
		{"control-two-unknown", `{"bogus":1,"other":2}`},
		{"control-skipped-two-inner", `{"bogus":{"k":1,"j":2}}`},
		{"control-sibling-objects", `{"a":{"k":1},"b":{"k":2}}`},
		{"control-known-escaped", `{"te\u0061ms":{}}`},
	}
}

const repeatedKeyFlatSkipCheck = `            if json_keyset_seen(c.kset_entries, c.kset_indices, c.kset_slots, kset, key[..n]) {
                c.jr.rep.repeated += 1;
                return json_fail(c.jr);
            }
`

const repeatedKeyTypedSkipCheck = `                if json_keyset_seen(jr.kset_entries, jr.kset_indices, jr.kset_slots, kset, key[..n]) {
                    return json_fail(jr);
                }
`

// repeatedKeyRemoveSkipSet is the negative control's edit: it deletes the
// skipped object's key-set check from both readers' runtimes. A test that
// stays green without it never held the skip to the rule.
func repeatedKeyRemoveSkipSet(t *testing.T, files map[string][]byte) {
	t.Helper()
	for file, cut := range map[string]string{
		"json_value.goose":  repeatedKeyFlatSkipCheck,
		"schema_json.goose": repeatedKeyTypedSkipCheck,
	} {
		src, ok := files[file]
		if !ok {
			t.Fatalf("NEGATIVE CONTROL FAILED: no %s in the generated runtime", file)
		}
		if !strings.Contains(string(src), cut) {
			t.Fatalf("NEGATIVE CONTROL FAILED: %s has no skip key-set check", file)
		}
		files[file] = []byte(strings.Replace(string(src), cut, "", 1))
	}
}

// repeatedKeyRun generates tables/examples, lets edit change the emitted
// files, and answers every row whose two readers disagree with the engine
// (or with each other's expected verdict).
func repeatedKeyRun(t *testing.T, edit func(*testing.T, map[string][]byte)) (fails []string, cases int) {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(fixedRoot, "tables", "examples")})
	if err != nil {
		t.Fatalf("gather tables/examples: %v", err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load tables/examples: %v", err)
	}
	model := tabletext.NewModel(u)
	root := model.Lookup(repeatedKeyRoot)
	if root == nil {
		t.Fatalf("tables/examples has no root %s", repeatedKeyRoot)
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
	if edit != nil {
		edit(t, files)
	}

	rows := repeatedKeyCases()
	ns := goose.Namespace(u)
	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	slices.Sort(mods)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	b.WriteString(`
fn probe(name: const u8[:], typed: bool) {
    var text: u8[>..] = [];
    if !read_file(name, text) { print("read-failed"); return; }
    var r = schema_rt::json_report();
    var ok = false;
    if typed {
        var v = ` + ns + `::New` + repeatedKeyRoot + `();
        ok = ` + ns + `::` + repeatedKeyRoot + `FromJson(v, text, r);
    } else {
        var data: u8[>..] = [];
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        let at = schema_rt::json_load_table(` + ns + `::TableUnit, ` + ns + `::TextUnit, "` + repeatedKeyRoot + `", text, data, vals, nodes, r);
        ok = at >= 0;
    }
    if ok && !r.damaged { print("read"); } else { print("refused"); }
}

fn main() {
`)
	for i := range rows {
		fmt.Fprintf(&b, "    probe(\"in%d.json\", false);\n", i)
		fmt.Fprintf(&b, "    probe(\"in%d.json\", true);\n", i)
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
	inputs := map[string][]byte{}
	for i, row := range rows {
		inputs[fmt.Sprintf("in%d.json", i)] = []byte(row.text)
	}
	out := strings.Split(strings.TrimRight(string(gooserun.Run(t, dir, "main.goose", inputs)), "\n"), "\n")
	if len(out) != 2*len(rows) {
		t.Fatalf("%d answers for %d texts:\n%s", len(out), len(rows), strings.Join(out, "\n"))
	}
	for i, row := range rows {
		inst := model.New(root)
		var report tabletext.Report
		ok := model.Read(inst, []byte(row.text), &report)
		want := "refused"
		if ok && !report.Refused {
			want = "read"
		}
		for r, reader := range []string{"flat", "typed"} {
			got := out[2*i+r]
			if got != want {
				fails = append(fails, fmt.Sprintf("%s/%s: got %s, want %s", row.name, reader, got, want))
			}
		}
	}
	return fails, len(rows)
}

func TestGooseJSONRepeatedKeyEveryObject(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Goose toolchain (it compiles and runs both JSON reads)")
	fails, cases := repeatedKeyRun(t, nil)
	for _, f := range fails {
		t.Error(f)
	}
	if cases == 0 {
		t.Fatal("no text was read: the gate would pass vacuously")
	}
	t.Logf("repeated key: %d texts agree with the engine in both readers", cases)
}

func TestGooseJSONRepeatedKeyEveryObjectNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Goose toolchain (it compiles and runs both JSON reads)")
	fails, _ := repeatedKeyRun(t, repeatedKeyRemoveSkipSet)
	if len(fails) == 0 {
		t.Fatal("NEGATIVE CONTROL FAILED: removing the skip's key-set check left every text green")
	}
	want := []string{
		"skipped-object-repeated",
		"skipped-array-repeated",
		"skipped-object-escape-repeated",
	}
	red := map[string]bool{}
	for _, f := range fails {
		name, _, _ := strings.Cut(f, "/")
		red[name] = true
	}
	for _, name := range want {
		if !red[name] {
			t.Fatalf("NEGATIVE CONTROL FAILED: %s stayed green without the skip's key-set check; red: %v", name, fails)
		}
	}
	t.Logf("repeated key negative control: red on %v", fails)
}
