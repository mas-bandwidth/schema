package squirreltable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// ttwSchema declares a total on a plain and an optional text list
// (docs/SPEC-TABLES.md §2.13).
const ttwSchema = `package texttotal

table Root
{
    labels [..4]string(8) | total = 8
    maybe []?string(8) | total = 4
}
`

// ttwProbe builds values at and past each total and answers what Save,
// Measure, ToJson and ToJsonMeasure do with each. An absent `?string` element
// counts zero toward the total.
const ttwProbe = `local schema = dofile("ttw/schema_module.nut", true);
local T = schema.require("MOD");
local text = function(table, s) {
  local e = T.Descriptors[table].make();
  e.Value = s;
  return e;
};
local absent = function() {
  local e = T.Descriptors["RootMaybeText"].make();
  e.ValuePresent = false;
  return e;
};
local present = function(s) {
  local e = text("RootMaybeText", s);
  e.ValuePresent = true;
  return e;
};
local verdict = function(name, v) {
  local saved = T.RootSave(v);
  local json = T.RootToJson(v);
  print(name + " save=" + (saved == null ? "refused" : "ok") +
    " measure=" + (T.RootMeasure(v) < 0 ? "refused" : "ok") +
    " json=" + (json == null ? "refused" : "ok") +
    " jsonmeasure=" + (T.RootToJsonMeasure(v) < 0 ? "refused" : "ok") + "\n");
};
local v = T.Descriptors["Root"].make();
v.Labels = [text("RootLabelsText", "abcd"), text("RootLabelsText", "efgh")];
verdict("labels_at", v);
v.Labels = [text("RootLabelsText", "abcd"), text("RootLabelsText", "efghi")];
verdict("labels_past", v);
v = T.Descriptors["Root"].make();
v.Maybe = [present("ab"), absent(), present("cd")];
verdict("maybe_at", v);
v.Maybe = [present("ab"), absent(), present("cde")];
verdict("maybe_past", v);
`

// ttwWant is the Go writer's answer on each row (docs/SPEC-TABLES.md §2.13):
// a value whose texts sum past the total is refused by every writer.
var ttwWant = map[string]string{
	"labels_at":   "save=ok measure=ok json=ok jsonmeasure=ok",
	"labels_past": "save=refused measure=refused json=refused jsonmeasure=refused",
	"maybe_at":    "save=ok measure=ok json=ok jsonmeasure=ok",
	"maybe_past":  "save=refused measure=refused json=refused jsonmeasure=refused",
}

func ttwRun(t *testing.T, patch func(map[string][]byte)) map[string]string {
	t.Helper()
	sqrun.Require(t)
	u, _, _ := nfLoadNamed(t, ttwSchema)
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	if patch != nil {
		patch(tables)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "ttw")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(strings.Replace(ttwProbe, "MOD", squirreltable.ModuleName(u), 1)))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", nil)), "\n"), "\n") {
		key, rest, _ := strings.Cut(line, " ")
		got[key] = rest
	}
	return got
}

// TestSquirrelTextTotalRefusesTheWrite holds Save, Measure, ToJson and
// ToJsonMeasure to the total (docs/SPEC-TABLES.md §2.13): a value at it is
// written, and one past it is refused by all four.
func TestSquirrelTextTotalRefusesTheWrite(t *testing.T) {
	t.Parallel()
	got := ttwRun(t, nil)
	for key, want := range ttwWant {
		if got[key] != want {
			t.Errorf("%s: %q, want %q", key, got[key], want)
		}
	}
}

// TestSquirrelTextTotalRefusesTheWriteNegativeControl removes both writers'
// total checks: every past row must go red and every row at the total stay
// green.
func TestSquirrelTextTotalRefusesTheWriteNegativeControl(t *testing.T) {
	t.Parallel()
	patch := func(tables map[string][]byte) {
		fpPatch(t, "schema_table_save.nut", [][2]string{
			{`if (("total" in d) && textTotalPast(d, val)) return false;`, ``},
		})(tables)
		fpPatch(t, "schema_table_json.nut", [][2]string{
			{`if (sum > d.total) return false;`, ``},
		})(tables)
	}
	got := ttwRun(t, patch)
	for key, want := range ttwWant {
		past := strings.HasSuffix(key, "_past")
		switch {
		case past && got[key] == want:
			t.Errorf("%s stayed green under the control", key)
		case !past && got[key] != want:
			t.Errorf("%s went red under the control: %q", key, got[key])
		}
	}
}
