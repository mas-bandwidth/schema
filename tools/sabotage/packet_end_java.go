package main

// The Java packet read's end check (SPEC §5, docs/PACKET-WIRE-ALGORITHM.md §3),
// removed from the generated one-packet read: a trailing whole byte and nonzero
// unused bits in the final byte must both be accepted, so test/packet-end/java
// reds on the two refused vectors.
func init() {
	sabotages["packet-end-java-read-end"] = []edit{{
		old: `	g.bpf("        if ((((long) end + 7) >>> 3) != (((long) numBits + 7) >>> 3)) {\n            return false;\n        }\n")
	g.bpf("        if ((end & 7) != 0 && (((data[end >>> 3] & 0xff) >>> (end & 7)) != 0)) {\n            return false;\n        }\n")`,
		new: `	// SABOTAGED: the packet end check removed`,
	}}
}
