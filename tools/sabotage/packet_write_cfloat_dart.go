package main

// SPEC §5.1 row 10 on the packet Write: a compressed float is checked finite
// and inside [min, max] before quantizing. Remove the range check from the
// generated writer; the finite check stays, so only the finite off-range
// cases may go red.
func init() {
	sabotages["packet-write-cfloat-dart-range"] = []edit{{
		old: `	g.pf("%s  assert(x >= %s && x <= %s); // SPEC §5.1 row 10\n", ind, f32lit(float64(minF)), f32lit(float64(maxF)))`,
		new: `	// SABOTAGED: the compressed float's value range is not checked`,
	}}
}
