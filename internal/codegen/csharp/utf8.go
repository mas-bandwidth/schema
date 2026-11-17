package csharp

import "github.com/mas-bandwidth/schema/v2/ir"

// unitHasStrings reports whether any struct field in the unit is a string(N),
// the trigger for the generated content validator.
func unitHasStrings(u *ir.Unit) bool {
	for _, st := range u.Structs {
		for _, f := range st.Fields {
			if f.Type.Kind == ir.TString {
				return true
			}
		}
	}
	return false
}

// emitTextValid emits the unit's string(N) content validator once, into the
// protocol-id home file. Both the read path and the packet writer's debug
// assert call it: a used byte range with a zero byte or malformed UTF-8 is
// content neither side accepts (SPEC §4.7, §5.1 row 8).
func (g *gen) emitTextValid() {
	g.needsSystem = true // ReadOnlySpan<byte>
	g.sf("// TextValid is the string(N) content rule: no zero byte and well-formed\n")
	g.sf("// UTF-8 over the used bytes (SPEC §4.7, §5.1 row 8).\n")
	g.sf("private static bool TextValid(ReadOnlySpan<byte> bytes)\n{\n")
	g.sf("    for (int utf8Index = 0; utf8Index < bytes.Length;)\n    {\n")
	g.sf("        int utf8Lead = bytes[utf8Index++];\n")
	g.sf("        if (utf8Lead == 0) return false; // a zero byte is not text (SPEC §5.1 row 8)\n")
	g.sf("        if (utf8Lead < 0x80) continue;\n")
	g.sf("        int utf8Remaining, utf8Point;\n")
	g.sf("        if ((utf8Lead & 0xE0) == 0xC0) { utf8Remaining = 1; utf8Point = utf8Lead & 0x1F; }\n")
	g.sf("        else if ((utf8Lead & 0xF0) == 0xE0) { utf8Remaining = 2; utf8Point = utf8Lead & 0x0F; }\n")
	g.sf("        else if ((utf8Lead & 0xF8) == 0xF0) { utf8Remaining = 3; utf8Point = utf8Lead & 0x07; }\n")
	g.sf("        else return false; // malformed UTF-8 lead (SPEC §4.7)\n")
	g.sf("        if (utf8Remaining > bytes.Length - utf8Index) return false;\n")
	g.sf("        for (int utf8Part = 0; utf8Part < utf8Remaining; utf8Part++)\n        {\n")
	g.sf("            int utf8Byte = bytes[utf8Index++];\n")
	g.sf("            if ((utf8Byte & 0xC0) != 0x80) return false;\n")
	g.sf("            utf8Point = (utf8Point << 6) | (utf8Byte & 0x3F);\n        }\n")
	g.sf("        if (utf8Remaining == 1 && utf8Point < 0x80) return false;\n")
	g.sf("        if (utf8Remaining == 2 && (utf8Point < 0x800 || (utf8Point >= 0xD800 && utf8Point <= 0xDFFF))) return false;\n")
	g.sf("        if (utf8Remaining == 3 && (utf8Point < 0x10000 || utf8Point > 0x10FFFF)) return false;\n")
	g.sf("    }\n")
	g.sf("    return true;\n}\n\n")
}

// emitReadUTF8 checks the used bytes through the unit's validator.
func (g *gen) emitReadUTF8(name, length, ind string) {
	g.sf("%sif (!TextValid(%s)) // a zero byte or malformed UTF-8 is content the read refuses (SPEC §4.7)\n", ind, g.prefix(name, length))
	g.sf("%s{\n%s    return false;\n%s}\n", ind, ind, ind)
}
