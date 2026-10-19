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
local fresh = function() {
  return { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0,
    malformed = false, refused = false, reason = null };
};
local r5 = fresh();
local j5 = J.FromJson(T, "Var", "{\"n\":100,\"req\":4}", r5);
print("jsonClamp." + (j5 == null ? "null" : "val") + "." + (r5.reason == null ? "none" : r5.reason) + "\n");
local r6 = fresh();
local j6 = J.FromJsonTolerant(T, "Var", "{\"n\":100,\"req\":4}", r6);
print("jsonClampTol." + (j6 == null ? "null" : "val") + "." + (r6.clamped == 1 ? "clamped" : "none") + "\n");
local r7 = fresh();
local j7 = J.FromJson(T, "Var", "{\"n\":\"x\",\"req\":4}", r7);
print("jsonKind." + (j7 == null ? "null" : "val") + "." + (r7.reason == null ? "none" : r7.reason) + "\n");
local r8 = fresh();
local j8 = J.FromJsonTolerant(T, "Var", "{\"n\":\"x\",\"req\":4}", r8);
print("jsonKindTol." + (j8 == null ? "null" : "val") + "." + (r8.kindMismatch == 1 ? "kind_mismatch" : "none") + "\n");
local r9 = fresh();
local j9 = J.FromJson(T, "Var", "{\"n\":3}", r9);
print("jsonMissing." + (j9 == null ? "null" : "val") + "." + (r9.reason == null ? "none" : r9.reason) + "\n");
local r10 = fresh();
local j10 = J.FromJsonTolerant(T, "Var", "{\"n\":3}", r10);
print("jsonMissingTol." + (j10 == null ? "null" : "val") + "." + (r10.malformed ? "malformed" : "ok") + "\n");
local fb = blob(256);
local fn = T.FixFixedSave(fb, [T.Fix()]);
local ftr = blob(fn - 1);
for (local i = 0; i < ftr.len(); i++) ftr[i] = fb[i];
local f1 = T.FixFixedLoad(ftr, [T.Fix()], null);
print("fixTrunc." + (f1 < 0 ? "neg" : "val") + "." + T.FixedReason + "\n");
local f2 = T.FixFixedLoadTolerant(ftr, [T.Fix()], null);
print("fixTol." + (f2 < 0 ? "neg" : "val") + "." + T.FixedReason + "\n");
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
		"jsonClamp.null.clamped",
		"jsonClampTol.val.clamped",
		"jsonKind.null.kind_mismatch",
		"jsonKindTol.val.kind_mismatch",
		"jsonMissing.null.missing_required: req",
		"jsonMissingTol.val.malformed",
		"fixTrunc.neg.layout_malformed",
		"fixTol.neg.layout_malformed",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing line %q in probe output:\n%s", want, got)
		}
	}
}
