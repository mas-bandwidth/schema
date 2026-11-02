package cpptable

import (
	"strings"
	"testing"
)

// A repeated map key inside an entry body selects its final occurrence,
// including when an earlier occurrence had an incompatible kind and a later
// occurrence uses a widening integer kind (docs/SPEC-TABLES.md §2.8, §3, §4).
func TestCppMapRepeatedKeyLastKindWins(t *testing.T) {
	u := loadUnitFromSource(t, `package probe
table Root
{
    entries map[uint32]int32
}
`)
	files, header := generateCPPHeaders(t, u)
	driver := strings.ReplaceAll(`#include "HEADER"
#include <stdint.h>
#include <stdio.h>
#include <string.h>

using namespace probe;

static void append_u64( uint8_t * dst, int64_t & n, uint64_t v )
{
    for ( int i = 0; i < 8; i++ )
    {
        dst[n++] = (uint8_t) ( ( v >> ( i * 8 ) ) & 0xffu );
    }
}

static int64_t build_wire( uint8_t * out, const uint8_t * keys, int64_t keys_len )
{
    int64_t n = 0;
    const int64_t entry_len = keys_len + 1;
    const int64_t payload_len = 3 + entry_len;
    out[n++] = 1; // form 1
    out[n++] = 1; // field ref 1 (entries)
    out[n++] = 14; // tkArray
    out[n++] = (uint8_t) payload_len;
    out[n++] = 13; // tkTable
    out[n++] = 1; // 1 entry
    out[n++] = (uint8_t) entry_len;
    memcpy( out + n, keys, (size_t) keys_len );
    n += keys_len;
    out[n++] = 0; // entry terminator
    out[n++] = 0; // root terminator
    append_u64( out, n, RootTableInfo.fields[0].id ); // ref 1: entries
    append_u64( out, n, RootEntriesEntryTableInfo.fields[0].id ); // ref 2: key
    append_u64( out, n, 2 ); // 2 id table entries
    return n;
}

static bool check_case( const char * name, const uint8_t * keys, int64_t keys_len, int32_t want_count, int32_t want_mismatch, int32_t want_widened )
{
    uint8_t wire[128];
    const int64_t wire_len = build_wire( wire, keys, keys_len );

    // 1. Builder load
    {
        RootBuilder builder;
        TableReport report;
        if ( !RootLoadBuilderTolerant( builder, wire, wire_len, &report ) )
        {
            printf( "%s: RootLoadBuilderTolerant failed\n", name );
            return false;
        }
        Root * root = builder.GetRoot();
        if ( root == NULL || report.malformed || root->entries.count != want_count || report.kind_mismatch != want_mismatch || report.widened != want_widened )
        {
            printf( "%s builder: count=%d mismatch=%d widened=%d malformed=%d\n",
                name, root ? (int) root->entries.count : -1, (int) report.kind_mismatch, (int) report.widened, (int) report.malformed );
            return false;
        }
        if ( want_count == 1 )
        {
            const int32_t * found = RootEntriesFind( builder.arena, root->entries, 7u );
            if ( found == NULL || *found != 0 )
            {
                printf( "%s builder: key 7 not found\n", name );
                return false;
            }
        }
    }

    // 2. Region load
    {
        TableRefuseReason measure_reason = ok;
        const int64_t region_bytes = RootLoadMeasure( wire, wire_len, NULL, &measure_reason );
        if ( region_bytes < 0 )
        {
            printf( "%s region: RootLoadMeasure refused (%d)\n", name, (int) measure_reason );
            return false;
        }
        alignas( 8 ) uint8_t region[4096];
        TableReport report;
        const Root * root = RootLoadTolerant( region, region_bytes, wire, wire_len, &report );
        if ( root == NULL )
        {
            printf( "%s region: RootLoadTolerant returned NULL\n", name );
            return false;
        }
        if ( report.malformed || root->entries.count != want_count || report.kind_mismatch != want_mismatch || report.widened != want_widened )
        {
            printf( "%s region: count=%d mismatch=%d widened=%d malformed=%d\n",
                name, (int) root->entries.count, (int) report.kind_mismatch, (int) report.widened, (int) report.malformed );
            return false;
        }
        if ( want_count == 1 )
        {
            const int32_t * found = root->entries.Find( 7u );
            if ( found == NULL || *found != 0 )
            {
                printf( "%s region: key 7 not found\n", name );
                return false;
            }
        }
    }
    return true;
}

int main()
{
    static const uint8_t bad_then_widened[] = { 2, 9, 0, 0, 0, 0, 0, 0, 0, 0, 2, 6, 7 };
    static const uint8_t widened_then_bad[] = { 2, 6, 7, 2, 9, 0, 0, 0, 0, 0, 0, 0, 0 };
    static const uint8_t bad_then_exact[]   = { 2, 9, 0, 0, 0, 0, 0, 0, 0, 0, 2, 8, 7, 0, 0, 0 };

    if ( !check_case( "bad_then_widened", bad_then_widened, sizeof( bad_then_widened ), 1, 1, 1 ) ) return 1;
    if ( !check_case( "widened_then_bad", widened_then_bad, sizeof( widened_then_bad ), 0, 1, 1 ) ) return 1;
    if ( !check_case( "bad_then_exact", bad_then_exact, sizeof( bad_then_exact ), 1, 1, 0 ) ) return 1;
    return 0;
}
`, "HEADER", header)
	if out, err := writeAndCompile(t, files, header, driver); err != nil {
		t.Fatalf("driver failed: %v\n%s", err, out)
	}
}
