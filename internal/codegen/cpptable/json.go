// The TEXT form: JSON in and out of one table, driven by the reflection
// descriptors (docs/SPEC-TABLES.md §16).
//
// ONE generic walk, emitted once per generated header behind the same macro
// guard the primitives use — not a per-table codec. That is the property
// which makes the text form schema's rather than a packer's, and it is a
// gate: the walker's source is the SAME BYTES in every generated header of
// the corpus (`make tables-json-walk`). Everything a walk needs about a
// table is already in §8's descriptors; the per-table surface is three thin
// wrappers that name a descriptor and nothing else.
package cpptable

import (
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// tableJsonWalk wraps the walker in its package guard. The package name is in
// the guard and the namespace and NOWHERE inside the markers, so the gate
// between them is a strict byte comparison across every header of the corpus.
//
// The walk meets a POINTER through three adapters it declares and does not
// define (docs/SPEC-TABLES.md §16.7). Their definitions sit outside the gated
// region and say what the UNIT is: a unit that declares no pointer answers
// with three stubs no field ever reaches, and a pointered unit answers with the
// graph half — the builder's reader, the region's writer and the `&node` map —
// which carries its own gate across the pointered units of the corpus.
func tableJsonWalk(pkg string, variable bool) string {
	guard := strings.ToUpper(pkg) + "_SCHEMA_TABLE_JSON"
	adapters := tableJsonFixedAdapters
	if variable {
		adapters = tableJsonGraphSource
	}
	// The include guard is LOAD-BEARING in a .cpp, which is not where a reader
	// expects to find one — hence the comment riding with it. It is what lets
	// several same-package .cpp files be concatenated into one translation
	// unit, the unity build a game project reaches for, without redefining
	// every walker function. The comment sits OUTSIDE the walk's markers, with
	// the guard and the namespace, so the package name never enters the region
	// the generic-walk gate compares.
	return "// The guard is not vestigial. Several " + pkg + " Table.cpp files may be\n" +
		"// concatenated into ONE translation unit — a unity build — and without it\n" +
		"// each would redefine the walk. It is also why the walk's functions may be\n" +
		"// weak (vague linkage) across separate objects: ODR requires their\n" +
		"// definitions to be token-identical, and the generic-walk gate is what\n" +
		"// proves that, byte for byte, across every generated .cpp.\n" +
		"#ifndef " + guard + "\n#define " + guard + "\n\nnamespace " + pkg + " {\n\n" +
		tableJsonAdapterDeclarations +
		tableJsonWalkSource +
		"\n" + adapters +
		"\n} // namespace " + pkg + "\n\n#endif // " + guard + "\n"
}

// tableJsonAdapterDeclarations is what the walk knows about a pointer: three
// functions it calls at a `*T` slot and at an `&`-prefixed key, declared here
// and defined after the walk by whichever half the unit carries.
const tableJsonAdapterDeclarations = `// ---- the pointer adapters (docs/SPEC-TABLES.md §16.7) ----
//
// The walk below is ONE walk, byte-identical in every generated .cpp, and a
// pointer is the one kind it cannot walk alone: reading one needs the
// builder's arena and writing one needs a region's deref, and neither exists
// in a unit that declares no pointer. So the walk calls these three and does
// not define them. A unit with no pointer defines them as stubs no field ever
// reaches; a pointered unit defines them in the graph half that follows the
// walk.

struct TableJsonIn;
struct TableJsonOut;

// a pointer field's object, or the ` + "`&node`" + ` reference standing in for it, into
// the slot; the cursor is on the opening brace
inline bool TableJsonReadPointer( TableJsonIn & in, void * slot, const TableFieldInfo * f, int32_t depth );
// the node a pointer slot names, in place — or as ` + "`&node`" + ` when it is shared
inline bool TableJsonWritePointer( TableJsonOut & out, const void * slot, const TableFieldInfo * f, int32_t depth );
// the FIRST key of an object the walk is skipping begins with ` + "`&`" + `: the cursor is
// on its value. A dropped definition still takes its label (§16.7); a fixed reader
// skips the value whole, as it skips everything else it does not place.
inline bool TableJsonSkippedAmpersand( TableJsonIn & in, const char * key, int32_t depth );

`

// tableJsonFixedAdapters answers for a unit that declares no pointer: no field
// has kind 17, so the two slot adapters are never reached, and an `&` key in a
// skipped value is the reserved prefix meeting a reader that cannot honor it.
const tableJsonFixedAdapters = `// ---- this unit declares no pointer ----
//
// Every field's kind is one the walk above places by itself, so the two slot
// adapters are unreachable and say so. The prefix adapter is reachable — a
// text may carry ` + "`&node`" + ` inside a value this reader skips — and the value is
// skipped whole: what a reader does not place it does not police, which is
// §4's tolerance (docs/SPEC-TABLES.md §16.7).

inline bool TableJsonReadPointer( TableJsonIn & in, void *, const TableFieldInfo *, int32_t )
{
    in.report->malformed = true;
    in.bad = true;
    return false;
}

inline bool TableJsonWritePointer( TableJsonOut &, const void *, const TableFieldInfo *, int32_t )
{
    return false;
}

inline bool TableJsonSkippedAmpersand( TableJsonIn & in, const char *, int32_t depth )
{
    return TableJsonSkipValue( in, depth + 1 );
}
`

// emitJsonDeclarations puts one closure member's text-form surface in the
// HEADER: three declarations and nothing else. The definitions, and the walker
// they call, live in the generated <Base>Table.cpp — so a translation unit
// that includes the header to use the wire codecs or the descriptors pays
// nothing for a form it never calls (docs/SPEC-TABLES.md §16.1, owner's ruling).
//
// A VARIABLE-LENGTH member is never held by value, so its three take the forms
// the class is held in (§16.7): FromJson reads into a BUILDER, whose arena is
// where every node comes from, and ToJson writes from the CONST root of a
// region, which is what Lock and Load both produce — with the allocator its
// identity map uses, defaulted to the program's pair as Measure and Save are.
func (g *tableGen) emitJsonDeclarations(st *ir.Struct) {
	if g.isVar(st.Name) {
		g.pf("// %s in and out of a JSON text (docs/SPEC-TABLES.md §16.7): read into a\n", st.Name)
		g.pf("// builder, written from a region's const root. A node named more than once\n")
		g.pf("// carries `&node` in the text. Defined in %sTable.cpp; link it to use them.\n", g.file.Base)
		g.pf("bool %sFromJson( %sBuilder & builder, const char * text, int64_t bytes, TableReport * report );\n", st.Name, st.Name)
		g.pf("int64_t %sToJsonMeasure( const %s * root, TableAllocator allocator = TableDefaultAllocator() );\n", st.Name, st.Name)
		g.pf("int64_t %sToJson( const %s * root, char * buffer, int64_t capacity, TableAllocator allocator = TableDefaultAllocator() );\n\n", st.Name, st.Name)
		return
	}
	g.pf("// %s in and out of a JSON text — one instance, one text, the generic\n", st.Name)
	g.pf("// walk over this type's descriptors (docs/SPEC-TABLES.md §16). Defined in\n")
	g.pf("// %sTable.cpp; link it to use them.\n", g.file.Base)
	g.pf("bool %sFromJson( %s & value, const char * text, int64_t bytes, TableReport * report );\n", st.Name, st.Name)
	g.pf("int64_t %sToJsonMeasure( const %s & value );\n", st.Name, st.Name)
	g.pf("int64_t %sToJson( const %s & value, char * buffer, int64_t capacity );\n\n", st.Name, st.Name)
}

// emitJsonDefinitions puts the same member's three definitions in the .cpp,
// each a thin wrapper naming a descriptor and nothing else.
func (g *tableGen) emitJsonDefinitions(st *ir.Struct) {
	if g.isVar(st.Name) {
		g.pf("bool %sFromJson( %sBuilder & builder, const char * text, int64_t bytes, TableReport * report )\n{\n", st.Name, st.Name)
		g.pf("    %s * root = builder.GetRoot();\n", st.Name)
		g.pf("    if ( root == NULL ) { if ( report != NULL ) { report->malformed = true; } return false; } // locked, or the root allocation failed\n")
		g.pf("    return TableJsonReadGraph( builder.main, root, %sTableType(), text, bytes, report );\n}\n\n", st.Name)
		g.pf("int64_t %sToJsonMeasure( const %s * root, TableAllocator allocator )\n{\n", st.Name, st.Name)
		g.pf("    return TableJsonWriteGraph( root, %sTableType(), NULL, 0, allocator );\n}\n\n", st.Name)
		g.pf("int64_t %sToJson( const %s * root, char * buffer, int64_t capacity, TableAllocator allocator )\n{\n", st.Name, st.Name)
		g.pf("    return TableJsonWriteGraph( root, %sTableType(), buffer, capacity, allocator );\n}\n\n", st.Name)
		return
	}
	g.pf("bool %sFromJson( %s & value, const char * text, int64_t bytes, TableReport * report )\n{\n", st.Name, st.Name)
	g.pf("    return TableJsonRead( &value, %sTableType(), text, bytes, report );\n}\n\n", st.Name)
	g.pf("int64_t %sToJsonMeasure( const %s & value )\n{\n", st.Name, st.Name)
	g.pf("    return TableJsonWrite( &value, %sTableType(), NULL, 0 );\n}\n\n", st.Name)
	g.pf("int64_t %sToJson( const %s & value, char * buffer, int64_t capacity )\n{\n", st.Name, st.Name)
	g.pf("    return TableJsonWrite( &value, %sTableType(), buffer, capacity );\n}\n\n", st.Name)
}

// tableJsonWalkSource is the walker. It reads and writes ONLY the columns
// every generated header carries, so its text never varies with the unit —
// which is what the generic-walk gate asserts.
const tableJsonWalkSource = `// ---- json walk: begin ----
//
// The TEXT form (docs/SPEC-TABLES.md §16): one table, one text, one walk over the
// reflection descriptors (§8). Reading fills ONE caller-owned instance and
// allocates nothing beyond it; writing targets a caller buffer with the
// wire's measure/write symmetry. Everything AROUND this — which file goes
// with which instance, what key an instance is filed under, how instances
// link into a root table's collections — is a packer's opinion and stays
// with the tool that holds it.
//
// The dialect: trailing commas are accepted on read (the authoring files
// this exists for carry them) and never written; comments are not JSON and
// are refused; unknown keys are skipped and counted; a duplicate key is
// last-wins and counted; a key present with the wrong JSON type is skipped
// and counted, never coerced.

static const int32_t kTableJsonMaxDepth = 128;

// A key longer than this cannot name a field, so it is skipped as unknown.
static const int32_t kTableJsonMaxKey = 256;

// The longest numeric token the walk will convert. Anything longer is a
// value no field can hold and counts as a kind mismatch.
static const int32_t kTableJsonMaxNumber = 512;

// The decimal point the C runtime is CURRENTLY using. Number conversion is
// the one locale-sensitive corner of the grammar — JSON's point is always
// '.', the runtime's is whatever the program set — so every number crosses
// this one character on the way out and on the way back in. Nothing else in
// the walk consults the locale.
inline char TableJsonDecimalPoint()
{
    const struct lconv * conv = localeconv();
    if ( conv != NULL && conv->decimal_point != NULL && conv->decimal_point[0] != 0 )
    {
        return conv->decimal_point[0];
    }
    return '.';
}

// ---- storage access: the descriptors give an offset and a width, and the
// ---- storage is the HOST's, so every load and store goes through a width
// ---- switch rather than a memcpy into the low bytes of a wider word

// finite: not a NaN, not an infinity. Written without <cmath> — the walk's
// runtime surface stays the handful of functions it already names.
// A vocabulary entry the descriptor could not spell. The generated name
// functions answer "???" for a value outside the declared set, and that is
// not a name — writing it would put a spelling in the text that the reader
// then counts as unknown, turning a refusal into a silent loss.
inline bool TableJsonNamed( const char * name )
{
    return name != NULL && strcmp( name, "???" ) != 0;
}

inline bool TableJsonFinite( double v )
{
    return v == v && v <= 1.7976931348623157e308 && v >= -1.7976931348623157e308;
}

inline uint64_t TableJsonGetRaw( const void * storage, uint32_t width )
{
    switch ( width )
    {
        case 1: { uint8_t v = 0;  memcpy( &v, storage, 1 ); return v; }
        case 2: { uint16_t v = 0; memcpy( &v, storage, 2 ); return v; }
        case 4: { uint32_t v = 0; memcpy( &v, storage, 4 ); return v; }
        case 8: { uint64_t v = 0; memcpy( &v, storage, 8 ); return v; }
    }
    return 0;
}

inline void TableJsonSetRaw( void * storage, uint32_t width, uint64_t value )
{
    switch ( width )
    {
        case 1: { uint8_t v = (uint8_t) value;   memcpy( storage, &v, 1 ); break; }
        case 2: { uint16_t v = (uint16_t) value; memcpy( storage, &v, 2 ); break; }
        case 4: { uint32_t v = (uint32_t) value; memcpy( storage, &v, 4 ); break; }
        case 8: { uint64_t v = value;            memcpy( storage, &v, 8 ); break; }
    }
}

inline int64_t TableJsonGetSigned( const void * storage, uint32_t width )
{
    uint64_t raw = TableJsonGetRaw( storage, width );
    if ( width < 8 )
    {
        uint64_t sign = uint64_t( 1 ) << ( width * 8 - 1 );
        if ( ( raw & sign ) != 0 )
        {
            raw |= ~( ( sign << 1 ) - 1 );
        }
    }
    return (int64_t) raw;
}

// a counted field's companion: a string's length, a bytes' length, a counted
// array's count. Bounded by the declared extent on the way out, so a storage
// invariant a caller broke cannot walk off the end of the array.
inline int32_t TableJsonCount( const void * base, const TableFieldInfo * f )
{
    if ( !f->counted )
    {
        return f->array_bound;
    }
    int32_t count = 0;
    memcpy( &count, (const uint8_t *) base + f->count_offset, sizeof( count ) );
    if ( count < 0 ) { count = 0; }
    if ( count > f->array_bound ) { count = f->array_bound; }
    return count;
}

inline void TableJsonSetCount( void * base, const TableFieldInfo * f, int32_t count )
{
    if ( f->counted )
    {
        memcpy( (uint8_t *) base + f->count_offset, &count, sizeof( count ) );
    }
}

// ---- what a field's kind expects to see in the text ----
//
// One classifier, consulted by both directions, so a reader and a writer can
// never disagree about a kind's JSON form. 'o' object, 'a' array, 's'
// string, 'n' number, 'b' boolean.
//
// A vocabulary field is spelled by NAME: an enum is one name, a flags mask
// is the array of the names of its set bits. The two are told apart by the
// id column — an enum variant rides under a wire id, a flags BIT never does
// (docs/SPEC-TABLES.md §4), so a name function with no id function is flags.
//
// bytes(N) is the one kind whose element kind does not decide its form: it
// shares u8 with a plain array of u8, and rides as base64. The schema type
// name settles it, and "bytes" is a keyword no declaration can claim.
inline bool TableJsonIsBytes( const TableFieldInfo * f )
{
    return f->is_array && f->kind == 6 && strcmp( f->type_name, "bytes" ) == 0;
}

// An ENUM-KEYED array (docs/SPEC-TABLES.md §2.4): its JSON form is an OBJECT
// keyed by variant name, not a positional array, because that is what the
// storage is — one slot per variant, addressed by the variant.
inline bool TableJsonIsKeyed( const TableFieldInfo * f )
{
    return f->key_name != NULL;
}

// THE KEY A STORAGE SLOT HOLDS (§2.4, §8): the storage shifts left, so slot i
// holds the key i + 1 and nothing is stored for None. This is the ONE place
// the walker spells the shift.
inline uint64_t TableJsonKeyedSlotKey( int64_t slot )
{
    return (uint64_t) ( slot + 1 );
}

// A slot whose key names a variant of the keying enum. Every slot in
// [0, array_bound) does, unless the enum carries max-headroom variants outside
// a table closure, where a reserved value names nothing and its key id is 0 —
// the reserved id no declared name can fold to (§5).
inline bool TableJsonKeyedSlotValid( const TableFieldInfo * f, int64_t slot )
{
    return f->key_id( TableJsonKeyedSlotKey( slot ) ) != 0;
}

inline bool TableJsonIsFlags( const TableFieldInfo * f )
{
    return f->enum_name != NULL && f->variant_id == NULL;
}

inline bool TableJsonIsEnum( const TableFieldInfo * f )
{
    return f->variant_id != NULL && f->arms == NULL;
}

inline char TableJsonShape( const TableFieldInfo * f )
{
    if ( f->kind == 12 ) return 's';           // string
    if ( TableJsonIsBytes( f ) ) return 's';   // bytes: base64
    if ( TableJsonIsKeyed( f ) ) return 'o';   // an object keyed by variant NAME
    if ( f->is_array ) return 'a';
    if ( f->arms != NULL ) return 'o';         // union: an object with ONE key
    if ( f->kind == 13 ) return 'o';           // nested table or type
    if ( f->kind == 17 ) return 'o';           // a pointer: the pointee's object in place, or null (§16.7)
    if ( TableJsonIsEnum( f ) ) return 's';
    if ( TableJsonIsFlags( f ) ) return 'a';
    if ( f->kind == 1 ) return 'b';
    return 'n';
}

// the ELEMENT shape of an array field — the same classifier one level down
inline char TableJsonElementShape( const TableFieldInfo * f )
{
    if ( f->kind == 13 ) return 'o';
    if ( TableJsonIsEnum( f ) ) return 's';
    if ( TableJsonIsFlags( f ) ) return 'a';
    if ( f->kind == 1 ) return 'b';
    return 'n';
}

// A guarded group rides only when its guard reads true — the wire's own
// elision (§4), carried into the text so a text and a wire written from one
// instance say the same thing. The guard is spelled as its branch condition
// over bool fields of the SAME type ("at_rest", "!at_rest",
// "active && has_target"), so evaluating it is a walk of the same
// descriptor. Nothing is inferred in the other direction: reading places
// every key it can name, and the guard is a plain bool key (§16.2).
inline bool TableJsonGuardHolds( const void * base, const TableTypeInfo * info, const char * guard )
{
    const char * p = guard;
    for ( ;; )
    {
        while ( *p == ' ' || *p == '&' ) { p++; }
        if ( *p == 0 ) { return true; }
        bool want = true;
        if ( *p == '!' ) { want = false; p++; }
        const char * start = p;
        while ( *p != 0 && *p != ' ' && *p != '&' ) { p++; }
        size_t length = (size_t) ( p - start );
        bool value = false;
        for ( int32_t i = 0; i < info->num_fields; i++ )
        {
            const TableFieldInfo * f = &info->fields[i];
            if ( strlen( f->name ) == length && strncmp( f->name, start, length ) == 0 )
            {
                value = TableJsonGetRaw( (const uint8_t *) base + f->offset, f->elem_size ) != 0;
                break;
            }
        }
        if ( value != want ) { return false; }
    }
}

// ---- writing ----

// The writer sink MEASURES when the buffer is NULL and WRITES when it is
// not, over one code path — so measure and write agree byte for byte, the
// wire's invariant (§9) carried across.
struct TableJsonOut
{
    char * buffer;
    int64_t capacity;
    int64_t offset;
    bool overflow;
    void * graph; // the pointered write's identity map (§16.7); NULL for a fixed table

    void raw( const char * data, int64_t count )
    {
        if ( buffer != NULL )
        {
            if ( offset + count > capacity ) { overflow = true; return; }
            memcpy( buffer + offset, data, (size_t) count );
        }
        offset += count;
    }
    void put( char c ) { raw( &c, 1 ); }
    void text( const char * s ) { raw( s, (int64_t) strlen( s ) ); }
    void line( int32_t depth )
    {
        put( '\n' );
        for ( int32_t i = 0; i < depth; i++ ) { raw( "  ", 2 ); }
    }
};

inline const char * TableJsonBase64Alphabet()
{
    return "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
}

inline void TableJsonWriteBase64( TableJsonOut & out, const uint8_t * data, int32_t length )
{
    const char * alphabet = TableJsonBase64Alphabet();
    out.put( '"' );
    int32_t i = 0;
    for ( ; i + 3 <= length; i += 3 )
    {
        uint32_t triple = ( uint32_t( data[i] ) << 16 ) | ( uint32_t( data[i+1] ) << 8 ) | uint32_t( data[i+2] );
        char quad[4] = { alphabet[ ( triple >> 18 ) & 0x3f ], alphabet[ ( triple >> 12 ) & 0x3f ],
                         alphabet[ ( triple >> 6 ) & 0x3f ], alphabet[ triple & 0x3f ] };
        out.raw( quad, 4 );
    }
    if ( i < length )
    {
        int32_t left = length - i;
        uint32_t triple = uint32_t( data[i] ) << 16;
        if ( left == 2 ) { triple |= uint32_t( data[i+1] ) << 8; }
        char quad[4] = { alphabet[ ( triple >> 18 ) & 0x3f ], alphabet[ ( triple >> 12 ) & 0x3f ], '=', '=' };
        if ( left == 2 ) { quad[2] = alphabet[ ( triple >> 6 ) & 0x3f ]; }
        out.raw( quad, 4 );
    }
    out.put( '"' );
}

// One UTF-8 sequence at s, or -1 when the bytes there are not one. Rejects
// the lot: a stray continuation, an overlong form, a surrogate half, and
// anything past U+10FFFF.
inline int32_t TableJsonUtf8( const char * s, int32_t remaining, int32_t * width )
{
    unsigned char lead = (unsigned char) s[0];
    int32_t want = 0;
    int32_t code = 0;
    if ( lead < 0x80 ) { *width = 1; return lead; }
    else if ( lead >= 0xc2 && lead <= 0xdf ) { want = 2; code = lead & 0x1f; }
    else if ( lead >= 0xe0 && lead <= 0xef ) { want = 3; code = lead & 0x0f; }
    else if ( lead >= 0xf0 && lead <= 0xf4 ) { want = 4; code = lead & 0x07; }
    else { return -1; }
    if ( remaining < want ) { return -1; }
    for ( int32_t i = 1; i < want; i++ )
    {
        unsigned char next = (unsigned char) s[i];
        if ( ( next & 0xc0 ) != 0x80 ) { return -1; }
        code = ( code << 6 ) | ( next & 0x3f );
    }
    if ( want == 3 && code < 0x800 ) { return -1; }          // overlong
    if ( want == 4 && code < 0x10000 ) { return -1; }        // overlong
    if ( code >= 0xd800 && code <= 0xdfff ) { return -1; }   // a surrogate half
    if ( code > 0x10ffff ) { return -1; }
    *width = want;
    return code;
}

// A JSON text MUST be valid UTF-8 (RFC 8259 §8.1). The read path is
// byte-transparent — the wire imposes no encoding (§3) and a string may hold
// anything — so the WRITER is where that obligation is met: a byte that is
// not part of a well-formed sequence is written as U+FFFD, one per bad byte,
// and never raw. A text this walk writes is therefore readable by any
// conforming parser, which a raw byte would not be. The cost is stated
// plainly: for a string holding invalid UTF-8, the round trip is NOT
// byte-identical, because the alternative is emitting a text that is not
// JSON.
inline void TableJsonWriteString( TableJsonOut & out, const char * s, int32_t length )
{
    static const char hex[] = "0123456789abcdef";
    out.put( '"' );
    for ( int32_t i = 0; i < length; i++ )
    {
        unsigned char c = (unsigned char) s[i];
        switch ( c )
        {
            case '"':  out.raw( "\\\"", 2 ); break;
            case '\\': out.raw( "\\\\", 2 ); break;
            case '\b': out.raw( "\\b", 2 ); break;
            case '\f': out.raw( "\\f", 2 ); break;
            case '\n': out.raw( "\\n", 2 ); break;
            case '\r': out.raw( "\\r", 2 ); break;
            case '\t': out.raw( "\\t", 2 ); break;
            default:
                if ( c < 0x20 )
                {
                    char escape[6] = { '\\', 'u', '0', '0', hex[ c >> 4 ], hex[ c & 0xf ] };
                    out.raw( escape, 6 );
                }
                else if ( c < 0x80 )
                {
                    out.put( (char) c );
                }
                else
                {
                    int32_t width = 0;
                    if ( TableJsonUtf8( s + i, length - i, &width ) < 0 )
                    {
                        out.raw( "\xef\xbf\xbd", 3 ); // U+FFFD, one per bad byte
                    }
                    else
                    {
                        out.raw( s + i, width );
                        i += width - 1;
                    }
                }
                break;
        }
    }
    out.put( '"' );
}

inline void TableJsonWriteUnsigned( TableJsonOut & out, uint64_t value )
{
    char digits[24];
    int32_t n = 0;
    do
    {
        digits[n++] = (char) ( '0' + (int) ( value % 10 ) );
        value /= 10;
    } while ( value != 0 );
    char text[24];
    for ( int32_t i = 0; i < n; i++ ) { text[i] = digits[n - 1 - i]; }
    out.raw( text, n );
}

inline void TableJsonWriteSigned( TableJsonOut & out, int64_t value )
{
    if ( value < 0 )
    {
        out.put( '-' );
        TableJsonWriteUnsigned( out, uint64_t( 0 ) - (uint64_t) value );
        return;
    }
    TableJsonWriteUnsigned( out, (uint64_t) value );
}

// A float writes at the SHORTEST precision that reads back as the same value
// at the field's own width, so a round trip is exact and a text stays
// readable. Non-finite values have no JSON spelling at all, and the writer
// REFUSES rather than losing one silently — the same rule measure and save
// already apply to an enum value no variant names (§5).
inline bool TableJsonWriteFloat( TableJsonOut & out, double value, bool single )
{
    if ( !TableJsonFinite( value ) ) { return false; }
    char text[64];
    int low = single ? 6 : 15;
    int high = single ? 9 : 17;
    int length = 0;
    for ( int digits = low; ; digits++ )
    {
        length = snprintf( text, sizeof( text ), "%.*g", digits, value );
        if ( length <= 0 || length >= (int) sizeof( text ) ) { return false; }
        if ( digits >= high ) { break; }
        // the round-trip check runs BEFORE the decimal point is normalised:
        // the token still carries whatever point snprintf just produced
        if ( single )
        {
            if ( (double) strtof( text, NULL ) == value ) { break; }
        }
        else
        {
            if ( strtod( text, NULL ) == value ) { break; }
        }
    }
    char point = TableJsonDecimalPoint();
    if ( point != '.' )
    {
        for ( int i = 0; i < length; i++ )
        {
            if ( text[i] == point ) { text[i] = '.'; }
        }
    }
    out.raw( text, length );
    return true;
}

inline bool TableJsonWriteValue( TableJsonOut & out, const void * base, const TableTypeInfo * info, int32_t depth );

// one scalar, at one storage address: a nested object, a union, a
// vocabulary, or a number
inline bool TableJsonWriteScalar( TableJsonOut & out, const void * storage, const TableFieldInfo * f, int32_t depth )
{
    if ( f->arms != NULL )
    {
        // a union is an object with ONE key, the arm's name; None is {}
        const TableUnionInfo * arms = f->arms();
        uint64_t tag = TableJsonGetRaw( (const uint8_t *) storage + arms->tag_offset, arms->tag_size );
        if ( tag == 0 )
        {
            out.raw( "{}", 2 );
            return true;
        }
        if ( (int64_t) tag > f->enum_max )
        {
            return false; // a tag no arm names, exactly as measure refuses it
        }
        const char * arm = f->enum_name( tag );
        // and refuse on the NAME, not merely on the bound: §16.2 says a value
        // no variant NAMES is refused, so the check is the name. Writing
        // whatever came back would emit "???", a spelling the reader counts
        // as unknown — a silent round-trip loss in place of a refusal.
        if ( !TableJsonNamed( arm ) ) { return false; }
        out.put( '{' );
        out.line( depth + 1 );
        TableJsonWriteString( out, arm, (int32_t) strlen( arm ) );
        out.raw( ": ", 2 );
        if ( !TableJsonWriteValue( out, (const uint8_t *) storage + arms->arms[tag].offset, arms->arms[tag].table, depth + 1 ) )
        {
            return false;
        }
        out.line( depth );
        out.put( '}' );
        return true;
    }
    if ( f->kind == 13 )
    {
        return TableJsonWriteValue( out, storage, f->table, depth );
    }
    if ( TableJsonIsEnum( f ) )
    {
        uint64_t value = TableJsonGetRaw( storage, f->elem_size );
        // a value no variant names has no text spelling, exactly as it has no
        // wire identity: the writer REFUSES rather than writing None over it,
        // the rule measure and save already apply (docs/SPEC-TABLES.md §5)
        if ( (int64_t) value > f->enum_max ) { return false; }
        if ( value != 0 && f->variant_id( value ) == 0 ) { return false; }
        const char * name = f->enum_name( value );
        if ( !TableJsonNamed( name ) ) { return false; }
        TableJsonWriteString( out, name, (int32_t) strlen( name ) );
        return true;
    }
    if ( TableJsonIsFlags( f ) )
    {
        uint64_t bits = TableJsonGetRaw( storage, f->elem_size );
        if ( bits == 0 )
        {
            out.raw( "[]", 2 );
            return true;
        }
        out.put( '[' );
        bool first = true;
        for ( int64_t bit = 0; bit < 64; bit++ )
        {
            if ( ( bits & ( uint64_t( 1 ) << bit ) ) == 0 ) { continue; }
            if ( bit > f->enum_max )
            {
                return false; // a bit no variant names has no text spelling
            }
            const char * name = f->enum_name( (uint64_t) bit );
            if ( !TableJsonNamed( name ) ) { return false; }
            if ( !first ) { out.put( ',' ); }
            first = false;
            out.line( depth + 1 );
            TableJsonWriteString( out, name, (int32_t) strlen( name ) );
        }
        out.line( depth );
        out.put( ']' );
        return true;
    }
    switch ( f->kind )
    {
        case 1:
            out.text( TableJsonGetRaw( storage, f->elem_size ) != 0 ? "true" : "false" );
            return true;
        case 10:
        {
            float v = 0.0f;
            memcpy( &v, storage, sizeof( v ) );
            return TableJsonWriteFloat( out, (double) v, true );
        }
        case 11:
        {
            double v = 0.0;
            memcpy( &v, storage, sizeof( v ) );
            return TableJsonWriteFloat( out, v, false );
        }
        case 2: case 3: case 4: case 5:
            TableJsonWriteSigned( out, TableJsonGetSigned( storage, f->elem_size ) );
            return true;
        default:
            TableJsonWriteUnsigned( out, TableJsonGetRaw( storage, f->elem_size ) );
            return true;
    }
}

inline bool TableJsonWriteField( TableJsonOut & out, const void * base, const TableFieldInfo * f, int32_t depth )
{
    const uint8_t * storage = (const uint8_t *) base + f->offset;
    if ( f->kind == 17 )
    {
        return TableJsonWritePointer( out, storage, f, depth );
    }
    if ( f->kind == 12 )
    {
        TableJsonWriteString( out, (const char *) storage, TableJsonCount( base, f ) );
        return true;
    }
    if ( TableJsonIsBytes( f ) )
    {
        TableJsonWriteBase64( out, storage, TableJsonCount( base, f ) );
        return true;
    }
    if ( TableJsonIsKeyed( f ) )
    {
        // one entry per SLOT, keyed by the variant that owns it, so inserting
        // a variant next season moves nothing in the text either. Slot i holds
        // the key i + 1: nothing is stored for None, so nothing is written for it.
        out.put( '{' );
        bool first = true;
        for ( int64_t slot = 0; slot < f->array_bound; slot++ )
        {
            if ( !TableJsonKeyedSlotValid( f, slot ) ) { continue; }
            if ( !first ) { out.put( ',' ); }
            first = false;
            out.line( depth + 1 );
            const char * key = f->key_name( TableJsonKeyedSlotKey( slot ) );
            TableJsonWriteString( out, key, (int32_t) strlen( key ) );
            out.raw( ": ", 2 );
            if ( !TableJsonWriteScalar( out, storage + slot * f->elem_size, f, depth + 1 ) )
            {
                return false;
            }
        }
        if ( first ) { out.raw( "}", 1 ); return true; }
        out.line( depth );
        out.put( '}' );
        return true;
    }
    if ( f->is_array )
    {
        int32_t count = TableJsonCount( base, f );
        if ( count == 0 )
        {
            out.raw( "[]", 2 );
            return true;
        }
        out.put( '[' );
        for ( int32_t i = 0; i < count; i++ )
        {
            if ( i > 0 ) { out.put( ',' ); }
            out.line( depth + 1 );
            if ( !TableJsonWriteScalar( out, storage + (int64_t) i * f->elem_size, f, depth + 1 ) )
            {
                return false;
            }
        }
        out.line( depth );
        out.put( ']' );
        return true;
    }
    return TableJsonWriteScalar( out, storage, f, depth );
}

// One instance's fields, in DECLARATION ORDER, defaults included — a text is
// for people and tools, and a text that elides is a text a reader has to know
// the schema to complete. ` + "`any`" + ` says whether the object is already open on
// entry — a shared node's ` + "`&node`" + ` opens it before the fields (§16.7) — and
// whether it is open on return.
inline bool TableJsonWriteFields( TableJsonOut & out, const void * base, const TableTypeInfo * info, int32_t depth, bool & any )
{
    for ( int32_t i = 0; i < info->num_fields; i++ )
    {
        const TableFieldInfo * f = &info->fields[i];
        if ( f->guard[0] != 0 && !TableJsonGuardHolds( base, info, f->guard ) ) { continue; }
        // an ABSENT optional writes no key: presence of the key IS the
        // presence (§16.2), so an absent field is an absent key and nothing
        // else would read back as absent
        if ( f->optional &&
             TableJsonGetRaw( (const uint8_t *) base + f->present_offset, 1 ) == 0 )
        {
            continue;
        }
        if ( !any ) { out.put( '{' ); }
        else { out.put( ',' ); }
        any = true;
        out.line( depth + 1 );
        TableJsonWriteString( out, f->json, (int32_t) strlen( f->json ) );
        out.raw( ": ", 2 );
        if ( !TableJsonWriteField( out, base, f, depth + 1 ) ) { return false; }
    }
    return true;
}

// One instance as one object. The writer carries the reader's depth cap
// (§16.2): a pointer chain nests as deep as it is long (§16.7), and a text the
// writer produced past the cap would be a text the reader refuses.
inline bool TableJsonWriteValue( TableJsonOut & out, const void * base, const TableTypeInfo * info, int32_t depth )
{
    if ( depth > kTableJsonMaxDepth ) { return false; }
    bool any = false;
    if ( !TableJsonWriteFields( out, base, info, depth, any ) ) { return false; }
    if ( !any )
    {
        out.raw( "{}", 2 );
        return true;
    }
    out.line( depth );
    out.put( '}' );
    return true;
}

// ---- reading ----

struct TableJsonIn
{
    const char * text;
    int64_t size;
    int64_t pos;
    TableReport * report;
    bool bad;     // the text is not JSON: the walk stops and keeps what it placed
    void * graph; // the pointered read's builder and label map (§16.7); NULL for a fixed table
};

inline void TableJsonSpace( TableJsonIn & in )
{
    while ( in.pos < in.size )
    {
        char c = in.text[in.pos];
        if ( c == ' ' || c == '\t' || c == '\n' || c == '\r' ) { in.pos++; continue; }
        // comments are not JSON, and a walk that guessed at one would be
        // reading a dialect nobody wrote down
        if ( c == '/' ) { in.bad = true; }
        return;
    }
}

inline char TableJsonPeek( TableJsonIn & in )
{
    TableJsonSpace( in );
    return in.pos < in.size ? in.text[in.pos] : 0;
}

// the shape of the value sitting at the cursor, without consuming it
inline char TableJsonValueShape( TableJsonIn & in )
{
    char c = TableJsonPeek( in );
    switch ( c )
    {
        case '{': return 'o';
        case '[': return 'a';
        case '"': return 's';
        case 't': case 'f': return 'b';
        case 'n': return 'z';
        case 0: return 0;
        default: return 'n';
    }
}

inline bool TableJsonLiteral( TableJsonIn & in, const char * word )
{
    int64_t length = (int64_t) strlen( word );
    if ( in.pos + length > in.size || memcmp( in.text + in.pos, word, (size_t) length ) != 0 )
    {
        in.bad = true;
        return false;
    }
    in.pos += length;
    return true;
}

// one \uXXXX escape body; -1 when the four hex digits are not there
inline int TableJsonHex4( TableJsonIn & in )
{
    if ( in.pos + 4 > in.size ) { return -1; }
    int value = 0;
    for ( int i = 0; i < 4; i++ )
    {
        char c = in.text[in.pos + i];
        int digit;
        if ( c >= '0' && c <= '9' ) { digit = c - '0'; }
        else if ( c >= 'a' && c <= 'f' ) { digit = c - 'a' + 10; }
        else if ( c >= 'A' && c <= 'F' ) { digit = c - 'A' + 10; }
        else { return -1; }
        value = ( value << 4 ) | digit;
    }
    in.pos += 4;
    return value;
}

inline int32_t TableJsonEncodeUtf8( uint32_t code, char * unit )
{
    if ( code < 0x80 ) { unit[0] = (char) code; return 1; }
    if ( code < 0x800 )
    {
        unit[0] = (char) ( 0xc0 | ( code >> 6 ) );
        unit[1] = (char) ( 0x80 | ( code & 0x3f ) );
        return 2;
    }
    if ( code < 0x10000 )
    {
        unit[0] = (char) ( 0xe0 | ( code >> 12 ) );
        unit[1] = (char) ( 0x80 | ( ( code >> 6 ) & 0x3f ) );
        unit[2] = (char) ( 0x80 | ( code & 0x3f ) );
        return 3;
    }
    unit[0] = (char) ( 0xf0 | ( code >> 18 ) );
    unit[1] = (char) ( 0x80 | ( ( code >> 12 ) & 0x3f ) );
    unit[2] = (char) ( 0x80 | ( ( code >> 6 ) & 0x3f ) );
    unit[3] = (char) ( 0x80 | ( code & 0x3f ) );
    return 4;
}

// Scan one JSON string into a caller buffer. Bytes are appended ONE CODE
// POINT AT A TIME — an escape's encoding, or a UTF-8 sequence read whole —
// so a string longer than the field is clamped AT A CODE POINT BOUNDARY and
// never cut through a multi-byte character. Clamping is counted, never
// fatal, exactly as it is on the wire (§4). A NULL destination scans past a
// string without keeping it.
inline bool TableJsonScanString( TableJsonIn & in, char * out, int32_t capacity, int32_t * length )
{
    if ( TableJsonPeek( in ) != '"' ) { in.bad = true; return false; }
    in.pos++;
    int32_t placed = 0;
    bool clamped = false;
    for ( ;; )
    {
        if ( in.pos >= in.size ) { in.bad = true; return false; }
        char c = in.text[in.pos];
        if ( c == '"' ) { in.pos++; break; }
        char unit[4];
        int32_t unit_length = 0;
        if ( c == '\\' )
        {
            in.pos++;
            if ( in.pos >= in.size ) { in.bad = true; return false; }
            char escape = in.text[in.pos++];
            switch ( escape )
            {
                case '"':  unit[0] = '"';  unit_length = 1; break;
                case '\\': unit[0] = '\\'; unit_length = 1; break;
                case '/':  unit[0] = '/';  unit_length = 1; break;
                case 'b':  unit[0] = '\b'; unit_length = 1; break;
                case 'f':  unit[0] = '\f'; unit_length = 1; break;
                case 'n':  unit[0] = '\n'; unit_length = 1; break;
                case 'r':  unit[0] = '\r'; unit_length = 1; break;
                case 't':  unit[0] = '\t'; unit_length = 1; break;
                case 'u':
                {
                    int high = TableJsonHex4( in );
                    if ( high < 0 ) { in.bad = true; return false; }
                    uint32_t code = (uint32_t) high;
                    if ( high >= 0xd800 && high <= 0xdbff && in.pos + 2 <= in.size &&
                         in.text[in.pos] == '\\' && in.text[in.pos + 1] == 'u' )
                    {
                        int64_t mark = in.pos;
                        in.pos += 2;
                        int low = TableJsonHex4( in );
                        if ( low >= 0xdc00 && low <= 0xdfff )
                        {
                            code = 0x10000 + ( ( (uint32_t) high - 0xd800 ) << 10 ) + ( (uint32_t) low - 0xdc00 );
                        }
                        else
                        {
                            in.pos = mark; // a lone lead surrogate rides as itself
                        }
                    }
                    // a surrogate half that never found its partner has no
                    // UTF-8 encoding: encoding it anyway would manufacture
                    // CESU-8 — invalid UTF-8 — out of input that was valid
                    // JSON, so it reads as the replacement character
                    if ( code >= 0xd800 && code <= 0xdfff ) { code = 0xfffd; }
                    unit_length = TableJsonEncodeUtf8( code, unit );
                    break;
                }
                default: in.bad = true; return false;
            }
        }
        else if ( (unsigned char) c < 0x20 )
        {
            in.bad = true; // a raw control character is not a JSON string body
            return false;
        }
        else
        {
            // a UTF-8 sequence read WHOLE, so the clamp below can only land
            // between code points. Only bytes that ACTUALLY look like
            // continuations are taken: the wire imposes no encoding (§3), so
            // a string may legitimately hold a stray lead byte, and one at
            // the end of a text must not swallow the closing quote.
            unsigned char lead = (unsigned char) c;
            int32_t want = 1;
            if ( ( lead & 0xe0 ) == 0xc0 ) { want = 2; }
            else if ( ( lead & 0xf0 ) == 0xe0 ) { want = 3; }
            else if ( ( lead & 0xf8 ) == 0xf0 ) { want = 4; }
            unit[0] = c;
            in.pos++;
            unit_length = 1;
            while ( unit_length < want && in.pos < in.size &&
                    ( (unsigned char) in.text[in.pos] & 0xc0 ) == 0x80 )
            {
                unit[unit_length++] = in.text[in.pos++];
            }
        }
        if ( out != NULL )
        {
            if ( placed + unit_length <= capacity )
            {
                memcpy( out + placed, unit, (size_t) unit_length );
                placed += unit_length;
            }
            else
            {
                clamped = true;
            }
        }
    }
    if ( clamped ) { in.report->clamped++; }
    if ( length != NULL ) { *length = placed; }
    return true;
}

// the numeric token at the cursor, copied out whole; false = not a number
// Scan one number, to JSON's OWN grammar (RFC 8259 §6) and not to a run of
// number-ish characters:
//
//     number = [ "-" ] int [ frac ] [ exp ]
//     int    = "0" / ( digit1-9 *digit )
//     frac   = "." 1*digit
//     exp    = ( "e" / "E" ) [ "-" / "+" ] 1*digit
//
// Scanning the production is what makes a typo in an authoring file a
// DIAGNOSTIC rather than a value: "1-2" scans as 1 and leaves "-2" where the
// object expects a comma, so the text is malformed — which is what §16.2
// already promises. A permissive scan would hand "1-2" to a digit loop and
// report a clamp, and a config pipeline would never hear about it. Leading
// "+", leading zeros, ".5" and "3." are not JSON either.
inline bool TableJsonWalkNumber( TableJsonIn & in, bool * integral )
{
    TableJsonSpace( in );
    bool whole = true;
    if ( in.pos < in.size && in.text[in.pos] == '-' ) { in.pos++; }
    // int: a lone zero, or a non-zero digit and any digits after it
    if ( in.pos >= in.size ) { return false; }
    if ( in.text[in.pos] == '0' )
    {
        in.pos++;
    }
    else if ( in.text[in.pos] >= '1' && in.text[in.pos] <= '9' )
    {
        while ( in.pos < in.size && in.text[in.pos] >= '0' && in.text[in.pos] <= '9' ) { in.pos++; }
    }
    else
    {
        return false;
    }
    // frac
    if ( in.pos < in.size && in.text[in.pos] == '.' )
    {
        in.pos++;
        if ( in.pos >= in.size || in.text[in.pos] < '0' || in.text[in.pos] > '9' ) { return false; }
        while ( in.pos < in.size && in.text[in.pos] >= '0' && in.text[in.pos] <= '9' ) { in.pos++; }
        whole = false;
    }
    // exp
    if ( in.pos < in.size && ( in.text[in.pos] == 'e' || in.text[in.pos] == 'E' ) )
    {
        in.pos++;
        if ( in.pos < in.size && ( in.text[in.pos] == '-' || in.text[in.pos] == '+' ) ) { in.pos++; }
        if ( in.pos >= in.size || in.text[in.pos] < '0' || in.text[in.pos] > '9' ) { return false; }
        while ( in.pos < in.size && in.text[in.pos] >= '0' && in.text[in.pos] <= '9' ) { in.pos++; }
        whole = false;
    }
    *integral = whole;
    return true;
}

// the same production, with the token kept for conversion
inline bool TableJsonScanNumber( TableJsonIn & in, char * token, int32_t capacity, int32_t * length, bool * integral )
{
    TableJsonSpace( in );
    int64_t start = in.pos;
    if ( !TableJsonWalkNumber( in, integral ) ) { return false; }
    int64_t count = in.pos - start;
    if ( count <= 0 || count >= capacity ) { return false; }
    memcpy( token, in.text + start, (size_t) count );
    token[count] = 0;
    *length = (int32_t) count;
    return true;
}

// the token's exact double, through the runtime's own converter — which
// speaks the LOCALE's decimal point, so the token crosses back over that
// character on its way in
inline double TableJsonTokenDouble( const char * token, int32_t length, bool single )
{
    char work[kTableJsonMaxNumber];
    memcpy( work, token, (size_t) length );
    work[length] = 0;
    char point = TableJsonDecimalPoint();
    if ( point != '.' )
    {
        for ( int32_t i = 0; i < length; i++ )
        {
            if ( work[i] == '.' ) { work[i] = point; }
        }
    }
    if ( single ) { return (double) strtof( work, NULL ); }
    return strtod( work, NULL );
}

// the token's exact integer, parsed digit by digit so no width and no
// locale can move it. Saturation is reported as a clamp, the wire's rule for
// a value outside what the reader can hold (§4).
inline int64_t TableJsonTokenInteger( const char * token, int32_t length, bool is_signed, bool * saturated )
{
    int32_t i = 0;
    bool negative = false;
    if ( i < length && ( token[i] == '-' || token[i] == '+' ) )
    {
        negative = token[i] == '-';
        i++;
    }
    uint64_t magnitude = 0;
    bool over = false;
    for ( ; i < length; i++ )
    {
        uint64_t digit = (uint64_t) ( token[i] - '0' );
        if ( magnitude > ( UINT64_MAX - digit ) / 10 ) { over = true; break; }
        magnitude = magnitude * 10 + digit;
    }
    if ( !is_signed )
    {
        // -0 IS zero, and clamping it would report an event that did not
        // happen; only a real negative magnitude is out of range here
        if ( negative ) { *saturated = magnitude != 0; return 0; }
        if ( over ) { *saturated = true; return (int64_t) UINT64_MAX; }
        *saturated = false;
        return (int64_t) magnitude;
    }
    if ( negative )
    {
        if ( over || magnitude > ( uint64_t( 1 ) << 63 ) ) { *saturated = true; return INT64_MIN; }
        *saturated = false;
        if ( magnitude == ( uint64_t( 1 ) << 63 ) ) { return INT64_MIN; }
        return -(int64_t) magnitude;
    }
    if ( over || magnitude > (uint64_t) INT64_MAX ) { *saturated = true; return INT64_MAX; }
    *saturated = false;
    return (int64_t) magnitude;
}

inline bool TableJsonSkipValue( TableJsonIn & in, int32_t depth );

inline bool TableJsonSkipContainer( TableJsonIn & in, char close, int32_t depth )
{
    if ( depth > kTableJsonMaxDepth ) { in.bad = true; return false; }
    in.pos++; // the opening bracket
    bool first = true;
    for ( ;; )
    {
        char c = TableJsonPeek( in );
        if ( c == close ) { in.pos++; return true; }
        if ( c == 0 ) { in.bad = true; return false; }
        if ( close == '}' )
        {
            // the key is kept, because a skipped OBJECT may still be a
            // pointer's: an ` + "`&node`" + ` opening it names a node the storage could
            // not hold, and the numbering has to survive the drop (§16.7).
            // Anywhere but first, the prefix is the reserved key out of place
            // — in a pointered unit; a fixed unit skips the value whole.
            char key[kTableJsonMaxKey];
            int32_t key_length = 0;
            if ( !TableJsonScanString( in, key, kTableJsonMaxKey - 1, &key_length ) ) { return false; }
            key[key_length] = 0;
            if ( TableJsonPeek( in ) != ':' ) { in.bad = true; return false; }
            in.pos++;
            if ( key[0] == '&' && in.graph != NULL )
            {
                if ( !first ) { in.report->malformed = true; in.bad = true; return false; }
                if ( !TableJsonSkippedAmpersand( in, key, depth ) ) { return false; }
                first = false;
                c = TableJsonPeek( in );
                if ( c == ',' ) { in.pos++; continue; }
                if ( c == close ) { in.pos++; return true; }
                in.bad = true;
                return false;
            }
        }
        first = false;
        if ( !TableJsonSkipValue( in, depth + 1 ) ) { return false; }
        c = TableJsonPeek( in );
        if ( c == ',' ) { in.pos++; continue; }   // a trailing comma is accepted
        if ( c == close ) { in.pos++; return true; }
        in.bad = true;
        return false;
    }
}

inline bool TableJsonSkipValue( TableJsonIn & in, int32_t depth )
{
    char c = TableJsonPeek( in );
    switch ( c )
    {
        case '{': return TableJsonSkipContainer( in, '}', depth );
        case '[': return TableJsonSkipContainer( in, ']', depth );
        case '"': return TableJsonScanString( in, NULL, 0, NULL );
        case 't': return TableJsonLiteral( in, "true" );
        case 'f': return TableJsonLiteral( in, "false" );
        case 'n': return TableJsonLiteral( in, "null" );
        case 0:   in.bad = true; return false;
        default:
        {
            // consumed, never converted: skipping needs no buffer, and this
            // is the one walk a hostile text drives to the depth cap. It is
            // the SAME production the value path scans, so an unknown key
            // cannot smuggle past a number a named key would refuse.
            bool integral = false;
            if ( !TableJsonWalkNumber( in, &integral ) ) { in.bad = true; return false; }
            return true;
        }
    }
}

inline bool TableJsonReadTable( TableJsonIn & in, void * base, const TableTypeInfo * info, int32_t depth );

// place one scalar at one storage address
inline bool TableJsonReadScalar( TableJsonIn & in, void * storage, const TableFieldInfo * f, int32_t depth )
{
    if ( f->arms != NULL )
    {
        // a union is an object with ONE key, the arm's name; {} is None, and
        // two keys is a text this walk will not guess at
        const TableUnionInfo * arms = f->arms();
        if ( TableJsonPeek( in ) != '{' ) { in.bad = true; return false; }
        in.pos++;
        TableJsonSetRaw( (uint8_t *) storage + arms->tag_offset, arms->tag_size, 0 );
        if ( TableJsonPeek( in ) == '}' ) { in.pos++; return true; }
        char key[kTableJsonMaxKey];
        int32_t key_length = 0;
        if ( !TableJsonScanString( in, key, kTableJsonMaxKey - 1, &key_length ) ) { return false; }
        key[key_length] = 0;
        if ( TableJsonPeek( in ) != ':' ) { in.bad = true; return false; }
        in.pos++;
        int64_t tag = 0;
        for ( int64_t t = 1; t <= f->enum_max; t++ )
        {
            if ( strcmp( f->enum_name( (uint64_t) t ), key ) == 0 ) { tag = t; break; }
        }
        if ( tag == 0 )
        {
            in.report->unknown++;
            if ( !TableJsonSkipValue( in, depth + 1 ) ) { return false; }
        }
        else
        {
            void * payload = (uint8_t *) storage + arms->arms[tag].offset;
            arms->arms[tag].table->reset( payload );
            if ( !TableJsonReadTable( in, payload, arms->arms[tag].table, depth + 1 ) ) { return false; }
            TableJsonSetRaw( (uint8_t *) storage + arms->tag_offset, arms->tag_size, (uint64_t) tag );
        }
        char c = TableJsonPeek( in );
        if ( c == ',' ) { in.pos++; c = TableJsonPeek( in ); }
        if ( c == '}' ) { in.pos++; return true; }
        in.bad = true; // a second key: a one-of with two arms is not a value
        return false;
    }
    if ( f->kind == 13 )
    {
        f->table->reset( storage );
        return TableJsonReadTable( in, storage, f->table, depth + 1 );
    }
    if ( TableJsonIsEnum( f ) )
    {
        char name[kTableJsonMaxKey];
        int32_t name_length = 0;
        if ( !TableJsonScanString( in, name, kTableJsonMaxKey - 1, &name_length ) ) { return false; }
        name[name_length] = 0;
        for ( int64_t v = 0; v <= f->enum_max; v++ )
        {
            if ( strcmp( f->enum_name( (uint64_t) v ), name ) == 0 )
            {
                TableJsonSetRaw( storage, f->elem_size, (uint64_t) v );
                return true;
            }
        }
        // a name this build cannot name reads as None and counts as unknown,
        // exactly as an unknown variant id does on the wire (§4)
        TableJsonSetRaw( storage, f->elem_size, 0 );
        in.report->unknown++;
        return true;
    }
    if ( TableJsonIsFlags( f ) )
    {
        if ( TableJsonPeek( in ) != '[' ) { in.bad = true; return false; }
        in.pos++;
        uint64_t bits = 0;
        for ( ;; )
        {
            char c = TableJsonPeek( in );
            if ( c == ']' ) { in.pos++; break; }
            if ( c == 0 ) { in.bad = true; return false; }
            if ( c != '"' )
            {
                in.report->kind_mismatch++;
                if ( !TableJsonSkipValue( in, depth + 1 ) ) { return false; }
            }
            else
            {
                char name[kTableJsonMaxKey];
                int32_t name_length = 0;
                if ( !TableJsonScanString( in, name, kTableJsonMaxKey - 1, &name_length ) ) { return false; }
                name[name_length] = 0;
                bool found = false;
                for ( int64_t bit = 0; bit <= f->enum_max; bit++ )
                {
                    if ( strcmp( f->enum_name( (uint64_t) bit ), name ) == 0 )
                    {
                        bits |= uint64_t( 1 ) << bit;
                        found = true;
                        break;
                    }
                }
                if ( !found ) { in.report->unknown++; }
            }
            c = TableJsonPeek( in );
            if ( c == ',' ) { in.pos++; continue; }
            if ( c == ']' ) { in.pos++; break; }
            in.bad = true;
            return false;
        }
        TableJsonSetRaw( storage, f->elem_size, bits );
        return true;
    }
    if ( f->kind == 1 )
    {
        char c = TableJsonPeek( in );
        if ( c == 't' ) { if ( !TableJsonLiteral( in, "true" ) ) { return false; } TableJsonSetRaw( storage, f->elem_size, 1 ); return true; }
        if ( !TableJsonLiteral( in, "false" ) ) { return false; }
        TableJsonSetRaw( storage, f->elem_size, 0 );
        return true;
    }
    char token[kTableJsonMaxNumber];
    int32_t length = 0;
    bool integral = false;
    if ( !TableJsonScanNumber( in, token, kTableJsonMaxNumber, &length, &integral ) )
    {
        in.bad = true;
        return false;
    }
    if ( f->kind == 10 || f->kind == 11 )
    {
        bool single = f->kind == 10;
        double value = TableJsonTokenDouble( token, length, single );
        // A magnitude the field's format cannot hold is the WRONG SHAPE for
        // the kind, and it never reaches storage: 1e400 is not a float64 and
        // 1e300 is not a float32. Storing the infinity the conversion
        // produced would leave an instance this walk called CLEAN that
        // ToJsonMeasure then refuses forever (a non-finite float has no JSON
        // spelling), and §16.1's one invariant is that a text which reads
        // clean writes back.
        if ( !TableJsonFinite( value ) )
        {
            in.report->kind_mismatch++;
            return true;
        }
        if ( f->has_range )
        {
            if ( value < f->range_min ) { value = f->range_min; in.report->clamped++; }
            else if ( value > f->range_max ) { value = f->range_max; in.report->clamped++; }
        }
        if ( single )
        {
            float narrow = (float) value;
            if ( !TableJsonFinite( (double) narrow ) )
            {
                in.report->kind_mismatch++;
                return true;
            }
            memcpy( storage, &narrow, sizeof( narrow ) );
        }
        else
        {
            memcpy( storage, &value, sizeof( value ) );
        }
        return true;
    }
    // JSON HAS ONE NUMBER TYPE. 2.0 IS the integer 2 and 1e3 IS 1000, and a
    // library that round-trips numbers through a double emits them that way —
    // this walker's own float writer emits 1e+21. So an integer field takes
    // any number whose VALUE is integral, however it was spelled; only a
    // genuinely fractional value is the wrong shape for it.
    bool is_signed = f->kind >= 2 && f->kind <= 5;
    bool saturated = false;
    int64_t value = 0;
    if ( integral )
    {
        value = TableJsonTokenInteger( token, length, is_signed, &saturated );
    }
    else
    {
        double d = TableJsonTokenDouble( token, length, false );
        if ( !TableJsonFinite( d ) )
        {
            in.report->kind_mismatch++;
            return true;
        }
        if ( is_signed )
        {
            if ( d >= 9223372036854775808.0 ) { value = INT64_MAX; saturated = true; }
            else if ( d < -9223372036854775808.0 ) { value = INT64_MIN; saturated = true; }
            else if ( d != (double) (int64_t) d ) { in.report->kind_mismatch++; return true; }
            else { value = (int64_t) d; }
        }
        else
        {
            if ( d < 0.0 )
            {
                // a negative for an unsigned field clamps to zero, as the
                // exact digit path already does
                if ( d != (double) (int64_t) d ) { in.report->kind_mismatch++; return true; }
                value = 0;
                saturated = true;
            }
            else if ( d >= 18446744073709551616.0 ) { value = (int64_t) UINT64_MAX; saturated = true; }
            else if ( d != (double) (uint64_t) d ) { in.report->kind_mismatch++; return true; }
            else { value = (int64_t) (uint64_t) d; }
        }
    }
    if ( saturated ) { in.report->clamped++; }
    if ( f->has_range )
    {
        if ( (double) value < f->range_min ) { value = (int64_t) f->range_min; in.report->clamped++; }
        else if ( (double) value > f->range_max ) { value = (int64_t) f->range_max; in.report->clamped++; }
    }
    // the field's own storage width is the last bound: a value past it
    // clamps rather than wrapping, which is what the wire does too
    if ( f->elem_size < 8 )
    {
        if ( is_signed )
        {
            int64_t high = ( int64_t( 1 ) << ( f->elem_size * 8 - 1 ) ) - 1;
            int64_t low = -high - 1;
            if ( value > high ) { value = high; in.report->clamped++; }
            else if ( value < low ) { value = low; in.report->clamped++; }
        }
        else
        {
            uint64_t high = ( uint64_t( 1 ) << ( f->elem_size * 8 ) ) - 1;
            if ( value < 0 ) { value = 0; in.report->clamped++; }
            else if ( (uint64_t) value > high ) { value = (int64_t) high; in.report->clamped++; }
        }
    }
    // at eight bytes the storage IS the parser's width, and an unsigned value
    // past INT64_MAX rides here as a negative int64 by design — the token
    // parser already turned a NEGATIVE token for an unsigned field into a
    // clamped zero, so there is nothing left to bound.
    TableJsonSetRaw( storage, f->elem_size, (uint64_t) value );
    return true;
}

inline bool TableJsonReadField( TableJsonIn & in, void * base, const TableFieldInfo * f, int32_t depth )
{
    uint8_t * storage = (uint8_t *) base + f->offset;
    if ( f->kind == 12 )
    {
        int32_t length = 0;
        if ( !TableJsonScanString( in, (char *) storage, f->array_bound, &length ) ) { return false; }
        storage[length] = 0;
        TableJsonSetCount( base, f, length );
        return true;
    }
    if ( TableJsonIsBytes( f ) )
    {
        // base64 decodes STRAIGHT INTO the field's storage, six bits at a
        // time — no window, no temporary, so a bytes(N) of any declared
        // extent reads the same way. A base64 body carries no escapes, so a
        // backslash in one is simply not an alphabet character.
        if ( TableJsonPeek( in ) != '"' ) { in.bad = true; return false; }
        in.pos++;
        memset( storage, 0, (size_t) f->array_bound );
        TableJsonSetCount( base, f, 0 );
        const char * alphabet = TableJsonBase64Alphabet();
        int32_t placed = 0;
        uint32_t accumulator = 0;
        int32_t held = 0;
        bool clamped = false;
        bool malformed = false;
        for ( ;; )
        {
            if ( in.pos >= in.size ) { in.bad = true; return false; }
            char c = in.text[in.pos++];
            if ( c == '"' ) { break; }
            if ( c == '=' || malformed ) { continue; }
            const char * at = c != 0 ? strchr( alphabet, c ) : NULL;
            if ( at == NULL ) { malformed = true; continue; }
            accumulator = ( accumulator << 6 ) | (uint32_t) ( at - alphabet );
            held += 6;
            if ( held >= 8 )
            {
                held -= 8;
                if ( placed < f->array_bound )
                {
                    storage[placed++] = (uint8_t) ( ( accumulator >> held ) & 0xff );
                }
                else
                {
                    clamped = true;
                }
            }
        }
        if ( malformed )
        {
            // a body that is not base64 is the wrong shape for the kind: the
            // field keeps its default and the event is counted
            in.report->kind_mismatch++;
            return true;
        }
        if ( clamped ) { in.report->clamped++; }
        TableJsonSetCount( base, f, placed );
        return true;
    }
    if ( TableJsonIsKeyed( f ) )
    {
        if ( TableJsonPeek( in ) != '{' ) { in.bad = true; return false; }
        in.pos++;
        // every slot back to its declared defaults first, so a key the text
        // omits keeps them and a repeated field key cannot leave an earlier
        // occurrence's slots standing
        for ( int32_t i = 0; i < f->array_bound; i++ )
        {
            void * slot = storage + (int64_t) i * f->elem_size;
            if ( f->kind == 13 ) { f->table->reset( slot ); }
            else { memset( slot, 0, (size_t) f->elem_size ); }
        }
        char shape = TableJsonElementShape( f );
        // A KEYED OBJECT'S KEYS ARE KEYS: a variant named twice is a duplicate
        // key like any other, last-wins and counted (§16.2). Tracked the way
        // a table's own field keys are — a bounded, allocation-free bitmask;
        // a vocabulary wider than this still reads, its repeats simply stop
        // being counted.
        uint64_t seen[8] = {};
        for ( ;; )
        {
            char c = TableJsonPeek( in );
            if ( c == '}' ) { in.pos++; break; }
            if ( c == 0 ) { in.bad = true; return false; }
            char key[kTableJsonMaxKey];
            int32_t key_length = 0;
            if ( !TableJsonScanString( in, key, kTableJsonMaxKey - 1, &key_length ) ) { return false; }
            key[key_length] = 0;
            if ( TableJsonPeek( in ) != ':' ) { in.bad = true; return false; }
            in.pos++;
            int64_t slot = -1;
            for ( int64_t v = 0; v < f->array_bound; v++ )
            {
                // nothing is stored for None, so "None" finds no slot and is
                // an unknown key like any other name this reader cannot place
                if ( !TableJsonKeyedSlotValid( f, v ) ) { continue; }
                if ( strcmp( f->key_name( TableJsonKeyedSlotKey( v ) ), key ) == 0 ) { slot = v; break; }
            }
            if ( slot >= 0 && slot < 512 )
            {
                uint64_t bit = uint64_t( 1 ) << ( slot & 63 );
                if ( ( seen[slot >> 6] & bit ) != 0 ) { in.report->duplicate++; }
                seen[slot >> 6] |= bit;
            }
            if ( slot < 0 )
            {
                in.report->unknown++;
                if ( !TableJsonSkipValue( in, depth + 1 ) ) { return false; }
            }
            else if ( TableJsonValueShape( in ) != shape )
            {
                in.report->kind_mismatch++;
                if ( !TableJsonSkipValue( in, depth + 1 ) ) { return false; }
            }
            else if ( !TableJsonReadScalar( in, storage + slot * f->elem_size, f, depth + 1 ) )
            {
                return false;
            }
            c = TableJsonPeek( in );
            if ( c == ',' ) { in.pos++; continue; } // a trailing comma is accepted
            if ( c == '}' ) { in.pos++; break; }
            in.bad = true;
            return false;
        }
        return true;
    }
    if ( f->is_array )
    {
        if ( TableJsonPeek( in ) != '[' ) { in.bad = true; return false; }
        in.pos++;
        // LAST WINS has to be true of a repeated ARRAY key too, and it is
        // wire-visible: a fixed array writes every slot, so a second, shorter
        // occurrence overlaying a prefix would leave the first occurrence's
        // tail standing. The field goes back to its declared defaults before
        // this occurrence's elements are placed — the re-establishment a nested
        // table and a union arm already get. A table element's defaults are
        // its own (the reset hook); every other element kind's storage
        // default is zero, which is what the generated array declares.
        if ( f->kind == 13 )
        {
            for ( int32_t i = 0; i < f->array_bound; i++ )
            {
                f->table->reset( storage + (int64_t) i * f->elem_size );
            }
        }
        else
        {
            memset( storage, 0, (size_t) f->array_bound * (size_t) f->elem_size );
        }
        TableJsonSetCount( base, f, 0 );
        int32_t placed = 0;
        char shape = TableJsonElementShape( f );
        for ( ;; )
        {
            char c = TableJsonPeek( in );
            if ( c == ']' ) { in.pos++; break; }
            if ( c == 0 ) { in.bad = true; return false; }
            if ( placed >= f->array_bound )
            {
                // more elements than the reader's bound: the bounded prefix
                // is kept and the excess counts, the wire's rule (§4)
                in.report->clamped++;
                if ( !TableJsonSkipValue( in, depth + 1 ) ) { return false; }
            }
            else if ( TableJsonValueShape( in ) != shape )
            {
                in.report->kind_mismatch++;
                if ( !TableJsonSkipValue( in, depth + 1 ) ) { return false; }
                placed++;
            }
            else
            {
                if ( !TableJsonReadScalar( in, storage + (int64_t) placed * f->elem_size, f, depth + 1 ) ) { return false; }
                placed++;
            }
            c = TableJsonPeek( in );
            if ( c == ',' ) { in.pos++; continue; }
            if ( c == ']' ) { in.pos++; break; }
            in.bad = true;
            return false;
        }
        // a fixed array's tail keeps the defaults the prefill left there,
        // exactly as a short wire count does
        TableJsonSetCount( base, f, placed );
        return true;
    }
    return TableJsonReadScalar( in, storage, f, depth );
}

inline bool TableJsonReadTableKeys( TableJsonIn & in, void * base, const TableTypeInfo * info, int32_t depth, const char * first_key );

// ONE table object: keys are field keys, unknown ones are skipped and
// counted, a repeated key is last-wins and counted. The instance is already
// at its declared defaults when this is entered, so a key the text never
// mentions keeps the default an absent field takes on the wire (§4).
inline bool TableJsonReadTable( TableJsonIn & in, void * base, const TableTypeInfo * info, int32_t depth )
{
    if ( depth > kTableJsonMaxDepth ) { in.bad = true; return false; }
    if ( TableJsonPeek( in ) != '{' ) { in.bad = true; return false; }
    in.pos++;
    return TableJsonReadTableKeys( in, base, info, depth, NULL );
}

// The keys of an object whose brace is already consumed. A pointer's object
// opens the same way a table's does, but its FIRST key may be ` + "`&node`" + ` (§16.7)
// and the adapter that reads it has to scan the key to know — so it hands the
// key it scanned in as ` + "`first_key`" + `, with the colon consumed, and this places
// it before scanning the rest.
inline bool TableJsonReadTableKeys( TableJsonIn & in, void * base, const TableTypeInfo * info, int32_t depth, const char * first_key )
{
    // duplicate tracking, bounded and allocation-free: a table with more
    // fields than this still reads, its repeats simply stop being counted
    uint64_t seen[8] = {};
    for ( ;; )
    {
        char key[kTableJsonMaxKey];
        char c = 0;
        if ( first_key != NULL )
        {
            memcpy( key, first_key, strlen( first_key ) + 1 ); // scanned into a buffer this size by the caller
            first_key = NULL;
        }
        else
        {
            c = TableJsonPeek( in );
            if ( c == '}' ) { in.pos++; return true; }
            if ( c == 0 ) { in.bad = true; return false; }
            int32_t key_length = 0;
            if ( !TableJsonScanString( in, key, kTableJsonMaxKey - 1, &key_length ) ) { return false; }
            key[key_length] = 0;
            if ( TableJsonPeek( in ) != ':' ) { in.bad = true; return false; }
            in.pos++;
        }
        int32_t index = -1;
        for ( int32_t i = 0; i < info->num_fields; i++ )
        {
            if ( strcmp( info->fields[i].json, key ) == 0 ) { index = i; break; }
        }
        if ( key[0] == '&' )
        {
            // THE AMPERSAND PREFIX IS RESERVED TO THE FORM (docs/SPEC-TABLES.md
            // §16.7). No declaration may take a key beginning with it, so this
            // is never a field this build lacks — it is the sharing construct
            // somewhere it cannot stand: ` + "`&node`" + ` is the FIRST key of a pointer's
            // object and nothing else, and the adapter that reads a pointer
            // has consumed it before these keys are read. MALFORMED, refused
            // and counted; never counted as unknown, never skipped.
            in.report->malformed = true;
            in.bad = true;
            return false;
        }
        if ( index < 0 )
        {
            in.report->unknown++;
            if ( !TableJsonSkipValue( in, depth + 1 ) ) { return false; }
        }
        else
        {
            const TableFieldInfo * f = &info->fields[index];
            if ( index < 512 )
            {
                uint64_t bit = uint64_t( 1 ) << ( index & 63 );
                if ( ( seen[index >> 6] & bit ) != 0 ) { in.report->duplicate++; }
                seen[index >> 6] |= bit;
            }
            // PRESENCE OF THE KEY IS THE PRESENCE (§16.2): reaching this line
            // is the key being present, so an optional is set present
            // whatever its value — with one exception the page names: a JSON
            // null, which reads as ABSENT rather than as a value.
            char got = TableJsonValueShape( in );
            if ( f->kind == 17 )
            {
                // a pointer: null is a null pointer, an object is the pointee
                // in place or an ` + "`&node`" + ` reference to one (§16.7), and anything
                // else is the wrong shape for the kind
                if ( got == 'z' )
                {
                    if ( !TableJsonLiteral( in, "null" ) ) { return false; }
                    TableJsonSetRaw( (uint8_t *) base + f->offset, f->elem_size, 0 );
                }
                else if ( got != 'o' )
                {
                    in.report->kind_mismatch++;
                    if ( !TableJsonSkipValue( in, depth + 1 ) ) { return false; }
                }
                else if ( !TableJsonReadPointer( in, (uint8_t *) base + f->offset, f, depth ) )
                {
                    return false;
                }
            }
            else if ( f->optional && got == 'z' )
            {
                if ( !TableJsonLiteral( in, "null" ) ) { return false; }
                // absent, and back at its defaults: a repeated key whose last
                // occurrence is null must not leave an earlier value standing
                if ( f->table != NULL ) { f->table->reset( (uint8_t *) base + f->offset ); }
                else { memset( (uint8_t *) base + f->offset, 0, (size_t) f->elem_size ); }
                TableJsonSetRaw( (uint8_t *) base + f->present_offset, 1, 0 );
            }
            else
            {
                if ( got != TableJsonShape( f ) )
                {
                    // the wrong JSON type for the kind: skipped, never coerced
                    in.report->kind_mismatch++;
                    if ( !TableJsonSkipValue( in, depth + 1 ) ) { return false; }
                }
                else if ( !TableJsonReadField( in, base, f, depth ) )
                {
                    return false;
                }
                if ( f->optional )
                {
                    TableJsonSetRaw( (uint8_t *) base + f->present_offset, 1, 1 );
                }
            }
        }
        c = TableJsonPeek( in );
        if ( c == ',' ) { in.pos++; continue; } // a trailing comma is accepted
        if ( c == '}' ) { in.pos++; return true; }
        in.bad = true;
        return false;
    }
}

// ---- the two entry points the per-table wrappers name ----

inline bool TableJsonRead( void * value, const TableTypeInfo * info, const char * text, int64_t bytes, TableReport * report )
{
    TableReport ignored;
    TableJsonIn in;
    in.text = text;
    in.size = bytes;
    in.pos = 0;
    in.report = report != NULL ? report : &ignored;
    in.bad = false;
    in.graph = NULL;
    info->reset( value );
    if ( text == NULL || bytes < 0 )
    {
        in.report->malformed = true;
        return false;
    }
    bool ok = TableJsonReadTable( in, value, info, 0 );
    if ( ok )
    {
        TableJsonSpace( in );
        if ( in.pos != in.size ) { in.bad = true; } // trailing rubbish is not one text
    }
    if ( in.bad || !ok )
    {
        in.report->malformed = true;
        return false;
    }
    return true;
}

inline int64_t TableJsonWrite( const void * value, const TableTypeInfo * info, char * buffer, int64_t capacity )
{
    TableJsonOut out;
    out.buffer = buffer;
    out.capacity = capacity;
    out.offset = 0;
    out.overflow = false;
    out.graph = NULL;
    if ( !TableJsonWriteValue( out, value, info, 0 ) ) { return -1; }
    // THE CANONICAL TEXT ENDS WITH EXACTLY ONE NEWLINE (docs/SPEC-TABLES.md
    // §16.1). Every writer emits it — this walk, the C# walk and
    // "schema unpack" — and every reader accepts a text with or without one,
    // because the trailing whitespace a read already skips is what makes the
    // two the same text. It is a byte of the FORM rather than a file
    // convention: a text that is written to a file, pasted into a diff and
    // handed back through a pipe has to be one text in all three places, and a
    // buffer whose last byte is a closing brace is the one shape that is not.
    out.put( '\n' );
    if ( out.overflow ) { return -1; }
    return out.offset;
}

// ---- json walk: end ----
`

// tableJsonGraphSource is the VARIABLE class's half of the form, emitted only
// in a unit that declares a pointer. It defines the three adapters the walk
// calls and the two entry points a pointered table's wrappers name, and it is
// gated the way the walk is: the same bytes in every pointered unit of the
// corpus (`make tables-json-graph-walk`).
const tableJsonGraphSource = `// ---- json graph walk: begin ----
//
// THE VARIABLE CLASS's half of the text form (docs/SPEC-TABLES.md §16.7). The
// walk above places every kind but one; this defines the three adapters it
// calls for that one, and the two entry points a pointered table's wrappers
// name. The text is the fixed class's — a pointee is an object in place — and a
// node named more than once carries ` + "`&node`" + `: defined once, with its fields,
// and referenced after by ` + "`{ \"&node\": N }`" + ` alone.

// ---- the identity map ----
//
// ONE map shape serves both directions. Writing keys it by a node's ADDRESS and
// counts the slots that name the node, so the second pass knows at a node's
// first occurrence whether it will be named again; reading keys it by the
// text's own label and answers the node it defined. Open addressing, a
// multiply-shift hash and quadrupling growth — TablePackMap's shape (§6.2), on
// the same terms: proportional to nodes, never to bytes, on the authoring
// side, and released before the call returns.

struct TableJsonGraphEntry
{
    uint64_t key;               // a node's address (write) or a label (read); 0 is an empty slot
    int64_t count;              // write: how many slots name this node
    int64_t label;              // write: the ` + "`&node`" + ` label assigned at its first write, 0 until then
    uint8_t open;               // the descent is still open: a reference here is a cycle (write), a self-reference (read)
    uint32_t node;              // read: the node's arena offset; 0 for a definition the reader dropped
    const TableTypeInfo * type; // read: the node's table; NULL for a dropped one
};

struct TableJsonGraphMap
{
    TableJsonGraphEntry * entries;
    int64_t capacity;         // a power of two, or zero while empty
    int64_t count;
    TableAllocator allocator; // the caller's pair (§6.5): the builder's on read, the one handed to ToJson on write
};

inline void TableJsonGraphMapInit( TableJsonGraphMap & map, TableAllocator allocator )
{
    map.entries = NULL;
    map.capacity = 0;
    map.count = 0;
    map.allocator = allocator;
}

inline void TableJsonGraphMapShutdown( TableJsonGraphMap & map )
{
    map.allocator.free( map.allocator.context, map.entries );
    TableJsonGraphMapInit( map, map.allocator );
}

inline int64_t TableJsonGraphMapSlot( const TableJsonGraphMap & map, uint64_t key )
{
    uint64_t hash = key * 0x9E3779B97F4A7C15ull;
    hash ^= hash >> 29;
    int64_t mask = map.capacity - 1;
    int64_t at = (int64_t) ( hash & (uint64_t) mask );
    while ( map.entries[at].key != 0 && map.entries[at].key != key )
    {
        at = ( at + 1 ) & mask;
    }
    return at;
}

inline TableJsonGraphEntry * TableJsonGraphMapFind( TableJsonGraphMap & map, uint64_t key )
{
    if ( map.capacity == 0 ) { return NULL; }
    TableJsonGraphEntry * entry = &map.entries[ TableJsonGraphMapSlot( map, key ) ];
    return entry->key == key ? entry : NULL;
}

inline bool TableJsonGraphMapGrow( TableJsonGraphMap & map )
{
    TableJsonGraphMap grown;
    grown.allocator = map.allocator;
    grown.capacity = map.capacity != 0 ? map.capacity * 4 : 64;
    grown.count = 0;
    grown.entries = (TableJsonGraphEntry *) map.allocator.alloc( map.allocator.context, grown.capacity * (int64_t) sizeof( TableJsonGraphEntry ) ); // zeroed, by the pair's contract
    if ( grown.entries == NULL ) { return false; }
    for ( int64_t i = 0; i < map.capacity; i++ )
    {
        if ( map.entries[i].key == 0 ) { continue; }
        grown.entries[ TableJsonGraphMapSlot( grown, map.entries[i].key ) ] = map.entries[i];
        grown.count++;
    }
    map.allocator.free( map.allocator.context, map.entries );
    map = grown;
    return true;
}

// the entry for a key, made if it was not there; ` + "`taken`" + ` says which. NULL is the
// allocator refusing, and the walk refuses with it.
inline TableJsonGraphEntry * TableJsonGraphMapReach( TableJsonGraphMap & map, uint64_t key, bool & taken )
{
    if ( ( map.count + 1 ) * 4 >= map.capacity * 3 ) // keep the load factor under three quarters
    {
        if ( !TableJsonGraphMapGrow( map ) ) { return NULL; }
    }
    TableJsonGraphEntry * entry = &map.entries[ TableJsonGraphMapSlot( map, key ) ];
    taken = entry->key != key;
    if ( taken )
    {
        entry->key = key;
        map.count++;
    }
    return entry;
}

// ---- reading: into a builder ----

struct TableJsonGraphIn
{
    TableWorker * worker;  // where every node comes from
    TableJsonGraphMap labels; // a label -> the node it defined
};

// ` + "`&node`" + `'s value, the LABEL: a positive integer spelled as one — digits, no sign, no
// fraction, no exponent, no leading zero (§16.7). Anything else is malformed.
inline bool TableJsonScanLabel( TableJsonIn & in, uint64_t & label )
{
    TableJsonSpace( in );
    if ( in.pos >= in.size || in.text[in.pos] < '1' || in.text[in.pos] > '9' )
    {
        in.report->malformed = true;
        in.bad = true;
        return false;
    }
    uint64_t value = 0;
    while ( in.pos < in.size && in.text[in.pos] >= '0' && in.text[in.pos] <= '9' )
    {
        uint64_t digit = (uint64_t) ( in.text[in.pos] - '0' );
        if ( value > ( UINT64_MAX - digit ) / 10 )
        {
            in.report->malformed = true;
            in.bad = true;
            return false;
        }
        value = value * 10 + digit;
        in.pos++;
    }
    label = value;
    return true;
}

// A pointer's object. Its FIRST key decides what it is: ` + "`&node`" + ` naming a label not
// yet defined, with fields after it, is a DEFINITION; ` + "`&node`" + ` naming one already
// defined, alone, is a REFERENCE; any other key is a node named once, its
// object in place. The node comes from the
// builder's arena, and the slot holds its arena offset (§6.3).
inline bool TableJsonReadPointer( TableJsonIn & in, void * slot, const TableFieldInfo * f, int32_t depth )
{
    TableJsonGraphIn * graph = (TableJsonGraphIn *) in.graph;
    if ( graph == NULL ) { in.report->malformed = true; in.bad = true; return false; }
    // the pointee nests one level down, exactly as a by-value table does, and
    // takes the same cap: a chain nests as deep as it is long (§16.7)
    if ( depth + 1 > kTableJsonMaxDepth ) { in.bad = true; return false; }
    if ( TableJsonPeek( in ) != '{' ) { in.bad = true; return false; }
    in.pos++;
    char c = TableJsonPeek( in );
    if ( c == '}' )
    {
        // an empty object: a node at its defaults, named once
        in.pos++;
        void * node = f->emplace( *graph->worker, slot );
        if ( node == NULL ) { in.report->malformed = true; in.bad = true; return false; } // the arena refused
        return true;
    }
    if ( c == 0 ) { in.bad = true; return false; }
    char key[kTableJsonMaxKey];
    int32_t key_length = 0;
    if ( !TableJsonScanString( in, key, kTableJsonMaxKey - 1, &key_length ) ) { return false; }
    key[key_length] = 0;
    if ( TableJsonPeek( in ) != ':' ) { in.bad = true; return false; }
    in.pos++;
    if ( strcmp( key, "&node" ) != 0 )
    {
        // a node named once: the pointee's object in place, and this key is
        // its first field — unless it is the reserved prefix under a spelling
        // this form does not have, which ReadTableKeys refuses
        void * node = f->emplace( *graph->worker, slot );
        if ( node == NULL ) { in.report->malformed = true; in.bad = true; return false; }
        return TableJsonReadTableKeys( in, node, f->table, depth + 1, key );
    }
    uint64_t label = 0;
    if ( !TableJsonScanLabel( in, label ) ) { return false; }
    bool taken = false;
    TableJsonGraphEntry * entry = TableJsonGraphMapReach( graph->labels, label, taken );
    if ( entry == NULL ) { in.report->malformed = true; in.bad = true; return false; }
    // ONE SPELLING, and what follows the label says which half it is: fields
    // after a label the text has not defined DEFINE it, and a label alone that
    // the text has defined REFERS to it. The other two are malformed — a label
    // alone that the text never defined, which would otherwise read as a default
    // node under a silent report, and a field after a label already defined,
    // which would be a second definition. That is what keeps a typo loud.
    c = TableJsonPeek( in );
    if ( c == ',' ) { in.pos++; c = TableJsonPeek( in ); }
    bool bare = c == '}';
    if ( bare == taken ) { in.report->malformed = true; in.bad = true; return false; }
    if ( bare )
    {
        // A REFERENCE. A label is defined when its object CLOSES, so a
        // reference met inside its own definition — at any depth of by-value
        // nesting — names a node whose descent is still open: the cycle the
        // wire refuses (§3.1), refused here where it is written. A definition
        // the reader dropped names no node, so the slot stays null with
        // nothing more counted — the drop was counted where it happened. A
        // node of another table than the slot declares is a kind mismatch, as
        // on the wire.
        in.pos++;
        if ( entry->open != 0 ) { in.report->malformed = true; in.bad = true; return false; }
        TableRef ref;
        if ( entry->type == NULL )
        {
            memcpy( slot, &ref, sizeof( ref ) );
            return true;
        }
        if ( entry->type != f->table )
        {
            memcpy( slot, &ref, sizeof( ref ) );
            in.report->kind_mismatch++;
            return true;
        }
        ref.value = (int64_t) entry->node;
        memcpy( slot, &ref, sizeof( ref ) );
        return true;
    }
    // A DEFINITION: the node is allocated, the label is its, and the keys after
    // ` + "`&node`" + ` are its fields. The entry is OPEN until the object closes, so a
    // reference to the label from inside the node's own fields is refused as
    // the cycle it is; the node and its table are filled in at the close.
    void * node = f->emplace( *graph->worker, slot );
    if ( node == NULL ) { in.report->malformed = true; in.bad = true; return false; }
    entry->open = 1;
    if ( !TableJsonReadTableKeys( in, node, f->table, depth + 1, NULL ) ) { return false; }
    entry = TableJsonGraphMapFind( graph->labels, label ); // the map may have grown under the descent
    if ( entry == NULL ) { in.report->malformed = true; in.bad = true; return false; }
    TableRef ref;
    memcpy( &ref, slot, sizeof( ref ) );
    entry->node = (uint32_t) ref.value;
    entry->type = f->table;
    entry->open = 0;
    return true;
}

// An ` + "`&`" + `-prefixed key opening an object the walk is SKIPPING — a value past an
// array's bound, an unknown key's value, a value of the wrong shape. A
// definition in there still takes its label, so the numbering survives whatever
// the storage could not hold (§16.7): the label is registered with no node, and a
// reference to it reads null. Any other prefixed key is the reserved prefix
// out of place.
inline bool TableJsonSkippedAmpersand( TableJsonIn & in, const char * key, int32_t )
{
    TableJsonGraphIn * graph = (TableJsonGraphIn *) in.graph;
    if ( graph == NULL || strcmp( key, "&node" ) != 0 ) { in.report->malformed = true; in.bad = true; return false; }
    uint64_t label = 0;
    if ( !TableJsonScanLabel( in, label ) ) { return false; }
    bool taken = false;
    if ( TableJsonGraphMapReach( graph->labels, label, taken ) == NULL ) { in.report->malformed = true; in.bad = true; return false; }
    return true; // a fresh entry is node 0, type NULL: a definition with no node
}

// ---- writing: from a region's const root ----

struct TableJsonGraphOut
{
    TableJsonGraphMap nodes; // a node's address -> how many slots name it, and its ` + "`&node`" + ` once assigned
    bool counting;           // PASS ONE: count the references, refuse a cycle, emit nothing
    int64_t next_label;
};

// The node a slot names: null as ` + "`null`" + `, a node named once as its object in
// place, and a node named more than once under the construct. Which of the
// last two it is was learned in pass one; pass two spells it.
inline bool TableJsonWritePointer( TableJsonOut & out, const void * slot, const TableFieldInfo * f, int32_t depth )
{
    TableJsonGraphOut * graph = (TableJsonGraphOut *) out.graph;
    if ( graph == NULL ) { return false; }
    const void * node = f->resolve( slot );
    if ( node == NULL )
    {
        out.raw( "null", 4 );
        return true;
    }
    bool taken = false;
    TableJsonGraphEntry * entry = TableJsonGraphMapReach( graph->nodes, (uint64_t) (uintptr_t) node, taken );
    if ( entry == NULL ) { return false; }
    if ( graph->counting )
    {
        // PASS ONE: one visit per node, every slot that names it counted, and
        // a reference to a node whose descent is still open is a cycle —
        // refused here as the wire refuses it (§3.1)
        entry->count++;
        if ( !taken ) { return entry->open == 0; }
        entry->open = 1;
        if ( !TableJsonWriteValue( out, node, f->table, depth ) ) { return false; }
        entry = TableJsonGraphMapFind( graph->nodes, (uint64_t) (uintptr_t) node ); // the map may have grown under the descent
        if ( entry == NULL ) { return false; }
        entry->open = 0;
        return true;
    }
    // PASS TWO: a node named once is its object in place; a node named more
    // than once is DEFINED at its first occurrence — ` + "`&node`" + ` first, then its
    // fields — and REFERENCED by ` + "`&node`" + ` alone after that, spelled the same way at
    // every site. Labels run from 1 in first-write order and are the text's own,
    // so a stray number in a hand-edited text is most often one never defined.
    if ( entry->count <= 1 )
    {
        return TableJsonWriteValue( out, node, f->table, depth );
    }
    if ( depth > kTableJsonMaxDepth ) { return false; }
    if ( entry->label != 0 )
    {
        out.put( '{' );
        out.line( depth + 1 );
        out.raw( "\"&node\": ", 9 );
        TableJsonWriteUnsigned( out, (uint64_t) entry->label );
        out.line( depth );
        out.put( '}' );
        return true;
    }
    entry->label = ++graph->next_label;
    out.put( '{' );
    out.line( depth + 1 );
    out.raw( "\"&node\": ", 9 );
    TableJsonWriteUnsigned( out, (uint64_t) entry->label );
    bool any = true;
    int64_t before = out.offset;
    if ( !TableJsonWriteFields( out, node, f->table, depth, any ) ) { return false; }
    // a definition carries at least one field, because a label alone is a
    // reference: a shared node with nothing to write has no definition this
    // form can spell, and the writer refuses it as it refuses any value it
    // cannot spell (§16.3)
    if ( out.offset == before ) { return false; }
    out.line( depth );
    out.put( '}' );
    return true;
}

// ---- the two entry points a pointered table's wrappers name ----

// The text into the builder's root. Every node the text names is allocated in
// the builder's arena through the field's own Emplace; the label map is the
// walk's, released before this returns. The root itself takes no label — nothing
// may name it (§16.7) — so an ` + "`&node`" + ` at the root is refused like any other key
// of the prefix.
inline bool TableJsonReadGraph( TableWorker & worker, void * root, const TableTypeInfo * info, const char * text, int64_t bytes, TableReport * report )
{
    TableReport ignored;
    if ( worker.arena == NULL ) { if ( report != NULL ) { report->malformed = true; } return false; }
    TableJsonGraphIn graph;
    graph.worker = &worker;
    TableJsonGraphMapInit( graph.labels, worker.arena->allocator );
    TableJsonIn in;
    in.text = text;
    in.size = bytes;
    in.pos = 0;
    in.report = report != NULL ? report : &ignored;
    in.bad = false;
    in.graph = &graph;
    info->reset( root );
    if ( text == NULL || bytes < 0 )
    {
        in.report->malformed = true;
        return false;
    }
    bool ok = TableJsonReadTable( in, root, info, 0 );
    if ( ok )
    {
        TableJsonSpace( in );
        if ( in.pos != in.size ) { in.bad = true; } // trailing rubbish is not one text
    }
    TableJsonGraphMapShutdown( graph.labels );
    if ( in.bad || !ok )
    {
        in.report->malformed = true;
        return false;
    }
    return true;
}

// The text of a region's const root: measured when the buffer is NULL, written
// when it is not, over one code path. Two passes over one walk — the first
// counts how many slots name each node and refuses a cycle, the second writes
// — so a node's first occurrence knows whether it will be named again. The
// ROOT's entry is open for the whole first pass, so a reference back at it is
// the cycle it is (§3.1), and it takes no label.
inline int64_t TableJsonWriteGraph( const void * root, const TableTypeInfo * info, char * buffer, int64_t capacity, TableAllocator allocator )
{
    if ( root == NULL ) { return -1; }
    TableJsonGraphOut graph;
    TableJsonGraphMapInit( graph.nodes, allocator );
    graph.counting = true;
    graph.next_label = 0;
    bool taken = false;
    TableJsonGraphEntry * entry = TableJsonGraphMapReach( graph.nodes, (uint64_t) (uintptr_t) root, taken );
    if ( entry == NULL ) { TableJsonGraphMapShutdown( graph.nodes ); return -1; }
    entry->open = 1;
    TableJsonOut count;
    count.buffer = NULL;
    count.capacity = 0;
    count.offset = 0;
    count.overflow = false;
    count.graph = &graph;
    bool ok = TableJsonWriteValue( count, root, info, 0 );
    graph.counting = false;
    TableJsonOut out;
    out.buffer = buffer;
    out.capacity = capacity;
    out.offset = 0;
    out.overflow = false;
    out.graph = &graph;
    if ( ok ) { ok = TableJsonWriteValue( out, root, info, 0 ); }
    TableJsonGraphMapShutdown( graph.nodes );
    if ( !ok ) { return -1; }
    out.put( '\n' ); // the canonical text ends with exactly one newline (§16.1)
    if ( out.overflow ) { return -1; }
    return out.offset;
}

// ---- json graph walk: end ----
`
