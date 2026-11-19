package main

// SPEC §5: the generated packet read ends once, after the top-level object,
// refusing a whole byte left over and non-zero unused bits in the final byte.
// Remove the end check from the emitter; the writer stays as it is, so only
// the trailing-byte and padding verdicts may go red.
func init() {
	sabotages["packet-end-dart-read"] = []edit{{
		old: `	g.bpf("  final endByte = (numBits + 7) >>> 3;\n")
	g.bpf("  if (((bitsRead + 7) >>> 3) != endByte) {\n")
	g.bpf("    return false; // a whole byte is left after the object (SPEC §5)\n")
	g.bpf("  }\n")
	g.bpf("  if (bitsRead < endByte * 8) {\n")
	g.bpf("    final unusedBits = endByte * 8 - bitsRead;\n")
	g.bpf("    final unusedMask = ((1 << unusedBits) - 1) << (bitsRead & 7);\n")
	g.bpf("    if ((view.getUint8(endByte - 1) & unusedMask) != 0) {\n")
	g.bpf("      return false; // non-zero unused bits in the final byte (SPEC §5)\n")
	g.bpf("    }\n")
	g.bpf("  }\n")`,
		new: `	g.bpf("  // SABOTAGED: the packet end check is removed\n")`,
	}}
}
