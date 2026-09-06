// THE TWO COST OBSERVABLES for the doc and tags columns (docs/SPEC-TABLES.md
// §8.7). Each cost claim §8.1 makes has a failure a test can see, and this
// binary is where both are seen.
//
// 1. `doc` IS NEVER NULL. The walk below CONCATENATES every doc in the
//    tabledemo closure into one buffer with NO NULL TEST. A NULL column
//    faults here rather than printing a line that happens to look right, so
//    the rule has a red state and the Makefile's negative control — an
//    emitter patched to write NULL for one absent doc — takes this binary
//    down.
//
// 2. ABSENCE IS ONE SHARED EMPTY STRING. C++ has address identity, so the
//    walk asserts every absent doc in the unit compares equal BY ADDRESS to
//    the unit's one TableDocNone. An emitter that inlined "" per row would
//    give each row its own object and go red here.
//
// `tags` takes the same shape one column over: absent is NULL with a count of
// 0, never a per-row empty array, and the walk checks the pair agrees.

#include <cstdio>
#include <cstring>

#include "TablesTable.h"

using namespace tabledemo;

static char concatenated[1 << 16];
static int64_t used = 0;
static int64_t rows = 0;
static int64_t annotated = 0;
static int failures = 0;

// concatenate appends one doc column with no null test — observable 1.
static void concatenate( const char * doc )
{
    const int64_t n = (int64_t) strlen( doc );
    if ( used + n < (int64_t) sizeof( concatenated ) )
    {
        memcpy( concatenated + used, doc, (size_t) n );
        used += n;
    }
}

// checkAnnotation holds one row to both observables: the doc column is the
// shared empty string when the row carries no text, and the tag pair agrees
// about absence.
static void checkAnnotation( const char * what, const char * doc, int32_t num_tags, const char * const * tags )
{
    rows++;
    concatenate( doc );
    if ( doc[0] == 0 && doc != TableDocNone )
    {
        printf( "FAIL %s: an absent doc is its own empty object, not the unit's one TableDocNone\n", what );
        failures++;
    }
    if ( doc[0] != 0 )
    {
        annotated++;
    }
    if ( ( num_tags == 0 ) != ( tags == NULL ) )
    {
        printf( "FAIL %s: num_tags %d beside a %s tag list — absence is 0 and NULL together\n",
                what, num_tags, tags == NULL ? "NULL" : "non-NULL" );
        failures++;
    }
    for ( int32_t i = 0; i < num_tags; i++ )
    {
        if ( tags[i] == NULL )
        {
            printf( "FAIL %s: tag %d is NULL — a declared tag is a string literal\n", what, i );
            failures++;
        }
        else
        {
            concatenate( tags[i] );
        }
    }
    if ( num_tags > 0 )
    {
        annotated++;
    }
}

// walk visits one table's descriptor and every field of it, recursing through
// the nested-table column so the whole closure is reached from its roots.
static void walk( const TableTypeInfo * type, int depth )
{
    if ( type == NULL || depth > 8 )
    {
        return;
    }
    checkAnnotation( type->name, type->doc, type->num_tags, type->tags );
    for ( int32_t i = 0; i < type->num_fields; i++ )
    {
        const TableFieldInfo * f = &type->fields[i];
        checkAnnotation( f->name, f->doc, f->num_tags, f->tags );
        walk( f->table, depth + 1 );
    }
}

int main()
{
    walk( RootConfigTableType(), 0 );
    walk( LoadoutConfigTableType(), 0 );
    walk( ProfileConfigTableType(), 0 );
    walk( WeaponConfigTableType(), 0 );

    if ( rows == 0 )
    {
        printf( "FAIL doc/tags gate: the walk reached no descriptor row\n" );
        return 1;
    }
    // the exhibit (docs/SPEC-TABLES.md §8.7): tabledemo carries an annotation
    // at every level the language admits, so a walk that found none is a walk
    // over a corpus the exhibit never reached
    if ( annotated == 0 )
    {
        printf( "FAIL doc/tags gate: %lld rows and not one annotation — the exhibit is not in this build\n",
                (long long) rows );
        return 1;
    }
    if ( failures != 0 )
    {
        printf( "FAIL doc/tags gate: %d failures over %lld rows\n", failures, (long long) rows );
        return 1;
    }
    printf( "doc/tags gate: %lld descriptor rows concatenated with no null test, %lld annotated, %lld bytes of text\n",
            (long long) rows, (long long) annotated, (long long) used );
    return 0;
}
