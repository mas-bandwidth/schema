package goosetable

// NESTING DEPTH IS NOT GENERIC DEPTH (docs/SPEC-TABLES.md §16.2): the text
// form's readers and writers reach a nested value through a plain call to its
// type's own reader, so a chain of tables, arrays of tables and unions nests
// past the 16 generic specializations Goose allows on one call path (§7.8 of
// its spec). The unit chains jsonDeepLevels tables, every third level
// through an array of the next and every third through a union arm, so each
// container is 17 deep. Each row's report line and written text are the Go
// engine's (internal/tabletext) over the same unit.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const jsonDeepLevels = 51

// jsonDeepSchema is the chain: L<k> holds L<k+1> directly when k%3 is 0, as
// the elements of a [..2] array when 1, and as union U<k>'s arm when 2.
func jsonDeepSchema() string {
	var b strings.Builder
	b.WriteString("package probe\n\nfixed table Root {\n    l L0\n}\n")
	for k := 0; k < jsonDeepLevels; k++ {
		next := fmt.Sprintf("L%d", k+1)
		fmt.Fprintf(&b, "\nfixed table L%d {\n", k)
		switch k % 3 {
		case 0:
			fmt.Fprintf(&b, "    n %s\n", next)
		case 1:
			fmt.Fprintf(&b, "    a [..2]%s\n", next)
		case 2:
			fmt.Fprintf(&b, "    u U%d\n", k)
		}
		b.WriteString("    v int32\n}\n")
		if k%3 == 2 {
			fmt.Fprintf(&b, "\nunion U%d {\n    x %s\n}\n", k, next)
		}
	}
	fmt.Fprintf(&b, "\nfixed table L%d {\n    v int32\n}\n", jsonDeepLevels)
	return b.String()
}

// jsonDeepText is a value for every level, with extra at the deepest level.
func jsonDeepText(extra string) string {
	inner := `{"v": 1` + extra + `}`
	for k := jsonDeepLevels - 1; k >= 0; k-- {
		switch k % 3 {
		case 0:
			inner = fmt.Sprintf(`{"n": %s, "v": %d}`, inner, k)
		case 1:
			inner = fmt.Sprintf(`{"v": %d, "a": [%s, {}]}`, k, inner)
		case 2:
			inner = fmt.Sprintf(`{"u": {"x": %s}, "v": %d}`, inner, k)
		}
	}
	return `{"l": ` + inner + `}`
}

func jsonDeepTexts() []string {
	return []string{
		jsonDeepText(""),
		jsonDeepText(`, "mystery": [[{"a": 1}]]`),
		jsonDeepText(`, "v": "wrong shape"`),
		jsonDeepText(`, "v": 2`),
		`{"l": {"n": {"a": [{"u": {}}, {"u": {"nope": 1}}, {}]}}}`,
	}
}

func jsonDeepUnit(t *testing.T) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(path, []byte(jsonDeepSchema()), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := loadSchemaPaths([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestGooseJSONNestsPastTheGenericBound(t *testing.T) {
	t.Parallel()
	u := jsonDeepUnit(t)
	texts := jsonDeepTexts()

	model := tabletext.NewModel(u)
	var want []string
	for _, text := range texts {
		inst := model.New(model.Lookup("Root"))
		var r tabletext.Report
		if !model.Read(inst, []byte(text), &r) || r.Malformed {
			t.Fatalf("%s: the Go engine refused the text (%+v)", text, r)
		}
		out, err := model.Write(inst)
		if err != nil {
			t.Fatalf("%s: the Go engine's text: %v", text, err)
		}
		out = append(out, '\n') // ToJson ends its text with a newline
		want = append(want, fmt.Sprintf("%d,%d,%d,%d,%d,false,read %x", r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, out))
	}

	files, err := goose.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	ns := goose.Namespace(u)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	b.WriteString("\n" + gooserun.HexProbe + "\n")
	b.WriteString("fn probe(name: const u8[:]) {\n")
	b.WriteString("    var text: u8[>..] = [];\n    if !read_file(name, text) { print(\"read-failed\"); return; }\n")
	fmt.Fprintf(&b, "    var v = %s::NewRoot();\n    var r = schema_rt::json_report();\n", ns)
	fmt.Fprintf(&b, "    let ok = %s::RootFromJsonTolerant(v, text, r);\n", ns)
	b.WriteString("    if !ok || r.malformed { print(\"refused\"); print(\"-\"); return; }\n")
	b.WriteString("    print(r.unknown, \",\", r.kind_mismatch, \",\", r.widened, \",\", r.clamped, \",\", r.duplicate, \",false,read\");\n")
	fmt.Fprintf(&b, "    var out: u8[>..] = [];\n    let n = %s::RootToJson(v, out);\n    if n != out.len { print(\"short\"); }\n    hex(out);\n", ns)
	b.WriteString("}\n\nfn main() {\n")
	inputs := map[string][]byte{}
	for i, text := range texts {
		name := fmt.Sprintf("in%d.json", i)
		inputs[name] = []byte(text)
		fmt.Fprintf(&b, "    probe(%q);\n", name)
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
	if len(lines) != 2*len(texts) {
		t.Fatalf("the probe answered %d lines for %d texts:\n%s", len(lines), len(texts), strings.Join(lines, "\n"))
	}
	for i := range texts {
		if got := lines[2*i] + " " + lines[2*i+1]; got != want[i] {
			t.Errorf("text %d: Goose lands %.200s, the Go engine %.200s", i, got, want[i])
		}
	}
}
