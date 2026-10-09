//go:build matched

package main

import benchtable "benchtable"

// IN THE MATCHED BUILD THE MEASURED TABLE WIRE IS THE FIXED FORM (form byte 3,
// docs/SPEC-TABLES.md §3.4), exactly as it is on the C++ and C legs. The
// tolerant form still runs every correctness gate it always ran, without a
// clock: the corpus is one corpus, both forms carry the same 64 logical
// records, and a run that stopped loading the tolerant half would report a
// corpus id the paired driver could not match.
func runShape() {
	measured := gateOnly
	gateOnly = true
	benchTable[benchtable.BenchMixed](
		"bench_table", "bench_table", 400000,
		benchtable.BenchMixedReset,
		benchtable.BenchMixedSave,
		func(v *benchtable.BenchMixed, b []byte) bool {
			var report benchtable.TableReport
			return benchtable.BenchMixedLoad(v, b, &report) && !report.Malformed
		})
	gateOnly = measured

	benchFixed[benchtable.FixedTable, benchtable.TableFixedEntry](
		"bench_fixed", 400000,
		benchtable.FixedTableFixedLayout,
		benchtable.FixedTableFixedMeasure,
		benchtable.FixedTableFixedSave,
		func(v []benchtable.FixedTable, b []byte, plan []benchtable.TableFixedEntry) int64 {
			var report benchtable.TableReport
			return benchtable.FixedTableFixedLoad(v, b, plan, &report)
		})
}
