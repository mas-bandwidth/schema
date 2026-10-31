package mootable_test

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// manifestLines is every line of the tables manifest of one kind, split.
func manifestLines(t *testing.T, kind string) [][]string {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out [][]string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) > 0 && fields[0] == kind {
			out = append(out, fields)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// blockDumpMoo spells an opened block the way the conformance block-dump
// surface does (test/conformance/go/blockdump.go): the projection's leaves in
// descriptor order, then every out-of-line array row by row, a float as its
// bits — produced from table_blockread.moo's Read, ReadAt and Triple alone.
const blockDumpMoo = `local function text(v: any, used: number): string
	local parts = { "\"" }
	for i = 0, used - 1 do
		local c = if type(v) == "string" then string.byte(v, i + 1) :: number else buffer.readu8(v, i)
		if c >= 0x20 and c < 0x7f and c ~= 34 and c ~= 92 then
			parts[#parts + 1] = string.char(c)
		else
			parts[#parts + 1] = string.format("\\x%02x", c)
		end
	end
	parts[#parts + 1] = "\" len=" .. used
	return table.concat(parts)
end

local function scalar(v: any, kind: number): string
	if kind == 1 then
		return if v then "true" else "false"
	elseif kind == 10 then
		return string.format("0x%08x", v)
	elseif kind == 11 then
		return string.format("0x%08x%08x", v[2], v[1])
	end
	if type(v) == "number" then
		return tostring(v)
	end
	return tostring(v)
end

local lines: { string } = {}
local function dumpRecord(info: any, rec: any, path: string)
	for _, f in info.fields do
		if f.outOfLine then
			continue
		end
		local name = if path == "" then f.name else path .. "." .. f.name
		local v = rec[f.name]
		if f.counted then
			local used = if type(v) == "string" then #v else buffer.len(v)
			lines[#lines + 1] = "  " .. name .. " = " .. text(v, used)
		elseif f.isArray then
			for s = 0, f.arrayBound - 1 do
				local at = name .. "[" .. s .. "]"
				if f.element ~= nil then
					dumpRecord(f.element, v[s + 1], at)
				else
					lines[#lines + 1] = "  " .. at .. " = " .. scalar(v[s + 1], f.kind)
				end
			end
		elseif f.element ~= nil then
			dumpRecord(f.element, v, name)
		else
			lines[#lines + 1] = "  " .. name .. " = " .. scalar(v, f.kind)
		end
		if f.optional then
			lines[#lines + 1] = "  " .. name .. "#present = " .. tostring(rec[f.name .. "#present"])
		end
	end
end

local function dumpBlock(block: any)
	local info = block.info
	local rec, why = B.Read(block)
	if rec == nil then
		print("REFUSED " .. why.name .. " " .. why.field)
		return
	end
	lines[#lines + 1] = "projection " .. info.name .. " @0"
	dumpRecord(info, rec, "")
	for _, f in info.fields do
		if f.outOfLine then
			local offsetOf, count, stride = B.Triple(block, f)
			lines[#lines + 1] = string.format("array %s %s @%d count=%d stride=%d", f.name, f.element.name, offsetOf, count, stride)
			for r = 0, count - 1 do
				local at = offsetOf + r * stride
				lines[#lines + 1] = string.format("row %d @%d", r, at)
				dumpRecord(f.element, rec[f.name][r + 1], "")
			end
		end
	end
	print(table.concat(lines, "\n"))
end
`

// blockMain opens inputs.block (base `lead`, the caller's claim `extent`, or a
// nil buffer) at the named table and prints the dump, or the refusal.
func blockMain(g *generatedUnit, table string, lead, extent int, null bool, dump bool) string {
	var b strings.Builder
	b.WriteString("--!strict\nlocal inputs = require(\"./inputs\")\nlocal B = require(\"./table_blockread\")\n")
	for _, m := range g.tableModules() {
		fmt.Fprintf(&b, "local _%s = require(\"./%s\")\n", m, m)
	}
	b.WriteString(blockDumpMoo)
	mod := ""
	for _, m := range g.tableModules() {
		if strings.Contains(string(g.files[m+".moo"]), "M."+table+"BlockInfo = ") {
			mod = m
		}
	}
	fmt.Fprintf(&b, "local info = _%s.%sBlockInfo\n", mod, table)
	data := "inputs.block"
	if null {
		data = "nil"
	}
	fmt.Fprintf(&b, "local block, why = B.BlockOpen(info, %s, %d, %d)\n", data, lead, extent)
	b.WriteString("if block == nil then print(\"refuse \" .. tostring(why)) return end\n")
	if dump {
		b.WriteString("dumpBlock(block)\n")
	} else {
		b.WriteString("print(\"open\")\n")
	}
	return b.String()
}

// blockTableOf names the block-form table a block fixture is an image of
// (test/conformance/go/blocks.go answers it by the fixture's name).
func blockTableOf(name string) string {
	switch {
	case strings.HasPrefix(name, "block_render"):
		return "RenderFrame"
	case strings.HasPrefix(name, "block_padded"):
		return "PaddedFrame"
	}
	return ""
}

func blockUnit(t *testing.T) *generatedUnit {
	t.Helper()
	units, _ := readManifest(t)
	g := generateUnit(t, units, "blockdemo")
	if g.err != nil {
		t.Fatal(g.err)
	}
	return g
}

// TestMooBlockDumpsMatchTheCorpus is the block form's value gate
// (docs/SPEC-TABLES.md §19.2): every `block` row of the manifest opens through
// table_blockread.moo, and its reflective read, spelled as the block-dump
// surface spells it, equals the pinned dump byte for byte.
func TestMooBlockDumpsMatchTheCorpus(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_blockread.moo)")
	t.Parallel()
	g := blockUnit(t)
	rows := manifestLines(t, "block")
	for _, row := range rows {
		data, err := os.ReadFile(filepath.Join(root, row[3]))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(root, row[4]))
		if err != nil {
			t.Fatal(err)
		}
		got := g.run(t, blockMain(g, blockTableOf(row[1]), 0, len(data), false, true), map[string][]byte{"block": data})
		if got != strings.TrimRight(string(want), "\n") {
			t.Errorf("%s: the moo dump differs from %s:\n%s", row[1], row[4], firstLineDiff(got, string(want)))
		}
	}
	if len(rows) == 0 {
		t.Fatal("the manifest names no block")
	}
	if out := g.analyze(t); out != "" {
		t.Fatalf("moo-analyze is not silent:\n%s", out)
	}
}

