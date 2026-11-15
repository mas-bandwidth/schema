// The STRICT FORM-1 WALK emitted into every generated Table header
// (docs/SPEC-TABLES.md §6.5). It is the declaration-free transcription of the
// engine's oracle (internal/tablewire/wirecheck.go): the conditions decidable
// from the wire alone, applied to every byte of a form-1 file's root body, in
// fields and records this reader declares and in those it does not.
//
// Skipped is not unread: an unknown field is walked by the same rules as a
// known one, so damage inside it refuses the load. Each form-1 read runs this
// walk before any value is decoded, and a false answer is the strict read's
// refusal, with nothing else defined (D1). The walk allocates nothing of its
// own: the one reach set a pointer-bearing unit needs is a mark array it
// obtains through the allocator hook, sized by the node count the FILE actually
// holds, never by a count the input claims. A unit that declares no
// pointer-bearing type needs no marks at all — a fixed reader has no reference
// that could reach a node, so it refuses a non-empty node table outright.
//
// A non-finite float (§3.3) and a count above the int32 extent cap (§2.9)
// are refused here even in fields the decoder skips: their verdict is
// decidable from the wire. The first such condition carries its diagnostic
// name through the walk, so a declared field keeps its existing reason.
package cpptable

// tableStrictRuntime returns the shared walk: the cursor, the two bounds, and
// the body/content/payload/union walks with their scalar, seen-ref and
// element-kind helpers. It is emitted into every Table header, before the
// arena runtime that the pointered entry below calls.
func tableStrictRuntime() string {
	return `// THE STRICT FORM-1 WALK (docs/SPEC-TABLES.md §6.5): the conditions decidable
// from the wire alone, applied to EVERY byte of a form-1 file's root body — in
// fields and records this reader declares and in those it does not. Skipped is
// not unread: an unknown field is walked by the same rules as a known one, so
// damage inside it refuses the load. Every form-1 read runs this walk before
// any value is decoded, and a false answer is the strict read's refusal, with
// nothing else defined (D1). The walk takes no declaration and allocates
// nothing of its own: the one reach set a pointer-bearing unit needs is a mark
// array it obtains through the allocator hook, sized by the node count the
// FILE actually holds, never by a count the input claims. A unit that declares
// no pointer-bearing type needs no marks at all: a fixed reader has no
// reference that could reach a node, so it refuses a non-empty node table
// outright.

// The walk's cursor: the id table a body's references resolve against, and the
// node-table field's payload when the root body carries one (the entry reads it
// after the root body is walked).
struct TableStrict
{
    const TableIdTable * ids;
    const uint8_t * node_table;
    int64_t node_table_bytes;
    bool node_table_present;
    // whether this reader carries pointer-bearing types and can therefore
    // reach a node: a fixed reader cannot, so a non-null pointer is damage
    bool pointers;
    TableRefuseReason refusal;
};

// THE BINARY NESTING BOUND (docs/SPEC-TABLES.md §4): a framed payload is one
// level deeper than the payload holding it.
static const int kTableStrictMaxDepth = 128;
// THE SEEN-REF BITSET: a body names no more than the id table's bound, so 4096
// bits (64 words) on the stack tracks the references a body has seen (R5, H7).
static const int32_t kTableStrictSeenWords = 64;

inline bool TableStrictBody( TableStrict & s, const uint8_t * b, int64_t n, bool root, int depth );
inline bool TableStrictContent( TableStrict & s, const uint8_t * b, int64_t n, uint8_t kind, int depth );
inline bool TableStrictPayload( TableStrict & s, TableReader & r, uint8_t kind, int depth );
inline bool TableStrictUnion( TableStrict & s, TableReader & r, int depth );
inline bool TableStrictArray( TableStrict & s, const uint8_t * b, int64_t n, int depth );
inline bool TableStrictKeyed( TableStrict & s, const uint8_t * b, int64_t n, int depth );

// a fixed-width kind's payload size in bytes, 0 for the framed kinds: the same
// closed set the kind-width helper carries, but for the skip rule's own
// set — a pointer (17), an enum (30) and a union (15) are framed, not fixed
inline int32_t TableStrictWidth( uint8_t kind )
{
    switch ( kind )
    {
        case 1: case 2: case 6: case 20: case 25: return 1;
        case 3: case 7: case 21: case 26: return 2;
        case 4: case 8: case 10: case 22: case 27: return 4;
        case 5: case 9: case 11: case 23: case 28: case 36: return 8;
        case 18: case 19: case 24: case 29: return 16;
    }
    return 0;
}

// test-and-set one body reference into the seen bitset; false = the same field
// id or keyed slot twice in one body (R5, H7)
inline bool TableStrictSeen( uint64_t * seen, uint64_t ref )
{
    const uint64_t word = ( ref - 1 ) >> 6;
    const uint64_t bit = 1ull << ( ( ref - 1 ) & 63 );
    if ( seen[word] & bit ) { return false; }
    seen[word] |= bit;
    return true;
}

// a fixed-width value's bytes: a bool byte other than 0 or 1 is damage
inline bool TableStrictScalar( TableStrict & s, const uint8_t * b, uint8_t kind )
{
    if ( kind == 1 ) { return b[0] <= 1; }
    // Kinds 10 and 11 are the plain floats. Their all-ones exponent is
    // visible in the raw little-endian wire bytes, including skipped fields.
    const bool non_finite32 = kind == 10 && ( b[3] & 0x7f ) == 0x7f && ( b[2] & 0x80 ) != 0;
    const bool non_finite64 = kind == 11 && ( b[7] & 0x7f ) == 0x7f && ( b[6] & 0xf0 ) == 0xf0;
    if ( non_finite32 || non_finite64 ) { s.refusal = non_finite_float; return false; }
    return true;
}

inline bool TableStrictFailed( const TableStrict & s, TableReport * report )
{
    if ( s.refusal == non_finite_float ) { TableNonFiniteFloat( report ); }
    else if ( s.refusal == count_over_extent_cap )
    {
        report->refused = true;
        report->reason = count_over_extent_cap;
        report->refuse_reason = count_over_extent_cap;
    }
    else { report->damaged = true; report->refuse_reason = wire_damaged; }
    return false;
}

// THE ELEMENT KINDS ARE CLOSED (docs/SPEC-TABLES.md §3): an array body opens
// with 1-13, 15, 17, 18-29 or 30; a keyed body with 1-11, 13, 18-29 or 30.
inline bool TableStrictArrayElemKind( uint8_t kind )
{
    return ( kind >= 1 && kind <= 13 ) || kind == 15 || kind == 17 || ( kind >= 18 && kind <= 29 ) || kind == 30;
}
inline bool TableStrictKeyedElemKind( uint8_t kind )
{
    return ( kind >= 1 && kind <= 11 ) || kind == 13 || ( kind >= 18 && kind <= 29 ) || kind == 30;
}

inline bool TableStrictBody( TableStrict & s, const uint8_t * b, int64_t n, bool root, int depth )
{
    uint64_t seen[kTableStrictSeenWords];
    for ( int32_t w = 0; w < kTableStrictSeenWords; w++ ) { seen[w] = 0; }
    TableReport ignored;
    TableReader r( b, n, &ignored, s.ids );
    for ( ;; )
    {
        uint64_t ref = 0;
        if ( !r.getleb( ref ) ) { return false; }
        if ( ref == 0 ) { return r.offset == n; } // the terminator is the body's last byte (L3)
        if ( s.ids == NULL || ref > (uint64_t) s.ids->count ) { return false; } // F9
        const uint64_t id = s.ids->at( ref );
        if ( !TableStrictSeen( seen, ref ) ) { return false; } // R5: one field id twice in one body
        if ( !r.has( 1 ) ) { return false; }
        const uint8_t kind = r.get8();
        if ( id == kTableNodeTableFieldId )
        {
            // the node table rides once, at the root, as kind 12 (T1, T5)
            if ( !root || kind != 12 ) { return false; }
            uint64_t len = 0;
            if ( !r.getleb( len ) || !r.room( len ) ) { return false; }
            s.node_table = r.buffer + r.offset;
            s.node_table_bytes = (int64_t) len;
            s.node_table_present = true;
            r.offset += (int64_t) len;
            continue;
        }
        // a reserved announcement id out of place (R6): they ride the message
        // form's announcement, never a file's body
        if ( id == 0xFFFFFFFFFFFFFFFEull || id == 0xFFFFFFFFFFFFFFFDull ) { return false; }
        if ( !TableStrictPayload( s, r, kind, depth ) ) { return false; }
    }
}

// content walks a payload already framed by a length, and requires it to be
// consumed exactly: a field's L, an arm's L, a keyed slot's L.
inline bool TableStrictContent( TableStrict & s, const uint8_t * b, int64_t n, uint8_t kind, int depth )
{
    const int32_t width = TableStrictWidth( kind );
    if ( width > 0 )
    {
        if ( n != width ) { return false; } // an L that is not its kind's width (H9)
        return TableStrictScalar( s, b, kind );
    }
    switch ( kind )
    {
        case 12: return TableUtf8Valid( b, (uint64_t) n ); // ill-formed text is damage
        case 31: return true; // the escape: opaque by rule (§3)
        case 32: return n == 0; // kind 32 with a nonzero L is damage
        case 13:
        {
            if ( depth >= kTableStrictMaxDepth ) { return false; }
            return TableStrictBody( s, b, n, false, depth + 1 );
        }
        case 14:
        {
            if ( depth >= kTableStrictMaxDepth ) { return false; }
            return TableStrictArray( s, b, n, depth + 1 );
        }
        case 16:
        {
            if ( depth >= kTableStrictMaxDepth ) { return false; }
            return TableStrictKeyed( s, b, n, depth + 1 );
        }
        case 15: case 17: case 30:
        {
            TableReport ignored;
            TableReader r( b, n, &ignored, s.ids );
            if ( !TableStrictPayload( s, r, kind, depth ) ) { return false; }
            return r.offset == n; // bytes left inside an arm after its payload
        }
    }
    return false; // a kind no reader can skip (R3)
}

// payload walks one SELF-FRAMED payload of a given kind: what follows a field's
// kind byte, and an array element.
inline bool TableStrictPayload( TableStrict & s, TableReader & r, uint8_t kind, int depth )
{
    const int32_t width = TableStrictWidth( kind );
    if ( width > 0 )
    {
        if ( !r.has( width ) ) { return false; }
        if ( !TableStrictScalar( s, r.buffer + r.offset, kind ) ) { return false; }
        r.offset += width;
        return true;
    }
    switch ( kind )
    {
        case 17: // a pointer: the index is bounds-checked at the graph, never here (T7)
        {
            uint64_t index = 0;
            if ( !r.getleb( index ) ) { return false; }
            // a fixed reader has no reference that could reach a node, so a
            // non-null pointer is a node no numbering can hold (§3.1, §6.5)
            return s.pointers || index == 0;
        }
        case 30: // an enum: a variant reference into the id table
        {
            uint64_t ref = 0;
            return r.getleb( ref ) && ( s.ids == NULL || ref <= (uint64_t) s.ids->count );
        }
        case 15:
            return TableStrictUnion( s, r, depth );
        case 12: case 31: case 32: case 13: case 14: case 16:
        {
            uint64_t n = 0;
            if ( !r.getleb( n ) || !r.room( n ) ) { return false; } // L4
            const uint8_t * b = r.buffer + r.offset;
            r.offset += (int64_t) n;
            return TableStrictContent( s, b, (int64_t) n, kind, depth );
        }
    }
    return false; // a kind no reader can skip (R3)
}

// union walks a union payload in place: the arm reference, and for a set arm
// the arm's kind, its L and the content L frames.
inline bool TableStrictUnion( TableStrict & s, TableReader & r, int depth )
{
    uint64_t ref = 0;
    if ( !r.getleb( ref ) ) { return false; }
    if ( ref == 0 ) { return true; } // the empty arm carries nothing
    if ( s.ids == NULL || ref > (uint64_t) s.ids->count || !r.has( 1 ) ) { return false; }
    const uint8_t kind = r.get8();
    uint64_t len = 0;
    if ( !r.getleb( len ) || !r.room( len ) ) { return false; }
    const uint8_t * b = r.buffer + r.offset;
    r.offset += (int64_t) len;
    // a union is a level when its arm is structural (docs/SPEC-TABLES.md §4)
    const bool structural = kind == 13 || kind == 14 || kind == 15 || kind == 16;
    if ( structural )
    {
        if ( depth >= kTableStrictMaxDepth ) { return false; }
        return TableStrictContent( s, b, (int64_t) len, kind, depth + 1 );
    }
    return TableStrictContent( s, b, (int64_t) len, kind, depth );
}

// THE HEADER LIES INSIDE ITS OWN L (H2, H3): a body too short to carry the
// element kind and the count is damage, inert at any value.
inline bool TableStrictArray( TableStrict & s, const uint8_t * b, int64_t n, int depth )
{
    if ( n < 2 ) { return false; }
    const uint8_t elem = b[0];
    if ( !TableStrictArrayElemKind( elem ) ) { return false; } // an element kind outside the closed list
    TableReport ignored;
    TableReader r( b + 1, n - 1, &ignored, s.ids );
    uint64_t count = 0;
    if ( !r.getleb( count ) ) { return false; }
    // the cap applies even when this array belongs to an unknown field
    if ( count > 0x7FFFFFFFull ) { s.refusal = count_over_extent_cap; return false; }
    for ( uint64_t i = 0; i < count; i++ )
    {
        if ( !TableStrictPayload( s, r, elem, depth ) ) { return false; }
    }
    return r.offset == r.size; // bytes left inside an array's L after its elements (L6)
}

inline bool TableStrictKeyed( TableStrict & s, const uint8_t * b, int64_t n, int depth )
{
    if ( n < 2 ) { return false; }
    const uint8_t elem = b[0];
    if ( !TableStrictKeyedElemKind( elem ) ) { return false; }
    TableReport ignored;
    TableReader r( b + 1, n - 1, &ignored, s.ids );
    uint64_t count = 0;
    if ( !r.getleb( count ) ) { return false; }
    // the cap applies even when this keyed array belongs to an unknown field
    if ( count > 0x7FFFFFFFull ) { s.refusal = count_over_extent_cap; return false; }
    uint64_t seen[kTableStrictSeenWords];
    for ( int32_t w = 0; w < kTableStrictSeenWords; w++ ) { seen[w] = 0; }
    for ( uint64_t i = 0; i < count; i++ )
    {
        uint64_t key = 0;
        if ( !r.getleb( key ) || key == 0 || s.ids == NULL || key > (uint64_t) s.ids->count ) { return false; } // H7
        if ( !TableStrictSeen( seen, key ) ) { return false; } // the same keyed slot twice
        uint64_t len = 0;
        if ( !r.getleb( len ) || !r.room( len ) ) { return false; }
        const uint8_t * eb = r.buffer + r.offset;
        r.offset += (int64_t) len;
        if ( !TableStrictContent( s, eb, (int64_t) len, elem, depth ) ) { return false; }
    }
    return r.offset == r.size; // bytes left inside a keyed array's L after its slots
}

`
}

