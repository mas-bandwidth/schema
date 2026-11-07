package mootable_test

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// blockBuildMain opens inputs.block at the named table, reads it, builds a
// fresh block of the same counts through table_block.moo's Begin, writes the
// read value into it with Write, and prints the built bytes as hex — or, when
// `counts` is non-empty, builds from those counts alone and prints the bytes
// Begin wrote (no row, no padding, §19.1).
func blockBuildMain(g *generatedUnit, table string, counts string) string {
	mod := ""
	for _, m := range g.tableModules() {
		if strings.Contains(string(g.files[m+".moo"]), "M."+table+"BlockInfo = ") {
			mod = m
		}
	}
	var b strings.Builder
	b.WriteString("--!strict\nlocal inputs = require(\"./inputs\")\nlocal R = require(\"./table_blockread\")\nlocal W = require(\"./table_block\")\n")
	fmt.Fprintf(&b, "local T = require(\"./%s\")\nlocal info = T.%sBlockInfo\n", mod, table)
	b.WriteString(`local function hex(buf: buffer, n: number): string
	local parts = {}
	for i = 0, n - 1 do
		parts[#parts + 1] = string.format("%02x", buffer.readu8(buf, i))
	end
	return table.concat(parts)
end
`)
	if counts != "" {
		fmt.Fprintf(&b, "local counts: { [string]: number } = %s\n", counts)
		b.WriteString(`local why = { name = "", count = 0, maximum = 0 }
local block, bytes = W.Begin(info, counts, why)
if block == nil then
	print("refuse " .. why.name .. " " .. why.count .. " " .. why.maximum)
	return
end
print(hex(block.buffer, bytes :: number))
`)
		return b.String()
	}
	b.WriteString(`local src, why = R.BlockOpen(info, inputs.block)
if src == nil then
	print("refuse " .. tostring(why))
	return
end
local value = R.Read(src)
local counts: { [string]: number } = {}
for _, f in info.fields do
	if f.outOfLine then
		local _, count = R.Triple(src, f)
		counts[f.name] = count
	end
end
local block, bytes = W.Begin(info, counts)
if block == nil then
	print("refuse begin")
	return
end
local refusal = W.Write(block, value)
if refusal ~= nil then
	print("refuse " .. refusal.name .. " " .. refusal.field)
	return
end
print(hex(block.buffer, bytes :: number))
`)
	return b.String()
}

// TestMooBlockBuilderRebuildsTheGoldens is the builder's value gate
// (docs/SPEC-TABLES.md §19.1): each `block` row's image, read through
// table_blockread.moo and rebuilt through table_block.moo's Begin and Write,
// is the golden's bytes, byte for byte.
func TestMooBlockBuilderRebuildsTheGoldens(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_block.moo)")
	t.Parallel()
	g := blockUnit(t)
	rows := manifestLines(t, "block")
	if len(rows) == 0 {
		t.Fatal("the manifest names no block")
	}
	for _, row := range rows {
		data, err := os.ReadFile(filepath.Join(root, row[3]))
		if err != nil {
			t.Fatal(err)
		}
		got := g.run(t, blockBuildMain(g, blockTableOf(row[1]), ""), map[string][]byte{"block": data})
		if got != hex.EncodeToString(data) {
			t.Errorf("%s: the rebuilt block differs from %s:\n%s", row[1], row[3], firstByteDiff(got, data))
		}
	}
	if out := g.analyze(t); out != "" {
		t.Fatalf("moo-analyze is not silent:\n%s", out)
	}
}

func firstByteDiff(got string, want []byte) string {
	h := hex.EncodeToString(want)
	if len(got) != len(h) {
		return fmt.Sprintf("moo built %d bytes, want %d", len(got)/2, len(h)/2)
	}
	for i := 0; i+2 <= len(h); i += 2 {
		if got[i:i+2] != h[i:i+2] {
			return fmt.Sprintf("byte %d: moo %s, want %s", i/2, got[i:i+2], h[i:i+2])
		}
	}
	return "equal"
}

