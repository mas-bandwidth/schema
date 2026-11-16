package main

func init() {
	sabotages["packet-write-cfloat-moo-range"] = []edit{{
		old: `g.refuse(ind, fmt.Sprintf("%s < 0x%08X or %s > 0x%08X", ordered, key(float32Bits(f.FMin)), ordered, key(float32Bits(f.FMax))), "compressed float range")`,
		new: `_ = ordered; _ = key // SABOTAGED`,
	}}
}
