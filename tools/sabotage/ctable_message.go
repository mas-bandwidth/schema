package main

func init() {
	sabotages["message-c-wrong-slot"] = []edit{{
		old: `	g.pf("%stable_bit_put(w,%d,kTableMessageRefBitsHere);\n", ind, g.messageSlot(e))`,
		new: `	g.pf("%stable_bit_put(w,%d,kTableMessageRefBitsHere);\n", ind, g.messageSlot(e)+1) // SABOTAGED: a different vocabulary slot`,
	}}
}
