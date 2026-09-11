// THE FIELD HEADER AND THE 128-BIT PUT, HELD TO MAIN'S BYTES
// (docs/SPEC-TABLES.md §3).
//
// A FIELD HEADER IS A REFERENCE AND THE KIND BYTE BEHIND IT, and the emitter
// used to write the pair as `w.putleb( ref ); w.put8( kind );` — two calls,
// two capacity tests, two memcpy of one byte each in the common case, because
// a reference below 128 is a single LEB128 byte. TableWriter::header now
// assembles that two-byte pair in a stack buffer and hands it to raw once; a
// reference at 128 or above still goes through putleb then put8, which is the
// old spelling exactly. put128 does the same for the two 64-bit lanes of a
// 128-bit value: sixteen bytes, low half first, one raw.
//
// NEITHER IS ALLOWED TO MOVE ONE BYTE, and the only proof of that worth having
// is the bytes themselves, against the bytes the emitter wrote BEFORE header
// existed. testdata/headerput128/golden.txt was produced by main's emitter over
// this schema and these values (regenerate with SCHEMA_UPDATE_HEADER_GOLDEN=1,
// which is how it was pinned); this test generates the header with THIS
// emitter, saves the same values, and compares.
//
// WHAT THE VALUES REACH FOR, and why each one is here:
//
//   - A NESTED TABLE OF 200 FIELDS whose set count is swept from 0 to 200. The
//     id table interns in first-use order, so the number of fields set in that
//     nested table is exactly the number of references that separate the root
//     fields declared BEFORE it from the root fields declared AFTER it. The
//     sweep walks the 127/128 boundary through the late block one field at a
//     time: the same field rides under the SHORT arm in one row of the golden
//     and under the LONG arm in the next, and the two spellings have to agree
//     for the row to match.
//   - 128-BIT FIELDS, signed and unsigned, at the root, inside the nested
//     table, inside array elements and inside a fixed array — so put128 rides
//     under every framing the emitter has for it, including values that fill
//     both lanes (2^128-1, 1<<127, 1<<64).
//   - EVERY HEADER-BEARING KIND the value form reaches: a scalar, a string, a
//     bytes blob, an enum, a nested table, a counted array of tables, a fixed
//     array of tables, an enum-keyed array, and a union — the union twice, on
//     two different arms, so the ARM header (its own reference and kind) rides
//     under both arms of the header too.
//
// The driver also holds the save to its own contract at each value: Save's
// length is Measure's, a load and a re-save reproduce the bytes, and EVERY
// capacity below the full length answers -1 — the whole sweep, 0 through n-1,
// so a capacity that cuts a two-byte header in half is in it by construction.
// All of it under -DNDEBUG, because the `overflow` flag is not a debug check.
//
// Beside the golden, two things the pin cannot see, and so a second driver
// that only THIS emitter compiles: header against `putleb` then `put8` at
// references either side of 128 up to 2^64-1, and put128 against two put64,
// each of them byte for byte and each of them swept across every capacity that
// cannot hold the value. And a negative control: the same golden driver
// compiled against a header whose two-byte arm writes the pair BACKWARDS must
// not reproduce the pin.
package compiler

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// headerProbeWideFields is the nested table's field count. It is above 127 on
// purpose and swept below and above it: the id table's references are LEB128
// and header's two arms part at 128, so a closure that interns more than 127
// entries is the only place the arm a given field takes can MOVE.
const headerProbeWideFields = 200

