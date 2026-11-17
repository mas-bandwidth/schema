package squirreltable_test

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// saveBoolSchema has one bool field, the shape whose write the control holds
// to docs/SPEC.md §5.1.
const saveBoolSchema = `package probe

table Root
{
    flag bool
}
`

// saveBoolPrelude saves and measures one bool value, saying what each
// answered, the measure and the saved bytes. A raised error is reported, never
// allowed to escape, because a refusal must answer null and not raise.
const saveBoolPrelude = `local schema = dofile("probe/schema_module.nut", true);
local T = schema.require("__MOD__");
local hex = function(b) {
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%02x", b[i] & 0xff);
  return s;
};
local probe = function(key, value) {
  local v = T.Root();
  v.Flag = value;
  local raised = false;
  local b = null;
  local m = null;
  try { b = T.RootSave(v); } catch (e) { raised = true; }
  try { m = T.RootMeasure(v); } catch (e) { raised = true; }
  local verdict = raised ? "raised" : (b == null ? "refused" : "wrote");
  local bytes = b == null ? "-" : hex(b);
  local measure = (b == null || m == null) ? "-1" : m.tostring();
  print(key + " " + verdict + " " + measure + " " + bytes + "\n");
};
probe("string", "x");
probe("integer", 1);
probe("true", true);
probe("false", false);
`

func runSaveBool(t *testing.T, dir string) map[string][]string {
	t.Helper()
	got := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", nil)), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) != 4 {
			t.Fatalf("probe line %q is not a case, a verdict, a measure and bytes", line)
		}
		got[f[0]] = f[1:]
	}
	return got
}

// TestSquirrelSaveRefusesNonBool holds the variable save to the write contract
// (docs/SPEC.md §5.1): a bool field refuses a value that is not a Squirrel
// bool, before any byte is written, with no VM error, while the lawful values
// write and measure. The control's second half removes the bool guard from a
// scratch copy of the save runtime: the same non-bool then writes 1 by
// truthiness, so the pin goes red.
func TestSquirrelSaveRefusesNonBool(t *testing.T) {
	t.Parallel()
	unit := unitFromSchema(t, saveBoolSchema)
	dir := t.TempDir()
	mod := emitUnit(t, dir, "probe", unit)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(strings.ReplaceAll(saveBoolPrelude, "__MOD__", mod)))

	got := runSaveBool(t, dir)
	for _, key := range []string{"string", "integer"} {
		row, ok := got[key]
		if !ok {
			t.Fatalf("%s: the probe printed no row", key)
		}
		if row[0] != "refused" || row[1] != "-1" || row[2] != "-" {
			t.Errorf("%s: Save/Measure answered %v, want refused with no bytes (docs/SPEC.md §5.1)", key, row)
		}
	}
	for _, key := range []string{"true", "false"} {
		row, ok := got[key]
		if !ok {
			t.Fatalf("%s: the probe printed no row", key)
		}
		if row[0] != "wrote" || row[2] == "-" {
			t.Errorf("%s: Save answered %v, want bytes", key, row)
		}
		if row[1] != strconv.Itoa(len(row[2])/2) {
			t.Errorf("%s: Measure %s, Save %d bytes", key, row[1], len(row[2])/2)
		}
	}
	lawful := got["true"][2]

	const anchor = "    if (typeof val != \"bool\") return refuseValue(w);\n"
	runtime := string(squirreltable.Runtime()["schema_table_save.nut"])
	if n := strings.Count(runtime, anchor); n != 1 {
		t.Fatalf("the control's anchor appears %d times in the save runtime, want exactly once", n)
	}
	writeFile(t, filepath.Join(dir, "probe", "schema_table_save.nut"), []byte(strings.Replace(runtime, anchor, "", 1)))

	patched := runSaveBool(t, dir)
	for _, key := range []string{"string", "integer"} {
		row, ok := patched[key]
		if !ok {
			t.Fatalf("%s: the probe printed no row without the guard", key)
		}
		if row[0] != "wrote" || row[2] != lawful {
			t.Errorf("with the bool guard removed, %s answers %v, want the true twin's bytes %s", key, row, lawful)
		}
		if row[1] != strconv.Itoa(len(lawful)/2) {
			t.Errorf("with the bool guard removed, %s measures %s, Save %d bytes", key, row[1], len(lawful)/2)
		}
	}
}
