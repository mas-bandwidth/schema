#include "CaptionTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE wide
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER WIDE
#include "unit.h"
SCHEMA_CONFORMANCE_CODEC( Caption, caption )
SCHEMA_CONFORMANCE_CODEC( Stamp, stamp )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( widedemo, Caption ),
    SCHEMA_CONFORMANCE_ROW( widedemo, Stamp )
};
/* Sets the one field a write-contract row names (driver.h). */
int conformance_write_refusal_set_widedemo( const char * root, void * value, const char * path,
                                            const char * kind, const char * text )
{
    if ( strcmp( root, "Caption" ) == 0 && strcmp( path, "title" ) == 0 )
    {
        wide_caption_t * v = (wide_caption_t *) value;
        return conformance_write_refusal_units( v->title, (int32_t) ( sizeof( v->title ) / sizeof( v->title[0] ) ) - 1, &v->title_length, kind, text );
    }
    return 0;
}

/* The fixed form's FixedSave, a batch of one; widedemo rides no message row. */
int conformance_write_refusal_write_widedemo( int surface, const char * root, const void * value, const uint8_t * announcement,
                                              int64_t announcement_bytes, uint8_t * buffer, int64_t capacity, int64_t * wrote )
{
    (void) announcement; (void) announcement_bytes;
    if ( surface != CONFORMANCE_WRITE_FIXED || strcmp( root, "Caption" ) != 0 ) { return 0; }
    *wrote = wide_caption_fixed_save( (const wide_caption_t *) value, 1, buffer, capacity );
    return 1;
}

const ConformanceCodec * conformance_codecs_widedemo( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}