// tableStrictFixedEntry returns the FIXED reader's entry: walk the root body,
// and refuse a non-empty node table outright — a fixed reader has no reference
// that could reach a node, so no mark array is needed.
func tableStrictFixedEntry() string {
	return `// The FIXED reader's strict walk: the root body, then a non-empty node table
// refused outright — no reference could reach a node it numbered.
inline bool TableStrictWalk( const uint8_t * body, int64_t body_bytes, const TableIdTable & table, TableReport * report )
{
    TableStrict s;
    s.ids = &table;
    s.node_table = NULL;
    s.node_table_bytes = 0;
    s.node_table_present = false;
    s.pointers = false;
    s.refusal = wire_damaged;
    if ( !TableStrictBody( s, body, body_bytes, true, 0 ) ) { return TableStrictFailed( s, report ); }
    if ( s.node_table_present )
    {
        // an empty node table (a lone zero count) is lawful and carries nothing;
        // any record in it is a node no reference can reach (§3.1, §6.5)
        TableReport ignored;
        TableReader r( s.node_table, s.node_table_bytes, &ignored, &table );
        uint64_t count = 0;
        if ( !r.getleb( count ) || count != 0 || r.offset != s.node_table_bytes )
        {
            report->damaged = true;
            report->refuse_reason = wire_damaged;
            return false;
        }
    }
    return true;
}

`
}

