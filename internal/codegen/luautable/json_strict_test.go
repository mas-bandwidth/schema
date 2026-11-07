package luautable_test

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE PLAIN NAME IS THE STRICT TEXT READ (docs/SPEC-TABLES.md §6.5), and THE
// TEXT READ COUNTS A MISSING REQUIRED FIELD AS THE BINARY READ DOES (§2.3):
// J.FromJson refuses with one named reason and no value every read that
// crosses a meaning boundary (malformed, missing_required, blob_shared,
// clamped, kind_mismatch), while unknown, widened and duplicate alone are
// counted and the read succeeds; J.FromJsonTolerant keeps the recovered value
// with the counters, and answers no value on a malformed text or a missing
// required field. Every answer is compared with schema-go's generated
// <T>FromJSON and <T>FromJSONTolerant over the same unit and the same text,
// never with a hand-typed expectation.
const jsonStrictSchema = `package probe
table Current
{
    active bool
    if active
    {
        req int32 | required
    }
}
table Exact
{
    ver uint8 | min = 7, max = 7, required
}
table Inner
{
    id uint8 | required
}
table Outer
{
    id uint8
    inner Inner
}
table Plain
{
    n uint8 | max = 10
    s string(4)
    f float32
}
`

// jsonStrictCase is one text read under one root.
type jsonStrictCase struct {
	name, root, text string
}

var jsonStrictCases = []jsonStrictCase{
	{"current-off", "Current", `{"active":false}`},
	{"current-on-missing", "Current", `{"active":true}`},
	{"current-on-present", "Current", `{"active":true,"req":3}`},
	{"exact-missing", "Exact", `{}`},
	{"exact-present", "Exact", `{"ver":7}`},
	{"exact-wrong-type", "Exact", `{"ver":"seven"}`},
	{"nested-missing", "Outer", `{"id":1,"inner":{}}`},
	{"nested-present", "Outer", `{"id":1,"inner":{"id":2}}`},
	{"plain-clean", "Plain", `{"n":3,"s":"ab","f":1.5}`},
	{"plain-clamped", "Plain", `{"n":11}`},
	{"plain-string-clamped", "Plain", `{"s":"abcdefg"}`},
	{"plain-kind", "Plain", `{"n":"x"}`},
	{"plain-unknown", "Plain", `{"zz":1}`},
	{"plain-duplicate", "Plain", `{"n":1,"n":2}`},
	{"plain-malformed", "Plain", `{"n":`},
}

// jsonStrictGoOracle runs the generated Go reads over every case and returns
// one "<strict>|<tolerant>" answer per case.
func jsonStrictGoOracle(t *testing.T, u *ir.Unit) map[string]string {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	var b strings.Builder
	b.WriteString(`package probe

import (
	"errors"
	"fmt"
	"testing"
)

func jsonStrictClass(ok bool, err error) string {
	if ok && err == nil {
		return "ok"
	}
	var m TableMissingRequired
	if errors.As(err, &m) {
		return "missing_required: " + m.Fields
	}
	for _, r := range []TableRefuseReason{TableRefuseMalformed, TableRefuseBlobShared, TableRefuseClamped, TableRefuseKindMismatch, TableRefuseNonFiniteFloat} {
		if errors.Is(err, r) {
			return r.Error()
		}
	}
	return fmt.Sprintf("other(%v)", err)
}

func jsonStrictTolerant(ok bool, r *TableReport) string {
	return fmt.Sprintf("%t,%d,%d,%d,%d,%d,%t", ok, r.Unknown, r.KindMismatch, r.Clamped, r.Duplicate, r.MissingRequired, r.Malformed)
}

func TestJSONStrictOracle(t *testing.T) {
`)
	for _, c := range jsonStrictCases {
		fmt.Fprintf(&b, "\t{ var v, w %sBuilder; if !v.Init() || !w.Init() { t.Fatal(\"builder\") }; var r, tr TableReport; ok := %sFromJSON(&v, []byte(%q), &r); tok := %sFromJSONTolerant(&w, []byte(%q), &tr); fmt.Printf(\"@%s|%%s|%%s\\n\", jsonStrictClass(ok, r.Err()), jsonStrictTolerant(tok, &tr)); v.Shutdown(); w.Shutdown() }\n",
			c.root, c.root, c.text, c.root, c.text, c.name)
	}
	b.WriteString("}\n")
	dir := t.TempDir()
	files["go.mod"] = fmt.Appendf(nil, "module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", tfSerializeGoRuntime(t))
	files["oracle_test.go"] = []byte(b.String())
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-run", "^TestJSONStrictOracle$", "-v", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go oracle failed: %v\n%s", err, out)
	}
	return jsonStrictAnswers(string(out))
}

// jsonStrictAnswers collects the "@<name>|<answer>" lines a probe printed.
func jsonStrictAnswers(out string) map[string]string {
	got := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if after, ok := strings.CutPrefix(strings.TrimSpace(line), "@"); ok {
			if name, answer, ok := strings.Cut(after, "|"); ok {
				got[name] = answer
			}
		}
	}
	return got
}

