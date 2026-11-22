package squirreltable_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

// TestSquirrelKindHeldEscapeRefuses (R17: the newer reader reads old data,
// never the other way): kind 31 is held by name and never written, so a reader
// meets it only as damage, exactly as 33 and 34. The shared input carries a
// kind 31 field the reader cannot name; the walk, the strict read and the
// retaining load all refuse the whole load. The old reader skipped it by its L
// and read the file.
func TestSquirrelKindHeldEscapeRefuses(t *testing.T) {
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mod := emitUnit(t, dir, "evo", loadUnit(t, root, []string{"test/tables/varevo/VarEvo1.schema"}))
	program := fmt.Sprintf(`local I=dofile("inputs.nut",true);
local S=dofile("evo/schema_module.nut",true); local T=S.require(%q);
local L=S.require("schema_table_load"); local R=S.require("schema_table_refusal"); local RT=S.require("schema_table_retain");
local fresh=function(){return {unknown=0,mismatch=0,widened=0,oor=0,repeated=0,bodyDamaged=false,refused=false};};
local w=I["wire"];
local a=L.loadWalk(T,"EvoDoc",w,fresh());
local b=R.ReadStrict(T,"EvoDoc",w,fresh());
local c=RT.LoadRetain(T,"EvoDoc",w,{capacity=1048576,idCapacity=1024},fresh());
print("walk="+(a==null)+" strict="+(b==null)+" retain="+(c==null)+"\n");`, mod)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(program))
	wire := mustRead(t, filepath.Join(root, "testdata/wire/tables/framing/varevo_kind31_escape.bin"))
	got := strings.TrimSpace(string(sqrun.Run(t, dir, "main.nut", map[string][]byte{"wire": wire})))
	const want = "walk=true strict=true retain=true"
	if got != want {
		t.Errorf("kind 31 at an unnamed field: got %q, want every load refused (%q)", got, want)
	}
}
