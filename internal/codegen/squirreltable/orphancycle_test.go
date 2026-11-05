package squirreltable_test

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const (
	sqOrphanCycleFrom = `if (_require("schema_table_save").cyclic(T, rootName, v, verts)) report.malformed = true;`
	sqOrphanCycleTo   = `if (_require("schema_table_save").numberForCook(T, rootName, v) == null) report.malformed = true;`
)

func runSquirrelOrphanCycleProbe(t *testing.T, wire []byte, plant bool) string {
	t.Helper()
	if _, err := sqrun.Interpreter(); err != nil {
		sqrun.Require(t)
	}
	repo, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, repo, []string{"tables/pointers"})
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tf, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tf)
	if plant {
		src := string(files["schema_table_load.nut"])
		if n := strings.Count(src, sqOrphanCycleFrom); n != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the pointer_orphan_cycle plant anchor appears %d times in schema_table_load.nut, want once", n)
		}
		files["schema_table_load.nut"] = []byte(strings.Replace(src, sqOrphanCycleFrom, sqOrphanCycleTo, 1))
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "graphdemo")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	main := fmt.Sprintf(`local inputs = dofile("inputs.nut", true);
local schema = dofile("graphdemo/schema_module.nut", true);
local T = schema.require(%q);
local r = {};
local v = T.SceneLoad(inputs["vec"], r);
local damaged = (v == null) && ("malformed" in r) && r.malformed;
print((v != null ? "loaded" : "null") + "|" + (damaged ? "damage" : "ok") + "\n");
`, squirreltable.ModuleName(u))
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(main))
	return strings.TrimSpace(string(sqrun.Run(struct{ testing.TB }{t}, dir, "main.nut", map[string][]byte{"vec": wire})))
}

// TestSquirrelPointerOrphanCycleIsDamage runs the shared vector
// pointer_orphan_cycle (testdata/wire/tables/fuzz-vectors/INDEX.txt) through
// the pinned Squirrel interpreter and checks that a cycle among records the
// root never reaches is refused as damage, that its acyclic twin holding the
// same two orphan records loads cleanly, and that a plant restoring the
// root-only walk turns the cyclic vector red.
func TestSquirrelPointerOrphanCycleIsDamage(t *testing.T) {
	t.Parallel()
	repo, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "fuzz-vectors", "pointer_orphan_cycle.bin"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, repo, []string{"tables/pointers"})
	m := tabletext.NewModel(u)
	var rep tabletext.Report
	ok, err := tablewire.Decode(m, m.New(m.Lookup("Scene")), wire, &rep)
	if err != nil {
		t.Fatal(err)
	}
	if ok || !rep.Malformed {
		t.Fatalf("the Go engine decoded pointer_orphan_cycle with ok=%t malformed=%t, want damage", ok, rep.Malformed)
	}
	if got := runSquirrelOrphanCycleProbe(t, wire, false); got != "null|damage" {
		t.Fatalf("graphdemo.Scene(pointer_orphan_cycle.bin): Squirrel SceneLoad answered %q, want \"null|damage\" (a cycle among records the root never reaches is damage)", got)
	}
	if got := runSquirrelOrphanCycleProbe(t, wire, true); got == "null|damage" {
		t.Fatalf("NEGATIVE CONTROL FAILED: restoring the root-only cycle walk left pointer_orphan_cycle answering %q", got)
	}

	twin := bytes.Replace(wire, []byte{0x04, 0x11, 0x02, 0x00}, []byte{0x04, 0x11, 0x03, 0x00}, 1)
	if bytes.Equal(twin, wire) {
		t.Fatal("pointer_orphan_cycle.bin did not contain expected ListNode 2 self-edge 04 11 02 00")
	}
	var twinRep tabletext.Report
	twinOK, err := tablewire.Decode(m, m.New(m.Lookup("Scene")), twin, &twinRep)
	if err != nil {
		t.Fatal(err)
	}
	if !twinOK || twinRep.Malformed {
		t.Fatalf("the Go engine decoded the acyclic orphan twin with ok=%t malformed=%t, want loaded", twinOK, twinRep.Malformed)
	}
	if got := runSquirrelOrphanCycleProbe(t, twin, false); got != "loaded|ok" {
		t.Fatalf("graphdemo.Scene(acyclic orphan twin): Squirrel SceneLoad answered %q, want \"loaded|ok\"", got)
	}
}
