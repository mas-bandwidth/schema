// THE EDITOR GATE, and it is a dogfood rather than a thought experiment
// (docs/SPEC-TABLES.md §8.7). This program has the generated view file and
// NOTHING ELSE: no schema files on disk, no compiler, no knowledge of a single
// declaration's name. It calls UnitView() and prints the whole build — every
// constant with its value, every enum and flags and union with its variants in
// order, every type and every table with every property — and the listing it
// prints is byte-compared against the listing the compiler produces from its
// own IR. If it cannot reach a declaration or a property of one, the view is
// incomplete and the gate says so.
//
// The unit is named by the build, not by this source: VIEW_HEADER is the
// generated header and VIEW_NAMESPACE the unit's package, so one program
// serves every unit in the corpus and knows nothing about any of them.
//
// A MULTI-LINE doc prints as ONE LINE, each newline written \n and the
// escape's own backslash written \\. The listing is a line-oriented byte
// comparison, so a column whose text carries newlines has to be flattened
// before it is compared, and both halves flattening it by the same rule is
// what keeps the comparison a comparison rather than a formatting argument.

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include VIEW_HEADER

// THE PROGRAM CLAIMS NO NAME THE UNIT MIGHT DECLARE. A schema is free to
// declare `Holder`, `round` or anything else, and this source is compiled
// against every unit in the corpus, so the package is reached through an alias
// rather than pulled in wholesale.
namespace unit_ns = VIEW_NAMESPACE;

// flatten writes one doc column as a single line. It reads the column with NO
// NULL TEST: doc is never NULL (§8.1), and a NULL here faults rather than
// printing a line that happens to look right.
static void flatten( const char * doc )
{
    for ( const char * p = doc; *p != 0; p++ )
    {
#ifdef VIEW_UNFLATTENED
        // THE NEGATIVE CONTROL for the flattening rule: a printer that writes
        // the newline verbatim splits a multi-line doc across lines, and the
        // corpus gate's byte comparison must go red for it.
        fputc( *p, stdout );
#else
        if ( *p == '\\' )     { fputs( "\\\\", stdout ); }
        else if ( *p == '\n' ) { fputs( "\\n", stdout ); }
        else                   { fputc( *p, stdout ); }
#endif
    }
}

// annotation prints a row's tags and its doc, the doc LAST because it is
// free-form text and everything after it on the line would be ambiguous.
static void annotation( int32_t num_tags, const char * const * tags, const char * doc )
{
    fputs( " tags=[", stdout );
    for ( int32_t i = 0; i < num_tags; i++ )
    {
        if ( i > 0 ) { fputc( ',', stdout ); }
        fputs( tags[i], stdout );
    }
    fputs( "] doc=", stdout );
    flatten( doc );
    fputc( '\n', stdout );
}

static void vocabulary( const char * what, const unit_ns::ViewVocabulary & v, const char * row )
{
    printf( "%s %s file=%s max=%lld bits=%d variants=%d",
            what, v.name, v.file, (long long) v.max, v.storage_bits, v.num_variants );
    annotation( v.num_tags, v.tags, v.doc );
    for ( int32_t i = 0; i < v.num_variants; i++ )
    {
        const unit_ns::ViewVariant & r = v.variants[i];
        printf( "%s %s %s %llu %s id=%016llx",
                what, v.name, row, (unsigned long long) r.value, r.name, (unsigned long long) r.id );
        annotation( r.num_tags, r.tags, r.doc );
    }
}

// ---- the general arm's probe and overlay (docs/SPEC-TABLES.md §8.7) ----
//
// For each arm that names no record the listing carries the arm's kind and
// bounds off its FIELD descriptor, the value read at the arm's offset from the
// base of the union's storage, and where that offset sits relative to the
// union's own payload base. A generic walker holds no union to instantiate, so
// it takes a HOLDER — the first registry entry declaring a field of that
// union's type, the tables in registry order and then the types — establishes
// that holder's defaults through the descriptor's own reset hook, writes the
// arm's tag at the arm's offset and reads it back.
//
// EVERY ARM OVERLAYS, with the tag at offset 0 and the overlay after it
// (§8.1), so every arm's offset is the union's payload base and the OVERLAY
// column is 0 on every general arm. That is the column an arm whose offset
// does not reach its own value goes red in: the arm row's own offset, from the
// view file, against the payload base the table header's arms table carries.

struct Holder
{
    const unit_ns::TableTypeInfo * type;
    const unit_ns::TableFieldInfo * field;
};

static Holder findHolder( const unit_ns::UnitViewInfo * unit, const char * union_name )
{
    Holder none = { NULL, NULL };
    const unit_ns::ViewType * sets[2] = { unit->tables, unit->types };
    int32_t counts[2] = { unit->num_tables, unit->num_types };
    for ( int s = 0; s < 2; s++ )
    {
        for ( int32_t i = 0; i < counts[s]; i++ )
        {
            const unit_ns::TableTypeInfo * type = sets[s][i].type;
            for ( int32_t f = 0; f < type->num_fields; f++ )
            {
                if ( type->fields[f].arms != NULL && strcmp( type->fields[f].type_name, union_name ) == 0 )
                {
                    Holder found = { type, &type->fields[f] };
                    return found;
                }
            }
        }
    }
    return none;
}

