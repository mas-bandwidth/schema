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
// idiom: it is damage, malformed counts, and damage discards the whole load
// (docs/SPEC-TABLES.md §4).
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
// TERMINALLY on the packet wire, and the table read refuses it whole too:
// malformed counts, and damage discards the whole load (§4).
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
    int64_t start; // the token's first byte in the source document (§16.9.3)
    int64_t end;   // one past its last byte
};

// A DOCUMENT'S SPANS ARE ITS SOURCE COORDINATES (docs/SPEC-TABLES.md §16.9.3):
// a path names a value, and the span is that value's token only, so an edit
// replaces exactly those bytes and leaves every other byte alone. The span
// also carries the value the schema LANDED, which is the Go oracle's typed
// canon (internal/codegen/gotable/textform.go: tableTextFormNode.canon): a
// float32 keeps its bit pattern and an integer its raw scaled value, so a
// semantic no-op is decided after the field's conversion, never by the token's
// spelling or a host floating-point rendering of it.
struct tableTextFormSpan
{
    int64_t start;
    int64_t end;
    char kind;
    std::string canon;
};

struct tableTextFormSpans
{
    std::vector<std::string> paths;
    std::vector<tableTextFormSpan> spans;

    void add( const std::string & path, const TableTextFormNode & node )
    {
        for ( size_t i = 0; i < paths.size(); i++ )
        {
            if ( paths[i] == path ) { spans[i].start = node.start; spans[i].end = node.end; spans[i].kind = node.kind; return; }
        }
        paths.push_back( path );
        tableTextFormSpan span;
        span.start = node.start;
        span.end = node.end;
        span.kind = node.kind;
        span.canon = std::string();
        spans.push_back( span );
    }

    void canon( const std::string & path, const std::string & value )
    {
        for ( size_t i = 0; i < paths.size(); i++ ) { if ( paths[i] == path ) { spans[i].canon = value; return; } }
    }

    const tableTextFormSpan * find( const std::string & path ) const
    {
        for ( size_t i = 0; i < paths.size(); i++ ) { if ( paths[i] == path ) { return &spans[i]; } }
        return NULL;
    }
};

inline std::string tableTextFormJoin( const std::string & path, const std::string & key )
{
    if ( path.empty() ) { return key; }
    return path + "." + key;
}

inline std::string tableTextFormIndex( const std::string & path, size_t i )
{
    return path + "[" + std::to_string( (long long) i ) + "]";
}

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
        const int64_t begin = i;
        const char c = s[i];
        if ( c == '(' )
        {
            i++;
            node.kind = '(';
            node.start = begin;
            for ( ;; )
            {
                gap();
                if ( i >= n ) { return fail( "text_unterminated" ); }
                if ( s[i] == ')' ) { i++; node.end = i; return true; }
                TableTextFormNode child;
                if ( !value( child, depth + 1 ) ) { return false; }
                node.items.push_back( child );
            }
        }
        if ( c == '"' ) { node.kind = '"'; node.start = begin; const bool ok = quoted( node.text ); node.end = i; return ok; }
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
            node.start = begin;
            node.end = i;
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
        node.start = begin;
        node.end = i;
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

inline bool TableTextFormRecord( const TableTextFormNode & n, const TableTypeInfo * info, std::string & out, const char ** why, const std::string & path, tableTextFormSpans * spans );
inline bool TableTextFormField( const TableTextFormNode & n, const TableFieldInfo * f, std::string & out, const char ** why, const std::string & path, tableTextFormSpans * spans );

inline void tableTextFormMark( tableTextFormSpans * spans, const std::string & path, const TableTextFormNode & node )
{
    if ( spans != NULL ) { spans->add( path, node ); }
}

inline void tableTextFormCanon( tableTextFormSpans * spans, const std::string & path, const std::string & value )
{
    if ( spans != NULL ) { spans->canon( path, value ); }
}

