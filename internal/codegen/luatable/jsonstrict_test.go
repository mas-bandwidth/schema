package luatable

// THE PLAIN FromJson IS THE ONE STRICT TEXT READ (docs/SPEC-TABLES.md §6.5,
// §16.2; docs/plans/strict-only.md): it refuses a text that is damaged, out of
// range, of the wrong kind, a repeated key or a lone surrogate, answering nil,
// while unknown alone reads clean. Its verdict is held to the generated Go's
// (internal/codegen/gotable) and to the manifest's.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type strictJSONRow struct {
	name     string
	unit     string
	root     string
	textPath string
	expected string
	hostile  bool
}

// strictJSONRows reads every JSON-bearing row of the tables manifest: the
// instance rows (whose text is testdata/conformance/tables/json/<name>.json)
// and the json-hostile rows (whose text and report line live in the row).
func strictJSONRows(t *testing.T, repo, manifestPath string) []strictJSONRow {
	t.Helper()
	var rows []strictJSONRow
	for _, r := range jsonManifestRows(t, manifestPath, "instance") {
		if r.noText {
			continue
		}
		rows = append(rows, strictJSONRow{
			name:     r.caseName,
			unit:     r.unit,
			root:     r.root,
			textPath: filepath.Join(repo, "testdata", "conformance", "tables", "json", r.caseName+".json"),
		})
	}
	for _, r := range jsonManifestRows(t, manifestPath, "json-hostile") {
		rows = append(rows, strictJSONRow{
			name:     r.caseName,
			unit:     r.unit,
			root:     r.root,
			textPath: filepath.Join(repo, r.path, r.root+".json"),
			expected: r.expected,
			hostile:  true,
		})
	}
	return rows
}

// strictLuaProbe prints one line per row: the plain read's verdict, ok or
// refused.
func strictLuaProbe(module string, rows []strictJSONRow) string {
	var b strings.Builder
	b.WriteString(`local T = require("` + module + `")
local J = require("json")
local inputs = require("inputs")
local rows = {
`)
	for i, row := range rows {
		fmt.Fprintf(&b, "  { key = %q, root = %q },\n", fmt.Sprintf("row%d", i), row.root)
	}
	b.WriteString(`}
for _, row in ipairs(rows) do
  local v = J.FromJson(T, row.root, inputs[row.key], {})
  print(row.key .. "|strict|" .. (v == nil and "refused" or "ok"))
end
`)
	return b.String()
}