// jsonStrictLuau runs both Luau reads over every case under the pinned
// interpreter, with the emitted JSON runtime passed through patch (the
// identity for the gate, a sabotage for its control).
func jsonStrictLuau(t *testing.T, u *ir.Unit, patch func(string) string) map[string]string {
	t.Helper()
	g := requiredFiles(t, u)
	src := string(g.files["schema_json.luau"])
	if src == "" {
		t.Fatal("the unit emitted no schema_json.luau")
	}
	if err := os.WriteFile(filepath.Join(g.dir, "schema_json.luau"), []byte(patch(src)), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	var b strings.Builder
	b.WriteString(jsonModules(g))
	b.WriteString(`local json = require("./schema_json")
local inputs = require("./inputs")
local function tolerant(ok, r)
	return string.format("%s,%d,%d,%d,%d,%d,%s", tostring(ok), r.unknown, r.kindMismatch, r.clamped,
		r.duplicate, r.missingRequired or 0, tostring(r.malformed))
end
local function fresh()
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local cases = {
`)
	for _, c := range jsonStrictCases {
		inputs[c.name] = []byte(c.text)
		fmt.Fprintf(&b, "\t{ %q, %q },\n", c.name, c.root)
	}
	b.WriteString(`}
for _, c in cases do
	local name, root = c[1], c[2]
	local r = fresh()
	local v = json.FromJson(root, inputs[name], r)
	local strict = "ok"
	if v == nil then strict = r.reason or "nil reason" end
	local tr = fresh()
	local w = json.FromJsonTolerant(root, inputs[name], tr)
	print("@" .. name .. "|" .. strict .. "|" .. tolerant(w ~= nil, tr))
end
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return jsonStrictAnswers(string(luaurun.Run(t, g.dir, "main.luau", inputs)))
}

// jsonStrictMismatches names every case whose Luau answer is not Go's.
func jsonStrictMismatches(t *testing.T, want, got map[string]string) []string {
	t.Helper()
	var bad []string
	for _, c := range jsonStrictCases {
		if want[c.name] == "" {
			t.Fatalf("%s: the Go oracle printed no answer", c.name)
		}
		if got[c.name] != want[c.name] {
			bad = append(bad, c.name)
			t.Logf("%s: Luau %q, Go %q", c.name, got[c.name], want[c.name])
		}
	}
	sort.Strings(bad)
	return bad
}

func jsonStrictUnit(t *testing.T) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Req.schema")
	if err := os.WriteFile(path, []byte(jsonStrictSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the strict-read schema does not load: %v", err)
	}
	return u
}

// TestLuauJsonPlainReadIsStrict holds both reads to schema-go's on every case.
func TestLuauJsonPlainReadIsStrict(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and the Go toolchain")
	u := jsonStrictUnit(t)
	want := jsonStrictGoOracle(t, u)
	got := jsonStrictLuau(t, u, func(s string) string { return s })
	if bad := jsonStrictMismatches(t, want, got); len(bad) > 0 {
		t.Fatalf("the Luau text reads disagree with schema-go on %v", bad)
	}
	refused := 0
	for _, c := range jsonStrictCases {
		if !strings.HasPrefix(want[c.name], "ok|") {
			refused++
		}
	}
	if refused == 0 || refused == len(jsonStrictCases) {
		t.Fatalf("the cases do not split strict refusals from clean reads (%d of %d refused)", refused, len(jsonStrictCases))
	}
	t.Logf("%d cases agree with schema-go, %d of them refused by the plain name", len(jsonStrictCases), refused)
}

// TestLuauJsonStrictNegativeControl plants each rule's sabotage in a scratch
// copy of the emitted runtime and requires the comparison to go red on
// exactly that rule's cases: the plain name made tolerant reds the cases only
// the strict read refuses, and the required-field close removed reds the
// cases that leave a demanded field out.
func TestLuauJsonStrictNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and the Go toolchain")
	u := jsonStrictUnit(t)
	want := jsonStrictGoOracle(t, u)
	plants := []struct {
		name, anchor, planted string
		reds                  []string
	}{
		{
			"plain name tolerant",
			"  for _, k in STRICT do\n",
			"  for _, k in ({} :: { { string } }) do\n",
			[]string{"exact-wrong-type", "plain-clamped", "plain-kind", "plain-string-clamped"},
		},
		{
			"required close removed",
			"  if missing == nil then return true end\n",
			"  if missing == nil or true then return true end\n",
			[]string{"current-on-missing", "exact-missing", "nested-missing"},
		},
	}
	for _, p := range plants {
		got := jsonStrictLuau(t, u, func(s string) string {
			if strings.Count(s, p.anchor) != 1 {
				t.Fatalf("%s: the anchor %q is not in the runtime exactly once", p.name, p.anchor)
			}
			return strings.Replace(s, p.anchor, p.planted, 1)
		})
		bad := jsonStrictMismatches(t, want, got)
		if !slices.Equal(bad, p.reds) {
			t.Fatalf("%s: the comparison went red on %v, want exactly %v", p.name, bad, p.reds)
		}
		t.Logf("%s: red on exactly %v", p.name, bad)
	}
}
