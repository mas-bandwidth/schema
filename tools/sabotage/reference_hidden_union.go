package main

func init() {
	sabotages["reference-hidden-union-extent"] = []edit{{old: "g.emitUnreachedUnionExtentRefusal(f, ev)", new: "// SABOTAGED: hidden union containers never enter the extent refusal"}}
}
