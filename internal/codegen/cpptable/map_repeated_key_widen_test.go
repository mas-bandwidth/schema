package cpptable

import (
	"strings"
	"testing"
)

// A repeated map key inside an entry body is damage under the strict-only
// read (docs/plans/strict-only.md, decision 5): the load refuses the whole
// value, including when one occurrence had an incompatible kind and another
// uses a widening integer kind (docs/SPEC-TABLES.md §2.8, §3, §4).
func TestCppMapRepeatedKeyRefused(t *testing.T) {
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

static bool check_case( const char * name, const uint8_t * keys, int64_t keys_len )
{
    uint8_t wire[128];
    const int64_t wire_len = build_wire( wire, keys, keys_len );

    // 1. Builder load refuses the whole value.
    {
        RootBuilder builder;
        TableReport report;
        if ( RootLoadBuilder( builder, wire, wire_len, &report ) )
        {
            printf( "%s: RootLoadBuilder accepted a repeated map key\n", name );
            return false;
        }
        if ( !report.refused )
        {
            printf( "%s builder: repeated key was not a refusal\n", name );
            return false;
        }
    }

    // 2. Region load refuses the whole value.
    {
        alignas( 8 ) uint8_t region[4096];
        TableReport report;
        const Root * root = RootLoad( region, (int64_t) sizeof( region ), wire, wire_len, &report );
        if ( root != NULL || !report.refused )
        {
            printf( "%s region: repeated key was not refused\n", name );
            return false;
        }
    }
    return true;
}

int main()
{
    static const uint8_t bad_then_widened[] = { 2, 9, 0, 0, 0, 0, 0, 0, 0, 0, 2, 6, 7 };
    static const uint8_t widened_then_bad[] = { 2, 6, 7, 2, 9, 0, 0, 0, 0, 0, 0, 0, 0 };
    static const uint8_t bad_then_exact[]   = { 2, 9, 0, 0, 0, 0, 0, 0, 0, 0, 2, 8, 7, 0, 0, 0 };

    if ( !check_case( "bad_then_widened", bad_then_widened, sizeof( bad_then_widened ) ) ) return 1;
    if ( !check_case( "widened_then_bad", widened_then_bad, sizeof( widened_then_bad ) ) ) return 1;
    if ( !check_case( "bad_then_exact", bad_then_exact, sizeof( bad_then_exact ) ) ) return 1;
    return 0;
}
`, "HEADER", header)
	if out, err := writeAndCompile(t, files, header, driver); err != nil {
		t.Fatalf("driver failed: %v\n%s", err, out)
	}
}
