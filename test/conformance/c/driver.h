/* THE C CONFORMANCE DRIVER's shared surface (test/conformance/README.md).
 *
 * Each unit this driver reads gets a translation unit of its own, which
 * includes only its own headers and hands the surfaces back through the ERASED
 * shapes below; main.c includes no generated header at all. That keeps the
 * matrix one row per unit. It is no longer forced: every name a C unit
 * declares carries its package (SPEC §6.3), so units link together and their
 * headers may share a translation unit too (test/c-two-units). */

#ifndef SCHEMA_CONFORMANCE_DRIVER_H
#define SCHEMA_CONFORMANCE_DRIVER_H

#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

/* every helper here is used by SOME translation unit of this driver and not by
   all of them, which is the shape a shared header has. */
#if defined(__GNUC__) || defined(__clang__)
#define SCHEMA_CONFORMANCE_UNUSED __attribute__((unused))
#else
#define SCHEMA_CONFORMANCE_UNUSED
#endif

/* Each unit declares its OWN report type (<package>_table_report_t), so the
 * driver carries one report shape of its own and each row copies into it.
 * The verdict is the whole contract; the counters are local diagnostics. */
typedef struct ConformanceReport
{
    int unknown, refused, widened, retained, retain_lost;
    int blob_shared; /* §2.5: extra namings of one blob, which every writer refuses */
} ConformanceReport;

/* One row per (unit, root) the corpus names. Every row is the SAME six
 * function pointers, so a surface is one loop over the table and not a switch
 * per root. */
typedef struct ConformanceCodec
{
    const char * unit;
    const char * root;
    int64_t (*message_fuzz)(const uint8_t * wire,int64_t bytes,uint8_t ** saved,ConformanceReport * report,int * loaded,int64_t * extent);
    int (*message)(const uint8_t * announcement,int64_t announcement_bytes,const uint8_t * wire,int64_t bytes,uint8_t ** answer,int64_t * size,ConformanceReport * report);
    int ( *load )( void * value, const uint8_t * bytes, int64_t size, ConformanceReport * report );
    int64_t ( *measure )( const void * value );
    int64_t ( *save )( const void * value, uint8_t * buffer, int64_t capacity );
    int ( *from_json )( void * value, const char * text, int64_t bytes, ConformanceReport * report );
    int64_t ( *to_json )( const void * value, char * buffer, int64_t capacity );
    void * ( *storage )( void ); /* one static instance per root, reset by load */
    int64_t (*load_measure)(const uint8_t * wire, int64_t bytes);
    int64_t (*cook_measure)(const void * value);
    int (*cook)(const void * value,void * buffer,uint64_t capacity,int big);
    int64_t (*retain_fuzz)(const uint8_t *,int64_t,uint8_t **,ConformanceReport *,int *,int64_t *);
    /* puts a json-write-refusal line's damage into a loaded fixed root (FORMAT.md,
       json-write-refusal): 0 when the root has no such text field, NULL for a
       variable root */
    int (*damage)(void * value,const char * field,int wide);
    /* THE VARIABLE-FORM READ of a fixed root, which the framing rows use: a
       framing vector is read as form 1 and written back as the root's own
       form (docs/rulings.json fixed-root-refuses-form1). NULL: `load`. */
    int ( *load_variable )( void * value, const uint8_t * bytes, int64_t size, ConformanceReport * report );
    /* A COOK RECIPE'S SHARED BLOB (FORMAT.md, `cook-write ... <source> <alias>`):
       point the alias slot of a constructed graph root at the source slot's
       node, the in-memory sharing a cook packs as it finds it and no wire read
       delivers (docs/SPEC-TABLES.md §2.5). NULL for a root with no node slots. */
    int ( *share )( void * value, const char * source, const char * alias );
    /* THE VARIABLE-FORM WRITE beside load_variable, so the soak still drives the
       variable writer whose byte order the two endian controls sabotage. NULL:
       `measure` / `save`. */
    int64_t ( *measure_variable )( const void * value );
    int64_t ( *save_variable )( const void * value, uint8_t * buffer, int64_t capacity );
    /* A MESSAGE-BATCH ROW (FORMAT.md, `message-batch`): the count load_messages
       returned into storage for 256 bodies, -1 for a refused announcement.
       NULL for a variable root, which has no message form. */
    int64_t ( *message_count )( const uint8_t * announcement, int64_t announcement_bytes, const uint8_t * wire, int64_t bytes );
} ConformanceCodec;

