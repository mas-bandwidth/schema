package main

func init() {
	// Removes the packet writer's string(N) content assert (docs/SPEC.md §5.1
	// row 8): the used bytes must hold no zero byte and be well-formed UTF-8.
	// The read path keeps its validator, so only ill-formed writes go red.
	sabotages["packet-write-text-cs"] = []edit{{
		old: `		if f.Type.Kind == ir.TString {
			g.writeAssert(ind, fmt.Sprintf("TextValid(%s)", g.prefix(name, length)),
				fmt.Sprintf("%s holds a zero byte or is not well-formed UTF-8", name),
				" // the used bytes hold no zero byte and are well-formed UTF-8 (SPEC §5.1 row 8)")
		}
`,
		new: `		// SABOTAGED: no string(N) content check on the packet write
`,
	}}
}
