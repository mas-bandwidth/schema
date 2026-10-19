package squirreltable_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// TestSquirrelTextListTotalBytesBound holds docs/SPEC-TABLES.md §2.13's TOTAL,
// `| total = N` on a text list, to the Go oracle (TestGoTextListTotalBytesBound,
// internal/codegen/gotable): the writer refuses a sum past N on the wire and in
// the JSON text, and the reader keeps the longest prefix within N, counts
// clamped once and names the field. It drives one value at N-1, at N and at
// N+1 for each of the four spellings, then reads an over-total wire and text
// written by a table with the same field ids and no total.
func TestSquirrelTextListTotalBytesBound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "probe.schema")
	if err := os.WriteFile(schemaPath, []byte(textListTotalSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}

	type listCase struct {
		field string
		cls   string
		opt   bool
	}
	cases := []listCase{
		{"Names", "LNamesText", false},
		{"Maybe", "LMaybeText", true},
		{"Repos", "LReposText", false},
		{"Bases", "LBasesText", true},
	}
	under := map[string][]any{
		"Names": {"abcde", "fghij", "i"},
		"Maybe": {"abcdefgh", nil, "ij"},
		"Repos": {"abcdefgh", "ij"},
		"Bases": {nil, "abcdefgh", "ij"},
	}
	at := map[string][]any{
		"Names": {"abcde", "fghij", "ij"},
		"Maybe": {"abcdefgh", nil, "ijkl"},
		"Repos": {"abcdefgh", "ijkl"},
		"Bases": {nil, "abcdefgh", "ijkl"},
	}
	past := map[string][]any{
		"Names": {"abcde", "fghij", "ijk"},
		"Maybe": {"abcdefgh", nil, "ijklm"},
		"Repos": {"abcdefgh", "ijklm"},
		"Bases": {nil, "abcdefgh", "ijklm"},
	}
	over := map[string][]any{
		"Names": {"abcde", "fghij", "k", "lmno", "x"},
		"Maybe": {"abcdefgh", nil, "ijkl", "m"},
		"Repos": {"abcdefgh", "ijkl", "m"},
		"Bases": {nil, "abcdefgh", "ijkl", "", "m"},
	}
	want := map[string]string{}
	var script strings.Builder
	script.WriteString(textListTotalPrelude)
	for _, c := range cases {
		for _, phase := range []string{"under", "at", "past"} {
			specs := under[c.field]
			if phase == "at" {
				specs = at[c.field]
			} else if phase == "past" {
				specs = past[c.field]
			}
			key := c.field + "." + phase
			fmt.Fprintf(&script, "check(%q,%q,%q,%q,%v,%s);\n", key, "L", c.field, c.cls, c.opt, jsLiteral(specs))
			kept, sum := textBytes(specs)
			switch phase {
			case "under":
				want[key+".write"] = fmt.Sprintf("wire %d", sum)
				want[key+".load"] = fmt.Sprintf("%d %d 0", kept, sum)
				want[key+".jsonwrite"] = fmt.Sprintf("text %d", sum)
				want[key+".jsonread"] = fmt.Sprintf("%d %d 0", kept, sum)
			case "at":
				want[key+".write"] = fmt.Sprintf("wire %d", sum)
				want[key+".load"] = fmt.Sprintf("%d %d 0", kept, sum)
				want[key+".jsonwrite"] = fmt.Sprintf("text %d", sum)
				want[key+".jsonread"] = fmt.Sprintf("%d %d 0", kept, sum)
			case "past":
				want[key+".write"] = "null -1"
				want[key+".jsonwrite"] = "null -1"
			}
		}
		key := c.field + ".over"
		fmt.Fprintf(&script, "over(%q,%q,%q,%v,%s);\n", key, c.field, "Wide"+c.cls[1:], c.opt, jsLiteral(over[c.field]))
		want[key+".wireload"] = fmt.Sprintf("%d %d 1", textPrefixCount(over[c.field], 12), textPrefixSum(over[c.field], 12))
		want[key+".jsonread"] = fmt.Sprintf("%d %d 1", textPrefixCount(over[c.field], 12), textPrefixSum(over[c.field], 12))
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(script.String()))

	stdout := string(sqrun.Run(t, dir, "main.nut", nil))
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("probe line %q has no key", line)
		}
		got[k] = v
	}
	for key, w := range want {
		if got[key] != w {
			t.Errorf("%s = %q, want %q", key, got[key], w)
		}
	}
	t.Logf("compared %d text-list total rows (at, one under and one past) on the wire and in JSON", len(want))
}

