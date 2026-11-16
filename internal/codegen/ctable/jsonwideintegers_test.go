// The JSON form's integers (docs/SPEC-TABLES.md §16.2, docs/rulings.json
// json-wide-integers-are-strings and json-integer-token-only): an integer
// wider than 32 bits is written as a STRING of decimal digits and reads as a
// string only; every integer field takes an integer token only — no
// fraction, no exponent, no negative zero — and its range is tested on the
// exact value, never through a double. The vectors under
// testdata/wire/json-integers/ are the corpus.
package ctable

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const cJsonWideIntegersSchema = `package jsonintegers

table Integers
{
    small   int32
    wide    int64
    uwide   uint64
    wide128 int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    ranged  int64 | min = 0, max = 9007199254740993
    frac    fixed(48, 16) | min = -10, max = 10
    counts  map[uint64]int32
}


enum Slot { Alpha, Beta }
union Arm { wide int64 }
table Sites {
    plain int64
    array [..3]int64
    opt ?uint64
    arm Arm
    vals map[uint64]uint64
    keyed [Slot]int64
    u128 uint128
    b33 bits(33)
    b64 bits(64)
    f128 fixed(64, 64) | min = -10, max = 10
    narrow int32
    fixed32 fixed(16, 16) | min = -10, max = 10
}

table FixedWide
{
    tiny fixed(2, 62) | min = -1, max = 1
    huge fixed(2, 126) | min = -1, max = 1
}
`

type cJsonWideRow struct {
	name        string
	text        []byte
	read        bool
	canonical   []byte
	numberToken bool
}

