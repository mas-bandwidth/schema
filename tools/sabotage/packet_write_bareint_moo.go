package main

func init() {
	// SPEC §5.1 row 11: drop the signed bare intN width check in the moo
	// packet writer, keeping only an integer check of 32-bit magnitude (2^53 - 1 is
	// a literal moo-analyze refuses), so an out-of-width signed value
	// reaches the wire.
	sabotages["packet-write-bareint-moo-signed-width"] = []edit{{
		old: `			g.refuse(ind, fmt.Sprintf("not isInt(%s, -%s, %s)", val, half, new(big.Int).Sub(half, big.NewInt(1))), "")`,
		new: `			g.refuse(ind, fmt.Sprintf("not isInt(%s, -4294967295, 4294967295)", val), "") // SABOTAGED: no width check
			_ = half`,
	}}
}
