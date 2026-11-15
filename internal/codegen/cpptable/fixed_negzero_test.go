package cpptable

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A compressed float's -0 lands as +0 and is written as +0
// (docs/SPEC-TABLES.md §3.3, docs/rulings.json fixed-compressed-negzero). The
// vector is the reference writer's file holding the sign bit alone in the
// compressed field; this leg reads it through its generated load, lands the
// value 0 with the sign bit clear, refuses and counts nothing, and saves the
// canonical bytes the +0 record writes. The Go leg's test of the same name is
// the worked example.
func TestFixedCompressedNegZeroRead(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "a C++ compiler (it runs the generated load over the vector)")
	corpus := cppFixedCorpus(t)
	schema := filepath.Join("..", "..", "..", "test", "tables", "FNZ.schema")
	for _, vec := range fixedvector.NegativeZero {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			file := filepath.Join(corpus, vec.File)
			body := fmt.Sprintf(`
    FILE * fp = fopen( %[1]q, "rb" );
    if ( fp == NULL ) { printf( "cannot open the vector\n" ); return 1; }
    const int64_t len = (int64_t) fread( data, 1, sizeof( data ), fp );
    fclose( fp );
    uint32_t layout_bytes = 0;
    memcpy( &layout_bytes, data + kTableFixedHeaderBytes, 4 );
    const int64_t value_at = kTableFixedHeaderBytes + 4 + (int64_t) layout_bytes + 8 + 4;
    uint32_t raw = 0;
    memcpy( &raw, data + value_at, 4 );
    if ( raw != 0x80000000u ) { printf( "the vector does not hold a compressed float's -0: %%#08x\n", raw ); return 1; }
    %[2]sReset( back[0] );
    n = %[2]sFixedLoad( back, 1, data, len, plan, 4096, NULL, &r );
    if ( n != 1 ) { printf( "the reader refused a compressed -0: n=%%lld refused=%%d\n", (long long) n, (int) r.refused ); return 1; }
    if ( r.refused || r.damaged || r.unknown != 0 || r.widened != 0 || r.missing_required != 0 )
    {
        printf( "the read of a compressed -0 reported refused=%%d damaged=%%d unknown=%%d widened=%%d missing=%%d\n",
                (int) r.refused, (int) r.damaged, (int) r.unknown, (int) r.widened, (int) r.missing_required );
        return 1;
    }
    if ( back[0].value < 0.0f || back[0].value > 0.0f ) { printf( "a compressed -0 did not land as zero\n" ); return 1; }
    uint32_t bits = 0;
    memcpy( &bits, &back[0].value, 4 );
    if ( bits != 0u ) { printf( "a compressed float's -0 kept its sign: %%#08x\n", bits ); return 1; }
    std::vector<uint8_t> out( (size_t) %[2]sFixedMeasure( 1 ) );
    const int64_t m = %[2]sFixedSave( back, 1, out.data(), (int64_t) out.size() );
    if ( m != len ) { printf( "the save wrote %%lld bytes, want %%lld\n", (long long) m, (long long) len ); return 1; }
    memset( data + value_at, 0, 4 );
    if ( memcmp( out.data(), data, (size_t) len ) != 0 ) { printf( "saving the landed +0 did not write the canonical bytes\n" ); return 1; }
    printf( "negative_zero: landed +0, saved the canonical bytes\n" );
`, file, vec.Root)
			out, err := cppRunFixedProbe(t, schema, vec.Root, body)
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
			t.Logf("%s", out)
		})
	}
}
