package squirreltable_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// TestSquirrelAnnounceReadsFormBeforeLength pins the announcement reader to the
// Go engine's order: the form byte is read before the length
// (docs/SPEC-TABLES.md §3.3). A form byte this reader does not carry is
// newer_form even when the buffer is too short to hold the rest of an
// announcement; an empty buffer stays malformed.
func TestSquirrelAnnounceReadsFormBeforeLength(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, root, []string{"tables/examples/Pack.schema"})
	dir := t.TempDir()
	for name, b := range squirrelMessageFiles(t, u, nil) {
		if !strings.Contains(name, "/") {
			writeFile(t, filepath.Join(dir, name), b)
		}
	}
	module := squirreltable.ModuleName(u)
	script := fmt.Sprintf(`local I=dofile("inputs.nut",true);local T=dofile("schema_module.nut",true).require(%q);
local V={};local ar={};T.AnnounceRead(V,I["empty"],ar);if(ar.malformed)print("empty:ok\n");else print("empty:fail:"+ar.reason+"\n");
local V2={};local ar2={};T.AnnounceRead(V2,I["ff"],ar2);if(ar2.refused&&ar2.reason=="newer_form")print("ff:ok\n");else print("ff:fail:"+ar2.reason+":"+ar2.malformed+"\n");
local V3={};local ar3={};T.AnnounceRead(V3,I["ff00"],ar3);if(ar3.refused&&ar3.reason=="newer_form")print("ff00:ok\n");else print("ff00:fail:"+ar3.reason+":"+ar3.malformed+"\n");
local V4={};local ar4={};T.AnnounceRead(V4,I["two"],ar4);if(ar4.refused&&ar4.reason=="message_form_as_file")print("two:ok\n");else print("two:fail:"+ar4.reason+":"+ar4.malformed+"\n");
`, module)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(script))
	out := string(sqrun.Run(t, dir, "main.nut", map[string][]byte{
		"ff":    {0xff},
		"ff00":  {0xff, 0x00},
		"two":   {0x02},
		"empty": {},
	}))
	for _, want := range []string{"empty:ok", "ff:ok", "ff00:ok", "two:ok"} {
		if !strings.Contains(out, want) {
			t.Errorf("%s missing from probe: %s", want, out)
		}
	}
}