// expectedBegin lays a block out from its counts with the schema's own engine,
// the same walk the generated Go builder runs (ir.BlockExtent chooses each
// array's start; the triple goes into the projection at the field's
// TripleOffset): the prologue and the triples, every other byte zero.
func expectedBegin(bl *ir.BlockLayout, buildVersion uint64, counts []int64) []byte {
	aligns := make([]int64, len(bl.Arrays))
	for i, a := range bl.Arrays {
		aligns[i] = a.ElemAlign()
	}
	starts, used := ir.BlockExtent(bl, aligns, counts)
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

// TestMooBlockBeginMatchesTheLayoutEngine builds each block table at empty,
// one-row, middle and full counts and holds Begin's bytes to ir.BlockExtent's
// walk (the Go builder's), then an over-count and a negative count to the
// refusal naming the array, the count and the maximum.
func TestMooBlockBeginMatchesTheLayoutEngine(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_block.moo)")
	t.Parallel()
	g := blockUnit(t)
	blocks := ir.Blocks(g.unit)
	if blocks == nil || len(blocks.Tables) == 0 {
		t.Fatal("the unit has no block form")
	}
	for _, bl := range blocks.Tables {
		n := len(bl.Arrays)
		for _, shape := range []string{"empty", "one", "middle", "full"} {
			counts := make([]int64, n)
			var lit []string
			for i, a := range bl.Arrays {
				switch shape {
				case "one":
					counts[i] = 1
				case "middle":
					counts[i] = a.Max / 2
				case "full":
					counts[i] = a.Max
				}
				lit = append(lit, fmt.Sprintf("[%q] = %d", a.Field.Name, counts[i]))
			}
			want := hex.EncodeToString(expectedBegin(bl, ir.BuildVersion(g.unit), counts))
			got := g.run(t, blockBuildMain(g, bl.Table.Name, "{ "+strings.Join(lit, ", ")+" }"), nil)
			if got != want {
				t.Errorf("%s %s: Begin's bytes differ from the layout engine's:\nmoo  %.160s\nwant %.160s", bl.Table.Name, shape, got, want)
			}
		}
		for i, a := range bl.Arrays {
			for _, c := range []int64{a.Max + 1, -1} {
				var lit []string
				for j, o := range bl.Arrays {
					v := int64(0)
					if j == i {
						v = c
					}
					lit = append(lit, fmt.Sprintf("[%q] = %d", o.Field.Name, v))
				}
				got := g.run(t, blockBuildMain(g, bl.Table.Name, "{ "+strings.Join(lit, ", ")+" }"), nil)
				want := fmt.Sprintf("refuse %s %d %d", a.Field.Name, c, a.Max)
				if got != want {
					t.Errorf("%s %s=%d: Begin answers %q, want %q", bl.Table.Name, a.Field.Name, c, got, want)
				}
			}
		}
	}
}

// TestMooBlockWriteRefusesWhatItCannotHold: a row count that is not the
// built count, a float that is not finite, and a text past its bound each
// refuse by name, and a clean write of the same value does not.
func TestMooBlockWriteRefusesWhatItCannotHold(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_block.moo)")
	t.Parallel()
	g := blockUnit(t)
	row := manifestLines(t, "block")[0]
	data, err := os.ReadFile(filepath.Join(root, row[3]))
	if err != nil {
		t.Fatal(err)
	}
	table := blockTableOf(row[1])
	script := strings.Replace(blockBuildMain(g, table, ""), "local refusal = W.Write(block, value)", "MUTATE\nlocal refusal = W.Write(block, value)", 1)
	for _, c := range []struct{ name, mutate, want string }{
		{"rows", "value.cameras[2] = value.cameras[1]", "refuse row_count " + table + ".cameras"},
		{"nan", "value.cameras[1].fov = 0x7fc00000", "refuse non_finite_float RenderCamera.fov"},
		{"inf64", "value.cameras[1].position.x = require(\"./table_values\").Uint64.new(0, 0x7ff00000)", "refuse non_finite_float RenderVector3.x"},
		{"clean", "", ""},
	} {
		main := strings.Replace(script, "MUTATE", c.mutate, 1)
		got := g.run(t, main, map[string][]byte{"block": data})
		if c.want == "" {
			if got != hex.EncodeToString(data) {
				t.Errorf("%s: the clean write is not the golden", c.name)
			}
			continue
		}
		if table != "RenderFrame" {
			t.Fatalf("the first block row is %s", table)
		}
		if !strings.HasPrefix(got, c.want) {
			t.Errorf("%s: write answers %q, want %q...", c.name, got, c.want)
		}
	}
}

