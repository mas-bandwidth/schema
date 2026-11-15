package main

// Packet end, the moo leg (SPEC §5 "Read termination"): the public one-packet
// read's end call is dropped from the generated code, so the body reads the
// top-level object and accepts whatever follows. The exact packet still reads
// as 3; the trailing byte and the non-zero padding are accepted too, which the
// gate must catch by name.
func init() {
	sabotages["packet-end-moo-no-end"] = []edit{{
		old: `g.pf("\tif not stream:serializeEnd() then\n\t\treturn false\n\tend\n")`,
		new: `g.pf("\t-- SABOTAGED: the end check is gone\n")`,
	}}
}
