// Repeated map entry keys on the wire: the same field id twice in one body is
// damage, and the strict read refuses the whole load (docs/SPEC-TABLES.md §4,
// "the same field id twice in one body"). Last-wins on a repeated field id or
// map key is retired (§13.10), so no ordering of a bad, widened or exact
// occurrence reads.
package ctable

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestCMapRepeatedKeyRefused(t *testing.T) {
	t.Parallel()
	vec, err := os.ReadFile("../../../testdata/wire/tables/fuzz-vectors/map_repeated_key_widen.bin")
	if err != nil {
		t.Fatalf("read map_repeated_key_widen.bin: %v", err)
	}
	var hexBytes []string
	for _, b := range vec {
		hexBytes = append(hexBytes, fmt.Sprintf("0x%02x", b))
	}
	vecInit := strings.Join(hexBytes, ", ")

	schema := `package mapdemo

fixed table Item
{
    count int32
}

table Pairs
{
    slots map[uint32][2]*Item
    past int32 | was = "after"
}

table Simple
{
    items map[uint32]int32
}
`

	probe := fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define CHECK( cond ) do { if ( !( cond ) ) { fprintf( stderr, "CHECK failed at line %%d: %%s\n", __LINE__, #cond ); return 1; } } while ( 0 )

/* Cases 2-4 carry two conditions, a repeated key id (damage) and a key kind
   no widening reaches (kind mismatch); which one the walk meets first is the
   leg's own (docs/SPEC-TABLES.md §2.9, §6.5), so either of the two names is
   this leg's honest reason. Case 5 carries the repeat alone. */
#define REPEATED_REFUSAL( rep ) ( ( rep ).refused == 1 && ( ( rep ).refuse_reason == SCHEMA_TABLE_REFUSE_WIRE_DAMAGED || ( rep ).refuse_reason == SCHEMA_TABLE_REFUSE_KIND_MISMATCH ) )

static void put_u64_le( uint8_t * dst, uint64_t v )
{
    int i;
    for ( i = 0; i < 8; i++ ) { dst[i] = (uint8_t) ( v >> ( i * 8 ) ); }
}

int main( void )
{
    /* 1. Shared fuzz vector map_repeated_key_widen.bin on mapdemo.Pairs */
    {
        static const uint8_t wire[] = { %s };
        TableReport rep = { 0 };
        int64_t rbytes = pairs_load_measure( wire, (int64_t) sizeof( wire ) );
        uint8_t * region;
        const Pairs * root;
        CHECK( rbytes == 88 );
        region = (uint8_t *) calloc( 1, (size_t) rbytes + 64 );
        CHECK( region != NULL );
        root = pairs_load( (uint8_t *) ( ( (uintptr_t) region + 63u ) & ~(uintptr_t) 63u ), rbytes, wire, (int64_t) sizeof( wire ), &rep );
        CHECK( root == NULL );
        CHECK( rep.refused == 1 );
        CHECK( rep.refuse_reason != SCHEMA_TABLE_REFUSE_OK );
        free( region );
    }

    /* Obtain exact wire IDs for Simple.items, key, and value by saving a clean 1-entry Simple */
    uint64_t id_items = 0, id_key = 0, id_val = 0;
    {
        SimpleBuilder b;
        Simple * r;
        int32_t * slot;
        uint8_t clean[256];
        int64_t n;
        CHECK( simple_builder_init( &b ) );
        r = simple_builder_root( &b );
        CHECK( r != NULL );
        slot = simple_items_insert( &b.main, &r->items, 7u );
        CHECK( slot != NULL );
        *slot = 42;
        CHECK( simple_builder_lock( &b ) );
        n = simple_save( NULL, (const Simple *) b.region, clean, (int64_t) sizeof( clean ) );
        CHECK( n > 32 );
        /* ID table at end of wire: 3 IDs (items, key, value) + 8-byte count = 32 bytes */
        {
            const uint8_t * idtab = clean + n - 32;
            int i;
            id_items = 0; id_key = 0; id_val = 0;
            for ( i = 0; i < 8; i++ )
            {
                id_items |= ( (uint64_t) idtab[i] ) << ( i * 8 );
                id_key   |= ( (uint64_t) idtab[8 + i] ) << ( i * 8 );
                id_val   |= ( (uint64_t) idtab[16 + i] ) << ( i * 8 );
            }
        }
        simple_builder_shutdown( &b );
    }

    /* 2. bad_then_widened: key appears first as tkString (12, "x"), then as tkU8 (6, 7), value as tkI32 (4, 42) */
    {
        uint8_t wire[64];
        int pos = 0;
        TableReport rep = { 0 };
        int64_t rbytes;
        uint8_t * region;
        const Simple * root;

        wire[pos++] = 1; /* form */
        wire[pos++] = 1; /* ref=1 (items) */
        wire[pos++] = 14; /* kind=array */
        wire[pos++] = 17; /* L = 2 (elem_kind=13, count=1) + 1 (entry L=14) + 14 */
        wire[pos++] = 13; /* elem_kind = table */
        wire[pos++] = 1;  /* count = 1 */
        wire[pos++] = 14; /* entry L = 14 */
        /* 1st key: bad kind (string) */
        wire[pos++] = 2; wire[pos++] = 12; wire[pos++] = 1; wire[pos++] = 0x78;
        /* 2nd key: widened kind (u8 = 7) */
        wire[pos++] = 2; wire[pos++] = 6; wire[pos++] = 7;
        /* value: i32 = 42 */
        wire[pos++] = 3; wire[pos++] = 4; wire[pos++] = 42; wire[pos++] = 0; wire[pos++] = 0; wire[pos++] = 0;
        wire[pos++] = 0; /* entry end */
        wire[pos++] = 0; /* root end */
        put_u64_le( wire + pos, id_items ); pos += 8;
        put_u64_le( wire + pos, id_key );   pos += 8;
        put_u64_le( wire + pos, id_val );   pos += 8;
        put_u64_le( wire + pos, 3 );        pos += 8;

        rbytes = simple_load_measure( wire, pos );
        CHECK( rbytes > 0 );
        region = (uint8_t *) calloc( 1, (size_t) rbytes + 64 );
        root = simple_load( (uint8_t *) ( ( (uintptr_t) region + 63u ) & ~(uintptr_t) 63u ), rbytes, wire, pos, &rep );
        CHECK( root == NULL );
        CHECK( REPEATED_REFUSAL( rep ) );
        free( region );
    }

    /* 3. widened_then_bad: key appears first as tkU8 (6, 7), then as tkString (12, "x") -> refused */
    {
        uint8_t wire[64];
        int pos = 0;
        TableReport rep = { 0 };
        int64_t rbytes;
        uint8_t * region;
        const Simple * root;

        wire[pos++] = 1;
        wire[pos++] = 1; wire[pos++] = 14; wire[pos++] = 17;
        wire[pos++] = 13; wire[pos++] = 1;
        wire[pos++] = 14;
        /* 1st key: widened kind (u8 = 7) */
        wire[pos++] = 2; wire[pos++] = 6; wire[pos++] = 7;
        /* 2nd key: bad kind (string) */
        wire[pos++] = 2; wire[pos++] = 12; wire[pos++] = 1; wire[pos++] = 0x78;
        /* value: i32 = 42 */
        wire[pos++] = 3; wire[pos++] = 4; wire[pos++] = 42; wire[pos++] = 0; wire[pos++] = 0; wire[pos++] = 0;
        wire[pos++] = 0;
        wire[pos++] = 0;
        put_u64_le( wire + pos, id_items ); pos += 8;
        put_u64_le( wire + pos, id_key );   pos += 8;
        put_u64_le( wire + pos, id_val );   pos += 8;
        put_u64_le( wire + pos, 3 );        pos += 8;

        rbytes = simple_load_measure( wire, pos );
        CHECK( rbytes > 0 );
        region = (uint8_t *) calloc( 1, (size_t) rbytes + 64 );
        root = simple_load( (uint8_t *) ( ( (uintptr_t) region + 63u ) & ~(uintptr_t) 63u ), rbytes, wire, pos, &rep );
        CHECK( root == NULL );
        CHECK( REPEATED_REFUSAL( rep ) );
        free( region );
    }

    /* 4. bad_then_exact: key appears first as tkString (12, "x"), then as tkU32 (8, 9) -> refused */
    {
        uint8_t wire[64];
        int pos = 0;
        TableReport rep = { 0 };
        int64_t rbytes;
        uint8_t * region;
        const Simple * root;

        wire[pos++] = 1;
        wire[pos++] = 1; wire[pos++] = 14; wire[pos++] = 20;
        wire[pos++] = 13; wire[pos++] = 1;
        wire[pos++] = 17;
        /* 1st key: bad kind (string) */
        wire[pos++] = 2; wire[pos++] = 12; wire[pos++] = 1; wire[pos++] = 0x78;
        /* 2nd key: exact kind (u32 = 9) */
        wire[pos++] = 2; wire[pos++] = 8; wire[pos++] = 9; wire[pos++] = 0; wire[pos++] = 0; wire[pos++] = 0;
        /* value: i32 = 42 */
        wire[pos++] = 3; wire[pos++] = 4; wire[pos++] = 42; wire[pos++] = 0; wire[pos++] = 0; wire[pos++] = 0;
        wire[pos++] = 0;
        wire[pos++] = 0;
        put_u64_le( wire + pos, id_items ); pos += 8;
        put_u64_le( wire + pos, id_key );   pos += 8;
        put_u64_le( wire + pos, id_val );   pos += 8;
        put_u64_le( wire + pos, 3 );        pos += 8;

        rbytes = simple_load_measure( wire, pos );
        CHECK( rbytes > 0 );
        region = (uint8_t *) calloc( 1, (size_t) rbytes + 64 );
        root = simple_load( (uint8_t *) ( ( (uintptr_t) region + 63u ) & ~(uintptr_t) 63u ), rbytes, wire, pos, &rep );
        CHECK( root == NULL );
        CHECK( REPEATED_REFUSAL( rep ) );
        free( region );
    }

    /* 5. exact_then_exact: the key id twice, both u32 (8, 9): no kind is wrong,
       the repeat alone is the damage. 6 is the same entry with the key once,
       which reads: the control that 5's framing is otherwise sound. */
    {
        int repeat;
        for ( repeat = 1; repeat >= 0; repeat-- )
        {
            uint8_t wire[64];
            int pos = 0;
            TableReport rep = { 0 };
            int64_t rbytes;
            uint8_t * region;
            const Simple * root;

            wire[pos++] = 1;
            wire[pos++] = 1; wire[pos++] = 14; wire[pos++] = (uint8_t) ( repeat ? 22 : 16 );
            wire[pos++] = 13; wire[pos++] = 1;
            wire[pos++] = (uint8_t) ( repeat ? 19 : 13 );
            wire[pos++] = 2; wire[pos++] = 8; wire[pos++] = 9; wire[pos++] = 0; wire[pos++] = 0; wire[pos++] = 0;
            if ( repeat )
            {
                wire[pos++] = 2; wire[pos++] = 8; wire[pos++] = 9; wire[pos++] = 0; wire[pos++] = 0; wire[pos++] = 0;
            }
            wire[pos++] = 3; wire[pos++] = 4; wire[pos++] = 42; wire[pos++] = 0; wire[pos++] = 0; wire[pos++] = 0;
            wire[pos++] = 0;
            wire[pos++] = 0;
            put_u64_le( wire + pos, id_items ); pos += 8;
            put_u64_le( wire + pos, id_key );   pos += 8;
            put_u64_le( wire + pos, id_val );   pos += 8;
            put_u64_le( wire + pos, 3 );        pos += 8;

            rbytes = simple_load_measure( wire, pos );
            CHECK( rbytes > 0 );
            region = (uint8_t *) calloc( 1, (size_t) rbytes + 64 );
            CHECK( region != NULL );
            root = simple_load( (uint8_t *) ( ( (uintptr_t) region + 63u ) & ~(uintptr_t) 63u ), rbytes, wire, pos, &rep );
            if ( repeat )
            {
                CHECK( root == NULL );
                CHECK( rep.refused == 1 && rep.refuse_reason == SCHEMA_TABLE_REFUSE_WIRE_DAMAGED );
            }
            else
            {
                const int32_t * found;
                CHECK( root != NULL );
                CHECK( rep.refused == 0 && rep.refuse_reason == SCHEMA_TABLE_REFUSE_OK );
                CHECK( root->items.count == 1 );
                found = simple_items_find( &root->items, 9u );
                CHECK( found != NULL && *found == 42 );
            }
            free( region );
        }
    }

    return 0;
}
`, vecInit)

	runCProbe(t, schema, probe)
}
