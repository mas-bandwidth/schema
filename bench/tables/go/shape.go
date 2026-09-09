//go:build !matched

package main

import benchtable "benchtable"

func runShape() {
	benchTable[benchtable.TableMixed](
		"bench_table", "bench_table", 400000,
		benchtable.TableMixedReset,
		benchtable.TableMixedSave,
		func(v *benchtable.TableMixed, b []byte) bool {
			var report benchtable.TableReport
			return benchtable.TableMixedLoad(v, b, &report) && !report.Malformed
		})
}
