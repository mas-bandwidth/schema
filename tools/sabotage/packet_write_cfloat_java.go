package main

// The Java packet writer's compressed-float range assert (SPEC §5.1 row 10),
// dropped from checkWrite. A finite value outside [min, max] is then written.
// The finite assert and the integer clamp after rounding stay.
func init() {
	sabotages["packet-write-cfloat-java-range"] = []edit{{
		old: "\t\t\tg.pf(\"%s// inside [min, max] before quantizing (SPEC §5.1 row 10)\\n\", ind)\n" +
			"\t\t\tg.pf(\"%sassert %s >= %sf;\\n\", ind, name, lo)\n" +
			"\t\t\tg.pf(\"%sassert %s <= %sf;\\n\", ind, name, hi)",
		new: "\t\t\tg.pf(\"%s// SABOTAGED: compressed-float range assert removed (%s, %s, %s)\\n\", ind, name, lo, hi)",
	}}
}
