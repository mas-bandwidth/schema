package luautable_test

// THE FIXED FORM'S FIXED-POINT, TEXT AND CONTAINER VALUE ROWS ON THE LUAU
// LEG, PROVED AGAINST A LIVE C++ REFERENCE (docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-ALGORITHM.md). The shared fixed-proof suite
// (internal/fixedproof) supplies each row's schema, its live C++ reference and
// the file and writer negative controls; this file adds the Luau driver: the
// row's records built through the emitted constructors, saved through
// schema_fixed.luau, and the values the load of the reference's file must
// land. Only group B, the nine fixed-point, text and container rows, is
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

// luauFixedGroupBRows is the nine fixed-point, text and container rows this card registers.
var luauFixedGroupBRows = []string{
	"signed-fixed-point",
	"unsigned-fixed-point",
	"flags-values",
	"enum-values",
	"utf8-values",
	"byte-buffers",
	"nested-types",
	"nested-fixed-tables",
	"fixed-arrays",
}

// luauFixedGroupBRow is the Luau side of one group B row: the statements that
// build the values through the emitted constructors, and the predicate the
// loaded values must satisfy.
type luauFixedGroupBRow struct {
	build string
	check string
}

func luauFixedGroupBRowFor(t *testing.T, id string) luauFixedGroupBRow {
	t.Helper()
	switch id {
	case "signed-fixed-point":
		return luauFixedGroupBRow{
			build: `
local v0 = T.NewT()
v0.A = -524288
v0.B = -6553600i
v0.C = R.int128(0xffffffffff000000i, 0xffffffffffffffffi)
local v1 = T.NewT()
v1.A = 524288
v1.B = 6553600i
v1.C = R.int128(0x0000000000fa0000i, 0i)
values = { v0, v1 }
`,
			check: `back[1].A == -524288 and back[1].B == -6553600i and back[1].C.lo == 0xffffffffff000000i and back[1].C.hi == 0xffffffffffffffffi
	and back[2].A == 524288 and back[2].B == 6553600i and back[2].C.lo == 0x0000000000fa0000i and back[2].C.hi == 0i`,
		}
	case "unsigned-fixed-point":
		return luauFixedGroupBRow{
			build: `
local v0 = T.NewT()
v0.A = 0
v0.B = 0i
v0.C = R.int128(0i, 0i)
local v1 = T.NewT()
v1.A = 1048576
v1.B = 6553600i
v1.C = R.int128(65536000i, 0i)
values = { v0, v1 }
`,
			check: `back[1].A == 0 and back[1].B == 0i and back[1].C.lo == 0i and back[1].C.hi == 0i
	and back[2].A == 1048576 and back[2].B == 6553600i and back[2].C.lo == 65536000i and back[2].C.hi == 0i`,
		}
	case "flags-values":
		return luauFixedGroupBRow{
			build: `
local v0 = T.NewT()
v0.Caps = 0i
local v1 = T.NewT()
v1.Caps = 5i
local v2 = T.NewT()
v2.Caps = 7i
values = { v0, v1, v2 }
`,
			check: `back[1].Caps == 0i and back[2].Caps == 5i and back[3].Caps == 7i`,
		}
	case "enum-values":
		return luauFixedGroupBRow{
			build: `
local v0 = T.NewT()
v0.G = 0
local v1 = T.NewT()
v1.G = 1
local v2 = T.NewT()
v2.G = 3
values = { v0, v1, v2 }
`,
			check: `back[1].G == 0 and back[2].G == 1 and back[3].G == 3`,
		}
	case "utf8-values":
		return luauFixedGroupBRow{
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
buffer.writestring(v1.S, 0, "hello")
v1.SLength = 5
values = { v0, v1 }
`,
			check: `back[1].SLength == 0 and back[2].SLength == 5 and buffer.readstring(back[2].S, 0, 5) == "hello"`,
		}
	case "byte-buffers":
		return luauFixedGroupBRow{
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
buffer.writeu8(v1.Blob, 0, 0xff)
buffer.writeu8(v1.Blob, 1, 0x00)
buffer.writeu8(v1.Blob, 2, 0x5a)
buffer.writeu8(v1.Blob, 3, 1)
buffer.writeu8(v1.Blob, 4, 2)
buffer.writeu8(v1.Blob, 5, 3)
v1.BlobLength = 6
values = { v0, v1 }
`,
			check: `back[1].BlobLength == 0 and back[2].BlobLength == 6
	and buffer.readu8(back[2].Blob, 0) == 0xff
	and buffer.readu8(back[2].Blob, 1) == 0x00
	and buffer.readu8(back[2].Blob, 2) == 0x5a
	and buffer.readu8(back[2].Blob, 3) == 1
	and buffer.readu8(back[2].Blob, 4) == 2
	and buffer.readu8(back[2].Blob, 5) == 3`,
		}
	case "nested-types":
		return luauFixedGroupBRow{
			build: `
local v0 = T.NewT()
buffer.writestring(v0.Badge.Title, 0, "hi")
v0.Badge.TitleLength = 2
values = { v0 }
`,
			check: `back[1].Badge.TitleLength == 2 and buffer.readstring(back[1].Badge.Title, 0, 2) == "hi"`,
		}
	case "nested-fixed-tables":
		return luauFixedGroupBRow{
			build: `
local v0 = T.NewT()
v0.Child.N = -2147483648
local v1 = T.NewT()
v1.Child.N = 2147483647
values = { v0, v1 }
`,
			check: `back[1].Child.N == -2147483648 and back[2].Child.N == 2147483647`,
		}
	case "fixed-arrays":
		return luauFixedGroupBRow{
			build: `
local v0 = T.NewT()
v0.Tag[1] = -2147483648
v0.Tag[2] = 0
v0.Tag[3] = 2147483647
values = { v0 }
`,
			check: `back[1].Tag[1] == -2147483648 and back[1].Tag[2] == 0 and back[1].Tag[3] == 2147483647`,
		}
	}
	t.Fatalf("no Luau driver for row %q", id)
	return luauFixedGroupBRow{}
}

