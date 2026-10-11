package cpptable

// THE ROADMAP'S INTEGER AND FLOAT VALUE ROWS ON THE CPP LEG
// (docs/roadmap.sexp node `fixed-tables`, ROADMAP.md "NEW Fixed Tables"). One
// subtest per task id, table-driven, t.Parallel() first.
//
// A [capability] task is met when "the leg writes and reads the construct with
// non-default and boundary values, asserted, in a target a gate runs". A source
// grep is not a write and a read, so each row below GENERATES this leg's own
// fixed-form header for a per-row fixture, COMPILES a probe against it and RUNS
// the probe: the probe writes records with the row's non-default and boundary
// values through the generated `...FixedSave` and reads them back through the
// generated `...FixedLoad`, asserting the value and the cleanliness counters.
// This is the same generate-compile-run unit `cppRunCapability` (fixedroadmap_t10_test.go)
// and `cppRunVersionProbe` (fixedversioning_test.go) use.
//
// THE PAGE SENTENCES EACH ASSERTION IMPLEMENTS:
//
//   - docs/SPEC-TABLES.md §3.4: "A FIXED-TABLE RECORD IS AN EIGHT-BYTE HASH OF
//     THE WRITER'S LAYOUT, AND THEN THE VALUES IN DECLARED ORDER, EVERY FIELD
//     AT ITS BOUND." — every field's declared storage is written and read, and
//     a declared bound is the boundary value asserted.
//   - docs/FIXED-FORM-ALGORITHM.md: the clamps the reader runs after the copy
//     hold a ranged scalar to its declared min/max and a bits(N) value to
//     2^N - 1, counting each one in `report->clamped`.
//
// THE PER-ROW FIXTURES CARRY NO fixed(...)/ufixed(...) AND NO int128/uint128,
// so their headers take no serialize.h include (cpptable.go's unitHas128 fires
// for those), and the probe compiles with the C++ compiler alone — no sibling
// runtime checkout. THE FOUR ROWS THAT NEED ONE (signed-128, unsigned-128,
// signed-fixed-point, unsigned-fixed-point) are NAMED AS NOT RUN, never faked:
// their subtests carry the named SKIP and their verdicts are `unknown`.

import (
	"os"
	"path/filepath"
	"testing"
)

// t09Row is one roadmap task: its id, the fixture's package (the C++ namespace
// the probe opens), the schema it generates from, and the probe body that
// writes and reads the values the row is about.
type t09Row struct {
	id     string
	pkg    string
	schema string
	body   string
	// notRun, when non-empty, is why the row cannot be probed in this tree; the
	// subtest SKIPs BY NAME and the verdict is `unknown`.
	notRun string
}

