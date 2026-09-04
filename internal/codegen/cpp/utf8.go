package cpp

import (
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// fileHasStrings reports whether any declaration in the file carries a
// string(N) field — the trigger for emitting the UTF-8 validator the read
// path calls (SPEC §4.7).
func fileHasStrings(f *ir.File) bool {
	for _, d := range f.Decls {
		var fields []*ir.Field
		if st, ok := d.(*ir.Struct); ok {
			fields = st.Fields
		}
		for _, fd := range fields {
			if fd.Type.Kind == ir.TString {
				return true
			}
		}
	}
	return false
}

// emitUtf8Validator emits the well-formedness check the READ path runs:
// a string(N) payload that is not well-formed UTF-8 fails the read, in every
// build mode, and the refusal is terminal (SPEC §4.7). The check is
// generated-code validation because no serialize primitive performs it and
// three targets have no runtime at all. There is no write-side twin: §5 puts
// text well-formedness on the reader rather than on a write assert some
// targets carry and others do not. Guarded per package: the validator lives
// inside namespace <package>, so each unit in a translation unit needs its own
// copy exactly once — several wire headers of one package can land in one
// translation unit.
func (g *gen) emitUtf8Validator() {
	guard := "SCHEMA_" + strings.ToUpper(g.unit.Package) + "_UTF8_VALID_DEFINED"
	g.pf("#ifndef %s\n#define %s\n", guard, guard)
	g.pf("// string(N) payloads are well-formed UTF-8 and the READER enforces it\n")
	g.pf("// (SPEC §4.7): a malformed payload fails the read, in every build mode,\n")
	g.pf("// and the refusal is terminal. Rejects truncated sequences, bare\n")
	g.pf("// continuations, overlongs, surrogates and code points past U+10FFFF.\n")
	g.pf("inline bool schema_utf8_valid( const uint8_t * bytes, int32_t length )\n{\n")
	g.pf("    int32_t i = 0;\n")
	g.pf("    while ( i < length )\n    {\n")
	g.pf("        uint8_t lead = bytes[i];\n")
	g.pf("        int32_t continuations;\n")
	g.pf("        uint32_t code_point;\n")
	g.pf("        if ( lead < 0x80 )\n        {\n            i++;\n            continue;\n        }\n")
	g.pf("        else if ( ( lead & 0xE0 ) == 0xC0 )\n        {\n            continuations = 1;\n            code_point = lead & 0x1F;\n        }\n")
	g.pf("        else if ( ( lead & 0xF0 ) == 0xE0 )\n        {\n            continuations = 2;\n            code_point = lead & 0x0F;\n        }\n")
	g.pf("        else if ( ( lead & 0xF8 ) == 0xF0 )\n        {\n            continuations = 3;\n            code_point = lead & 0x07;\n        }\n")
	g.pf("        else\n        {\n            return false;\n        }\n")
	g.pf("        if ( i + continuations >= length )\n        {\n            return false;\n        }\n")
	g.pf("        for ( int32_t k = 1; k <= continuations; k++ )\n        {\n")
	g.pf("            if ( ( bytes[i + k] & 0xC0 ) != 0x80 )\n            {\n                return false;\n            }\n")
	g.pf("            code_point = ( code_point << 6 ) | uint32_t( bytes[i + k] & 0x3F );\n        }\n")
	g.pf("        if ( continuations == 1 && code_point < 0x80 )\n        {\n            return false;\n        }\n")
	g.pf("        if ( continuations == 2 && ( code_point < 0x800 || ( code_point >= 0xD800 && code_point <= 0xDFFF ) ) )\n        {\n            return false;\n        }\n")
	g.pf("        if ( continuations == 3 && ( code_point < 0x10000 || code_point > 0x10FFFF ) )\n        {\n            return false;\n        }\n")
	g.pf("        i += 1 + continuations;\n    }\n")
	g.pf("    return true;\n}\n")
	g.pf("#endif // %s\n\n", guard)
}
