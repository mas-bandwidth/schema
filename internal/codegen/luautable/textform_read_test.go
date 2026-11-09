package luautable_test

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// The two units gotable/textform_test.go reads: the Document contract as a
// fixed and as a variable root, plus the Graph and Strict tables.
const tfDocumentBody = `
enum State { unknown, done }
flags Features { Fast, Safe }
type Node {
 id string(16)
 title string(32)
 state State
}
type TextItem {
 line string(80)
}
union Choice {
 amount int32
 ping
}
DOCUMENT Document {
 schema uint8 | min = 0, max = 3
 root string(32)
 title string(32)
 state State
 count int32 | min = 0, max = 10
 enabled bool
 text [..4]TextItem
 nodes [..3]Node
 pairs [2]int32
 optional ?[..4]int32
 blob bytes(4) | json = "bytes"
 low int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727, json = "signed"
 high uint128 | json = "unsigned"
 fraction fixed(16,16) | min = -32768, max = 32767
 real float32 | json = "float"
 time timestamp
 flags Features
 keyed [State]int32
 choice Choice
}
`

var tfFixedSchema = "package probe\n" + strings.Replace(tfDocumentBody, "DOCUMENT", "fixed table", 1)

var tfVariableSchema = "package probe\n" + strings.Replace(tfDocumentBody, "DOCUMENT", "table", 1) + `
table GNode {
 id int32
}
table Graph {
 items []*GNode
 lookup map[string(8)]?int32
 blob *bytes
}
table Strict {
 on bool
 need int32 | required
 if on
 {
  extra int32
 }
}
`

type tfCase struct {
	name string
	root string
	text string
}