// A NUMBER'S TYPED VALUE IS THE FIELD'S, NOT THE HOST'S (docs/SPEC-TABLES.md
// §16.9.3, Go internal/codegen/gotable/textform.go float and integer): the
// float kinds canonicalise the LANDED bit pattern, so 1.00000001 in a float32
// field is the same value as 1.0, and an integral or fixed kind canonicalises
// the token's EXACT decimal value, so 2, 2.0 and 20e-1 are one value and a
// dyadic fixed spelling 1.500000 is 1.5. Nothing here rounds through long
// double, which is what made a float32 no-op look like a change.
inline std::string tableTextFormCanonDecimal( const std::string & token )
{
    size_t i = 0;
    bool negative = false;
    if ( i < token.size() && ( token[i] == '-' || token[i] == '+' ) ) { negative = token[i] == '-'; i++; }
    std::string digits;
    int64_t exponent = 0;
    while ( i < token.size() && token[i] >= '0' && token[i] <= '9' ) { digits += token[i++]; }
    if ( i < token.size() && token[i] == '.' )
    {
        i++;
        while ( i < token.size() && token[i] >= '0' && token[i] <= '9' ) { digits += token[i++]; exponent--; }
    }
    if ( i < token.size() && ( token[i] == 'e' || token[i] == 'E' ) )
    {
        i++;
        bool exp_negative = false;
        if ( i < token.size() && ( token[i] == '-' || token[i] == '+' ) ) { exp_negative = token[i] == '-'; i++; }
        int64_t e = 0;
        while ( i < token.size() && token[i] >= '0' && token[i] <= '9' ) { if ( e < 1000000 ) { e = e * 10 + ( token[i] - '0' ); } i++; }
        exponent += exp_negative ? -e : e;
    }
    if ( digits.empty() ) { return token; }
    size_t lead = 0;
    while ( lead + 1 < digits.size() && digits[lead] == '0' ) { lead++; }
    digits = digits.substr( lead );
    if ( digits == "0" ) { return "0"; }
    size_t end = digits.size();
    while ( end > 1 && digits[end - 1] == '0' ) { end--; exponent++; }
    digits = digits.substr( 0, end );
    char buffer[32];
    snprintf( buffer, sizeof( buffer ), "e%lld", (long long) exponent );
    return std::string( negative ? "-" : "" ) + digits + buffer;
}

inline std::string tableTextFormCanonFloat( const std::string & token, bool wide )
{
    char buffer[32];
    if ( !wide )
    {
        const float value = (float) strtod( token.c_str(), NULL );
        uint32_t bits = 0;
        memcpy( &bits, &value, sizeof( bits ) );
        snprintf( buffer, sizeof( buffer ), "%llx", (unsigned long long) bits );
        return std::string( buffer );
    }
    const double value = strtod( token.c_str(), NULL );
    uint64_t bits = 0;
    memcpy( &bits, &value, sizeof( bits ) );
    snprintf( buffer, sizeof( buffer ), "%llx", (unsigned long long) bits );
    return std::string( buffer );
}

