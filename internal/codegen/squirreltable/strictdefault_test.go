package squirreltable_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

// strictDefaultPrelude is the strict-by-default probe (docs/SPEC-TABLES.md
// §6.5): the plain load and FromJson refuse every ReadStrict reason, and the
// Tolerant twins keep the recovered value. A truncated wire and a truncated
// text are forged here from clean ones by slicing off the last byte.
const strictDefaultPrelude = `local schema = dofile(%q + "/schema_module.nut", true);
local T = schema.require(%q);
local L = schema.require("schema_table_load");
local S = schema.require("schema_table_save");
local J = schema.require("schema_table_json");
local clean = { N = 3, Req = 4 };
local wire = S.save(T, "Var", clean);
if (wire == null) throw "save refused a clean Var";
local trun = blob(wire.len() - 1);
for (local i = 0; i < trun.len(); i++) trun[i] = wire[i];
local r1 = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0, malformed = false, refused = false, reason = null };
local v1 = L.load(T, "Var", trun, r1);
print("loadTrunc." + (v1 == null ? "null" : "val") + "." + (r1.reason == null ? "none" : r1.reason) + "\n");
local r2 = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0, malformed = false, refused = false, reason = null };
local v2 = L.loadTolerant(T, "Var", trun, r2);
print("loadTol." + (v2 == null ? "null" : "val") + "." + (r2.malformed ? "malformed" : "ok") + "\n");
local r3 = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0, malformed = false, refused = false, reason = null };
local j1 = J.FromJson(T, "Var", "{\"n\":3", r3);
print("jsonTrunc." + (j1 == null ? "null" : "val") + "." + (r3.reason == null ? "none" : r3.reason) + "." + (r3.malformed ? "m" : "c") + "\n");
local r4 = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0, malformed = false, refused = false, reason = null };
local j2 = J.FromJsonTolerant(T, "Var", "{\"n\":3", r4);
print("jsonTol." + (j2 == null ? "null" : "val") + "." + (r4.malformed ? "malformed" : "ok") + "\n");
`

// TestSquirrelDefaultReadIsStrict pins docs/SPEC-TABLES.md §6.5's rule for the
// Squirrel leg: the plain load and FromJson are the strict reads (a truncated
// wire and text refuse with a named reason, the value not recovered) while the
// Tolerant twins keep the recovered value.
func TestSquirrelDefaultReadIsStrict(t *testing.T) {
	t.Parallel()
	unit := loadSource(t, `package probe
fixed table Fix { n int32 = 1 | min = 0, max = 10
 m int32 = 2 }
table Var { n int32 = 1 | min = 0, max = 10
 req int32 | required }
`)
	dir := t.TempDir()
	mod := emitUnit(t, dir, "probe", unit)
	var p strings.Builder
	fmt.Fprintf(&p, strictDefaultPrelude, "probe", mod)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	got := string(sqrun.Run(t, dir, "main.nut", nil))
	for _, want := range []string{
		"loadTrunc.null.wire_damaged",
		"loadTol.null.malformed",
		"jsonTrunc.null.malformed",
		"jsonTol.val.malformed",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing line %q in probe output:\n%s", want, got)
		}
	}
}
