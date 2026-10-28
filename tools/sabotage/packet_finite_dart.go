package main

// SPEC §4.3: a packet float read refuses an all-ones exponent as
// non_finite_float. Remove the raw-bits check at both widths; the write
// contracts stay, so only the read refusals may go red.
func init() {
	sabotages["packet-finite-dart-read"] = []edit{
		{
			old: `	g.pf("%sif ((v & 0x7f800000) == 0x7f800000) {\n", ind)`,
			new: `	g.pf("%sif (v < 0) { // SABOTAGED: no float32 finite check\n", ind)`,
		},
		{
			old: `		g.pf("%sif ((lo & 0x7ff0000000000000) == 0x7ff0000000000000) {\n", ind)`,
			new: `		g.pf("%sif (lo != lo) { // SABOTAGED: no float64 finite check\n", ind)`,
		},
	}
}
