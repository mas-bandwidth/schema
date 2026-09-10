package ctable

// tableFixedRuntime is the FIXED FORM's package-scoped runtime
// (docs/SPEC-TABLES.md §3.4): the plan entry, the ONE read loop, the block
// reader and the plan compiler. It rides inside the same package guard as the
// rest of the primitives, so whichever <Base>Table.h a translation unit
// includes first defines it for the whole unit.
//
// EVERYTHING HERE IS ALLOCATION-FREE, which is this library's rule everywhere
// else and has no exception on this form: the plan's storage is the caller's,
// declared by capacity, and a block whose plan does not fit is a refusal by
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
// compiler — the one a STRANGER's block needs, which no generator can run
// ahead of time.
//
// @INLINE@ is textually replaced with the per-package force-inline macro
// (tableInlineMacro), exactly as the file wire's runtime does it.
const tableFixedRuntime = `
/* ---------------------------------------------------------------------------
   THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4)

   THE SYMBOLS SAY "VOCAB" WHERE §3.4 SAYS "VOCABULARY BLOCK", and the reason is
   that this codebase already spent the word BLOCK on §19's block form, whose
   zero-cost gate holds that not one Block symbol appears in a Table source. The
   spec keeps the owner's own name for the structure; the symbols get out of the
   way of a gate that was there first.

   THE TYPES KEEP THE REFERENCE'S SPELLING AND THE FUNCTIONS DO NOT.
   TableFixedEntry is TableFixedEntry in both legs, because a type a generated
   header names in a declaration should read the same in every port; the
   functions are snake_case behind table_fixed_, because that is how
   table_writer_put32 and table_reader_get32 read three hundred lines above
   this one and a C header has one namespace to be consistent inside.
   --------------------------------------------------------------------------- */

enum { kTableFixedForm = 3 };

/* THE OPS ARE THE WHOLE SET. The IDENTITY plan carries only the first three;
   the other two are what a plan compiled from another writer's block adds.

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
    kTableFixedWidenF  = 6  /* f32 into f64, §4's float rung */
};

/* arg on a kTableFixedText entry */
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
/* THE FIXED FORM'S THREE. Each is a refusal by name with nothing decoded and no
   counter moved, on the form byte's own precedent (§3.4). */
enum
{
    SCHEMA_TABLE_NO_BLOCK = 7,        /* a record whose hash names no vocabulary block this reader holds */
    SCHEMA_TABLE_BLOCK_MALFORMED = 8, /* bytes handed to the form as a block that are not one: the count overruns, or the tree does not close */
    SCHEMA_TABLE_PLAN_TOO_LARGE = 9   /* a block whose compiled plan does not fit the plan storage the caller declared: this codec never allocates */
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
    uint8_t arg;
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

/* THE HASH is fnv1a64 over the block's bytes exactly as written (§3.4). */
static SCHEMA_UNUSED uint64_t table_fixed_hash_of( const uint8_t * block, int64_t bytes )
{
    uint64_t h = 0xcbf29ce484222325ull;
    int64_t i;
    for ( i = 0; i < bytes; ++i )
    {
        h ^= (uint64_t) block[i];
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
   (test/bench/fixedform_measure.cpp). */
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
    if ( n <= 64 )
    {
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
            const uint32_t unit = ( p->arg == kTableFixedTextWide ) ? 2u : 1u;
            const uint32_t cap = p->size / unit;
            int32_t v = (int32_t) table_fixed_get32( src + p->src );
            if ( v < 0 ) { v = 0; (*clamped)++; }
            else if ( (uint32_t) v > cap ) { v = (int32_t) cap; (*clamped)++; }
            memcpy( dst + p->dst, &v, 4 );
            table_fixed_copy_run( dst + p->aux, src + p->src + 4, p->size );
            if ( p->arg != kTableFixedTextBytes )
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
            /* A VARIANT ORDINAL IS ITS POSITION IN THE BLOCK, so a writer whose
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

/* ---- THE BLOCK -------------------------------------------------------------

   An entry is SEVENTEEN BYTES and the block is a count and a run of them. THE
   FORMAT NEVER MOVES, which is what lets a reader of this major parse a later
   major's block and step over a kind it does not know by the size the entry
   states. */

enum { kTableFixedEntryBytes = 17 };

typedef struct TableFixedVocabEntry
{
    uint64_t id;
    uint32_t size;
    uint32_t children;
    uint8_t kind;
} TableFixedVocabEntry;

/* The C++ struct's own defaults, again as a function: C has none of its own. */
static SCHEMA_UNUSED TableFixedVocabEntry table_fixed_vocab_entry_zero( void )
{
    TableFixedVocabEntry out;
    memset( &out, 0, sizeof( out ) );
    return out;
}

typedef struct TableFixedVocab
{
    const uint8_t * bytes;
    int32_t count;
} TableFixedVocab;

/* bytes IS SET TO NULL AND NOT LEFT TO THE memset, because a null pointer is
   not required to be all-zero bits and the whole block reader tests it by name. */
static SCHEMA_UNUSED TableFixedVocab table_fixed_vocab_zero( void )
{
    TableFixedVocab out;
    memset( &out, 0, sizeof( out ) );
    out.bytes = NULL;
    out.count = 0;
    return out;
}

/* AN INDEX PAST THE ENTRIES IS ANSWERED, NOT READ. Every index into a block
   is arithmetic over child counts a STRANGER wrote, so the one place that can
   hold the whole walk inside the buffer is the one place that touches it. An
   out-of-range index answers kind 0xFF, which is a kind no declaration has and
   nothing matches, so the walk that asked for it finds nothing and moves on. */
static SCHEMA_UNUSED TableFixedVocabEntry table_fixed_entry_at( const TableFixedVocab * b, int32_t i )
{
    TableFixedVocabEntry out = table_fixed_vocab_entry_zero();
    if ( b->bytes == NULL || i < 0 || i >= b->count ) { out.kind = 0xFFu; return out; }
    const uint8_t * e = b->bytes + 4 + (int64_t) i * kTableFixedEntryBytes;
    out.id = table_fixed_get64( e );
    out.kind = e[8];
    out.size = table_fixed_get32( e + 9 );
    out.children = table_fixed_get32( e + 13 );
    return out;
}

/* table_fixed_subtree is how many entries the subtree rooted at i occupies, so
   a walk steps over a child it does not want without knowing what is in it. */
static SCHEMA_UNUSED int32_t table_fixed_subtree( const TableFixedVocab * b, int32_t i )
{
    /* ITERATIVE ON PURPOSE. A block is a stranger's bytes, so a chain of
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

static SCHEMA_UNUSED uint32_t table_fixed_union_arm_bytes( const TableFixedVocab * b, int32_t i )
{
    const TableFixedVocabEntry e = table_fixed_entry_at( b, i );
    uint32_t widest = 0;
    int32_t at = i + 1;
    uint32_t k;
    for ( k = 0; k < e.children && at < b->count; ++k )
    {
        const TableFixedVocabEntry a = table_fixed_entry_at( b, at );
        if ( a.size > widest ) { widest = a.size; }
        at += table_fixed_subtree( b, at );
    }
    return widest;
}

static SCHEMA_UNUSED uint32_t table_fixed_tag_bytes( const TableFixedVocab * b, int32_t i )
{
    return table_fixed_entry_at( b, i ).size - table_fixed_union_arm_bytes( b, i );
}

/* C++ FILLS AN out REFERENCE AND ANSWERS bool; C fills an out POINTER and
   answers 0 or 1. Everything else, including the refusal that leaves out empty,
   is the same. */
static SCHEMA_UNUSED int table_fixed_vocab_read( const uint8_t * bytes, int64_t length, TableFixedVocab * out )
{
    if ( bytes == NULL || length < 4 ) { return 0; }
    const uint32_t count = table_fixed_get32( bytes );
    if ( count == 0 || (int64_t) count * kTableFixedEntryBytes + 4 != length ) { return 0; }
    out->bytes = bytes;
    out->count = (int32_t) count;
    /* THE TREE HAS TO CLOSE: the root's subtree is the whole block */
    if ( table_fixed_subtree( out, 0 ) != out->count ) { out->bytes = NULL; out->count = 0; return 0; }
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

/* TableFixedDst is MY side of the walk: one row per entry of my own block,
   carrying the storage facts a block entry cannot. The generator emits it as a
   static const array beside the block, so it needs no zeroing helper of its
   own — nothing in this runtime ever declares one. */
typedef struct TableFixedDst
{
    uint32_t dst;    /* this entry's storage offset inside its parent */
    uint32_t stride; /* an array entry's storage stride */
    uint32_t aux;    /* a text field's buffer offset */
    uint8_t counted; /* an array that carries a live count */
    uint8_t arg;     /* a text field's flavour */
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
    TableReport * report;
} TableFixedCompiler;

/* The C++ struct's defaults once more, and here they are a whole-struct zero
   plus the two pointers named, for the reason table_fixed_vocab_zero gives. */
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
       a STRANGER wrote, so a block whose child sizes do not sum to its parent's
       could otherwise name a byte past the record. A block that does is refused
       WHOLE and never partly compiled (docs/SPEC-TABLES.md §3.4). */
    {
        const uint64_t reach = (uint64_t) e.src + (uint64_t) e.size + ( e.op == kTableFixedText ? 4ull : 0ull );
        if ( reach > (uint64_t) c->record ||
             ( e.guard != SCHEMA_TABLE_FIXED_NO_GUARD && e.guard >= c->record ) )
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

static SCHEMA_UNUSED void table_fixed_compile_entry( TableFixedCompiler * c,
                                                     const TableFixedVocab * theirs, int32_t ti, uint32_t their_at,
                                                     const TableFixedVocab * mine, int32_t mi, const TableFixedDst * dst, uint32_t my_at,
                                                     uint32_t guard, uint8_t arg );

/* table_fixed_match_children walks a TABLE's children on both sides. */
static SCHEMA_UNUSED void table_fixed_match_children( TableFixedCompiler * c,
                                                      const TableFixedVocab * theirs, int32_t ti, uint32_t their_at,
                                                      const TableFixedVocab * mine, int32_t mi, const TableFixedDst * dst, uint32_t my_at,
                                                      uint32_t guard, uint8_t arg )
{
    const TableFixedVocabEntry te = table_fixed_entry_at( theirs, ti );
    const TableFixedVocabEntry me = table_fixed_entry_at( mine, mi );
    int32_t my_child = mi + 1;
    /* THE LOOP INDICES ARE DECLARED AHEAD OF THEIR LOOPS, which is this
       backend's shape everywhere and not a C99 limitation: j and k are reused
       by the census below, and a redeclaration inside the second pair of loops
       would shadow these two and trip -Wshadow. */
    uint32_t j, k;
    for ( k = 0; k < me.children && my_child < mine->count; ++k )
    {
        const TableFixedVocabEntry mc = table_fixed_entry_at( mine, my_child );
        int32_t their_child = ti + 1;
        uint32_t their_off = their_at;
        for ( j = 0; j < te.children && their_child < theirs->count; ++j )
        {
            const TableFixedVocabEntry tc = table_fixed_entry_at( theirs, their_child );
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
        const TableFixedVocabEntry tc = table_fixed_entry_at( theirs, tc_at );
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
                                                     const TableFixedVocab * theirs, int32_t ti, uint32_t their_at,
                                                     const TableFixedVocab * mine, int32_t mi, const TableFixedDst * dst, uint32_t my_at,
                                                     uint32_t guard, uint8_t arg )
{
    const TableFixedVocabEntry te = table_fixed_entry_at( theirs, ti );
    const TableFixedVocabEntry me = table_fixed_entry_at( mine, mi );
    const TableFixedDst * d = &dst[mi];
    const uint32_t at = my_at + d->dst;
    const uint32_t aux_at = my_at + d->aux;
    /* THE NESTING A STRANGER'S BLOCK CAN ASK FOR IS CAPPED. The language's own
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
                const TableFixedVocabEntry tel = table_fixed_entry_at( theirs, ti + 1 );
                const TableFixedVocabEntry mel = table_fixed_entry_at( mine, mi + 1 );
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
                for ( i = 0; i < n; ++i )
                {
                    table_fixed_compile_entry( c, theirs, ti + 1, their_base + i * tel.size,
                                               mine, mi + 1, dst, at + i * d->stride, guard, arg );
                }
                break;
            }
            case 16: /* an enum-keyed array: every slot, matched by the KEY's id */
            {
                const TableFixedVocabEntry tkey = table_fixed_entry_at( theirs, ti + 1 );
                const TableFixedVocabEntry mkey = table_fixed_entry_at( mine, mi + 1 );
                const int32_t tel = ti + 1 + table_fixed_subtree( theirs, ti + 1 );
                const int32_t mel = mi + 1 + table_fixed_subtree( mine, mi + 1 );
                const TableFixedVocabEntry tee = table_fixed_entry_at( theirs, tel );
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
                    const TableFixedVocabEntry ma = table_fixed_entry_at( mine, my_arm );
                    int32_t their_arm = ti + 1;
                    uint32_t j;
                    for ( j = 0; j < te.children && their_arm < theirs->count; ++j )
                    {
                        const TableFixedVocabEntry ta = table_fixed_entry_at( theirs, their_arm );
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
            case 30: /* an enum: the ordinal is the block's position, so it remaps */
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
                e.op = kTableFixedText; e.arg = d->arg;
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
                }
                else if ( te.size < me.size && me.size <= 8 )
                {
                    e.size = te.size; e.dstsize = (uint8_t) me.size; e.op = kTableFixedWiden;
                    table_fixed_push( c, e );
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

static SCHEMA_UNUSED int32_t table_fixed_compile( const TableFixedVocab * theirs,
                                                  const uint8_t * my_block, int32_t my_block_bytes,
                                                  const TableFixedDst * dst,
                                                  TableFixedEntry * plan, int32_t plan_capacity, int32_t * guarded,
                                                  TableReport * report )
{
    TableFixedVocab mine = table_fixed_vocab_zero();
    TableFixedCompiler c;
    int32_t plain;
    int32_t out;
    int32_t split;
    int32_t i;
    if ( plan == NULL || plan_capacity <= 0 ) { return -1; }
    if ( !table_fixed_vocab_read( my_block, my_block_bytes, &mine ) ) { return -1; }
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
`