// luauFixedGroupBUnit generates a unit's Luau type and table surfaces and
// writes them into a fresh directory.
func luauFixedGroupBUnit(t *testing.T, u *ir.Unit) (*generatedUnit, string) {
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

const luauFixedGroupBHex = `local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
`

// luauFixedGroupBSaveProgram builds the row's values and saves them through
// schema_fixed.luau, printing "@x <hex>".
func luauFixedGroupBSaveProgram(mod, root, build string) string {
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
`, mod, luauFixedGroupBHex, root, build)
}

// luauFixedGroupBLoadProgram loads inputs.wire through schema_fixed.luau,
// holds the values to the row's predicate, and re-saves them, printing
// "@x <hex>", or "@refused <why>" / "@mismatch <why>".
func luauFixedGroupBLoadProgram(mod, root string, count int, check string) string {
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
`, mod, luauFixedGroupBHex, root, count, check)
}

func luauFixedGroupBRun(t *testing.T, dir, main string, inputs map[string][]byte) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	return string(luaurun.Run(t, dir, "main.luau", inputs))
}

func luauFixedGroupBBytes(t *testing.T, out string) ([]byte, error) {
	t.Helper()
	for line := range strings.SplitSeq(out, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "@x "); ok {
			b, err := hex.DecodeString(strings.TrimSpace(rest))
			if err != nil {
				t.Fatalf("bad hex from probe: %v", err)
			}
			return b, nil
		}
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "@refused "); ok {
			return nil, fmt.Errorf("the load refused the wire: %s", rest)
		}
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "@mismatch "); ok {
			return nil, fmt.Errorf("the load landed the wrong values: %s", rest)
		}
	}
	return nil, fmt.Errorf("the probe printed no result:\n%s", out)
}

