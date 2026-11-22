package luautable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A fixed read with no capacity reads the file's own records: 4,096 is a
// limit the spec never states (Go's destination slice is the only bound), so
// 4,097 records read, and a caller-set capacity below the count still refuses
// batch_too_large. The planted old default of 4,096 goes red.
func TestFixedLuauNoCapacityIsUnbounded(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	run := func(plant bool) string {
		g := generateFiles(t, map[string]string{"Batch.schema": fixedBatchCapacitySchema})
		if plant {
			path := filepath.Join(g.dir, "schema_fixed.luau")
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			const anchor = "local cap = capacity or count\n"
			if strings.Count(string(src), anchor) != 1 {
				t.Fatalf("the anchor %q is not once in schema_fixed.luau", anchor)
			}
			if err := os.WriteFile(path, []byte(strings.Replace(string(src), anchor, "local cap = capacity or 4096\n", 1)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		probe := `--!strict
local T = require("./BatchTable")
local F = require("./schema_fixed")
local fixed = { hash = T.BatchFixedHash, recordBytes = T.BatchFixedRecordBytes, layout = T.BatchFixedLayout }
local rows = {}
for i = 1, 4097 do
	local v = T.NewBatch()
	v.A = i
	rows[i] = v
end
local file = F.save(T.BatchDescriptor, fixed, rows)
local rep: any = {}
local back = F.read(T.BatchDescriptor, fixed, file, rep)
print(tostring(back ~= nil and #back) .. " " .. tostring(rep.reason))
local small: any = {}
print(tostring(F.read(T.BatchDescriptor, fixed, file, small, 4096) == nil) .. " " .. tostring(small.reason))
`
		if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", nil)))
	}
	const want = "4097 nil\ntrue batch_too_large"
	if got := run(false); got != want {
		t.Fatalf("no capacity: got %q want %q", got, want)
	}
	if got := run(true); got == want {
		t.Fatal("the planted 4,096 default is not red")
	}
}
