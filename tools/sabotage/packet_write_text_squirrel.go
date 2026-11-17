package main

func init() {
	// Removes the write-side text check of SPEC §5.1 row 8 from the Squirrel
	// packet writer: the used bytes of string(N) may then hold a zero byte or
	// malformed UTF-8. The length guard stays, so only the ill-formed text
	// cases may go red.
	sabotages["packet-write-text-squirrel-write"] = []edit{{
		old: `m.Printf("%sif (!%s(%s, %s)) return false;\n", indent, m.Helper(validText), val, length)`,
		new: `m.Printf("%sif (false && !%s(%s, %s)) return false; // SABOTAGED\n", indent, m.Helper(validText), val, length)`,
	}}
}
