package squirreltable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

// THE SLOTS PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED (docs/SPEC-TABLES.md
// §3.4; docs/SPEC-CONFORMANCE.md, "Counted-array slots after a lawful fixed
// load"), the recipe in test/tables/FixedSlack.schema's header. A Squirrel
// counted array is an array of exactly `count` elements, so the rule here is
// that after loading A and then B into the SAME caller values every array
// holds exactly B's count and no element left over from A, the same as a
// fresh load of B. The leg holds the rule already: the load walk decodes each
// record into a fresh value and hands its arrays to the caller's value, and
// <T>FixedDecodeBody sizes each array to its count (fixedform.go); this test
// guards it.
func TestSquirrelFixedSlackPastCountHoldsDefaults(t *testing.T) {
	slowtest.Gate(t, "the pinned Squirrel interpreter (it runs the generated fixed load over FixedSlack.schema)")
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeGenerated(t, dir, fixedLoadUnit(t, root, "test/tables/FixedSlack.schema"))
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(squirrelSlackProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(sqrun.Run(t, dir, "main.nut", nil)))
	want := []string{
		"cells 1 nums 1",
		"cell 0 77 subs 1 77",
		"num 77",
	}
	for _, tag := range []string{"fresh", "reused"} {
		if got := squirrelSlackLines(out, tag); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s: after loading B the value holds\n%s\nwant exactly B's live elements\n%s\nprobe output:\n%s",
				tag, strings.Join(got, "\n"), strings.Join(want, "\n"), out)
		}
	}
	if !strings.Contains(out, "refused -1") {
		t.Fatalf("the truncated file read:\n%s", out)
	}
	newborn := squirrelSlackLines(out, "newborn")
	if strings.Join(newborn, "\n") != "cells 0 nums 0\nnum" {
		t.Fatalf("a fresh SlackRoot dumps %q", newborn)
	}
	if got := squirrelSlackLines(out, "refused"); strings.Join(got, "\n") != strings.Join(newborn, "\n") {
		t.Errorf("a refused load left %q, want a fresh value %q\nprobe output:\n%s", got, newborn, out)
	}
}

func squirrelSlackLines(out, tag string) []string {
	var got []string
	for line := range strings.SplitSeq(out, "\n") {
		if rest, ok := strings.CutPrefix(line, tag+" "); ok && rest != "-1" {
			got = append(got, rest)
		}
	}
	return got
}

const squirrelSlackProbe = `local S = dofile("schema_module.nut", true);
local T = S.require("TblslackTables");
local function cell(n) {
  local c = T.SlackCell(); c.Id = 77;
  for (local j = 0; j < n; j++) { local s = T.SlackSub(); s.V = 77; c.Subs.append(s); }
  return c;
}
local function dump(tag, r) {
  print(tag + " cells " + r.Cells.len() + " nums " + r.Nums.len() + "\n");
  for (local i = 0; i < r.Cells.len(); i++) {
    local c = r.Cells[i];
    local s = tag + " cell " + i + " " + c.Id + " subs " + c.Subs.len();
    foreach (x in c.Subs) s += " " + x.V;
    print(s + "\n");
  }
  local n = tag + " num";
  foreach (x in r.Nums) n += " " + x;
  print(n + "\n");
}
local a = T.SlackRoot();
for (local i = 0; i < 3; i++) a.Cells.append(cell(2));
a.Nums = [77, 77, 77, 77];
local b = T.SlackRoot();
b.Cells.append(cell(1));
b.Nums = [77];
local A = blob(T.SlackRootFixedMeasure(1)); T.SlackRootFixedSave(A, [a]);
local B = blob(T.SlackRootFixedMeasure(1)); T.SlackRootFixedSave(B, [b]);
local fresh = [T.SlackRoot()];
T.SlackRootFixedLoad(B, fresh, null);
dump("fresh", fresh[0]);
local v = [T.SlackRoot()];
T.SlackRootFixedLoad(A, v, null);
T.SlackRootFixedLoad(B, v, null);
dump("reused", v[0]);
T.SlackRootFixedLoad(A, v, null);
local short = blob(B.len() - 1);
for (local i = 0; i < short.len(); i++) short[i] = B[i];
print("refused " + T.SlackRootFixedLoad(short, v, null) + "\n");
dump("refused", v[0]);
dump("newborn", T.SlackRoot());
`
