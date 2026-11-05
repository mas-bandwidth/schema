package ctable

const tableTextRuntime = `
#ifndef SCHEMA_TABLE_UTF16_RUNTIME
#define SCHEMA_TABLE_UTF16_RUNTIME
static SCHEMA_UNUSED uint16_t table_wire_utf16_unit( const uint8_t * bytes, int64_t index )
{
    return (uint16_t) ((uint16_t) bytes[index*2] | ((uint16_t) bytes[index*2+1] << 8));
}
static SCHEMA_UNUSED int table_wire_utf16( const uint8_t * bytes, int64_t units )
{
    int64_t i = 0;
    while ( i < units ) {
        uint16_t unit = table_wire_utf16_unit( bytes, i++ );
        if ( unit == 0 ) { return 0; }
        if ( unit >= 0xd800 && unit <= 0xdbff ) {
            uint16_t low;
            if ( i == units ) { return 0; }
            low = table_wire_utf16_unit( bytes, i++ );
            if ( low < 0xdc00 || low > 0xdfff ) { return 0; }
        } else if ( unit >= 0xdc00 && unit <= 0xdfff ) { return 0; }
    }
    return 1;
}
/* A WRITE KEEPS PAIRED WIDE TEXT (docs/SPEC.md §5.1): a wide string a program
   built holds no unpaired surrogate when a writer puts it on a wire. It reads
   the storage's own units, and a zero unit is a whole code point the rule
   admits. */
static SCHEMA_UNUSED int table_wire_utf16_paired( const uint16_t * units, int64_t count )
{
    int64_t i;
    for ( i = 0; i < count; i++ ) {
        uint16_t unit = units[i];
        if ( unit >= 0xd800 && unit <= 0xdbff ) {
            if ( i + 1 >= count || units[i+1] < 0xdc00 || units[i+1] > 0xdfff ) { return 0; }
            i++;
        } else if ( unit >= 0xdc00 && unit <= 0xdfff ) { return 0; }
    }
    return 1;
}
/* A FIXED WRITE KEEPS THE FIXED READER'S WIDE TEXT (docs/SPEC.md §5.1): the
   fixed reader refuses a zero unit as text_ill_formed, so the fixed writer's
   contract holds the storage to the pairing rule above AND to no zero unit. */
static SCHEMA_UNUSED int table_wire_utf16_fixed_text( const uint16_t * units, int64_t count )
{
    int64_t i;
    for ( i = 0; i < count; i++ ) {
        if ( units[i] == 0 ) { return 0; }
    }
    return table_wire_utf16_paired( units, count );
}
static SCHEMA_UNUSED int64_t table_wire_utf16_clamp( const uint8_t * bytes, int64_t units, int64_t bound )
{
    if ( units <= bound ) { return units; }
    if ( bound > 0 ) {
        uint16_t last = table_wire_utf16_unit( bytes, bound-1 );
        if ( last >= 0xd800 && last <= 0xdbff ) { bound--; }
    }
    return bound;
}
#endif
`
