package main

func init() {
	sabotages["packet-write-text-js-write"] = []edit{{
		old: `	pf("%sif (!%s(%s, %s)) {\n%s  return %s;\n%s}\n", ind, utf8Helper, name, length, ind, refusal, ind)`,
		new: `	// SABOTAGED: the write-side text check is not emitted`,
	}}
}