// TestCJsonWideIntegersAreStrings holds every row of the json-integers
// manifest through the generated C writer and reader. A read row lands every
// value, and the writer's canonical text is compared byte for byte with the
// committed canonical text; a refused row refuses the whole load.
func TestCJsonWideIntegersAreStrings(t *testing.T) {
	t.Parallel()

	const dir = "../../../testdata/wire/json-integers"
	canonical, err := os.ReadFile(filepath.Join(dir, "canonical.read.json"))
	if err != nil {
		t.Fatal(err)
	}
	// §16.1: the canonical text ends with exactly one newline, the byte the
	// writer emits and every reader accepts with or without. The vector file
	// holds the text without it.
	canonicalText := append(append([]byte(nil), canonical...), '\n')
	f, err := os.Open(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var rows []cJsonWideRow
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 5 {
			t.Fatalf("MANIFEST.tsv: want five columns, got %q", line)
		}
		name, jsonFile, verdict := cols[0], cols[2], cols[3]
		if verdict != "read" && verdict != "refused" {
			t.Fatalf("%s: verdict %q", name, verdict)
		}
		text, err := os.ReadFile(filepath.Join(dir, jsonFile))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		rows = append(rows, cJsonWideRow{name: name, text: text, read: verdict == "read", canonical: canonicalText})
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("the manifest names no rows")
	}
	tokens, err := os.Open(filepath.Join(dir, "NUMBER-TOKEN.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer tokens.Close()
	scanner = bufio.NewScanner(tokens)
	count := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 5 || cols[3] != "refused" {
			t.Fatalf("NUMBER-TOKEN.tsv: invalid row %q", line)
		}
		text, err := os.ReadFile(filepath.Join(dir, cols[2]))
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, cJsonWideRow{name: cols[0], text: text, numberToken: true})
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("NUMBER-TOKEN.tsv names no rows")
	}

	var probe strings.Builder
	probe.WriteString(`#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "ProbeTable.h"

typedef struct Row {
    const char * name;
    const char * text;
    int read;
    const char * canonical;
    int number_token;
} Row;

static const Row rows[] = {
`)
	for _, row := range rows {
		read := 0
		if row.read {
			read = 1
		}
		probe.WriteString("    { " + cStringLiteral(row.name) + ", " + cStringLiteral(string(row.text)) + ", " +
			strconv.Itoa(read) + ", " + cStringLiteral(string(row.canonical)) + ", " + strconv.Itoa(func() int {
			if row.numberToken {
				return 1
			}
			return 0
		}()) + " },\n")
	}
	probe.WriteString(`};

static int check_fields( const Integers * v )
{
    TableSequenceCursor c = integers_counts_each( NULL, &v->counts );
    const IntegersCountsEntry * e;
    int count = 0, zero = 0, big = 0;
    if ( v->small != 7 ) { fprintf( stderr, "small=%d\n", (int) v->small ); return 1; }
    if ( v->wide != -42 ) { fprintf( stderr, "wide=%lld\n", (long long) v->wide ); return 1; }
    if ( v->uwide != UINT64_C( 18446744073709551615 ) ) { fprintf( stderr, "uwide=%llu\n", (unsigned long long) v->uwide ); return 1; }
    if ( !serialize_int128_equal( v->wide128, serialize_int128_make( UINT64_C( 9223372036854775807 ), UINT64_C( 18446744073709551615 ) ) ) ) { fprintf( stderr, "wide128\n" ); return 1; }
    if ( v->ranged != INT64_C( 9007199254740993 ) ) { fprintf( stderr, "ranged=%lld\n", (long long) v->ranged ); return 1; }
    if ( v->frac != INT64_C( 32768 ) ) { fprintf( stderr, "frac=%lld\n", (long long) v->frac ); return 1; }
    while ( ( e = (const IntegersCountsEntry *) table_sequence_next( &c ) ) != NULL )
    {
        count++;
        if ( e->key == 0 && e->value == 1 ) { zero = 1; }
        if ( e->key == UINT64_C( 9007199254740993 ) && e->value == 2 ) { big = 1; }
    }
    if ( count != 2 || !zero || !big ) { fprintf( stderr, "counts\n" ); return 1; }
    return 0;
}

static int check_fixed_wide( void );

typedef struct SiteCase { const char * name; const char * text; int read; } SiteCase;
static const SiteCase site_cases[] = {
    { "plain number", "{\"plain\":5}", 0 },
    { "plain string", "{\"plain\":\"5\"}", 1 },
    { "array number", "{\"array\":[\"1\",2]}", 0 },
    { "array string", "{\"array\":[\"1\",\"2\"]}", 1 },
    { "optional number", "{\"opt\":7}", 0 },
    { "optional string", "{\"opt\":\"7\"}", 1 },
    { "union number", "{\"arm\":{\"wide\":9}}", 0 },
    { "union string", "{\"arm\":{\"wide\":\"9\"}}", 1 },
    { "map value number", "{\"vals\":{\"18446744073709551615\":5}}", 0 },
    { "map value string", "{\"vals\":{\"18446744073709551615\":\"5\"}}", 1 },
    { "keyed number", "{\"keyed\":{\"Alpha\":5}}", 0 },
    { "keyed string", "{\"keyed\":{\"Alpha\":\"5\"}}", 1 },
    { "uint128 number", "{\"u128\":340282366920938463463374607431768211455}", 0 },
    { "uint128 string", "{\"u128\":\"340282366920938463463374607431768211455\"}", 1 },
    { "bits33 number", "{\"b33\":8589934591}", 0 },
    { "bits33 string", "{\"b33\":\"8589934591\"}", 1 },
    { "bits64 number", "{\"b64\":18446744073709551615}", 0 },
    { "bits64 string", "{\"b64\":\"18446744073709551615\"}", 1 },
    { "fixed128 number", "{\"f128\":0.5}", 0 },
    { "fixed128 string", "{\"f128\":\"0.5\"}", 1 },
    { "int32 number", "{\"narrow\":-7}", 1 },
    { "fixed32 exponent", "{\"fixed32\":15e-1}", 1 },
};
static int check_sites( void )
{
    size_t i;
    int failures = 0;
    for ( i = 0; i < sizeof( site_cases ) / sizeof( site_cases[0] ); i++ )
    {
        const SiteCase * c = &site_cases[i];
        SitesBuilder b;
        TableReport r;
        int ok;
        memset( &r, 0, sizeof( r ) );
        if ( !sites_builder_init( &b ) ) { return 1; }
        ok = sites_from_json( &b, c->text, (int64_t) strlen( c->text ), &r );
        if ( c->read ? ( !ok || r.refused ) : ( ok || !r.refused || r.refuse_reason != SCHEMA_TABLE_REFUSE_KIND_MISMATCH ) )
        {
            fprintf( stderr, "FAIL site %s: ok=%d refused=%d reason=%d\n", c->name, ok, r.refused, (int) r.refuse_reason );
            failures++;
        }
        sites_builder_shutdown( &b );
    }
    return failures;
}

int main( void )
{
    int failures = 0;
    size_t i;
    for ( i = 0; i < sizeof( rows ) / sizeof( rows[0] ); i++ )
    {
        const Row * row = &rows[i];
        IntegersBuilder b;
        TableReport r;
        int ok;
        memset( &r, 0, sizeof( r ) );
        if ( !integers_builder_init( &b ) ) { fprintf( stderr, "FAIL %s: no builder\n", row->name ); return 1; }
        ok = integers_from_json( &b, row->text, (int64_t) strlen( row->text ), &r );
        if ( row->read )
        {
            const Integers * root;
            int64_t n;
            char * text;
            if ( !ok || r.refused ) { fprintf( stderr, "FAIL %s: refused a read row\n", row->name ); failures++; integers_builder_shutdown( &b ); continue; }
            if ( !integers_builder_lock( &b ) ) { fprintf( stderr, "FAIL %s: lock\n", row->name ); failures++; integers_builder_shutdown( &b ); continue; }
            root = (const Integers *) (const void *) b.region;
            if ( root == NULL || check_fields( root ) ) { fprintf( stderr, "FAIL %s: fields\n", row->name ); failures++; integers_builder_shutdown( &b ); continue; }
            n = integers_to_json_measure( root );
            if ( n < 0 ) { fprintf( stderr, "FAIL %s: the writer refused\n", row->name ); failures++; integers_builder_shutdown( &b ); continue; }
            text = (char *) malloc( (size_t) n + 1 );
            if ( text == NULL ) { fprintf( stderr, "FAIL %s: out of memory\n", row->name ); return 1; }
            if ( integers_to_json( root, text, n + 1 ) != n ) { fprintf( stderr, "FAIL %s: measure and write disagree\n", row->name ); failures++; free( text ); integers_builder_shutdown( &b ); continue; }
            text[n] = 0;
            if ( strcmp( text, row->canonical ) != 0 )
            {
                size_t k = 0;
                while ( text[k] != 0 && row->canonical[k] != 0 && text[k] == row->canonical[k] ) { k++; }
                fprintf( stderr, "FAIL %s: the text moved at byte %lu (wrote %lu, want %lu)\nwrote:\n%s\nwant:\n%s\n", row->name, (unsigned long) k, (unsigned long) strlen( text ), (unsigned long) strlen( row->canonical ), text, row->canonical );
                failures++;
            }
            free( text );
        }
        else if ( ok && !r.refused )
        {
            fprintf( stderr, "FAIL %s: accepted a refused row\n", row->name );
            failures++;
        }
        if ( row->number_token && ( !r.refused || r.refuse_reason != SCHEMA_TABLE_REFUSE_KIND_MISMATCH ) )
        {
            fprintf( stderr, "FAIL %s: number token refusal is not kind_mismatch\n", row->name );
            failures++;
        }
        integers_builder_shutdown( &b );
    }
    failures += check_fixed_wide();
    failures += check_sites();
    return failures == 0 ? 0 : 1;
}

/* A wide fixed-point value spells as a string longer than any integer's: the
   64-bit fixed(2, 62) raw 1 is 64 characters, and the 128-bit fixed(2, 126)
   raw 1 is 128. The reader must take its own writer's text, and the writer
   must reproduce the exact spelling. */
static int check_fixed_wide( void )
{
    static const char * input =
        "{\"tiny\":\"0.00000000000000000021684043449710088680149056017398834228515625\","
        "\"huge\":\"0.000000000000000000000000000000000000011754943508222875079687365372222456778186655567720875215087517062784172594547271728515625\"}";
    static const char * canonical =
        "{\n"
        "  \"tiny\": \"0.00000000000000000021684043449710088680149056017398834228515625\",\n"
        "  \"huge\": \"0.000000000000000000000000000000000000011754943508222875079687365372222456778186655567720875215087517062784172594547271728515625\"\n"
        "}\n";
    FixedWideBuilder b;
    TableReport r;
    const FixedWide * root;
    int64_t n;
    char * text;
    int failures = 0;
    memset( &r, 0, sizeof( r ) );
    if ( !fixed_wide_builder_init( &b ) ) { fprintf( stderr, "FAIL fixed: no builder\n" ); return 1; }
    if ( !fixed_wide_from_json( &b, input, (int64_t) strlen( input ), &r ) || r.refused )
    {
        fprintf( stderr, "FAIL fixed: refused its own writer's spelling\n" );
        fixed_wide_builder_shutdown( &b );
        return 1;
    }
    if ( !fixed_wide_builder_lock( &b ) )
    {
        fprintf( stderr, "FAIL fixed: lock\n" );
        fixed_wide_builder_shutdown( &b );
        return 1;
    }
    root = (const FixedWide *) (const void *) b.region;
    if ( root == NULL || root->tiny != INT64_C( 1 ) ||
         !serialize_int128_equal( root->huge, serialize_int128_make( UINT64_C( 0 ), UINT64_C( 1 ) ) ) )
    {
        fprintf( stderr, "FAIL fixed: fields\n" );
        fixed_wide_builder_shutdown( &b );
        return 1;
    }
    n = fixed_wide_to_json_measure( root );
    if ( n < 0 )
    {
        fprintf( stderr, "FAIL fixed: the writer refused\n" );
        fixed_wide_builder_shutdown( &b );
        return 1;
    }
    text = (char *) malloc( (size_t) n + 1 );
    if ( text == NULL ) { fprintf( stderr, "FAIL fixed: out of memory\n" ); fixed_wide_builder_shutdown( &b ); return 1; }
    if ( fixed_wide_to_json( root, text, n + 1 ) != n )
    {
        fprintf( stderr, "FAIL fixed: measure and write disagree\n" );
        free( text );
        fixed_wide_builder_shutdown( &b );
        return 1;
    }
    text[n] = 0;
    if ( strcmp( text, canonical ) != 0 )
    {
        fprintf( stderr, "FAIL fixed: the text moved (wrote %lu, want %lu)\nwrote:\n%swant:\n%s",
            (unsigned long) strlen( text ), (unsigned long) strlen( canonical ), text, canonical );
        failures++;
    }
    free( text );
    fixed_wide_builder_shutdown( &b );
    return failures;
}
`)
	/* The 128-bit storage a generated C header names is serialize.h's lo/hi
	   pair. The wall stages no sibling checkout, so the probe carries the
	   four definitions this unit names; the JSON walk is the subject. */
	shim := t.TempDir()
	if err := os.WriteFile(filepath.Join(shim, "serialize.h"), []byte(cJsonWideSerializeShim), 0600); err != nil {
		t.Fatal(err)
	}
	runCGenerated(t, cJsonWideIntegersSchema, probe.String(), "-I", shim)
}