/* A GROWING TEXT, for the two dumps. The harness compares bytes, so nothing
 * here formats anything a reader has to parse back. */
typedef struct ConformanceText
{
    char * data;
    size_t length;
    size_t capacity;
} ConformanceText;

SCHEMA_CONFORMANCE_UNUSED static void conformance_text_raw( ConformanceText * out, const char * data, size_t bytes )
{
    if ( out->length + bytes + 1 > out->capacity )
    {
        size_t want = ( out->capacity ? out->capacity : 4096 );
        while ( want < out->length + bytes + 1 ) { want *= 2; }
        out->data = (char *) realloc( out->data, want );
        if ( out->data == NULL ) { abort(); }
        out->capacity = want;
    }
    memcpy( out->data + out->length, data, bytes );
    out->length += bytes;
    out->data[out->length] = 0;
}

SCHEMA_CONFORMANCE_UNUSED static void conformance_text_add( ConformanceText * out, const char * s )
{
    conformance_text_raw( out, s, strlen( s ) );
}

/* THE WRITE CONTRACT's batteries (docs/SPEC.md §5.1): `write-refusal` and the
 * three surfaces beside it, one writer each. Each unit a row names carries a
 * SETTER, which sets the one field and answers 0 for a (root, path, kind) it
 * does not know, and a WRITER, which writes the value through the fixed form's
 * FixedSave or the message form's SaveMessages as a batch of one and answers 0
 * for a root it does not know; the case runner forks, so an assert's abort is
 * an answer and not the end of the surface. */
#define CONFORMANCE_WRITE_VARIABLE 0
#define CONFORMANCE_WRITE_FIXED 1
#define CONFORMANCE_WRITE_MESSAGE 2
#define CONFORMANCE_WRITE_JSON 3
int conformance_write_refusal_set_tabledemo( const char * root, void * value, const char * path,
                                             const char * kind, const char * text );
int conformance_write_refusal_write_tabledemo( int surface, const char * root, const void * value, const uint8_t * announcement,
                                               int64_t announcement_bytes, uint8_t * buffer, int64_t capacity, int64_t * wrote );
int conformance_write_refusal_set_backenddemo( const char * root, void * value, const char * path,
                                               const char * kind, const char * text );
int conformance_write_refusal_write_backenddemo( int surface, const char * root, const void * value, const uint8_t * announcement,
                                                 int64_t announcement_bytes, uint8_t * buffer, int64_t capacity, int64_t * wrote );
/* a narrow text's storage by `length` (filled with `a`) or `bytes` (exact), and
   a wide text's by `units`: 0 for a kind or a value the storage cannot take */
int conformance_write_refusal_set_scalars( const char * root, void * value, const char * path,
                                           const char * kind, const char * text );
int conformance_write_refusal_write_scalars( int surface, const char * root, const void * value, const uint8_t * announcement,
                                             int64_t announcement_bytes, uint8_t * buffer, int64_t capacity, int64_t * wrote );
int conformance_write_refusal_set_cooktorture( const char * root, void * value, const char * path,
                                               const char * kind, const char * text );
int conformance_write_refusal_write_cooktorture( int surface, const char * root, const void * value, const uint8_t * announcement,
                                                 int64_t announcement_bytes, uint8_t * buffer, int64_t capacity, int64_t * wrote );
int conformance_write_refusal_set_tblvarevo3( const char * root, void * value, const char * path,
                                              const char * kind, const char * text );
