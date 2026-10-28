package main

// the Java packet reader's finite-float check (SPEC §4.3), dropped at both
// widths: test/java/FiniteMain.java must red on its non_finite_float read
// vectors while the write contracts and the bit-exact round trips stay green
func init() {
	sabotages["packet-finite-java-read-check"] = []edit{{
		old: `		g.pf("%sif ((v & 0x7f800000L) == 0x7f800000L) {\n%s    return -1; // non_finite_float: NaN or an infinity (SPEC §4.3)\n%s}\n", ind, ind, ind)`,
		new: `		// SABOTAGED: the float32 read takes every pattern`,
	}, {
		old: `		g.pf("%sif ((lo & 0x7ff0000000000000L) == 0x7ff0000000000000L) {\n%s    return -1; // non_finite_float: NaN or an infinity (SPEC §4.3)\n%s}\n", ind, ind, ind)`,
		new: `		// SABOTAGED: the float64 read takes every pattern`,
	}}
}