// strictGoProbe writes the generated Go oracle test for one unit: one plain
// FromJson per row, its verdict ok or refused.
func strictGoProbe(pkg string, rows []strictJSONRow, texts map[string][]byte, fixed map[string]bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestStrictJSONOracle(t *testing.T) {\n", pkg)
	b.WriteString("\tf, err := os.Create(\"oracle.txt\")\n\tif err != nil { t.Fatal(err) }\n\tdefer f.Close()\n")
	for i, row := range rows {
		text := texts[fmt.Sprintf("row%d", i)]
		if fixed[row.root] {
			fmt.Fprintf(&b, "\t{\n\t\tvar v %s\n\t\tvar r TableReport\n\t\tok := %sFromJSON(&v, []byte(%q), &r)\n", row.root, row.root, text)
		} else {
			fmt.Fprintf(&b, "\t{\n\t\tvar v %sBuilder\n\t\tv.Init()\n\t\tvar r TableReport\n\t\tok := %sFromJSON(&v, []byte(%q), &r)\n", row.root, row.root, text)
		}
		fmt.Fprintf(&b, "\t\tverdict := \"ok\"\n\t\tif !ok {\n\t\t\tverdict = \"refused\"\n\t\t}\n")
		fmt.Fprintf(&b, "\t\tif _, err := f.WriteString(%q + \"|\" + verdict + \"\\n\"); err != nil { t.Fatal(err) }\n", fmt.Sprintf("row%d", i))
		if !fixed[row.root] {
			b.WriteString("\t\tv.Shutdown()\n")
		}
		b.WriteString("\t}\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// TestLuaFromJsonIsStrict pins docs/SPEC-TABLES.md §6.5 and §16.2 over every
// JSON-bearing row of the tables manifest: the plain FromJson's VERDICT is the
// generated Go's and the manifest's (a hostile row's read or refused, a clean
// read for an instance's canonical text). Reason names are never compared
// across languages (docs/rulings.json refusal-verdict-only).
func TestLuaFromJsonIsStrict(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "go and the pinned lua")
	repo := filepath.Join("..", "..", "..")
	// the generated Go oracle links the PINNED serialize.go (the sibling
	// checkout CI clones at SERIALIZE_GO_TAG, or make deps' fetched copy),
	// never whatever version the module cache happens to hold
	serializeGo := slowtest.SerializeGo(t, repo)
	manifestPath := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifestPath)
	unitFiles := map[string][]string{}
	for k, paths := range units {
		var full []string
		for _, p := range paths {
			full = append(full, filepath.Join(repo, p))
		}
		unitFiles[k] = full
	}

	byUnit := map[string][]strictJSONRow{}
	var order []string
	for _, row := range strictJSONRows(t, repo, manifestPath) {
		if _, ok := byUnit[row.unit]; !ok {
			order = append(order, row.unit)
		}
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}

	compared := 0
	for _, unit := range order {
		rows := byUnit[unit]
		paths, ok := unitFiles[unit]
		if !ok {
			t.Fatalf("unit %s has no manifest unit", unit)
		}
		u := luarun.LoadUnit(t, paths...)
		if len(ir.TableWideFields(u)) > 0 {
			t.Logf("unit %s carries the wide kinds; its JSON rows are not this card's (docs/SPEC-TABLES.md §15)", unit)
			continue
		}
		luaFiles, err := Generate(u)
		if err != nil {
			t.Logf("unit %s is not generated for lua: %v", unit, err)
			continue
		}

		texts := map[string][]byte{}
		for i, row := range rows {
			data, err := os.ReadFile(row.textPath)
			if err != nil {
				t.Fatalf("%s: read text: %v", row.name, err)
			}
			texts[fmt.Sprintf("row%d", i)] = data
		}

		luaDir := t.TempDir()
		luarun.WriteFiles(t, luaDir, luaFiles)
		if err := os.WriteFile(filepath.Join(luaDir, "main.lua"), []byte(strictLuaProbe(ModuleName(u), rows)), 0o644); err != nil {
			t.Fatal(err)
		}
		out := string(luarun.Run(t, luaDir, "main.lua", texts))
		luaVerdict := map[string]string{}
		for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
			parts := strings.SplitN(line, "|", 3)
			if len(parts) != 3 {
				t.Fatalf("%s: bad lua probe line %q", unit, line)
			}
			luaVerdict[parts[0]+"|"+parts[1]] = parts[2]
		}

		goFiles, err := golang.Generate(u)
		if err != nil {
			t.Fatalf("unit %s: generate Go: %v", unit, err)
		}
		tables, err := gotable.Generate(u)
		if err != nil {
			t.Fatalf("unit %s: generate Go tables: %v", unit, err)
		}
		maps.Copy(goFiles, tables)
		fixed := map[string]bool{}
		for _, row := range rows {
			st := u.Tables[row.root]
			fixed[row.root] = st != nil && st.FixedDeclared
		}
		goFiles["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", serializeGo))
		goFiles["strict_oracle_test.go"] = []byte(strictGoProbe(u.Package, rows, texts, fixed))
		goDir := t.TempDir()
		for name, data := range goFiles {
			if err := os.WriteFile(filepath.Join(goDir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command("nice", "-n", "19", "go", "test", "-count=1", "-timeout", "600s", "-run", "^TestStrictJSONOracle$", ".")
		cmd.Dir = goDir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("the generated Go oracle for unit %s: %v\n%s", unit, err, output)
		}
		oracle, err := os.ReadFile(filepath.Join(goDir, "oracle.txt"))
		if err != nil {
			t.Fatal(err)
		}
		goVerdict := map[string]string{}
		for line := range strings.SplitSeq(strings.TrimRight(string(oracle), "\n"), "\n") {
			key, verdict, ok := strings.Cut(line, "|")
			if !ok {
				t.Fatalf("%s: bad Go oracle line %q", unit, line)
			}
			goVerdict[key] = verdict
		}

		for i, row := range rows {
			key := fmt.Sprintf("row%d", i)
			want := goVerdict[key]
			if got := luaVerdict[key+"|strict"]; got != want {
				t.Errorf("%s: plain FromJson answered %q, the generated Go %q", row.name, got, want)
			}
			// the manifest's own verdict: a hostile row's read or refused, a
			// canonical instance text read
			wantManifest := "ok"
			if row.hostile && row.expected != "read" {
				wantManifest = "refused"
			}
			if got := luaVerdict[key+"|strict"]; got != wantManifest {
				t.Errorf("%s: plain FromJson answered %q, the manifest %q", row.name, got, wantManifest)
			}
			compared++
		}
	}
	if compared == 0 {
		t.Fatal("no JSON row compared")
	}
	t.Logf("%d JSON rows: the plain FromJson's verdict agrees with the generated Go and the manifest", compared)
}
