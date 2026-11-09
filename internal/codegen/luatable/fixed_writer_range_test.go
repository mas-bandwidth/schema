package luatable

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.5, §4.6;
// docs/plans/strict-only.md): the shared cross-version vectors every leg reads
// (internal/fixedvector.WriterRange). An OLD writer's file with one ranged
// value forged past that writer's range is read by the NEW reader whose own
// range is wider. Under the one strict read the reader REFUSES the whole load
// (and ReadFixed refuses the single record) as out_of_range — a scalar, one
// element of a ranged fixed array, and an unsigned 64-bit integer at or above
// 2^63 compared unsigned — matching the C++ reference's corpus manifest.
//
// TestLuaFixedWriterRangeVectorsNegativeControl plants faults in the real
// emitted runtime artifact (fixed.lua) so strict_value falls back to the
// reader's range alone or drops the unsigned comparison, and requires the
// forged writer-range vectors to stop refusing.

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
			oldClean, err := os.ReadFile(filepath.Join(corpus, "old_"+vec.Row+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			if got := g.load(t, vec.Root, oldClean); got.head != "ok 1 w0 u0" || got.dump["r0.v"] != "100" {
				t.Fatalf("old_%s.bin: got %s %v, want ok 1 w0 u0 with r0.v=100", vec.Row, got.head, got.dump)
			}
			newClean, err := os.ReadFile(filepath.Join(corpus, "new_"+vec.Row+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			if got := g.load(t, vec.Root, newClean); got.head != "ok 1 w0 u0" || got.dump["r0.v"] != "150" {
				t.Fatalf("new_%s.bin: got %s %v, want ok 1 w0 u0 with r0.v=150", vec.Row, got.head, got.dump)
			}
		})
	}
}

func TestLuaFixedWriterRangeVectorsNegativeControl(t *testing.T) {
	slowtest.Gate(t, "lua (it runs the emitted fixed-form runtime over the reference corpus)")
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	corpus := luaFixedCorpus(t, repo)
	older := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "VOLD_range_widen.schema"))
	newer := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "VNEW_range_widen.schema"))
	oldEntry, ok := FixedLineageOf(older, "RangeWiden")
	if !ok {
		t.Fatal("no fixed root RangeWiden in VOLD_range_widen")
	}
	lineage := map[string][]FixedLineageEntry{"RangeWiden": {oldEntry}}
	for _, tc := range []struct {
		name, file, anchor, repl, field, val string
	}{
		{
			name:   "range_widen",
			file:   fixedvector.RangeWidenHostile,
			anchor: "    if wr ~= nil then\n      lo, hi = wr.lo, wr.hi\n    end\n",
			repl:   "",
			field:  "r0.v",
			val:    "150",
		},
		{
			name:   "range_widen_array",
			file:   fixedvector.RangeWidenArrayHostile,
			anchor: "    if wr ~= nil then\n      lo, hi = wr.lo, wr.hi\n    end\n",
			repl:   "",
			field:  "r0.vals.3",
			val:    "150",
		},
		{
			name:   "range_widen_u64",
			file:   fixedvector.RangeWidenU64Hostile,
			anchor: "        if math.ult(v, lo) or math.ult(hi, v) then error(\"out_of_range\", 0) end\n",
			repl:   "",
			field:  "r0.u",
			val:    "-9223372036854775801",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wire, err := os.ReadFile(filepath.Join(corpus, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			g := luaEvoGen(t, newer, lineage)
			g.plant(t, tc.anchor, tc.repl)
			got := g.load(t, "RangeWiden", wire)
			if !strings.HasPrefix(got.head, "ok 1 ") || got.dump[tc.field] != tc.val {
				t.Fatalf("negative control (%s): planted runtime still refused or missed %s=%s: head=%q dump=%v",
					tc.file, tc.field, tc.val, got.head, got.dump)
			}
		})
	}
}