inline bool TableTextFormScalar( const TableTextFormNode & n, const TableFieldInfo * f, std::string & out, const char ** why, const std::string & path, tableTextFormSpans * spans )
{
    if ( f->optional && n.kind == 'z' ) { out += "null"; tableTextFormCanon( spans, path, "null" ); return true; }
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
            const std::string pp = tableTextFormIndex( path, i );
            tableTextFormMark( spans, pp, pair );
            if ( pair.kind != '(' || pair.items.size() != 2 ) { *why = "text_kind_mismatch"; return false; }
            tableTextFormMark( spans, tableTextFormIndex( pp, 0 ), pair.items[0] );
            tableTextFormCanon( spans, tableTextFormIndex( pp, 0 ), pair.items[0].text );
            if ( i > 0 ) { out += ','; }
            if ( key->kind == 12 ) { TableTextFormJsonString( out, pair.items[0].text ); }
            else { TableTextFormJsonString( out, pair.items[0].text ); }
            out += ':';
            for ( size_t k = 0; k < seen.size(); k++ ) { if ( seen[k] == pair.items[0].text ) { *why = "text_duplicate_key"; return false; } }
            seen.push_back( pair.items[0].text );
            if ( !TableTextFormField( pair.items[1], value, out, why, tableTextFormIndex( pp, 1 ), spans ) ) { return false; }
        }
        out += '}';
        return true;
    }
    if ( f->is_array && f->array_bound == 0 )
    {
        if ( n.kind != '(' ) { *why = "text_kind_mismatch"; return false; }
        out += '[';
        for ( size_t i = 0; i < n.items.size(); i++ )
        {
            if ( i > 0 ) { out += ','; }
            const std::string ip = tableTextFormIndex( path, i );
            tableTextFormMark( spans, ip, n.items[i] );
            if ( !TableTextFormScalar( n.items[i], f, out, why, ip, spans ) ) { return false; }
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
            const std::string sp = tableTextFormJoin( path, key.text );
            tableTextFormMark( spans, sp, key );
            tableTextFormMark( spans, sp, n.items[i + 1] );
            TableTextFormJsonString( out, key.text );
            out += ':';
            if ( !TableTextFormScalar( n.items[i + 1], &element, out, why, sp, spans ) ) { return false; }
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
            const std::string ap = tableTextFormJoin( path, key.text );
            tableTextFormMark( spans, ap, key );
            tableTextFormMark( spans, ap, n.items[i + 1] );
            TableTextFormJsonString( out, key.text );
            out += ':';
            const TableUnionArmInfo & arm = f->arms()->arms[tag];
            if ( arm.field != NULL ) { if ( !TableTextFormField( n.items[i + 1], arm.field, out, why, ap, spans ) ) { return false; } }
            else if ( arm.table != NULL ) { if ( !TableTextFormRecord( n.items[i + 1], arm.table, out, why, ap, spans ) ) { return false; } }
            else { if ( n.items[i + 1].kind != 'z' ) { *why = "text_kind_mismatch"; return false; } out += "null"; tableTextFormCanon( spans, ap, "null" ); }
        }
        out += '}';
        return true;
    }
    if ( f->kind == 17 )
    {
        if ( n.kind == 'z' ) { out += "null"; tableTextFormCanon( spans, path, "null" ); return true; }
        if ( f->table == NULL )
        {
            if ( n.kind != '"' ) { *why = "text_kind_mismatch"; return false; }
            TableTextFormJsonString( out, n.text );
            tableTextFormCanon( spans, path, n.text );
            return true;
        }
        return TableTextFormRecord( n, f->table, out, why, path, spans );
    }
    if ( f->kind == 13 ) { return TableTextFormRecord( n, f->table, out, why, path, spans ); }
    if ( f->variant_id != NULL )
    {
        if ( !TableTextFormKeyNode( n ) ) { *why = "text_kind_mismatch"; return false; }
        for ( int64_t v = 0; v <= f->enum_max; v++ )
        {
            const char * name = f->enum_name( (uint64_t) v );
            if ( name != NULL && n.text == name ) { TableTextFormJsonString( out, n.text ); tableTextFormCanon( spans, path, n.text ); return true; }
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
            tableTextFormMark( spans, tableTextFormIndex( path, i ), n.items[i] );
            tableTextFormCanon( spans, tableTextFormIndex( path, i ), n.items[i].text );
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
        tableTextFormCanon( spans, path, n.text );
        return true;
    }
    if ( f->kind == 1 )
    {
        if ( n.kind == 't' ) { out += "true"; tableTextFormCanon( spans, path, "true" ); return true; }
        if ( n.kind == 'f' ) { out += "false"; tableTextFormCanon( spans, path, "false" ); return true; }
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
    // THE TYPED CANON (docs/SPEC-TABLES.md §16.9.3, Go
    // internal/codegen/gotable/textform.go float and integer): a float kind
    // answers its landed bit pattern, every integral and fixed kind the token's
    // exact decimal value.
    if ( f->kind == 10 || f->kind == 11 ) { tableTextFormCanon( spans, path, tableTextFormCanonFloat( n.text, f->kind == 11 ) ); }
    else { tableTextFormCanon( spans, path, tableTextFormCanonDecimal( n.text ) ); }
    out += n.text;
    return true;
}

inline bool TableTextFormField( const TableTextFormNode & n, const TableFieldInfo * f, std::string & out, const char ** why, const std::string & path, tableTextFormSpans * spans )
{
    tableTextFormMark( spans, path, n );
    if ( f->optional && n.kind == 'z' ) { out += "null"; tableTextFormCanon( spans, path, "null" ); return true; }
    if ( f->is_array )
    {
        if ( f->kind == 6 && strcmp( f->type_name, "bytes" ) == 0 )
        {
            if ( n.kind != '"' ) { *why = "text_kind_mismatch"; return false; }
            int64_t dec = 0;
            if ( !TableB64Valid( n.text.c_str(), (int64_t) n.text.size(), dec ) ) { *why = "text_encoding"; return false; }
            if ( f->array_bound != 0 && dec > f->array_bound ) { *why = "text_bound"; return false; }
            TableTextFormJsonString( out, n.text );
            tableTextFormCanon( spans, path, n.text );
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
                const std::string ip = tableTextFormIndex( path, i );
                tableTextFormMark( spans, ip, n.items[i] );
                if ( !TableTextFormScalar( n.items[i], f, out, why, ip, spans ) ) { return false; }
            }
            out += ']';
            return true;
        }
    }
    return TableTextFormScalar( n, f, out, why, path, spans );
}

