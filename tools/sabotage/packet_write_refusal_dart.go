package main

// SPEC §5.1 row 1 on the Dart packet Write: a ranged integer is asserted
// inside its range. Remove the upper bound from the scalar's assert; only
// scalar_high may go red.
func init() {
	sabotages["packet-write-refusal-dart-bound"] = []edit{{
		old: `		g.assertRange(name, f.IntMin, f.IntMax, f.IntMax.Cmp(maxInt64) <= 0, ind)
		g.emitWriteOffset(name, f.IntMin, f.IntMax, ind)
		return`,
		new: `		g.assertRange(name, f.IntMin, maxInt64, true, ind) // SABOTAGED: no upper-bound assert
		g.emitWriteOffset(name, f.IntMin, f.IntMax, ind)
		return`,
	}}
}
