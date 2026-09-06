// THE TWO COST OBSERVABLES for the doc and tags columns, and THE TWO PLACES
// ONE DECLARATION'S ANNOTATION IS SPELLED TWICE (docs/SPEC-TABLES.md §8.7).
// Each claim §8.1 and §8.3 make has a failure a test can see, and this binary
// is where all four are seen.
//
// 1. `doc` IS NEVER NULL. The walk below CONCATENATES every doc in the unit
//    into one buffer with NO NULL TEST — the DESCRIPTOR half over each
//    declaration's closure, reached from the registry's entries through the
//    `table` column, and the REGISTRY half over every row `UnitView()` hands
//    out. A NULL column faults here rather than printing a line that happens
//    to look right, so the rule has a red state and the Makefile's negative
//    control, an emitter patched to write NULL for one absent doc, takes this
//    binary down.
//
// 2. ABSENCE IS ONE SHARED EMPTY STRING. C++ has address identity, so the
//    walk asserts every absent doc in the unit compares equal BY ADDRESS to
//    the unit's one TableDocNone. An emitter that inlined "" per row would
//    give each row its own object and go red here.
//
// 3. THE GENERAL ARM PAIR. An arm that names no record is a field line, so
//    its `ViewVariant` row and the `TableFieldInfo` that row points at carry
//    the same doc and the same tags. They agree by construction, and this is
//    where that is checked rather than assumed.
//
// 4. THE TYPE PAIR. Every `ViewType`'s doc and tags must equal the doc and
//    tags on the `TableTypeInfo` its `type` points at, string for string and
//    entry for entry.
//
// `tags` takes the same shape one column over: absent is NULL with a count of
// 0, never a per-row empty array, and the walk checks the pair agrees.
//
// The unit is named by the build, not by this source: VIEW_HEADER is the
// generated view header and VIEW_NAMESPACE the unit's package, and the
// program claims no name a schema might declare.

#include <cstdio>
#include <cstring>

#include VIEW_HEADER

namespace unit_ns = VIEW_NAMESPACE;

static char concatenated[1 << 18];
static int64_t used = 0;
static int64_t rows = 0;
static int64_t annotated = 0;
static int failures = 0;