// textBytes is the count and byte sum of a list of specs, absent elements
// adding nothing.
func textBytes(specs []any) (int, int) {
	n, sum := 0, 0
	for _, s := range specs {
		if s == nil {
			n++
			continue
		}
		n++
		sum += len(s.(string))
	}
	return n, sum
}

// textPrefixCount and textPrefixSum are the reader's prefix within total.
func textPrefixCount(specs []any, total int) int {
	n, sum := 0, 0
	for _, s := range specs {
		add := 0
		if s != nil {
			add = len(s.(string))
		}
		if sum+add > total {
			break
		}
		sum += add
		n++
	}
	return n
}

func textPrefixSum(specs []any, total int) int {
	n, sum := 0, 0
	for i, s := range specs {
		_ = i
		add := 0
		if s != nil {
			add = len(s.(string))
		}
		if sum+add > total {
			break
		}
		sum += add
		n++
	}
	return sum
}

func jsLiteral(specs []any) string {
	var b strings.Builder
	b.WriteString("[")
	for i, s := range specs {
		if i > 0 {
			b.WriteString(",")
		}
		if s == nil {
			b.WriteString("null")
			continue
		}
		q, _ := json.Marshal(s)
		b.Write(q)
	}
	b.WriteString("]")
	return b.String()
}

const textListTotalSchema = `package probe

table L
{
    names [..8]string(8) | total = 12
    maybe [..8]?string(8) | total = 12
    repos []string(8) | total = 12
    bases []?string(8) | total = 12
}

table Wide
{
    names [..8]string(8)
    maybe [..8]?string(8)
    repos []string(8)
    bases []?string(8)
}
`

const textListTotalPrelude = `local schema = dofile("out/schema_module.nut", true);
local T = schema.require("ProbeTables");
local S = schema.require("schema_table_save");
local L = schema.require("schema_table_load");
local J = schema.require("schema_table_json");
local report = function() { return { unknown=0, kindMismatch=0, widened=0, clamped=0, duplicate=0, malformed=false, refused=false }; };
local build = function(root, field, cls, opt, specs) {
  local v = T[root]();
  local arr = [];
  foreach (spec in specs) {
    local e = T[cls]();
    if (opt) { e.ValuePresent = spec != null; e.Value = spec == null ? "" : spec; }
    else { e.Value = spec; }
    arr.append(e);
  }
  v[field] = arr;
  return v;
};
local sumOf = function(arr) {
  local n = 0;
  foreach (e in arr) {
    if (!("ValuePresent" in e) || e.ValuePresent) n += e.Value.len();
  }
  return n;
};
local out = function(key, value) { print(key + "=" + value + "\n"); };
local check = function(key, root, field, cls, opt, specs) {
  local v = build(root, field, cls, opt, specs);
  local wire = S.save(T, root, v);
  local measure = S.measure(T, root, v);
  out(key + ".write", wire == null ? "null " + measure : "wire " + sumOf(v[field]));
  if (wire != null) {
    local r = report();
    local got = L.load(T, root, wire, r);
    out(key + ".load", got[field].len() + " " + sumOf(got[field]) + " " + r.clamped);
  }
  local text = J.ToJson(T, root, v);
  local jm = J.ToJsonMeasure(T, root, v);
  out(key + ".jsonwrite", text == null ? "null " + jm : "text " + sumOf(v[field]));
  local jr = report();
  local jv = J.FromJson(T, root, text == null ? "{}" : text, jr);
  out(key + ".jsonread", jv[field].len() + " " + sumOf(jv[field]) + " " + jr.clamped);
};
local over = function(key, field, cls, opt, specs) {
  local v = build("Wide", field, cls, opt, specs);
  local wire = S.save(T, "Wide", v);
  local r = report();
  local got = L.load(T, "L", wire, r);
  out(key + ".wireload", got[field].len() + " " + sumOf(got[field]) + " " + r.clamped);
  local text = J.ToJson(T, "Wide", v);
  local jr = report();
  local jv = J.FromJson(T, "L", text, jr);
  out(key + ".jsonread", jv[field].len() + " " + sumOf(jv[field]) + " " + jr.clamped);
};
`
