package ctable

import (
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
)

// A compressed float's -0 lands as +0 and is written as +0
// (docs/SPEC-TABLES.md §3.3, docs/rulings.json fixed-compressed-negzero). The
// vector is the reference writer's file holding the sign bit alone in the
// compressed field; this leg reads it through its generated load, lands the
// value 0 with the sign bit clear, refuses nothing, and saves the canonical
// bytes the +0 record writes.
func TestFixedCompressedNegZeroRead(t *testing.T) {
	t.Parallel()
	corpus := cStrictCorpus(t)
	schema := cReadSchema(t, "FNZ")
	for _, vec := range fixedvector.NegativeZero {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			body := `
    {
        uint32_t layout_bytes;
        int64_t value_at;
        uint32_t got;
        uint32_t landed;
        uint8_t want[1 << 20];
        uint8_t out[1 << 20];
        int64_t need;
        memcpy( &layout_bytes, data + kTableFixedHeaderBytes, 4 );
        value_at = kTableFixedHeaderBytes + 4 + (int64_t) layout_bytes + 8 + 4;
        if ( value_at + 4 > len )
            { printf( "the vector is shorter than its compressed field\n" ); return 1; }
        memcpy( &got, data + value_at, 4 );
        if ( got != 0x80000000u )
            { printf( "the vector does not hold a compressed float's -0: %#08x\n", got ); return 1; }
        if ( n != 1 )
            { printf( "the reader refused a compressed -0: n=%lld\n", (long long) n ); return 1; }
        if ( r.refused != 0 || r.reason != 0 || r.refuse_reason != 0 || r.non_finite_floats != 0 || r.unknown != 0 || r.widened != 0 )
            { printf( "the read of a compressed -0 reported refused=%d reason=%d rr=%d nf=%d u=%d w=%d\n",
                     r.refused, r.reason, r.refuse_reason, r.non_finite_floats, r.unknown, r.widened ); return 1; }
        memcpy( &landed, &back[0].value, 4 );
        if ( landed != 0 )
            { printf( "a compressed float's -0 kept its sign: %#08x\n", landed ); return 1; }
        need = neg_zero_fixed_measure( 1 );
        if ( need != len )
            { printf( "the save measures %lld bytes, want %lld\n", (long long) need, (long long) len ); return 1; }
        if ( neg_zero_fixed_save( back, 1, out, sizeof( out ) ) != need )
            { printf( "the save did not write the measured bytes\n" ); return 1; }
        memcpy( want, data, (size_t) len );
        memset( want + value_at, 0, 4 );
        if ( memcmp( out, want, (size_t) len ) != 0 )
            { printf( "saving the landed +0 did not write the canonical bytes\n" ); return 1; }
    }
`
			out, err := cRunVersionProbe(t, schema, nil, 0, filepath.Join(corpus, vec.File), body, "")
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
		})
	}
}