inline bool TableTextFormRecord( const TableTextFormNode & n, const TableTypeInfo * info, std::string & out, const char ** why, const std::string & path, tableTextFormSpans * spans )
{
    tableTextFormMark( spans, path, n );
    if ( n.kind != '(' || ( n.items.size() % 2 ) != 0 ) { *why = "text_kind_mismatch"; return false; }
    out += '{';
    std::vector<int32_t> seen;
    for ( size_t i = 0; i < n.items.size(); i += 2 )
    {
        const TableTextFormNode & key = n.items[i];
        if ( !TableTextFormKeyNode( key ) ) { *why = "text_syntax"; return false; }
        if ( !key.text.empty() && key.text[0] == '&' ) { *why = "text_reference"; return false; }
        int32_t found = -1;
        for ( int32_t j = 0; j < info->num_fields; j++ ) { if ( key.text == info->fields[j].json ) { found = j; break; } }
        if ( found < 0 ) { *why = "text_unknown_key"; return false; }
        for ( size_t k = 0; k < seen.size(); k++ ) { if ( seen[k] == found ) { *why = "text_duplicate_key"; return false; } }
        seen.push_back( found );
        const std::string fp = tableTextFormJoin( path, info->fields[found].json );
        tableTextFormMark( spans, fp, key );
        if ( i > 0 ) { out += ','; }
        TableTextFormJsonString( out, info->fields[found].json );
        out += ':';
        if ( !TableTextFormField( n.items[i + 1], &info->fields[found], out, why, fp, spans ) ) { return false; }
    }
    out += '}';
    return true;
}

// tableTextFormParse is the read half of the document: parse, resolve and
// write the canonical JSON, optionally recording every value's source span
// (docs/SPEC-TABLES.md §16.9.1, §16.9.2, §16.9.3).
inline bool tableTextFormParse( const char * text, int64_t bytes, const TableTypeInfo * info, std::string & out, const char ** why, tableTextFormSpans * spans )
{
    *why = NULL;
    TableTextFormReader r;
    r.s = text;
    r.n = bytes;
    r.i = 0;
    r.why = why;
    r.gap();
    TableTextFormNode root;
    if ( !r.value( root, 0 ) ) { return false; }
    r.gap();
    if ( r.i != r.n ) { *why = "text_syntax"; return false; }
    if ( !TableTextFormRecord( root, info, out, why, std::string(), spans ) ) { return false; }
    return true;
}

// TableTextFormToJson parses one strict document and writes the canonical
// JSON of the same value (docs/SPEC-TABLES.md §16.9.1, §16.9.2). A refusal
// names itself in *why (§16.9.4).
inline bool TableTextFormToJson( const char * text, int64_t bytes, const TableTypeInfo * info, std::string & out, const char ** why )
{
    return tableTextFormParse( text, bytes, info, out, why, NULL );
}

// tableTextFormApply splices the edits by ORIGINAL byte span, so an earlier
// replacement's new length never moves a later one (docs/SPEC-TABLES.md
// §16.9.3).
struct tableTextFormSplice
{
    int64_t start;
    int64_t end;
    std::string text;
    std::string path;
    std::string canon; // the ORIGINAL value's typed canon (§16.9.3)
};

inline std::string tableTextFormApply( const std::string & src, const std::vector<tableTextFormSplice> & splices )
{
    std::string out;
    size_t at = 0;
    for ( size_t i = 0; i < splices.size(); i++ )
    {
        out.append( src, at, (size_t) ( splices[i].start - (int64_t) at ) );
        out += splices[i].text;
        at = (size_t) splices[i].end;
    }
    out.append( src, at, std::string::npos );
    return out;
}

