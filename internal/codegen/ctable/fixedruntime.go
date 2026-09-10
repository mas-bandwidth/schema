package ctable

// tableFixedRuntime is the FIXED FORM's package-scoped runtime
// (docs/SPEC-TABLES.md §3.4): the plan entry, the ONE read loop, the LAYOUT
// reader and the plan compiler. It rides inside the same package guard as the
// rest of the primitives, so whichever <Base>Table.h a translation unit
// includes first defines it for the whole unit.
//
// EVERYTHING HERE IS ALLOCATION-FREE, which is this library's rule everywhere
// else and has no exception on this form: the plan's storage is the caller's,
// declared by capacity, and a layout whose plan does not fit is a refusal by
// name.
//
// THIS IS THE C TWIN OF internal/codegen/cpptable/fixedruntime.go, and the
// wire, the ops, the partition, the refusals and the counters are the same to
// the byte. Three spellings differ and each is the C form of one C++ mechanism
// rather than a new idea: the RAII depth guard is a paired increment and
// decrement, because C has no destructor; the 128-bit stores are two named
// functions in tableFixedRuntime128 below, because C has neither overloading
// nor a builtin 128-bit integer; and every function is snake_case behind
// static SCHEMA_UNUSED, because that is how the rest of this backend's runtime
// reads.
//
// THE PLAN BUILDER IS IN NEITHER LEG ANY MORE. Writing this port is what took
// it out of the reference: the identity plan was a compile-time walk only C++
// could run, and it is now the schema compiler's (ir/fixedform.go), laid down
// by every backend as static data. What is left here is the run-time
// compiler — the one a STRANGER's layout needs, which no generator can run
// ahead of time.
//
// @INLINE@ is textually replaced with the per-package force-inline macro
// (tableInlineMacro), exactly as the file wire's runtime does it.
const tableFixedRuntime = `
/* ---------------------------------------------------------------------------
   THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4)

   THE TYPES KEEP THE REFERENCE'S SPELLING AND THE FUNCTIONS DO NOT.
   TableFixedEntry is TableFixedEntry in both legs, because a type a generated
   header names in a declaration should read the same in every port; the
   functions are snake_case behind table_fixed_, because that is how
   table_writer_put32 and table_reader_get32 read three hundred lines above
   this one and a C header has one namespace to be consistent inside.
   --------------------------------------------------------------------------- */

enum { kTableFixedForm = 3 };

/* THE HEADER, ONE RULE FOR ALL FIVE FORMS (docs/SPEC-TABLES.md §3, "THE FIRST
   BYTE"): the FORM BYTE at offset 0, seven RESERVED ZERO bytes, the form's own
   EIGHT-BYTE HASH at offset 8, and the body at 16 — the alignment a
   memory-mapped body needs. The fixed form does not need the alignment today;
   it pads anyway, so the bytes do not move again the day the cook and the block
   form join the registry under the same header.

   The hash here is the LAYOUT's. Each record still carries its own eight-byte
   hash, which §3.4 has always said and which the header does not replace: the
   header names the layout ONCE for the file, and a record names the layout it
   was stamped by. */
enum { kTableFixedHeaderBytes = 16, kTableFixedHashAt = 8 };

/* THE OPS ARE THE WHOLE SET. The IDENTITY plan carries only the first three;
   the rest are what a plan compiled from another writer's layout adds. clamp
   is compiled-only: a ranged integer is a copy on the identity path and the
   generated straight-line pass holds it after the copy (docs/SPEC-TABLES.md §3.4).

   C HAS NO ENUM BASE TYPE, so where C++ writes enum : uint8_t these are plain
   anonymous enumerators — ints, stored into the uint8_t op and arg columns at
   the point of use, which is what the C++ enum's underlying type was saying. */
enum
{
    kTableFixedCopy    = 0, /* move size bytes */
    kTableFixedCount   = 1, /* a count: clamp it to the reader's own bound */
    kTableFixedText    = 2, /* a length, then the units, then terminate */
    kTableFixedOrdinal = 3, /* a variant ordinal, remapped through the plan's own table */
    kTableFixedWiden   = 4, /* a narrower source into a wider destination */
    kTableFixedConst   = 5, /* a constant this reader's own storage takes: a remapped union tag */
    kTableFixedWidenF  = 6, /* f32 into f64, §4's float rung */
    kTableFixedClamp   = 7  /* a ranged integer, in place on the destination the copy landed */
};

/* meta on a kTableFixedText entry */
enum
{
    kTableFixedTextUtf8  = 1,
    kTableFixedTextWide  = 2,
    kTableFixedTextBytes = 3
};

/* kTableFixedNoGuard IS THE ONE CONSTANT THAT CANNOT BE AN ENUMERATOR, and the
   reason is arithmetic rather than taste: an enumerator in C is an int, and
   0xFFFFFFFF does not fit one. So the value the C++ reference spells as an
   inline constexpr uint32_t is a MACRO here, guarded because a translation unit
   that includes two generated headers of this family meets it twice. */
#ifndef SCHEMA_TABLE_FIXED_NO_GUARD
#define SCHEMA_TABLE_FIXED_NO_GUARD 0xFFFFFFFFu
#endif

/* THE PLAN'S SOURCE AND DESTINATION NEVER ALIAS: one is a record in a read
   buffer and the other is the caller's own storage. Saying so is worth real
   time in the read loop, and the spelling is the compiler's — except in C,
   where C99 has the keyword itself and the feature test is what picks it. The
   __cplusplus arm matters because a generated header is includable from C++
   through its extern "C" block, and C++ has no restrict. */
#ifndef SCHEMA_TABLE_RESTRICT
#if defined( __STDC_VERSION__ ) && __STDC_VERSION__ >= 199901L && !defined( __cplusplus )
#define SCHEMA_TABLE_RESTRICT restrict
#elif defined( _MSC_VER )
#define SCHEMA_TABLE_RESTRICT __restrict
#elif defined( __GNUC__ ) || defined( __clang__ )
#define SCHEMA_TABLE_RESTRICT __restrict__
#else
#define SCHEMA_TABLE_RESTRICT
#endif
#endif

/* WHY A FIXED READ WAS REFUSED, by name (docs/SPEC-TABLES.md §3.3, §3.4). The
   message form's four reasons come with them under their own guard, because a
   fixed load refuses a batch larger than the caller's room by the same name the
   message form does, and a unit may carry the fixed form without carrying the
   message surface that would otherwise have declared it. Whichever block a
   translation unit meets first declares the set; the values are the same. */
#ifndef SCHEMA_TABLE_MESSAGE_REASONS
#define SCHEMA_TABLE_MESSAGE_REASONS
enum { SCHEMA_TABLE_NO_VOCABULARY=3,SCHEMA_TABLE_SECOND_ANNOUNCEMENT=4,SCHEMA_TABLE_VOCABULARY_TOO_LARGE=5,SCHEMA_TABLE_BATCH_TOO_LARGE=6 };
#endif
/* THE FIXED FORM'S THREE, AND THE LAYOUT VALIDATION'S SIX. Each is a refusal by
   name with nothing decoded and no counter moved, on the form byte's own
   precedent (§3.4). THE LAYOUT is what form 1 calls the vocabulary block.

   The six validation names exist because a layout is the one structure a reader
   must parse before it knows anything at all, and it arrives from an UNTRUSTED
   PEER: each rule it is held to refuses under its own name rather than under
   one word for all of them, and every one runs BEFORE a record byte is touched.
   The VALUES are this leg's own; the NAMES are the reference's, so a peer is
   refused by the same name whichever leg reads it. */
enum
{
    SCHEMA_TABLE_NO_LAYOUT = 7,        /* a record whose hash names no LAYOUT this reader holds */
    SCHEMA_TABLE_LAYOUT_MALFORMED = 8, /* bytes handed to the form as a layout that are not one: fewer than a header's worth of them */
    SCHEMA_TABLE_PLAN_TOO_LARGE = 9,   /* a layout whose compiled plan does not fit the plan storage the caller declared: this codec never allocates */
    SCHEMA_TABLE_LAYOUT_COUNT_MISMATCH = 10,  /* the entry count does not fit the layout's length exactly */
    SCHEMA_TABLE_LAYOUT_KIND_UNKNOWN = 11,    /* a kind OUTSIDE §3's closed set: a newer FORM BYTE, not a newer layout of this one */
    SCHEMA_TABLE_LAYOUT_KIND_INVALID = 12,    /* a kind this build KNOWS, used in a way its own definition does not allow */
    SCHEMA_TABLE_LAYOUT_SIZE_MISMATCH = 13,   /* a stated constant size that is not the one its kind fixes, or not the one its children account for */
    SCHEMA_TABLE_LAYOUT_TREE_UNCLOSED = 14,   /* the pre-order child walk does not consume exactly the entries */
    SCHEMA_TABLE_LAYOUT_TOO_DEEP = 15,        /* a nesting depth past what this reader walks: a bound on the WALK, not on the wire */
    SCHEMA_TABLE_LAYOUT_RECORD_TOO_LARGE = 16, /* a record size past §3.4's 65536-byte bound: a size this build will not decode */
    SCHEMA_TABLE_PREVIOUS_FORM = 17            /* a FORM BYTE this form is AHEAD of: the variable form handed to a fixed reader (§3) */
};

/* THE READ LOOP'S BODY IS ALWAYS INLINE. It is written once and used from both
   halves of the loop below, and a call there is the whole cost of the form.
   This leg defines no inline macro of its own: the two functions that need it
   carry the per-package force-inline macro the rest of these primitives already
   feature-test and define, so a unit has ONE such spelling and not two. */

/* §4'S WIDENING RUNGS, and the fixed form spends no rule of its own on them: a
   kind that GREW since the writer decodes at the writer's width and lands
   exactly, counting one widened. Coming back DOWN the ladder, or across two
   of them, is a kind that MOVED and is reported rather than reinterpreted.

   C++ ANSWERS bool AND C ANSWERS int, 0 or 1, which is what this backend does
   everywhere a predicate crosses a signature (table_reader_has beside it). */
static SCHEMA_UNUSED int table_fixed_widens( uint8_t from, uint8_t to )
{
    if ( from >= 6 && from <= 9 && to >= 6 && to <= 9 ) { return to > from; }  /* u8 .. u64 */
    if ( from >= 2 && from <= 5 && to >= 2 && to <= 5 ) { return to > from; }  /* i8 .. i64 */
    return from == 10 && to == 11;                                            /* f32 -> f64 */
}
static SCHEMA_UNUSED int table_fixed_signed_kind( uint8_t kind ) { return kind >= 2 && kind <= 5; }

/* A PLAN ENTRY. src and dst are byte offsets — into the record's body and into
   the reader's own storage. guard is the byte offset of a union tag when the
   entry belongs to an arm, and SCHEMA_TABLE_FIXED_NO_GUARD when it does not. */
typedef struct TableFixedEntry
{
    uint32_t src;
    uint32_t dst;
    uint32_t size;
    uint32_t aux;
    uint32_t guard;
    uint8_t op;
    /* arg IS THE GUARD'S TAG AND NOTHING ELSE: the ordinal the byte at guard
       must hold for this entry to run. meta IS THE OP'S OWN ARGUMENT — a text
       entry's flavour. THEY ARE TWO LANES BECAUSE THEY ARE TWO FACTS: they
       shared one, and a string(N) under a union's arm then had to be either
       guarded correctly or read with the right flavour and could not be both. */
    uint8_t arg;
    uint8_t meta;
    uint8_t dstsize;
    uint8_t sign; /* a WIDEN's source is two's complement, so it sign-extends */
} TableFixedEntry;

/* C HAS NO DEFAULT MEMBER INITIALIZERS, so the C++ struct's own defaults — zero
   everywhere and NO GUARD on guard — are this function instead, and every place
   the compiler below declares an entry starts from it. The guard column is the
   reason it cannot be a plain {0}: an entry whose guard reads 0 is an entry
   guarded by the byte at offset 0, which is a different plan. */
static SCHEMA_UNUSED TableFixedEntry table_fixed_entry_zero( void )
{
    TableFixedEntry e;
    memset( &e, 0, sizeof( e ) );
    e.guard = SCHEMA_TABLE_FIXED_NO_GUARD;
    return e;
}

/* A PLAN IS PARTITIONED: every UNGUARDED entry first, then every guarded one,
   and guarded is where the second half starts. Entries are independent —
   each writes its own bytes and a union's arms are mutually exclusive — so the
   order is free, and what it buys is that the entries that are nearly all of
   the plan never test a guard at all. Under a per-entry guard test the whole
   read is 13% slower, measured (test/bench/fixedform_measure.cpp).

   THE THREE FACTS TRAVEL AS THREE ARGUMENTS — the entry array, its count and
   the split. table_fixed_run below takes them in that order and the identity
   plan is emitted as exactly those three. */

/* THE IDENTITY PLAN HAS NO BUILDER, IN THIS LEG OR IN THE REFERENCE, AND
   WRITING THIS LEG IS WHAT TOOK IT OUT OF BOTH. It was a compile-time walk of
   the type's leaves, coalesced by generic C++ the compiler ran while it was
   still running — and C has no such thing to run it with. So the work moved ONE
   STEP EARLIER, to the SCHEMA COMPILER (ir/fixedform.go), which knows every
   offset that pass would have discovered and knows them sooner. Every backend
   now emits the identity plan as a static array that is ALREADY COALESCED and
   ALREADY PARTITIONED, beside the two constants that say how long it is and
   where its guarded half starts. Nothing is built at run time, and nothing at
   load time, for this build's own hash — in any port.

   THE COALESCER ITSELF IS NOT GONE, and that is the load-bearing half of this
   note. The pass at the tail of table_fixed_compile is the same coalescer, rule
   for rule — two neighbouring COPY entries whose source and destination both
   advance together are one entry, inside each half and never across the split —
   and it is what runs for A STRANGER'S BLOCK, which no generator can run ahead
   of time. So the two arrivals differ and the rule does not.

   THE OFFSETS ARE THIS COMPILER'S TOO, EVENTUALLY. A plan the schema compiler
   laid down carries destinations the schema compiler computed, so the generated
   header asserts every one of them against offsetof and sizeof before the plan
   appears. A layout the generator ever got wrong is a build error here and
   never a misplaced value at run time. */

/* ---- the little-endian moves ----------------------------------------------- */

static SCHEMA_UNUSED void table_fixed_put8( uint8_t * b, uint8_t v ) { b[0] = v; }
static SCHEMA_UNUSED void table_fixed_put16( uint8_t * b, uint16_t v ) { b[0] = (uint8_t)( v ); b[1] = (uint8_t)( v >> 8 ); }
static SCHEMA_UNUSED void table_fixed_put32( uint8_t * b, uint32_t v )
{
    int i;
    for ( i = 0; i < 4; ++i ) { b[i] = (uint8_t)( v >> ( 8 * i ) ); }
}
static SCHEMA_UNUSED void table_fixed_put64( uint8_t * b, uint64_t v )
{
    int i;
    for ( i = 0; i < 8; ++i ) { b[i] = (uint8_t)( v >> ( 8 * i ) ); }
}
static SCHEMA_UNUSED void table_fixed_putf32( uint8_t * b, float v ) { uint32_t w; memcpy( &w, &v, 4 ); table_fixed_put32( b, w ); }
static SCHEMA_UNUSED void table_fixed_putf64( uint8_t * b, double v ) { uint64_t w; memcpy( &w, &v, 8 ); table_fixed_put64( b, w ); }
/* THE 128-BIT PAIR IS NOT HERE. C++ spells it as one template over a builtin
   128-bit integer; this leg's 128-bit storage is serialize.h's lo/hi pair, so
   the two functions that replace the template can only be emitted into a unit
   that includes serialize.h. They are emitted separately and appended by the
   caller for exactly those units, so that NOT ONE LINE OF THIS RUNTIME NAMES A
   PACKET-HEADER TYPE and a Table header stays includable from a translation
   unit that has no packet header at all. */
static SCHEMA_UNUSED uint32_t table_fixed_get32( const uint8_t * b )
{
    uint32_t v = 0;
    int i;
    for ( i = 0; i < 4; ++i ) { v |= ( (uint32_t) b[i] ) << ( 8 * i ); }
    return v;
}
static SCHEMA_UNUSED uint64_t table_fixed_get64( const uint8_t * b )
{
    uint64_t v = 0;
    int i;
    for ( i = 0; i < 8; ++i ) { v |= ( (uint64_t) b[i] ) << ( 8 * i ); }
    return v;
}

/* THE HASH is fnv1a64 over the layout's bytes exactly as written (§3.4). */
static SCHEMA_UNUSED uint64_t table_fixed_hash_of( const uint8_t * layout, int64_t bytes )
{
    uint64_t h = 0xcbf29ce484222325ull;
    int64_t i;
    for ( i = 0; i < bytes; ++i )
    {
        h ^= (uint64_t) layout[i];
        h *= 0x100000001b3ull;
    }
    return h;
}

/* ---- THE RUN COPY ----------------------------------------------------------

   A RUN IS A SMALL, KNOWN NUMBER OF BYTES AND THE COPY IS WRITTEN AS
   OVERLAPPING UNALIGNED WORD MOVES, NOT AS A CALL. This is a REQUIREMENT of
   the form and not an optimization a port may skip: the ruling that there is
   ONE reader path rests on a measurement, and with a runtime-length memcpy per
   entry that same measurement is 3.1x instead of 1.3x
   (test/bench/fixedform_measure.cpp).

   AND EVERY MOVE STAYS WITHIN [ run start, run end ). Overlapping moves are the
   technique; a move anchored OUTSIDE the run is not the technique, it is a
   defect — it clobbers whatever field sits in front of the destination and it
   reads past the record body. */
static SCHEMA_UNUSED @INLINE@ void table_fixed_copy_run( uint8_t * d, const uint8_t * s, uint32_t n )
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
    /* EVERY MOVE IS ANCHORED INSIDE [ s, s + n ), AND THAT IS THE INVARIANT THIS
       BRANCH EXISTS TO STATE. A run of 17..31 bytes has no room for a sixteen,
       a second sixteen and a thirty-two, so it takes TWO SIXTEENS that OVERLAP
       in the middle: one at the run's start and one at end - 16, both wholly
       within the run because n >= 16. Anchoring the tail move at n - 32
       instead would put it 32 - n bytes IN FRONT of the run — reading and
       writing a neighbour's bytes, and reading past the record body, which for
       a file's last record is past the buffer. */
    if ( n <= 32 )
    {
        uint64_t w[2];
        memcpy( w, s, 16 ); memcpy( d, w, 16 );
        memcpy( w, s + n - 16, 16 ); memcpy( d + n - 16, w, 16 );
        return;
    }
    if ( n <= 64 )
    {
        /* n >= 33 here, so n - 32 is at least 1 and the tail move begins
           inside the run, exactly as the two moves in front of it do. */
        uint64_t w[4];
        memcpy( w, s, 16 ); memcpy( d, w, 16 );
        memcpy( w, s + 16, 16 ); memcpy( d + 16, w, 16 );
        memcpy( w, s + n - 32, 32 ); memcpy( d + n - 32, w, 32 );
        return;
    }
    memcpy( d, s, n );
}

/* ---- THE ONE READ LOOP -----------------------------------------------------

   A READ IS A PREFILL AND THIS LOOP, AND NOTHING ELSE. Which plan it is handed
   is the only thing that differs between reading this build's own record and
   reading anybody else's.

   THE C++ REFERENCE TAKES THE ENTRY BY const & AND THE COUNTERS BY int32_t &.
   C has no reference: the entry arrives as a const TableFixedEntry * and the
   two counters as int32_t *, which is the same call and the same code once the
   force-inline has done its work. */
static SCHEMA_UNUSED @INLINE@ void table_fixed_apply( const TableFixedEntry * p, const uint8_t * base,
                                                      const uint8_t * SCHEMA_TABLE_RESTRICT src,
                                                      uint8_t * SCHEMA_TABLE_RESTRICT dst,
                                                      int32_t * clamped, int32_t * widened )
{
    switch ( p->op )
    {
        case kTableFixedCopy:
        {
            table_fixed_copy_run( dst + p->dst, src + p->src, p->size );
            break;
        }
        case kTableFixedCount:
        {
            int32_t v = (int32_t) table_fixed_get32( src + p->src );
            if ( v < 0 ) { v = 0; (*clamped)++; }
            else if ( v > (int32_t) p->size ) { v = (int32_t) p->size; (*clamped)++; }
            memcpy( dst + p->dst, &v, 4 );
            break;
        }
        case kTableFixedText:
        {
            const uint32_t unit = ( p->meta == kTableFixedTextWide ) ? 2u : 1u;
            const uint32_t cap = p->size / unit;
            int32_t v = (int32_t) table_fixed_get32( src + p->src );
            if ( v < 0 ) { v = 0; (*clamped)++; }
            else if ( (uint32_t) v > cap ) { v = (int32_t) cap; (*clamped)++; }
            memcpy( dst + p->dst, &v, 4 );
            table_fixed_copy_run( dst + p->aux, src + p->src + 4, p->size );
            if ( p->meta != kTableFixedTextBytes )
            {
                /* the used length terminates the buffer, whose storage is one
                   unit longer than the bound for exactly this. A store, not a
                   call: memset of a runtime length is a call. */
                uint8_t * end = dst + p->aux + (uint32_t) v * unit;
                end[0] = 0;
                if ( unit == 2 ) { end[1] = 0; }
            }
            break;
        }
        case kTableFixedOrdinal:
        {
            /* A VARIANT ORDINAL IS ITS POSITION IN THE LAYOUT, so a writer whose
               enum gained a variant IN THE MIDDLE is remapped here and never
               reinterpreted. The table is the plan's own, laid down by the
               compiler above the entries. */
            uint32_t raw = 0;
            memcpy( &raw, src + p->src, p->size );
            const uint16_t * remap = (const uint16_t *) (const void *) ( base + p->aux );
            uint32_t v = 0;
            if ( raw != 0 && raw <= (uint32_t) remap[0] ) { v = remap[raw]; }
            memcpy( dst + p->dst, &v, p->dstsize );
            break;
        }
        case kTableFixedWiden:
        {
            uint64_t raw = 0;
            memcpy( &raw, src + p->src, p->size );
            if ( p->sign != 0 )
            {
                /* TWO'S COMPLEMENT WIDENS BY ITS SIGN BIT, which is the whole
                   reason a widen is an op and not a short copy. */
                const unsigned bits = p->size * 8u;
                const uint64_t top = 1ull << ( bits - 1 );
                if ( raw & top ) { raw |= ~( ( top << 1 ) - 1ull ); }
            }
            memcpy( dst + p->dst, &raw, p->dstsize );
            (*widened)++;
            break;
        }
        case kTableFixedWidenF:
        {
            float f = 0.0f;
            memcpy( &f, src + p->src, 4 );
            const double wide = (double) f;
            memcpy( dst + p->dst, &wide, 8 );
            (*widened)++;
            break;
        }
        case kTableFixedConst:
        {
            memcpy( dst + p->dst, &p->aux, p->size );
            break;
        }
        case kTableFixedClamp:
        {
            /* IN PLACE ON THE DESTINATION the copy or widen just landed. src is
               unused: this op does not read the record. meta bit 0 is the low
               end, bit 1 the high end; sign is signedness. THE COUNT IS AN OR
               OF THE TWO ENDS, the same select-and-add the identity pass emits. */
            uint64_t lo = 0, hi = 0, raw = 0;
            int32_t under = 0, over = 0;
            memcpy( &lo, base + p->aux, 8 );
            memcpy( &hi, base + p->aux + 8, 8 );
            memcpy( &raw, dst + p->dst, p->size );
            if ( p->sign != 0 && p->size < 8 )
            {
                const unsigned bits = p->size * 8u;
                const uint64_t top = 1ull << ( bits - 1 );
                if ( raw & top ) { raw |= ~( ( top << 1 ) - 1ull ); }
            }
            if ( p->sign != 0 )
            {
                if ( ( p->meta & 1u ) != 0 ) { under = (int32_t) ( (int64_t) raw < (int64_t) lo ); }
                if ( ( p->meta & 2u ) != 0 ) { over  = (int32_t) ( (int64_t) raw > (int64_t) hi ); }
                raw = under ? lo : ( over ? hi : raw );
            }
            else
            {
                if ( ( p->meta & 1u ) != 0 ) { under = (int32_t) ( raw < lo ); }
                if ( ( p->meta & 2u ) != 0 ) { over  = (int32_t) ( raw > hi ); }
                raw = under ? lo : ( over ? hi : raw );
            }
            (*clamped) += under | over;
            memcpy( dst + p->dst, &raw, p->size );
            break;
        }
        default: break;
    }
}

/* A READ IS A PREFILL AND THIS LOOP, AND NOTHING ELSE. Which plan it is handed
   is the only thing that differs between reading this build's own record and
   reading anybody else's. */
static SCHEMA_UNUSED void table_fixed_run( const TableFixedEntry * plan, int32_t count, int32_t guarded,
                                           const uint8_t * SCHEMA_TABLE_RESTRICT src,
                                           uint8_t * SCHEMA_TABLE_RESTRICT dst,
                                           TableReport * report )
{
    /* THE COUNTERS ARE LOCAL AND WRITTEN BACK ONCE. A report the loop wrote
       through is a pointer the compiler must assume aliases the destination,
       and every store to a field would then reload it. */
    int32_t clamped = 0;
    int32_t widened = 0;
    int32_t i;
    /* THE ORDINAL REMAP TABLES RIDE ABOVE THE ENTRIES IN THE SAME STORAGE, so
       the plan's own base is the base an ordinal entry's aux is measured from.
       The C++ reference passes (const uint8_t *) plan for this and so does
       this leg, unchanged: base is deliberately NOT restrict, because it does
       alias the plan the loop is reading. */
    for ( i = 0; i < guarded; ++i )
    {
        table_fixed_apply( &plan[i], (const uint8_t *) plan, src, dst, &clamped, &widened );
    }
    for ( i = guarded; i < count; ++i )
    {
        const TableFixedEntry * p = &plan[i];
        if ( src[p->guard] != p->arg ) { continue; }
        table_fixed_apply( p, (const uint8_t *) plan, src, dst, &clamped, &widened );
    }
    report->clamped += clamped;
    report->widened += widened;
}

/* ---- THE LAYOUT ------------------------------------------------------------

   An entry is SEVENTEEN BYTES and the layout is a COUNT and a run of them.
   THERE IS NO VERSION BYTE INSIDE IT (docs/SPEC-TABLES.md §3.4): the FORM BYTE
   versions everything behind it, the layout's own format included, so a layout
   format change is a NEW FORM BYTE and never a wider entry. */

enum { kTableFixedEntryBytes = 17 };
enum { kTableFixedLayoutHeaderBytes = 4 }; /* the u32 entry count, and nothing else */

/* §3.4'S RECORD BOUND, and a reader holds an untrusted peer's layout to it for
   the same reason the compiler holds a declaration to it: a record past it is
   one this build will not decode.

   A BOUND ON THE WALK follows it, not on the wire: the validation below is
   recursive, so a hostile layout of three thousand entries each claiming one
   child would otherwise spend a reader's stack before any rule fired. */
enum { kTableFixedRecordMaxBytes = 65536 };
enum { kTableFixedMaxDepth = 64 };

typedef struct TableFixedLayoutEntry
{
    uint64_t id;
    uint32_t size;
    uint32_t children;
    uint8_t kind;
} TableFixedLayoutEntry;

/* The C++ struct's own defaults, again as a function: C has none of its own. */
static SCHEMA_UNUSED TableFixedLayoutEntry table_fixed_layout_entry_zero( void )
{
    TableFixedLayoutEntry out;
    memset( &out, 0, sizeof( out ) );
    return out;
}

typedef struct TableFixedLayoutView
{
    const uint8_t * bytes;
    int32_t count;
} TableFixedLayoutView;

/* bytes IS SET TO NULL AND NOT LEFT TO THE memset, because a null pointer is
   not required to be all-zero bits and the whole layout reader tests it by name. */
static SCHEMA_UNUSED TableFixedLayoutView table_fixed_layout_view_zero( void )
{
    TableFixedLayoutView out;
    memset( &out, 0, sizeof( out ) );
    out.bytes = NULL;
    out.count = 0;
    return out;
}

/* AN INDEX PAST THE ENTRIES IS ANSWERED, NOT READ. Every index into a layout
   is arithmetic over child counts a STRANGER wrote, so the one place that can
   hold the whole walk inside the buffer is the one place that touches it. An
   out-of-range index answers kind 0xFF, which is a kind no declaration has and
   nothing matches, so the walk that asked for it finds nothing and moves on. */
static SCHEMA_UNUSED TableFixedLayoutEntry table_fixed_entry_at( const TableFixedLayoutView * b, int32_t i )
{
    TableFixedLayoutEntry out = table_fixed_layout_entry_zero();
    if ( b->bytes == NULL || i < 0 || i >= b->count ) { out.kind = 0xFFu; return out; }
    const uint8_t * e = b->bytes + kTableFixedLayoutHeaderBytes + (int64_t) i * kTableFixedEntryBytes;
    out.id = table_fixed_get64( e );
    out.kind = e[8];
    out.size = table_fixed_get32( e + 9 );
    out.children = table_fixed_get32( e + 13 );
    return out;
}

/* table_fixed_subtree is how many entries the subtree rooted at i occupies, so
   a walk steps over a child it does not want without knowing what is in it. */
static SCHEMA_UNUSED int32_t table_fixed_subtree( const TableFixedLayoutView * b, int32_t i )
{
    /* ITERATIVE ON PURPOSE. A layout is a stranger's bytes, so a chain of
       single-child entries is a stack depth the wire gets to choose; this walk
       gives it none. */
    if ( i < 0 || i >= b->count ) { return 1; }
    int64_t pending = 1;
    int32_t n = 0;
    int32_t at = i;
    while ( pending > 0 && at < b->count )
    {
        pending--;
        n++;
        pending += (int64_t) table_fixed_entry_at( b, at ).children;
        at++;
        if ( pending > (int64_t) b->count ) { break; }
    }
    return n;
}

static SCHEMA_UNUSED uint32_t table_fixed_union_arm_bytes( const TableFixedLayoutView * b, int32_t i )
{
    const TableFixedLayoutEntry e = table_fixed_entry_at( b, i );
    uint32_t widest = 0;
    int32_t at = i + 1;
    uint32_t k;
    for ( k = 0; k < e.children && at < b->count; ++k )
    {
        const TableFixedLayoutEntry a = table_fixed_entry_at( b, at );
        if ( a.size > widest ) { widest = a.size; }
        at += table_fixed_subtree( b, at );
    }
    return widest;
}

static SCHEMA_UNUSED uint32_t table_fixed_tag_bytes( const TableFixedLayoutView * b, int32_t i )
{
    return table_fixed_entry_at( b, i ).size - table_fixed_union_arm_bytes( b, i );
}

/* ---- THE LAYOUT'S OWN VALIDATION, RULE BY NAMED RULE -----------------------

   A LAYOUT ARRIVES FROM AN UNTRUSTED PEER and it is the one structure a reader
   must parse before it knows anything at all, so every rule below runs BEFORE
   a single record byte is touched and each refuses under its OWN NAME rather
   than under one word for all of them (docs/SPEC-TABLES.md §3.4). Rule for
   rule and name for name, this is the C++ reference's own: a peer must be
   refused by the same name whichever leg reads it.

   A KIND THIS BUILD DOES NOT KNOW IS A REFUSAL, and that is a ruling rather
   than an oversight. A FIXED FORM HAS A CLOSED KIND SET: the form byte versions
   everything behind it, kinds included, so a layout carrying a kind outside the
   set is not a newer layout of THIS form — it is a layout of a form whose byte
   this reader never saw.

   NOR IS THERE A CYCLE RULE, because a pre-order walk cannot express a cycle:
   an entry's children ARE THE ENTRIES THAT FOLLOW IT, so a child's index is
   always higher than its parent's, the layout is finite, and there is no back
   reference for a cycle to be made of. */

typedef struct TableFixedCheck
{
    const TableFixedLayoutView * layout;
    int reason;
    int bad;
} TableFixedCheck;

static SCHEMA_UNUSED void table_fixed_fail( TableFixedCheck * c, int why )
{
    if ( !c->bad ) { c->bad = 1; c->reason = why; }
}

/* A LEAF KIND'S ADMITTED SIZES. Kind and size fix each other on this wire with
   ONE exception, and it is stated here rather than left to be found: a
   bits(N) field rides at its DECLARED STORAGE WIDTH — four bytes for N <= 32 and
   eight above (§3.4) — under the unsigned integer kind its BIT COUNT picks
   (§3), so kinds 6 and 7 admit four as well as their own width. */
static SCHEMA_UNUSED int table_fixed_leaf_size( uint8_t kind, uint32_t size, int * is_leaf )
{
    *is_leaf = 1;
    switch ( kind )
    {
        case 1:  return size == 1u;                  /* bool */
        case 2:  return size == 1u;                  /* i8 */
        case 3:  return size == 2u;                  /* i16 */
        case 4:  return size == 4u;                  /* i32 */
        case 5:  return size == 8u;                  /* i64 */
        case 6:  return size == 1u || size == 4u;    /* u8,  and bits( 1 .. 8 ) */
        case 7:  return size == 2u || size == 4u;    /* u16, and bits( 9 .. 16 ) */
        case 8:  return size == 4u;                  /* u32, and bits( 17 .. 32 ) */
        case 9:  return size == 8u;                  /* u64, flags, and bits( 33 .. 64 ) */
        case 10: return size == 4u;                  /* f32 */
        case 11: return size == 8u;                  /* f64 */
        case 17: return size == 4u;                  /* a pointer index, which no fixed table carries */
        case 18: case 19: return size == 16u;        /* i128, u128 */
        case 20: case 25: return size == 1u;         /* fixed8, ufixed8 */
        case 21: case 26: return size == 2u;
        case 22: case 27: return size == 4u;
        case 23: case 28: return size == 8u;
        case 24: case 29: return size == 16u;
        case 32: return size == 0u;                  /* a variant, and an arm that holds nothing */
        default: break;
    }
    *is_leaf = 0;
    return 1;
}

static SCHEMA_UNUSED int table_fixed_ordinal_width( uint32_t n ) { return n == 1u || n == 2u || n == 4u || n == 8u; }

/* THE CLOSED KIND SET (docs/SPEC-TABLES.md §3, §3.4): §3's own kinds 1..30,
   the no-payload variant 32, wstring 33, and the ONE kind this form's layout
   adds, the optional wrapper 35. 31 is §3's BODY framing escape and 34 is
   reserved: neither is a kind a declaration spells, so neither is ever an
   entry's kind. */
static SCHEMA_UNUSED int table_fixed_known_kind( uint8_t kind )
{
    if ( kind >= 1u && kind <= 30u ) { return 1; }
    return kind == 32u || kind == 33u || kind == 35u;
}

/* table_fixed_check_entry validates the subtree rooted at i and answers how
   many entries it occupies, which is the same arithmetic table_fixed_subtree
   does and is why that walk is safe to run afterwards and only afterwards. */
static SCHEMA_UNUSED int32_t table_fixed_check_entry( TableFixedCheck * c, int32_t i, int32_t depth )
{
    TableFixedLayoutEntry e;
    int32_t at;
    uint64_t sum = 0;
    uint32_t widest = 0;
    uint32_t first_size = 0, second_size = 0;
    uint8_t first_kind = 0;
    uint32_t first_children = 0;
    int kids_are_variants = 1;
    uint32_t k;
    int is_leaf = 0;
    if ( c->bad ) { return 0; }
    if ( i < 0 || i >= c->layout->count ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_TREE_UNCLOSED ); return 0; }
    if ( depth > kTableFixedMaxDepth ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_TOO_DEEP ); return 0; }
    e = table_fixed_entry_at( c->layout, i );
    /* THE CLOSED KIND SET, FIRST: a kind outside it is a layout of another form
       and is refused whole, never walked and never stepped over. */
    if ( !table_fixed_known_kind( e.kind ) ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_KIND_UNKNOWN ); return 0; }
    if ( e.size > kTableFixedRecordMaxBytes ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_RECORD_TOO_LARGE ); return 0; }

    /* THE CHILDREN FIRST, in the pre-order the layout is written in. */
    at = i + 1;
    for ( k = 0; k < e.children; ++k )
    {
        const int32_t child = at;
        const int32_t used = table_fixed_check_entry( c, child, depth + 1 );
        TableFixedLayoutEntry ce;
        if ( c->bad ) { return 0; }
        ce = table_fixed_entry_at( c->layout, child );
        if ( k == 0 ) { first_size = ce.size; first_kind = ce.kind; first_children = ce.children; }
        if ( k == 1 ) { second_size = ce.size; }
        if ( ce.kind != 32 ) { kids_are_variants = 0; }
        sum += ce.size;
        if ( ce.size > widest ) { widest = ce.size; }
        if ( sum > (uint64_t) kTableFixedRecordMaxBytes ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_RECORD_TOO_LARGE ); return 0; }
        at += used;
    }

    if ( !table_fixed_leaf_size( e.kind, e.size, &is_leaf ) ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_SIZE_MISMATCH ); return 0; }
    if ( is_leaf )
    {
        if ( e.children != 0u ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_KIND_INVALID ); return 0; }
        return at - i;
    }
    switch ( e.kind )
    {
        case 13: /* a TABLE: its size is the SUM of its fields' */
        {
            if ( (uint64_t) e.size != sum ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_SIZE_MISMATCH ); return 0; }
            break;
        }
        case 35: /* the OPTIONAL WRAPPER: one child, one present byte in front of it */
        {
            if ( e.children != 1u ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_KIND_INVALID ); return 0; }
            if ( (uint64_t) e.size != sum + 1u ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_SIZE_MISMATCH ); return 0; }
            break;
        }
        case 14: /* an ARRAY: a whole number of elements, behind a count or not */
        {
            int bare, counted;
            if ( e.children != 1u ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_KIND_INVALID ); return 0; }
            if ( first_size == 0u ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_SIZE_MISMATCH ); return 0; }
            bare = ( e.size % first_size ) == 0u;
            counted = e.size >= 4u && ( ( e.size - 4u ) % first_size ) == 0u;
            if ( !bare && !counted ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_SIZE_MISMATCH ); return 0; }
            break;
        }
        case 16: /* an ENUM-KEYED array: the KEY ENUM then the ELEMENT, every slot written */
        {
            if ( e.children != 2u ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_KIND_INVALID ); return 0; }
            if ( first_kind != 30 ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_KIND_INVALID ); return 0; }
            if ( second_size == 0u || ( e.size % second_size ) != 0u ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_SIZE_MISMATCH ); return 0; }
            /* AT LEAST one slot per variant. Not exactly one: an enum widened
               by an explicit max has more slots than it has names, and the
               layout carries only the names. */
            if ( e.size / second_size < first_children ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_SIZE_MISMATCH ); return 0; }
            break;
        }
        case 15: /* a UNION: the TAG at its own width, then the WIDEST ARM */
        {
            if ( e.children == 0u ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_KIND_INVALID ); return 0; }
            if ( e.size <= widest ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_SIZE_MISMATCH ); return 0; }
            if ( !table_fixed_ordinal_width( e.size - widest ) ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_SIZE_MISMATCH ); return 0; }
            break;
        }
        case 30: /* an ENUM: the ordinal's storage width, and its children are VARIANTS */
        {
            if ( !table_fixed_ordinal_width( e.size ) ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_SIZE_MISMATCH ); return 0; }
            if ( e.children != 0u && !kids_are_variants ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_KIND_INVALID ); return 0; }
            break;
        }
        case 12: /* string(N): the length, then N bytes */
        {
            if ( e.children != 0u ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_KIND_INVALID ); return 0; }
            if ( e.size < 4u ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_SIZE_MISMATCH ); return 0; }
            break;
        }
        case 33: /* wstring(N): the length in CODE UNITS, then 2N bytes */
        {
            if ( e.children != 0u ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_KIND_INVALID ); return 0; }
            if ( e.size < 4u || ( ( e.size - 4u ) % 2u ) != 0u ) { table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_SIZE_MISMATCH ); return 0; }
            break;
        }
        /* Every kind of the closed set is either a leaf above or a case here,
           so this is unreachable — and it refuses rather than admits, because
           a kind that reached it is a kind the two lists disagree about. */
        default: table_fixed_fail( c, SCHEMA_TABLE_LAYOUT_KIND_UNKNOWN ); return 0;
    }
    return at - i;
}

/* table_fixed_parse_layout is the WHOLE of what a reader trusts a layout on. It
   answers 0 and a REASON BY NAME, and the caller reports that reason.

   C++ FILLS AN out REFERENCE AND ANSWERS bool; C fills an out POINTER and
   answers 0 or 1. Everything else, including the refusal that leaves out empty,
   is the same. */
static SCHEMA_UNUSED int table_fixed_parse_layout( const uint8_t * bytes, int64_t length, TableFixedLayoutView * out, int * why )
{
    uint32_t count;
    TableFixedLayoutView view;
    TableFixedLayoutEntry root;
    TableFixedCheck c;
    int32_t used;
    out->bytes = NULL;
    out->count = 0;
    if ( bytes == NULL || length < kTableFixedLayoutHeaderBytes ) { *why = SCHEMA_TABLE_LAYOUT_MALFORMED; return 0; }
    /* 1. THE ENTRY COUNT FITS THE LAYOUT LENGTH EXACTLY */
    count = table_fixed_get32( bytes );
    if ( count == 0u || (int64_t) count * kTableFixedEntryBytes + kTableFixedLayoutHeaderBytes != length )
    {
        *why = SCHEMA_TABLE_LAYOUT_COUNT_MISMATCH;
        return 0;
    }
    view = table_fixed_layout_view_zero();
    view.bytes = bytes;
    view.count = (int32_t) count;
    /* 2. THE ROOT IS A TABLE, and its size is the record's body */
    root = table_fixed_entry_at( &view, 0 );
    if ( root.kind != 13 ) { *why = SCHEMA_TABLE_LAYOUT_KIND_INVALID; return 0; }
    if ( root.size == 0u || root.size > kTableFixedRecordMaxBytes ) { *why = SCHEMA_TABLE_LAYOUT_RECORD_TOO_LARGE; return 0; }
    /* 3. EVERY KIND IN THE CLOSED SET AND USED AS ITS DEFINITION ALLOWS, EVERY
          SIZE THE ONE ITS CHILDREN ACCOUNT FOR, NOTHING PAST THE WALK'S BOUND */
    c.layout = &view;
    c.reason = SCHEMA_TABLE_LAYOUT_MALFORMED;
    c.bad = 0;
    used = table_fixed_check_entry( &c, 0, 0 );
    if ( c.bad ) { *why = c.reason; return 0; }
    /* 4. THE PRE-ORDER WALK CONSUMES EXACTLY THE ENTRIES: the tree closes and
          the layout has nothing left over */
    if ( used != view.count ) { *why = SCHEMA_TABLE_LAYOUT_TREE_UNCLOSED; return 0; }
    *out = view;
    return 1;
}

/* ---- THE PLAN COMPILER -----------------------------------------------------

   The SAME LOOP runs over this plan as over the identity plan. What the
   compiler does, once per peer, is what a read would otherwise do once per
   record: map the writer's ids onto this reader's fields; leave a field it
   cannot name OUT of the plan, which is what skips it, by arithmetic that was
   going to step past it anyway; and leave a field the writer does not carry
   out too, which is what defaults it, because the prefill already put the
   declared default there. */

/* TableFixedDst is MY side of the walk: one row per entry of my own layout,
   carrying the storage facts a layout entry cannot. The generator emits it as a
   static const array beside the layout, so it needs no zeroing helper of its
   own — nothing in this runtime ever declares one. */
typedef struct TableFixedDst
{
    uint32_t dst;    /* this entry's storage offset inside its parent */
    uint32_t stride; /* an array entry's storage stride */
    uint32_t aux;    /* a text field's buffer offset */
    uint8_t counted; /* an array that carries a live count */
    uint8_t meta;    /* a text field's flavour, which is the TEXT OP's own argument */
    uint64_t lo;     /* a compiled clamp's low end, bit pattern of the slot */
    uint64_t hi;     /* a compiled clamp's high end */
    uint8_t clamp;   /* bit 0 = low, bit 1 = high, bit 2 = signed */
    uint8_t width;   /* the slot's bytes; 0 = this row is not a compiled clamp */
} TableFixedDst;

/* C HAS NO bool: want_guarded, overflow and hostile are ints holding 0 or 1,
   which is what this backend spells everywhere a C++ bool crosses a struct or a
   signature. They are only ever assigned 0 or 1, so the C++ code's
   bool-against-bool comparisons below stay comparisons and need no normalising. */
typedef struct TableFixedCompiler
{
    TableFixedEntry * plan;
    int32_t capacity;
    int32_t count;
    int32_t pool;      /* bytes of remap table laid down from the TOP, downward */
    uint32_t record;   /* the WRITER's declared body size: every entry is bounded by it */
    int32_t depth;     /* the nesting this walk is inside, capped below */
    int want_guarded;  /* THE WALK RUNS TWICE, unguarded first (§3.4) */
    int overflow;
    int hostile;
    int skip_clamp;    /* counted-array elements: the live count, never slack */
    TableReport * report;
} TableFixedCompiler;

/* The C++ struct's defaults once more, and here they are a whole-struct zero
   plus the two pointers named, for the reason table_fixed_layout_view_zero gives. */
static SCHEMA_UNUSED void table_fixed_compiler_init( TableFixedCompiler * c )
{
    memset( c, 0, sizeof( *c ) );
    c->plan = NULL;
    c->report = NULL;
}

static SCHEMA_UNUSED void table_fixed_push( TableFixedCompiler * c, TableFixedEntry e )
{
    /* THE WALK RUNS TWICE and each pass keeps its own half, which is how the
       plan comes out partitioned without a second array to partition it in. */
    if ( ( e.guard != SCHEMA_TABLE_FIXED_NO_GUARD ) != c->want_guarded ) { return; }
    /* EVERY ENTRY IS BOUNDED BY THE WRITER'S OWN RECORD, and this is the read
       side's whole defence: the plan's source offsets are arithmetic over sizes
       a STRANGER wrote, so a layout whose child sizes do not sum to its parent's
       could otherwise name a byte past the record. A layout that does is refused
       WHOLE and never partly compiled (docs/SPEC-TABLES.md §3.4). */
    {
        /* A CLAMP DOES NOT READ THE RECORD: it holds the destination the copy
           already landed. src+size is not a wire reach. */
        if ( e.op != kTableFixedClamp )
        {
            const uint64_t reach = (uint64_t) e.src + (uint64_t) e.size + ( e.op == kTableFixedText ? 4ull : 0ull );
            if ( reach > (uint64_t) c->record ||
                 ( e.guard != SCHEMA_TABLE_FIXED_NO_GUARD && e.guard >= c->record ) )
            {
                c->hostile = 1;
                return;
            }
        }
        else if ( e.guard != SCHEMA_TABLE_FIXED_NO_GUARD && e.guard >= c->record )
        {
            c->hostile = 1;
            return;
        }
    }
    const int32_t room = c->capacity - ( c->pool + (int32_t) sizeof( TableFixedEntry ) - 1 ) / (int32_t) sizeof( TableFixedEntry );
    if ( c->count >= room ) { c->overflow = 1; return; }
    c->plan[c->count++] = e;
}

/* table_fixed_lay_table lays a remap table above the entries and answers its
   byte offset from the plan's base. Tables grow DOWNWARD from the top, so the
   coalescing pass, which only moves entries down, never moves one. */
static SCHEMA_UNUSED uint32_t table_fixed_lay_table( TableFixedCompiler * c, const uint16_t * values, int32_t n )
{
    const int32_t bytes = ( n + 1 ) * (int32_t) sizeof( uint16_t );
    const int32_t total = (int32_t) sizeof( TableFixedEntry ) * c->capacity;
    c->pool += bytes;
    if ( total - c->pool < c->count * (int32_t) sizeof( TableFixedEntry ) ) { c->overflow = 1; return 0; }
    const uint32_t at = (uint32_t) ( total - c->pool );
    uint16_t * slots = (uint16_t *) (void *) ( (uint8_t *) c->plan + at );
    int32_t i;
    slots[0] = (uint16_t) n;
    for ( i = 0; i < n; ++i ) { slots[1 + i] = values[i]; }
    return at;
}

/* table_fixed_lay_bounds lays a compiled clamp's two ends above the entries,
   the same pool the ordinal remap tables use, and answers the byte offset from
   the plan's base. memcpy, not a typed store: the pool is not 8-aligned. */
static SCHEMA_UNUSED uint32_t table_fixed_lay_bounds( TableFixedCompiler * c, uint64_t lo, uint64_t hi )
{
    const int32_t bytes = 16;
    const int32_t total = (int32_t) sizeof( TableFixedEntry ) * c->capacity;
    uint8_t * at;
    c->pool += bytes;
    if ( total - c->pool < c->count * (int32_t) sizeof( TableFixedEntry ) ) { c->overflow = 1; return 0; }
    at = (uint8_t *) (void *) ( (uint8_t *) c->plan + (uint32_t) ( total - c->pool ) );
    memcpy( at, &lo, 8 );
    memcpy( at + 8, &hi, 8 );
    return (uint32_t) ( total - c->pool );
}

/* table_fixed_push_clamp is ONE ranged scalar after the copy or widen that
   landed it. Counted-array elements skip: the live count is a value the plan
   cannot name, and clamping slack would count a clamp on a byte nobody wrote. */
static SCHEMA_UNUSED void table_fixed_push_clamp( TableFixedCompiler * c, const TableFixedDst * d,
                                                  uint32_t at, uint32_t guard, uint8_t arg )
{
    TableFixedEntry e;
    if ( c->skip_clamp || d->width == 0 ) { return; }
    e = table_fixed_entry_zero();
    e.src = 0; e.dst = at; e.size = d->width;
    e.aux = table_fixed_lay_bounds( c, d->lo, d->hi );
    e.guard = guard; e.op = (uint8_t) kTableFixedClamp; e.arg = arg;
    e.meta = (uint8_t) ( d->clamp & 3u );
    e.sign = ( d->clamp & 4u ) ? (uint8_t) 1 : (uint8_t) 0;
    table_fixed_push( c, e );
}

static SCHEMA_UNUSED void table_fixed_compile_entry( TableFixedCompiler * c,
                                                     const TableFixedLayoutView * theirs, int32_t ti, uint32_t their_at,
                                                     const TableFixedLayoutView * mine, int32_t mi, const TableFixedDst * dst, uint32_t my_at,
                                                     uint32_t guard, uint8_t arg );

/* table_fixed_match_children walks a TABLE's children on both sides. */
static SCHEMA_UNUSED void table_fixed_match_children( TableFixedCompiler * c,
                                                      const TableFixedLayoutView * theirs, int32_t ti, uint32_t their_at,
                                                      const TableFixedLayoutView * mine, int32_t mi, const TableFixedDst * dst, uint32_t my_at,
                                                      uint32_t guard, uint8_t arg )
{
    const TableFixedLayoutEntry te = table_fixed_entry_at( theirs, ti );
    const TableFixedLayoutEntry me = table_fixed_entry_at( mine, mi );
    int32_t my_child = mi + 1;
    /* THE LOOP INDICES ARE DECLARED AHEAD OF THEIR LOOPS, which is this
       backend's shape everywhere and not a C99 limitation: j and k are reused
       by the census below, and a redeclaration inside the second pair of loops
       would shadow these two and trip -Wshadow. */
    uint32_t j, k;
    for ( k = 0; k < me.children && my_child < mine->count; ++k )
    {
        const TableFixedLayoutEntry mc = table_fixed_entry_at( mine, my_child );
        int32_t their_child = ti + 1;
        uint32_t their_off = their_at;
        for ( j = 0; j < te.children && their_child < theirs->count; ++j )
        {
            const TableFixedLayoutEntry tc = table_fixed_entry_at( theirs, their_child );
            if ( tc.id == mc.id )
            {
                table_fixed_compile_entry( c, theirs, their_child, their_off, mine, my_child, dst, my_at, guard, arg );
                break;
            }
            their_off += tc.size;
            their_child += table_fixed_subtree( theirs, their_child );
        }
        my_child += table_fixed_subtree( mine, my_child );
    }
    /* EVERY FIELD OF THEIRS I COULD NOT NAME IS ONE unknown */
    int32_t tc_at = ti + 1;
    for ( j = 0; j < te.children && tc_at < theirs->count; ++j )
    {
        const TableFixedLayoutEntry tc = table_fixed_entry_at( theirs, tc_at );
        int named = 0;
        int32_t mc_at = mi + 1;
        for ( k = 0; k < me.children && mc_at < mine->count; ++k )
        {
            if ( table_fixed_entry_at( mine, mc_at ).id == tc.id ) { named = 1; break; }
            mc_at += table_fixed_subtree( mine, mc_at );
        }
        if ( !named && c->report != NULL ) { c->report->unknown++; }
        tc_at += table_fixed_subtree( theirs, tc_at );
    }
}

static SCHEMA_UNUSED void table_fixed_compile_entry( TableFixedCompiler * c,
                                                     const TableFixedLayoutView * theirs, int32_t ti, uint32_t their_at,
                                                     const TableFixedLayoutView * mine, int32_t mi, const TableFixedDst * dst, uint32_t my_at,
                                                     uint32_t guard, uint8_t arg )
{
    const TableFixedLayoutEntry te = table_fixed_entry_at( theirs, ti );
    const TableFixedLayoutEntry me = table_fixed_entry_at( mine, mi );
    const TableFixedDst * d = &dst[mi];
    const uint32_t at = my_at + d->dst;
    const uint32_t aux_at = my_at + d->aux;
    /* THE NESTING A STRANGER'S LAYOUT CAN ASK FOR IS CAPPED. The language's own
       closure is nowhere near this deep, and a wire does not get to pick a
       recursion depth. */
    if ( c->depth > 64 ) { c->hostile = 1; return; }
    /* C++ SPENDS AN RAII GUARD ON THE DECREMENT; C HAS NO DESTRUCTOR, so the
       body is a do/while( 0 ) and the decrement is the single statement after
       it. THE DECREMENT IS ON EVERY PATH — every early exit inside the body is
       a break out of the do/while and not a return, which is the whole reason
       the body is wrapped at all. Adding a return in there would leak a level
       of depth per call and hand the wire back the recursion budget this cap
       exists to take away. */
    c->depth++;
    do
    {
        if ( te.kind != me.kind )
        {
            if ( table_fixed_widens( te.kind, me.kind ) )
            {
                TableFixedEntry e = table_fixed_entry_zero();
                e.src = their_at; e.dst = at; e.size = te.size; e.dstsize = (uint8_t) me.size;
                e.guard = guard; e.arg = arg;
                e.op = ( te.kind == 10 ) ? (uint8_t) kTableFixedWidenF : (uint8_t) kTableFixedWiden;
                e.sign = table_fixed_signed_kind( te.kind ) ? (uint8_t) 1 : (uint8_t) 0;
                table_fixed_push( c, e );
                table_fixed_push_clamp( c, d, at, guard, arg );
                break;
            }
            /* A KIND THAT MOVED IS REPORTED AND NEVER REINTERPRETED (§4). */
            if ( c->report != NULL ) { c->report->kind_mismatch++; }
            break;
        }
        switch ( me.kind )
        {
            case 35: /* the OPTIONAL wrapper: the present byte, then the payload whole */
            {
                TableFixedEntry e = table_fixed_entry_zero();
                e.src = their_at; e.dst = aux_at; e.size = 1; e.guard = guard; e.op = kTableFixedCopy; e.arg = arg;
                table_fixed_push( c, e );
                table_fixed_compile_entry( c, theirs, ti + 1, their_at + 1, mine, mi + 1, dst, my_at, guard, arg );
                break;
            }
            case 13: /* a nested table: match its fields */
            {
                table_fixed_match_children( c, theirs, ti, their_at, mine, mi, dst, at, guard, arg );
                break;
            }
            case 14: /* an array: the count, then min( their bound, my bound ) elements */
            {
                const TableFixedLayoutEntry tel = table_fixed_entry_at( theirs, ti + 1 );
                const TableFixedLayoutEntry mel = table_fixed_entry_at( mine, mi + 1 );
                const uint32_t head = d->counted ? 4u : 0u;
                const uint32_t their_n = tel.size ? ( te.size - head ) / tel.size : 0u;
                const uint32_t my_n = mel.size ? ( me.size - head ) / mel.size : 0u;
                const uint32_t their_base = their_at + head;
                uint32_t n;
                uint32_t i;
                if ( d->counted )
                {
                    TableFixedEntry e = table_fixed_entry_zero();
                    e.src = their_at; e.dst = aux_at; e.size = my_n; e.guard = guard; e.op = kTableFixedCount; e.arg = arg;
                    table_fixed_push( c, e );
                }
                n = their_n < my_n ? their_n : my_n;
                {
                    const int prev_skip = c->skip_clamp;
                    if ( d->counted ) { c->skip_clamp = 1; }
                    for ( i = 0; i < n; ++i )
                    {
                        table_fixed_compile_entry( c, theirs, ti + 1, their_base + i * tel.size,
                                                   mine, mi + 1, dst, at + i * d->stride, guard, arg );
                    }
                    c->skip_clamp = prev_skip;
                }
                break;
            }
            case 16: /* an enum-keyed array: every slot, matched by the KEY's id */
            {
                const TableFixedLayoutEntry tkey = table_fixed_entry_at( theirs, ti + 1 );
                const TableFixedLayoutEntry mkey = table_fixed_entry_at( mine, mi + 1 );
                const int32_t tel = ti + 1 + table_fixed_subtree( theirs, ti + 1 );
                const int32_t mel = mi + 1 + table_fixed_subtree( mine, mi + 1 );
                const TableFixedLayoutEntry tee = table_fixed_entry_at( theirs, tel );
                uint32_t k;
                for ( k = 0; k < mkey.children && mi + 2 + (int32_t) k < mine->count; ++k )
                {
                    const uint64_t key_id = table_fixed_entry_at( mine, mi + 2 + (int32_t) k ).id;
                    uint32_t j;
                    for ( j = 0; j < tkey.children && ti + 2 + (int32_t) j < theirs->count; ++j )
                    {
                        if ( table_fixed_entry_at( theirs, ti + 2 + (int32_t) j ).id != key_id ) { continue; }
                        table_fixed_compile_entry( c, theirs, tel, their_at + j * tee.size,
                                                   mine, mel, dst, at + k * d->stride, guard, arg );
                        break;
                    }
                }
                break;
            }
            case 15: /* a union: the tag, remapped, then each arm matched by id */
            {
                const uint32_t their_tag = table_fixed_tag_bytes( theirs, ti );
                const uint32_t my_tag = table_fixed_tag_bytes( mine, mi );
                int32_t my_arm = mi + 1;
                uint32_t k;
                for ( k = 0; k < me.children && my_arm < mine->count; ++k )
                {
                    const TableFixedLayoutEntry ma = table_fixed_entry_at( mine, my_arm );
                    int32_t their_arm = ti + 1;
                    uint32_t j;
                    for ( j = 0; j < te.children && their_arm < theirs->count; ++j )
                    {
                        const TableFixedLayoutEntry ta = table_fixed_entry_at( theirs, their_arm );
                        if ( ta.id == ma.id )
                        {
                            /* MY tag value, written under THEIR tag's guard */
                            TableFixedEntry tag = table_fixed_entry_zero();
                            tag.src = their_at; tag.dst = aux_at; tag.size = my_tag;
                            tag.aux = k + 1; tag.guard = their_at; tag.op = kTableFixedConst; tag.arg = (uint8_t) ( j + 1 );
                            table_fixed_push( c, tag );
                            table_fixed_compile_entry( c, theirs, their_arm, their_at + their_tag,
                                                       mine, my_arm, dst, at, their_at, (uint8_t) ( j + 1 ) );
                            break;
                        }
                        their_arm += table_fixed_subtree( theirs, their_arm );
                    }
                    my_arm += table_fixed_subtree( mine, my_arm );
                }
                break;
            }
            case 30: /* an enum: the ordinal is the layout's position, so it remaps */
            {
                if ( ( guard != SCHEMA_TABLE_FIXED_NO_GUARD ) != c->want_guarded ) { break; }
                uint16_t remap[256];
                const uint32_t n = te.children < 255u ? te.children : 255u;
                TableFixedEntry e;
                uint32_t j;
                for ( j = 0; j < n; ++j )
                {
                    const uint64_t vid = table_fixed_entry_at( theirs, ti + 1 + (int32_t) j ).id;
                    uint16_t landed = 0;
                    uint32_t k;
                    for ( k = 0; k < me.children && mi + 1 + (int32_t) k < mine->count; ++k )
                    {
                        if ( table_fixed_entry_at( mine, mi + 1 + (int32_t) k ).id == vid ) { landed = (uint16_t) ( k + 1 ); break; }
                    }
                    remap[j] = landed;
                }
                e = table_fixed_entry_zero();
                e.src = their_at; e.dst = at; e.size = te.size; e.guard = guard; e.op = kTableFixedOrdinal;
                e.arg = arg; e.dstsize = (uint8_t) me.size;
                e.aux = table_fixed_lay_table( c, remap, (int32_t) n );
                table_fixed_push( c, e );
                break;
            }
            case 12: case 33: /* text */
            {
                const uint32_t units = ( me.size - 4u ) < ( te.size - 4u ) ? ( me.size - 4u ) : ( te.size - 4u );
                TableFixedEntry e = table_fixed_entry_zero();
                e.src = their_at; e.dst = at; e.size = units; e.aux = aux_at; e.guard = guard;
                e.op = kTableFixedText; e.arg = arg; e.meta = d->meta;
                table_fixed_push( c, e );
                break;
            }
            default:
            {
                TableFixedEntry e = table_fixed_entry_zero();
                e.src = their_at; e.dst = at; e.guard = guard; e.arg = arg;
                if ( te.size == me.size )
                {
                    e.size = me.size; e.op = kTableFixedCopy;
                    table_fixed_push( c, e );
                    table_fixed_push_clamp( c, d, at, guard, arg );
                }
                else if ( te.size < me.size && me.size <= 8 )
                {
                    e.size = te.size; e.dstsize = (uint8_t) me.size; e.op = kTableFixedWiden;
                    table_fixed_push( c, e );
                    table_fixed_push_clamp( c, d, at, guard, arg );
                }
                else if ( c->report != NULL )
                {
                    c->report->kind_mismatch++;
                }
                break;
            }
        }
    } while ( 0 );
    c->depth--;
}

static SCHEMA_UNUSED int32_t table_fixed_compile( const TableFixedLayoutView * theirs,
                                                  const uint8_t * my_layout, int32_t my_layout_bytes,
                                                  const TableFixedDst * dst,
                                                  TableFixedEntry * plan, int32_t plan_capacity, int32_t * guarded,
                                                  TableReport * report )
{
    TableFixedLayoutView mine = table_fixed_layout_view_zero();
    TableFixedCompiler c;
    int32_t plain;
    int32_t out;
    int32_t split;
    int32_t i;
    int why = SCHEMA_TABLE_LAYOUT_MALFORMED;
    if ( plan == NULL || plan_capacity <= 0 ) { return -1; }
    /* MY OWN layout goes through the same rules a stranger's does. It is this
       build's own bytes, so it cannot fail — and saying so in the one place
       both sides go through is what keeps that true. */
    if ( !table_fixed_parse_layout( my_layout, (int64_t) my_layout_bytes, &mine, &why ) ) { return -1; }
    table_fixed_compiler_init( &c );
    c.plan = plan;
    c.capacity = plan_capacity;
    c.report = report;
    c.record = table_fixed_entry_at( theirs, 0 ).size;
    c.want_guarded = 0;
    table_fixed_match_children( &c, theirs, 0, 0, &mine, 0, dst, 0, SCHEMA_TABLE_FIXED_NO_GUARD, 0 );
    plain = c.count;
    c.want_guarded = 1;
    c.report = NULL; /* the unknown census is the first pass's; counting it twice would lie */
    table_fixed_match_children( &c, theirs, 0, 0, &mine, 0, dst, 0, SCHEMA_TABLE_FIXED_NO_GUARD, 0 );
    if ( c.overflow || c.hostile ) { return c.hostile ? -2 : -1; }
    /* COALESCE inside each half, never across the split. THIS IS THE SAME PASS
       THE C++ REFERENCE RUNS AT COMPILE TIME on its identity plan, and running
       it here is what keeps a stranger's plan and this build's own plan the
       same array read by the same loop. */
    out = 0;
    split = 0;
    for ( i = 0; i < c.count; ++i )
    {
        if ( i == plain ) { split = out; }
        if ( out > split && plan[out-1].op == kTableFixedCopy && plan[i].op == kTableFixedCopy &&
             plan[out-1].guard == plan[i].guard && plan[out-1].arg == plan[i].arg &&
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
    return out;
}

/* ---- THE PLAN CACHE: once per peer, never once per record (§3.4) -----------

   THE CALLER OWNS THIS. The codec never allocates: the slots are a named
   table of 64, the plan bytes they point at are a slab the caller hands
   init, and overflow is a miss — compile into the caller's plan buffer and
   do not store, which is Elixir's stance (JS is an unbounded Map). Identity
   hash never consults it. compiles counts successful compiles through this
   cache, which is the pin; made is 1 on a slot after the compile that
   filled it. */

enum { kTableFixedPlanCacheCapacity = 64 };

typedef struct TableFixedPlanCacheSlot
{
    uint64_t hash;
    TableFixedEntry * plan;
    int32_t count;
    int32_t guarded;
    int64_t record_bytes;
    uint8_t made;
} TableFixedPlanCacheSlot;

typedef struct TableFixedPlanCache
{
    TableFixedPlanCacheSlot slots[kTableFixedPlanCacheCapacity];
    TableFixedEntry * storage; /* capacity * stride entries, caller-owned */
    int32_t stride;            /* entries per slot, the plan capacity */
    int32_t used;
    int32_t compiles;
} TableFixedPlanCache;

static SCHEMA_UNUSED void table_fixed_plan_cache_init( TableFixedPlanCache * cache, TableFixedEntry * storage, int32_t stride )
{
    int32_t i;
    cache->storage = storage;
    cache->stride = stride;
    cache->used = 0;
    cache->compiles = 0;
    for ( i = 0; i < kTableFixedPlanCacheCapacity; ++i )
    {
        cache->slots[i].hash = 0;
        cache->slots[i].plan = NULL;
        cache->slots[i].count = 0;
        cache->slots[i].guarded = 0;
        cache->slots[i].record_bytes = 0;
        cache->slots[i].made = 0;
    }
}
`