// probeArm writes one arm's tag at its own offset and reads it back. Each arm
// is probed on its own storage, because the arms OVERLAY and a second write
// would land on the first arm's bytes.
static uint64_t probeArm( const Holder & holder, const unit_ns::TableFieldInfo * arm, uint64_t tag )
{
    unsigned char * storage = (unsigned char *) calloc( 1, holder.type->size );
    if ( storage == NULL )
    {
        return 0;
    }
    holder.type->reset( storage );
    unsigned char * base = storage + holder.field->offset + arm->offset;
    size_t width = arm->elem_size < 8 ? (size_t) arm->elem_size : 8;
    memcpy( base, &tag, width );
    uint64_t value = 0;
    memcpy( &value, base, width );
    free( storage );
    return value;
}

static void unionVocabulary( const unit_ns::UnitViewInfo * unit, const unit_ns::ViewVocabulary & v )
{
    printf( "union %s file=%s max=%lld bits=%d variants=%d",
            v.name, v.file, (long long) v.max, v.storage_bits, v.num_variants );
    annotation( v.num_tags, v.tags, v.doc );
    Holder holder = findHolder( unit, v.name );
    const unit_ns::TableUnionInfo * arms = holder.type != NULL ? holder.field->arms() : NULL;
    for ( int32_t i = 0; i < v.num_variants; i++ )
    {
        const unit_ns::ViewVariant & r = v.variants[i];
        printf( "union %s arm %llu %s id=%016llx payload=%s record=%s field=%s",
                v.name, (unsigned long long) r.value, r.name, (unsigned long long) r.id,
                r.payload_name == NULL ? "-" : r.payload_name,
                r.payload != NULL ? "yes" : "no",
                r.field != NULL ? "yes" : "no" );
        if ( r.field != NULL )
        {
            printf( " kind=%d bound=%d", (int) r.field->kind, (int) r.field->array_bound );
            if ( arms != NULL )
            {
                printf( " probe=%llu overlay=%d",
                        (unsigned long long) probeArm( holder, r.field, r.value ),
                        (int) r.field->offset - (int) arms->arms[1].offset );
            }
            else
            {
                printf( " probe=- overlay=-" );
            }
        }
        else
        {
            printf( " kind=- bound=- probe=- overlay=-" );
        }
        annotation( r.num_tags, r.tags, r.doc );
    }
}

static void declaration( const char * what, const unit_ns::ViewType & entry )
{
    const unit_ns::TableTypeInfo * type = entry.type;
    printf( "%s %s file=%s fields=%d", what, entry.name, entry.file, type->num_fields );
    annotation( entry.num_tags, entry.tags, entry.doc );
    for ( int32_t i = 0; i < type->num_fields; i++ )
    {
        const unit_ns::TableFieldInfo & f = type->fields[i];
        printf( "%s %s field %s json=%s type=%s id=%016llx optional=%s",
                what, entry.name, f.name, f.json == NULL ? "-" : f.json, f.type_name,
                (unsigned long long) f.id, f.optional ? "true" : "false" );
        annotation( f.num_tags, f.tags, f.doc );
    }
}

int main()
{
    const unit_ns::UnitViewInfo * unit = unit_ns::UnitView();
    printf( "unit package=%s protocol=%016llx\n", unit->package, (unsigned long long) unit->protocol_id );
    for ( int32_t i = 0; i < unit->num_constants; i++ )
    {
        const unit_ns::ViewConstant & c = unit->constants[i];
        uint64_t bits = 0;
        memcpy( &bits, &c.float_value, sizeof( bits ) );
        printf( "constant %s file=%s type=%s float=%s int=%lld real=%016llx",
                c.name, c.file, c.type_name, c.is_float ? "true" : "false",
                (long long) c.int_value, (unsigned long long) ( c.is_float ? bits : 0 ) );
        annotation( c.num_tags, c.tags, c.doc );
    }
    for ( int32_t i = 0; i < unit->num_enums; i++ )
    {
        vocabulary( "enum", unit->enums[i], "variant" );
    }
    for ( int32_t i = 0; i < unit->num_flags; i++ )
    {
        vocabulary( "flags", unit->flags[i], "bit" );
    }
    for ( int32_t i = 0; i < unit->num_unions; i++ )
    {
        unionVocabulary( unit, unit->unions[i] );
    }
    for ( int32_t i = 0; i < unit->num_types; i++ )
    {
        declaration( "type", unit->types[i] );
    }
    for ( int32_t i = 0; i < unit->num_tables; i++ )
    {
        declaration( "table", unit->tables[i] );
    }
    return 0;
}
