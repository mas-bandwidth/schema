package cpptable

import "github.com/mas-bandwidth/schema/v2/ir"

// THE ONE CONTENT RULE THE WIRE HAS (docs/SPEC-TABLES.md §3, §4), on BOTH
// text kinds: a kind 12 payload is well-formed UTF-8 with no zero byte and a
// kind 33 payload is paired UTF-16 with no zero unit, each checked as it
// arrives and before the reader's own bound, and each clamp cuts at a boundary
// its own content rule keeps whole. Emitted after TableReader in every unit,
// and reached from a text field, a text arm, a string map key, a text map
// value and a *string blob record.
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

// ONE CODE UNIT off the wire: two bytes LITTLE-ENDIAN, this wire's order for
// every fixed-width number (docs/SPEC-TABLES.md §3). No unit can exceed
// 0xFFFF, because two bytes cannot spell one.
inline uint16_t TableUtf16Unit( const uint8_t * bytes, int64_t index )
{
    return uint16_t( uint16_t( bytes[index * 2] ) | ( uint16_t( bytes[index * 2 + 1] ) << 8 ) );
}

// ILL-FORMED WIDE TEXT IS DAMAGE (docs/SPEC-TABLES.md §3, §4): a kind 33
// payload carrying an UNPAIRED SURROGATE or a ZERO CODE UNIT among its units,
// checked AS IT ARRIVES and before the reader's own bound, on the rule kind 12
// takes for UTF-8. An ODD L is framing damage and the caller rejects it ahead
// of this, because units is L / 2. SPEC.md §4.12 refuses the same content
// TERMINALLY on the packet wire; here the field reads its declared default,
// one malformed counts, and the parent reads on past L.
inline bool TableUtf16Valid( const uint8_t * bytes, int64_t units )
{
    int64_t i = 0;
    while ( i < units )
    {
        const uint16_t unit = TableUtf16Unit( bytes, i );
        if ( unit == 0 ) { return false; }
        if ( unit >= 0xD800 && unit <= 0xDBFF )
        {
            if ( i + 1 >= units ) { return false; } // a high surrogate with no low half
            const uint16_t low = TableUtf16Unit( bytes, i + 1 );
            if ( low < 0xDC00 || low > 0xDFFF ) { return false; }
            i += 2;
            continue;
        }
        if ( unit >= 0xDC00 && unit <= 0xDFFF ) { return false; } // a low surrogate first
        i++;
    }
    return true;
}

// A CLAMP CUTS AT A CODE UNIT BOUNDARY AND NEVER SPLITS A PAIR (§3, §16.2):
// the first bound units of a payload the check above already accepted, and
// where the last kept unit is a HIGH SURROGATE whose low half did not fit,
// that unit is dropped with it. So a clamp can never invent an unpaired
// surrogate, exactly as kind 12's clamp can never invent a broken sequence.
inline int64_t TableUtf16Clamp( const uint8_t * bytes, int64_t units, int64_t bound )
{
    if ( units <= bound ) { return units; }
    int64_t cut = bound;
    if ( cut > 0 )
    {
        const uint16_t last = TableUtf16Unit( bytes, cut - 1 );
        if ( last >= 0xD800 && last <= 0xDBFF ) { cut--; }
    }
    return cut;
}
`

// rootReachesStringBlob reports whether a root's numbering can name a *string
// record, which is where the content rule meets a node (§3.1).
func (g *tableGen) rootReachesStringBlob(root *ir.Struct) bool {
	for _, b := range reachableBlobs(root) {
		if b.constant == "kTableStringTypeId" {
			return true
		}
	}
	return false
}