// tableTextFormEditText is the document writer's edit (§16.9.3): resolve the
// source, replace the named scalars' token spans only, re-read the result
// under the same schema, and keep the old spelling where the edit's value is a
// semantic no-op. A refusal leaves the old document untouched.
inline bool tableTextFormEditText( const std::string & document, const TableTypeInfo * info, const TableTextFormEdit * edits, int64_t count, std::string & edited, std::string & json, const char ** why )
{
    *why = NULL;
    tableTextFormSpans original;
    std::string originalJson;
    if ( !tableTextFormParse( document.data(), (int64_t) document.size(), info, originalJson, why, &original ) ) { return false; }
    std::vector<tableTextFormSplice> splices;
    for ( int64_t e = 0; e < count; e++ )
    {
        if ( edits[e].path == NULL || edits[e].text == NULL ) { *why = "text_edit_conflict"; return false; }
        const tableTextFormSpan * span = original.find( edits[e].path );
        if ( span == NULL || span->kind == '(' ) { *why = "text_edit_conflict"; return false; }
        TableTextFormReader r;
        r.s = edits[e].text;
        r.n = (int64_t) strlen( edits[e].text );
        r.i = 0;
        r.why = why;
        r.gap();
        TableTextFormNode value;
        if ( !r.value( value, 0 ) ) { return false; }
        r.gap();
        if ( value.kind == '(' || r.i != r.n ) { *why = "text_edit_conflict"; return false; }
        tableTextFormSplice splice;
        splice.start = span->start;
        splice.end = span->end;
        splice.text = edits[e].text;
        splice.path = edits[e].path;
        splice.canon = span->canon;
        splices.push_back( splice );
    }
    for ( size_t i = 1; i < splices.size(); i++ )
    {
        tableTextFormSplice hold = splices[i];
        size_t j = i;
        while ( j > 0 && splices[j - 1].start > hold.start ) { splices[j] = splices[j - 1]; j--; }
        splices[j] = hold;
    }
    for ( size_t i = 1; i < splices.size(); i++ )
    {
        if ( splices[i].start < splices[i - 1].end ) { *why = "text_edit_conflict"; return false; }
    }
    // Validate the whole edit first; a refusal leaves the old pair untouched.
    std::string allJson;
    const std::string all = tableTextFormApply( document, splices );
    if ( !tableTextFormParse( all.data(), (int64_t) all.size(), info, allJson, why, NULL ) ) { return false; }
    // A semantic no-op retains the old spelling (§16.9.3, Go
    // internal/codegen/gotable/textform.go tableTextFormEdit): compare the
    // LANDED value at the edited path, so a float32 that rounds the candidate
    // back to the old bit pattern and a fixed or integer spelling of the same
    // exact value both keep their bytes.
    std::vector<tableTextFormSplice> kept;
    for ( size_t i = 0; i < splices.size(); i++ )
    {
        std::vector<tableTextFormSplice> one( 1, splices[i] );
        const std::string candidate = tableTextFormApply( document, one );
        std::string candidateJson;
        tableTextFormSpans candidateSpans;
        if ( !tableTextFormParse( candidate.data(), (int64_t) candidate.size(), info, candidateJson, why, &candidateSpans ) ) { return false; }
        const tableTextFormSpan * after = candidateSpans.find( splices[i].path );
        if ( after == NULL || after->canon != splices[i].canon ) { kept.push_back( splices[i] ); }
    }
    edited = tableTextFormApply( document, kept );
    if ( !tableTextFormParse( edited.data(), (int64_t) edited.size(), info, json, why, NULL ) ) { return false; }
    return true;
}

