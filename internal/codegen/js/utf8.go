package js

import (
	"fmt"
	"strings"
)

// utf8Helper is the name of the module-scoped text validator both tiers call
// at every string field's read: one definition per module, not one copy per
// field.
const utf8Helper = "validText"

// emitTextHelper renders the module-scoped validator: the used bytes are
// well-formed UTF-8 and carry no interior null (SPEC §4.7), checked in place
// with no decoder and no allocation. The scalar ranges are Unicode Table 3-7,
// as in the C++ reference.
func emitTextHelper() string {
	var b strings.Builder
	pf := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	pf("// %s(bytes, length) -> bool: bytes[0, length) is well-formed UTF-8 with no\n", utf8Helper)
	pf("// interior null (SPEC §4.7). Every string field's read calls it; nothing\n")
	pf("// allocates and nothing latches.\n")
	pf("function %s(bytes, length) {\n", utf8Helper)
	pf("  for (let utf8Index = 0; utf8Index < %s;) {\n", "length")
	pf("    const utf8Lead = bytes[utf8Index++];\n")
	pf("    if (utf8Lead < 0x80) {\n")
	pf("      if (utf8Lead === 0) {\n")
	pf("        return false; // an interior null is content the read refuses\n")
	pf("      }\n")
	pf("      continue;\n")
	pf("    }\n")
	pf("    let utf8Remaining = 0;\n")
	pf("    let utf8Point = 0;\n")
	pf("    if ((utf8Lead & 0xe0) === 0xc0) {\n")
	pf("      utf8Remaining = 1;\n")
	pf("      utf8Point = utf8Lead & 0x1f;\n")
	pf("    } else if ((utf8Lead & 0xf0) === 0xe0) {\n")
	pf("      utf8Remaining = 2;\n")
	pf("      utf8Point = utf8Lead & 0x0f;\n")
	pf("    } else if ((utf8Lead & 0xf8) === 0xf0) {\n")
	pf("      utf8Remaining = 3;\n")
	pf("      utf8Point = utf8Lead & 0x07;\n")
	pf("    } else {\n")
	pf("      return false; // malformed UTF-8 lead\n")
	pf("    }\n")
	pf("    if (utf8Remaining > length - utf8Index) {\n")
	pf("      return false;\n")
	pf("    }\n")
	pf("    for (let utf8Part = 0; utf8Part < utf8Remaining; utf8Part++) {\n")
	pf("      const utf8Byte = bytes[utf8Index++];\n")
	pf("      if ((utf8Byte & 0xc0) !== 0x80) {\n")
	pf("        return false;\n")
	pf("      }\n")
	pf("      utf8Point = (utf8Point << 6) | (utf8Byte & 0x3f);\n")
	pf("    }\n")
	pf("    if (utf8Remaining === 1 && utf8Point < 0x80) {\n")
	pf("      return false; // overlong\n")
	pf("    }\n")
	pf("    if (utf8Remaining === 2 && (utf8Point < 0x800 || (utf8Point >= 0xd800 && utf8Point <= 0xdfff))) {\n")
	pf("      return false; // overlong, or a surrogate\n")
	pf("    }\n")
	pf("    if (utf8Remaining === 3 && (utf8Point < 0x10000 || utf8Point > 0x10ffff)) {\n")
	pf("      return false; // overlong, or past U+10FFFF\n")
	pf("    }\n")
	pf("  }\n")
	pf("  return true;\n")
	pf("}\n\n")
	return b.String()
}

// emitReadText emits the call site: one line per string field.
func emitReadText(pf func(string, ...any), name, length, ind string) {
	pf("%sif (!%s(%s, %s)) {\n%s  return false;\n%s}\n", ind, utf8Helper, name, length, ind, ind)
}