int conformance_write_refusal_write_tblvarevo3( int surface, const char * root, const void * value, const uint8_t * announcement,
                                                int64_t announcement_bytes, uint8_t * buffer, int64_t capacity, int64_t * wrote );
int conformance_write_refusal_text( char * storage, int32_t capacity, int32_t * length, const char * kind, const char * text );
int conformance_write_refusal_bytes( uint8_t * storage, int32_t capacity, int32_t * length, const char * kind, const char * text );
int conformance_write_refusal_units( uint16_t * storage, int32_t capacity, int32_t * length, const char * kind, const char * text );
#define CONFORMANCE_WROTE 1
#define CONFORMANCE_REFUSED 2
#define CONFORMANCE_NO_SETTER 3
#define CONFORMANCE_BROKEN 4
int conformance_write_refusal_case( int surface, const ConformanceCodec * codec, const char * unit, const uint8_t * wire, int64_t bytes,
                                    const uint8_t * announcement, int64_t announcement_bytes,
                                    const char * root, const char * path, const char * kind, const char * text );

int conformance_retain(const uint8_t * wire,int64_t bytes,int short_buffer,int64_t id_capacity,int * counters,uint8_t ** output,int64_t * output_bytes,char ** text,int64_t * text_bytes);
int conformance_retain_varevo1(const uint8_t * wire,int64_t bytes,int short_buffer,int64_t id_capacity,int * counters,uint8_t ** output,int64_t * output_bytes,char ** text,int64_t * text_bytes);
int conformance_retain_varevo2(const uint8_t * wire,int64_t bytes,int short_buffer,int64_t id_capacity,int * counters,uint8_t ** output,int64_t * output_bytes,char ** text,int64_t * text_bytes);
int conformance_torture_block_open_ex(void * data,int64_t bytes,int * reason,const uint8_t ** out_base,const void ** out_type);

/* the per-unit entry points, each defined in that unit's own translation unit */
const ConformanceCodec * conformance_codecs_armdemo(int * count);
const ConformanceCodec * conformance_codecs_mapdemo(int * count);
const ConformanceCodec * conformance_codecs_listdemo(int * count);
const ConformanceCodec * conformance_codecs_streamdemo(int * count);
const ConformanceCodec * conformance_codecs_tblw1(int * count);
const ConformanceCodec * conformance_codecs_tblw2(int * count);
const ConformanceCodec * conformance_codecs_tblp2(int * count);
const ConformanceCodec * conformance_codecs_tblg1(int * count);
const ConformanceCodec * conformance_codecs_blobdemo( int * count );
const ConformanceCodec * conformance_codecs_messagedemo( int * count );
const ConformanceCodec * conformance_codecs_tblm1( int * count );
const ConformanceCodec * conformance_codecs_tblm2( int * count );
const ConformanceCodec * conformance_codecs_tbla1( int * count );
const ConformanceCodec * conformance_codecs_tbla2( int * count );
const ConformanceCodec * conformance_codecs_tblk1( int * count );
const ConformanceCodec * conformance_codecs_tblk2( int * count );
const ConformanceCodec * conformance_codecs_tblr1( int * count );
const ConformanceCodec * conformance_codecs_tblr2( int * count );
const ConformanceCodec * conformance_codecs_backenddemo( int * count );
const ConformanceCodec * conformance_codecs_vocabdemo( int * count );
const ConformanceCodec * conformance_codecs_vocab9demo( int * count );
const ConformanceCodec * conformance_codecs_tabledemo( int * count );
const ConformanceCodec * conformance_codecs_graphdemo( int * count );
const ConformanceCodec * conformance_codecs_tblv1( int * count );
const ConformanceCodec * conformance_codecs_tblv2( int * count );
const ConformanceCodec * conformance_codecs_tblp1( int * count );
const ConformanceCodec * conformance_codecs_tblp3( int * count );
const ConformanceCodec * conformance_codecs_scalars( int * count );
const ConformanceCodec * conformance_codecs_tblscalars2( int * count );
const ConformanceCodec * conformance_codecs_tblfixedevo( int * count );
const ConformanceCodec * conformance_codecs_widelist( int * count );
const ConformanceCodec * conformance_codecs_featurewitnesses( int * count );
const ConformanceCodec * conformance_codecs_tblvarevo1( int * count );
const ConformanceCodec * conformance_codecs_tblvarevo2( int * count );
const ConformanceCodec * conformance_codecs_tblvarevo3( int * count );
const ConformanceCodec * conformance_codecs_cooktorture( int * count );
const ConformanceCodec * conformance_codecs_cooktorturewide( int * count );

