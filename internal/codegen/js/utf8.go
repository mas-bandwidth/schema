package js

// emitReadUTF8 validates the used bytes in place, with no decoder or allocation.
// The scalar ranges are Unicode Table 3-7, as in the C++ reference.
func emitReadUTF8(pf func(string, ...any), name, length, ind string) {
	pf("%sfor (let utf8Index = 0; utf8Index < %s;)\n%s{\n", ind, length, ind)
	pf("%s  const utf8Lead = %s[utf8Index++];\n", ind, name)
	pf("%s  if (utf8Lead < 0x80) continue;\n", ind)
	pf("%s  let utf8Remaining, utf8Point;\n", ind)
	pf("%s  if ((utf8Lead & 0xE0) === 0xC0) { utf8Remaining = 1; utf8Point = utf8Lead & 0x1F; }\n", ind)
	pf("%s  else if ((utf8Lead & 0xF0) === 0xE0) { utf8Remaining = 2; utf8Point = utf8Lead & 0x0F; }\n", ind)
	pf("%s  else if ((utf8Lead & 0xF8) === 0xF0) { utf8Remaining = 3; utf8Point = utf8Lead & 0x07; }\n", ind)
	pf("%s  else return false; // malformed UTF-8 lead (SPEC §4.7)\n", ind)
	pf("%s  if (utf8Remaining > %s - utf8Index) return false;\n", ind, length)
	pf("%s  for (let utf8Part = 0; utf8Part < utf8Remaining; utf8Part++)\n%s  {\n", ind, ind)
	pf("%s    const utf8Byte = %s[utf8Index++];\n", ind, name)
	pf("%s    if ((utf8Byte & 0xC0) !== 0x80) return false;\n", ind)
	pf("%s    utf8Point = (utf8Point << 6) | (utf8Byte & 0x3F);\n%s  }\n", ind, ind)
	pf("%s  if (utf8Remaining === 1 && utf8Point < 0x80) return false;\n", ind)
	pf("%s  if (utf8Remaining === 2 && (utf8Point < 0x800 || (utf8Point >= 0xD800 && utf8Point <= 0xDFFF))) return false;\n", ind)
	pf("%s  if (utf8Remaining === 3 && (utf8Point < 0x10000 || utf8Point > 0x10FFFF)) return false;\n%s}\n", ind, ind)
}
