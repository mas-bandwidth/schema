package cpptable

import "github.com/mas-bandwidth/schema/v2/ir"

// THE ONE CONTENT RULE THE WIRE HAS (docs/SPEC-TABLES.md §3, §4): a kind 12
// payload is well-formed UTF-8 with no zero byte, checked as it arrives and
// before the reader's own bound, and a clamp cuts at a code point boundary.
// Emitted after TableReader in every unit, and reached from a string field, a
// string arm, a string map key and a *string blob record.
const tableTextRuntime = `
// ILL-FORMED TEXT IS DAMAGE (docs/SPEC-TABLES.md §3, §4): a kind 12 payload is
// well-formed UTF-8 with no zero byte among its bytes, checked AS IT ARRIVES
// and before the reader's own bound, because a payload that is not text is not
// text at whatever length the reader would have kept. Rejects a zero byte, a
// truncated sequence, a bare continuation, an overlong encoding, a surrogate
// and a code point past U+10FFFF, which is SPEC.md §4.7's rule in this wire's
// idiom: the field reads its declared default, one malformed counts, and the
// parent reads on past L.
inline bool TableUtf8Valid( const uint8_t * bytes, int64_t length )
{
    int64_t i = 0;
    while ( i < length )
    {
        const uint8_t lead = bytes[i];
        int64_t continuations;
        uint32_t code_point;
        if ( lead == 0 ) { return false; }
        if ( lead < 0x80 ) { i++; continue; }
        else if ( ( lead & 0xE0 ) == 0xC0 ) { continuations = 1; code_point = lead & 0x1F; }
        else if ( ( lead & 0xF0 ) == 0xE0 ) { continuations = 2; code_point = lead & 0x0F; }
        else if ( ( lead & 0xF8 ) == 0xF0 ) { continuations = 3; code_point = lead & 0x07; }
        else { return false; }
        if ( i + continuations >= length ) { return false; }
        for ( int64_t k = 1; k <= continuations; k++ )
        {
            if ( ( bytes[i + k] & 0xC0 ) != 0x80 ) { return false; }
            code_point = ( code_point << 6 ) | uint32_t( bytes[i + k] & 0x3F );
        }
        if ( continuations == 1 && code_point < 0x80 ) { return false; }
        if ( continuations == 2 && ( code_point < 0x800 || ( code_point >= 0xD800 && code_point <= 0xDFFF ) ) ) { return false; }
        if ( continuations == 3 && ( code_point < 0x10000 || code_point > 0x10FFFF ) ) { return false; }
        i += 1 + continuations;
    }
    return true;
}

// A CLAMP CUTS AT A CODE POINT BOUNDARY (§3, §16.2): the last whole code point
// that fits within the bound, over a payload the check above already accepted,
// so a clamp can never invent ill-formed storage.
inline int64_t TableUtf8Clamp( const uint8_t * bytes, int64_t length, int64_t bound )
{
    if ( length <= bound ) { return length; }
    int64_t cut = bound;
    while ( cut > 0 && ( bytes[cut] & 0xC0 ) == 0x80 ) { cut--; }
    return cut;
}
`

// rootReachesStringBlob reports whether a root's numbering can name a *string
// record, which is where the content rule meets a node (§3.1).
func (g *tableGen) rootReachesStringBlob(root *ir.Struct) bool {
	for _, b := range g.reachableBlobs(root) {
		if b.constant == "kTableStringTypeId" {
			return true
		}
	}
	return false
}