/* the BLOCK unit (blockdemo): open one image at the extent and pointer the
 * caller claims, and read every row out of the descriptors */
int conformance_block_open( const char * name, const uint8_t * data, size_t bytes, int64_t extent, int pointer );
int conformance_block_dump( const char * name, const uint8_t * data, size_t bytes, ConformanceText * out );

/* the COOK unit (graphdemo): the canonical node dump, and the forgery
 * battery's one question.
 *
 * conformance_quiet silences the walk's own refusal messages. A driver run
 * wants them — a walk that refused is a case that failed. The FUZZER does not:
 * a forged file whose walk refuses is the CORRECT outcome, tens of thousands of
 * times, and the noise would bury the one message that matters. It is a flag
 * rather than a redirect because the sanitizers write to stderr too. */
extern int conformance_quiet;
int conformance_cook_open_reason(const char * root,const uint8_t * data,size_t bytes,int64_t extent,int pointer,int * reason);
int conformance_block_open_reason(const char * name,const uint8_t * data,size_t bytes,int64_t extent,int pointer,int * reason);
int conformance_cook_dump( const char * root, const uint8_t * data, size_t bytes, ConformanceText * out );
int conformance_cook_open( const char * root, const uint8_t * data, size_t bytes, int64_t extent, int pointer );

/* THE BUFFER A CALLER HOLDS. `extent` is the length the caller CLAIMS, which
 * may run past the bytes the file carries or short of them; `pointer` is where
 * its base sits — 0 an aligned base, 1..63 that many bytes past one, and a
 * negative pointer means no buffer at all. A driver allocates EXACTLY the
 * claim, copies what fits and zeroes the rest, so a reader that walks past what
 * it was given walks into a sanitizer's redzone rather than into a neighbour. */
typedef struct ConformanceBuffer
{
    uint8_t * allocation;
    uint8_t * base;
    int64_t bytes;
} ConformanceBuffer;

SCHEMA_CONFORMANCE_UNUSED static int conformance_buffer_create( ConformanceBuffer * buffer, const uint8_t * data, size_t bytes,
                                      int64_t extent, int pointer )
{
    size_t copy;
    buffer->allocation = NULL;
    buffer->base = NULL;
    buffer->bytes = extent < 0 ? (int64_t) bytes : extent;
    if ( pointer < 0 ) { buffer->bytes = 0; return 1; } /* no buffer at all */
    buffer->allocation = (uint8_t *) malloc( (size_t) ( buffer->bytes > 0 ? buffer->bytes : 1 ) + 64 + 64 );
    if ( buffer->allocation == NULL ) { return 0; }
    buffer->base = (uint8_t *) ( ( (uintptr_t) buffer->allocation + 63 ) & ~(uintptr_t) 63 );
    buffer->base += pointer;
    memset( buffer->base, 0, (size_t) buffer->bytes );
    copy = bytes < (size_t) buffer->bytes ? bytes : (size_t) buffer->bytes;
    memcpy( buffer->base, data, copy );
    return 1;
}

SCHEMA_CONFORMANCE_UNUSED static void conformance_buffer_destroy( ConformanceBuffer * buffer )
{
    free( buffer->allocation );
    buffer->allocation = NULL;
    buffer->base = NULL;
}


