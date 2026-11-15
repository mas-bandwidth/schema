package main

func init() {
	// Removes the one-packet end check from the generated C# public read
	// (SPEC §5 "Read termination"): the trailing-byte and final-byte padding
	// verdicts must go red, while the exact packet stays accepted.
	sabotages["packet-end-cs-read"] = []edit{{
		old: `	g.sf("    return stream.SerializeEnd();\n}\n\n")`,
		new: `	g.sf("    return true; // SABOTAGED: no end check\n}\n\n")`,
	}}
}
