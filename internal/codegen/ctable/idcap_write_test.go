package ctable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE C WRITER REFUSES AN ID TABLE ITS READERS REFUSE (docs/SPEC-TABLES.md §3,
// ruling R8). The unit is tablewire.IdCapSchema: every keys slot set needs 4095
// ids, x one more and y one more. 4096 ids is written (its count word is 4096
// and the load measure accepts it); 4097 is refused by measure and by save,
// and save writes nothing. Red when the cap is taken out of the writer.
func TestCWriterRefusesMoreThan4096Ids(t *testing.T) {
	t.Parallel()
	runCGenerated(t, tablewire.IdCapSchema(), `#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "ProbeTable.h"

static uint8_t buf[1 << 20];

static int row( uint8_t x, uint8_t y, int ids )
{
    Root v;
    int64_t n, w;
    uint64_t count = 0;
    int i;
    memset( &v, 0, sizeof( v ) );
    for ( i = 0; i < (int) sizeof( v.keys ); i++ ) { v.keys[i] = 1; }
    v.x = x; v.y = y;
    n = root_measure( NULL, &v );
    memset( buf, 0xA5, sizeof( buf ) );
    w = root_save( NULL, &v, buf, (int64_t) sizeof( buf ) );
    if ( ids > 4096 )
    {
        if ( n >= 0 || w >= 0 ) { printf( "%d ids: measure %lld save %lld, want refusals\n", ids, (long long) n, (long long) w ); return 1; }
        if ( buf[0] != 0xA5 ) { printf( "%d ids: a refused save wrote bytes\n", ids ); return 1; }
        return 0;
    }
    if ( n <= 0 || w != n ) { printf( "%d ids: measure %lld save %lld\n", ids, (long long) n, (long long) w ); return 1; }
    for ( i = 0; i < 8; i++ ) { count |= (uint64_t) buf[w - 8 + i] << ( 8 * i ); }
    if ( count != (uint64_t) ids ) { printf( "%d ids: count word %llu\n", ids, (unsigned long long) count ); return 1; }
    if ( root_load_measure( buf, w ) <= 0 ) { printf( "%d ids: the reader refused the file\n", ids ); return 1; }
    return 0;
}

int main( void )
{
    int failed = row( 0, 0, 4095 ) + row( 1, 0, 4096 ) + row( 1, 1, 4097 );
    if ( failed ) { return 1; }
    printf( "ok\n" );
    return 0;
}
`)
}
