package cpptable

// tableFixedRuntime is the FIXED FORM's package-scoped runtime
// (docs/SPEC-TABLES.md §3.4): the plan entry, the constexpr plan builder, the
// ONE read loop, the block reader and the plan compiler. It rides inside the
// same package guard as the rest of the primitives, so whichever <Base>Table.h
// a translation unit includes first defines it for the whole unit.
//
// EVERYTHING HERE IS ALLOCATION-FREE, which is this library's rule everywhere
// else and has no exception on this form: the plan's storage is the caller's,
// declared by capacity, and a block whose plan does not fit is a refusal by
// name.
const tableFixedRuntime = `
// ---------------------------------------------------------------------------
// THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4)
// ---------------------------------------------------------------------------

inline constexpr uint8_t kTableFixedForm = 3;

// THE OPS ARE THE WHOLE SET. The IDENTITY plan carries only the first three;
// the other two are what a plan compiled from another writer's block adds.
enum : uint8_t
{
    kTableFixedCopy    = 0, // move size bytes
    kTableFixedCount   = 1, // a count: clamp it to the reader's own bound
    kTableFixedText    = 2, // a length, then the units, then terminate
    kTableFixedOrdinal = 3, // a variant ordinal, remapped through the plan's own table
    kTableFixedWiden   = 4, // a narrower source into a wider destination
    kTableFixedConst   = 5, // a constant this reader's own storage takes: a remapped union tag
};

// arg on a kTableFixedText entry
enum : uint8_t
{
    kTableFixedTextUtf8  = 1,
    kTableFixedTextWide  = 2,
    kTableFixedTextBytes = 3,
};

inline constexpr uint32_t kTableFixedNoGuard = 0xFFFFFFFFu;

// A PLAN ENTRY. src and dst are byte offsets — into the record's body and into
// the reader's own storage. guard is the byte offset of a union tag when the
// entry belongs to an arm, and kTableFixedNoGuard when it does not.
struct TableFixedEntry
{
    uint32_t src = 0;
    uint32_t dst = 0;
    uint32_t size = 0;
    uint32_t aux = 0;
    uint32_t guard = kTableFixedNoGuard;
    uint8_t op = 0;
    uint8_t arg = 0;
    uint8_t dstsize = 0;
};

template <int N> struct TableFixedPlan
{
    TableFixedEntry entries[N];
    int32_t count;
};

// THE COALESCER, and it is the only optimization a plan compiler performs:
// two neighbouring COPY entries whose source and destination both advance
// together are one entry. It runs at COMPILE TIME on the identity plan and at
// plan-compile time on any other, so the two are the same array read by the
// same loop.
template <int N, typename F> constexpr TableFixedPlan<N> TableFixedBuildPlan( F emit )
{
    TableFixedEntry raw[N] = {};
    const int written = emit( raw, (uint32_t) 0, (uint32_t) 0 );
    TableFixedPlan<N> out{};
    out.count = 0;
    for ( int i = 0; i < written; ++i )
    {
        if ( out.count > 0 &&
             out.entries[out.count - 1].op == kTableFixedCopy && raw[i].op == kTableFixedCopy &&
             out.entries[out.count - 1].guard == raw[i].guard &&
             out.entries[out.count - 1].arg == raw[i].arg &&
             out.entries[out.count - 1].src + out.entries[out.count - 1].size == raw[i].src &&
             out.entries[out.count - 1].dst + out.entries[out.count - 1].size == raw[i].dst )
        {
            out.entries[out.count - 1].size += raw[i].size;
            continue;
        }
        out.entries[out.count] = raw[i];
        out.count++;
    }
    return out;
}

// ---- the little-endian moves -----------------------------------------------

inline void TableFixedPut8( uint8_t * b, uint8_t v ) { b[0] = v; }
inline void TableFixedPut16( uint8_t * b, uint16_t v ) { b[0] = (uint8_t)( v ); b[1] = (uint8_t)( v >> 8 ); }
inline void TableFixedPut32( uint8_t * b, uint32_t v ) { for ( int i = 0; i < 4; ++i ) { b[i] = (uint8_t)( v >> ( 8 * i ) ); } }
inline void TableFixedPut64( uint8_t * b, uint64_t v ) { for ( int i = 0; i < 8; ++i ) { b[i] = (uint8_t)( v >> ( 8 * i ) ); } }
inline void TableFixedPutF32( uint8_t * b, float v ) { uint32_t w; memcpy( &w, &v, 4 ); TableFixedPut32( b, w ); }
inline void TableFixedPutF64( uint8_t * b, double v ) { uint64_t w; memcpy( &w, &v, 8 ); TableFixedPut64( b, w ); }
template <typename T> inline void TableFixedPut128( uint8_t * b, T v )
{
    // sixteen bytes, the LOW 64-bit half first, which is this wire's order for
    // the family everywhere else (docs/SPEC-TABLES.md §3)
    TableFixedPut64( b, (uint64_t) v );
    TableFixedPut64( b + 8, (uint64_t) ( v >> 64 ) );
}
inline uint32_t TableFixedGet32( const uint8_t * b )
{
    uint32_t v = 0;
    for ( int i = 0; i < 4; ++i ) { v |= ( (uint32_t) b[i] ) << ( 8 * i ); }
    return v;
}
inline uint64_t TableFixedGet64( const uint8_t * b )
{
    uint64_t v = 0;
    for ( int i = 0; i < 8; ++i ) { v |= ( (uint64_t) b[i] ) << ( 8 * i ); }
    return v;
}

// THE HASH is fnv1a64 over the block's bytes exactly as written (§3.4).
inline uint64_t TableFixedHashOf( const uint8_t * block, int64_t bytes )
{
    uint64_t h = 0xcbf29ce484222325ull;
    for ( int64_t i = 0; i < bytes; ++i )
    {
        h ^= (uint64_t) block[i];
        h *= 0x100000001b3ull;
    }
    return h;
}

// ---- THE RUN COPY ----------------------------------------------------------
//
// A RUN IS A SMALL, KNOWN NUMBER OF BYTES AND THE COPY IS WRITTEN AS
// OVERLAPPING UNALIGNED WORD MOVES, NOT AS A CALL. This is a REQUIREMENT of
// the form and not an optimization a port may skip: the ruling that there is
// ONE reader path rests on a measurement, and with a runtime-length memcpy per
// entry that same measurement is 3.1x instead of 1.3x
// (test/bench/fixedform_measure.cpp).
inline void TableFixedCopyRun( uint8_t * d, const uint8_t * s, uint32_t n )
{
    if ( n <= 16 )
    {
        if ( n >= 8 )
        {
            uint64_t a, b;
            memcpy( &a, s, 8 ); memcpy( &b, s + n - 8, 8 );
            memcpy( d, &a, 8 ); memcpy( d + n - 8, &b, 8 );
        }
        else if ( n >= 4 )
        {
            uint32_t a, b;
            memcpy( &a, s, 4 ); memcpy( &b, s + n - 4, 4 );
            memcpy( d, &a, 4 ); memcpy( d + n - 4, &b, 4 );
        }
        else if ( n > 0 )
        {
            d[0] = s[0]; d[n >> 1] = s[n >> 1]; d[n - 1] = s[n - 1];
        }
        return;
    }
    if ( n <= 64 )
    {
        uint8_t w[32];
        memcpy( w, s, 16 ); memcpy( d, w, 16 );
        memcpy( w, s + 16, 16 ); memcpy( d + 16, w, 16 );
        memcpy( w, s + n - 32, 32 ); memcpy( d + n - 32, w, 32 );
        return;
    }
    memcpy( d, s, n );
}

// ---- THE ONE READ LOOP -----------------------------------------------------
//
// A READ IS A PREFILL AND THIS LOOP, AND NOTHING ELSE. Which plan it is handed
// is the only thing that differs between reading this build's own record and
// reading anybody else's.
inline void TableFixedRun( const TableFixedEntry * plan, int32_t count, const uint8_t * src, uint8_t * dst, TableReport * report )
{
    for ( int32_t i = 0; i < count; ++i )
    {
        const TableFixedEntry & p = plan[i];
        if ( p.guard != kTableFixedNoGuard && src[p.guard] != p.arg ) { continue; }
        switch ( p.op )
        {
            case kTableFixedCopy:
            {
                TableFixedCopyRun( dst + p.dst, src + p.src, p.size );
                break;
            }
            case kTableFixedCount:
            {
                int32_t v = (int32_t) TableFixedGet32( src + p.src );
                if ( v < 0 ) { v = 0; report->clamped++; }
                else if ( v > (int32_t) p.size ) { v = (int32_t) p.size; report->clamped++; }
                memcpy( dst + p.dst, &v, 4 );
                break;
            }
            case kTableFixedText:
            {
                const uint32_t unit = ( p.arg == kTableFixedTextWide ) ? 2u : 1u;
                const uint32_t cap = p.size / unit;
                int32_t v = (int32_t) TableFixedGet32( src + p.src );
                if ( v < 0 ) { v = 0; report->clamped++; }
                else if ( (uint32_t) v > cap ) { v = (int32_t) cap; report->clamped++; }
                memcpy( dst + p.dst, &v, 4 );
                TableFixedCopyRun( dst + p.aux, src + p.src + 4, p.size );
                if ( p.arg != kTableFixedTextBytes )
                {
                    // the used length terminates the buffer, whose storage is
                    // one unit longer than the bound for exactly this
                    memset( dst + p.aux + (uint32_t) v * unit, 0, unit );
                }
                break;
            }
            case kTableFixedOrdinal:
            {
                // A VARIANT ORDINAL IS ITS POSITION IN THE BLOCK, so a writer
                // whose enum gained a variant IN THE MIDDLE is remapped here
                // and never reinterpreted. The table is the plan's own, laid
                // down by the compiler above the entries.
                uint32_t raw = 0;
                memcpy( &raw, src + p.src, p.size );
                const uint16_t * table = (const uint16_t *) (const void *) ( (const uint8_t *) plan + p.aux );
                uint32_t v = 0;
                if ( raw != 0 && raw <= table[0] ) { v = table[raw]; }
                memcpy( dst + p.dst, &v, p.dstsize );
                break;
            }
            case kTableFixedWiden:
            {
                uint64_t raw = 0;
                memcpy( &raw, src + p.src, p.size );
                memcpy( dst + p.dst, &raw, p.dstsize );
                report->widened++;
                break;
            }
            case kTableFixedConst:
            {
                memcpy( dst + p.dst, &p.aux, p.size );
                break;
            }
            default: break;
        }
    }
}

// ---- THE BLOCK -------------------------------------------------------------
//
// An entry is SEVENTEEN BYTES and the block is a count and a run of them. THE
// FORMAT NEVER MOVES, which is what lets a reader of this major parse a later
// major's block and step over a kind it does not know by the size the entry
// states.

inline constexpr int32_t kTableFixedEntryBytes = 17;

struct TableFixedBlockEntry
{
    uint64_t id = 0;
    uint32_t size = 0;
    uint32_t children = 0;
    uint8_t kind = 0;
};

struct TableFixedBlockView
{
    const uint8_t * bytes = NULL;
    int32_t count = 0;
};

inline TableFixedBlockEntry TableFixedEntryAt( const TableFixedBlockView & b, int32_t i )
{
    const uint8_t * e = b.bytes + 4 + (int64_t) i * kTableFixedEntryBytes;
    TableFixedBlockEntry out;
    out.id = TableFixedGet64( e );
    out.kind = e[8];
    out.size = TableFixedGet32( e + 9 );
    out.children = TableFixedGet32( e + 13 );
    return out;
}

// TableFixedSubtree is how many entries the subtree rooted at i occupies, so a
// walk steps over a child it does not want without knowing what is in it.
inline int32_t TableFixedSubtree( const TableFixedBlockView & b, int32_t i )
{
    if ( i < 0 || i >= b.count ) { return 1; }
    const TableFixedBlockEntry e = TableFixedEntryAt( b, i );
    int32_t n = 1;
    int32_t at = i + 1;
    for ( uint32_t c = 0; c < e.children; ++c )
    {
        if ( at >= b.count ) { break; }
        const int32_t sub = TableFixedSubtree( b, at );
        at += sub;
        n += sub;
    }
    return n;
}

inline uint32_t TableFixedUnionArmBytes( const TableFixedBlockView & b, int32_t i )
{
    const TableFixedBlockEntry e = TableFixedEntryAt( b, i );
    uint32_t widest = 0;
    int32_t at = i + 1;
    for ( uint32_t k = 0; k < e.children; ++k )
    {
        const TableFixedBlockEntry a = TableFixedEntryAt( b, at );
        if ( a.size > widest ) { widest = a.size; }
        at += TableFixedSubtree( b, at );
    }
    return widest;
}

inline uint32_t TableFixedTagBytes( const TableFixedBlockView & b, int32_t i )
{
    return TableFixedEntryAt( b, i ).size - TableFixedUnionArmBytes( b, i );
}

inline bool TableFixedParseBlock( const uint8_t * bytes, int64_t length, TableFixedBlockView & out )
{
    if ( bytes == NULL || length < 4 ) { return false; }
    const uint32_t count = TableFixedGet32( bytes );
    if ( count == 0 || (int64_t) count * kTableFixedEntryBytes + 4 != length ) { return false; }
    out.bytes = bytes;
    out.count = (int32_t) count;
    // THE TREE HAS TO CLOSE: the root's subtree is the whole block
    if ( TableFixedSubtree( out, 0 ) != out.count ) { out.bytes = NULL; out.count = 0; return false; }
    return true;
}

// ---- THE PLAN COMPILER -----------------------------------------------------
//
// The SAME LOOP runs over this plan as over the identity plan. What the
// compiler does, once per peer, is what a read would otherwise do once per
// record: map the writer's ids onto this reader's fields; leave a field it
// cannot name OUT of the plan, which is what skips it, by arithmetic that was
// going to step past it anyway; and leave a field the writer does not carry
// out too, which is what defaults it, because the prefill already put the
// declared default there.

// TableFixedDst is MY side of the walk: one row per entry of my own block,
// carrying the storage facts a block entry cannot.
struct TableFixedDst
{
    uint32_t dst = 0;    // this entry's storage offset inside its parent
    uint32_t stride = 0; // an array entry's storage stride
    uint32_t aux = 0;    // a text field's buffer offset
    uint8_t counted = 0; // an array that carries a live count
    uint8_t arg = 0;     // a text field's flavour
};

struct TableFixedCompiler
{
    TableFixedEntry * plan = NULL;
    int32_t capacity = 0;
    int32_t count = 0;
    int32_t pool = 0; // bytes of remap table laid down from the TOP, downward
    bool overflow = false;
    TableReport * report = NULL;
};

inline void TableFixedPush( TableFixedCompiler & c, const TableFixedEntry & e )
{
    const int32_t room = c.capacity - ( c.pool + (int32_t) sizeof( TableFixedEntry ) - 1 ) / (int32_t) sizeof( TableFixedEntry );
    if ( c.count >= room ) { c.overflow = true; return; }
    c.plan[c.count++] = e;
}

// TableFixedLayTable lays a remap table above the entries and answers its byte
// offset from the plan's base. Tables grow DOWNWARD from the top, so the
// coalescing pass, which only moves entries down, never moves one.
inline uint32_t TableFixedLayTable( TableFixedCompiler & c, const uint16_t * values, int32_t n )
{
    const int32_t bytes = ( n + 1 ) * (int32_t) sizeof( uint16_t );
    const int32_t total = (int32_t) sizeof( TableFixedEntry ) * c.capacity;
    c.pool += bytes;
    if ( total - c.pool < c.count * (int32_t) sizeof( TableFixedEntry ) ) { c.overflow = true; return 0; }
    const uint32_t at = (uint32_t) ( total - c.pool );
    uint16_t * dst = (uint16_t *) (void *) ( (uint8_t *) c.plan + at );
    dst[0] = (uint16_t) n;
    for ( int32_t i = 0; i < n; ++i ) { dst[1 + i] = values[i]; }
    return at;
}

inline void TableFixedCompileEntry( TableFixedCompiler & c,
                                    const TableFixedBlockView & theirs, int32_t ti, uint32_t their_at,
                                    const TableFixedBlockView & mine, int32_t mi, const TableFixedDst * dst, uint32_t my_at,
                                    uint32_t guard, uint8_t arg );

// TableFixedMatchChildren walks a TABLE's children on both sides.
inline void TableFixedMatchChildren( TableFixedCompiler & c,
                                     const TableFixedBlockView & theirs, int32_t ti, uint32_t their_at,
                                     const TableFixedBlockView & mine, int32_t mi, const TableFixedDst * dst, uint32_t my_at,
                                     uint32_t guard, uint8_t arg )
{
    const TableFixedBlockEntry te = TableFixedEntryAt( theirs, ti );
    const TableFixedBlockEntry me = TableFixedEntryAt( mine, mi );
    int32_t my_child = mi + 1;
    for ( uint32_t k = 0; k < me.children; ++k )
    {
        const TableFixedBlockEntry mc = TableFixedEntryAt( mine, my_child );
        int32_t their_child = ti + 1;
        uint32_t their_off = their_at;
        for ( uint32_t j = 0; j < te.children; ++j )
        {
            const TableFixedBlockEntry tc = TableFixedEntryAt( theirs, their_child );
            if ( tc.id == mc.id )
            {
                TableFixedCompileEntry( c, theirs, their_child, their_off, mine, my_child, dst, my_at, guard, arg );
                break;
            }
            their_off += tc.size;
            their_child += TableFixedSubtree( theirs, their_child );
        }
        my_child += TableFixedSubtree( mine, my_child );
    }
    // EVERY FIELD OF THEIRS I COULD NOT NAME IS ONE unknown
    int32_t tc_at = ti + 1;
    for ( uint32_t j = 0; j < te.children; ++j )
    {
        const TableFixedBlockEntry tc = TableFixedEntryAt( theirs, tc_at );
        bool named = false;
        int32_t mc_at = mi + 1;
        for ( uint32_t k = 0; k < me.children; ++k )
        {
            if ( TableFixedEntryAt( mine, mc_at ).id == tc.id ) { named = true; break; }
            mc_at += TableFixedSubtree( mine, mc_at );
        }
        if ( !named && c.report != NULL ) { c.report->unknown++; }
        tc_at += TableFixedSubtree( theirs, tc_at );
    }
}

inline void TableFixedCompileEntry( TableFixedCompiler & c,
                                    const TableFixedBlockView & theirs, int32_t ti, uint32_t their_at,
                                    const TableFixedBlockView & mine, int32_t mi, const TableFixedDst * dst, uint32_t my_at,
                                    uint32_t guard, uint8_t arg )
{
    const TableFixedBlockEntry te = TableFixedEntryAt( theirs, ti );
    const TableFixedBlockEntry me = TableFixedEntryAt( mine, mi );
    const TableFixedDst & d = dst[mi];
    const uint32_t at = my_at + d.dst;
    const uint32_t aux_at = my_at + d.aux;
    if ( te.kind != me.kind )
    {
        // A KIND THAT MOVED IS REPORTED AND NEVER REINTERPRETED (§4).
        if ( c.report != NULL ) { c.report->kind_mismatch++; }
        return;
    }
    switch ( me.kind )
    {
        case 35: // the OPTIONAL wrapper: the present byte, then the payload whole
        {
            TableFixedEntry e;
            e.src = their_at; e.dst = aux_at; e.size = 1; e.guard = guard; e.op = kTableFixedCopy; e.arg = arg;
            TableFixedPush( c, e );
            TableFixedCompileEntry( c, theirs, ti + 1, their_at + 1, mine, mi + 1, dst, my_at, guard, arg );
            break;
        }
        case 13: // a nested table: match its fields
        {
            TableFixedMatchChildren( c, theirs, ti, their_at, mine, mi, dst, at, guard, arg );
            break;
        }
        case 14: // an array: the count, then min( their bound, my bound ) elements
        {
            const TableFixedBlockEntry tel = TableFixedEntryAt( theirs, ti + 1 );
            const TableFixedBlockEntry mel = TableFixedEntryAt( mine, mi + 1 );
            const uint32_t head = d.counted ? 4u : 0u;
            const uint32_t their_n = tel.size ? ( te.size - head ) / tel.size : 0u;
            const uint32_t my_n = mel.size ? ( me.size - head ) / mel.size : 0u;
            uint32_t their_base = their_at + head;
            if ( d.counted )
            {
                TableFixedEntry e;
                e.src = their_at; e.dst = aux_at; e.size = my_n; e.guard = guard; e.op = kTableFixedCount; e.arg = arg;
                TableFixedPush( c, e );
            }
            const uint32_t n = their_n < my_n ? their_n : my_n;
            for ( uint32_t i = 0; i < n; ++i )
            {
                TableFixedCompileEntry( c, theirs, ti + 1, their_base + i * tel.size,
                                        mine, mi + 1, dst, at + i * d.stride, guard, arg );
            }
            break;
        }
        case 16: // an enum-keyed array: every slot, matched by the KEY's id
        {
            const TableFixedBlockEntry tkey = TableFixedEntryAt( theirs, ti + 1 );
            const TableFixedBlockEntry mkey = TableFixedEntryAt( mine, mi + 1 );
            const int32_t tel = ti + 1 + TableFixedSubtree( theirs, ti + 1 );
            const int32_t mel = mi + 1 + TableFixedSubtree( mine, mi + 1 );
            const TableFixedBlockEntry tee = TableFixedEntryAt( theirs, tel );
            for ( uint32_t k = 0; k < mkey.children; ++k )
            {
                const uint64_t key_id = TableFixedEntryAt( mine, mi + 2 + k ).id;
                for ( uint32_t j = 0; j < tkey.children; ++j )
                {
                    if ( TableFixedEntryAt( theirs, ti + 2 + j ).id != key_id ) { continue; }
                    TableFixedCompileEntry( c, theirs, tel, their_at + j * tee.size,
                                            mine, mel, dst, at + k * d.stride, guard, arg );
                    break;
                }
            }
            break;
        }
        case 15: // a union: the tag, remapped, then each arm matched by id
        {
            const uint32_t their_tag = TableFixedTagBytes( theirs, ti );
            const uint32_t my_tag = TableFixedTagBytes( mine, mi );
            int32_t my_arm = mi + 1;
            for ( uint32_t k = 0; k < me.children; ++k )
            {
                const TableFixedBlockEntry ma = TableFixedEntryAt( mine, my_arm );
                int32_t their_arm = ti + 1;
                for ( uint32_t j = 0; j < te.children; ++j )
                {
                    const TableFixedBlockEntry ta = TableFixedEntryAt( theirs, their_arm );
                    if ( ta.id == ma.id )
                    {
                        // MY tag value, written under THEIR tag's guard
                        TableFixedEntry tag;
                        tag.src = their_at; tag.dst = aux_at; tag.size = my_tag;
                        tag.aux = k + 1; tag.guard = their_at; tag.op = kTableFixedConst; tag.arg = (uint8_t) ( j + 1 );
                        TableFixedPush( c, tag );
                        TableFixedCompileEntry( c, theirs, their_arm, their_at + their_tag,
                                                mine, my_arm, dst, at, their_at, (uint8_t) ( j + 1 ) );
                        break;
                    }
                    their_arm += TableFixedSubtree( theirs, their_arm );
                }
                my_arm += TableFixedSubtree( mine, my_arm );
            }
            break;
        }
        case 30: // an enum: the ordinal is the block's position, so it remaps
        {
            uint16_t map[256];
            const uint32_t n = te.children < 255u ? te.children : 255u;
            for ( uint32_t j = 0; j < n; ++j )
            {
                const uint64_t vid = TableFixedEntryAt( theirs, ti + 1 + (int32_t) j ).id;
                uint16_t landed = 0;
                for ( uint32_t k = 0; k < me.children; ++k )
                {
                    if ( TableFixedEntryAt( mine, mi + 1 + (int32_t) k ).id == vid ) { landed = (uint16_t) ( k + 1 ); break; }
                }
                map[j] = landed;
            }
            TableFixedEntry e;
            e.src = their_at; e.dst = at; e.size = te.size; e.guard = guard; e.op = kTableFixedOrdinal;
            e.arg = arg; e.dstsize = (uint8_t) me.size;
            e.aux = TableFixedLayTable( c, map, (int32_t) n );
            TableFixedPush( c, e );
            break;
        }
        case 12: case 33: // text
        {
            const uint32_t units = ( me.size - 4u ) < ( te.size - 4u ) ? ( me.size - 4u ) : ( te.size - 4u );
            TableFixedEntry e;
            e.src = their_at; e.dst = at; e.size = units; e.aux = aux_at; e.guard = guard;
            e.op = kTableFixedText; e.arg = d.arg;
            TableFixedPush( c, e );
            break;
        }
        default:
        {
            TableFixedEntry e;
            e.src = their_at; e.dst = at; e.guard = guard; e.arg = arg;
            if ( te.size == me.size )
            {
                e.size = me.size; e.op = kTableFixedCopy;
                TableFixedPush( c, e );
            }
            else if ( te.size < me.size && me.size <= 8 )
            {
                e.size = te.size; e.dstsize = (uint8_t) me.size; e.op = kTableFixedWiden;
                TableFixedPush( c, e );
            }
            else if ( c.report != NULL )
            {
                c.report->kind_mismatch++;
            }
            break;
        }
    }
}

inline int32_t TableFixedCompile( const TableFixedBlockView & theirs,
                                  const uint8_t * my_block, int32_t my_block_bytes,
                                  const TableFixedDst * dst,
                                  TableFixedEntry * plan, int32_t plan_capacity, TableReport * report )
{
    TableFixedBlockView mine;
    if ( plan == NULL || plan_capacity <= 0 ) { return -1; }
    if ( !TableFixedParseBlock( my_block, my_block_bytes, mine ) ) { return -1; }
    TableFixedCompiler c;
    c.plan = plan;
    c.capacity = plan_capacity;
    c.report = report;
    TableFixedMatchChildren( c, theirs, 0, 0, mine, 0, dst, 0, kTableFixedNoGuard, 0 );
    if ( c.overflow ) { return -1; }
    // COALESCE, exactly as the identity plan is coalesced
    int32_t out = 0;
    for ( int32_t i = 0; i < c.count; ++i )
    {
        if ( out > 0 && plan[out-1].op == kTableFixedCopy && plan[i].op == kTableFixedCopy &&
             plan[out-1].guard == plan[i].guard && plan[out-1].arg == plan[i].arg &&
             plan[out-1].src + plan[out-1].size == plan[i].src &&
             plan[out-1].dst + plan[out-1].size == plan[i].dst )
        {
            plan[out-1].size += plan[i].size;
            continue;
        }
        plan[out++] = plan[i];
    }
    return out;
}
`
