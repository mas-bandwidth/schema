package main

func init() {
	sabotages["reference-counted-tail"] = []edit{{old: "g.emitCountedTailReset(f, ind+\"    \")", new: "g.pf(\"%s(void)previous_count; // SABOTAGED: leave stale backing slots\\n\", ind)"}}
}
