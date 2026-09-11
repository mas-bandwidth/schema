package cpptable

// tableFixedRuntime is the FIXED FORM's package-scoped runtime
// (docs/SPEC-TABLES.md §3.4): the plan entry, the constexpr plan builder, the
// ONE read loop, the LAYOUT reader and the plan compiler. It rides inside the
// same package guard as the rest of the primitives, so whichever <Base>Table.h
// a translation unit includes first defines it for the whole unit.
//
// EVERYTHING HERE IS ALLOCATION-FREE, which is this library's rule everywhere
// else and has no exception on this form: the plan's storage is the caller's,
// declared by capacity, and a layout whose plan does not fit is a refusal by
// name.
const tableFixedRuntime = `
// ---------------------------------------------------------------------------
// THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4)
// ---------------------------------------------------------------------------

constexpr uint8_t kTableFixedForm = 3;

// THE HEADER, ONE RULE FOR ALL FIVE FORMS (docs/SPEC-TABLES.md §3, "THE FIRST
// BYTE"): the FORM BYTE at offset 0, seven RESERVED ZERO bytes, the form's own
// EIGHT-BYTE HASH at offset 8, and the body at 16 — the alignment a
// memory-mapped body needs. The fixed form does not need the alignment today;
// it pads anyway, so the bytes do not move again the day the cook and the block
// form join the registry under the same header.
//
// The hash here is the LAYOUT's. Each record still carries its own eight-byte
// hash, which §3.4 has always said and which the header does not replace: the
// header names the layout ONCE for the file, and a record names the layout it
// was stamped by.
constexpr int64_t kTableFixedHeaderBytes = 16;
constexpr int64_t kTableFixedHashAt     = 8;

// THE OPS ARE THE WHOLE SET. The IDENTITY plan carries only the first three;
// the other four are what a plan compiled from another writer's layout adds.
// NONE OF THEM CLAMPS: a bound is held by the generated bounds pass that runs
// after the loop, the same pass for either plan (docs/SPEC-TABLES.md §3.4).
enum : uint8_t
{
    kTableFixedCopy    = 0, // move size bytes
    kTableFixedCount   = 1, // a count: clamp it to the reader's own bound
    kTableFixedText    = 2, // a length, then the units, then terminate
    kTableFixedOrdinal = 3, // a variant ordinal, remapped through the plan's own table
    kTableFixedWiden   = 4, // a narrower source into a wider destination
    kTableFixedConst   = 5, // a constant this reader's own storage takes: a remapped union tag
    kTableFixedWidenF  = 6, // f32 into f64, §4's float rung
    kTableFixedPresent = 7, // T into ?T: unguarded constant 1 into the present byte (bill §12.8)
};

// meta on a kTableFixedText entry
enum : uint8_t
{
    kTableFixedTextUtf8  = 1,
    kTableFixedTextWide  = 2,
    kTableFixedTextBytes = 3,
};

constexpr uint32_t kTableFixedNoGuard = 0xFFFFFFFFu;

// THE PLAN'S SOURCE AND DESTINATION NEVER ALIAS: one is a record in a read
// buffer and the other is the caller's own storage. Saying so is worth real
// time in the read loop, and the spelling is the compiler's.
#if defined( _MSC_VER )
#define TABLE_RESTRICT __restrict
#elif defined( __GNUC__ ) || defined( __clang__ )
#define TABLE_RESTRICT __restrict__
#else
#define TABLE_RESTRICT
#endif

// THE READ LOOP'S BODY IS ALWAYS INLINE. It is written once and used from both
// halves of the loop below, and a call there is the whole cost of the form.
#if defined( _MSC_VER )
#define TABLE_FIXED_INLINE __forceinline
#elif defined( __GNUC__ ) || defined( __clang__ )
#define TABLE_FIXED_INLINE inline __attribute__(( always_inline ))
#else
#define TABLE_FIXED_INLINE inline
#endif

// §4'S WIDENING RUNGS, and the fixed form spends no rule of its own on them: a
// kind that GREW since the writer decodes at the writer's width and lands
// exactly, counting one widened. Coming back DOWN the ladder, or across two
// of them, is a kind that MOVED and is reported rather than reinterpreted.
inline bool TableFixedWidens( uint8_t from, uint8_t to )
{
    if ( from >= 6 && from <= 9 && to >= 6 && to <= 9 ) { return to > from; }  // u8 .. u64
    if ( from >= 2 && from <= 5 && to >= 2 && to <= 5 ) { return to > from; }  // i8 .. i64
    if ( from >= 20 && from <= 24 && to >= 20 && to <= 24 ) { return to > from; } // signed fixed(I,F): I grows, F stays
    if ( from >= 25 && from <= 29 && to >= 25 && to <= 29 ) { return to > from; } // unsigned ufixed(I,F)
    return from == 10 && to == 11;                                            // f32 -> f64
}
inline bool TableFixedSignedKind( uint8_t kind )
{
    return ( kind >= 2 && kind <= 5 ) || ( kind >= 20 && kind <= 24 ); // i8..i64, or signed fixed(I,F)
}

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
    // arg IS THE GUARD'S TAG AND NOTHING ELSE: the ordinal the tag at guard
    // must hold for this entry to run. It is FULL WIDTH, matching the tag
    // read (bill §12.7): a union past 255 arms, an enum past 255 variants,
    // the lane is not a byte. meta IS THE OP'S OWN ARGUMENT — a text
    // entry's flavour. THEY ARE TWO LANES BECAUSE THEY ARE TWO FACTS: they
    // shared one, and a string(N) under a union's arm then had to be either
    // guarded correctly or read with the right flavour and could not be both.
    uint64_t arg = 0;
    uint8_t meta = 0;
    uint8_t dstsize = 0;
    uint8_t sign = 0; // a WIDEN's source is two's complement, so it sign-extends
    // argw IS THE GUARD'S WIDTH IN BYTES. A union tag is one, two, four or
    // eight, and comparing only the FIRST of them fires arm 1 on a foreign
    // tag of 0x0101 — an ordinal no arm of this build names. Zero is read as
    // one, which is the width a tag had when this field did not exist. It
    // sits last so a ten-value aggregate still names op, arg, meta, dstsize
    // and sign in that order.
    uint8_t argw = 1;
};

// A PLAN IS PARTITIONED: every UNGUARDED entry first, then every guarded one,
// and "guarded" is where the second half starts. Entries are independent —
// each writes its own bytes and a union's arms are mutually exclusive — so the
// order is free, and what it buys is that the entries that are nearly all of
// the plan never test a guard at all. Under a per-entry guard test the whole
// read is 13% slower, measured (test/bench/fixedform_measure.cpp).
//
// THERE IS NO PLAN TYPE AND NO COMPILE-TIME PLAN BUILDER HERE, and there was
// one: a constexpr walk of the type's leaves, coalesced by generic C++ code the
// compiler ran at build time. THE SCHEMA COMPILER DOES THAT WALK NOW
// (ir/fixedform.go) and every backend lays the finished plan down as static
// data — an array, a count and the split — which is ONE answer for every port
// instead of one per language, and it is what let the C leg carry this form at
// all: C has no constexpr to run a walk with. The coalescer still exists at run
// time, in TableFixedCompile below, because a plan compiled from a STRANGER's
// layout can only be built when that layout arrives.

// ---- the little-endian moves -----------------------------------------------

inline void TableFixedPut8( uint8_t * b, uint8_t v ) { b[0] = v; }
inline void TableFixedPut16( uint8_t * b, uint16_t v ) { b[0] = (uint8_t)( v ); b[1] = (uint8_t)( v >> 8 ); }
inline void TableFixedPut32( uint8_t * b, uint32_t v ) { for ( int i = 0; i < 4; ++i ) { b[i] = (uint8_t)( v >> ( 8 * i ) ); } }
inline void TableFixedPut64( uint8_t * b, uint64_t v ) { for ( int i = 0; i < 8; ++i ) { b[i] = (uint8_t)( v >> ( 8 * i ) ); } }
inline void TableFixedPutF32( uint8_t * b, float v ) { uint32_t w; memcpy( &w, &v, 4 ); TableFixedPut32( b, w ); }
inline void TableFixedPutF64( uint8_t * b, double v ) { uint64_t w; memcpy( &w, &v, 8 ); TableFixedPut64( b, w ); }
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

// THE TAG IS COMPARED AT ITS OWN WIDTH. A two- or four-byte tag whose low
// byte happens to be 1 is not arm 1: it is an ordinal this build has no arm
// for, and reading only the first byte let 0x0101 run arm 1's entries over
// a stranger's record.
inline uint64_t TableFixedTagAt( const uint8_t * src, uint32_t guard, uint8_t argw )
{
    const uint8_t w = argw == 0 ? 1u : ( argw > 8u ? 8u : argw );
    uint64_t v = 0;
    for ( uint8_t i = 0; i < w; ++i )
    {
        v |= ( (uint64_t) src[guard + i] ) << ( 8 * i );
    }
    return v;
}

// THE HASH is fnv1a64 over the layout's bytes exactly as written (§3.4).
inline uint64_t TableFixedHashOf( const uint8_t * layout, int64_t bytes )
{
    uint64_t h = 0xcbf29ce484222325ull;
    for ( int64_t i = 0; i < bytes; ++i )
    {
        h ^= (uint64_t) layout[i];
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
//
// AND EVERY MOVE STAYS WITHIN [ run start, run end ). Overlapping moves are the
// technique; a move anchored OUTSIDE the run is not the technique, it is a
// defect — it clobbers whatever field sits in front of the destination and it
// reads past the record body.
TABLE_FIXED_INLINE void TableFixedCopyRun( uint8_t * d, const uint8_t * s, uint32_t n )
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
    // EVERY MOVE IS ANCHORED INSIDE [ s, s + n ), AND THAT IS THE INVARIANT THIS
    // BRANCH EXISTS TO STATE. A run of 17..31 bytes has no room for a sixteen,
    // a second sixteen and a thirty-two, so it takes TWO SIXTEENS that OVERLAP
    // in the middle: one at the run's start and one at end - 16, both wholly
    // within the run because n >= 16. Anchoring the tail move at n - 32
    // instead would put it 32 - n bytes IN FRONT of the run — reading and
    // writing a neighbour's bytes, and reading past the record body, which for
    // a file's last record is past the buffer.
    if ( n <= 32 )
    {
        uint64_t w[2];
        memcpy( w, s, 16 ); memcpy( d, w, 16 );
        memcpy( w, s + n - 16, 16 ); memcpy( d + n - 16, w, 16 );
        return;
    }
    if ( n <= 64 )
    {
        // n >= 33 here, so n - 32 is at least 1 and the tail move begins
        // inside the run, exactly as the two moves in front of it do.
        uint64_t w[4];
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
TABLE_FIXED_INLINE void TableFixedApply( const TableFixedEntry & p, const uint8_t * base,
                             const uint8_t * TABLE_RESTRICT src, uint8_t * TABLE_RESTRICT dst,
                             int32_t & clamped, int32_t & widened )
{
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
            if ( v < 0 ) { v = 0; clamped++; }
            else if ( v > (int32_t) p.size ) { v = (int32_t) p.size; clamped++; }
            memcpy( dst + p.dst, &v, 4 );
            break;
        }
        case kTableFixedText:
        {
            const uint32_t unit = ( p.meta == kTableFixedTextWide ) ? 2u : 1u;
            const uint32_t cap = p.size / unit;
            int32_t v = (int32_t) TableFixedGet32( src + p.src );
            if ( v < 0 ) { v = 0; clamped++; }
            else if ( (uint32_t) v > cap ) { v = (int32_t) cap; clamped++; }
            memcpy( dst + p.dst, &v, 4 );
            TableFixedCopyRun( dst + p.aux, src + p.src + 4, p.size );
            if ( p.meta != kTableFixedTextBytes )
            {
                // the used length terminates the buffer, whose storage is one
                // unit longer than the bound for exactly this. A store, not a
                // call: memset of a runtime length is a call.
                uint8_t * end = dst + p.aux + (uint32_t) v * unit;
                end[0] = 0;
                if ( unit == 2 ) { end[1] = 0; }
            }
            break;
        }
        case kTableFixedOrdinal:
        {
            // A VARIANT ORDINAL IS ITS POSITION IN THE LAYOUT, so a writer whose
            // enum gained a variant IN THE MIDDLE is remapped here and never
            // reinterpreted. The table is the plan's own, laid down by the
            // compiler above the entries.
            uint64_t raw = 0;
            memcpy( &raw, src + p.src, p.size );
            const uint16_t * table = (const uint16_t *) (const void *) ( base + p.aux );
            uint64_t v = 0;
            if ( raw != 0 )
            {
                if ( raw <= (uint64_t) table[0] ) { v = table[raw]; }
                if ( v == 0 ) { clamped++; }
            }
            memcpy( dst + p.dst, &v, p.dstsize );
            break;
        }
        case kTableFixedWiden:
        {
            uint64_t raw = 0;
            memcpy( &raw, src + p.src, p.size );
            if ( p.sign != 0 )
            {
                // TWO'S COMPLEMENT WIDENS BY ITS SIGN BIT, which is the whole
                // reason a widen is an op and not a short copy.
                const unsigned bits = p.size * 8u;
                const uint64_t top = 1ull << ( bits - 1 );
                if ( raw & top ) { raw |= ~( ( top << 1 ) - 1ull ); }
            }
            memcpy( dst + p.dst, &raw, p.dstsize );
            widened++;
            break;
        }
        case kTableFixedWidenF:
        {
            // f32 into f64, exact: a NaN's payload is data and rides on the
            // bits, since the hardware conversion would set the quiet bit
            // (§4). Same surgery as TableWidenF32, the packet form's float
            // rung. ALGORITHM §4.5: widenf is exact by construction, NaN
            // payloads included.
            const uint32_t bits = TableFixedGet32( src + p.src );
            double d;
            if ( ( bits & 0x7F800000u ) == 0x7F800000u && ( bits & 0x007FFFFFu ) != 0 )
            {
                const uint64_t sign = (uint64_t) ( bits >> 31 ) << 63;
                const uint64_t payload = (uint64_t) ( bits & 0x007FFFFFu ) << 29;
                const uint64_t nan_bits = sign | 0x7FF0000000000000ull | payload;
                memcpy( &d, &nan_bits, 8 );
            }
            else
            {
                float f;
                memcpy( &f, &bits, 4 );
                d = (double) f;
            }
            memcpy( dst + p.dst, &d, 8 );
            widened++;
            break;
        }
        case kTableFixedConst:
        {
            memcpy( dst + p.dst, &p.aux, p.size );
            // An unguarded None const with dstsize = the WRITER's arm count:
            // a tag past that set lands None (already written) and COUNTS.
            if ( p.aux == 0 && p.dstsize != 0 && p.guard == kTableFixedNoGuard )
            {
                uint64_t raw = 0;
                memcpy( &raw, src + p.src, p.size );
                if ( raw > (uint64_t) p.dstsize ) { clamped++; }
            }
            break;
        }
        case kTableFixedPresent:
        {
            dst[p.dst] = 1; // T into ?T: every old value lands present (bill §12.8)
            break;
        }
        default: break;
    }
}

// A READ IS A PREFILL AND THIS LOOP, AND NOTHING ELSE. Which plan it is handed
// is the only thing that differs between reading this build's own record and
// reading anybody else's.
inline void TableFixedRun( const TableFixedEntry * plan, int32_t count, int32_t guarded,
                           const uint8_t * TABLE_RESTRICT src, uint8_t * TABLE_RESTRICT dst,
                           TableReport * report )
{
    // THE COUNTERS ARE LOCAL AND WRITTEN BACK ONCE. A report the loop wrote
    // through is a pointer the compiler must assume aliases the destination,
    // and every store to a field would then reload it.
    int32_t clamped = 0;
    int32_t widened = 0;
    for ( int32_t i = 0; i < guarded; ++i )
    {
        TableFixedApply( plan[i], (const uint8_t *) plan, src, dst, clamped, widened );
    }
    for ( int32_t i = guarded; i < count; ++i )
    {
        const TableFixedEntry & p = plan[i];
        if ( TableFixedTagAt( src, p.guard, p.argw ) != p.arg ) { continue; }
        TableFixedApply( p, (const uint8_t *) plan, src, dst, clamped, widened );
    }
    report->clamped += clamped;
    report->widened += widened;
}

// ---- THE PREFILL -----------------------------------------------------------
//
// §3.4'S PREFILL ANSWERS ONE QUESTION — what does a field the record does not
// carry hold? — and the owner's rule is that it answers it for EXACTLY THE
// BYTES THE PLAN DOES NOT LAND, and no others. Prefilling a byte the plan is
// about to overwrite is a write thrown away, and a form whose write makes two
// passes over a body should not have its read make three.
//
// So the prefill is A LIST OF RANGES rather than a whole-value reset, and it is
// computed where the plan is: the type's own VALUE BYTES — the identity plan's
// destinations, laid down sorted and merged by the schema compiler as
// <Name>FixedCover — MINUS every byte this plan lands.
//
// THE IDENTITY PLAN'S LIST IS EMPTY, and that is the rule's LIMIT CASE and not
// an exception to it: its destinations ARE the value bytes, so there is nothing
// left to subtract. Which is why nothing in the read loop asks whether the plan
// it is running is the identity one — it runs the list it was handed, and that
// list is empty.

struct TableFixedFill
{
    uint32_t dst = 0;
    uint32_t size = 0;
};

// THE PREFILL ITSELF: the declared defaults, into the ranges named and nowhere
// else. The default image is the caller's own storage reset once per read, not
// per record.
inline void TableFixedFillRun( const TableFixedFill * fill, int32_t count,
                               const uint8_t * TABLE_RESTRICT defaults, uint8_t * TABLE_RESTRICT dst )
{
    for ( int32_t i = 0; i < count; ++i )
    {
        TableFixedCopyRun( dst + fill[i].dst, defaults + fill[i].dst, fill[i].size );
    }
}

// TableFixedEntryLands is the bytes of the READER'S OWN STORAGE one entry
// writes — TWO runs, because a text entry lands a length and a buffer and every
// other op lands one run and leaves the second empty.
//
// A GUARDED ENTRY COUNTS AS LANDING ITS BYTES. It is a union arm, and an arm's
// storage is the arm's: the tag says which one is live and nothing reads the
// others. What must never be conditional is the TAG, and the compiler below
// lays an unguarded one down for exactly this reason.
inline void TableFixedEntryLands( const TableFixedEntry & e, uint32_t * lands )
{
    lands[0] = e.dst;
    lands[1] = e.dst;
    lands[2] = 0;
    lands[3] = 0;
    switch ( e.op )
    {
        case kTableFixedCount: lands[1] = e.dst + 4u; break;
        case kTableFixedText:
        {
            const uint32_t unit = ( e.meta == kTableFixedTextWide ) ? 2u : 1u;
            lands[1] = e.dst + 4u;
            lands[2] = e.aux;
            lands[3] = e.aux + e.size + ( e.meta == kTableFixedTextBytes ? 0u : unit );
            break;
        }
        case kTableFixedWidenF: lands[1] = e.dst + 8u; break;
        case kTableFixedWiden:
        case kTableFixedOrdinal: lands[1] = e.dst + e.dstsize; break;
        default: lands[1] = e.dst + e.size; break; // copy, const
    }
}

// TableFixedFills is the subtraction: the cover MINUS everything the plan
// lands, as a list of ranges, answered into the out array or merely counted
// when it is NULL. It walks the cover once and, at each position, asks the plan what
// run starts there and where the next one starts — so it costs the plan's
// length per RUN of the answer, once per file and never per record. The plan's
// length is the caller's declared plan capacity, which is what bounds it.
inline int32_t TableFixedFills( const TableFixedEntry * plan, int32_t count,
                                const TableFixedFill * cover, int32_t cover_count,
                                TableFixedFill * out )
{
    int32_t n = 0;
    for ( int32_t r = 0; r < cover_count; ++r )
    {
        const uint32_t hi = cover[r].dst + cover[r].size;
        uint32_t pos = cover[r].dst;
        while ( pos < hi )
        {
            uint32_t end = pos;  // the end of the landed run that covers pos
            uint32_t next = hi;  // where the next landed run starts, past pos
            for ( int32_t i = 0; i < count; ++i )
            {
                uint32_t lands[4];
                TableFixedEntryLands( plan[i], lands );
                for ( int32_t q = 0; q < 4; q += 2 )
                {
                    const uint32_t lo = lands[q];
                    const uint32_t top = lands[q + 1];
                    if ( top <= lo ) { continue; }
                    if ( lo <= pos && pos < top ) { if ( top > end ) { end = top; } }
                    else if ( lo > pos && lo < next ) { next = lo; }
                }
            }
            if ( end > pos ) { pos = end; continue; }
            if ( next > hi ) { next = hi; }
            if ( out != NULL ) { out[n].dst = pos; out[n].size = next - pos; }
            n++;
            pos = next;
        }
    }
    return n;
}

// ---- THE LAYOUT ------------------------------------------------------------
//
// An entry is SEVENTEEN BYTES and the layout is a COUNT and a run of them.
// THERE IS NO VERSION BYTE INSIDE IT (docs/SPEC-TABLES.md §3.4): the FORM BYTE
// versions everything behind it, the layout's own format included, so a layout
// format change is a NEW FORM BYTE and never a wider entry.

constexpr int32_t kTableFixedEntryBytes = 17;
constexpr int32_t kTableFixedLayoutHeaderBytes = 4; // the u32 entry count, and nothing else

// §3.4'S RECORD BOUND, and a reader holds an untrusted peer's layout to it for
// the same reason the compiler holds a declaration to it: a record past it is
// one this build will not decode.
constexpr uint32_t kTableFixedRecordMaxBytes = 65536u;

// A BOUND ON THE WALK, not on the wire: the validation below is recursive, so
// a hostile layout of three thousand entries each claiming one child would
// otherwise spend a reader's stack before any rule fired.
constexpr int32_t kTableFixedMaxDepth = 64;

struct TableFixedLayoutEntry
{
    uint64_t id = 0;
    uint32_t size = 0;
    uint32_t children = 0;
    uint8_t kind = 0;
};

struct TableFixedLayoutView
{
    const uint8_t * bytes = NULL;
    int32_t count = 0;
};

// AN INDEX PAST THE ENTRIES IS ANSWERED, NOT READ. Every index into a layout
// is arithmetic over child counts a STRANGER wrote, so the one place that can
// hold the whole walk inside the buffer is the one place that touches it. An
// out-of-range index answers kind 0xFF, which is a kind no declaration has and
// nothing matches, so the walk that asked for it finds nothing and moves on.
inline TableFixedLayoutEntry TableFixedEntryAt( const TableFixedLayoutView & b, int32_t i )
{
    TableFixedLayoutEntry out;
    if ( b.bytes == NULL || i < 0 || i >= b.count ) { out.kind = 0xFFu; return out; }
    const uint8_t * e = b.bytes + kTableFixedLayoutHeaderBytes + (int64_t) i * kTableFixedEntryBytes;
    out.id = TableFixedGet64( e );
    out.kind = e[8];
    out.size = TableFixedGet32( e + 9 );
    out.children = TableFixedGet32( e + 13 );
    return out;
}

// TableFixedSubtree is how many entries the subtree rooted at i occupies, so a
// walk steps over a child it does not want without knowing what is in it.
inline int32_t TableFixedSubtree( const TableFixedLayoutView & b, int32_t i )
{
    // ITERATIVE ON PURPOSE. A layout is a stranger's bytes, so a chain of
    // single-child entries is a stack depth the wire gets to choose; this walk
    // gives it none.
    if ( i < 0 || i >= b.count ) { return 1; }
    int64_t pending = 1;
    int32_t n = 0;
    int32_t at = i;
    while ( pending > 0 && at < b.count )
    {
        pending--;
        n++;
        pending += (int64_t) TableFixedEntryAt( b, at ).children;
        at++;
        if ( pending > (int64_t) b.count ) { break; }
    }
    return n;
}

inline uint32_t TableFixedUnionArmBytes( const TableFixedLayoutView & b, int32_t i )
{
    const TableFixedLayoutEntry e = TableFixedEntryAt( b, i );
    uint32_t widest = 0;
    int32_t at = i + 1;
    for ( uint32_t k = 0; k < e.children && at < b.count; ++k )
    {
        const TableFixedLayoutEntry a = TableFixedEntryAt( b, at );
        if ( a.size > widest ) { widest = a.size; }
        at += TableFixedSubtree( b, at );
    }
    return widest;
}

inline uint32_t TableFixedTagBytes( const TableFixedLayoutView & b, int32_t i )
{
    return TableFixedEntryAt( b, i ).size - TableFixedUnionArmBytes( b, i );
}

// ---- THE LAYOUT'S OWN VALIDATION, RULE BY NAMED RULE -----------------------
//
// A LAYOUT ARRIVES FROM AN UNTRUSTED PEER and it is the one structure a reader
// must parse before it knows anything at all, so every rule below runs BEFORE
// a single record byte is touched and each refuses under its OWN NAME rather
// than under one word for all of them (docs/SPEC-TABLES.md §3.4).
//
// A KIND THIS BUILD DOES NOT KNOW IS A REFUSAL, and that is a ruling rather
// than an oversight. A FIXED FORM HAS A CLOSED KIND SET: the form byte versions
// everything behind it, kinds included, so a layout carrying a kind outside the
// set is not a newer layout of THIS form — it is a layout of a form whose byte
// this reader never saw, and there is no version of "step over it" that is
// honest about that. layout_kind_unknown says exactly what happened, and the
// peer is told to speak this form or a form this reader carries.
//
// NOR IS THERE A CYCLE RULE, because a pre-order walk cannot express a cycle:
// an entry's children ARE THE ENTRIES THAT FOLLOW IT, so a child's index is
// always higher than its parent's, the layout is finite, and there is no back
// reference for a cycle to be made of. The tree-closure rule is what bounds
// the walk, and the depth rule is what bounds the reader's stack.

struct TableFixedCheck
{
    const TableFixedLayoutView * layout = NULL;
    TableMessageReason why = layout_malformed;
    bool bad = false;
};

inline void TableFixedFail( TableFixedCheck & c, TableMessageReason why )
{
    if ( !c.bad ) { c.bad = true; c.why = why; }
}

// A LEAF KIND'S ADMITTED SIZES. Kind and size fix each other on this wire with
// ONE exception, and it is stated here rather than left to be found: a
// bits(N) field rides at its DECLARED STORAGE WIDTH — four bytes for N <= 32 and
// eight above (§3.4) — under the unsigned integer kind its BIT COUNT picks
// (§3), so kinds 6 and 7 admit four as well as their own width.
inline bool TableFixedLeafSize( uint8_t kind, uint32_t size, bool & is_leaf )
{
    is_leaf = true;
    switch ( kind )
    {
        case 1:  return size == 1u;                  // bool
        case 2:  return size == 1u;                  // i8
        case 3:  return size == 2u;                  // i16
        case 4:  return size == 4u;                  // i32
        case 5:  return size == 8u;                  // i64
        case 6:  return size == 1u || size == 4u;    // u8,  and bits( 1 .. 8 )
        case 7:  return size == 2u || size == 4u;    // u16, and bits( 9 .. 16 )
        case 8:  return size == 4u;                  // u32, and bits( 17 .. 32 )
        case 9:  return size == 8u;                  // u64, flags, and bits( 33 .. 64 )
        case 10: return size == 4u;                  // f32
        case 11: return size == 8u;                  // f64
        case 17: return size == 4u;                  // a pointer index, which no fixed table carries
        case 18: case 19: return size == 16u;        // i128, u128
        case 20: case 25: return size == 1u;         // fixed8, ufixed8
        case 21: case 26: return size == 2u;
        case 22: case 27: return size == 4u;
        case 23: case 28: return size == 8u;
        case 24: case 29: return size == 16u;
        case 32: return size == 0u;                  // a variant, and an arm that holds nothing
    }
    is_leaf = false;
    return true;
}

inline bool TableFixedOrdinalWidth( uint32_t n ) { return n == 1u || n == 2u || n == 4u || n == 8u; }

// THE CLOSED KIND SET (docs/SPEC-TABLES.md §3, §3.4): §3's own kinds 1..30,
// the no-payload variant 32, wstring 33, and the ONE kind this form's layout
// adds, the optional wrapper 35. 31 is §3's BODY framing escape and 34 is
// reserved: neither is a kind a declaration spells, so neither is ever an
// entry's kind. A KIND OUTSIDE THIS SET IS A REFUSAL, not a leaf to step over
// — see the note above TableFixedCheck.
inline bool TableFixedKnownKind( uint8_t kind )
{
    if ( kind >= 1u && kind <= 30u ) { return true; }
    return kind == 32u || kind == 33u || kind == 35u;
}

// TableFixedCheckEntry validates the subtree rooted at i and answers how many
// entries it occupies, which is the same arithmetic TableFixedSubtree does and
// is why that walk is safe to run afterwards and only afterwards.
inline int32_t TableFixedCheckEntry( TableFixedCheck & c, int32_t i, int32_t depth )
{
    if ( c.bad ) { return 0; }
    if ( i < 0 || i >= c.layout->count ) { TableFixedFail( c, layout_tree_unclosed ); return 0; }
    if ( depth > kTableFixedMaxDepth ) { TableFixedFail( c, layout_too_deep ); return 0; }
    const TableFixedLayoutEntry e = TableFixedEntryAt( *c.layout, i );
    // THE CLOSED KIND SET, FIRST: a kind outside it is a layout of another form
    // and is refused whole, never walked and never stepped over.
    if ( !TableFixedKnownKind( e.kind ) ) { TableFixedFail( c, layout_kind_unknown ); return 0; }
    if ( e.size > kTableFixedRecordMaxBytes ) { TableFixedFail( c, layout_record_too_large ); return 0; }

    // THE CHILDREN FIRST, in the pre-order the layout is written in.
    int32_t at = i + 1;
    uint64_t sum = 0;
    uint32_t widest = 0;
    uint32_t first_size = 0, second_size = 0;
    uint8_t first_kind = 0;
    uint32_t first_children = 0;
    bool kids_are_variants = true;
    for ( uint32_t k = 0; k < e.children; ++k )
    {
        const int32_t child = at;
        const int32_t used = TableFixedCheckEntry( c, child, depth + 1 );
        if ( c.bad ) { return 0; }
        const TableFixedLayoutEntry ce = TableFixedEntryAt( *c.layout, child );
        if ( k == 0 ) { first_size = ce.size; first_kind = ce.kind; first_children = ce.children; }
        if ( k == 1 ) { second_size = ce.size; }
        if ( ce.kind != 32 ) { kids_are_variants = false; }
        sum += ce.size;
        if ( ce.size > widest ) { widest = ce.size; }
        if ( sum > kTableFixedRecordMaxBytes ) { TableFixedFail( c, layout_record_too_large ); return 0; }
        at += used;
    }

    bool is_leaf = false;
    if ( !TableFixedLeafSize( e.kind, e.size, is_leaf ) ) { TableFixedFail( c, layout_size_mismatch ); return 0; }
    if ( is_leaf )
    {
        if ( e.children != 0u ) { TableFixedFail( c, layout_kind_invalid ); return 0; }
        return at - i;
    }
    switch ( e.kind )
    {
        case 13: // a TABLE: its size is the SUM of its fields'
        {
            if ( (uint64_t) e.size != sum ) { TableFixedFail( c, layout_size_mismatch ); return 0; }
            break;
        }
        case 35: // the OPTIONAL WRAPPER: one child, one present byte in front of it
        {
            if ( e.children != 1u ) { TableFixedFail( c, layout_kind_invalid ); return 0; }
            if ( (uint64_t) e.size != sum + 1u ) { TableFixedFail( c, layout_size_mismatch ); return 0; }
            break;
        }
        case 14: // an ARRAY: a whole number of elements, behind a count or not
        {
            if ( e.children != 1u ) { TableFixedFail( c, layout_kind_invalid ); return 0; }
            if ( first_size == 0u ) { TableFixedFail( c, layout_size_mismatch ); return 0; }
            const bool bare = ( e.size % first_size ) == 0u;
            const bool counted = e.size >= 4u && ( ( e.size - 4u ) % first_size ) == 0u;
            if ( !bare && !counted ) { TableFixedFail( c, layout_size_mismatch ); return 0; }
            break;
        }
        case 16: // an ENUM-KEYED array: the KEY ENUM then the ELEMENT, every slot written
        {
            if ( e.children != 2u ) { TableFixedFail( c, layout_kind_invalid ); return 0; }
            if ( first_kind != 30 ) { TableFixedFail( c, layout_kind_invalid ); return 0; }
            if ( second_size == 0u || ( e.size % second_size ) != 0u ) { TableFixedFail( c, layout_size_mismatch ); return 0; }
            // AT LEAST one slot per variant. Not exactly one: an enum widened
            // by an explicit max has more slots than it has names, and the
            // layout carries only the names.
            if ( e.size / second_size < first_children ) { TableFixedFail( c, layout_size_mismatch ); return 0; }
            break;
        }
        case 15: // a UNION: the TAG at its own width, then the WIDEST ARM
        {
            if ( e.children == 0u ) { TableFixedFail( c, layout_kind_invalid ); return 0; }
            if ( e.size <= widest ) { TableFixedFail( c, layout_size_mismatch ); return 0; }
            if ( !TableFixedOrdinalWidth( e.size - widest ) ) { TableFixedFail( c, layout_size_mismatch ); return 0; }
            break;
        }
        case 30: // an ENUM: the ordinal's storage width, and its children are VARIANTS
        {
            if ( !TableFixedOrdinalWidth( e.size ) ) { TableFixedFail( c, layout_size_mismatch ); return 0; }
            if ( e.children != 0u && !kids_are_variants ) { TableFixedFail( c, layout_kind_invalid ); return 0; }
            break;
        }
        case 12: // string(N): the length, then N bytes
        {
            if ( e.children != 0u ) { TableFixedFail( c, layout_kind_invalid ); return 0; }
            if ( e.size < 4u ) { TableFixedFail( c, layout_size_mismatch ); return 0; }
            break;
        }
        case 33: // wstring(N): the length in CODE UNITS, then 2N bytes
        {
            if ( e.children != 0u ) { TableFixedFail( c, layout_kind_invalid ); return 0; }
            if ( e.size < 4u || ( ( e.size - 4u ) % 2u ) != 0u ) { TableFixedFail( c, layout_size_mismatch ); return 0; }
            break;
        }
        // Every kind of the closed set is either a leaf above or a case here,
        // so this is unreachable — and it refuses rather than admits, because
        // a kind that reached it is a kind the two lists disagree about.
        default: TableFixedFail( c, layout_kind_unknown ); return 0;
    }
    return at - i;
}

// TableFixedParseLayout is the WHOLE of what a reader trusts a layout on. It
// answers false and a REASON BY NAME, and the caller reports that reason.
inline bool TableFixedParseLayout( const uint8_t * bytes, int64_t length, TableFixedLayoutView & out, TableMessageReason & why )
{
    out.bytes = NULL;
    out.count = 0;
    if ( bytes == NULL || length < kTableFixedLayoutHeaderBytes ) { why = layout_malformed; return false; }
    // 1. THE ENTRY COUNT FITS THE LAYOUT LENGTH EXACTLY
    const uint32_t count = TableFixedGet32( bytes );
    if ( count == 0u || (int64_t) count * kTableFixedEntryBytes + kTableFixedLayoutHeaderBytes != length )
    {
        why = layout_count_mismatch;
        return false;
    }
    TableFixedLayoutView view;
    view.bytes = bytes;
    view.count = (int32_t) count;
    // 2. THE ROOT IS A TABLE, and its size is the record's body
    const TableFixedLayoutEntry root = TableFixedEntryAt( view, 0 );
    if ( root.kind != 13 ) { why = layout_kind_invalid; return false; }
    if ( root.size == 0u || root.size > kTableFixedRecordMaxBytes ) { why = layout_record_too_large; return false; }
    // 3. EVERY KIND IN THE CLOSED SET AND USED AS ITS DEFINITION ALLOWS, EVERY
    //    SIZE THE ONE ITS CHILDREN ACCOUNT FOR, NOTHING PAST THE WALK'S BOUND
    TableFixedCheck c;
    c.layout = &view;
    const int32_t used = TableFixedCheckEntry( c, 0, 0 );
    if ( c.bad ) { why = c.why; return false; }
    // 4. THE PRE-ORDER WALK CONSUMES EXACTLY THE ENTRIES: the tree closes and
    //    the layout has nothing left over
    if ( used != view.count ) { why = layout_tree_unclosed; return false; }
    out = view;
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

// TableFixedDst is MY side of the walk: one row per entry of my own layout,
// carrying the storage facts a layout entry cannot.
struct TableFixedDst
{
    uint32_t dst = 0;    // this entry's storage offset inside its parent
    uint32_t stride = 0; // an array entry's storage stride
    uint32_t aux = 0;    // a text field's buffer offset
    uint8_t counted = 0; // an array that carries a live count
    uint8_t meta = 0;    // a text field's flavour, which is the TEXT OP's own argument
};

struct TableFixedCompiler
{
    TableFixedEntry * plan = NULL;
    int32_t capacity = 0;
    int32_t count = 0;
    int32_t pool = 0; // bytes of remap table laid down from the TOP, downward
    uint32_t record = 0; // the WRITER's declared body size: every entry is bounded by it
    int32_t depth = 0;   // the nesting this walk is inside, capped below
    bool want_guarded = false; // THE WALK RUNS TWICE, unguarded first (§3.4)
    bool overflow = false;
    bool hostile = false;
    uint8_t argw = 1; // the CURRENT union's tag width, stamped onto every guarded push
    TableReport * report = NULL;
};

inline void TableFixedPush( TableFixedCompiler & c, TableFixedEntry e )
{
    // THE WALK RUNS TWICE and each pass keeps its own half, which is how the
    // plan comes out partitioned without a second array to partition it in.
    if ( ( e.guard != kTableFixedNoGuard ) != c.want_guarded ) { return; }
    // EVERY ENTRY IS BOUNDED BY THE WRITER'S OWN RECORD, and this is the read
    // side's whole defence: the plan's source offsets are arithmetic over sizes
    // a STRANGER wrote, so a layout whose child sizes do not sum to its parent's
    // could otherwise name a byte past the record. A layout that does is refused
    // WHOLE and never partly compiled (docs/SPEC-TABLES.md §3.4).
    {
        if ( e.guard != kTableFixedNoGuard ) { e.argw = c.argw ? c.argw : 1u; }
        const uint8_t gw = e.argw == 0 ? 1u : e.argw;
        const bool guard_out = e.guard != kTableFixedNoGuard &&
            ( e.guard >= c.record || (uint64_t) e.guard + gw > (uint64_t) c.record );
        const uint64_t reach = (uint64_t) e.src + (uint64_t) e.size + ( e.op == kTableFixedText ? 4ull : 0ull );
        if ( reach > (uint64_t) c.record || guard_out )
        {
            c.hostile = true;
            return;
        }
    }
    const int32_t room = c.capacity - ( c.pool + (int32_t) sizeof( TableFixedEntry ) - 1 ) / (int32_t) sizeof( TableFixedEntry );
    if ( c.count >= room ) { c.overflow = true; return; }
    c.plan[c.count++] = e;
}

// TableFixedLayTable lays a remap table above the entries and answers its byte
// offset from the plan's base. Tables grow DOWNWARD from the top, so the
// coalescing pass, which only moves entries down, never moves one.
inline uint32_t TableFixedLayTable( TableFixedCompiler & c, const uint16_t * values, int32_t n )
{
    if ( n < 0 || n > 65535 ) { c.overflow = true; return 0; }
    const int32_t bytes = ( n + 1 ) * (int32_t) sizeof( uint16_t );
    const int32_t total = (int32_t) sizeof( TableFixedEntry ) * c.capacity;
    c.pool += bytes;
    if ( total - c.pool < c.count * (int32_t) sizeof( TableFixedEntry ) ) { c.overflow = true; return 0; }
    const uint32_t at = (uint32_t) ( total - c.pool );
    uint16_t * dst = (uint16_t *) (void *) ( (uint8_t *) c.plan + at );
    dst[0] = (uint16_t) n;
    if ( values != NULL )
    {
        for ( int32_t i = 0; i < n; ++i ) { dst[1 + i] = values[i]; }
    }
    return at;
}

// TableFixedLayFills reserves the prefill's ranges in the SAME POOL the remap
// tables come out of, above the entries and growing downward, and answers the
// byte offset of the array from the plan's base. The pool is the caller's plan
// storage: this codec allocates nothing, here as everywhere, and a plan whose
// pool does not fit is a refusal by name.
inline uint32_t TableFixedLayFills( TableFixedCompiler & c, int32_t n )
{
    const int32_t total = (int32_t) sizeof( TableFixedEntry ) * c.capacity;
    // ROUNDED SO THE ARRAY IS ALIGNED: a remap table is laid in units of two
    // bytes and this array's members are four wide.
    c.pool = ( c.pool + n * (int32_t) sizeof( TableFixedFill ) + 3 ) & ~3;
    if ( total - c.pool < c.count * (int32_t) sizeof( TableFixedEntry ) ) { c.overflow = true; return 0; }
    return (uint32_t) ( total - c.pool );
}


inline void TableFixedCompileEntry( TableFixedCompiler & c,
                                    const TableFixedLayoutView & theirs, int32_t ti, uint32_t their_at,
                                    const TableFixedLayoutView & mine, int32_t mi, const TableFixedDst * dst, uint32_t my_at,
                                    uint32_t guard, uint64_t arg );

// TableFixedMatchChildren walks a TABLE's children on both sides.
inline void TableFixedMatchChildren( TableFixedCompiler & c,
                                     const TableFixedLayoutView & theirs, int32_t ti, uint32_t their_at,
                                     const TableFixedLayoutView & mine, int32_t mi, const TableFixedDst * dst, uint32_t my_at,
                                     uint32_t guard, uint64_t arg )
{
    const TableFixedLayoutEntry te = TableFixedEntryAt( theirs, ti );
    const TableFixedLayoutEntry me = TableFixedEntryAt( mine, mi );
    int32_t my_child = mi + 1;
    for ( uint32_t k = 0; k < me.children && my_child < mine.count; ++k )
    {
        const TableFixedLayoutEntry mc = TableFixedEntryAt( mine, my_child );
        int32_t their_child = ti + 1;
        uint32_t their_off = their_at;
        for ( uint32_t j = 0; j < te.children && their_child < theirs.count; ++j )
        {
            const TableFixedLayoutEntry tc = TableFixedEntryAt( theirs, their_child );
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
    for ( uint32_t j = 0; j < te.children && tc_at < theirs.count; ++j )
    {
        const TableFixedLayoutEntry tc = TableFixedEntryAt( theirs, tc_at );
        bool named = false;
        int32_t mc_at = mi + 1;
        for ( uint32_t k = 0; k < me.children && mc_at < mine.count; ++k )
        {
            if ( TableFixedEntryAt( mine, mc_at ).id == tc.id ) { named = true; break; }
            mc_at += TableFixedSubtree( mine, mc_at );
        }
        if ( !named && c.report != NULL ) { c.report->unknown++; }
        tc_at += TableFixedSubtree( theirs, tc_at );
    }
}

inline void TableFixedCompileEntry( TableFixedCompiler & c,
                                    const TableFixedLayoutView & theirs, int32_t ti, uint32_t their_at,
                                    const TableFixedLayoutView & mine, int32_t mi, const TableFixedDst * dst, uint32_t my_at,
                                    uint32_t guard, uint64_t arg )
{
    const TableFixedLayoutEntry te = TableFixedEntryAt( theirs, ti );
    const TableFixedLayoutEntry me = TableFixedEntryAt( mine, mi );
    const TableFixedDst & d = dst[mi];
    const uint32_t at = my_at + d.dst;
    // THE NESTING A STRANGER'S LAYOUT CAN ASK FOR IS CAPPED. The language's own
    // closure is nowhere near this deep, and a wire does not get to pick a
    // recursion depth.
    if ( c.depth > 64 ) { c.hostile = true; return; }
    struct TableFixedDepth
    {
        TableFixedCompiler & owner;
        explicit TableFixedDepth( TableFixedCompiler & o ) : owner( o ) { ++owner.depth; }
        ~TableFixedDepth() { --owner.depth; }
    } depth_guard( c );
    (void) depth_guard;
    const uint32_t aux_at = my_at + d.aux;
    // T into ?T: an unguarded present op, then the payload under the wrapper
    // (bill §12.8, algorithm §5.2). The writer's kind is the payload's; the
    // reader's is the optional wrapper.
    if ( me.kind == 35 && te.kind != 35 )
    {
        TableFixedEntry e;
        e.dst = aux_at; e.size = 1; e.guard = guard; e.arg = arg; e.op = kTableFixedPresent;
        TableFixedPush( c, e );
        TableFixedCompileEntry( c, theirs, ti, their_at, mine, mi + 1, dst, my_at, guard, arg );
        return;
    }
    if ( te.kind != me.kind )
    {
        if ( TableFixedWidens( te.kind, me.kind ) )
        {
            TableFixedEntry e;
            e.src = their_at; e.dst = at; e.size = te.size; e.dstsize = (uint8_t) me.size;
            e.guard = guard; e.arg = arg;
            e.op = ( te.kind == 10 ) ? kTableFixedWidenF : kTableFixedWiden;
            e.sign = TableFixedSignedKind( te.kind ) ? 1u : 0u;
            TableFixedPush( c, e );
            return;
        }
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
            const TableFixedLayoutEntry tel = TableFixedEntryAt( theirs, ti + 1 );
            const TableFixedLayoutEntry mel = TableFixedEntryAt( mine, mi + 1 );
            const uint32_t head = d.counted ? 4u : 0u;
            const uint32_t their_n = tel.size ? ( te.size - head ) / tel.size : 0u;
            const uint32_t my_n = mel.size ? ( me.size - head ) / mel.size : 0u;
            uint32_t their_base = their_at + head;
            if ( d.counted )
            {
                TableFixedEntry e;
                e.src = their_at; e.dst = aux_at; e.size = their_n; e.guard = guard; e.op = kTableFixedCount; e.arg = arg;
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
            const TableFixedLayoutEntry tkey = TableFixedEntryAt( theirs, ti + 1 );
            const TableFixedLayoutEntry mkey = TableFixedEntryAt( mine, mi + 1 );
            const int32_t tel = ti + 1 + TableFixedSubtree( theirs, ti + 1 );
            const int32_t mel = mi + 1 + TableFixedSubtree( mine, mi + 1 );
            const TableFixedLayoutEntry tee = TableFixedEntryAt( theirs, tel );
            for ( uint32_t k = 0; k < mkey.children && mi + 2 + (int32_t) k < mine.count; ++k )
            {
                const uint64_t key_id = TableFixedEntryAt( mine, mi + 2 + (int32_t) k ).id;
                for ( uint32_t j = 0; j < tkey.children && ti + 2 + (int32_t) j < theirs.count; ++j )
                {
                    if ( TableFixedEntryAt( theirs, ti + 2 + (int32_t) j ).id != key_id ) { continue; }
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
            const uint8_t saved_argw = c.argw;
            c.argw = ( their_tag >= 1u && their_tag <= 8u ) ? (uint8_t) their_tag : 1u;
            int32_t my_arm = mi + 1;
            // THE TAG'S "NONE" GOES DOWN FIRST AND UNGUARDED. Every tag value
            // below is written under THEIR tag's guard, so a record naming an
            // arm this build does not have would land no tag at all — and the
            // prefill cannot answer for it, because those bytes are named by
            // the guarded entries. The plan is partitioned unguarded-first, so
            // this is overwritten by the arm that matches and stands when none
            // does. It is the ONE byte this form writes twice, and it is the
            // compiled path's alone.
            //
            // Nested: None answers to the OUTER tag at the OUTER width. Push
            // stamps c.argw, which is the inner tag's width here, so restore
            // the outer width for this one entry (bill §12.7, #876 card 13).
            {
                const uint8_t inner_argw = c.argw;
                if ( guard != kTableFixedNoGuard ) { c.argw = saved_argw ? saved_argw : 1u; }
                TableFixedEntry none;
                none.src = their_at; none.dst = aux_at; none.size = my_tag;
                none.aux = 0; none.guard = guard; none.op = kTableFixedConst; none.arg = arg;
                // Writer arm count, for a tag past the old set (bill §12.5).
                if ( te.children > 0 && te.children <= 255u ) { none.dstsize = (uint8_t) te.children; }
                TableFixedPush( c, none );
                c.argw = inner_argw;
            }
            const int32_t restamp_from = c.count;
            for ( uint32_t k = 0; k < me.children && my_arm < mine.count; ++k )
            {
                const TableFixedLayoutEntry ma = TableFixedEntryAt( mine, my_arm );
                int32_t their_arm = ti + 1;
                for ( uint32_t j = 0; j < te.children && their_arm < theirs.count; ++j )
                {
                    const TableFixedLayoutEntry ta = TableFixedEntryAt( theirs, their_arm );
                    if ( ta.id == ma.id )
                    {
                        // MY tag value, written under THEIR tag's guard
                        TableFixedEntry tag;
                        tag.src = their_at; tag.dst = aux_at; tag.size = my_tag;
                        tag.aux = k + 1; tag.guard = their_at; tag.op = kTableFixedConst; tag.arg = (uint64_t) j + 1u;
                        tag.argw = c.argw;
                        TableFixedPush( c, tag );
                        TableFixedCompileEntry( c, theirs, their_arm, their_at + their_tag,
                                                mine, my_arm, dst, at, their_at, (uint64_t) j + 1u );
                        break;
                    }
                    their_arm += TableFixedSubtree( theirs, their_arm );
                }
                my_arm += TableFixedSubtree( mine, my_arm );
            }
            // Nested: an arm inside an arm answers to the OUTER tag, which is
            // the one that decides whether any of it is there at all. Identity
            // rewrites inner-guarded leaves onto that tag; the compiled path
            // does the same so a foreign outer arm is not read as this inner
            // union (#876 card 13).
            if ( guard != kTableFixedNoGuard )
            {
                const uint8_t outer_w = saved_argw ? saved_argw : 1u;
                for ( int32_t i = restamp_from; i < c.count; ++i )
                {
                    // The inner tag's own consts stay on the inner tag: restamping
                    // them onto the outer tag fires every inner arm when the outer
                    // matches, and the last arm wins. Payload leaves answer to the
                    // OUTER tag (#876 card 13).
                    if ( c.plan[i].guard == their_at &&
                         !( c.plan[i].op == kTableFixedConst && c.plan[i].dst == aux_at ) )
                    {
                        c.plan[i].guard = guard;
                        c.plan[i].arg = arg;
                        c.plan[i].argw = outer_w;
                    }
                }
            }
            c.argw = saved_argw;
            break;
        }
        case 30: // an enum: the ordinal is the layout's position, so it remaps
        {
            if ( ( guard != kTableFixedNoGuard ) != c.want_guarded ) { break; }
            // A GROWN ORDINAL WIDTH IS A WIDEN (bill §12.7, algorithm §5.2):
            // append-only means the writer's ordinal IS the reader's, and the
            // counter owes one. Remap is only for a merge that reordered names.
            if ( te.size < me.size )
            {
                TableFixedEntry e;
                e.src = their_at; e.dst = at; e.size = te.size; e.dstsize = (uint8_t) me.size;
                e.guard = guard; e.arg = arg; e.op = kTableFixedWiden; e.sign = 0;
                TableFixedPush( c, e );
                break;
            }
            // FULL WIDTH (bill §12.7): the remap is one slot per writer
            // variant, not a 255-deep stack array. n past 65535 is a plan
            // that does not fit the uint16 length word; overflow refuses it.
            const uint32_t n = te.children;
            const uint32_t at_map = TableFixedLayTable( c, NULL, (int32_t) n );
            if ( c.overflow ) { break; }
            uint16_t * map = (uint16_t *) (void *) ( (uint8_t *) c.plan + at_map );
            for ( uint32_t j = 0; j < n; ++j )
            {
                const uint64_t vid = TableFixedEntryAt( theirs, ti + 1 + (int32_t) j ).id;
                uint16_t landed = 0;
                for ( uint32_t k = 0; k < me.children && mi + 1 + (int32_t) k < mine.count; ++k )
                {
                    if ( TableFixedEntryAt( mine, mi + 1 + (int32_t) k ).id == vid ) { landed = (uint16_t) ( k + 1 ); break; }
                }
                map[1 + j] = landed;
            }
            TableFixedEntry e;
            e.src = their_at; e.dst = at; e.size = te.size; e.guard = guard; e.op = kTableFixedOrdinal;
            e.arg = arg; e.dstsize = (uint8_t) me.size;
            e.aux = at_map;
            TableFixedPush( c, e );
            break;
        }
        case 12: case 33: // text
        {
            const uint32_t units = ( me.size - 4u ) < ( te.size - 4u ) ? ( me.size - 4u ) : ( te.size - 4u );
            TableFixedEntry e;
            e.src = their_at; e.dst = at; e.size = units; e.aux = aux_at; e.guard = guard;
            e.op = kTableFixedText; e.arg = arg; e.meta = d.meta;
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

inline int32_t TableFixedCompile( const TableFixedLayoutView & theirs,
                                  const uint8_t * my_layout, int32_t my_layout_bytes,
                                  const TableFixedDst * dst,
                                  const TableFixedFill * cover, int32_t cover_count,
                                  TableFixedEntry * plan, int32_t plan_capacity, int32_t * guarded,
                                  uint32_t * fill_at, int32_t * fill_count,
                                  TableReport * report )
{
    TableFixedLayoutView mine;
    TableMessageReason why = layout_malformed;
    if ( plan == NULL || plan_capacity <= 0 ) { return -1; }
    // MY OWN layout goes through the same rules a stranger's does. It is this
    // build's own bytes, so it cannot fail — and saying so in the one place
    // both sides go through is what keeps that true.
    if ( !TableFixedParseLayout( my_layout, my_layout_bytes, mine, why ) ) { return -1; }
    TableFixedCompiler c;
    c.plan = plan;
    c.capacity = plan_capacity;
    c.report = report;
    c.record = TableFixedEntryAt( theirs, 0 ).size;
    c.want_guarded = false;
    TableFixedMatchChildren( c, theirs, 0, 0, mine, 0, dst, 0, kTableFixedNoGuard, 0 );
    const int32_t plain = c.count;
    c.want_guarded = true;
    c.report = NULL; // the unknown census is the first pass's; counting it twice would lie
    TableFixedMatchChildren( c, theirs, 0, 0, mine, 0, dst, 0, kTableFixedNoGuard, 0 );
    if ( c.overflow || c.hostile )
    {
        if ( c.hostile && report != NULL ) { report->reason = layout_record_too_large; }
        return c.hostile ? -2 : -1;
    }
    // COALESCE inside each half, never across the split
    int32_t out = 0;
    int32_t split = 0;
    for ( int32_t i = 0; i < c.count; ++i )
    {
        if ( i == plain ) { split = out; }
        if ( out > split && plan[out-1].op == kTableFixedCopy && plan[i].op == kTableFixedCopy &&
             plan[out-1].guard == plan[i].guard && plan[out-1].arg == plan[i].arg &&
             plan[out-1].argw == plan[i].argw &&
             plan[out-1].src + plan[out-1].size == plan[i].src &&
             plan[out-1].dst + plan[out-1].size == plan[i].dst )
        {
            plan[out-1].size += plan[i].size;
            continue;
        }
        plan[out++] = plan[i];
    }
    if ( plain == c.count ) { split = out; }
    if ( guarded != NULL ) { *guarded = split; }
    // THE PREFILL'S RANGES, out of the same pool and by the same rule: the
    // type's value bytes minus everything this plan lands.
    c.count = out;
    *fill_at = 0;
    *fill_count = TableFixedFills( plan, out, cover, cover_count, NULL );
    if ( *fill_count > 0 )
    {
        const uint32_t at = TableFixedLayFills( c, *fill_count );
        if ( c.overflow ) { return -1; }
        TableFixedFills( plan, out, cover, cover_count, (TableFixedFill *) (void *) ( (uint8_t *) plan + at ) );
        *fill_at = at;
    }
    return out;
}

// ---- THE PLAN CACHE: once per peer, never once per record (§3.4) -----------
//
// THE CALLER OWNS THIS. The codec never allocates: the slots are a named
// table of 64, the plan bytes they point at are a slab the caller hands
// Init, and overflow is a miss — compile into the caller's plan buffer and
// do not store, which is Elixir's stance (JS is an unbounded Map). Identity
// hash never consults it. compiles counts successful compiles through this
// cache, which is the pin; made is 1 on a slot after the compile that
// filled it.

// One COMPILE'd lineage entry: the hash LOAD looks up, the layout bytes it
// memcmps, the writer's record size. Plans for older hashes are compiled from
// these trusted bytes, never from the file (algorithm §5.3).
struct TableFixedKnownLayout
{
    uint64_t hash = 0;
    const uint8_t * layout = NULL;
    int64_t layout_bytes = 0;
    int64_t record_bytes = 0;
};

// Writer ranges the hostile pass clamps against (bill §12.5). dst is the
// READER's storage offset; lo/hi are the WRITER's declared bounds.
struct TableFixedKnownRange
{
    uint32_t dst = 0;
    uint8_t width = 0;
    uint8_t sgn = 0;
    int64_t lo = 0;
    int64_t hi = 0;
};

inline void TableFixedClampKnownRanges( const TableFixedKnownRange * ranges, int32_t n,
                                        uint8_t * dst, TableReport * report )
{
    if ( ranges == NULL || n <= 0 || dst == NULL ) { return; }
    int32_t clamped = 0;
    for ( int32_t i = 0; i < n; ++i )
    {
        const TableFixedKnownRange & b = ranges[i];
        if ( b.width == 0 || b.width > 8 ) { continue; }
        uint64_t raw = 0;
        memcpy( &raw, dst + b.dst, b.width );
        int64_t v = 0;
        if ( b.sgn != 0 )
        {
            const unsigned bits = (unsigned) b.width * 8u;
            const uint64_t top = 1ull << ( bits - 1 );
            if ( raw & top ) { raw |= ~( ( top << 1 ) - 1ull ); }
            v = (int64_t) raw;
        }
        else
        {
            v = (int64_t) raw;
        }
        int64_t landed = v;
        if ( landed < b.lo ) { landed = b.lo; clamped++; }
        else if ( landed > b.hi ) { landed = b.hi; clamped++; }
        if ( landed == v ) { continue; }
        uint64_t out = (uint64_t) landed;
        memcpy( dst + b.dst, &out, b.width );
    }
    if ( report != NULL ) { report->clamped += clamped; }
}

constexpr int32_t kTableFixedPlanCacheCapacity = 64;

struct TableFixedPlanCacheSlot
{
    uint64_t hash = 0;
    TableFixedEntry * plan = NULL;
    int32_t count = 0;
    int32_t guarded = 0;
    int64_t record_bytes = 0;
    const TableFixedFill * fill = NULL;
    int32_t fill_count = 0;
    uint8_t made = 0;
};

struct TableFixedPlanCache
{
    TableFixedPlanCacheSlot slots[kTableFixedPlanCacheCapacity];
    TableFixedEntry * storage = NULL; // capacity * stride entries, caller-owned
    int32_t stride = 0;               // entries per slot, the plan capacity
    int32_t used = 0;
    int32_t compiles = 0;
};

inline void TableFixedPlanCacheInit( TableFixedPlanCache & cache, TableFixedEntry * storage, int32_t stride )
{
    cache.storage = storage;
    cache.stride = stride;
    cache.used = 0;
    cache.compiles = 0;
    for ( int32_t i = 0; i < kTableFixedPlanCacheCapacity; ++i )
    {
        cache.slots[i].hash = 0;
        cache.slots[i].plan = NULL;
        cache.slots[i].count = 0;
        cache.slots[i].guarded = 0;
        cache.slots[i].record_bytes = 0;
        cache.slots[i].fill = NULL;
        cache.slots[i].fill_count = 0;
        cache.slots[i].made = 0;
    }
}
`

// tableFixedRuntime128 is the FIXED FORM's 128-bit half, and it is separate for
// one reason: it names serialize.h's storage types, so it may only be emitted
// into a unit whose closure carries 128-bit storage — the same unit that
// includes serialize.h for it. The C leg splits the pair for the same reason
// (internal/codegen/ctable/fixedruntime.go).
//
// TWO OVERLOADS rather than one generic function: the two are the whole set,
// and the C leg, which has neither overloading nor generics, spells them as two
// named functions. It must be appended AFTER tableFixedRuntime, because both
// call TableFixedPut64.
const tableFixedRuntime128 = `
// sixteen bytes, the LOW 64-bit half first, which is this wire's order for the
// family everywhere else (docs/SPEC-TABLES.md §3).
inline void TableFixedPut128( uint8_t * b, serialize::int128_t v )
{
    TableFixedPut64( b, (uint64_t) v );
    TableFixedPut64( b + 8, (uint64_t) ( v >> 64 ) );
}
inline void TableFixedPut128( uint8_t * b, serialize::uint128_t v )
{
    TableFixedPut64( b, (uint64_t) v );
    TableFixedPut64( b + 8, (uint64_t) ( v >> 64 ) );
}
`