// luauFixedGroupBRunner is the Luau leg's driver for group B of the shared
// fixed-proof suite.
type luauFixedGroupBRunner struct{}

func (r *luauFixedGroupBRunner) Name() string { return "luau" }

func (r *luauFixedGroupBRunner) Save(t *testing.T, tc fixedproof.RowCase, u *ir.Unit) []byte {
	t.Helper()
	row := luauFixedGroupBRowFor(t, tc.ID)
	g, mod := luauFixedGroupBUnit(t, u)
	out := luauFixedGroupBRun(t, g.dir, luauFixedGroupBSaveProgram(mod, tc.Root, row.build), nil)
	b, err := luauFixedGroupBBytes(t, out)
	if err != nil {
		t.Fatalf("%s: luau did not save: %v", tc.ID, err)
	}
	return b
}

func (r *luauFixedGroupBRunner) LoadAndResave(t *testing.T, tc fixedproof.RowCase, u *ir.Unit, wire []byte) ([]byte, error) {
	t.Helper()
	row := luauFixedGroupBRowFor(t, tc.ID)
	g, mod := luauFixedGroupBUnit(t, u)
	out := luauFixedGroupBRun(t, g.dir, luauFixedGroupBLoadProgram(mod, tc.Root, tc.Count, row.check), map[string][]byte{"wire": wire})
	return luauFixedGroupBBytes(t, out)
}

func (r *luauFixedGroupBRunner) SaveMutated(t *testing.T, tc fixedproof.RowCase, u *ir.Unit) ([]byte, error) {
	t.Helper()
	row := luauFixedGroupBRowFor(t, tc.ID)
	g, mod := luauFixedGroupBUnit(t, u)
	const anchor = "\t\twriteStruct(d, root, v, rec)\n"
	planted := anchor + "\t\tbuffer.writeu8(rec, fixed.recordBytes - 1, bit32.bxor(buffer.readu8(rec, fixed.recordBytes - 1), 1))\n"
	src := string(g.files["schema_fixed.luau"])
	if strings.Count(src, anchor) != 1 {
		t.Fatalf("%s: the record write anchor is not in schema_fixed.luau exactly once", tc.ID)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "schema_fixed.luau"), []byte(strings.Replace(src, anchor, planted, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luauFixedGroupBRun(t, g.dir, luauFixedGroupBSaveProgram(mod, tc.Root, row.build), nil)
	return luauFixedGroupBBytes(t, out)
}

func luauFixedGroupBCase(t *testing.T, id string) fixedproof.RowCase {
	t.Helper()
	for _, c := range fixedproof.Cases() {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("the shared fixed-proof suite has no row %q", id)
	return fixedproof.RowCase{}
}

// TestLuauFixedProofGroupB proves each group B fixed-point, text and container
// row against the live C++ reference: the leg's save is the reference's file
// byte for byte, the load of the reference's file lands the row's values and
// re-saves it to the reference's bytes, and the reference loads the leg's file
// back to its own.
func TestLuauFixedProofGroupB(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "luau and c++ (it runs the emitted fixed-form runtime against a compiled C++ reference)")
	runner := &luauFixedGroupBRunner{}
	for _, id := range luauFixedGroupBRows {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			tc := luauFixedGroupBCase(t, id)
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

// TestLuauFixedProofGroupBNegativeControl breaks each group B row two ways
// and requires the gates TestLuauFixedProofGroupB holds green to go red: the
// reference's file is damaged (the form byte, and the last record's first
// body byte), and schema_fixed.luau's writer is planted to flip each record's
// last body byte.
func TestLuauFixedProofGroupBNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "luau and c++ (it runs the emitted fixed-form runtime against a compiled C++ reference)")
	runner := &luauFixedGroupBRunner{}
	for _, id := range luauFixedGroupBRows {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			tc := luauFixedGroupBCase(t, id)
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
