package main

// Remove only the generated Squirrel compressed-float range refusal. The
// shared packet-write-cfloat cases then show the writer throwing for
// non-finite inputs or accepting finite off-range values.
func init() {
	sabotages["packet-write-cfloat-squirrel"] = []edit{{
		old: "\t\t\tm.Printf(\"%sif (%s != %s || %s < %s || %s > %s) return false; // SPEC §5.1 row 10\\n\",\n\t\t\t\tindent, val, val, val, Float32Lit(f.FMin), val, Float32Lit(f.FMax))\n",
		new: "\t\t\t// SABOTAGED: compressed-float declaration range refusal removed\n",
	}}
}