/* THE 2026-11-22 SURFACES (test/conformance/README.md, "The 2026-11-22
 * surfaces ask what no surface asked before"). The two writers beside the four
 * above: the COOK writer is every codec's own cook_measure / cook, and the
 * BLOCK writer is a unit's, through its `write`. A setter answers
 * CONFORMANCE_SET_UNREPRESENTABLE for a value this leg's STORAGE cannot hold
 * (a native [N]T holds exactly N elements, an int8_t holds no 128), which the
 * row answers `unrepresentable` and never `refused` on the runtime's behalf. */
#define CONFORMANCE_WRITE_COOK 4
#define CONFORMANCE_WRITE_BLOCK 5
#define CONFORMANCE_SET_UNREPRESENTABLE 2
#define CONFORMANCE_UNREPRESENTABLE 5
#define CONFORMANCE_DIED 6     /* the writer's child died by a signal that is no assert: unanswered, and named */
#define CONFORMANCE_ROW_WIRE 7 /* the row's own wire did not load: a corpus fault, never a verdict */
/* an `int` row's value against [min, max]: 1 set into *out, 0 for a value that
   does not parse, CONFORMANCE_SET_UNREPRESENTABLE for one past the storage */
int conformance_write_refusal_int( const char * text, long long min, long long max, long long * out );
/* an `elems` row against a NATIVE array of `storage` elements: the declared
   count leaves it as loaded (1), and any other count is unrepresentable */
int conformance_write_refusal_elems( const char * text, long long storage );
int conformance_write_refusal_set_tblsurf( const char * root, void * value, const char * path,
                                           const char * kind, const char * text );
int conformance_write_refusal_write_tblsurf( int surface, const char * root, const void * value, const uint8_t * announcement,
                                             int64_t announcement_bytes, uint8_t * buffer, int64_t capacity, int64_t * wrote );
int conformance_write_refusal_set_mapdemo( const char * root, void * value, const char * path,
                                           const char * kind, const char * text );
int conformance_write_refusal_set_graphdemo( const char * root, void * value, const char * path,
                                             const char * kind, const char * text );
/* a unit no fixed, message or block row names writes through no unit writer */
int conformance_write_refusal_write_none( int surface, const char * root, const void * value, const uint8_t * announcement,
                                          int64_t announcement_bytes, uint8_t * buffer, int64_t capacity, int64_t * wrote );

/* ONE UNIT's announcement, send and read-into, for the `announce` and
 * `read-into` surfaces. Every pointer calls the unit's REAL generated runtime;
 * NULL where the unit is named by no such row. */
typedef struct ConformanceUnit1122
{
    const char * unit;
    int64_t ( *announce_measure )( void );
    int64_t ( *announce )( uint8_t * buffer, int64_t capacity );
    /* AnnounceRead into a fresh vocabulary whose strict flag is `strict` */
    int ( *announce_read )( const uint8_t * announcement, int64_t bytes, int strict );
    /* SaveMessages of one, with no announcement read or written on the
       connection: 1 wrote (*out, *size), 0 refused, -1 no such root */
    int ( *send )( const char * root, const void * value, uint8_t ** out, int64_t * size );
    /* one report and one set of values through the lawful read, the damaged
       read, the text of every value, and the lawful read again: 0 answered,
       1 no such root or form, 2 unanswered (named on stderr) */
    int ( *read_into )( const char * root, const char * form, const uint8_t * lawful, int64_t lawful_bytes,
                        const uint8_t * damaged, int64_t damaged_bytes, int64_t count,
                        const uint8_t * announcement, int64_t announcement_bytes, ConformanceText * answer );
} ConformanceUnit1122;
const ConformanceUnit1122 * conformance_unit1122_tblsurf( void );
const ConformanceUnit1122 * conformance_unit1122_tblann4096( void );
const ConformanceUnit1122 * conformance_unit1122_tblann4097( void );
const ConformanceUnit1122 * conformance_unit1122_messagedemo( void );
const ConformanceUnit1122 * conformance_unit1122_backenddemo( void );
const ConformanceCodec * conformance_codecs_tblsurf( int * count );

#endif /* SCHEMA_CONFORMANCE_DRIVER_H */