const cJsonWideSerializeShim = `#ifndef SERIALIZE_H
#define SERIALIZE_H
#include <stdint.h>

typedef struct { uint64_t lo; uint64_t hi; } serialize_int128_t;
typedef struct { uint64_t lo; uint64_t hi; } serialize_uint128_t;

static inline serialize_int128_t serialize_int128_make( uint64_t hi, uint64_t lo )
{
    serialize_int128_t value;
    value.lo = lo;
    value.hi = hi;
    return value;
}

static inline int serialize_int128_equal( serialize_int128_t a, serialize_int128_t b )
{
    return a.lo == b.lo && a.hi == b.hi;
}

static inline serialize_uint128_t serialize_uint128_make( uint64_t hi, uint64_t lo )
{
    serialize_uint128_t value;
    value.lo = lo;
    value.hi = hi;
    return value;
}

static inline int serialize_uint128_equal( serialize_uint128_t a, serialize_uint128_t b )
{
    return a.lo == b.lo && a.hi == b.hi;
}

static inline int serialize_int128_compare( serialize_int128_t a, serialize_int128_t b )
{
    const int64_t ah = (int64_t) a.hi;
    const int64_t bh = (int64_t) b.hi;
    if ( ah != bh ) { return ah < bh ? -1 : 1; }
    if ( a.lo != b.lo ) { return a.lo < b.lo ? -1 : 1; }
    return 0;
}

#endif
`
