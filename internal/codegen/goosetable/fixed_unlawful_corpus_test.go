package goosetable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedunlawful"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// gooseUnlawfulAnswer is the Goose leg's answer to one case of the shared
// unlawful-lineage corpus (internal/fixedunlawful). The poison is the leg's
// own (goosePoison: every leaf of the NEW type 0x5A, every count and length
// at its bound), laid before the first read and again before the second, and
// the record image of every row handed in is held to a fresh value's. The
// answer adds the declared defaults read field by field off the VNEW schema,
// printed after the second refusal: `lead = 1`, `trail = 2`, and the case's
// own field at its type's default.
type gooseUnlawfulAnswer struct {
	// defaults maps a Goose expression over row 0 to the value it prints
	// when the row holds its declared defaults.
	defaults map[string]string
	// values, for the LAWFUL control only, lists the three Goose expressions
	// over row 0 the verdict is built from, in the case's Values order.
	values []string
}

func gooseUnlawfulDefaults(extra map[string]string) gooseUnlawfulAnswer {
	d := map[string]string{"values[0].Lead": "1", "values[0].Trail": "2"}
	for k, v := range extra {
		d[k] = v
	}
	return gooseUnlawfulAnswer{defaults: d}
}

var gooseUnlawfulAnswers = map[string]gooseUnlawfulAnswer{
	"unknown_census":      gooseUnlawfulDefaults(map[string]string{"values[0].Items[0].A": "0", "values[0].Items[3].A": "0"}),
	"unknown_census_many": gooseUnlawfulDefaults(map[string]string{"values[0].Items[0].A": "0", "values[2].Items[3].A": "0", "values[2].Lead": "1"}),
	"kind_moved":          gooseUnlawfulDefaults(map[string]string{"(if values[0].V == 0.0 { 1 } else { 0 })": "1"}),
	"arm_removed":         gooseUnlawfulDefaults(map[string]string{"values[0].Pick.Type": "0"}),
	"variant_removed":     gooseUnlawfulDefaults(nil),
	"lawful_control":      {values: []string{"values[0].Lead", "values[0].V", "values[0].Trail"}},
	"key_removed":         gooseUnlawfulDefaults(map[string]string{"values[0].Slots[0]": "0", "values[0].Slots[1]": "0"}),
	"array_shrunk":        gooseUnlawfulDefaults(map[string]string{"values[0].Vals[0]": "0", "values[0].Vals[1]": "0"}),
	"counted_shrunk":      gooseUnlawfulDefaults(map[string]string{"values[0].ValsCount": "0", "values[0].Vals[0]": "0"}),
	"text_narrowed":       gooseUnlawfulDefaults(map[string]string{"values[0].TextLength": "0", "values[0].Text[0]": "0"}),
	"bytes_narrowed":      gooseUnlawfulDefaults(map[string]string{"values[0].BlobLength": "0", "values[0].Blob[0]": "0"}),
}

// TestGooseFixedUnlawfulCorpus is the Goose leg's driver for the shared
// unlawful-lineage corpus. Per case: the NEW build, handed the OLD layout as a
// lineage entry, refuses the OLD file before any record, every counter zero,
// every poisoned row back at its declared defaults, on a first read and a
// second (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH/EMIT, §5.4, §5.9 #30;
// SPEC-TABLES §6.5). The refusal's NAME is pinned here, layout_malformed (the
// entry's plan is never laid, so the lineage answers it by name), and is never
// compared across legs (refusal-verdict-only). Goose refuses a hostile load in
// every build.
func TestGooseFixedUnlawfulCorpus(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	fixedunlawful.Run(t, "goosetable", gooseUnlawfulAnswers, func(t *testing.T, c fixedunlawful.Case, a gooseUnlawfulAnswer) string {
		t.Parallel()
		older := evolutionLoad(t, "test/tables/"+c.Old+".schema")
		newer := evolutionLoad(t, "test/tables/"+c.New+".schema")
		data := gooseCorpusFile(t, corpus, c.File)
		if got := gooseFixedRecords(t, data, older, c.Root); got != c.Records {
			t.Fatalf("%s: %s holds %d records under the OLD entry, the corpus says %d", c.Name, c.File, got, c.Records)
		}
		if c.Lawful {
			// THE LAWFUL CONTROL: the NEW build, handed the OLD layout, LOADS
			// the file. The rows are poisoned first (every leaf 0x5A), so a
			// load that leaves a row alone is seen, and the verdict is built
			// from the three values the probe PRINTED off row 0 after the load.
			got := gooseVersionProbe(t, gooseVersionCase{reader: newer, older: []*ir.Unit{older}, root: c.Root,
				capacity: c.Records, poison: true, data: data, print: a.values})
			got.file = c.Name
			got.reads(t, c.Records)
			if got.clamped != 0 || got.unknown != 0 || got.kindMismatch != 0 { // widened is 1: the widening is counted
				t.Fatalf("%s: the lawful control moved a counter it owes zero: %s", c.Name, got.raw)
			}
			if len(got.printed) != len(a.values) {
				t.Fatalf("%s: %d values printed for %d asked: %s", c.Name, len(got.printed), len(a.values), got.raw)
			}
			vals := fmt.Sprintf("lead=%s v=%s trail=%s", strings.TrimSpace(got.printed[0]), strings.TrimSpace(got.printed[1]), strings.TrimSpace(got.printed[2]))
			return fixedunlawful.Loaded(got.n, vals)
		}
		var exprs []string
		for e := range a.defaults {
			exprs = append(exprs, e)
		}
		// capacity past the file's records: the rows the file never reaches
		// are handed in too, and a refusal resets them as well (R4)
		got := gooseVersionProbe(t, gooseVersionCase{reader: newer, older: []*ir.Unit{older}, root: c.Root,
			capacity: c.Records + 2, poison: true, fresh: true, twice: true, data: data, print: exprs})
		got.file = c.Name
		// THE LEG'S OWN NAME for an unlawful entry, and the shared verdict:
		// -1, no counter, malformed false, every row at its defaults
		got.refuses(t, "layout_malformed", false, 0)
		if want := 2 * (c.Records + 2); len(got.dests) != want {
			t.Fatalf("%s: %d row images after the two reads, owed %d: %s", c.Name, len(got.dests), want, got.raw)
		}
		if got.secondUnknown != 0 || got.secondCounters != "0 0 0 0" {
			t.Fatalf("%s: the second refusal moved a counter (unknown %d, oor widened mismatch damaged %s): %s",
				c.Name, got.secondUnknown, got.secondCounters, got.raw)
		}
		if len(got.printed) != len(exprs) {
			t.Fatalf("%s: %d defaults printed for %d asked: %s", c.Name, len(got.printed), len(exprs), got.raw)
		}
		for i, e := range exprs {
			if v := strings.TrimSpace(got.printed[i]); v != a.defaults[e] {
				t.Fatalf("%s: after the second refusal %s is %s, its declared default is %s", c.Name, e, v, a.defaults[e])
			}
		}
		return fixedunlawful.Refused
	})
}
