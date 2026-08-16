package c

// emitInteriorNullScan emits the read-side interior-null refusal every
// generated reader owes (SPEC §4.7): string(N) carries bytes excluding 0x00,
// and a payload with a null anywhere in [0, length) is content the read
// refuses. The C emitter never inherited this check from the other four
// backends — generated C readers accepted streams the C++, C#, Go and Rust
// readers refuse — and it lands here in the fastest correct shape rather
// than the naive one: WORD-WISE, eight bytes per step under the
// ((w - 0x0101..) & ~w & 0x8080..) zero-byte idiom. A payload of eight bytes
// or more re-tests its final eight bytes as one whole, OVERLAPPING word, so
// every load stays inside the payload the stream already delivered — no
// buffer-slack dependence, no masking, and no endianness dependence (the
// idiom reports a zero byte wherever it sits in the word). A shorter payload
// takes a per-byte tail bounded at seven. SCHEMA_C_READ_INLINE: the read
// spine's inlining demand covers the scan too. Guarded against redefinition
// because several wire headers can land in one translation unit; the
// trailing underscore keeps the name out of the claimed-name registry's way
// (no schema declaration can generate it).
func (g *gen) emitInteriorNullScan() {
	g.pf("#ifndef SCHEMA_INTERIOR_NULL_DEFINED\n#define SCHEMA_INTERIOR_NULL_DEFINED\n")
	g.pf("/* string(N) carries bytes excluding 0x00: an interior null is content every\n")
	g.pf("   generated reader refuses (SPEC §4.7) — generated-code validation; no\n")
	g.pf("   serialize primitive performs it. The scan is word-wise: eight bytes per\n")
	g.pf("   step under the zero-byte idiom, and a payload of eight bytes or more\n")
	g.pf("   re-tests its final eight bytes as one whole (overlapping) word, so every\n")
	g.pf("   load stays inside the payload the stream already delivered. */\n")
	g.pf("static SCHEMA_UNUSED SCHEMA_C_READ_INLINE int schema_interior_null_( const serialize_uint8_t * bytes, int32_t length )\n{\n")
	g.pf("    serialize_uint64_t word;\n")
	g.pf("    int32_t i = 0;\n")
	g.pf("    if ( length >= 8 )\n    {\n")
	g.pf("        for ( ; i + 8 <= length; i += 8 )\n        {\n")
	g.pf("            memcpy( &word, bytes + i, 8 );\n")
	g.pf("            if ( ( ( word - 0x0101010101010101ULL ) & ~word & 0x8080808080808080ULL ) != 0 )\n")
	g.pf("            {\n                return 1;\n            }\n        }\n")
	g.pf("        if ( i < length )\n        {\n")
	g.pf("            memcpy( &word, bytes + length - 8, 8 );\n")
	g.pf("            if ( ( ( word - 0x0101010101010101ULL ) & ~word & 0x8080808080808080ULL ) != 0 )\n")
	g.pf("            {\n                return 1;\n            }\n        }\n")
	g.pf("        return 0;\n    }\n")
	g.pf("    for ( ; i < length; i++ )\n    {\n")
	g.pf("        if ( bytes[i] == 0 )\n        {\n            return 1;\n        }\n    }\n")
	g.pf("    return 0;\n}\n")
	g.pf("#endif /* SCHEMA_INTERIOR_NULL_DEFINED */\n\n")
}
