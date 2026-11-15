package java

// emitUTF8 validates the used bytes in place, with no decoder or allocation.
// The scalar ranges are Unicode Table 3-7, as in the C++ reference. refused is
// the enclosing function's refusal form: the read returns its verdict, the
// write predicate returns false to fail its assert.
func (g *gen) emitUTF8(name, length, ind, refused string) {
	g.pf("%sfor (int utf8Index = 0; utf8Index < %s;)\n%s{\n", ind, length, ind)
	g.pf("%s    int utf8Lead = %s[utf8Index++] & 0xFF;\n", ind, name)
	g.pf("%s    if (utf8Lead < 0x80) continue;\n", ind)
	g.pf("%s    int utf8Remaining, utf8Point;\n", ind)
	g.pf("%s    if ((utf8Lead & 0xE0) == 0xC0) { utf8Remaining = 1; utf8Point = utf8Lead & 0x1F; }\n", ind)
	g.pf("%s    else if ((utf8Lead & 0xF0) == 0xE0) { utf8Remaining = 2; utf8Point = utf8Lead & 0x0F; }\n", ind)
	g.pf("%s    else if ((utf8Lead & 0xF8) == 0xF0) { utf8Remaining = 3; utf8Point = utf8Lead & 0x07; }\n", ind)
	g.pf("%s    else %s; // malformed UTF-8 lead (SPEC §4.7)\n", ind, refused)
	g.pf("%s    if (utf8Remaining > %s - utf8Index) %s;\n", ind, length, refused)
	g.pf("%s    for (int utf8Part = 0; utf8Part < utf8Remaining; utf8Part++)\n%s    {\n", ind, ind)
	g.pf("%s        int utf8Byte = %s[utf8Index++] & 0xFF;\n", ind, name)
	g.pf("%s        if ((utf8Byte & 0xC0) != 0x80) %s;\n", ind, refused)
	g.pf("%s        utf8Point = (utf8Point << 6) | (utf8Byte & 0x3F);\n%s    }\n", ind, ind)
	g.pf("%s    if (utf8Remaining == 1 && utf8Point < 0x80) %s;\n", ind, refused)
	g.pf("%s    if (utf8Remaining == 2 && (utf8Point < 0x800 || (utf8Point >= 0xD800 && utf8Point <= 0xDFFF))) %s;\n", ind, refused)
	g.pf("%s    if (utf8Remaining == 3 && (utf8Point < 0x10000 || utf8Point > 0x10FFFF)) %s;\n%s}\n", ind, refused, ind)
}

func (g *gen) emitReadUTF8(name, length, ind string) {
	g.emitUTF8(name, length, ind, "return -1")
}