// TestMooBlockOpenHoldsTheBaseAlignment is §19.2's I5 row for the block read
// (docs/PORTING.md): each image opened at every lead from 0 to 128 opens at the
// aligned leads and refuses unaligned_base at every other, the alignment being
// the last clause (nothing else about the block changes with the lead).
func TestMooBlockOpenHoldsTheBaseAlignment(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_blockread.moo)")
	t.Parallel()
	g := blockUnit(t)
	for _, row := range manifestLines(t, "block") {
		data, err := os.ReadFile(filepath.Join(root, row[3]))
		if err != nil {
			t.Fatal(err)
		}
		var main strings.Builder
		main.WriteString("--!strict\nlocal inputs = require(\"./inputs\")\nlocal B = require(\"./table_blockread\")\n")
		for _, m := range g.tableModules() {
			fmt.Fprintf(&main, "local _%s = require(\"./%s\")\n", m, m)
		}
		table := blockTableOf(row[1])
		mod := ""
		for _, m := range g.tableModules() {
			if strings.Contains(string(g.files[m+".moo"]), "M."+table+"BlockInfo = ") {
				mod = m
			}
		}
		fmt.Fprintf(&main, "local info = _%s.%sBlockInfo\n", mod, table)
		fmt.Fprintf(&main, "for lead = 0, 128 do\n\tlocal moved = buffer.create(lead + %d)\n\tbuffer.copy(moved, lead, inputs.block)\n", len(data))
		fmt.Fprintf(&main, "\tlocal block, why = B.BlockOpen(info, moved, lead, %d)\n\tprint(lead .. \" \" .. (if block ~= nil then \"open\" else tostring(why)))\nend\n", len(data))
		got := strings.Split(g.run(t, main.String(), map[string][]byte{"block": data}), "\n")
		if len(got) != 129 {
			t.Fatalf("%s: %d answers", row[1], len(got))
		}
		for lead, line := range got {
			want := fmt.Sprintf("%d unaligned_base", lead)
			if lead%64 == 0 {
				want = fmt.Sprintf("%d open", lead)
			}
			if line != want {
				t.Errorf("%s: lead %d answers %q, want %q", row[1], lead, line, want)
			}
		}
	}
}

// TestMooBlockBuilderNegativeControls plants one defect per gate into the
// emitted table_block.moo and requires that gate's comparison to go red: the
// array start's rounding removed (the goldens and the layout engine), and the
// write's finite check removed (the NaN refusal).
func TestMooBlockBuilderNegativeControls(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_block.moo)")
	t.Parallel()
	for _, c := range []struct{ name, anchor, repl string }{
		{"align", "\t\t\toffset = roundUp(offset, f.startAlign)\n", "\t\t\toffset = roundUp(offset, 1)\n"},
		{"finite", "\t\tif not V.finite32(v) then\n", "\t\tif false then\n"},
		{"count", "\t\t\tif c < 0 or c > f.bound then\n", "\t\t\tif c < 0 then\n"},
	} {
		g := blockUnit(t)
		path := filepath.Join(g.dir, "table_block.moo")
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(src), c.anchor) != 1 {
			t.Fatalf("%s: the anchor is not in table_block.moo exactly once", c.name)
		}
		if err := os.WriteFile(path, []byte(strings.Replace(string(src), c.anchor, c.repl, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		row := manifestLines(t, "block")[0]
		data, _ := os.ReadFile(filepath.Join(root, row[3]))
		switch c.name {
		case "align":
			// the aligned-by-64 rows already sit on their own start: the
			// padded image carries the one whose start the rounding moves
			red := false
			for _, r := range manifestLines(t, "block") {
				d, _ := os.ReadFile(filepath.Join(root, r[3]))
				if g.run(t, blockBuildMain(g, blockTableOf(r[1]), ""), map[string][]byte{"block": d}) != hex.EncodeToString(d) {
					red = true
				}
			}
			if !red {
				t.Errorf("align: the goldens still rebuild with the array start unrounded")
			}
		case "finite":
			script := strings.Replace(blockBuildMain(g, "RenderFrame", ""), "local refusal = W.Write(block, value)", "value.cameras[1].fov = 0x7fc00000\nlocal refusal = W.Write(block, value)", 1)
			if got := g.run(t, script, map[string][]byte{"block": data}); strings.HasPrefix(got, "refuse non_finite_float") {
				t.Errorf("finite: the NaN still refuses with the check removed")
			}
		case "count":
			got := g.run(t, blockBuildMain(g, "RenderFrame", `{ ["cameras"] = 99999, ["ships"] = 0 }`), nil)
			if strings.HasPrefix(got, "refuse cameras") {
				t.Errorf("count: the over-count still refuses with the bound check removed")
			}
		}
	}
}