// t09Rows is the roadmap order of the eleven `[capability]` rows of the
// integer-and-float band of `fixed-tables`, each read off its `:id`.
var t09Rows = []t09Row{
	{
		id:  "signed-integers/cpp/valid-data",
		pkg: "t09signed",
		schema: `package t09signed

fixed table SignedInts
{
    i8  int8
    i16 int16
    i32 int32
    i64 int64
}
`,
		body: `
    SignedInts want[4];
    want[0].i8 = 0;          want[0].i16 = 0;          want[0].i32 = 0;          want[0].i64 = 0;
    want[1].i8 = -128;       want[1].i16 = -32768;     want[1].i32 = -2147483647 - 1; want[1].i64 = -9223372036854775807LL - 1;
    want[2].i8 = 127;        want[2].i16 = 32767;      want[2].i32 = 2147483647; want[2].i64 = 9223372036854775807LL;
    want[3].i8 = -1;         want[3].i16 = -1;         want[3].i32 = -1;         want[3].i64 = -1;

    static uint8_t buffer[1 << 16];
    int64_t written = SignedIntsFixedSave( want, 4, buffer, (int64_t) sizeof( buffer ) );
    if ( written != SignedIntsFixedMeasure( 4 ) ) { printf( "signed: save wrote %lld for 4 records\n", (long long) written ); return 1; }
    {
        const int64_t body0 = kTableFixedHeaderBytes + 4 + SignedIntsFixedLayoutBytes + 8;
        if ( buffer[body0 + 3 * SignedIntsFixedRecordBytes + 0] != 0xFFu )
            { printf( "signed: -1 rode int8 as %u, not 0xFF\n", buffer[body0 + 3 * SignedIntsFixedRecordBytes] ); return 1; }
    }
    static TableFixedEntry plan[4096];
    TableReport r; memset( &r, 0, sizeof( r ) );
    SignedInts got[4];
    int64_t n = SignedIntsFixedLoad( got, 4, buffer, written, plan, 4096, NULL, &r );
    if ( n != 4 ) { printf( "signed: load n=%lld malformed=%d refused=%d\n", (long long) n, (int) r.malformed, (int) r.refused ); return 1; }
    if ( r.malformed || r.refused || r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.widened != 0 )
        { printf( "signed: a clean round-trip is not a refusal and moves no counter: malformed=%d refused=%d u=%d km=%d c=%d w=%d\n", (int) r.malformed, (int) r.refused, r.unknown, r.kind_mismatch, r.clamped, r.widened ); return 1; }
    for ( int k = 0; k < 4; ++k )
        if ( got[k].i8 != want[k].i8 || got[k].i16 != want[k].i16 || got[k].i32 != want[k].i32 || got[k].i64 != want[k].i64 )
            { printf( "signed: record %d does not round-trip: i8 %d/%d i16 %d/%d i32 %d/%d i64 %lld/%lld\n", k, got[k].i8, want[k].i8, got[k].i16, want[k].i16, got[k].i32, want[k].i32, (long long) got[k].i64, (long long) want[k].i64 ); return 1; }
`,
	},
	{
		id:  "unsigned-integers/cpp/valid-data",
		pkg: "t09unsigned",
		schema: `package t09unsigned

fixed table UnsignedInts
{
    u8  uint8
    u16 uint16
    u32 uint32
    u64 uint64
}
`,
		body: `
    UnsignedInts want[3];
    want[0].u8 = 0;          want[0].u16 = 0;          want[0].u32 = 0;          want[0].u64 = 0;
    want[1].u8 = 255;        want[1].u16 = 65535;      want[1].u32 = 4294967295u; want[1].u64 = 18446744073709551615ull;
    want[2].u8 = 128;        want[2].u16 = 32768;      want[2].u32 = 2147483648u; want[2].u64 = 9223372036854775808ull;

    static uint8_t buffer[1 << 16];
    int64_t written = UnsignedIntsFixedSave( want, 3, buffer, (int64_t) sizeof( buffer ) );
    if ( written != UnsignedIntsFixedMeasure( 3 ) ) { printf( "unsigned: save wrote %lld for 3 records\n", (long long) written ); return 1; }
    {
        const int64_t body0 = kTableFixedHeaderBytes + 4 + UnsignedIntsFixedLayoutBytes + 8;
        if ( buffer[body0 + 1 * UnsignedIntsFixedRecordBytes + 0] != 0xFFu )
            { printf( "unsigned: 255 rode uint8 as %u, not 0xFF\n", buffer[body0 + UnsignedIntsFixedRecordBytes] ); return 1; }
    }
    static TableFixedEntry plan[4096];
    TableReport r; memset( &r, 0, sizeof( r ) );
    UnsignedInts got[3];
    int64_t n = UnsignedIntsFixedLoad( got, 3, buffer, written, plan, 4096, NULL, &r );
    if ( n != 3 ) { printf( "unsigned: load n=%lld malformed=%d refused=%d\n", (long long) n, (int) r.malformed, (int) r.refused ); return 1; }
    if ( r.malformed || r.refused || r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.widened != 0 )
        { printf( "unsigned: a clean round-trip is not a refusal and moves no counter: malformed=%d refused=%d u=%d km=%d c=%d w=%d\n", (int) r.malformed, (int) r.refused, r.unknown, r.kind_mismatch, r.clamped, r.widened ); return 1; }
    for ( int k = 0; k < 3; ++k )
        if ( got[k].u8 != want[k].u8 || got[k].u16 != want[k].u16 || got[k].u32 != want[k].u32 || got[k].u64 != want[k].u64 )
            { printf( "unsigned: record %d does not round-trip: u8 %u/%u u16 %u/%u u32 %u/%u u64 %llu/%llu\n", k, got[k].u8, want[k].u8, got[k].u16, want[k].u16, got[k].u32, want[k].u32, (unsigned long long) got[k].u64, (unsigned long long) want[k].u64 ); return 1; }
`,
	},
	{
		id:  "signed-128/cpp/valid-data",
		pkg: "t09signed128",
		notRun: "the generated header names serialize::int128_t/serialize::uint128_t for the 128-bit storage, so it `#include \"serialize.h\"` (cpptable.go's unitHas128), and no sibling ../serialize checkout is present; the C++ target is named as not run, never faked",
		schema: `package t09signed128

fixed table Signed128
{
    i128 int128 | min = -1000, max = 1000
}
`,
	},
	{
		id:  "unsigned-128/cpp/valid-data",
		pkg: "t09unsigned128",
		notRun: "the generated header names serialize::uint128_t for the 128-bit storage, so it `#include \"serialize.h\"` (cpptable.go's unitHas128), and no sibling ../serialize checkout is present; the C++ target is named as not run, never faked",
		schema: `package t09unsigned128

fixed table Unsigned128
{
    u128 uint128
}
`,
	},
	{
		id:  "ranged-integers/cpp/valid-data",
		pkg: "t09ranged",
		schema: `package t09ranged

fixed table RangedInts
{
    rng int32 | min = 0, max = 100
}
`,
		body: `
    RangedInts want[5];
    want[0].rng = 0;   want[1].rng = 100; want[2].rng = 50;
    want[3].rng = -1;  want[4].rng = 101;

    static uint8_t buffer[1 << 16];
    int64_t written = RangedIntsFixedSave( want, 5, buffer, (int64_t) sizeof( buffer ) );
    if ( written != RangedIntsFixedMeasure( 5 ) ) { printf( "ranged: save wrote %lld for 5 records\n", (long long) written ); return 1; }
    static TableFixedEntry plan[4096];
    TableReport r; memset( &r, 0, sizeof( r ) );
    RangedInts got[5];
    int64_t n = RangedIntsFixedLoad( got, 5, buffer, written, plan, 4096, NULL, &r );
    if ( n != 5 ) { printf( "ranged: load n=%lld malformed=%d refused=%d\n", (long long) n, (int) r.malformed, (int) r.refused ); return 1; }
    if ( r.malformed || r.refused || r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 )
        { printf( "ranged: a read is not a refusal and moves no counter but the bound: malformed=%d refused=%d u=%d km=%d w=%d\n", (int) r.malformed, (int) r.refused, r.unknown, r.kind_mismatch, r.widened ); return 1; }
    {
        const int32_t wantv[5] = { 0, 100, 50, 0, 100 };
        for ( int k = 0; k < 5; ++k )
            if ( got[k].rng != wantv[k] )
                { printf( "ranged: record %d lands rng=%d, not the declared bound %d\n", k, got[k].rng, wantv[k] ); return 1; }
        if ( r.clamped != 2 ) { printf( "ranged: the two out-of-range values did not count exactly 2 clamps: %d\n", r.clamped ); return 1; }
    }
`,
	},
	{
		id:  "bits-values/cpp/valid-data",
		pkg: "t09bits",
		schema: `package t09bits

fixed table BitsVals
{
    bitsv bits(12)
}
`,
		body: `
    BitsVals want[4];
    want[0].bitsv = 0;    want[1].bitsv = 1;    want[2].bitsv = 4095; want[3].bitsv = 4096;

    static uint8_t buffer[1 << 16];
    int64_t written = BitsValsFixedSave( want, 4, buffer, (int64_t) sizeof( buffer ) );
    if ( written != BitsValsFixedMeasure( 4 ) ) { printf( "bits: save wrote %lld for 4 records\n", (long long) written ); return 1; }
    static TableFixedEntry plan[4096];
    TableReport r; memset( &r, 0, sizeof( r ) );
    BitsVals got[4];
    int64_t n = BitsValsFixedLoad( got, 4, buffer, written, plan, 4096, NULL, &r );
    if ( n != 4 ) { printf( "bits: load n=%lld malformed=%d refused=%d\n", (long long) n, (int) r.malformed, (int) r.refused ); return 1; }
    if ( r.malformed || r.refused || r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 )
        { printf( "bits: a read is not a refusal and moves no counter but the width bound: malformed=%d refused=%d u=%d km=%d w=%d\n", (int) r.malformed, (int) r.refused, r.unknown, r.kind_mismatch, r.widened ); return 1; }
    {
        const uint32_t wantv[4] = { 0, 1, 4095, 4095 };
        for ( int k = 0; k < 4; ++k )
            if ( got[k].bitsv != wantv[k] )
                { printf( "bits: record %d lands bitsv=%u, not the declared 2^12-1 bound %u\n", k, got[k].bitsv, wantv[k] ); return 1; }
        if ( r.clamped != 1 ) { printf( "bits: the one value past 4095 did not count exactly 1 clamp: %d\n", r.clamped ); return 1; }
    }
`,
	},
	{
		id:  "float64-values/cpp/valid-data",
		pkg: "t09float64",
		schema: `package t09float64

fixed table Float64s
{
    f64 float64
}
`,
		body: `
    Float64s want[4];
    want[0].f64 = 3.141592653589793;  want[1].f64 = -2.5;
    want[2].f64 = 1e308;              want[3].f64 = -1e308;

    static uint8_t buffer[1 << 16];
    int64_t written = Float64sFixedSave( want, 4, buffer, (int64_t) sizeof( buffer ) );
    if ( written != Float64sFixedMeasure( 4 ) ) { printf( "float64: save wrote %lld for 4 records\n", (long long) written ); return 1; }
    static TableFixedEntry plan[4096];
    TableReport r; memset( &r, 0, sizeof( r ) );
    Float64s got[4];
    int64_t n = Float64sFixedLoad( got, 4, buffer, written, plan, 4096, NULL, &r );
    if ( n != 4 ) { printf( "float64: load n=%lld malformed=%d refused=%d\n", (long long) n, (int) r.malformed, (int) r.refused ); return 1; }
    if ( r.malformed || r.refused || r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.widened != 0 )
        { printf( "float64: a clean round-trip is not a refusal and moves no counter: malformed=%d refused=%d u=%d km=%d c=%d w=%d\n", (int) r.malformed, (int) r.refused, r.unknown, r.kind_mismatch, r.clamped, r.widened ); return 1; }
    for ( int k = 0; k < 4; ++k )
        if ( got[k].f64 != want[k].f64 )
            { printf( "float64: record %d lands %.17g, wrote %.17g\n", k, got[k].f64, want[k].f64 ); return 1; }
`,
	},
	{
		id:  "compressed-floats/cpp/valid-data",
		pkg: "t09compfloat",
		schema: `package t09compfloat

fixed table CompFloats
{
    cf float32 | min = -1, max = 1, resolution = 0.01
}
`,
		body: `
    CompFloats want[5];
    want[0].cf = -1.0f; want[1].cf = 1.0f; want[2].cf = 0.0f; want[3].cf = 0.5f; want[4].cf = 2.0f;

    static uint8_t buffer[1 << 16];
    int64_t written = CompFloatsFixedSave( want, 5, buffer, (int64_t) sizeof( buffer ) );
    if ( written != CompFloatsFixedMeasure( 5 ) ) { printf( "compressed: save wrote %lld for 5 records\n", (long long) written ); return 1; }
    static TableFixedEntry plan[4096];
    TableReport r; memset( &r, 0, sizeof( r ) );
    CompFloats got[5];
    int64_t n = CompFloatsFixedLoad( got, 5, buffer, written, plan, 4096, NULL, &r );
    if ( n != 5 ) { printf( "compressed: load n=%lld malformed=%d refused=%d\n", (long long) n, (int) r.malformed, (int) r.refused ); return 1; }
    if ( r.malformed || r.refused || r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 )
        { printf( "compressed: a read is not a refusal and moves no counter but the range bound: malformed=%d refused=%d u=%d km=%d w=%d\n", (int) r.malformed, (int) r.refused, r.unknown, r.kind_mismatch, r.widened ); return 1; }
    {
        const float wantv[5] = { -1.0f, 1.0f, 0.0f, 0.5f, 1.0f };
        for ( int k = 0; k < 5; ++k )
            if ( got[k].cf != wantv[k] )
                { printf( "compressed: record %d lands %.9g, not the declared bound %.9g\n", k, got[k].cf, wantv[k] ); return 1; }
        if ( r.clamped != 1 ) { printf( "compressed: the one value past 1.0 did not count exactly 1 clamp: %d\n", r.clamped ); return 1; }
    }
`,
	},
	{
		id:  "signed-fixed-point/cpp/valid-data",
		pkg: "t09fixed",
		notRun: "the generated header names serialize::uint128_t for a fixed-point field's storage (cpptable.go's unitHas128 fires for any TFixed field), so it `#include \"serialize.h\"`, and no sibling ../serialize checkout is present; the C++ target is named as not run, never faked",
		schema: `package t09fixed

fixed table SignedFixed
{
    fx fixed(16, 16) | min = -1, max = 1
}
`,
	},
	{
		id:  "unsigned-fixed-point/cpp/valid-data",
		pkg: "t09ufixed",
		notRun: "the generated header names serialize::uint128_t for a fixed-point field's storage (cpptable.go's unitHas128 fires for any TFixed field), so it `#include \"serialize.h\"`, and no sibling ../serialize checkout is present; the C++ target is named as not run, never faked",
		schema: `package t09ufixed

fixed table UnsignedFixed
{
    ufx ufixed(16, 16) | min = 0, max = 1
}
`,
	},
	{
		id:  "bool-values/cpp/valid-data",
		pkg: "t09bool",
		schema: `package t09bool

fixed table Bools
{
    b bool
}
`,
		body: `
    Bools want[4];
    want[0].b = false; want[1].b = true; want[2].b = false; want[3].b = true;

    static uint8_t buffer[1 << 16];
    int64_t written = BoolsFixedSave( want, 4, buffer, (int64_t) sizeof( buffer ) );
    if ( written != BoolsFixedMeasure( 4 ) ) { printf( "bool: save wrote %lld for 4 records\n", (long long) written ); return 1; }
    {
        const int64_t body0 = kTableFixedHeaderBytes + 4 + BoolsFixedLayoutBytes + 8;
        if ( buffer[body0 + 0 * BoolsFixedRecordBytes] != 0u || buffer[body0 + 1 * BoolsFixedRecordBytes] != 1u )
            { printf( "bool: the wire bytes are not byte != 0: false=%u true=%u\n", buffer[body0], buffer[body0 + BoolsFixedRecordBytes] ); return 1; }
    }
    static TableFixedEntry plan[4096];
    TableReport r; memset( &r, 0, sizeof( r ) );
    Bools got[4];
    int64_t n = BoolsFixedLoad( got, 4, buffer, written, plan, 4096, NULL, &r );
    if ( n != 4 ) { printf( "bool: load n=%lld malformed=%d refused=%d\n", (long long) n, (int) r.malformed, (int) r.refused ); return 1; }
    if ( r.malformed || r.refused || r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.widened != 0 )
        { printf( "bool: a clean round-trip is not a refusal and moves no counter: malformed=%d refused=%d u=%d km=%d c=%d w=%d\n", (int) r.malformed, (int) r.refused, r.unknown, r.kind_mismatch, r.clamped, r.widened ); return 1; }
    for ( int k = 0; k < 4; ++k )
        if ( got[k].b != want[k].b )
            { printf( "bool: record %d lands %d, wrote %d\n", k, (int) got[k].b, (int) want[k].b ); return 1; }
`,
	},
}

// TestFixedRoadmapCppT09IntegersFloats proves each row by generating the leg's
// fixed-form header, compiling a probe and running it. A row that cannot be
// probed in this tree SKIPs BY NAME (§5.9 #44's rule: a green folds an unrun
// row into a pass, a red blames the row for the harness), and its verdict is
// `unknown`.
func TestFixedRoadmapCppT09IntegersFloats(t *testing.T) {
	t.Parallel()
	for _, row := range t09Rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			if row.notRun != "" {
				t.Skipf("named as not run: %s", row.notRun)
			}
			dir := t.TempDir()
			path := filepath.Join(dir, row.pkg+".schema")
			if err := os.WriteFile(path, []byte(row.schema), 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := cppRunCapability(t, path, row.pkg, row.body)
			if err != nil {
				t.Fatalf("%s: %v\n%s", row.id, err, out)
			}
		})
	}
}
