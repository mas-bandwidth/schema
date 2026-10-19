package cpptable

import (
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE ONE CONTENT RULE THE WIRE HAS (docs/SPEC-TABLES.md §3, §4), on BOTH
// text kinds: a kind 12 payload is well-formed UTF-8 with no zero byte and a
// kind 33 payload is paired UTF-16 with no zero unit, each checked as it
// arrives and before the reader's own bound, and each clamp cuts at a boundary
// its own content rule keeps whole.
//
// THE TWO KINDS ARE TWO RUNTIMES, because they reach two different sets of
// units. tableTextRuntime below is kind 12's, emitted after TableReader in
// EVERY unit, and reached from a text field, a text arm, a string map key, a
// text map value and a *string blob record — a unit with none of those still
// carries it, because TableJsonScanUnit in the generic walk reads it, and that
// walk is ONE artifact, byte-identical in every generated .cpp.
// tableWideTextRuntime is kind 33's, and it is emitted only into a unit that
// declares wide text.
const tableTextRuntime = `
// ILL-FORMED TEXT IS DAMAGE (docs/SPEC-TABLES.md §3, §4): a kind 12 payload is
// well-formed UTF-8 with no zero byte among its bytes, checked AS IT ARRIVES
// and before the reader's own bound, because a payload that is not text is not
// text at whatever length the reader would have kept. Rejects a zero byte, a
// truncated sequence, a bare continuation, an overlong encoding, a surrogate
// and a code point past U+10FFFF, which is SPEC.md §4.7's rule in this wire's
// idiom: the field reads its declared default, one malformed counts, and the
// parent reads on past L.
//
// A LENGTH IS A 64-BIT NUMBER (§3), so it arrives as one: a payload length is
// whatever the wire spelled, and narrowing it to a signed count would read
// 0xFFFFFFFFFFFFFFFF as an empty payload.
inline bool TableUtf8Valid( const uint8_t * bytes, uint64_t length )
{
    uint64_t i = 0;
    while ( i < length )
    {
        const uint8_t lead = bytes[i];
        uint64_t continuations;
        uint32_t code_point;
        if ( lead == 0 ) { return false; }
        if ( lead < 0x80 ) { i++; continue; }
        else if ( ( lead & 0xE0 ) == 0xC0 ) { continuations = 1; code_point = lead & 0x1F; }
        else if ( ( lead & 0xF0 ) == 0xE0 ) { continuations = 2; code_point = lead & 0x0F; }
        else if ( ( lead & 0xF8 ) == 0xF0 ) { continuations = 3; code_point = lead & 0x07; }
        else { return false; }
        if ( i + continuations >= length ) { return false; }
        for ( uint64_t k = 1; k <= continuations; k++ )
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
//
// THE ANSWER IS NEVER ABOVE THE BOUND. The length arrives as the wire's own
// 64-bit number and the caller turns the answer back into the size of a copy,
// so a length no reader could have bounded has to leave here bounded: taken as
// a signed count, 0xFFFFFFFFFFFFFFFF is -1, -1 is under every bound, and the
// copy would run at SIZE_MAX.
inline int64_t TableUtf8Clamp( const uint8_t * bytes, uint64_t length, int64_t bound )
{
    if ( length <= (uint64_t) bound ) { return (int64_t) length; }
    int64_t cut = bound;
    while ( cut > 0 && ( bytes[cut] & 0xC0 ) == 0x80 ) { cut--; }
    return cut;
}
`

// tableWideTextRuntime is the KIND 33 half, and it is emitted ONLY into a unit
// that declares a `wstring(N)` somewhere — the census ir.WideTextFields takes,
// which is the same one every other target refuses the unit on. Nothing but a
// wide-text field, a wide-text arm or a wide-text map value reads a kind 33
// payload (docs/SPEC-TABLES.md §3), so a unit with no wide text has no call
// site for these three and carried forty-seven lines it could not reach. Split
// out for the reason the map, list and arena runtimes are: a unit pays for the
// construct it declares (§2.2).
const tableWideTextRuntime = `
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

// tableTextFormRuntime is the HAND-EDITABLE text form's reader
// (docs/SPEC-TABLES.md §16.9.1 grammar, §16.9.2 mapping). It parses the
// strict s-expression document into a tree, resolves every value against the
// field descriptor (§16.9.2), and transcodes the accepted value into the
// canonical JSON of the same value. The existing generic JSON walk (§16) then
// lands it, exactly as the Go implementation does: the JSON walk's counters
// must all be zero, because every place §16.2 would clamp, skip or last-win
// was refused before the walk ran (§16.9.4).
//
// The runtime is emitted once per generated Table.cpp behind the same
// package guard the JSON walk uses, and its functions are inline so a unity
// build and separate objects alike see one definition.
const tableTextFormRuntime = `// ---- hand-editable text form (docs/SPEC-TABLES.md §16.9): begin ----

struct TableTextFormNode
{
    char kind; // '(' list/record, '"' string, '0' number, ':' keyword, 't', 'f', 'z'
    std::string text;
    std::vector<TableTextFormNode> items;
};

// SHARING LABELS (docs/SPEC-TABLES.md §16.9.2, §16.7; the Go oracle,
// internal/codegen/gotable/textform.go tableTextFormLabel/pointer): a label
// names one node and need not be sequential on read. A definition completes
// before a later label may reference it; a second definition under one label
// is text_reference. The resolving walk records the state in source order, so
// the canonical JSON it writes carries the same labels the FromJson graph walk
// expects.
struct FormTextLabel
{
    std::string key;
    const TableTypeInfo * info;
    bool done;
};

struct FormTextResolver
{
    std::vector<FormTextLabel> labels;

    FormTextLabel * find( const std::string & key )
    {
        for ( size_t i = 0; i < labels.size(); i++ ) { if ( labels[i].key == key ) { return &labels[i]; } }
        return NULL;
    }
};

struct TableTextFormReader
{
    const char * s;
    int64_t n;
    int64_t i;
    const char ** why;

    bool fail( const char * name ) { if ( why != NULL && *why == NULL ) { *why = name; } return false; }

    bool gap()
    {
        for ( ;; )
        {
            while ( i < n && ( s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' ) ) { i++; }
            if ( i < n && s[i] == ';' )
            {
                while ( i < n && s[i] != '\n' ) { i++; }
                continue;
            }
            return true;
        }
    }

    static void putUtf8( std::string & out, uint32_t c )
    {
        if ( c < 0x80 ) { out += (char) c; }
        else if ( c < 0x800 ) { out += (char) ( 0xC0 | ( c >> 6 ) ); out += (char) ( 0x80 | ( c & 0x3F ) ); }
        else if ( c < 0x10000 ) { out += (char) ( 0xE0 | ( c >> 12 ) ); out += (char) ( 0x80 | ( ( c >> 6 ) & 0x3F ) ); out += (char) ( 0x80 | ( c & 0x3F ) ); }
        else { out += (char) ( 0xF0 | ( c >> 18 ) ); out += (char) ( 0x80 | ( ( c >> 12 ) & 0x3F ) ); out += (char) ( 0x80 | ( ( c >> 6 ) & 0x3F ) ); out += (char) ( 0x80 | ( c & 0x3F ) ); }
    }

    static int hex( char c )
    {
        if ( c >= '0' && c <= '9' ) { return c - '0'; }
        if ( c >= 'a' && c <= 'f' ) { return c - 'a' + 10; }
        if ( c >= 'A' && c <= 'F' ) { return c - 'A' + 10; }
        return -1;
    }

    bool quoted( std::string & out )
    {
        if ( i >= n || s[i] != '"' ) { return fail( "text_kind_mismatch" ); }
        i++;
        while ( i < n )
        {
            const unsigned char c = (unsigned char) s[i++];
            if ( c == '"' ) { return true; }
            if ( c < 0x20 ) { return fail( "text_encoding" ); }
            if ( c != '\\' ) { out += (char) c; continue; }
            if ( i >= n ) { return fail( "text_unterminated" ); }
            const char e = s[i++];
            switch ( e )
            {
                case '"': out += '"'; break;
                case '\\': out += '\\'; break;
                case '/': out += '/'; break;
                case 'b': out += '\b'; break;
                case 'f': out += '\f'; break;
                case 'n': out += '\n'; break;
                case 'r': out += '\r'; break;
                case 't': out += '\t'; break;
                case 'u':
                {
                    if ( i + 4 > n ) { return fail( "text_unterminated" ); }
                    uint32_t cp = 0;
                    for ( int k = 0; k < 4; k++ ) { const int h = hex( s[i + k] ); if ( h < 0 ) { return fail( "text_encoding" ); } cp = ( cp << 4 ) | (uint32_t) h; }
                    i += 4;
                    if ( cp >= 0xD800 && cp <= 0xDBFF )
                    {
                        if ( i + 6 > n || s[i] != '\\' || s[i + 1] != 'u' ) { return fail( "text_encoding" ); }
                        i += 2;
                        uint32_t low = 0;
                        for ( int k = 0; k < 4; k++ ) { const int h = hex( s[i + k] ); if ( h < 0 ) { return fail( "text_encoding" ); } low = ( low << 4 ) | (uint32_t) h; }
                        i += 4;
                        if ( low < 0xDC00 || low > 0xDFFF ) { return fail( "text_encoding" ); }
                        cp = 0x10000 + ( ( cp - 0xD800 ) << 10 ) + ( low - 0xDC00 );
                    }
                    else if ( cp >= 0xDC00 && cp <= 0xDFFF ) { return fail( "text_encoding" ); }
                    putUtf8( out, cp );
                    break;
                }
                default: return fail( "text_syntax" );
            }
        }
        return fail( "text_unterminated" );
    }

    bool value( TableTextFormNode & node, int depth )
    {
        if ( depth > 128 ) { return fail( "text_limit" ); }
        gap();
        if ( i >= n ) { return fail( "text_syntax" ); }
        const char c = s[i];
        if ( c == '(' )
        {
            i++;
            node.kind = '(';
            for ( ;; )
            {
                gap();
                if ( i >= n ) { return fail( "text_unterminated" ); }
                if ( s[i] == ')' ) { i++; return true; }
                TableTextFormNode child;
                if ( !value( child, depth + 1 ) ) { return false; }
                node.items.push_back( child );
            }
        }
        if ( c == '"' ) { node.kind = '"'; return quoted( node.text ); }
        if ( c == ':' )
        {
            i++;
            node.kind = ':';
            while ( i < n )
            {
                const char ch = s[i];
                if ( ( ch >= 'a' && ch <= 'z' ) || ( ch >= 'A' && ch <= 'Z' ) || ( ch >= '0' && ch <= '9' ) || ch == '_' || ch == '-' || ch == '/' || ch == '.' ) { node.text += ch; i++; }
                else { break; }
            }
            return !node.text.empty() || fail( "text_syntax" );
        }
        // an atom: number, true, false or null
        const int64_t start = i;
        while ( i < n )
        {
            const char ch = s[i];
            if ( ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == '(' || ch == ')' || ch == ';' || ch == '"' ) { break; }
            i++;
        }
        const std::string atom( s + start, (size_t) ( i - start ) );
        if ( atom.empty() ) { return fail( "text_syntax" ); }
        const char c0 = atom[0];
        if ( ( c0 >= 'a' && c0 <= 'z' ) || ( c0 >= 'A' && c0 <= 'Z' ) )
        {
            if ( atom == "true" ) { node.kind = 't'; return true; }
            if ( atom == "false" ) { node.kind = 'f'; return true; }
            if ( atom == "null" ) { node.kind = 'z'; return true; }
            return fail( "text_syntax" );
        }
        node.kind = '0';
        node.text = atom;
        return true;
    }
};

// TableTextFormJsonString, TableB64Val and TableB64Valid, TableUtf16Length
// are the §16.9.2 value checks: a string/wstring field's decoded length
// against its declared bound, and a bytes field's canonical base64 (zero pad
// bits, no stray byte) with its decoded length against its bound.
inline int TableB64Val( char c )
{
    if ( c >= 'A' && c <= 'Z' ) { return c - 'A'; }
    if ( c >= 'a' && c <= 'z' ) { return c - 'a' + 26; }
    if ( c >= '0' && c <= '9' ) { return c - '0' + 52; }
    if ( c == '+' ) { return 62; }
    if ( c == '/' ) { return 63; }
    return -1;
}

inline bool TableB64Valid( const char * s, int64_t n, int64_t & dec )
{
    dec = 0;
    if ( n % 4 != 0 ) { return false; }
    int64_t i = 0;
    while ( i < n && s[i] != '=' )
    {
        if ( TableB64Val( s[i] ) < 0 ) { return false; }
        i++;
    }
    int64_t pad = 0;
    while ( i < n ) { if ( s[i] != '=' ) { return false; } i++; pad++; }
    if ( pad > 2 ) { return false; }
    if ( pad == 1 ) { const int v = TableB64Val( s[n - 2] ); if ( v < 0 || ( v & 0x03 ) != 0 ) { return false; } }
    if ( pad == 2 ) { const int v = TableB64Val( s[n - 3] ); if ( v < 0 || ( v & 0x0F ) != 0 ) { return false; } }
    dec = ( ( n / 4 ) * 3 ) - pad;
    return true;
}

inline int64_t TableUtf16Length( const std::string & s )
{
    int64_t units = 0;
    for ( size_t i = 0; i < s.size(); )
    {
        const unsigned char c = (unsigned char) s[i];
        uint32_t cp;
        if ( c < 0x80 ) { cp = c; i += 1; }
        else if ( ( c & 0xE0 ) == 0xC0 ) { cp = ( (uint32_t)( c & 0x1F ) << 6 ) | ( (uint32_t)( (unsigned char) s[i + 1] & 0x3F ) ); i += 2; }
        else if ( ( c & 0xF0 ) == 0xE0 ) { cp = ( (uint32_t)( c & 0x0F ) << 12 ) | ( (uint32_t)( (unsigned char) s[i + 1] & 0x3F ) << 6 ) | ( (uint32_t)( (unsigned char) s[i + 2] & 0x3F ) ); i += 3; }
        else { cp = ( (uint32_t)( c & 0x07 ) << 18 ) | ( (uint32_t)( (unsigned char) s[i + 1] & 0x3F ) << 12 ) | ( (uint32_t)( (unsigned char) s[i + 2] & 0x3F ) << 6 ) | ( (uint32_t)( (unsigned char) s[i + 3] & 0x3F ) ); i += 4; }
        units++;
        if ( cp >= 0x10000 ) { units++; }
    }
    return units;
}

inline bool TableTextFormKeyNode( const TableTextFormNode & n ) { return n.kind == ':' || n.kind == '"'; }

inline void TableTextFormJsonString( std::string & out, const std::string & s )
{
    static const char hex[] = "0123456789abcdef";
    out += '"';
    for ( size_t i = 0; i < s.size(); i++ )
    {
        const unsigned char c = (unsigned char) s[i];
        switch ( c )
        {
            case '"': out += "\\\""; break;
            case '\\': out += "\\\\"; break;
            case '\b': out += "\\b"; break;
            case '\f': out += "\\f"; break;
            case '\n': out += "\\n"; break;
            case '\r': out += "\\r"; break;
            case '\t': out += "\\t"; break;
            default:
                if ( c < 0x20 ) { out += "\\u00"; out += hex[c >> 4]; out += hex[c & 15]; }
                else { out += (char) c; }
        }
    }
    out += '"';
}

inline bool TableTextFormRecord( const TableTextFormNode & n, const TableTypeInfo * info, FormTextResolver & r, std::string & out, const char ** why, int64_t first, const std::string & label );
inline bool TableTextFormField( const TableTextFormNode & n, const TableFieldInfo * f, FormTextResolver & r, std::string & out, const char ** why );

inline bool TableTextFormScalar( const TableTextFormNode & n, const TableFieldInfo * f, FormTextResolver & r, std::string & out, const char ** why )
{
    if ( f->optional && n.kind == 'z' ) { out += "null"; return true; }
    if ( f->is_array && f->array_bound == 0 && strncmp( f->type_name, "map[", 4 ) == 0 )
    {
        if ( n.kind != '(' ) { *why = "text_kind_mismatch"; return false; }
        const TableFieldInfo * key = &f->table->fields[0];
        const TableFieldInfo * value = &f->table->fields[1];
        out += '{';
        std::vector<std::string> seen;
        for ( size_t i = 0; i < n.items.size(); i++ )
        {
            const TableTextFormNode & pair = n.items[i];
            if ( pair.kind != '(' || pair.items.size() != 2 ) { *why = "text_kind_mismatch"; return false; }
            if ( i > 0 ) { out += ','; }
            if ( key->kind == 12 ) { TableTextFormJsonString( out, pair.items[0].text ); }
            else { TableTextFormJsonString( out, pair.items[0].text ); }
            out += ':';
            for ( size_t k = 0; k < seen.size(); k++ ) { if ( seen[k] == pair.items[0].text ) { *why = "text_duplicate_key"; return false; } }
            seen.push_back( pair.items[0].text );
            if ( !TableTextFormField( pair.items[1], value, r, out, why ) ) { return false; }
        }
        out += '}';
        return true;
    }
    if ( f->is_array && f->array_bound == 0 )
    {
        // AN ELEMENT IS NOT THE LIST (docs/SPEC-TABLES.md §16.9.2; Go
        // internal/codegen/gotable/textform.go sequence/scalar): resolve each
        // item at the ELEMENT level, so []*T reaches the pointer's sharing
        // construct once per item rather than treating the item as a list.
        if ( n.kind != '(' ) { *why = "text_kind_mismatch"; return false; }
        TableFieldInfo element = *f;
        element.is_array = false;
        element.counted = false;
        element.array_bound = 0;
        out += '[';
        for ( size_t i = 0; i < n.items.size(); i++ )
        {
            if ( i > 0 ) { out += ','; }
            if ( !TableTextFormScalar( n.items[i], &element, r, out, why ) ) { return false; }
        }
        out += ']';
        return true;
    }
    if ( f->key_name != NULL )
    {
        if ( n.kind != '(' || ( n.items.size() % 2 ) != 0 ) { *why = "text_kind_mismatch"; return false; }
        TableFieldInfo element = *f;
        element.key_name = NULL;
        element.is_array = false;
        element.array_bound = 0;
        element.optional = false;
        out += '{';
        std::vector<int32_t> seen;
        for ( size_t i = 0; i < n.items.size(); i += 2 )
        {
            if ( i > 0 ) { out += ','; }
            const TableTextFormNode & key = n.items[i];
            if ( !TableTextFormKeyNode( key ) ) { *why = "text_syntax"; return false; }
            int32_t slot = -1;
            for ( int32_t s = 1; s <= f->array_bound; s++ ) { const char * name = f->key_name( (uint64_t) s ); if ( name != NULL && key.text == name ) { slot = s; break; } }
            if ( slot < 0 ) { *why = "text_unknown_key"; return false; }
            for ( size_t k = 0; k < seen.size(); k++ ) { if ( seen[k] == slot ) { *why = "text_duplicate_key"; return false; } }
            seen.push_back( slot );
            TableTextFormJsonString( out, key.text );
            out += ':';
            if ( !TableTextFormScalar( n.items[i + 1], &element, r, out, why ) ) { return false; }
        }
        out += '}';
        return true;
    }
    if ( f->arms != NULL )
    {
        if ( n.kind != '(' || ( n.items.size() % 2 ) != 0 ) { *why = "text_kind_mismatch"; return false; }
        out += '{';
        std::vector<int64_t> seen;
        for ( size_t i = 0; i < n.items.size(); i += 2 )
        {
            if ( i > 0 ) { out += ','; }
            const TableTextFormNode & key = n.items[i];
            if ( !TableTextFormKeyNode( key ) ) { *why = "text_syntax"; return false; }
            if ( !key.text.empty() && key.text[0] == '&' ) { *why = "text_reference"; return false; }
            int64_t tag = 0;
            for ( int64_t t = 1; t <= f->enum_max; t++ ) { if ( strcmp( f->enum_name( (uint64_t) t ), key.text.c_str() ) == 0 ) { tag = t; break; } }
            if ( tag == 0 ) { *why = "text_unknown_key"; return false; }
            for ( size_t k = 0; k < seen.size(); k++ ) { if ( seen[k] == tag ) { *why = "text_duplicate_key"; return false; } }
            // A UNION HOLDS ONE ARM (docs/SPEC-TABLES.md §16.9.2, Go
            // internal/codegen/gotable/textform.go union): two different arms
            // is the wrong shape, and the same arm twice is a duplicate.
            if ( !seen.empty() ) { *why = "text_kind_mismatch"; return false; }
            seen.push_back( tag );
            TableTextFormJsonString( out, key.text );
            out += ':';
            const TableUnionArmInfo & arm = f->arms()->arms[tag];
            if ( arm.field != NULL ) { if ( !TableTextFormField( n.items[i + 1], arm.field, r, out, why ) ) { return false; } }
            else if ( arm.table != NULL ) { if ( !TableTextFormRecord( n.items[i + 1], arm.table, r, out, why, 0, "" ) ) { return false; } }
            else { out += "null"; }
        }
        out += '}';
        return true;
    }
    if ( f->kind == 17 )
    {
        if ( n.kind == 'z' ) { out += "null"; return true; }
        if ( f->table == NULL )
        {
            // A POINTER TO A SCALAR IS THE SCALAR WITHOUT A SCHEMA BOUND
            // (docs/SPEC-TABLES.md §16.9.2; Go internal/codegen/gotable/
            // textform.go pointer): *bytes still runs the STRICT base64 blob
            // check, so "AB==" is text_encoding and not a blob.
            if ( n.kind != '"' ) { *why = "text_kind_mismatch"; return false; }
            if ( strcmp( f->type_name, "bytes" ) == 0 )
            {
                int64_t dec = 0;
                if ( !TableB64Valid( n.text.c_str(), (int64_t) n.text.size(), dec ) ) { *why = "text_encoding"; return false; }
            }
            else if ( strcmp( f->type_name, "wstring" ) == 0 )
            {
                const int64_t units = TableUtf16Length( n.text );
                if ( f->array_bound != 0 && units > f->array_bound ) { *why = "text_bound"; return false; }
            }
            TableTextFormJsonString( out, n.text );
            return true;
        }
        // A VARIABLE ROOT'S GRAPH (docs/SPEC-TABLES.md §16.9.2, §16.7; Go
        // internal/codegen/gotable/textform.go pointer): a pointee's FIRST
        // pair may be the quoted key "&node", whose value is the label. A
        // definition carries the fields after it, a reference is the pair
        // alone, a label need not be sequential, and the label must name a
        // COMPLETED definition of the same table.
        const TableTypeInfo * info = f->table;
        if ( n.kind != '(' ) { *why = "text_kind_mismatch"; return false; }
        if ( n.items.empty() || !TableTextFormKeyNode( n.items[0] ) || n.items[0].text != "&node" )
        {
            return TableTextFormRecord( n, info, r, out, why, 0, "" );
        }
        if ( n.items.size() < 2 ) { *why = "text_syntax"; return false; }
        const TableTextFormNode & label = n.items[1];
        if ( label.kind != '0' || label.text.empty() || label.text[0] == '0' ) { *why = "text_reference"; return false; }
        for ( size_t k = 0; k < label.text.size(); k++ )
        {
            const char c = label.text[k];
            if ( c < '0' || c > '9' ) { *why = "text_reference"; return false; }
        }
        const std::string key = label.text;
        FormTextLabel * state = r.find( key );
        if ( n.items.size() == 2 )
        {
            // a reference: to a COMPLETED definition of the same table (§16.7)
            if ( state == NULL || !state->done || state->info != info ) { *why = "text_reference"; return false; }
            out += "{\"&node\":";
            out += key;
            out += '}';
            return true;
        }
        if ( state != NULL ) { *why = "text_reference"; return false; } // a second definition under one label
        const size_t at = r.labels.size();
        FormTextLabel opened;
        opened.key = key;
        opened.info = info;
        opened.done = false;
        r.labels.push_back( opened );
        if ( !TableTextFormRecord( n, info, r, out, why, 2, "\"&node\":" + key ) ) { return false; }
        r.labels[at].done = true; // index, not back(): a nested definition may have grown the list
        return true;
    }
    if ( f->kind == 13 ) { return TableTextFormRecord( n, f->table, r, out, why, 0, "" ); }
    if ( f->variant_id != NULL )
    {
        if ( !TableTextFormKeyNode( n ) ) { *why = "text_kind_mismatch"; return false; }
        for ( int64_t v = 0; v <= f->enum_max; v++ )
        {
            const char * name = f->enum_name( (uint64_t) v );
            if ( name != NULL && n.text == name ) { TableTextFormJsonString( out, n.text ); return true; }
        }
        *why = "text_unknown_variant";
        return false;
    }
    if ( f->enum_name != NULL )
    {
        if ( n.kind != '(' ) { *why = "text_kind_mismatch"; return false; }
        out += '[';
        std::vector<std::string> seen;
        for ( size_t i = 0; i < n.items.size(); i++ )
        {
            if ( !TableTextFormKeyNode( n.items[i] ) ) { *why = "text_kind_mismatch"; return false; }
            bool found = false;
            for ( int64_t b = 0; b <= f->enum_max; b++ ) { const char * name = f->enum_name( (uint64_t) b ); if ( name != NULL && n.items[i].text == name ) { found = true; break; } }
            if ( !found ) { *why = "text_unknown_variant"; return false; }
            for ( size_t k = 0; k < seen.size(); k++ ) { if ( seen[k] == n.items[i].text ) { *why = "text_duplicate_key"; return false; } }
            seen.push_back( n.items[i].text );
            if ( i > 0 ) { out += ','; }
            TableTextFormJsonString( out, n.items[i].text );
        }
        out += ']';
        return true;
    }
    if ( f->kind == 12 || f->kind == 33 || f->kind == 36 )
    {
        if ( n.kind != '"' ) { *why = "text_kind_mismatch"; return false; }
        int64_t size = (int64_t) n.text.size();
        if ( f->kind == 33 ) { size = TableUtf16Length( n.text ); }
        if ( f->array_bound != 0 && size > f->array_bound ) { *why = "text_bound"; return false; }
        TableTextFormJsonString( out, n.text );
        return true;
    }
    if ( f->kind == 1 )
    {
        if ( n.kind == 't' ) { out += "true"; return true; }
        if ( n.kind == 'f' ) { out += "false"; return true; }
        *why = "text_kind_mismatch";
        return false;
    }
    if ( n.kind != '0' ) { *why = "text_kind_mismatch"; return false; }
    double d = strtod( n.text.c_str(), NULL );
    if ( f->has_range )
    {
        if ( d < f->range_min || d > f->range_max ) { *why = "text_bound"; return false; }
    }
    // A FIXED-POINT TOKEN IS EXACT (docs/SPEC-TABLES.md §16.9.2, Go
    // internal/codegen/gotable/textform.go integer): scaled by 2^F it must be
    // an integer, so 0.1 is text_kind_mismatch and never a clamped raw unit.
    if ( f->frac_bits != 0 )
    {
        const double scaled = d * (double) ( (uint64_t) 1 << f->frac_bits );
        if ( scaled != (double) (int64_t) scaled ) { *why = "text_kind_mismatch"; return false; }
    }
    out += n.text;
    return true;
}

inline bool TableTextFormField( const TableTextFormNode & n, const TableFieldInfo * f, FormTextResolver & r, std::string & out, const char ** why )
{
    if ( f->optional && n.kind == 'z' ) { out += "null"; return true; }
    if ( f->is_array )
    {
        if ( f->kind == 6 && strcmp( f->type_name, "bytes" ) == 0 )
        {
            if ( n.kind != '"' ) { *why = "text_kind_mismatch"; return false; }
            int64_t dec = 0;
            if ( !TableB64Valid( n.text.c_str(), (int64_t) n.text.size(), dec ) ) { *why = "text_encoding"; return false; }
            if ( f->array_bound != 0 && dec > f->array_bound ) { *why = "text_bound"; return false; }
            TableTextFormJsonString( out, n.text );
            return true;
        }
        if ( f->array_bound != 0 && f->key_name == NULL )
        {
            if ( n.kind != '(' ) { *why = "text_kind_mismatch"; return false; }
            if ( !f->counted && n.items.size() != (size_t) f->array_bound ) { *why = "text_bound"; return false; }
            if ( f->counted && n.items.size() > (size_t) f->array_bound ) { *why = "text_bound"; return false; }
            out += '[';
            for ( size_t i = 0; i < n.items.size(); i++ )
            {
                if ( i > 0 ) { out += ','; }
                if ( !TableTextFormScalar( n.items[i], f, r, out, why ) ) { return false; }
            }
            out += ']';
            return true;
        }
    }
    return TableTextFormScalar( n, f, r, out, why );
}

// TableTextFormRecord resolves a list as a record's key/value pairs from pair
// index FIRST (a definition's "&node" label is pair 0); LABEL is the JSON
// already written for it, so the definition's canonical JSON opens with the
// sharing key (docs/SPEC-TABLES.md §16.9.2, §16.7; Go internal/codegen/
// gotable/textform.go record).
inline bool TableTextFormRecord( const TableTextFormNode & n, const TableTypeInfo * info, FormTextResolver & r, std::string & out, const char ** why, int64_t first, const std::string & label )
{
    if ( n.kind != '(' || ( n.items.size() % 2 ) != 0 ) { *why = "text_kind_mismatch"; return false; }
    out += '{';
    out += label;
    bool wrote = !label.empty();
    std::vector<int32_t> seen;
    for ( size_t i = (size_t) first; i < n.items.size(); i += 2 )
    {
        const TableTextFormNode & key = n.items[i];
        if ( !TableTextFormKeyNode( key ) ) { *why = "text_syntax"; return false; }
        if ( !key.text.empty() && key.text[0] == '&' ) { *why = "text_reference"; return false; }
        int32_t found = -1;
        for ( int32_t j = 0; j < info->num_fields; j++ ) { if ( key.text == info->fields[j].json ) { found = j; break; } }
        if ( found < 0 ) { *why = "text_unknown_key"; return false; }
        for ( size_t k = 0; k < seen.size(); k++ ) { if ( seen[k] == found ) { *why = "text_duplicate_key"; return false; } }
        seen.push_back( found );
        if ( wrote ) { out += ','; }
        TableTextFormJsonString( out, info->fields[found].json );
        out += ':';
        if ( !TableTextFormField( n.items[i + 1], &info->fields[found], r, out, why ) ) { return false; }
        wrote = true;
    }
    out += '}';
    return true;
}

// TableTextFormToJson parses one strict document and writes the canonical
// JSON of the same value (docs/SPEC-TABLES.md §16.9.1, §16.9.2). A refusal
// names itself in *why (§16.9.4).
inline bool TableTextFormToJson( const char * text, int64_t bytes, const TableTypeInfo * info, std::string & out, const char ** why )
{
    *why = NULL;
    TableTextFormReader reader;
    reader.s = text;
    reader.n = bytes;
    reader.i = 0;
    reader.why = why;
    reader.gap();
    TableTextFormNode root;
    if ( !reader.value( root, 0 ) ) { return false; }
    reader.gap();
    if ( reader.i != reader.n ) { *why = "text_syntax"; return false; }
    FormTextResolver resolver;
    if ( !TableTextFormRecord( root, info, resolver, out, why, 0, "" ) ) { return false; }
    return true;
}

// ---- hand-editable text form: end ----
`

// emitTextFormDeclarations puts one member's §16.9 reader on the header's
// text-form surface, beside the JSON entry points it rides on. The document
// read is the only operation this card carries (the writer and the Graph are
// their own cards, docs/SPEC-TABLES.md §16.9).
func (g *tableGen) emitTextFormDeclarations(st *ir.Struct) {
	if st.IsMapEntry() {
		return
	}
	if g.isVar(st.Name) {
		g.pf("// %sLoadTextForm reads one %s from the hand-editable text form\n", st.Name, st.Name)
		g.pf("// (docs/SPEC-TABLES.md §16.9): the strict s-expression document, resolved\n")
		g.pf("// against the field kinds and landed through the JSON walk. A refusal\n")
		g.pf("// names itself in *refusal (§16.9.4).\n")
		g.pf("bool %sLoadTextForm( const char * text, int64_t bytes, %sBuilder & builder, const char ** refusal );\n\n", st.Name, st.Name)
		return
	}
	g.pf("// %sLoadTextForm reads one %s from the hand-editable text form\n", st.Name, st.Name)
	g.pf("// (docs/SPEC-TABLES.md §16.9): the strict s-expression document, resolved\n")
	g.pf("// against the field kinds and landed through the JSON walk. A refusal\n")
	g.pf("// names itself in *refusal (§16.9.4).\n")
	g.pf("bool %sLoadTextForm( const char * text, int64_t bytes, %s & value, const char ** refusal );\n\n", st.Name, st.Name)
}

// emitTextFormDefinitions is the same member's definition: parse and resolve
// (TableTextFormToJson, §16.9.1/§16.9.2), then land the canonical JSON with
// the walk §16 already emits. A nonzero counter in that walk is refused, never
// trusted (§16.9.4): every clamp, skip and last-win was refused upstream.
func (g *tableGen) emitTextFormDefinitions(st *ir.Struct) {
	if st.IsMapEntry() {
		return
	}
	if g.isVar(st.Name) {
		g.pf("bool %sLoadTextForm( const char * text, int64_t bytes, %sBuilder & builder, const char ** refusal )\n{\n", st.Name, st.Name)
		g.pf("    std::string json;\n")
		g.pf("    if ( !TableTextFormToJson( text, bytes, %sTableType(), json, refusal ) ) { return false; }\n", st.Name)
		g.pf("    TableReport report;\n")
		g.pf("    if ( !%sFromJson( builder, json.data(), (int64_t) json.size(), &report ) || report.malformed || report.unknown != 0 || report.kind_mismatch != 0 || report.duplicate != 0 || report.clamped != 0 )\n", st.Name)
		g.pf("    {\n        *refusal = \"text_kind_mismatch\";\n        return false;\n    }\n")
		g.pf("    return true;\n}\n\n")
		return
	}
	g.pf("bool %sLoadTextForm( const char * text, int64_t bytes, %s & value, const char ** refusal )\n{\n", st.Name, st.Name)
	g.pf("    std::string json;\n")
	g.pf("    if ( !TableTextFormToJson( text, bytes, %sTableType(), json, refusal ) ) { return false; }\n", st.Name)
	g.pf("    TableReport report;\n")
	g.pf("    if ( !%sFromJson( value, json.data(), (int64_t) json.size(), &report ) || report.malformed || report.unknown != 0 || report.kind_mismatch != 0 || report.duplicate != 0 || report.clamped != 0 )\n", st.Name)
	g.pf("    {\n        *refusal = \"text_kind_mismatch\";\n        return false;\n    }\n")
	g.pf("    return true;\n}\n\n")
}

// tableTextFormWalk wraps the §16.9 runtime in the package guard so a unity
// build that concatenates several Table.cpp files defines it once.
func tableTextFormWalk(pkg string) string {
	guard := strings.ToUpper(pkg) + "_SCHEMA_TABLE_TEXTFORM"
	return "#ifndef " + guard + "\n#define " + guard + "\n\nnamespace " + pkg + " {\n\n" +
		tableTextFormRuntime + "\n} // namespace " + pkg + "\n\n#endif // " + guard + "\n"
}
