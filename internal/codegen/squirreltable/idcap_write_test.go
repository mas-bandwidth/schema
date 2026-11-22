package squirreltable_test

// THE WRITER REFUSES AN ID TABLE ITS READERS REFUSE (docs/SPEC-TABLES.md §3,
// ruling R8). Squirrel refuses in every build: Save answers null for a value
// that needs 4097 ids, Measure -1, and nothing is written. The values that
// need 4095 and 4096 are written, carry that count word, and read back. The
// unit is tablewire.IdCapSchema.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestSquirrelWriterRefusesMoreThan4096Ids(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	dir := t.TempDir()
	mod := emitUnit(t, dir, "idcap", unitFromSchema(t, tablewire.IdCapSchema()))
	script := `local T = dofile("idcap/schema_module.nut", true).require("` + mod + `");
local value = function(x, y) {
  local v = T.Root();
  for (local i = 0; i < v.Keys.len(); i++) v.Keys[i] = 1;
  v.X = x; v.Y = y;
  return v;
};
local count = function(b) {
  local n = 0;
  for (local i = 7; i >= 0; i--) n = n * 256 + (b[b.len() - 8 + i] & 0xff);
  return n;
};
foreach (row in [[0, 0], [1, 0]]) {
  local v = value(row[0], row[1]);
  local b = T.RootSave(v);
  if (b == null) { print("row" + row[0] + " refused\n"); continue; }
  local back = T.RootLoad(b);
  print("row" + row[0] + " wrote " + count(b) + " " + (back == null ? "unread" : "read") + "\n");
}
local over = value(1, 1);
print("over " + (T.RootSave(over) == null ? "refused" : "wrote") + " " + T.RootMeasure(over) + "\n");
`
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(script))
	out := string(sqrun.Run(t, dir, "main.nut", nil))
	for _, want := range []string{"row0 wrote 4095 read", "row1 wrote 4096 read", "over refused -1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