func firstLineDiff(got, want string) string {
	a, b := strings.Split(got, "\n"), strings.Split(strings.TrimRight(want, "\n"), "\n")
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y string
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return fmt.Sprintf("line %d: moo %q, want %q", i+1, x, y)
		}
	}
	return "equal"
}

// forgery is one `forgery` row of the manifest.
type forgery struct {
	name, kind, subject, base string
	pointer                   string
	offsets, widths           []int64
	values                    []uint64
	extent                    int64
	verdict                   string
}

func forgeries(t *testing.T, kind string) []forgery {
	t.Helper()
	var out []forgery
	for _, f := range manifestLines(t, "forgery") {
		if f[2] != kind {
			continue
		}
		fg := forgery{name: f[1], kind: f[2], subject: f[3], base: f[4], pointer: f[5], verdict: f[10]}
		for s := range strings.SplitSeq(f[6], ",") {
			v, err := strconv.ParseInt(s, 0, 64)
			if err != nil {
				t.Fatalf("%s: offset %q: %v", fg.name, s, err)
			}
			fg.offsets = append(fg.offsets, v)
		}
		for s := range strings.SplitSeq(f[7], ",") {
			v, _ := strconv.ParseInt(s, 0, 64)
			fg.widths = append(fg.widths, v)
		}
		for s := range strings.SplitSeq(f[8], ",") {
			v, err := strconv.ParseUint(s, 0, 64)
			if err != nil {
				t.Fatalf("%s: value %q: %v", fg.name, s, err)
			}
			fg.values = append(fg.values, v)
		}
		ext, err := strconv.ParseInt(f[9], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		fg.extent = ext
		out = append(out, fg)
	}
	return out
}

// materialize applies a forgery's patches to its base file and places it as
// the conformance harness does (test/conformance/harness/run.go,
// test/conformance/go/blocks.go place): a buffer of exactly the claimed
// extent, lead bytes past an aligned start, what fits copied, the rest zero.
func (fg forgery) materialize(t *testing.T, base []byte) (data []byte, lead, extent int, null bool) {
	t.Helper()
	file := append([]byte(nil), base...)
	for i, off := range fg.offsets {
		if off < 0 {
			continue
		}
		var w [8]byte
		binary.LittleEndian.PutUint64(w[:], fg.values[i])
		copy(file[off:off+fg.widths[i]], w[:fg.widths[i]])
	}
	n := len(file)
	if fg.extent >= 0 {
		n = int(fg.extent)
	}
	if fg.pointer == "null" {
		return nil, 0, n, true
	}
	lead, _ = strconv.Atoi(fg.pointer)
	out := make([]byte, lead+n)
	copy(out[lead:], file)
	return out, lead, n, false
}

func refusalReasons(t *testing.T) map[string]string {
	out := map[string]string{}
	for _, r := range manifestLines(t, "refusal") {
		out[r[1]] = r[2]
	}
	return out
}

// TestMooBlockForgeriesAnswerTheirReasons is §19.2's refusal battery: every
// `block` forgery of the manifest, materialised as the harness does, opens or
// refuses as its verdict says, and a refusal names the reason its `refusal`
// row pins.
func TestMooBlockForgeriesAnswerTheirReasons(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_blockread.moo)")
	t.Parallel()
	g := blockUnit(t)
	reasons := refusalReasons(t)
	fgs := forgeries(t, "block")
	if len(fgs) == 0 {
		t.Fatal("the manifest names no block forgery")
	}
	for _, fg := range fgs {
		base, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", fg.base+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		data, lead, extent, null := fg.materialize(t, base)
		got := g.run(t, blockMain(g, blockTableOf(fg.subject), lead, extent, null, false), map[string][]byte{"block": data})
		want := fg.verdict
		if fg.verdict == "refuse" {
			want = "refuse " + reasons[fg.name]
		}
		if got != want {
			t.Errorf("%s: moo answers %q, want %q", fg.name, got, want)
		}
	}
}