// tableFixedRuntime128 is the FIXED FORM's 128-bit half, and it is separate for
// one reason: it names serialize.h's storage types, so it may only be emitted
// into a unit that includes serialize.h. tableFixedRuntime above mentions
// serialize_ nowhere, which is what keeps a Table header includable from a
// translation unit that has no packet header at all — this backend's standing
// rule (see the package comment's "Plain byte code with NO serialize
// dependency"). The caller appends this to the primitives block for a unit
// whose closure carries 128-bit storage, and for no other.
//
// It must be appended AFTER tableFixedRuntime, because both functions call
// table_fixed_put64.
const tableFixedRuntime128 = `
/* ---- the 128-bit moves, ONLY where serialize.h is present ------------------

   C++ SPELLS THIS AS ONE TEMPLATE over a builtin 128-bit integer, and C has
   neither the template nor the builtin. This leg's 128-bit storage is
   serialize.h's lo/hi pair of 64-bit halves, and there are exactly two of them,
   so the template becomes TWO NAMED FUNCTIONS: one per storage type, with the
   same body. That is the same trade the rest of this backend makes wherever
   C++ resolves by overload — the C form is the switch or the pair, spelled out,
   resolving no call. */
static SCHEMA_UNUSED void table_fixed_put128_u( uint8_t * b, serialize_uint128_t v )
{
    /* sixteen bytes, the LOW 64-bit half first, which is this wire's order for
       the family everywhere else (docs/SPEC-TABLES.md §3) */
    table_fixed_put64( b, (uint64_t) v.lo );
    table_fixed_put64( b + 8, (uint64_t) v.hi );
}
static SCHEMA_UNUSED void table_fixed_put128_i( uint8_t * b, serialize_int128_t v )
{
    /* the SIGNED twin, and the bytes are the same bytes: a two's complement
       128-bit value's storage image is its unsigned image, low half first, so
       the sign lives in the top bit of the high half and nothing here has to
       know that (docs/SPEC-TABLES.md §3.4's constant-size table). */
    table_fixed_put64( b, (uint64_t) v.lo );
    table_fixed_put64( b + 8, (uint64_t) v.hi );
}

/* ---- the 128-bit COMPARE, for the read-side bounds -------------------------

   The straight-line bounds pass (docs/SPEC-TABLES.md §3.4) compares a value
   against its declared min and max. C++ writes that as < and >, because
   serialize's 128-bit types carry the operators; C's are STRUCTS, so the same
   two lines are these two functions. Each answers -1, 0 or 1, the high half
   deciding first, and the SIGNED twin reads its high half as two's complement
   — which is the whole difference between them. */
static SCHEMA_UNUSED int table_fixed_cmp128_u( serialize_uint128_t a, uint64_t bhi, uint64_t blo )
{
    if ( (uint64_t) a.hi != bhi ) { return (uint64_t) a.hi < bhi ? -1 : 1; }
    if ( (uint64_t) a.lo != blo ) { return (uint64_t) a.lo < blo ? -1 : 1; }
    return 0;
}
static SCHEMA_UNUSED int table_fixed_cmp128_i( serialize_int128_t a, uint64_t bhi, uint64_t blo )
{
    const int64_t ah = (int64_t) a.hi;
    const int64_t bh = (int64_t) bhi;
    if ( ah != bh ) { return ah < bh ? -1 : 1; }
    if ( (uint64_t) a.lo != blo ) { return (uint64_t) a.lo < blo ? -1 : 1; }
    return 0;
}
`
