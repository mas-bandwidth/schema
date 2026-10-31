package luatable

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE LUA FORM OF THE ACCESSOR AND DESCRIPTOR AGREEMENT GATE (docs/PORTING.md
// M11 and J1; roadmap port/lua/open/accessor-agreement).
//
// WHAT THE LUA LEG HAS INSTEAD OF A GENERATED ACCESSOR. The compiled legs emit,
// per record, an accessor at a baked-in offset and a descriptor beside it, and
// J1 holds the two against each other so that a moved accessor or a moved
// descriptor offset is seen without a pinned dump that happens to cover the
// field. Lua generates no accessor: a block row and a cook node are read by
// the one reflective walk (runtime/block.lua, runtime/uncook.lua) at offsets it
// takes from the descriptors, so there is no second path to drift from the
// descriptor, and "the accessor agrees with the descriptor" holds by
// construction. What can still be wrong is the layout itself, and it has TWO
// INDEPENDENT DERIVATIONS here, exactly the pair J1 exists to hold together:
//
//   - the compiler's own model (ir.Blocks, ir.RecordLayout, ir.FieldPieces),
//     the one the C++ static_asserts, the C# checks and the build version come
//     from, computed in Go; and
//   - the Lua runtime's: runtime/block.lua's B.Layout derives every record,
//     field, piece and array triple from the descriptors by its OWN arithmetic
//     (align_up, the pieces of a field), and the cook's field offsets are the
//     descriptors' `at`, emitted as data.
//
// This gate dumps the Lua derivation for EVERY block projection, EVERY block row
// and EVERY cook record of EVERY unit the corpus names, and requires it line
// for line equal to the compiler's. A moved offset, a mis-sized piece, a wrong
// alignment or a shifted triple is a named line, whether or not any pinned
// instance covers the field. The pinned dumps (TestLuaBlockRead, TestLuaCookRead)
// stay: they hold the VALUES read at those offsets.
//
// REVERSED WITNESSES (TestLuaLayoutAgreementNegativeControl): a scalar's block
// offset moved four bytes in B.Layout (the J1 accessor control's shape) must
// red on that field's lines and on nothing else; a cook pointer slot's `at`
// moved eight in the emitted descriptor (the slot control's shape) must red on
// that one field.

// luaLayoutProbe prints the Lua derivation. The request file names the block
// projections (one per line, "proj <Name>"), the block rows ("row <Name>") and
// the cook records ("cook <Name>") to dump.
const luaLayoutProbe = `local T = require(arg[2])
local B = require("block")

local function pieces(fl)
  local out = {}
  for i, p in ipairs(fl.pieces) do out[i] = p.offset .. ":" .. p.size end
  return table.concat(out, ",")
end

local lines = {}
local function out(...) lines[#lines + 1] = table.concat({ ... }, " ") end

for line in io.lines(arg[1]) do
  local kind, name = line:match("^(%S+) (%S+)$")
  if kind == "proj" or kind == "row" then
    local L = B.Layout(T, name, kind == "proj")
    out("rec", kind, name, L.size, L.align)
    for _, fl in ipairs(L.fields) do
      out("fld", kind, name, fl.f.name, fl.offset, fl.size, fl.align, pieces(fl))
      if fl.outOfLine then
        local row = B.Layout(T, fl.f.elem.target, false)
        out("arr", name, fl.f.name, fl.offset, fl.countOffset, fl.offset + 12, row.size)
      end
    end
  elseif kind == "cook" then
    local d = T.Tables[name]
    out("rec", "cook", name, d.size, d.align)
    for _, f in ipairs(d.fields) do
      out("fld", "cook", name, f.name, f.at)
    end
  end
end
print(table.concat(lines, "\n"))
`

// luaLayoutExpected is the compiler's derivation of the same lines for one unit,
// and the request the probe answers.
func luaLayoutExpected(u *ir.Unit) (request string, lines []string) {
	var req []string
	bu := ir.Blocks(u)
	pieces := func(u *ir.Unit, f *ir.Field, off int64, projection bool) string {
		var parts []string
		for _, p := range ir.BlockFieldPieceOffsets(u, f, off, projection) {
			parts = append(parts, fmt.Sprintf("%d:%d", p.Offset, p.Size))
		}
		return strings.Join(parts, ",")
	}
	for _, bl := range bu.Tables {
		name := bl.Table.Name
		req = append(req, "proj "+name)
		lines = append(lines, fmt.Sprintf("rec proj %s %d %d", name, bl.Projection.Size, bl.Projection.Align))
		for _, fl := range bl.Projection.Fields {
			lines = append(lines, fmt.Sprintf("fld proj %s %s %d %d %d %s", name, fl.Field.Name, fl.Offset, fl.Size, fl.Align, pieces(u, fl.Field, fl.Offset, true)))
		}
		for _, a := range bl.Arrays {
			lines = append(lines, fmt.Sprintf("arr %s %s %d %d %d %d", name, a.Field.Name, a.OffsetOfOffset, a.CountOffset, a.StrideOffset, a.Stride))
		}
	}
	for _, name := range bu.Order {
		m := bu.Layout(name)
		req = append(req, "row "+name)
		lines = append(lines, fmt.Sprintf("rec row %s %d %d", name, m.Size, m.Align))
		for _, fl := range m.Fields {
			lines = append(lines, fmt.Sprintf("fld row %s %s %d %d %d %s", name, fl.Field.Name, fl.Offset, fl.Size, fl.Align, pieces(u, fl.Field, fl.Offset, false)))
		}
	}
	names := make([]string, 0, len(u.Tables))
	for name := range u.Tables {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		m := ir.RecordLayout(u, u.Tables[name])
		req = append(req, "cook "+name)
		lines = append(lines, fmt.Sprintf("rec cook %s %d %d", name, m.Size, m.Align))
		for _, fl := range m.Fields {
			lines = append(lines, fmt.Sprintf("fld cook %s %s %d", name, fl.Field.Name, fl.Offset))
		}
	}
	return strings.Join(req, "\n") + "\n", lines
}

// luaLayoutRun dumps the Lua derivation for one unit with an optional edit of
// the emitted files, and returns the compiler's lines and the Lua ones.
func luaLayoutRun(t *testing.T, u *ir.Unit, edit func(files map[string][]byte)) (want, got []string) {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	req, want := luaLayoutExpected(u)
	if err := os.WriteFile(filepath.Join(dir, "request.txt"), []byte(req), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(luaLayoutProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := luaFuzzCommand(t, dir, "request.txt", ModuleName(u))
	res, err := cmd.Output()
	if err != nil {
		t.Fatalf("the layout probe failed: %v\n%s", err, res)
	}
	return want, strings.Split(strings.TrimRight(string(res), "\n"), "\n")
}

// luaLayoutDiff names the lines of one derivation the other lacks.
func luaLayoutDiff(want, got []string) []string {
	have := map[string]int{}
	for _, l := range got {
		have[l]++
	}
	var out []string
	for _, l := range want {
		if have[l] == 0 {
			out = append(out, "compiler "+l)
		} else {
			have[l]--
		}
	}
	for l, n := range have {
		if n > 0 {
			out = append(out, "lua      "+l)
		}
	}
	sort.Strings(out)
	return out
}

// luaLayoutUnits loads every unit of the corpus the Lua backend generates.
func luaLayoutUnits(t *testing.T) map[string]*ir.Unit {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	units := manifestUnits(t, filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	out := map[string]*ir.Unit{}
	for key, paths := range units {
		var full []string
		for _, p := range paths {
			full = append(full, filepath.Join(repo, p))
		}
		u := luarun.LoadUnit(t, full...)
		if _, err := Generate(u); err != nil {
			continue // a unit the target refuses by name has no layout to hold
		}
		out[key] = u
	}
	if len(out) == 0 {
		t.Fatal("no unit of the corpus is one the Lua backend generates")
	}
	return out
}

func TestLuaLayoutAgreesWithTheCompiler(t *testing.T) {
	t.Parallel()
	units := luaLayoutUnits(t)
	blocks, cooks := 0, 0
	for key, u := range units {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			want, got := luaLayoutRun(t, u, nil)
			if d := luaLayoutDiff(want, got); len(d) > 0 {
				k := min(len(d), 12)
				t.Fatalf("%d layout line(s) differ between the compiler's model and the Lua runtime's derivation; first %d:\n%s", len(d), k, strings.Join(d[:k], "\n"))
			}
		})
		bu := ir.Blocks(u)
		blocks += len(bu.Tables)
		cooks += len(u.Tables)
	}
	if blocks == 0 || cooks == 0 {
		t.Fatalf("the gate held %d block projections and %d cook records: it is watching nothing", blocks, cooks)
	}
	t.Logf("layout agreement: %d units, %d block projections, %d cook records", len(units), blocks, cooks)
}

// luaLayoutEdit replaces the one occurrence of old in files[name].
func luaLayoutEdit(t *testing.T, files map[string][]byte, name, old, repl string) {
	t.Helper()
	src, ok := files[name]
	if !ok {
		t.Fatalf("the emitted unit has no %s", name)
	}
	if c := bytes.Count(src, []byte(old)); c != 1 {
		t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage anchor appears %d times in %s, want 1: %q", c, name, old)
	}
	files[name] = bytes.Replace(src, []byte(old), []byte(repl), 1)
}

// TestLuaLayoutAgreementNegativeControl is the gate's reversed witness.
//
// The accessor control moves ONE scalar's block offset four bytes in B.Layout
// (RenderCamera.camera_id, the field the Dart leg's control moves): the gate
// must red on that field's `fld` lines, and on no other record's. The slot
// control moves ONE cook pointer slot's descriptor offset eight (TreeNode.left,
// the Dart leg's slot control): the gate must red on that one line.
func TestLuaLayoutAgreementNegativeControl(t *testing.T) {
	t.Parallel()
	units := luaLayoutUnits(t)
	t.Run("a block scalar moved four bytes", func(t *testing.T) {
		t.Parallel()
		u := units["blockdemo"]
		if u == nil {
			t.Fatal("the corpus has no blockdemo unit")
		}
		want, got := luaLayoutRun(t, u, func(files map[string][]byte) {
			luaLayoutEdit(t, files, "block.lua", "      f = f,\n      offset = start,\n", "      f = f,\n      offset = start + ((f.name == \"camera_id\") and 4 or 0), -- SABOTAGED\n")
		})
		d := luaLayoutDiff(want, got)
		if len(d) == 0 {
			t.Fatal("NEGATIVE CONTROL FAILED: a block scalar moved four bytes left the layout gate green")
		}
		for _, l := range d {
			if !strings.Contains(l, " camera_id ") {
				t.Errorf("the gate went red on a line that is not the moved field: %s", l)
			}
		}
		t.Logf("red as required on %d line(s), all RenderCamera.camera_id", len(d))
	})
	t.Run("a cook slot moved eight bytes", func(t *testing.T) {
		t.Parallel()
		u := units["graphdemo"]
		if u == nil {
			t.Fatal("the corpus has no graphdemo unit")
		}
		at := luaLayoutFieldAt(t, u, "TreeNode", "left")
		want, got := luaLayoutRun(t, u, func(files map[string][]byte) {
			module := ModuleName(u) + ".lua"
			src := string(files[module])
			from := strings.Index(src, `_M.Tables["TreeNode"]`)
			if from < 0 {
				t.Fatal("the emitted unit has no TreeNode descriptor")
			}
			rest := src[from:]
			anchor := `name = "left"`
			i := strings.Index(rest, anchor)
			j := strings.Index(rest[i:], "at = "+strconv.FormatInt(at, 10)+",")
			if i < 0 || j < 0 {
				t.Fatalf("NEGATIVE CONTROL FAILED: no `at = %d` after TreeNode.left in the emitted descriptor", at)
			}
			patched := rest[:i+j] + "at = " + strconv.FormatInt(at+8, 10) + ", -- SABOTAGED" + rest[i+j+len("at = "+strconv.FormatInt(at, 10)+","):]
			files[module] = []byte(src[:from] + patched)
		})
		d := luaLayoutDiff(want, got)
		if len(d) == 0 {
			t.Fatal("NEGATIVE CONTROL FAILED: a cook slot moved eight bytes left the layout gate green")
		}
		for _, l := range d {
			if !strings.Contains(l, "fld cook TreeNode left") {
				t.Errorf("the gate went red on a line that is not the moved slot: %s", l)
			}
		}
		t.Logf("red as required on %d line(s), all TreeNode.left", len(d))
	})
}

// luaLayoutFieldAt is the compiler's offset of one cook field.
func luaLayoutFieldAt(t *testing.T, u *ir.Unit, record, field string) int64 {
	t.Helper()
	st := u.Tables[record]
	if st == nil {
		t.Fatalf("the unit has no table %s", record)
	}
	fl := ir.RecordLayout(u, st).FieldByName(field)
	if fl == nil {
		t.Fatalf("%s has no field %s", record, field)
	}
	return fl.Offset
}
