package main

func init() {
	sabotages["packet-write-refusal-rust-upper-bound"] = []edit{{
		old: `g.writeAssert(ind, fmt.Sprintf("%s >= %s && %s <= %s", name, lo, name, hi), msg)`,
		new: `g.writeAssert(ind, fmt.Sprintf("%s >= %s && %s < %s /* SABOTAGED */", name, lo, name, hi), msg)`,
	}}
}