func headerProbeSchema() string {
	var b strings.Builder
	b.WriteString(`package headerprobe

enum Grade { Bronze, Silver, Gold }

type Buff { multiplier float32 = 1.0 }

type Debuff { amount int32 = 0 }

union Effect
{
    buff   Buff
    debuff Debuff
}

`)
	b.WriteString("// 200 fields: the count of them SET is the count of references that\n")
	b.WriteString("// separate the root's early block from its late one\nfixed table Wide\n{\n")
	for i := range headerProbeWideFields {
		fmt.Fprintf(&b, "    f%03d int32\n", i)
	}
	b.WriteString("}\n\n")
	b.WriteString(`fixed table Leaf
{
    n   int32
    big uint128
    neg int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
}

fixed table Slot
{
    power float32 = 1.0
    tag   int32
}

fixed table Root
{
    head        uint32
    early_big   uint128
    early_neg   int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    early_text  string(24)
    early_blob  bytes(24)
    early_pick  Effect
    early_leafs [..8]Leaf
    early_slots [Grade]Slot
    early_leaf  Leaf
    block       Wide
    late_big    uint128
    late_neg    int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    late_text   string(24)
    late_blob   bytes(24)
    late_pick   Effect
    late_leafs  [..8]Leaf
    late_slots  [Grade]Slot
    late_leaf   Leaf
    late_grade  Grade
    late_fixed  [2]Leaf
}
`)
	return b.String()
}

