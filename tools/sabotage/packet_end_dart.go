package main

// SPEC §5: the generated packet read ends once, after the top-level object,
// refusing a whole byte left over and non-zero unused bits in the final byte.
// Remove the end check from the emitter; the writer stays as it is, so only
// the trailing-byte and padding verdicts may go red.
func init() {
	sabotages["packet-end-dart-read"] = []edit{{
		old: `	g.pf("  final endByte = (numBits + 7) >>> 3;\n")
	g.pf("  if (((bitsRead + 7) >>> 3) != endByte) {\n")
	g.pf("    return false; // a whole byte is left after the object (SPEC §5)\n")
	g.pf("  }\n")
	g.pf("  if (bitsRead < endByte * 8) {\n")
	g.pf("    final unusedBits = endByte * 8 - bitsRead;\n")
	g.pf("    final unusedMask = ((1 << unusedBits) - 1) << (bitsRead & 7);\n")
	g.pf("    if ((view.getUint8(endByte - 1) & unusedMask) != 0) {\n")
	g.pf("      return false; // non-zero unused bits in the final byte (SPEC §5)\n")
	g.pf("    }\n")
	g.pf("  }\n")`,
		new: `	g.pf("  // SABOTAGED: the packet end check is removed\n")`,
	}}
}
