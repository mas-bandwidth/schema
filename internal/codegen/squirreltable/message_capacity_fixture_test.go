package squirreltable_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

// TestSquirrelMessageLoadCapacityFixture reads the committed capacity fixture,
// testdata/batch_capacity_two.bin (two LoginRequest bodies of tables/backend,
// client_build 40, written by the Go engine and pinned to it by
// test/conformance/harness TestBatchCapacityFixtureIsTheGoEnginesInEveryLeg),
// through the generated Squirrel's LoginRequestLoadMessages. Storage for one
// body whose ClientBuild is the sentinel 17 owes "A refused batch returns a
// count of `0`" (docs/SPEC-TABLES.md §3.3): 0, batch_too_large, and ClientBuild
// at its declared default after the refusal (ruling R4). Storage for two owes
// 2 and 40 in both. Storage for THREE, every row at the sentinel, then owes
// count 0 and all three rows at their declared default for the fixture with its
// form byte changed (a framing refusal) and for the fixture with its last byte
// dropped (a body or close refusal): row 2 is past the batch's count of 2, so a
// reset of only the rows the read reached leaves it at 17. A caller's capacity
// is a resource limit (§6.5), so this row runs here, in the leg's own tests,
// never in the shared suites.
func TestSquirrelMessageLoadCapacityFixture(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	back := emitUnit(t, dir, "backend", loadUnit(t, root, []string{"tables/backend"}))
	program := fmt.Sprintf(`local I=dofile("inputs.nut",true);
local B=dofile("backend/schema_module.nut",true).require(%q); local v={};local ar={};B.AnnounceRead(v,I["ann"],ar);
local def=B.LoginRequest().ClientBuild;
local one=[B.LoginRequest()];one[0].ClientBuild=17;local r1={};local n=B.LoginRequestLoadMessages(one,v,I["wire"],r1);
print("one "+n+" "+(("reason" in r1)?r1.reason:"-")+" "+(one[0].ClientBuild==def)+"\n");
local two=[B.LoginRequest(),B.LoginRequest()];local r2={};local m=B.LoginRequestLoadMessages(two,v,I["wire"],r2);
print("two "+m+" "+two[0].ClientBuild+" "+two[1].ClientBuild+"\n");
foreach(name in ["framing","body"]){
  local three=[B.LoginRequest(),B.LoginRequest(),B.LoginRequest()];
  foreach(row in three)row.ClientBuild=17;
  local r={};local k=B.LoginRequestLoadMessages(three,v,I[name],r);
  print(name+" "+k+" "+(three[0].ClientBuild==def)+" "+(three[1].ClientBuild==def)+" "+(three[2].ClientBuild==def)+"\n");
}`, back)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(program))
	wire := mustRead(t, filepath.Join("testdata", "batch_capacity_two.bin"))
	framing := append([]byte{3}, wire[1:]...)
	body := append([]byte(nil), wire[:len(wire)-1]...)
	inputs := map[string][]byte{
		"ann":     mustRead(t, filepath.Join(root, "testdata/wire/tables/backend_conn.bin")),
		"wire":    wire,
		"framing": framing,
		"body":    body,
	}
	got := strings.TrimSpace(string(sqrun.Run(t, dir, "main.nut", inputs)))
	if want := "one 0 batch_too_large true\ntwo 2 40 40\nframing 0 true true true\nbody 0 true true true"; got != want {
		t.Fatalf("capacity fixture:\n got  %q\n want %q", got, want)
	}
}