// ---- hand-editable text form: end ----
`

// emitTextFormDeclarations puts one member's §16.9 read, write and edit on
// the header's text-form surface (docs/SPEC-TABLES.md §16.9.3). The document
// is the source bytes and their length: Load with a document retains those
// bytes, Save writes them back unchanged, and Edit replaces scalar token spans
// only. The header stays C-like — no std:: spelling — so the document is a
// (pointer, length) pair the caller keeps alive.
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
	} else {
		g.pf("// %sLoadTextForm reads one %s from the hand-editable text form\n", st.Name, st.Name)
		g.pf("// (docs/SPEC-TABLES.md §16.9): the strict s-expression document, resolved\n")
		g.pf("// against the field kinds and landed through the JSON walk. A refusal\n")
		g.pf("// names itself in *refusal (§16.9.4).\n")
		g.pf("bool %sLoadTextForm( const char * text, int64_t bytes, %s & value, const char ** refusal );\n\n", st.Name, st.Name)
	}
	g.pf("// %sLoadTextForm with the DOCUMENT retains the source bytes (§16.9.3):\n", st.Name)
	g.pf("// *document and *document_bytes are the caller's own text, so a read then\n")
	g.pf("// %sSaveTextForm with no edit is the input byte for byte.\n", st.Name)
	if g.isVar(st.Name) {
		g.pf("bool %sLoadTextForm( const char * text, int64_t bytes, const char ** document, int64_t * document_bytes, %sBuilder & builder, const char ** refusal );\n\n", st.Name, st.Name)
	} else {
		g.pf("bool %sLoadTextForm( const char * text, int64_t bytes, const char ** document, int64_t * document_bytes, %s & value, const char ** refusal );\n\n", st.Name, st.Name)
	}
	g.pf("// %sSaveTextForm writes the document a Load or Edit returned\n", st.Name)
	g.pf("// (docs/SPEC-TABLES.md §16.9.3): the retained bytes, so a read then a\n")
	g.pf("// write with no edit is the input byte for byte. It answers the byte\n")
	g.pf("// count, or -1 when capacity cannot hold the document.\n")
	g.pf("int64_t %sSaveTextForm( const char * document, int64_t document_bytes, char * buffer, int64_t capacity );\n\n", st.Name)
	g.pf("// %sEditTextForm replaces existing scalar values (§16.9.3): each edit's\n", st.Name)
	g.pf("// token span and nothing else, all spans taken from the original document,\n")
	g.pf("// a semantic no-op keeping the old spelling. On success *edited_bytes is\n")
	g.pf("// the edited document's length; a short buffer answers false with\n")
	g.pf("// *edited_bytes set to what it needs. A refusal names itself in *refusal\n")
	g.pf("// and leaves the old document untouched.\n")
	if g.isVar(st.Name) {
		g.pf("bool %sEditTextForm( const char * document, int64_t document_bytes, const TableTextFormEdit * edits, int64_t count, char * buffer, int64_t capacity, int64_t * edited_bytes, %sBuilder & builder, const char ** refusal );\n\n", st.Name, st.Name)
	} else {
		g.pf("bool %sEditTextForm( const char * document, int64_t document_bytes, const TableTextFormEdit * edits, int64_t count, char * buffer, int64_t capacity, int64_t * edited_bytes, %s & value, const char ** refusal );\n\n", st.Name, st.Name)
	}
}

// emitTextFormDefinitions is the same member's definition: parse and resolve
// (TableTextFormToJson, §16.9.1/§16.9.2), then land the canonical JSON with
// the walk §16 already emits. A nonzero counter in that walk is refused, never
// trusted (§16.9.4): every clamp, skip and last-win was refused upstream.
// The write and edit are §16.9.3's: the retained source, and one scalar's
// token span changed at a time.
func (g *tableGen) emitTextFormDefinitions(st *ir.Struct) {
	if st.IsMapEntry() {
		return
	}
	loadDoc := func(storage string) {
		g.pf("    if ( !%sLoadTextForm( text, bytes, %s, refusal ) ) { return false; }\n", st.Name, storage)
		g.pf("    if ( document != NULL ) { *document = text; }\n")
		g.pf("    if ( document_bytes != NULL ) { *document_bytes = bytes; }\n")
		g.pf("    return true;\n}\n\n")
	}
	if g.isVar(st.Name) {
		g.pf("bool %sLoadTextForm( const char * text, int64_t bytes, %sBuilder & builder, const char ** refusal )\n{\n", st.Name, st.Name)
		g.pf("    std::string json;\n")
		g.pf("    if ( !TableTextFormToJson( text, bytes, %sTableType(), json, refusal ) ) { return false; }\n", st.Name)
		g.pf("    TableReport report;\n")
		g.pf("    if ( !%sFromJson( builder, json.data(), (int64_t) json.size(), &report ) || report.malformed || report.unknown != 0 || report.kind_mismatch != 0 || report.duplicate != 0 || report.clamped != 0 )\n", st.Name)
		g.pf("    {\n        *refusal = \"text_kind_mismatch\";\n        return false;\n    }\n")
		g.pf("    return true;\n}\n\n")
		g.pf("bool %sLoadTextForm( const char * text, int64_t bytes, const char ** document, int64_t * document_bytes, %sBuilder & builder, const char ** refusal )\n{\n", st.Name, st.Name)
		loadDoc("builder")
		g.pf("int64_t %sSaveTextForm( const char * document, int64_t document_bytes, char * buffer, int64_t capacity )\n{\n", st.Name)
		saveBody(g, st)
		g.pf("bool %sEditTextForm( const char * document, int64_t document_bytes, const TableTextFormEdit * edits, int64_t count, char * buffer, int64_t capacity, int64_t * edited_bytes, %sBuilder & builder, const char ** refusal )\n{\n", st.Name, st.Name)
		g.pf("    std::string edited;\n    std::string json;\n")
		g.pf("    if ( !tableTextFormEditText( std::string( document, (size_t) document_bytes ), %sTableType(), edits, count, edited, json, refusal ) ) { return false; }\n", st.Name)
		g.pf("    if ( edited_bytes != NULL ) { *edited_bytes = (int64_t) edited.size(); }\n")
		g.pf("    if ( buffer == NULL || capacity < (int64_t) edited.size() ) { return false; }\n")
		g.pf("    if ( !edited.empty() ) { memcpy( buffer, edited.data(), edited.size() ); }\n")
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
	g.pf("bool %sLoadTextForm( const char * text, int64_t bytes, const char ** document, int64_t * document_bytes, %s & value, const char ** refusal )\n{\n", st.Name, st.Name)
	loadDoc("value")
	g.pf("int64_t %sSaveTextForm( const char * document, int64_t document_bytes, char * buffer, int64_t capacity )\n{\n", st.Name)
	saveBody(g, st)
	g.pf("bool %sEditTextForm( const char * document, int64_t document_bytes, const TableTextFormEdit * edits, int64_t count, char * buffer, int64_t capacity, int64_t * edited_bytes, %s & value, const char ** refusal )\n{\n", st.Name, st.Name)
	g.pf("    std::string edited;\n    std::string json;\n")
	g.pf("    if ( !tableTextFormEditText( std::string( document, (size_t) document_bytes ), %sTableType(), edits, count, edited, json, refusal ) ) { return false; }\n", st.Name)
	g.pf("    if ( edited_bytes != NULL ) { *edited_bytes = (int64_t) edited.size(); }\n")
	g.pf("    if ( buffer == NULL || capacity < (int64_t) edited.size() ) { return false; }\n")
	g.pf("    if ( !edited.empty() ) { memcpy( buffer, edited.data(), edited.size() ); }\n")
	g.pf("    TableReport report;\n")
	g.pf("    if ( !%sFromJson( value, json.data(), (int64_t) json.size(), &report ) || report.malformed || report.unknown != 0 || report.kind_mismatch != 0 || report.duplicate != 0 || report.clamped != 0 )\n", st.Name)
	g.pf("    {\n        *refusal = \"text_kind_mismatch\";\n        return false;\n    }\n")
	g.pf("    return true;\n}\n\n")
}

// saveBody is the read-then-write identity (§16.9.3): the retained bytes and
// nothing else, or -1 when the caller's buffer cannot hold them.
func saveBody(g *tableGen, st *ir.Struct) {
	g.pf("    if ( document == NULL || document_bytes < 0 || buffer == NULL || capacity < document_bytes ) { return -1; }\n")
	g.pf("    if ( document_bytes > 0 ) { memcpy( buffer, document, (size_t) document_bytes ); }\n")
	g.pf("    return document_bytes;\n}\n\n")
}

// tableTextFormWalk wraps the §16.9 runtime in the package guard so a unity
// build that concatenates several Table.cpp files defines it once.
func tableTextFormWalk(pkg string) string {
	guard := strings.ToUpper(pkg) + "_SCHEMA_TABLE_TEXTFORM"
	return "#ifndef " + guard + "\n#define " + guard + "\n\nnamespace " + pkg + " {\n\n" +
		tableTextFormRuntime + "\n} // namespace " + pkg + "\n\n#endif // " + guard + "\n"
}
