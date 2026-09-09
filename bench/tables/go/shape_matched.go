//go:build matched

package main

import benchtable "benchtable"

func runShape() {
	benchTable[benchtable.BenchMixed](
		"bench_table", "bench_table", 400000,
		benchtable.BenchMixedReset,
		benchtable.BenchMixedSave,
		func(v *benchtable.BenchMixed, b []byte) bool {
			var report benchtable.TableReport
			return benchtable.BenchMixedLoad(v, b, &report) && !report.Malformed
		})
}
