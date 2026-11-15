package squirreltable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

const fixedIntegritySaveGrowthSchema = `package probe
fixed table Rec | integrity = "crc32c"
{
    value int32
}
`

const fixedIntegritySaveGrowthProbe = `local S = dofile("probe/schema_module.nut", true);
local T = S.require("ProbeTables");
local r = T.Rec(); r.Value = 42;

local empty = blob(0);
local n = T.RecFixedSave(empty, [r]);
if (n < 0) throw "empty save refused: " + T.FixedReason;
if (empty.len() != n) throw "empty save length " + empty.len() + " want " + n;

local back = [T.Rec()];
local got = T.RecFixedLoad(empty, back, null);
if (got != 1 || back[0].Value != r.Value) throw "empty save did not load: " + got + ":" + T.FixedReason;

local measured = T.RecFixedMeasure(1);
local presized = blob(measured);
local m = T.RecFixedSave(presized, [r]);
if (m != n || measured != n) throw "presized save length " + m + " want " + n;
for (local i = 0; i < n; i++) {
  if (empty[i] != presized[i]) throw "save bytes differ at " + i;
}
print("saved " + n + " bytes and read one record\n");
`

func TestFixedSquirrelIntegritySaveGrows(t *testing.T) {
	t.Parallel()
	u := loadSquirrelUnit(t, fixedIntegritySaveGrowthSchema)
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "probe")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGenerated(t, moduleDir, u)
	main := filepath.Join(dir, "main.nut")
	if err := os.WriteFile(main, []byte(fixedIntegritySaveGrowthProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(sqrun.Run(t, dir, "main.nut", nil))
	if !strings.Contains(out, "saved ") {
		t.Fatalf("integrity save did not grow and round-trip an empty blob: %q", out)
	}
}
