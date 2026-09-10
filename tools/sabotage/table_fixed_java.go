// THE FIXED FORM'S JAVA SABOTAGE (docs/SPEC-TABLES.md §3.4), and it moves a
// BYTE rather than removing a check.
//
// The Java leg's strongest claim is that the bytes it writes are the C++
// REFERENCE's, byte for byte, over the reference's own corpus. A byte
// comparison that has never been watched fail could be comparing a file with
// itself, so this writes a text field's length ONE TOO HIGH in the fixed
// writer's template — the one edit that changes a byte in every record of the
// corpus without changing a single offset — and the control requires the leg
// to go red naming the first byte that differs.
package main

func init() {
	sabotages["fixed-form-java-text-length"] = []edit{{
		old: "g.pf(\"%sTableFixed.put32(%s, %s + %d, %s.%sLength);\\n\", ind, buf, at, base, val, name)\n" +
			"\t\tg.pf(\"%sSystem.arraycopy(",
		new: "g.pf(\"%sTableFixed.put32(%s, %s + %d, %s.%sLength + 1);\\n\", ind, buf, at, base, val, name) // SABOTAGED: a text length one too high\n" +
			"\t\tg.pf(\"%sSystem.arraycopy(",
	}}
}
