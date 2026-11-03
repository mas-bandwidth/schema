/* the tabledemo unit's codecs (tables/examples). */
#include "TablesTable.h"
#include "WideTable.h"
#include "NestedTable.h"
#include "KeyedTable.h"
#include "PackTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE tabledemo
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TABLEDEMO
#include "unit.h"

#include <math.h> /* NAN and INFINITY, for the write-refusal setter */

SCHEMA_CONFORMANCE_CODEC( RootConfig, root_config )
SCHEMA_CONFORMANCE_CODEC( ProfileConfig, profile_config )
SCHEMA_CONFORMANCE_CODEC( LoadoutConfig, loadout_config )
SCHEMA_CONFORMANCE_CODEC( WideBlob, wide_blob )
SCHEMA_CONFORMANCE_CODEC( ArchiveConfig, archive_config )
SCHEMA_CONFORMANCE_CODEC( KeyedConfig, keyed_config )
SCHEMA_CONFORMANCE_CODEC( PackConfig, pack_config )

static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tabledemo, RootConfig ),
    SCHEMA_CONFORMANCE_ROW( tabledemo, ProfileConfig ),
    SCHEMA_CONFORMANCE_ROW( tabledemo, LoadoutConfig ),
    SCHEMA_CONFORMANCE_ROW( tabledemo, WideBlob ),
    SCHEMA_CONFORMANCE_ROW( tabledemo, ArchiveConfig ),
    SCHEMA_CONFORMANCE_ROW( tabledemo, KeyedConfig ),
    SCHEMA_CONFORMANCE_ROW( tabledemo, PackConfig )
};

/* Sets the one field a `write-refusal` row names. The set is EXACTLY the rows'
   (root, path, kind) triples: an unknown one answers 0, which the surface
   reports by name, so a new row cannot pass by being skipped. */
int conformance_write_refusal_set_tabledemo( const char * root, void * value, const char * path,
                                             const char * kind, const char * text )
{
    const long long n = strtoll( text, NULL, 10 );
    if ( strcmp( root, "RootConfig" ) == 0 )
    {
        tabledemo_root_config_t * v = (tabledemo_root_config_t *) value;
        tabledemo_weapon_config_t * w0 = &v->weapons[0];
        const int32_t weapons = (int32_t) ( sizeof( v->weapons ) / sizeof( v->weapons[0] ) );
        const int32_t note = (int32_t) sizeof( v->version_note ) - 1;
        int32_t i;
        if ( v->weapons_count < 1 ) { return 0; } /* every weapons[0] path needs element 0 */
        if ( strcmp( path, "weapons[0].penetration" ) == 0 && strcmp( kind, "int" ) == 0 ) { w0->penetration = (int32_t) n; return 1; }
        if ( strcmp( path, "weapons[0].channel" ) == 0 && strcmp( kind, "int" ) == 0 ) { w0->channel = (uint32_t) n; return 1; }
        if ( strcmp( path, "weapons[0].damage" ) == 0 && strcmp( kind, "float" ) == 0 )
        {
            if ( strcmp( text, "nan" ) == 0 ) { w0->damage = NAN; return 1; }
            if ( strcmp( text, "inf" ) == 0 ) { w0->damage = INFINITY; return 1; }
            if ( strcmp( text, "-inf" ) == 0 ) { w0->damage = -INFINITY; return 1; }
            return 0;
        }
        if ( strcmp( path, "weapons[0].effect" ) == 0 && strcmp( kind, "tag" ) == 0 )
        {
            /* a lawful tag takes its arm at the arm's declared defaults */
            if ( n == TABLEDEMO_EFFECT_TYPE_BUFF ) { tabledemo_effect_select_buff( &w0->effect ); }
            else if ( n == TABLEDEMO_EFFECT_TYPE_DEBUFF ) { tabledemo_effect_select_debuff( &w0->effect ); }
            else { w0->effect.type = (tabledemo_effect_type_t) n; }
            return 1;
        }
        if ( strcmp( path, "weapons" ) == 0 && strcmp( kind, "count" ) == 0 )
        {
            /* new elements are copies of element 0; past storage only the count
               moves, and the writer must refuse before it reads past the array */
            for ( i = v->weapons_count; i < (int32_t) n && i < weapons; i++ ) { v->weapons[i] = v->weapons[0]; }
            v->weapons_count = (int32_t) n;
            return 1;
        }
        if ( strcmp( path, "version_note" ) == 0 && strcmp( kind, "length" ) == 0 )
        {
            for ( i = 0; i < (int32_t) n && i < note; i++ ) { v->version_note[i] = 'a'; }
            v->version_note_length = (int32_t) n;
            return 1;
        }
        return 0;
    }
    if ( strcmp( root, "LoadoutConfig" ) == 0 )
    {
        tabledemo_loadout_config_t * v = (tabledemo_loadout_config_t *) value;
        if ( strcmp( path, "grade" ) == 0 && strcmp( kind, "ordinal" ) == 0 ) { v->grade = (tabledemo_grade_t) n; return 1; }
    }
    return 0;
}

const ConformanceCodec * conformance_codecs_tabledemo( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}
