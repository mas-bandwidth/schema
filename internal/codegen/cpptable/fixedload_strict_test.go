package cpptable

// TestFixedLoadPlainIsStrict pins docs/SPEC-TABLES.md §6.5 on the C++ leg's
// fixed form: the PLAIN <T>FixedLoad is the STRICT read and the only read
// (docs/plans/strict-only.md): no tolerant twin exists.
//
// One file carries a value past a declared range: the read of it answers -1,
// names out_of_range, resets every value it had written to its declared
// defaults and hands back THIS read's report (a stale counter the caller left
// in it is gone). A clean file reads whole, the same on every read.

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

    // THE PLAIN NAME refuses the same bytes, resets what it wrote and reports THIS read.
    {
        Ranged got[2];
        got[0].lead = 1234; got[1].lead = 5678;
        Ranged fresh; RangedReset( fresh );
        TableReport r;
        r.unknown = 99; // a previous read's counter the strict read must not keep
        const int64_t n = RangedFixedLoad( got, 2, clamps, clamp_bytes, plan, 4096, NULL, &r );
        if ( n != -1 ) { printf( "strict: the plain read accepted a out_of_range file: n=%lld\n", (long long) n ); return 1; }
        if ( !r.refused || r.refuse_reason != out_of_range || r.unknown != 0 ) { printf( "strict: the report is not this read's own: refused=%d reason=%d unknown=%d\n", (int) r.refused, (int) r.refuse_reason, r.unknown ); return 1; }
        for ( int k = 0; k < 2; ++k )
            if ( got[k].lead != fresh.lead || got[k].rng != fresh.rng )
                { printf( "strict: record %d was not reset to its declared defaults on the refusal\n", k ); return 1; }
    }

    // A CLEAN FILE reads whole, and the same on a second read.
    {
        Ranged a[2]; Ranged b[2];
        TableReport ra; TableReport rb;
        const int64_t na = RangedFixedLoad( a, 2, clean, clean_bytes, plan, 4096, NULL, &ra );
        const int64_t nb = RangedFixedLoad( b, 2, clean, clean_bytes, plan, 4096, NULL, &rb );
        if ( na != 2 || nb != 2 || a[0].rng != 50 || a[1].rng != 60 || b[1].rng != 60 || ra.refused || ra.damaged || ra.refuse_reason != ok || rb.refuse_reason != ok )
            { printf( "strict: a clean file did not read whole: %lld %lld\n", (long long) na, (long long) nb ); return 1; }
    }

    // A NULL REPORT is allowed on the strict name and still refuses.
    {
        Ranged got[2];
        if ( RangedFixedLoad( got, 2, clamps, clamp_bytes, plan, 4096, NULL, NULL ) != -1 )
            { printf( "strict: the plain read with no report accepted a out_of_range file\n" ); return 1; }
    }
`
	out, err := cppRunCapability(t, path, "fxstrict", body)
	if err != nil {
		t.Fatalf("plain FixedLoad is strict: %v\n%s", err, out)
	}
}
