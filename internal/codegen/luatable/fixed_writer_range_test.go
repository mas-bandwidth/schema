package luatable

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.5, §4.6;
// docs/plans/strict-only.md): the shared cross-version vectors every leg reads
// (internal/fixedvector.WriterRange). An OLD writer's file with one ranged
// value forged past that writer's range is read by the NEW reader whose own
// range is wider. Under the one strict read the reader REFUSES the whole load
// (and ReadFixed refuses the single record) as out_of_range — a scalar, one
// element of a ranged fixed array, and an unsigned 64-bit integer at or above
// 2^63 compared unsigned, and a compressed float (docs/rulings.json
// strict-only-read) — matching the C++ reference's corpus manifest.
//
// TestLuaFixedWriterRangeVectorsNegativeControl plants faults in the real
// emitted runtime artifact (fixed.lua) so strict_value falls back to the
// reader's range alone, drops the unsigned comparison, or drops the
// compressed float's writer check, and requires the forged writer-range
// vectors to stop refusing.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestLuaFixedWriterRangeVectors(t *testing.T) {
	slowtest.Gate(t, "lua (it runs the emitted fixed-form runtime over the reference corpus)")
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	corpus := luaFixedCorpus(t, repo)
	for _, vec := range fixedvector.WriterRange {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			if !want.Refused {
				t.Fatalf("the C++ reference landed %s: %+v", vec.File, want)
			}
			older := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "VOLD_"+vec.Row+".schema"))
			newer := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "VNEW_"+vec.Row+".schema"))
			oldEntry, ok := FixedLineageOf(older, vec.Root)
			if !ok {
				t.Fatalf("no fixed root %s in VOLD_%s", vec.Root, vec.Row)
			}
			lineage := map[string][]FixedLineageEntry{vec.Root: {oldEntry}}
			g := luaEvoGen(t, newer, lineage)
			wire, err := os.ReadFile(filepath.Join(corpus, vec.File))
			if err != nil {
				t.Fatal(err)
			}
			if got := g.load(t, vec.Root, wire); got.head != "refused out_of_range" {
				t.Fatalf("%s FixedLoad: got %q (%v), want refused out_of_range", vec.File, got.head, got.dump)
			}
			readMain := fmt.Sprintf(`local T = require(%q)
local I = require("inputs")
local ok, err = pcall(T.ReadFixed, %q, I.file)
if ok then
  print("ok")
else
  print("refused " .. (tostring(err):match("([%%w_]+)$") or tostring(err)))
end
`, g.mod, vec.Root)
			if got := g.run(t, readMain, map[string][]byte{"file": wire}); got != "refused out_of_range" {
				t.Fatalf("%s ReadFixed: got %q, want refused out_of_range", vec.File, got)
			}
			ctl, ok := luaWriterRangeControls[vec.Row]
			if !ok {
				t.Fatalf("no clean-file control for row %s", vec.Row)
			}
			oldClean, err := os.ReadFile(filepath.Join(corpus, "old_"+vec.Row+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			if got := g.load(t, vec.Root, oldClean); got.head != "ok 1 w0 u0" || got.dump[ctl.field] != ctl.old {
				t.Fatalf("old_%s.bin: got %s %v, want ok 1 w0 u0 with %s=%s", vec.Row, got.head, got.dump, ctl.field, ctl.old)
			}
			newClean, err := os.ReadFile(filepath.Join(corpus, "new_"+vec.Row+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			if got := g.load(t, vec.Root, newClean); got.head != "ok 1 w0 u0" || got.dump[ctl.field] != ctl.new {
				t.Fatalf("new_%s.bin: got %s %v, want ok 1 w0 u0 with %s=%s", vec.Row, got.head, got.dump, ctl.field, ctl.new)
			}
		})
	}
}

// luaWriterRangeControls is each writer-range row's clean-file control: the
// field and the value old_<row>.bin (read through the OLD entry, whose range
// is narrower than the reader's) and new_<row>.bin (the reader's own layout)
// each land. A float dumps as its float64 bits: 0.5 and 1.5 for the
// compressed float row, whose old_ value lies inside the writer's [-1, 1].
var luaWriterRangeControls = map[string]struct{ field, old, new string }{
	"range_widen":        {field: "r0.v", old: "100", new: "150"},
	"cfloat_range_widen": {field: "r0.aim", old: "0x3FE0000000000000", new: "0x3FF8000000000000"},
}

func TestLuaFixedWriterRangeVectorsNegativeControl(t *testing.T) {
	slowtest.Gate(t, "lua (it runs the emitted fixed-form runtime over the reference corpus)")
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	corpus := luaFixedCorpus(t, repo)
	for _, tc := range []struct {
		name, row, root, file, anchor, repl, field, val string
	}{
		{
			name:   "range_widen",
			row:    "range_widen",
			root:   "RangeWiden",
			file:   fixedvector.RangeWidenHostile,
			anchor: "    if wr ~= nil then\n      lo, hi = wr.lo, wr.hi\n    end\n",
			repl:   "",
			field:  "r0.v",
			val:    "150",
		},
		{
			name:   "range_widen_array",
			row:    "range_widen",
			root:   "RangeWiden",
			file:   fixedvector.RangeWidenArrayHostile,
			anchor: "    if wr ~= nil then\n      lo, hi = wr.lo, wr.hi\n    end\n",
			repl:   "",
			field:  "r0.vals.3",
			val:    "150",
		},
		{
			name:   "range_widen_u64",
			row:    "range_widen",
			root:   "RangeWiden",
			file:   fixedvector.RangeWidenU64Hostile,
			anchor: "        if math.ult(v, lo) or math.ult(hi, v) then error(\"out_of_range\", 0) end\n",
			repl:   "",
			field:  "r0.u",
			val:    "-9223372036854775801",
		},
		{
			// the compressed float's writer check deleted: the reader's own
			// [-2, 2] alone lands the forged 1.5
			name:   "cfloat_range_widen",
			row:    "cfloat_range_widen",
			root:   "CfloatRangeWiden",
			file:   fixedvector.CfloatRangeWidenHostile,
			anchor: "      if not (x >= wr.flo and x <= wr.fhi) then error(\"out_of_range\", 0) end\n",
			repl:   "",
			field:  "r0.aim",
			val:    "0x3FF8000000000000",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wire, err := os.ReadFile(filepath.Join(corpus, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			older := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "VOLD_"+tc.row+".schema"))
			newer := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "VNEW_"+tc.row+".schema"))
			oldEntry, ok := FixedLineageOf(older, tc.root)
			if !ok {
				t.Fatalf("no fixed root %s in VOLD_%s", tc.root, tc.row)
			}
			g := luaEvoGen(t, newer, map[string][]FixedLineageEntry{tc.root: {oldEntry}})
			g.plant(t, tc.anchor, tc.repl)
			got := g.load(t, tc.root, wire)
			if !strings.HasPrefix(got.head, "ok 1 ") || got.dump[tc.field] != tc.val {
				t.Fatalf("negative control (%s): planted runtime still refused or missed %s=%s: head=%q dump=%v",
					tc.file, tc.field, tc.val, got.head, got.dump)
			}
		})
	}
}
