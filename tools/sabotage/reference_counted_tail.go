package main

func init() {
	sabotages["reference-counted-tail"] = []edit{{old: "g.emitCountedTailReset(f, ind+\"    \")", new: "// SABOTAGED: leave stale backing slots after a repeated array"}}
}
