package main

func init() {
	// SPEC §5.1 row 11: drop the bare intN/uintN width check, keeping only
	// the integer check, so an out-of-width value reaches the wire.
	sabotages["packet-write-bareint-squirrel-width"] = []edit{{
		old: `			emitBareIntGuard(m, val, t, indent)`,
		new: `			m.Printf("%sif (typeof %s != \"integer\") return false; // SABOTAGED: no width check\n", indent, val)`,
	}}
}
