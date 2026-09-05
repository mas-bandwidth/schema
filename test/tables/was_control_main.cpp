// THE `was` CONTROL (docs/SPEC-TABLES.md §5). W1's fleet, whose node records
// carry fnv1a64( "Vessel" ), read under W2, where the table is Ship. Built
// against the shipped W2 the read is silent and every node lands; built
// against a W2 whose `was = "Vessel"` was stripped, every record is one this
// reader cannot name: `unknown` counts, the pointers read null, and the home
// vessel holds its declared default. The Makefile compiles this file twice and
// requires the two answers to differ exactly that way.
#include "W2Table.h"
#include <cstdio>
#include <cstdlib>
#include <cstring>

int main()
{
    FILE * f = fopen( "testdata/wire/tables/w1_fleet.bin", "rb" );
    if ( f == NULL ) { printf( "missing golden\n" ); return 2; }
    static uint8_t wire[4096];
    const int64_t n = (int64_t) fread( wire, 1, sizeof( wire ), f );
    fclose( f );
    const int64_t need = tblw2::FleetLoadMeasure( wire, n );
    if ( need < 0 ) { printf( "load measure refused\n" ); return 2; }
    uint8_t * region = (uint8_t *) malloc( (size_t) need );
    tblw2::TableReport report;
    const tblw2::Fleet * root = tblw2::FleetLoad( region, need, wire, n, &report );
    if ( root == NULL ) { printf( "load refused\n" ); return 2; }
    const tblw2::Ship * flagship = tblw2::ShipAt( root->flagship );
    printf( "unknown=%d kind_mismatch=%d malformed=%d flagship=%s escorts=%d home_name=%s\n",
            (int) report.unknown, (int) report.kind_mismatch, (int) report.malformed,
            flagship == NULL ? "null" : flagship->name, (int) root->escorts_count, root->home.name );
    free( region );
    return 0;
}
