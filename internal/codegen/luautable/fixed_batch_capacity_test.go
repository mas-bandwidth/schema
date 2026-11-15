package luautable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// fixedBatchCapacitySchema is one fixed table so a file of five records is
// written and read back as a batch (docs/FIXED-FORM-ALGORITHM.md §5.3).
const fixedBatchCapacitySchema = `package probe

fixed table Batch
{
    a int32 = 0
    b int32 = 5
}
`

// TestFixedLuauBatchCapacity holds the Luau fixed read to the caller's
// capacity (docs/FIXED-FORM-ALGORITHM.md §5.3 step 10): n := rest / record_bytes,
// and n past the capacity refuses the whole load before any record lands. The
// capacity is the caller's argument, and a caller that passes none gets the
// leg's default of 4,096, so a file of five records reads at five and at the
// default and is refused at four.
func TestFixedLuauBatchCapacity(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	g := generateFiles(t, map[string]string{"Batch.schema": fixedBatchCapacitySchema})
	if g.err != nil {
		t.Fatalf("the Luau unit does not generate: %v", g.err)
	}
	probe := `--!strict
local T = require("./BatchTable")
local F = require("./schema_fixed")
local fixed = { hash = T.BatchFixedHash, recordBytes = T.BatchFixedRecordBytes, layout = T.BatchFixedLayout }
local rows = {}
for i = 1, 5 do
	local v = T.NewBatch()
	v.A, v.B = i, i + 10
	rows[i] = v
end
local file = F.save(T.BatchDescriptor, fixed, rows)
local fails = 0
local function check(ok: boolean, what: string)
	if not ok then fails += 1; print("FAIL: " .. what) end
end
local full: any = {}
local all = F.read(T.BatchDescriptor, fixed, file, full)
check(all ~= nil and #all == 5, "no capacity did not read five records: " .. tostring(full.reason))
if all ~= nil then
	check(all[5].A == 5 and all[5].B == 15, "the fifth record did not land")
end
local small: any = {}
check(F.read(T.BatchDescriptor, fixed, file, small, 4) == nil, "capacity 4 read a file of five records")
local exact: any = {}
local got = F.read(T.BatchDescriptor, fixed, file, exact, 5)
check(got ~= nil and #got == 5, "capacity 5 did not read five records: " .. tostring(exact.reason))
check(not pcall(F.load, T.BatchDescriptor, fixed, file, nil, 4), "F.load at capacity 4 did not refuse")
check(pcall(F.load, T.BatchDescriptor, fixed, file, nil, 5), "F.load at capacity 5 refused")
check(pcall(F.load, T.BatchDescriptor, fixed, file), "F.load at the default capacity refused")
if fails == 0 then print("ok") end
`
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", nil)))
	if out != "ok" {
		t.Fatalf("fixed batch capacity:\n%s", out)
	}
}
