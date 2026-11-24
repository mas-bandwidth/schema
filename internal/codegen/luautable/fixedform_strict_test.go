package luautable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestLuauFixedPlainReadIsStrict holds the fixed form to docs/SPEC-TABLES.md
// §6.5, THE PLAIN NAME IS THE STRICT READ, THE TOLERANT ONE IS NAMED: F.read
// (and F.load, which raises) hands back nothing for a read whose report the
// strict read refuses, naming it. The refused entry is a lineage entry handed
// in at run time whose `b` changed kind off every ladder (int32 to float32):
// that entry is UNLAWFUL (docs/FIXED-FORM-ALGORITHM.md §5.2 EMIT, §5.4, §5.9
// #30), so no plan is laid and EVERY read of it refuses `layout_malformed` —
// the tolerant F.loadWalk as well, which keeps no record and moves no counter,
// because there is no compile census to count and read past. A widening alone
// never refuses (fixedversioning_test.go).
func TestLuauFixedPlainReadIsStrict(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	g := generateFiles(t, map[string]string{"Pt.schema": "package probe\n\nfixed table Pt\n{\n    a int32 = 0\n    b int32 = 5\n}\n"})
	if g.err != nil {
		t.Fatalf("the Luau unit does not generate: %v", g.err)
	}
	probe := `--!strict
local T = require("./PtTable")
local F = require("./schema_fixed")
local fails = 0
local function check(ok: boolean, what: string)
	if not ok then fails += 1; print("FAIL: " .. what) end
end
-- a stranger's layout the build handed in: Pt with b retyped float32
local forged = {}
for i, e in T.PtFixedLayout do
	forged[i] = { id = e.id, kind = if i == 3 then 10 else e.kind, size = e.size, children = e.children }
end
local known = { hash = 0x0123456789abcdefi, layout = forged, layoutBytes = 4 + 17 * #forged, recordBytes = 8 + T.PtFixedRecordBytes }
local fixed = { hash = T.PtFixedHash, recordBytes = T.PtFixedRecordBytes, layout = T.PtFixedLayout,
	lineage = { known, T.PtFixedLineage[#T.PtFixedLineage] }, floor = 0 }
local p = T.NewPt()
p.A, p.B = 7, 9
local file = F.save(T.PtDescriptor, { hash = known.hash, recordBytes = T.PtFixedRecordBytes, layout = forged }, { p })
local tolerant: any = { unknown = 9, kindMismatch = 9, widened = 9 }
local kept = F.loadWalk(T.PtDescriptor, fixed, file, tolerant)
check(kept == nil, "the tolerant read kept a record of an unlawful entry")
check(tolerant.reason == "layout_malformed", "the tolerant read names layout_malformed: " .. tostring(tolerant.reason))
check(tolerant.unknown == 0 and tolerant.kindMismatch == 0 and tolerant.widened == 0, "the tolerant refusal moved a counter")
local strict: any = {}
check(F.read(T.PtDescriptor, fixed, file, strict) == nil, "the plain read handed back a record its report refuses")
check(strict.reason == "layout_malformed" and strict.kindMismatch == 0, "the plain read names layout_malformed: " .. tostring(strict.reason))
local ok, err = pcall(F.load, T.PtDescriptor, fixed, file)
check(not ok and type(err) == "table" and (err :: any).name == "layout_malformed", "F.load raises the strict refusal by name")
local own = F.save(T.PtDescriptor, fixed, { p })
local clean: any = {}
local back = F.read(T.PtDescriptor, fixed, own, clean)
check(back ~= nil and back[1].B == 9 and clean.reason == nil, "a clean identity read passes the plain name")
if fails == 0 then print("ok") end
`
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", nil)))
	if out != "ok" {
		t.Fatalf("plain read strict:\n%s", out)
	}
}