// tableStrictPointerEntry returns the POINTERED reader's entry: walk the root
// body and every record's body, then judge the pointer graph through the arena
// runtime's TableGraphCheck over a mark array sized by the node count the file
// actually holds. It rides after the arena runtime, whose names it calls.
func tableStrictPointerEntry() string {
	return `// The POINTERED reader's strict walk: the root body, every record's body, and
// the pointer graph over a mark array sized by the node count the file actually
// holds (the allocator hook's, released before the read returns).
inline bool TableStrictWalk( const uint8_t * body, int64_t body_bytes, const TableIdTable & table, TableReport * report )
{
    TableStrict s;
    s.ids = &table;
    s.node_table = NULL;
    s.node_table_bytes = 0;
    s.node_table_present = false;
    s.pointers = true;
    s.refusal = wire_damaged;
    if ( !TableStrictBody( s, body, body_bytes, true, 0 ) ) { return TableStrictFailed( s, report ); }
    int64_t records = 0;
    if ( s.node_table_present )
    {
        uint64_t type_id = 0;
        const uint8_t * rec = NULL;
        int64_t len = 0;
        TableReport ignored;
        TableNodeScan scan = TableNodeScanBegin( body, body_bytes, &ignored, &table );
        while ( TableNodeScanNext( scan, type_id, rec, len ) )
        {
            // a blob record is its bytes; the graph check reads its text (§3.1)
            if ( type_id != kTableBytesTypeId && type_id != kTableStringTypeId )
            {
                if ( !TableStrictBody( s, rec, len, false, 0 ) ) { return TableStrictFailed( s, report ); }
            }
            records++;
        }
        if ( !TableNodeScanWhole( scan ) ) { report->damaged = true; report->refuse_reason = wire_damaged; return false; }
    }
    TableNodeDirEntry * scratch = (TableNodeDirEntry *) schema_allocate( ( records + 1 ) * (int64_t) sizeof( TableNodeDirEntry ) );
    if ( scratch == NULL ) { report->damaged = true; return false; }
    const bool ok = TableGraphCheck( body, body_bytes, &table, scratch, records + 1, report );
    schema_release( scratch );
    return ok;
}

`
}
