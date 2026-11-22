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
// count of `0`" and "refuses before it decodes anything" (docs/SPEC-TABLES.md
// §3.3): 0, batch_too_large, the sentinel untouched. Storage for two owes 2
// and 40 in both. A caller's capacity is a resource limit (§6.5), so this row
// runs here, in the leg's own tests, never in the shared suites.
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
local one=[B.LoginRequest()];one[0].ClientBuild=17;local r1={};local n=B.LoginRequestLoadMessages(one,v,I["wire"],r1);
print("one "+n+" "+(("reason" in r1)?r1.reason:"-")+" "+one[0].ClientBuild+"\n");
local two=[B.LoginRequest(),B.LoginRequest()];local r2={};local m=B.LoginRequestLoadMessages(two,v,I["wire"],r2);
print("two "+m+" "+two[0].ClientBuild+" "+two[1].ClientBuild+"\n");`, back)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(program))
	inputs := map[string][]byte{
		"ann":  mustRead(t, filepath.Join(root, "testdata/wire/tables/backend_conn.bin")),
		"wire": mustRead(t, filepath.Join("testdata", "batch_capacity_two.bin")),
	}
	got := strings.TrimSpace(string(sqrun.Run(t, dir, "main.nut", inputs)))
	if want := "one 0 batch_too_large 17\ntwo 2 40 40"; got != want {
		t.Fatalf("capacity fixture:\n got  %q\n want %q", got, want)
	}
}
