// TestSquirrelCarriesUnionsInTableClosures proves the Squirrel target carries
// the union a table closure holds (docs/SPEC-TABLES.md §2.6, §3, §16.2): a
// table arm, a payload-free arm and an array of unions generate, run under the
// pinned interpreter, save and load back to the same value, and write the same
// bytes as internal/tablewire over the same unit (the Go oracle).
package compiler

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// squirrelUnionPrelude loads the Go oracle's bytes through the emitted load
// runtime, saves the value, loads the saved bytes and saves them again: the
// two hex lines are the save and the round trip's save (docs/SPEC-TABLES.md
// §3).
const squirrelUnionPrelude = `local inputs = dofile("inputs.nut", true);
local schema = dofile("schema_module.nut", true);
local T = schema.require("%s");
local L = schema.require("schema_table_load");
local S = schema.require("schema_table_save");
local report = {
  unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
  duplicate = 0, malformed = false, refused = false
};
local v = L.loadTolerant(T, "L", inputs["oracle"], report);
if (v == null) { print("load-failed\n"); return; }
local b = S.save(T, "L", v);
if (b == null) { print("save-failed\n"); return; }
local v2 = L.loadTolerant(T, "L", b, report);
if (v2 == null) { print("reload-failed\n"); return; }
local b2 = S.save(T, "L", v2);
if (b2 == null) { print("resave-failed\n"); return; }
::hexline(b);
::hexline(b2);
`

func TestSquirrelCarriesUnionsInTableClosures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src, json string
	}{
		{
			"union table arm",
			"package probe\n\ntype Inner\n{\n    x int32\n}\nunion Pick\n{\n    alpha Inner\n    beta int32\n}\ntable L\n{\n    u Pick\n}\n",
			`{"u":{"alpha":{"x":7}}}`,
		},
		{
			"payload-free arm",
			"package probe\n\ntype Fire\n{\n    target uint16\n}\n\nunion Weapon\n{\n    laser Fire\n    ram\n}\n\ntable L\n{\n    fire Weapon\n}\n",
			`{"fire":{"ram":null}}`,
		},
		{
			"array of unions",
			"package probe\n\ntype Ping\n{\n    nonce uint32\n}\n\ntype Pong\n{\n    nonce uint32\n}\n\nunion Reply\n{\n    ping Ping\n    pong Pong\n}\n\nfixed table L\n{\n    history [..4]Reply\n}\n",
			`{"history":[{"ping":{"nonce":1}},{"pong":{"nonce":2}}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := unitFromSource(t, tc.src)
			model := tabletext.NewModel(u)
			value := model.New(u.Tables["L"])
			var report tabletext.Report
			if !model.Read(value, []byte(tc.json), &report) || !report.Silent() {
				t.Fatalf("the Go oracle did not read %s: %+v", tc.json, report)
			}
			oracle, err := tablewire.Encode(model, value)
			if err != nil {
				t.Fatalf("the Go oracle did not encode the value: %v", err)
			}

			files, err := New().Generate(u, "squirrel", Options{})
			if err != nil {
				t.Fatalf("squirrel refused a unit the table closure holds a union in: %v", err)
			}
			dir := t.TempDir()
			for name, data := range files {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			main := strings.Replace(squirrelUnionPrelude, "%s", squirreltable.ModuleName(u), 1)
			if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(main), 0o644); err != nil {
				t.Fatal(err)
			}
			out := sqrun.Run(t, dir, "main.nut", map[string][]byte{"oracle": oracle})
			lines, err := sqrun.HexLines(out)
			if err != nil {
				t.Fatalf("the probe did not print two hex lines: %v\n%s", err, out)
			}
			if len(lines) != 2 {
				t.Fatalf("the probe printed %d lines, want the save and the round trip's save", len(lines))
			}
			if !bytes.Equal(lines[0], oracle) {
				t.Errorf("the Squirrel save wrote %d bytes, the Go oracle %d\n got %x\nwant %x", len(lines[0]), len(oracle), lines[0], oracle)
			}
			if !bytes.Equal(lines[1], lines[0]) {
				t.Errorf("a value saved and loaded back did not save the same bytes\nfirst %x\nsecond %x", lines[0], lines[1])
			}
		})
	}
}
