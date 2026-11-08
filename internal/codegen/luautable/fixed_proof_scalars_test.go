package luautable_test

// THE FIXED FORM'S SCALAR VALUE ROWS ON THE LUAU LEG, PROVED AGAINST A LIVE
// C++ REFERENCE (docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-ALGORITHM.md). The
// shared fixed-proof suite (internal/fixedproof) supplies each row's schema,
// its live C++ reference and the file and writer negative controls; this file
// adds the Luau driver: the row's records built through the emitted
// constructors, saved through schema_fixed.luau, and the values the load of
// the reference's file must land. Only group A, the ten scalar rows, is
// registered here.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// luauFixedGroupARows is the ten scalar value rows this card registers.
var luauFixedGroupARows = []string{
	"bool-values",
	"signed-integers",
	"unsigned-integers",
	"signed-128",
	"unsigned-128",
	"ranged-integers",
	"bits-values",
	"float32-values",
	"float64-values",
	"compressed-floats",
}

// luauFixedRow is the Luau side of one row: the statements that build the
// values through the emitted constructors, and the predicate the loaded
// values must satisfy.
type luauFixedRow struct {
	build string
	check string
}

func luauFixedRowFor(t *testing.T, id string) luauFixedRow {
	t.Helper()
	switch id {
	case "bool-values":
		return luauFixedRow{
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.A = true
v1.B = false
values = { v0, v1 }
`,
			check: `back[1].A == false and back[1].B == true and back[2].A == true and back[2].B == false`,
		}
	case "signed-integers":
		return luauFixedRow{
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.A = 127
v1.B = 32767
v1.C = 2147483647
v1.D = 9223372036854775807i
local v2 = T.NewT()
v2.A = -1
v2.B = 0
v2.C = 1
v2.D = -2i
values = { v0, v1, v2 }
`,
			check: `back[1].A == -128 and back[1].B == -32768 and back[1].C == -2147483648 and back[1].D == 0x8000000000000000i
	and back[2].A == 127 and back[2].B == 32767 and back[2].C == 2147483647 and back[2].D == 9223372036854775807i
	and back[3].A == -1 and back[3].B == 0 and back[3].C == 1 and back[3].D == -2i`,
		}
	case "unsigned-integers":
		return luauFixedRow{
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.A = 0
v1.B = 0
v1.C = 0
v1.D = 0i
local v2 = T.NewT()
v2.A = 1
v2.B = 256
v2.C = 65536
v2.D = 4294967296i
values = { v0, v1, v2 }
`,
			check: `back[1].A == 255 and back[1].B == 65535 and back[1].C == 4294967295 and back[1].D == 0xffffffffffffffffi
	and back[2].A == 0 and back[2].B == 0 and back[2].C == 0 and back[2].D == 0i
	and back[3].A == 1 and back[3].B == 256 and back[3].C == 65536 and back[3].D == 4294967296i`,
		}
	case "signed-128":
		return luauFixedRow{
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.A = R.int128(0xffffffffffffffffi, 0x7fffffffffffffffi)
local v2 = T.NewT()
v2.A = R.int128(1i, 0i)
values = { v0, v1, v2 }
`,
			check: `back[1].A.lo == 0i and back[1].A.hi == 0x8000000000000000i
	and back[2].A.lo == 0xffffffffffffffffi and back[2].A.hi == 0x7fffffffffffffffi
	and back[3].A.lo == 1i and back[3].A.hi == 0i`,
		}
	case "unsigned-128":
		return luauFixedRow{
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.A = R.int128(0i, 0i)
local v2 = T.NewT()
v2.A = R.int128(1i, 0i)
values = { v0, v1, v2 }
`,
			check: `back[1].A.lo == 0xffffffffffffffffi and back[1].A.hi == 0xffffffffffffffffi
	and back[2].A.lo == 0i and back[2].A.hi == 0i
	and back[3].A.lo == 1i and back[3].A.hi == 0i`,
		}
	case "ranged-integers":
		return luauFixedRow{
			build: `
local v0 = T.NewT()
v0.A = -5
v0.B = -1000000000000i
v0.C = 0
local v1 = T.NewT()
v1.A = 5
v1.B = 1000000000000i
v1.C = 1000
values = { v0, v1 }
`,
			check: `back[1].A == -5 and back[1].B == -1000000000000i and back[1].C == 0
	and back[2].A == 5 and back[2].B == 1000000000000i and back[2].C == 1000`,
		}
	case "bits-values":
		return luauFixedRow{
			build: `
local v0 = T.NewT()
v0.A = 7
v0.B = 4095
v0.C = 1048575
v0.D = 1099511627775i
local v1 = T.NewT()
values = { v0, v1 }
`,
			check: `back[1].A == 7 and back[1].B == 4095 and back[1].C == 1048575 and back[1].D == 1099511627775i
	and back[2].A == 0 and back[2].B == 0 and back[2].C == 0 and back[2].D == 0i`,
		}
	case "float32-values":
		return luauFixedRow{
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.A = -1024.25
v1.B = 2048.75
values = { v0, v1 }
`,
			check: `back[1].A == 3.5 and back[1].B == -0.5 and back[2].A == -1024.25 and back[2].B == 2048.75`,
		}
	case "float64-values":
		return luauFixedRow{
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.A = -987654321.125
v1.B = 123456789.875
values = { v0, v1 }
`,
			check: `back[1].A == 2.5 and back[1].B == -1234.5 and back[2].A == -987654321.125 and back[2].B == 123456789.875`,
		}
	case "compressed-floats":
		return luauFixedRow{
			build: `
local v0 = T.NewT()
v0.A = 0.25
v0.B = -0.5
local v1 = T.NewT()
v1.A = 0.0
v1.B = -1.0
local v2 = T.NewT()
v2.A = 1.0
v2.B = 1.0
values = { v0, v1, v2 }
`,
			check: `back[1].A == 0.25 and back[1].B == -0.5
	and back[2].A == 0.0 and back[2].B == -1.0
	and back[3].A == 1.0 and back[3].B == 1.0`,
		}
	}
	t.Fatalf("no Luau driver for row %q", id)
	return luauFixedRow{}
}

// luauFixedUnit generates a unit's Luau type and table surfaces and writes
// them into a fresh directory.
func luauFixedUnit(t *testing.T, u *ir.Unit) (*generatedUnit, string) {
	t.Helper()
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	g := &generatedUnit{dir: t.TempDir(), files: files, loaded: true}
	mod := ""
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.luau") {
			if mod != "" {
				t.Fatalf("the row's unit has two table modules: %s and %s", mod, name)
			}
			mod = strings.TrimSuffix(name, ".luau")
		}
	}
	if mod == "" {
		t.Fatal("the row's unit emitted no table module")
	}
	return g, mod
}

const luauFixedHex = `local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
`

// luauFixedSaveProgram builds the row's values and saves them through
// schema_fixed.luau, printing "@x <hex>".
func luauFixedSaveProgram(mod, root, build string) string {
	return fmt.Sprintf(`--!strict
local T = require("./%[1]s")
local F = require("./schema_fixed")
local R = require("./schema_runtime")
%[2]s
local fixed = { hash = T.%[3]sFixedHash, recordBytes = T.%[3]sFixedRecordBytes, layout = T.%[3]sFixedLayout, lineage = T.%[3]sFixedLineage, floor = T.%[3]sFixedFloor }
local values: { any } = {}
%[4]s
local out = F.save(T.%[3]sDescriptor, fixed, values)
print("@x " .. hex(out))
`, mod, luauFixedHex, root, build)
}

// luauFixedLoadProgram loads inputs.wire through schema_fixed.luau, holds the
// values to the row's predicate, and re-saves them, printing "@x <hex>", or
// "@refused <why>" / "@mismatch <why>".
func luauFixedLoadProgram(mod, root string, count int, check string) string {
	return fmt.Sprintf(`--!strict
local inputs = require("./inputs")
local T = require("./%[1]s")
local F = require("./schema_fixed")
%[2]s
local fixed = { hash = T.%[3]sFixedHash, recordBytes = T.%[3]sFixedRecordBytes, layout = T.%[3]sFixedLayout, lineage = T.%[3]sFixedLineage, floor = T.%[3]sFixedFloor }
local ok, got = pcall(F.load, T.%[3]sDescriptor, fixed, inputs.wire)
if not ok then print("@refused " .. tostring(got)) return end
local back = got
if #back ~= %[4]d then print("@mismatch count " .. tostring(#back)) return end
if not (%[5]s) then print("@mismatch values") return end
local out = F.save(T.%[3]sDescriptor, fixed, back)
print("@x " .. hex(out))
`, mod, luauFixedHex, root, count, check)
}

func luauFixedRun(t *testing.T, dir, main string, inputs map[string][]byte) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	return string(luaurun.Run(t, dir, "main.luau", inputs))
}

func luauFixedBytes(t *testing.T, out string) ([]byte, error) {
	t.Helper()
	for line := range strings.SplitSeq(out, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "@x "); ok {
			b, err := hex.DecodeString(rest)
			if err != nil {
				return nil, fmt.Errorf("the probe printed %q: %w", rest, err)
			}
			return b, nil
		}
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "@refused "); ok {
			return nil, fmt.Errorf("the load refused: %s", rest)
		}
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "@mismatch "); ok {
			return nil, fmt.Errorf("the load landed the wrong values: %s", rest)
		}
	}
	return nil, fmt.Errorf("the probe printed no result:\n%s", out)
}

// luauFixedRunner is the Luau leg's driver for the shared fixed-proof suite.
type luauFixedRunner struct{}

func (r *luauFixedRunner) Name() string { return "luau" }

func (r *luauFixedRunner) Save(t *testing.T, tc fixedproof.RowCase, u *ir.Unit) []byte {
	t.Helper()
	row := luauFixedRowFor(t, tc.ID)
	g, mod := luauFixedUnit(t, u)
	out := luauFixedRun(t, g.dir, luauFixedSaveProgram(mod, tc.Root, row.build), nil)
	b, err := luauFixedBytes(t, out)
	if err != nil {
		t.Fatalf("%s: luau did not save: %v", tc.ID, err)
	}
	return b
}

func (r *luauFixedRunner) LoadAndResave(t *testing.T, tc fixedproof.RowCase, u *ir.Unit, wire []byte) ([]byte, error) {
	t.Helper()
	row := luauFixedRowFor(t, tc.ID)
	g, mod := luauFixedUnit(t, u)
	out := luauFixedRun(t, g.dir, luauFixedLoadProgram(mod, tc.Root, tc.Count, row.check), map[string][]byte{"wire": wire})
	return luauFixedBytes(t, out)
}

func (r *luauFixedRunner) SaveMutated(t *testing.T, tc fixedproof.RowCase, u *ir.Unit) ([]byte, error) {
	t.Helper()
	row := luauFixedRowFor(t, tc.ID)
	g, mod := luauFixedUnit(t, u)
	const anchor = "\t\twriteStruct(d, root, v, rec)\n"
	planted := anchor + "\t\tbuffer.writeu8(rec, fixed.recordBytes - 1, bit32.bxor(buffer.readu8(rec, fixed.recordBytes - 1), 1))\n"
	src := string(g.files["schema_fixed.luau"])
	if strings.Count(src, anchor) != 1 {
		t.Fatalf("%s: the record write anchor is not in schema_fixed.luau exactly once", tc.ID)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "schema_fixed.luau"), []byte(strings.Replace(src, anchor, planted, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luauFixedRun(t, g.dir, luauFixedSaveProgram(mod, tc.Root, row.build), nil)
	return luauFixedBytes(t, out)
}

func luauFixedCase(t *testing.T, id string) fixedproof.RowCase {
	t.Helper()
	for _, c := range fixedproof.Cases() {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("the shared fixed-proof suite has no row %q", id)
	return fixedproof.RowCase{}
}

// TestLuauFixedProofGroupA proves each group A scalar row against the live
// C++ reference: the leg's save is the reference's file byte for byte, the
// load of the reference's file lands the row's values and re-saves it to the
// reference's bytes, and the reference loads the leg's file back to its own.
func TestLuauFixedProofGroupA(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "luau and c++ (it runs the emitted fixed-form runtime against a compiled C++ reference)")
	runner := &luauFixedRunner{}
	for _, id := range luauFixedGroupARows {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			tc := luauFixedCase(t, id)
			u := fixedproof.ParseCheckUnit(t, tc.Schema)
			st := fixedproof.RootStruct(t, u, tc.Root)
			layout, hash, body := fixedproof.RootFacts(st)
			if body != tc.Body {
				t.Fatalf("%s: body bytes = %d, want %d", id, body, tc.Body)
			}
			if len(layout) < 4+17 || hash == 0 {
				t.Fatalf("%s: invalid layout (%d bytes) or zero hash", id, len(layout))
			}
			cppBytes, verifyInCpp := fixedproof.CppOracle(t, tc, u)
			legBytes := runner.Save(t, tc, u)
			if !bytes.Equal(legBytes, cppBytes) {
				t.Fatalf("%s: luau FixedSave bytes != C++ reference FixedSave bytes: %s\nleg=%x\ncpp=%x", id, firstDiff(legBytes, cppBytes), legBytes, cppBytes)
			}
			resaved, err := runner.LoadAndResave(t, tc, u, cppBytes)
			if err != nil {
				t.Fatalf("%s: luau FixedLoad of C++ reference bytes failed: %v", id, err)
			}
			if !bytes.Equal(resaved, cppBytes) {
				t.Fatalf("%s: luau re-save after loading C++ bytes differed: %s", id, firstDiff(resaved, cppBytes))
			}
			if err := verifyInCpp(legBytes); err != nil {
				t.Fatalf("%s: C++ FixedLoad verification of luau bytes failed: %v", id, err)
			}
		})
	}
}

// TestLuauFixedProofGroupANegativeControl breaks each group A row two ways
// and requires the gates TestLuauFixedProofGroupA holds green to go red: the
// reference's file is damaged (the form byte, and the last record's first
// body byte), and schema_fixed.luau's writer is planted to flip each record's
// last body byte.
func TestLuauFixedProofGroupANegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "luau and c++ (it runs the emitted fixed-form runtime against a compiled C++ reference)")
	runner := &luauFixedRunner{}
	for _, id := range luauFixedGroupARows {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			tc := luauFixedCase(t, id)
			u := fixedproof.ParseCheckUnit(t, tc.Schema)
			st := fixedproof.RootStruct(t, u, tc.Root)
			layout, _, body := fixedproof.RootFacts(st)
			cppBytes, verifyInCpp := fixedproof.CppOracle(t, tc, u)

			corruptForm := bytes.Clone(cppBytes)
			corruptForm[0] ^= 0x01
			if err := verifyInCpp(corruptForm); err == nil {
				t.Fatalf("%s: file negative control failed: C++ FixedLoad accepted a corrupted form byte", id)
			}
			if _, err := runner.LoadAndResave(t, tc, u, corruptForm); err == nil {
				t.Fatalf("%s: file negative control failed: luau FixedLoad accepted a corrupted form byte", id)
			}

			mutated := bytes.Clone(cppBytes)
			at := 20 + len(layout) + (tc.Count-1)*int(8+body) + 8
			if at >= len(mutated) || body <= 0 {
				t.Fatalf("%s: unexpected record body offset %d (len=%d)", id, at, len(mutated))
			}
			mutated[at] ^= 0x01
			if err := verifyInCpp(mutated); err == nil {
				t.Fatalf("%s: file negative control failed: C++ FixedLoad accepted a mutated body byte", id)
			}
			if resaved, err := runner.LoadAndResave(t, tc, u, mutated); err == nil && bytes.Equal(resaved, cppBytes) {
				t.Fatalf("%s: file negative control failed: luau re-saved the damaged file as the reference's bytes", id)
			}

			mutatedWrite, err := runner.SaveMutated(t, tc, u)
			if err != nil {
				t.Fatalf("%s: writer negative control failed: the planted writer did not save: %v", id, err)
			}
			if bytes.Equal(mutatedWrite, cppBytes) {
				t.Fatalf("%s: writer negative control failed: the planted writer still produced the reference's bytes", id)
			}
			if err := verifyInCpp(mutatedWrite); err == nil {
				t.Fatalf("%s: writer negative control failed: C++ accepted the planted writer's file", id)
			}
		})
	}
}