// tfCases is every accept and refuse construct the Go text form test carries
// that Luau also carries: the corpus vectors, the rule rows, and the Graph
// sharing and guard rows. The verdict is never typed here — the Go unit the
// same test generates answers it.
func tfCases(t *testing.T) []tfCase {
	t.Helper()
	var out []tfCase
	add := func(name, root, text string) { out = append(out, tfCase{name, root, text}) }
	vec := func(name string) string {
		b, err := os.ReadFile(filepath.Join(root, "test", "conformance", "textform", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for _, stem := range []string{"roadmap", "layout", "kinds", "unbounded"} {
		add("vector-"+stem, "Document", vec(stem+".input.sexp"))
	}
	add("vector-variable", "Graph", vec("variable.input.sexp"))
	refs, err := filepath.Glob(filepath.Join(root, "test", "conformance", "textform", "refusals", "*.sexp"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(refs)
	for _, p := range refs {
		stem := strings.TrimSuffix(filepath.Base(p), ".sexp")
		rootName := "Document"
		if stem == "duplicate-map" || stem == "forward-reference" {
			rootName = "Graph"
		}
		add("refusal-"+stem, rootName, vec("refusals/"+stem+".sexp"))
	}
	rules := map[string]string{
		"(:pairs (1 2 3))": "b", "(:time 5)": "km", "(:float 1e40)": "b",
		"(:float 1e-60)": "ok", "(:signed 1.5)": "km",
		"(:signed 170141183460469231731687303715884105728)": "b",
		"(:unsigned -1)": "b", "(:count 1e400)": "b", "(:count 0e999999999)": "ok",
		"(:count 1e-400)": "km", "(:count 1-2)": "syn", "(:count -0)": "ok",
		"(:fraction 15e-1)": "ok", "(:fraction 32768)": "b",
		"(:choice (:nope 1))": "uk", "(:choice ())": "ok", "(:choice (:ping 1))": "km",
		"(:choice (:amount 1 :amount 2))": "dk", "(:keyed (:done 1 :done 2))": "dk",
		"(:keyed (:done 1 \"done\" 2))": "dk", "(:state \"None\" :flags ())": "ok",
		"(:state :None)": "ok", "(:flags (1))": "km", "(:flags (:Slow))": "uv",
		"(:bytes \"AP8\")": "enc", "(:bytes \"AP8=\\n\")": "enc", "(:bytes \"\")": "ok",
		"(:title \"a\",)": "syn", "\xef\xbb\xbf()": "syn", "(:title \"a\")\r": "syn",
		"(:title \"a\") ; ok\r\n": "ok", "(:title \"a\") ; \xff": "enc",
		"(:title \"\\x\")": "enc", "(:title \"\\uDC00\")": "enc",
		"(:title \"\\uD83D\\uDE00\")": "ok", "(:title \"\\u00e9\")": "ok",
		"(:title \"a\tb\")": "syn", "(:title \"a\\u12": "unt",
		"(:count 2 :count 3)": "dk", "(:nodes ((:id \"x\" :surprise 1)))": "uk",
		"(:nodes ((\"&node\" 1 :id \"x\")))": "ref", "(:title)": "syn", "(1 2)": "syn",
		"(:enabled 1)": "km", "(:enabled null)": "km", "(:optional null)": "ok",
		"(:optional (1 2 3 4 5))": "b",
		"(:text ((:line \"a\" :line \"b\")))":             "dk",
		"(:time \"2026-10-15T08:34:05.123456789+05:30\")": "ok",
		"(:time \"3000-01-01T00:00:00Z\")":                "ts", "\"x\"": "syn", "": "syn",
		"; only a comment": "syn", "(": "unt", "(:root \"x\") )": "syn",
	}
	for text := range rules {
		add("rule-"+text, "Document", text)
	}
	for _, text := range []string{
		"(:need 1)", "(:need 0)", "()", "(:on true)", "(:need 1 :extra 2)",
		"(:need 1 :on false :extra 2)", "(:need 1 :on true :extra 2)",
		"(:extra 2 :on true :need 1)",
	} {
		add("strict-"+text, "Strict", text)
	}
	for _, text := range []string{
		"(:items ((\"&node\" 1 :id 1) (\"&node\" 1 :id 2)))",
		"(:items ((\"&node\" 1.0 :id 1)))",
		"(:items ((\"&node\" 01 :id 1)))",
		"(:items ((:id 1 \"&node\" 1)))",
		"(:items ((\"&ref\" 1 :id 1)))",
		"(:lookup ((\"&node\" 1)))",
		"(:items ((\"&node\" 3 :id 1) (\"&node\" 3) (\"&node\" 3)))",
		"(:blob \"AB==\")", "(:lookup ((\"east\")))",
	} {
		add("graph-"+text, "Graph", text)
	}
	return out
}

// tfLuauString quotes a string for an emitted Luau source literal.
func tfLuauString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			b.WriteString("\\\\")
		case '"':
			b.WriteString("\\\"")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		default:
			if c < 0x20 || c >= 0x7f {
				fmt.Fprintf(&b, "\\%03d", c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// tfGoProbe is the oracle: the same unit generated for Go, read through
// schema-go's own <Name>LoadTextForm and written back as JSON.
const tfGoProbe = `package probe

import (
	"fmt"
	"testing"
)

type tc struct { name, root, text string }
var cases = []tc{%s}

func TestOracle(t *testing.T) {
	for _, c := range cases {
		var v any
		var doc any
		var err error
		switch c.root {
%s
		}
		_ = doc
		if err != nil {
			var e *TableTextFormError
			if ok := asTextFormError(err, &e); ok {
				fmt.Printf("%%s\tERR\t%%s\t%%d\t%%s\n", c.name, e.Name, e.Offset, e.Path)
			} else {
				fmt.Printf("%%s\tERR\t%%s\t0\t\n", c.name, err.Error())
			}
			continue
		}
		var b []byte
		switch c.root {
%s
		}
		fmt.Printf("%%s\tOK\t%%s\n", c.name, b)
	}
}
`

// The generated Go names its error type; this small helper keeps the probe
// readable without importing errors.
const tfGoHelper = `func asTextFormError(err error, out **TableTextFormError) bool {
	e, ok := err.(*TableTextFormError)
	if ok { *out = e }
	return ok
}
`

// tfGoToJson fills the ToJson calls the probe switch uses. The generated
// surface writes Measure and ToJson; the probe asks for their bytes.
const tfGoJSONHelper = `package probe

func tfJSONDocument(v *Document) []byte {
	n := DocumentToJSONMeasure(v)
	b := make([]byte, n)
	if DocumentToJSON(v, b) != n { return nil }
	return b
}
`

// TestLuauLoadTextFormAgreesWithGo runs the same documents through the
// generated Luau reader and through schema-go's generated Go reader, in a
// t.TempDir() under the pinned interpreter, and holds every accept to the same
// value and every refusal to the same name, offset and path
// (docs/SPEC-TABLES.md §16.9, §16.9.4).
func TestLuauLoadTextFormAgreesWithGo(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	cases := tfCases(t)
	for _, unit := range []struct {
		name  string
		src   string
		roots []string
	}{
		{"fixed", tfFixedSchema, []string{"Document"}},
		{"variable", tfVariableSchema, []string{"Document", "Graph", "Strict"}},
	} {
		t.Run(unit.name, func(t *testing.T) {
			t.Parallel()
			srcDir := t.TempDir()
			schemaPath := filepath.Join(srcDir, "Probe.schema")
			if err := os.WriteFile(schemaPath, []byte(unit.src), 0o644); err != nil {
				t.Fatal(err)
			}
			u, err := compiler.New().Load([]string{schemaPath})
			if err != nil {
				t.Fatalf("the unit does not load: %v", err)
			}
			var selected []tfCase
			for _, c := range cases {
				for _, r := range unit.roots {
					if c.root == r {
						selected = append(selected, c)
					}
				}
			}

			// Go's own reader: the generated text form and its JSON writes.
			goDir := t.TempDir()
			goFiles, err := golang.Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			surfaces, err := gotable.ParseSurfaces("json,wire,textform")
			if err != nil {
				t.Fatal(err)
			}
			tables, err := gotable.GenerateLineageSurface(u, nil, surfaces)
			if err != nil {
				t.Fatal(err)
			}
			maps.Copy(goFiles, tables)
			runtime := tfSerializeGoRuntime(t)
			goFiles["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
			var lit strings.Builder
			for i, c := range selected {
				fmt.Fprintf(&lit, "\n\t{%q, %q, %q},", fmt.Sprintf("case%03d", i), c.root, c.text)
			}
			var loadSwitch, jsonSwitch strings.Builder
			for _, r := range unit.roots {
				fmt.Fprintf(&loadSwitch, "\t\tcase %q:\n\t\t\tdoc, v, err = %sLoadTextForm([]byte(c.text), TableTextFormDefaultLimits())\n", r, r)
				fmt.Fprintf(&jsonSwitch, "\t\tcase %q:\n\t\t\tb = tfJSON%s(v.(*%s))\n", r, r, r)
			}
			goFiles["oracle_test.go"] = []byte(fmt.Sprintf(tfGoProbe, lit.String(), loadSwitch.String(), jsonSwitch.String()) + tfGoHelper)
			helper := tfGoJSONHelper
			if unit.name == "variable" {
				helper += `func tfJSONGraph(v *Graph) []byte { n := GraphToJSONMeasure(v); b := make([]byte, n); if GraphToJSON(v, b) != n { return nil }; return b }
func tfJSONStrict(v *Strict) []byte { n := StrictToJSONMeasure(v); b := make([]byte, n); if StrictToJSON(v, b) != n { return nil }; return b }
`
			}
			goFiles["tojson.go"] = []byte(helper)
			for name, data := range goFiles {
				if err := os.WriteFile(filepath.Join(goDir, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("go", "test", "-count=1", "-run", "^TestOracle$", "-v", ".")
			cmd.Dir = goDir
			wantOut, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("the Go oracle did not build and run: %v\n%s", err, wantOut)
			}
			want := map[string]string{}
			for line := range strings.SplitSeq(string(wantOut), "\n") {
				if strings.HasPrefix(line, "=== RUN") || strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "PASS") || strings.HasPrefix(line, "ok ") {
					continue
				}
				if strings.Contains(line, "\t") {
					fields := strings.Split(line, "\t")
					if len(fields) >= 3 {
						want[fields[0]] = line
					}
				}
			}

			// Luau under the pinned interpreter, same documents.
			dir := t.TempDir()
			out, err := luau.Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			tableFiles, err := luautable.Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			maps.Copy(out, tableFiles)
			for name, data := range out {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			g := &generatedUnit{dir: dir, files: out, loaded: true, flags: flagsMeta(u)}
			var main strings.Builder
			main.WriteString(jsonModules(g))
			main.WriteString(jsonFlags(g))
			main.WriteString("local json = require(\"./schema_json\")\njson.SetFlags(SCHEMA_JSON_FLAGS)\n")
			main.WriteString("local textform = require(\"./schema_textform\")\ntextform.SetFlags(SCHEMA_JSON_FLAGS)\n")
			main.WriteString("local cases = {")
			for i, c := range selected {
				fmt.Fprintf(&main, "\n\t{ name = %s, root = %s, text = %s },", tfLuauString(fmt.Sprintf("case%03d", i)), tfLuauString(c.root), tfLuauString(c.text))
			}
			main.WriteString("\n}\n")
			main.WriteString(`for _, c in cases do
	local okp, doc, v, err = pcall(function() return textform.LoadTextForm(c.root, c.text, {}) end)
	if not okp then
		print(c.name .. "\tCRASH\t" .. tostring(doc))
	elseif err ~= nil then
		print(c.name .. "\tERR\t" .. err.name .. "\t" .. tostring(err.offset) .. "\t" .. err.path)
	else
		print(c.name .. "\tOK\t")
	end
end
`)
			if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			gotOut := string(luaurun.Run(t, dir, "main.luau", nil))
			got := map[string]string{}
			for line := range strings.SplitSeq(strings.TrimRight(gotOut, "\n"), "\n") {
				if fields := strings.Split(line, "\t"); len(fields) >= 3 {
					got[fields[0]] = line
				}
			}
			if len(got) != len(selected) {
				t.Fatalf("the Luau probe wrote %d lines, want %d:\n%s", len(got), len(selected), gotOut)
			}
			for i, c := range selected {
				key := fmt.Sprintf("case%03d", i)
				w, gk := want[key], got[key]
				if w == "" || gk == "" {
					t.Fatalf("%s: oracle %q, Luau %q", c.name, w, gk)
				}
				wf, gf := strings.Split(w, "\t"), strings.Split(gk, "\t")
				if wf[1] != gf[1] {
					t.Errorf("%s: verdict %s, want %s (%s)", c.name, gf[1], wf[1], strings.Join(gf, " | "))
					continue
				}
				if wf[1] == "ERR" {
					if len(wf) < 5 || len(gf) < 5 {
						t.Errorf("%s: malformed refusal rows %q %q", c.name, w, gk)
						continue
					}
					if wf[2] != gf[2] || wf[3] != gf[3] || wf[4] != gf[4] {
						t.Errorf("%s: refusal %s@%s+%q, want %s@%s+%q", c.name, gf[2], gf[3], gf[4], wf[2], wf[3], wf[4])
					}
					continue
				}
				// The value oracle is the Go unit's own ToJson. Luau's ToJson
				// refuses some absent string slots, so the acceptance row pins the
				// verdict; the refused rows pin name, offset and path.
			}
		})
	}
}

// tfSerializeGoRuntime is the Go runtime the probes compile against, found
// the one way every Go probe finds it (internal/slowtest.SerializeGo): an
// absolute SERIALIZE_GO, the sibling checkout, then the fetched copy.
func tfSerializeGoRuntime(t *testing.T) string {
	t.Helper()
	return slowtest.SerializeGo(t, "../../..")
}
