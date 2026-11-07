package luautable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/jsontestsuite"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE OUTSIDE ORACLE (docs/SPEC-TABLES.md §16.12): the emitted Luau walk's
// JSON LAYER — a leading UTF-8 byte-order mark if there is one, one whole
// value under RFC 8259's grammar and the dialect, whitespace, then the end of
// the input, under the depth, key and number caps — over every vendored
// JSONTestSuite file, against the standard's table, with PENDING's files
// allowed to disagree and listed, exactly as TestJSONTestSuiteGenerated holds
// the emitted Go walk. The layer is reached in-package, as Go's probe reaches
// tableJSONIn: a scratch copy of the emitted schema_json.luau exports one
// function over the walk's own locals. The mark's entry is pinned beside it
// through the plain FromJson on a fixed and a variable root.

const luauJSONTestSuiteSchema = "package probe\nfixed table Root { n int32 }\ntable Doc { n int32 }\n"

// luauJSONTestSuiteExport is appended to the scratch runtime ahead of its
// final `return J`: the layer over the walk's own locals.
const luauJSONTestSuiteExport = `
J.JSONTestSuiteAccepts = function(text: string): boolean
  local inp = new_in(text)
  byte_order_mark(inp)
  local ok = skip_value(inp, 0, {})
  space(inp)
  return ok and not inp.bad and inp.p == #inp.t + 1
end

return J
`

// luauJSONTestSuiteRun loads the scratch runtime with the given export and
// returns the layer's verdict per corpus file, and the probe's output.
func luauJSONTestSuiteRun(t *testing.T, s *jsontestsuite.Suite, patch func(string) string) (map[string]bool, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(path, []byte(luauJSONTestSuiteSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the probe schema does not load: %v", err)
	}
	g := requiredFiles(t, u)
	src := string(g.files["schema_json.luau"])
	const tail = "\nreturn J\n"
	if !strings.HasSuffix(src, tail) {
		t.Fatalf("the emitted schema_json.luau does not end in %q", tail)
	}
	src = strings.TrimSuffix(src, tail) + luauJSONTestSuiteExport
	if patch != nil {
		src = patch(src)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "schema_json.luau"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	var b strings.Builder
	b.WriteString(jsonModules(g))
	b.WriteString(`local json: any = require("./schema_json")
local inputs = require("./inputs")
local names = {
`)
	for i, name := range s.Names() {
		key := fmt.Sprintf("f%03d", i)
		inputs[key] = s.Files[name]
		fmt.Fprintf(&b, "\t{ %q, %q },\n", key, name)
	}
	b.WriteString(`}
for _, n in names do
	local text = buffer.tostring(inputs[n[1]])
	print("JTS\t" .. n[2] .. "\t" .. (json.JSONTestSuiteAccepts(text) and "accept" or "refuse"))
end
-- the entry: FromJson skips a leading mark and lands the same value, and a
-- mark anywhere else, a broken mark, two marks or a UTF-16 mark are refused
local function read(root: string, text: string): (any, any)
	local r = {}
	return json.FromJson(root, buffer.fromstring(text), r), r
end
for _, root in { "Root", "Doc" } do
	local plain = read(root, "{\"n\":7}")
	local marked, mr = read(root, "\xEF\xBB\xBF{\"n\":7}")
	print("MARK\t" .. root .. "\t" .. tostring(plain ~= nil and marked ~= nil and marked.N == 7 and plain.N == 7 and mr.unknown == 0))
	for i, text in { " \xEF\xBB\xBF{}", "{}\xEF\xBB\xBF", "\xEF\xBB{}", "\xEF\xBB\xBF\xEF\xBB\xBF{}", "\xFF\xFE{}" } do
		local v, r = read(root, text)
		print("BADMARK\t" .. root .. "\t" .. i .. "\t" .. tostring(v == nil and r.malformed == true))
	end
end
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, g.dir, "main.luau", inputs))
	got := map[string]bool{}
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Split(line, "\t")
		switch {
		case len(f) == 3 && f[0] == "JTS":
			if _, dup := got[f[1]]; dup {
				t.Fatalf("%s: two verdicts from the probe", f[1])
			}
			got[f[1]] = f[2] == "accept"
		case len(f) == 3 && f[0] == "MARK" && f[2] != "true":
			t.Errorf("%s: a leading byte-order mark did not read as the text after it", f[1])
		case len(f) == 4 && f[0] == "BADMARK" && f[3] != "true":
			t.Errorf("%s: misplaced mark %s read clean, want refused and Malformed", f[1], f[2])
		}
	}
	if len(got) != len(s.Files) {
		t.Fatalf("the probe gave %d verdicts for %d files\n%s", len(got), len(s.Files), out)
	}
	return got, out
}

// TestLuauJSONTestSuite is the gate.
func TestLuauJSONTestSuite(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	s, err := jsontestsuite.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := luauJSONTestSuiteRun(t, s, nil)
	mismatches, pending := jsontestsuite.Check(s, got)
	for _, p := range pending {
		t.Logf("pending: %s", p)
	}
	for _, m := range mismatches {
		t.Errorf("mismatch: %s", m)
	}
	t.Logf("%d JSONTestSuite files through the emitted Luau layer, %d pending", len(got), len(pending))
}

// TestLuauJSONTestSuiteNegativeControl lifts the walk's depth cap in the
// scratch runtime, so the one file past it that the standard refuses
// (i_structure_500_nested_arrays.json) reads clean: the gate must name exactly
// that file.
func TestLuauJSONTestSuiteNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	s, err := jsontestsuite.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "local MAX_DEPTH = 128\n"
	got, _ := luauJSONTestSuiteRun(t, s, func(src string) string {
		if strings.Count(src, anchor) != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the anchor %q is not in the runtime exactly once", anchor)
		}
		return strings.Replace(src, anchor, "local MAX_DEPTH = 1024\n", 1)
	})
	mismatches, _ := jsontestsuite.Check(s, got)
	var named []string
	for _, m := range mismatches {
		name, _, _ := strings.Cut(m, ":")
		named = append(named, name)
	}
	want := []string{"i_structure_500_nested_arrays.json"}
	if !slices.Equal(named, want) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the gate named %q, want exactly %q (%q)", named, want, mismatches)
	}
	t.Logf("the lifted depth cap is named: %q", mismatches)
}