// TestMooBlockNegativeControls plants one defect per gate into the emitted
// table_blockread.moo and requires that gate's comparison to go red: a float
// read one bit off (the dump), and the pitch check removed (block_pitch opens).
func TestMooBlockNegativeControls(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_blockread.moo)")
	t.Parallel()
	for _, c := range []struct{ name, anchor, repl string }{
		{"dump", "\t\treturn bits\n", "\t\treturn bits + 1\n"},
		{"pitch", "\t\t\tif stride ~= f.stride then\n", "\t\t\tif false then\n"},
	} {
		g := blockUnit(t)
		path := filepath.Join(g.dir, "table_blockread.moo")
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(src), c.anchor) != 1 {
			t.Fatalf("%s: the anchor is not in table_blockread.moo exactly once", c.name)
		}
		if err := os.WriteFile(path, []byte(strings.Replace(string(src), c.anchor, c.repl, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		switch c.name {
		case "dump":
			row := manifestLines(t, "block")[0]
			data, _ := os.ReadFile(filepath.Join(root, row[3]))
			want, _ := os.ReadFile(filepath.Join(root, row[4]))
			if got := g.run(t, blockMain(g, blockTableOf(row[1]), 0, len(data), false, true), map[string][]byte{"block": data}); got == strings.TrimRight(string(want), "\n") {
				t.Errorf("dump: the planted float still dumps clean")
			}
		case "pitch":
			for _, fg := range forgeries(t, "block") {
				if fg.name != "block_pitch" {
					continue
				}
				base, _ := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", fg.base+".bin"))
				data, lead, extent, null := fg.materialize(t, base)
				if got := g.run(t, blockMain(g, blockTableOf(fg.subject), lead, extent, null, false), map[string][]byte{"block": data}); got == "refuse bad_layout" {
					t.Errorf("pitch: block_pitch still refuses as bad_layout with the check removed")
				}
			}
		}
	}
}
