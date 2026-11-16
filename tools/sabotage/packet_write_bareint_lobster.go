package main

func init() {
	// Removes the signed narrow bare-integer width assert from the packet
	// writer (SPEC §5.1 row 11). The same-width mask stays, so an out-of-width
	// value still rides the wire as its low bits, exactly the fault the gate
	// catches.
	sabotages["packet-write-bareint-lobster-width"] = []edit{{
		old: `		lo := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), uint(width-1)))
		hi := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(width-1)), big.NewInt(1))
		g.Pf("%sassert %s >= %s and %s <= %s // the value fits the declared %d-bit width (SPEC §5.1 row 11)\n",
			indent, val, IntLit(lo), val, IntLit(hi), width)`,
		new: `		// SABOTAGED: no signed narrow bare-integer width check`,
	}}
}