// headerProbeGoldenDriver saves the values and prints the bytes. It names
// nothing this branch added, because MAIN'S EMITTER compiled it to make the pin.
func headerProbeGoldenDriver() string {
	var b strings.Builder
	b.WriteString(`#include <stdio.h>
#include <stdint.h>
#include <string.h>
#include "ProbeTable.h"

using namespace headerprobe;

static int failures = 0;
static uint8_t buffer[ 1 << 16 ];
static uint8_t twin[ 1 << 16 ];
static Root value;
static Root back;
static uint64_t max_ids = 0;
static uint64_t min_ids = ~(uint64_t) 0;

static void set_wide( Wide & w, int which, int32_t v )
{
    switch ( which )
    {
`)
	for i := range headerProbeWideFields {
		fmt.Fprintf(&b, "        case %d: w.f%03d = v; break;\n", i, i)
	}
	b.WriteString(`        default: break;
    }
}

// THE ID TABLE IS THE LAST THING IN THE FILE and its COUNT is the last eight
// bytes of it (docs/SPEC-TABLES.md §3). Reading it back is how this driver says
// which arm of the header rode: references are the id table's positions, so a
// file with more than 127 entries wrote references on both sides of 128.
static uint64_t id_count( const uint8_t * wire, int64_t n )
{
    uint64_t v = 0;
    for ( int i = 0; i < 8; i++ ) { v |= (uint64_t) wire[ n - 8 + i ] << ( i * 8 ); }
    return v;
}

// the SAME values every run. wide_fields is the count of fields SET in the
// nested table, which is the count of references between the early block and
// the late one: it is what walks the 127/128 boundary through the late fields.
static void build( Root & v, int32_t wide_fields, int32_t leaf_count )
{
    RootReset( v );

    v.head = 7;
    v.early_big = ~serialize::uint128_t( 0 );                 // both lanes full
    v.early_neg = -( serialize::int128_t( 1 ) << 100 );
    memcpy( v.early_text, "early", 5 ); v.early_text_length = 5;
    for ( int32_t i = 0; i < 6; i++ ) { v.early_blob[i] = (uint8_t) ( 0x10 + i ); }
    v.early_blob_length = 6;
    v.early_pick.type = EffectType::Buff;
    v.early_pick.buff.multiplier = 2.5f;
    v.early_leafs_count = leaf_count;
    for ( int32_t i = 0; i < leaf_count; i++ )
    {
        v.early_leafs[i].n = i + 1;
        v.early_leafs[i].big = serialize::uint128_t( 1 ) << ( 64 + i );  // the HIGH lane alone
        v.early_leafs[i].neg = serialize::int128_t( -( i + 1 ) );
    }
    v.early_slots[ Grade::Bronze ].tag = 3;
    v.early_slots[ Grade::Gold ].power = 4.0f;
    v.early_leaf.n = 11;
    v.early_leaf.big = serialize::uint128_t( 1 ) << 127;

    for ( int32_t k = 0; k < wide_fields; k++ ) { set_wide( v.block, (int) k, k + 1 ); }

    v.late_big = serialize::uint128_t( 1 ) << 64;             // the lane seam itself
    v.late_neg = serialize::int128_t( -9 );
    memcpy( v.late_text, "late", 4 ); v.late_text_length = 4;
    for ( int32_t i = 0; i < 3; i++ ) { v.late_blob[i] = (uint8_t) ( 0xa0 + i ); }
    v.late_blob_length = 3;
    v.late_pick.type = EffectType::Debuff;
    v.late_pick.debuff.amount = 17;
    v.late_leafs_count = leaf_count;
    for ( int32_t i = 0; i < leaf_count; i++ )
    {
        v.late_leafs[i].n = 100 + i;
        v.late_leafs[i].big = serialize::uint128_t( i + 1 );
        v.late_leafs[i].neg = -( serialize::int128_t( 1 ) << 120 );
    }
    v.late_slots[ Grade::Silver ].tag = 5;
    v.late_leaf.n = 21;
    v.late_leaf.neg = serialize::int128_t( -21 );
    v.late_grade = Grade::Gold;
    v.late_fixed[0].n = 31;
    v.late_fixed[1].big = serialize::uint128_t( 2 );
}

static void one( int32_t wide_fields, int32_t leaf_count )
{
    build( value, wide_fields, leaf_count );

    const int64_t wrote = RootSave( value, buffer, (int64_t) sizeof( buffer ) );
    if ( wrote < 0 ) { printf( "SAVE FAILED %d/%d\n", wide_fields, leaf_count ); failures++; return; }

    const int64_t measured = RootMeasure( value );
    if ( measured != wrote )
    {
        printf( "MEASURE DISAGREES %d/%d: measure %lld save %lld\n",
                wide_fields, leaf_count, (long long) measured, (long long) wrote );
        failures++;
    }

    // a load and a re-save reproduce the file, which is what says the headers
    // and the body lengths agree with the bytes between them
    TableReport report;
    if ( !RootLoad( back, buffer, wrote, &report ) )
    {
        printf( "LOAD FAILED %d/%d\n", wide_fields, leaf_count ); failures++;
    }
    else if ( report.unknown != 0 || report.kind_mismatch != 0 || report.clamped != 0 || report.malformed )
    {
        printf( "LOAD REPORTED %d/%d\n", wide_fields, leaf_count ); failures++;
    }
    else
    {
        const int64_t again = RootSave( back, twin, (int64_t) sizeof( twin ) );
        if ( again != wrote || memcmp( twin, buffer, (size_t) wrote ) != 0 )
        {
            printf( "ROUND TRIP MOVED %d/%d\n", wide_fields, leaf_count ); failures++;
        }
    }

    // THE OVERFLOW FLAG IS NOT A DEBUG CHECK (this driver compiles -DNDEBUG).
    // EVERY capacity short of the whole answers -1 — the empty buffer, one byte
    // short, and every cut in between, which is where a capacity that splits a
    // two-byte header down the middle lives.
    for ( int64_t cap = 0; cap < wrote; cap++ )
    {
        if ( RootSave( value, twin, cap ) != -1 )
        {
            printf( "SHORT BUFFER ACCEPTED %d/%d at %lld\n", wide_fields, leaf_count, (long long) cap );
            failures++;
            break;
        }
    }

    const uint64_t ids = id_count( buffer, wrote );
    if ( ids > max_ids ) { max_ids = ids; }
    if ( ids < min_ids ) { min_ids = ids; }

    printf( "wide=%d leafs=%d ids=%llu len=%lld ", wide_fields, leaf_count,
            (unsigned long long) ids, (long long) wrote );
    for ( int64_t i = 0; i < wrote; i++ ) { printf( "%02x", buffer[i] ); }
    printf( "\n" );
}

int main()
{
    static const int32_t wides[] = { 0, 1, 50, 100, 108, 110, 112, 114, 116, 118,
                                     120, 122, 124, 126, 127, 128, 130, 150, 200 };
    for ( unsigned k = 0; k < sizeof( wides ) / sizeof( wides[0] ); k++ )
    {
        one( wides[k], 0 );
        one( wides[k], 3 );
        one( wides[k], 8 );
    }
    // BOTH ARMS RODE: a file with more than 127 id-table entries named
    // references on both sides of 128, and every file named at least one.
    if ( max_ids <= 127 ) { printf( "NO LONG ARM: max ids %llu\n", (unsigned long long) max_ids ); failures++; }
    if ( min_ids < 1 ) { printf( "NO SHORT ARM\n" ); failures++; }
    printf( "ids min=%llu max=%llu\n", (unsigned long long) min_ids, (unsigned long long) max_ids );
    return failures == 0 ? 0 : 1;
}
`)
	return b.String()
}

