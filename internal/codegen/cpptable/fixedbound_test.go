package cpptable

// TestCPPFixedCountOverBoundRefusesByName is the C++ port's red-then-green pin
// for the OWNER RULE (2026-10-15): a fixed-form count or length past the PLAN's
// bound refuses BY NAME, `count_over_bound` / `length_over_bound`, before any
// destination byte is written — it never clamps (docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-ALGORITHM.md §4.5). A counted `[..95]T` and a `string(41)`
// carry forged counts/lengths past their bounds, each with and without slack
// behind the bound, on the IDENTITY plan (a compiler-level probe) and a count
// on a compiled lineage plan (a fixed-form probe), and in every case the load
// refuses by name and writes no destination byte. Every expected answer is the
// one the Go oracle pins for the same schema text and the same input bytes,
// transcribed from internal/codegen/gotable/fixedbound_test.go
// (TestFixedFormCountAndLengthOverBoundRefuseByName) and
// internal/codegen/gotable/fixedversioning_count_clamp_ends_test.go.

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
)

func TestCPPFixedCountOverBoundRefusesByName(t *testing.T) {
	t.Parallel()

	schema := `package probe
fixed table Root {
    n   [..95]int32
    txt string(41)
}
`
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "probe.schema")
	if err := os.WriteFile(schemaPath, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, schemaPath)

	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)

	outDir := t.TempDir()
	var header string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}

	root := cppFixedRootName(t, u)
	pkg := u.Package

	// THE IDENTITY-PLAN PROBE: build, save, then forge each count/length past
	// the bound, with and without slack behind it, and assert the refusal and
	// that not one destination byte moved (the back storage is poisoned
	// 0x5A before every load, and the refusal must leave it untouched).
	driver := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(`#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "@HEADER@"

using namespace @PKG@;

static uint8_t data[1 << 20];

static int64_t find( const uint8_t * needle, int n )
{
    for ( int64_t i = 0; i + n <= (int64_t) sizeof( data ); ++i )
        if ( memcmp( data + i, needle, n ) == 0 ) return i;
    return -1;
}

int main( void )
{
    @ROOT@ one;
    @ROOT@Reset( one );
    one.n_count = 1;
    one.n[0] = 0x11223344;
    one.txt_length = 1;
    one.txt[0] = '!';
    const int64_t need = @ROOT@FixedMeasure( 1 );
    const int64_t wrote = @ROOT@FixedSave( &one, 1, data, (int64_t) sizeof( data ) );
    if ( wrote != need ) { printf( "save %lld != measure %lld\n", (long long) wrote, (long long) need ); return 1; }

    static const uint8_t countNeedle[4] = { 0x44, 0x33, 0x22, 0x11 };
    const int64_t cAt = find( countNeedle, 4 );
    if ( cAt < 0 ) { printf( "identity: count field not found\n" ); return 1; }
    const int64_t countOff = cAt - 4;
    static const uint8_t lenNeedle[5] = { 1, 0, 0, 0, '!' };
    const int64_t lAt = find( lenNeedle, 5 );
    if ( lAt < 0 ) { printf( "identity: length field not found\n" ); return 1; }
    const int64_t lenOff = lAt;

    static TableFixedEntry plan[4096];
    int ok = 0;
    auto check = [&]( const char * name, uint8_t * bytes, TableMessageReason want ) {
        @ROOT@ back;
        memset( &back, 0x5A, sizeof( back ) );
        TableReport r;
        memset( &r, 0, sizeof( r ) );
        const int64_t n = @ROOT@FixedLoad( &back, 1, bytes, wrote, plan, 4096, NULL, &r );
        if ( n != -1 || !r.refused || r.reason != want )
        {
            printf( "%s: n=%lld refused=%d reason=%d, want %d\n", name, (long long) n, (int) r.refused, (int) r.reason, (int) want );
            return 1;
        }
        if ( r.malformed ) { printf( "%s: malformed set — a refusal by name is not malformed\n", name ); return 1; }
        if ( r.clamped != 0 || r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        {
            printf( "%s: refusal moved a counter: clamped=%d unknown=%d kind_mismatch=%d widened=%d duplicate=%d\n", name, r.clamped, r.unknown, r.kind_mismatch, r.widened, r.duplicate );
            return 1;
        }
        const uint8_t * p = (const uint8_t *) &back;
        for ( int64_t i = 0; i < (int64_t) sizeof( back ); ++i )
            if ( p[i] != 0x5A ) { printf( "%s: the refusal wrote destination byte %lld: 0x%02x\n", name, (long long) i, p[i] ); return 1; }
        return 0;
    };

    static uint8_t d[1 << 20];

    memcpy( d, data, wrote );
    TableFixedPut32( d + countOff, 200 );
    ok |= check( "identity count over bound", d, count_over_bound );

    memcpy( d, data, wrote );
    TableFixedPut32( d + countOff, 0xFFFFFFFFu );
    ok |= check( "identity count negative", d, count_over_bound );

    // THE BOUND EDGE, ONE PAST: 96 is the [..95] count bound plus one and must
    // refuse count_over_bound. The Go oracle has no bound+1 count row (its
    // over-bound count is 200), so the expected answer here is
    // docs/SPEC-TABLES.md §3.4 itself: a count past the PLAN's bound refuses by
    // name, never clamps.
    memcpy( d, data, wrote );
    TableFixedPut32( d + countOff, 96 );
    ok |= check( "identity count one past bound", d, count_over_bound );

    // THE LAST ACCEPTED COUNT: exactly 95 loads n=1 with the first element
    // intact, so the refusal is past the bound and not at it (§3.4).
    memcpy( d, data, wrote );
    TableFixedPut32( d + countOff, 95 );
    {
        @ROOT@ back;
        memset( &back, 0x5A, sizeof( back ) );
        TableReport r;
        memset( &r, 0, sizeof( r ) );
        const int64_t n = @ROOT@FixedLoad( &back, 1, d, wrote, plan, 4096, NULL, &r );
        if ( n != 1 || r.refused || back.n_count != 95 || back.n[0] != 0x11223344 )
        {
            printf( "identity count at bound: n=%lld refused=%d count=%d first=0x%x\n", (long long) n, (int) r.refused, (int) back.n_count, (unsigned) back.n[0] );
            return 1;
        }
    }

    memcpy( d, data, wrote );
    TableFixedPut32( d + lenOff, 99 );
    ok |= check( "identity length over bound", d, length_over_bound );

    memcpy( d, data, wrote );
    TableFixedPut32( d + lenOff, 0xFFFFFFFFu );
    ok |= check( "identity length negative", d, length_over_bound );

    memcpy( d, data, wrote );
    TableFixedPut32( d + countOff, 200 );
    for ( int64_t i = countOff + 4; i < wrote && i < countOff + 4 + 95LL * 4; ++i ) d[i] = 0xFF;
    ok |= check( "identity count over bound with slack", d, count_over_bound );

    memcpy( d, data, wrote );
    TableFixedPut32( d + lenOff, 99 );
    for ( int64_t i = lenOff + 4; i < wrote && i < lenOff + 4 + 41; ++i ) d[i] = 0xFF;
    ok |= check( "identity length over bound with slack", d, length_over_bound );

    if ( ok ) { printf( "identity: a forged count/length did not refuse\n" ); return 1; }
    printf( "OK identity\n" );
    return 0;
}
`, "@PKG@", pkg), "@ROOT@", root), "@HEADER@", header)

	probePath := filepath.Join(outDir, "probe.cpp")
	if err := os.WriteFile(probePath, []byte(driver), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	serialize, err := filepath.Abs("../../../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c++17", "-O1", "-I" + outDir, "-I" + serialize, probePath, "-o", filepath.Join(outDir, "probe")}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("identity probe did not compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(filepath.Join(outDir, "probe")).CombinedOutput(); err != nil {
		t.Fatalf("identity probe: %v\n%s", err, out)
	}

	// THE COMPILED LINEAGE, count: VNEW reads VOLD array_bounded_grow ([..4]
	// -> [..8]) through its lineage. A forged count of 200 past the WRITER's
	// bound (4) refuses `count_over_bound`, with and without slack behind the
	// bound (gotable/fixedbound_test.go, TestFixedFormCountAndLengthOverBound-
	// RefuseByName, the two "lineage count over bound" rows).
	corpus := cppFixedCorpus(t)
	countFile := filepath.Join(corpus, "old_array_bounded_grow.bin")
	countBody := `
    if ( n != -1 ) { printf( "lineage count: a forged count did not refuse: n=%lld\n", (long long) n ); return 1; }
    if ( !r.refused || r.reason != count_over_bound ) { printf( "lineage count: refused=%d reason=%d, want count_over_bound\n", (int) r.refused, (int) r.reason ); return 1; }
    if ( r.clamped != 0 || r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "lineage count: a counter moved: clamped=%d unknown=%d kind_mismatch=%d widened=%d duplicate=%d\n", r.clamped, r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
`
	countLocate := `
        static const uint8_t needle[8] = { 0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00 };
        int64_t found = -1;
        for ( int64_t i = 0; i + 8 <= len; ++i )
        {
            if ( memcmp( data + i, needle, 8 ) == 0 )
            {
                if ( found >= 0 ) { printf( "lineage count: locator matched twice\n" ); return 1; }
                found = i;
            }
        }
        if ( found < 0 ) { printf( "lineage count: locator matched nowhere\n" ); return 1; }
`
	countForge := `
    {
` + countLocate + `
        TableFixedPut32( data + found + 4, 200 );
    }
`
	countSlackForge := `
    {
` + countLocate + `
        TableFixedPut32( data + found + 4, 200 );
        for ( int64_t i = found + 8; i < len && i < found + 8 + 4LL * 4; ++i ) data[i] = 0xFF;
    }
`
	if out, err := cppRunVersionProbe(t, "array_bounded_grow", countFile, countBody, countForge, cppProbePoison); err != nil {
		t.Fatalf("compiled count: %v\n%s", err, out)
	}
	if out, err := cppRunVersionProbe(t, "array_bounded_grow", countFile, countBody, countSlackForge, cppProbePoison); err != nil {
		t.Fatalf("compiled count with slack: %v\n%s", err, out)
	}

	// THE COMPILED LINEAGE, length: VNEW reads VOLD string_grow (string(8) ->
	// string(16)) through its lineage. A forged length of 99 past the WRITER's
	// bound (8) refuses `length_over_bound`, with and without slack behind the
	// bound (gotable/fixedbound_test.go, TestFixedFormCountAndLengthOverBound-
	// RefuseByName, the two "lineage length over bound" rows).
	lenFile := filepath.Join(corpus, "old_string_grow.bin")
	lenBody := `
    if ( n != -1 ) { printf( "lineage length: a forged length did not refuse: n=%lld\n", (long long) n ); return 1; }
    if ( !r.refused || r.reason != length_over_bound ) { printf( "lineage length: refused=%d reason=%d, want length_over_bound\n", (int) r.refused, (int) r.reason ); return 1; }
    if ( r.clamped != 0 || r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "lineage length: a counter moved: clamped=%d unknown=%d kind_mismatch=%d widened=%d duplicate=%d\n", r.clamped, r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
`
	lenLocate := `
        static const uint8_t needle[8] = { 0xAA, 0xAA, 0xAA, 0xAA, 0x08, 0x00, 0x00, 0x00 };
        int64_t found = -1;
        for ( int64_t i = 0; i + 8 <= len; ++i )
        {
            if ( memcmp( data + i, needle, 8 ) == 0 )
            {
                if ( found >= 0 ) { printf( "lineage length: locator matched twice\n" ); return 1; }
                found = i;
            }
        }
        if ( found < 0 ) { printf( "lineage length: locator matched nowhere\n" ); return 1; }
`
	lenForge := `
    {
` + lenLocate + `
        TableFixedPut32( data + found + 4, 99 );
    }
`
	lenSlackForge := `
    {
` + lenLocate + `
        TableFixedPut32( data + found + 4, 99 );
        for ( int64_t i = found + 8; i < len && i < found + 8 + 8; ++i ) data[i] = 0xFF;
    }
`
	if out, err := cppRunVersionProbe(t, "string_grow", lenFile, lenBody, lenForge, cppProbePoison); err != nil {
		t.Fatalf("compiled length: %v\n%s", err, out)
	}
	if out, err := cppRunVersionProbe(t, "string_grow", lenFile, lenBody, lenSlackForge, cppProbePoison); err != nil {
		t.Fatalf("compiled length with slack: %v\n%s", err, out)
	}
}
