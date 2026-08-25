package cpp

import "github.com/mas-bandwidth/schema/ir"

// fileHasStrings reports whether any declaration in the file carries a
// string(N) field — the trigger for emitting the UTF-8 validator the
// write-side debug assert calls (SPEC §4.7).
func fileHasStrings(f *ir.File) bool {
	for _, d := range f.Decls {
		var fields []*ir.Field
		switch d := d.(type) {
		case *ir.Struct:
			fields = d.Fields
		case *ir.Object:
			fields = d.Fields
		}
		for _, fd := range fields {
			if fd.Type.Kind == ir.TString {
				return true
			}
		}
	}
	return false
}

// emitUtf8Validator emits the well-formedness check behind the write-side
// debug assert: string(N) payloads are well-formed UTF-8 BY CONTRACT,
// writer-trusted (SPEC §4.7) — no release-path cost, no
// read-path validation. Guarded against redefinition because several wire
// headers can land in one translation unit.
func (g *gen) emitUtf8Validator() {
	g.pf("#ifndef SCHEMA_UTF8_VALID_DEFINED\n#define SCHEMA_UTF8_VALID_DEFINED\n")
	g.pf("// string(N) payloads are well-formed UTF-8 BY CONTRACT (SPEC §4.7): the\n")
	g.pf("// write path debug-asserts with this validator and the release path costs\n")
	g.pf("// nothing. Rejects truncated sequences, bare continuations, overlongs,\n")
	g.pf("// surrogates and code points past U+10FFFF.\n")
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
	g.pf("#endif // SCHEMA_UTF8_VALID_DEFINED\n\n")
}