// headerProbeArmDriver names header and put128 DIRECTLY and holds each to the
// call pair it replaced. Only this branch's emitter compiles it.
const headerProbeArmDriver = `#include <stdio.h>
#include <stdint.h>
#include <string.h>
#include "ProbeTable.h"

using namespace headerprobe;

static int failures = 0;

// header( ref, kind ) against putleb( ref ) then put8( kind ), byte for byte,
// at references either side of 128 and out to the end of the range
static void header_matches_the_pair()
{
    static const uint64_t refs[] = { 0, 1, 2, 126, 127, 128, 129, 255, 256,
                                     16383, 16384, 0xffffffffull, 0xffffffffffffffffull };
    static const uint8_t kinds[] = { 1, 4, 12, 14, 30, 32 };
    for ( unsigned r = 0; r < sizeof( refs ) / sizeof( refs[0] ); r++ )
    {
        for ( unsigned k = 0; k < sizeof( kinds ) / sizeof( kinds[0] ); k++ )
        {
            uint8_t a[32] = {};
            uint8_t b[32] = {};
            TableWriter wa( a, (int64_t) sizeof( a ) );
            TableWriter wb( b, (int64_t) sizeof( b ) );
            wa.header( refs[r], kinds[k] );
            wb.putleb( refs[r] );
            wb.put8( kinds[k] );
            if ( wa.overflow || wb.overflow || wa.offset != wb.offset ||
                 memcmp( a, b, (size_t) wb.offset ) != 0 )
            {
                printf( "HEADER MOVED ref %llu kind %u\n", (unsigned long long) refs[r], (unsigned) kinds[k] );
                failures++;
                continue;
            }
            // EVERY capacity that cannot hold the header raises overflow, under
            // the IDENTICAL PREDICATE the call pair raised it under — including
            // capacity 1 against a two-byte header, the cut down its middle.
            // The SHORT arm is one raw, so it leaves the buffer alone; the LONG
            // arm is still putleb then put8 and behaves exactly as that pair
            // does, partial write and all.
            for ( int64_t cap = 0; cap < wa.offset; cap++ )
            {
                uint8_t c[32] = {};
                uint8_t d[32] = {};
                TableWriter wc( c, cap );
                TableWriter wd( d, cap );
                wc.header( refs[r], kinds[k] );
                wd.putleb( refs[r] );
                wd.put8( kinds[k] );
                if ( !wc.overflow || !wd.overflow )
                {
                    printf( "HEADER FIT ref %llu kind %u at cap %lld\n",
                            (unsigned long long) refs[r], (unsigned) kinds[k], (long long) cap );
                    failures++;
                    continue;
                }
                if ( refs[r] < 128 )
                {
                    if ( wc.offset != 0 )
                    {
                        printf( "HEADER SHORT ARM WROTE ref %llu kind %u at cap %lld\n",
                                (unsigned long long) refs[r], (unsigned) kinds[k], (long long) cap );
                        failures++;
                    }
                }
                else if ( wc.offset != wd.offset || memcmp( c, d, (size_t) cap ) != 0 )
                {
                    printf( "HEADER LONG ARM DIVERGED ref %llu kind %u at cap %lld\n",
                            (unsigned long long) refs[r], (unsigned) kinds[k], (long long) cap );
                    failures++;
                }
            }
        }
    }
}

// put128( lo, hi ) against put64( lo ) then put64( hi ), the low lane first
static void put128_matches_two_put64()
{
    static const uint64_t lanes[] = { 0, 1, 0x8000000000000000ull, 0x0123456789abcdefull,
                                      0xffffffffffffffffull };
    for ( unsigned i = 0; i < sizeof( lanes ) / sizeof( lanes[0] ); i++ )
    {
        for ( unsigned j = 0; j < sizeof( lanes ) / sizeof( lanes[0] ); j++ )
        {
            uint8_t a[32] = {};
            uint8_t b[32] = {};
            TableWriter wa( a, (int64_t) sizeof( a ) );
            TableWriter wb( b, (int64_t) sizeof( b ) );
            wa.put128( lanes[i], lanes[j] );
            wb.put64( lanes[i] );
            wb.put64( lanes[j] );
            if ( wa.overflow || wb.overflow || wa.offset != 16 || wb.offset != 16 ||
                 memcmp( a, b, 16 ) != 0 )
            {
                printf( "PUT128 MOVED %llu/%llu\n", (unsigned long long) lanes[i], (unsigned long long) lanes[j] );
                failures++;
                continue;
            }
            for ( int64_t cap = 0; cap < 16; cap++ )
            {
                uint8_t c[32] = {};
                TableWriter wc( c, cap );
                wc.put128( lanes[i], lanes[j] );
                if ( !wc.overflow || wc.offset != 0 )
                {
                    printf( "PUT128 FIT at cap %lld\n", (long long) cap );
                    failures++;
                }
            }
        }
    }
}

int main()
{
    header_matches_the_pair();
    put128_matches_two_put64();
    printf( "arms ok\n" );
    return failures == 0 ? 0 : 1;
}
`

