package main

func init() {
	sabotages["packet-write-refusal-java-upper"] = []edit{{
		old: `			g.pf("%sassert %s <= %s;\n", ind, expr, javaIntLit(max))`,
		new: `			g.pf("%sassert %s < %s; // SABOTAGED: upper bound moved\n", ind, expr, javaIntLit(max))`,
	}}
}