// concatenate appends one doc column with no null test. That is observable 1.
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
    if ( doc[0] == 0 && doc != unit_ns::TableDocNone )
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
        printf( "FAIL %s: num_tags %d beside a %s tag list. Absence is 0 and NULL together\n",
                what, num_tags, tags == NULL ? "NULL" : "non-NULL" );
        failures++;
    }
    for ( int32_t i = 0; i < num_tags; i++ )
    {
        if ( tags[i] == NULL )
        {
            printf( "FAIL %s: tag %d is NULL. A declared tag is a string literal\n", what, i );
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

// samePair holds the two records that describe ONE declared item to each
// other: the same doc text and the same tag list, string for string and entry
// for entry (docs/SPEC-TABLES.md §8.7).
static void samePair( const char * what,
                      const char * a_doc, int32_t a_num, const char * const * a_tags,
                      const char * b_doc, int32_t b_num, const char * const * b_tags )
{
    if ( strcmp( a_doc, b_doc ) != 0 )
    {
        printf( "FAIL %s: two records of one item disagree about doc: \"%s\" and \"%s\"\n", what, a_doc, b_doc );
        failures++;
        return;
    }
    if ( a_num != b_num )
    {
        printf( "FAIL %s: two records of one item carry %d and %d tags\n", what, a_num, b_num );
        failures++;
        return;
    }
    for ( int32_t i = 0; i < a_num; i++ )
    {
        if ( strcmp( a_tags[i], b_tags[i] ) != 0 )
        {
            printf( "FAIL %s: two records of one item disagree about tag %d: \"%s\" and \"%s\"\n",
                    what, i, a_tags[i], b_tags[i] );
            failures++;
        }
    }
}

// walk visits one declaration's descriptor and every field of it, recursing
// through the nested-table column and through a union field's ARMS so the
// whole closure is reached from its roots. An arm is a field line
// (docs/SPEC-TABLES.md §2.6, §8.1): an arm that names a declaration hangs that
// declaration's descriptor off the arm row, and a general arm carries a field
// row of its own with its own doc and tag columns. Both reach the observables
// only if the walk descends here.
static void walk( const unit_ns::TableTypeInfo * type, int depth )
{
    if ( type == NULL || depth > 8 )
    {
        return;
    }
    checkAnnotation( type->name, type->doc, type->num_tags, type->tags );
    for ( int32_t i = 0; i < type->num_fields; i++ )
    {
        const unit_ns::TableFieldInfo * f = &type->fields[i];
        checkAnnotation( f->name, f->doc, f->num_tags, f->tags );
        walk( f->table, depth + 1 );
        if ( f->arms == NULL )
        {
            continue;
        }
        // the tag range is [0, enum_max]. Index 0 is the empty arm and names
        // neither a table nor a field row
        const unit_ns::TableUnionInfo * u = f->arms();
        for ( int64_t tag = 0; tag <= f->enum_max; tag++ )
        {
            const unit_ns::TableUnionArmInfo * arm = &u->arms[tag];
            if ( arm->field != NULL )
            {
                checkAnnotation( arm->field->name, arm->field->doc, arm->field->num_tags, arm->field->tags );
            }
            walk( arm->table, depth + 1 );
        }
    }
}

// registry is the second half of the walk: every row UnitView() hands out —
// the six declaration sets, and every variant, bit and arm inside the three
// vocabularies. A field's annotation is on its descriptor, reached above, so
// the registry does not restate it.
static void registry( const unit_ns::UnitViewInfo * unit )
{
    for ( int32_t i = 0; i < unit->num_constants; i++ )
    {
        const unit_ns::ViewConstant & c = unit->constants[i];
        checkAnnotation( c.name, c.doc, c.num_tags, c.tags );
    }
    const unit_ns::ViewVocabulary * vocabularies[3] = { unit->enums, unit->flags, unit->unions };
    int32_t counts[3] = { unit->num_enums, unit->num_flags, unit->num_unions };
    for ( int s = 0; s < 3; s++ )
    {
        for ( int32_t i = 0; i < counts[s]; i++ )
        {
            const unit_ns::ViewVocabulary & v = vocabularies[s][i];
            checkAnnotation( v.name, v.doc, v.num_tags, v.tags );
            for ( int32_t r = 0; r < v.num_variants; r++ )
            {
                const unit_ns::ViewVariant & row = v.variants[r];
                checkAnnotation( row.name, row.doc, row.num_tags, row.tags );
                // THE GENERAL ARM PAIR (docs/SPEC-TABLES.md §8.7)
                if ( row.field != NULL )
                {
                    samePair( row.name, row.doc, row.num_tags, row.tags,
                              row.field->doc, row.field->num_tags, row.field->tags );
                }
            }
        }
    }
    const unit_ns::ViewType * sets[2] = { unit->types, unit->tables };
    int32_t setCounts[2] = { unit->num_types, unit->num_tables };
    for ( int s = 0; s < 2; s++ )
    {
        for ( int32_t i = 0; i < setCounts[s]; i++ )
        {
            const unit_ns::ViewType & entry = sets[s][i];
            checkAnnotation( entry.name, entry.doc, entry.num_tags, entry.tags );
            // THE TYPE PAIR (docs/SPEC-TABLES.md §8.7)
            samePair( entry.name, entry.doc, entry.num_tags, entry.tags,
                      entry.type->doc, entry.type->num_tags, entry.type->tags );
            walk( entry.type, 0 );
        }
    }
}

int main()
{
    registry( unit_ns::UnitView() );

    if ( rows == 0 )
    {
        printf( "FAIL doc/tags gate: the walk reached no descriptor row\n" );
        return 1;
    }
#ifdef VIEW_EXHIBIT
    // the exhibit (docs/SPEC-TABLES.md §8.7): tabledemo carries an annotation
    // at every level the language admits, so a walk that found none is a walk
    // over a corpus the exhibit never reached
    if ( annotated == 0 )
    {
        printf( "FAIL doc/tags gate: %lld rows and not one annotation. The exhibit is not in this build\n",
                (long long) rows );
        return 1;
    }
#endif
    if ( failures != 0 )
    {
        printf( "FAIL doc/tags gate: %d failures over %lld rows\n", failures, (long long) rows );
        return 1;
    }
    printf( "doc/tags gate: %lld descriptor and registry rows concatenated with no null test, %lld annotated, %lld bytes of text\n",
            (long long) rows, (long long) annotated, (long long) used );
    return 0;
}