// TestCppTableHeaderAndPut128Bytes holds the generated save path to the bytes
// main's emitter wrote, over values that ride BOTH arms of the field header and
// carry 128-bit fields under every framing the emitter has for them.
func TestCppTableHeaderAndPut128Bytes(t *testing.T) {
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("the generated save path is C++: no c++ on PATH")
	}
	runtime, err := filepath.Abs("../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runtime, "serialize.h")); err != nil {
		t.Skip("serialize runtime header not at the sibling path")
	}
	pinning := os.Getenv("SCHEMA_UPDATE_HEADER_GOLDEN") == "1"

	files, err := New().Generate(unitFromSource(t, headerProbeSchema()), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	header := string(files["ProbeTable.h"])
	// the writer the emitter grew has to carry header and put128 AT ALL, or the
	// comparison below would pass by proving nothing. Not while PINNING: the pin
	// is taken from the emitter that had neither.
	if !pinning {
		for _, want := range []string{
			"void header( uint64_t ref, uint8_t kind )",
			"if ( ref < 128 )",
			"uint8_t b[2] = { uint8_t( ref ), kind };",
			"void put128( uint64_t lo, uint64_t hi )",
			"uint8_t b[16] = { uint8_t( lo ), uint8_t( lo >> 8 )",
			"raw( b, 16 );",
			"w.header( ",
		} {
			if !strings.Contains(header, want) {
				t.Fatalf("the generated save path does not carry %q — this test would prove nothing", want)
			}
		}
		// and no field header may still ride as the two-call pair
		if strings.Contains(header, "); w.put8( ") && strings.Contains(header, "w.putleb( ref_") {
			t.Fatal("a field header still rides as putleb then put8")
		}
		if strings.Contains(header, "void put128( uint64_t lo, uint64_t hi ) { put64( lo ); put64( hi ); }") {
			t.Fatal("put128 still writes its lanes through two put64")
		}
	}

	golden := filepath.Join("testdata", "headerput128", "golden.txt")
	out := headerProbeRun(t, cxx, runtime, dir, "golden", headerProbeGoldenDriver())
	if pinning {
		if err := os.MkdirAll(filepath.Dir(golden), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, out, 0600); err != nil {
			t.Fatal(err)
		}
		t.Logf("pinned %s (%d bytes)", golden, len(out))
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read %s: %v", golden, err)
	}
	headerProbeSame(t, out, want)

	// header and put128 against the calls they replaced, which the pin cannot
	// see because main's emitter has neither
	headerProbeRun(t, cxx, runtime, dir, "arms", headerProbeArmDriver)

	// THE NEGATIVE CONTROL: the same driver against a writer whose two-byte arm
	// puts the kind byte first. It must not reproduce the pin.
	t.Run("swapped header bytes go red", func(t *testing.T) {
		bad := t.TempDir()
		for name, data := range files {
			text := string(data)
			if name == "ProbeTable.h" {
				text = strings.Replace(text,
					"uint8_t b[2] = { uint8_t( ref ), kind };",
					"uint8_t b[2] = { kind, uint8_t( ref ) };", 1)
				if text == string(data) {
					t.Fatal("the control could not swap the header's two bytes")
				}
			}
			if err := os.WriteFile(filepath.Join(bad, name), []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
		}
		got := headerProbeBuildRun(t, cxx, runtime, bad, "control", headerProbeGoldenDriver())
		if string(got) == string(want) {
			t.Fatal("the golden did not move when the header wrote its two bytes backwards")
		}
	})
}

// headerProbeRun compiles and runs one driver against the generated header and
// requires it to succeed.
func headerProbeRun(t *testing.T, cxx, runtime, dir, name, source string) []byte {
	t.Helper()
	out, err := headerProbeCompileRun(t, cxx, runtime, dir, name, source)
	if err != nil {
		t.Fatalf("%s: %v\n%s", name, err, out)
	}
	return out
}

// headerProbeBuildRun is the same for a driver that is EXPECTED to disagree:
// a nonzero exit is one of the ways it may say so.
func headerProbeBuildRun(t *testing.T, cxx, runtime, dir, name, source string) []byte {
	t.Helper()
	out, _ := headerProbeCompileRun(t, cxx, runtime, dir, name, source)
	return out
}

func headerProbeCompileRun(t *testing.T, cxx, runtime, dir, name, source string) ([]byte, error) {
	t.Helper()
	main := filepath.Join(dir, name+"_main.cpp")
	if err := os.WriteFile(main, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, name)
	// -DNDEBUG on purpose: the release build is where a write-side check may
	// compile out, and the overflow flag must not
	args := []string{
		"-std=c++17", "-O2", "-DNDEBUG", "-Wall", "-Wextra", "-Werror", "-Wshadow",
		"-ffp-contract=off", "-I", dir, "-I", runtime, main, "-o", bin,
	}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile %s: %v\n%s", name, err, out)
	}
	return exec.Command(bin).CombinedOutput()
}

// headerProbeSame reports the first line that moved, which is the line whose
// hex says which header or which lane went somewhere else.
func headerProbeSame(t *testing.T, got, want []byte) {
	t.Helper()
	if string(got) == string(want) {
		return
	}
	gotLines, wantLines := strings.Split(string(got), "\n"), strings.Split(string(want), "\n")
	for i := range gotLines {
		if i >= len(wantLines) {
			t.Fatalf("the save path grew a line the pin does not have: %s", gotLines[i])
		}
		if gotLines[i] != wantLines[i] {
			t.Fatalf("THE SAVED BYTES MOVED at line %d\n  pinned: %s\n     got: %s", i+1, wantLines[i], gotLines[i])
		}
	}
	t.Fatalf("the save path lost a line the pin has: %s", wantLines[len(gotLines)])
}
