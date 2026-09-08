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
