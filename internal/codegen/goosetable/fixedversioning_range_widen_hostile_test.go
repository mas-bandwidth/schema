package goosetable

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.5), the
// shared cross-version vectors every leg reads (fixedvector.WriterRange): an
// OLD writer's file with one ranged value forged past that writer's range, read
// by the NEW reader whose own range is wider. The reader clamps to the WRITER's
// range: a scalar, one element of a ranged fixed array (held the same as a
// scalar), and a uint64 at or above 2^63 (compared unsigned, so it lands the
// max). The clamp count is the retired manifest field, zero on every dumped
// line (fixedvector.Expect.Clamped). The landed values are the manifest's
// `expect=`, which is what the C++ reference's own NEW reader landed.
func TestGooseFixedVersioningRangeWidenHostile(t *testing.T) {
	slowtest.Gate(t, "goose (it runs the generated NEW reader over the forged file)")
	t.Parallel()
	corpus := fixedCorpus(t)
	for _, vec := range fixedvector.WriterRange {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			leaves, err := want.Leaves()
			if err != nil {
				t.Fatal(err)
			}
			var print []string
			for _, l := range leaves {
				expr := "values[0]." + strings.ToUpper(l.Field[:1]) + l.Field[1:]
				if l.Index >= 0 {
					expr += fmt.Sprintf("[%d]", l.Index)
				}
				print = append(print, expr)
			}
			older := evolutionLoad(t, "test/tables/VOLD_"+vec.Row+".schema")
			newer := evolutionLoad(t, "test/tables/VNEW_"+vec.Row+".schema")
			mustRefuse := want.Refused || want.Clamped > 0
			got := gooseVersionProbe(t, gooseVersionCase{reader: newer, older: []*ir.Unit{older}, root: vec.Root,
				capacity: 1, poison: mustRefuse, fresh: mustRefuse, data: gooseCorpusFile(t, corpus, vec.File), print: print})
			if mustRefuse {
				got.refuses(t, "out_of_range", false, 0)
				return
			}
			got.reads(t, 1)
			if len(got.printed) != len(leaves) {
				t.Fatalf("printed %v, want %d values", got.printed, len(leaves))
			}
			for k, l := range leaves {
				if got.printed[k] != strconv.FormatInt(l.Value, 10) {
					t.Errorf("%s landed %s, want %d", l.Path, got.printed[k], l.Value)
				}
			}
		})
		// THE REFUSAL IS REACHED, NOT ASSUMED (R4): none of the three vectors refuses on
		// its own (the clamp lands), so a copy with record 0's per-record hash forged
		// refuses no_layout, and every row handed in (4, all poisoned) must be back at
		// its declared defaults. Old code left the poison in rows 0..k.
		t.Run(vec.Name+"_refused", func(t *testing.T) {
			t.Parallel()
			data := append([]byte(nil), gooseCorpusFile(t, corpus, vec.File)...)
			lb := int(uint32(data[16]) | uint32(data[17])<<8 | uint32(data[18])<<16 | uint32(data[19])<<24)
			for i := range 8 {
				data[20+lb+i] = ^data[20+lb+i]
			}
			older := evolutionLoad(t, "test/tables/VOLD_"+vec.Row+".schema")
			newer := evolutionLoad(t, "test/tables/VNEW_"+vec.Row+".schema")
			got := gooseVersionProbe(t, gooseVersionCase{reader: newer, older: []*ir.Unit{older}, root: vec.Root,
				capacity: 4, poison: true, fresh: true, data: data})
			got.refuses(t, "no_layout", false, 0)
		})
	}
}
