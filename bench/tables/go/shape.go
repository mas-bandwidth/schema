//go:build !matched

package main

import benchtable "benchtable"

var (
	loadRegions [numVariants + 2][bufferSize]byte
	lastTarget  any
	regionSlot  = -1
)

func nextLoadRegion[T any](v *T) []byte {
	if lastTarget != v {
		lastTarget = v
		if regionSlot+1 < len(loadRegions) {
			regionSlot++
		}
	}
	return loadRegions[regionSlot][:]
}

func saveWithoutContext[T any, C any](fn func(*T, []byte, ...C) int64) func(*T, []byte) int64 {
	return func(v *T, b []byte) int64 {
		return fn(v, b)
	}
}

func loadIntoValue[T any](dst *T, loaded *T) bool {
	if loaded == nil {
		return false
	}
	*dst = *loaded
	return true
}

func runShape() {
	benchTable[benchtable.TableMixed](
		"bench_table", "bench_table", 400000,
		benchtable.TableMixedReset,
		saveWithoutContext(benchtable.TableMixedSave),
		func(v *benchtable.TableMixed, b []byte) bool {
			var report benchtable.TableReport
			return loadIntoValue(v, benchtable.TableMixedLoad(nextLoadRegion(v), b, &report))
		})
}
