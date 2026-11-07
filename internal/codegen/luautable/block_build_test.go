package luautable_test

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const blockBuilderSchema = `package probe
fixed table Row {
	value uint64
	flag bool
}
fixed table Root {
	stamp uint64
	rows [..65]Row
	other [..3]Row
}
`

// blockUnit loads one inline schema source into a unit.
func blockUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// blockBuild writes a unit's Luau modules into a fresh directory and returns
// the directory, the module basename and the files.
func blockBuild(t *testing.T, u *ir.Unit) (string, string, map[string][]byte) {
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
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, "ProbeTable", files
}

// blockParamsLiteral renders the block form's layout facts from the schema's
// own engine (ir/blocklayout.go) as a Luau literal the runtime builder reads.
// The prologue's uint64 words are passed pre-split into low/high u32 halves,
// because the pinned Luau interpreter has no 64-bit integer buffer writers.
func blockParamsLiteral(bl *ir.BlockLayout, buildVersion uint64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "{ magicLo = %d, magicHi = %d", uint32(ir.BlockMagic&0xffffffff), uint32(ir.BlockMagic>>32))
	fmt.Fprintf(&b, ", buildVersionLo = %d, buildVersionHi = %d", uint32(buildVersion&0xffffffff), uint32(buildVersion>>32))
	fmt.Fprintf(&b, ", byteOrder = 1, size = %d, arrays = {\n", bl.Projection.Size)
	for _, a := range bl.Arrays {
		fmt.Fprintf(&b, "\t\t{ name = %q, max = %d, stride = %d, triple = %d },\n", a.Field.Name, a.Max, a.Stride, a.TripleOffset)
	}
	b.WriteString("\t} }")
	return b.String()
}

// blockExpected lays a block out from its counts with the schema's own engine,
// the same walk the generated Go builder runs: ir.BlockExtent chooses each
// array's start, the triple goes into the projection at the field's
// TripleOffset, and the start is only the triple's OffsetOf value
// (docs/SPEC-TABLES.md §19.1; internal/codegen/gotable/blockbuild.go).
func blockExpected(bl *ir.BlockLayout, buildVersion uint64, counts []int64) []byte {
	elemAligns := make([]int64, len(bl.Arrays))
	for i, a := range bl.Arrays {
		elemAligns[i] = a.ElemAlign()
	}
	starts, used := ir.BlockExtent(bl, elemAligns, counts)
	buf := make([]byte, used)
	binary.LittleEndian.PutUint64(buf[0:8], ir.BlockMagic)
	binary.LittleEndian.PutUint64(buf[8:16], buildVersion)
	binary.LittleEndian.PutUint64(buf[16:24], ir.BlockByteOrderLittle)
	for i, a := range bl.Arrays {
		at := a.TripleOffset
		binary.LittleEndian.PutUint64(buf[at:at+8], uint64(starts[i]))
		binary.LittleEndian.PutUint32(buf[at+8:at+12], uint32(counts[i]))
		binary.LittleEndian.PutUint32(buf[at+12:at+16], uint32(a.Stride))
	}
	return buf
}

// TestLuauBlockBuilderMatchesGo builds the block bytes the generated Go builder
// builds, byte for byte, and checks an over-count producer refuses by the same
// name (docs/SPEC-TABLES.md §19.1).
func TestLuauBlockBuilderMatchesGo(t *testing.T) {
	t.Parallel()
	u := blockUnit(t, blockBuilderSchema)
	bl := ir.Blocks(u).Block("Root")
	if bl == nil {
		t.Fatal("the unit has no Root block form")
	}
	buildVersion := ir.BuildVersion(u)

	dir, mod, _ := blockBuild(t, u)
	counts := []int64{17, 2}
	expected := blockExpected(bl, buildVersion, counts)

	var cl strings.Builder
	cl.WriteString("{ ")
	for i, a := range bl.Arrays {
		if i > 0 {
			cl.WriteString(", ")
		}
		fmt.Fprintf(&cl, "%s = %d", a.Field.Name, counts[i])
	}
	cl.WriteString(" }")

	main := luaurun.HexProbe + fmt.Sprintf(`--!strict
local T = require("./%s")
local B = require("./schema_block")
local p = %s
local counts = %s
local buf, bytes = B.Begin(p, counts)
hex(buf)
local r = { name = "", count = 0, maximum = 0 }
local over = {}
for _, a in p.arrays do over[a.name] = a.max + 1 end
local buf2 = B.Begin(p, over, r)
if buf2 ~= nil then print("NO_REFUSE") else print(r.name .. "/" .. r.count .. "/" .. r.maximum) end
`, mod, blockParamsLiteral(bl, buildVersion), cl.String())

	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, dir, "main.luau", nil)
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("the probe printed too few lines: %q", out)
	}
	got, err := hex.DecodeString(strings.TrimSpace(lines[0]))
	if err != nil {
		t.Fatalf("the probe printed %q: %v", lines[0], err)
	}
	if string(got) != string(expected) {
		at := 0
		for at < len(got) && at < len(expected) && got[at] == expected[at] {
			at++
		}
		t.Errorf("block bytes differ at byte %d: got %x want %x", at, got, expected)
	}

	wantRefusal := fmt.Sprintf("%s/%d/%d", bl.Arrays[0].Field.Name, bl.Arrays[0].Max+1, bl.Arrays[0].Max)
	if got2 := strings.TrimSpace(lines[1]); got2 != wantRefusal {
		t.Errorf("over-count refusal = %q, want %q", got2, wantRefusal)
	}
}
