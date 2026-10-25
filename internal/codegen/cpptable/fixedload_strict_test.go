package cpptable

// TestFixedLoadPlainIsStrict pins docs/SPEC-TABLES.md §6.5 on the C++ leg's
// fixed form: the PLAIN <T>FixedLoad is the STRICT read and the tolerant read
// exists only under its explicit name, <T>FixedLoadTolerant, exactly as the
// Go oracle's internal/codegen/gotable/fixedform.go emits the pair.
//
// One file carries a value past a declared range, so the tolerant read lands
// the clamped value and counts one `clamped`; the plain read of the SAME bytes
// answers -1, resets every value it had written to its declared defaults and
// hands back THIS read's report (a stale counter the caller left in it is
// gone). A clean file reads the same through both names.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestFixedLoadPlainIsStrict(t *testing.T) {
	slowtest.Gate(t, "the c++ compiler (the leg's own toolchain)")
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "fxstrict.schema")
	if err := os.WriteFile(path, []byte(`package fxstrict

fixed table Ranged
{
    lead uint32
    rng int32 | min = 0, max = 100
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	body := `
    static uint8_t clean[1 << 12];
    static uint8_t clamps[1 << 12];
    static TableFixedEntry plan[4096];
    Ranged want[2];
    RangedReset( want[0] ); RangedReset( want[1] );
    want[0].lead = 7; want[0].rng = 50;
    want[1].lead = 9; want[1].rng = 60;
    const int64_t clean_bytes = RangedFixedSave( want, 2, clean, (int64_t) sizeof( clean ) );
    if ( clean_bytes != RangedFixedMeasure( 2 ) ) { printf( "strict: clean save wrote %lld\n", (long long) clean_bytes ); return 1; }
    want[1].rng = 101; // past max: the write side clamps nothing (§3.1), so the wire carries it
    const int64_t clamp_bytes = RangedFixedSave( want, 2, clamps, (int64_t) sizeof( clamps ) );
    if ( clamp_bytes != RangedFixedMeasure( 2 ) ) { printf( "strict: clamp save wrote %lld\n", (long long) clamp_bytes ); return 1; }

    // THE TOLERANT NAME keeps the recovered records and counts the clamp.
    {
        Ranged got[2];
        TableReport r;
        const int64_t n = RangedFixedLoadTolerant( got, 2, clamps, clamp_bytes, plan, 4096, NULL, &r );
        if ( n != 2 || r.clamped != 1 || got[1].rng != 100 || got[0].lead != 7 || got[1].lead != 9 )
            { printf( "strict: the tolerant read did not keep the clamped records: n=%lld clamped=%d rng=%d\n", (long long) n, r.clamped, got[1].rng ); return 1; }
    }

    // THE PLAIN NAME refuses the same bytes, resets what it wrote and reports THIS read.
    {
        Ranged got[2];
        got[0].lead = 1234; got[1].lead = 5678;
        Ranged fresh; RangedReset( fresh );
        TableReport r;
        r.unknown = 99; // a previous read's counter the strict read must not keep
        const int64_t n = RangedFixedLoad( got, 2, clamps, clamp_bytes, plan, 4096, NULL, &r );
        if ( n != -1 ) { printf( "strict: the plain read accepted a clamped file: n=%lld\n", (long long) n ); return 1; }
        if ( r.clamped != 1 || r.unknown != 0 ) { printf( "strict: the report is not this read's own: clamped=%d unknown=%d\n", r.clamped, r.unknown ); return 1; }
        for ( int k = 0; k < 2; ++k )
            if ( got[k].lead != fresh.lead || got[k].rng != fresh.rng )
                { printf( "strict: record %d was not reset to its declared defaults on the refusal\n", k ); return 1; }
    }

    // A CLEAN FILE reads the same through both names.
    {
        Ranged a[2]; Ranged b[2];
        TableReport ra; TableReport rb;
        const int64_t na = RangedFixedLoad( a, 2, clean, clean_bytes, plan, 4096, NULL, &ra );
        const int64_t nb = RangedFixedLoadTolerant( b, 2, clean, clean_bytes, plan, 4096, NULL, &rb );
        if ( na != 2 || nb != 2 || a[0].rng != 50 || a[1].rng != 60 || b[1].rng != 60 || ra.clamped != 0 || ra.malformed || ra.refused )
            { printf( "strict: a clean file did not read the same through both names: %lld %lld\n", (long long) na, (long long) nb ); return 1; }
    }

    // A NULL REPORT is allowed on the strict name and still refuses.
    {
        Ranged got[2];
        if ( RangedFixedLoad( got, 2, clamps, clamp_bytes, plan, 4096, NULL, NULL ) != -1 )
            { printf( "strict: the plain read with no report accepted a clamped file\n" ); return 1; }
    }
`
	out, err := cppRunCapability(t, path, "fxstrict", body)
	if err != nil {
		t.Fatalf("plain FixedLoad is strict: %v\n%s", err, out)
	}
}
