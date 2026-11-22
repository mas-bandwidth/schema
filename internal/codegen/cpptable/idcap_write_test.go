package cpptable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE C++ WRITER REFUSES AN ID TABLE ITS READERS REFUSE (docs/SPEC-TABLES.md
// §3, ruling R8). The unit is tablewire.IdCapSchema: every keys slot set needs
// 4095 ids, x one more and y one more. 4096 ids is written (its count word is
// 4096 and the load measure accepts it); 4097 is refused by RootMeasure and by
// RootSave, and RootSave writes nothing. Red when the cap is taken out.
func TestCPPWriterRefusesMoreThan4096Ids(t *testing.T) {
	t.Parallel()
	u := loadUnitFromSource(t, tablewire.IdCapSchema())
	body := `
    static uint8_t buf[ 1 << 20 ];
    const int rows[3][3] = { { 0, 0, 4095 }, { 1, 0, 4096 }, { 1, 1, 4097 } };
    for ( int r = 0; r < 3; r++ )
    {
        const int ids = rows[r][2];
        Root v; RootReset( v );
        for ( size_t i = 0; i < sizeof( v.keys.slots ); i++ ) { v.keys.slots[i] = 1; }
        v.x = (uint8_t) rows[r][0]; v.y = (uint8_t) rows[r][1];
        const int64_t n = RootMeasure( &v );
        memset( buf, 0xA5, sizeof( buf ) );
        const int64_t w = RootSave( &v, buf, (int64_t) sizeof( buf ) );
        if ( ids > 4096 )
        {
            if ( n >= 0 || w >= 0 ) { printf( "%d ids: measure %lld save %lld, want refusals\n", ids, (long long) n, (long long) w ); return 1; }
            if ( buf[0] != 0xA5 ) { printf( "%d ids: a refused save wrote bytes\n", ids ); return 1; }
            continue;
        }
        if ( n <= 0 || w != n ) { printf( "%d ids: measure %lld save %lld\n", ids, (long long) n, (long long) w ); return 1; }
        uint64_t count = 0;
        for ( int i = 0; i < 8; i++ ) { count |= (uint64_t) buf[w - 8 + i] << ( 8 * i ); }
        if ( count != (uint64_t) ids ) { printf( "%d ids: count word %llu\n", ids, (unsigned long long) count ); return 1; }
        if ( RootLoadMeasure( buf, w ) <= 0 ) { printf( "%d ids: the reader refused the file\n", ids ); return 1; }
    }
`
	out, err := cppHeldRun(t, u, body)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
