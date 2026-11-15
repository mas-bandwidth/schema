package ctable

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// vecRoot is the corpus root: the one fixed table no other fixed table in the
// unit names by value.
const vecRoot = "DeadRoot"

// EVERY DEAD BYTE IN A FIXED RECORD IS ZERO, AND A NON-ZERO ONE REFUSES THE
// WHOLE LOAD (docs/rulings.json fixed-dead-bytes-are-zero; docs/SPEC-TABLES.md
// §3.4, §13.10; docs/FIXED-FORM-ALGORITHM.md §3.1, §4.5, §5.3 step 11). The
// shared corpus (fixedvector.DeadBytes) has, per dead kind, one control whose
// every dead byte is zero and one changed file with a single dead region made
// non-zero: the generated C load refuses each changed file whole, writes no
// destination byte, and reads each control and saves it back byte for byte.
func TestFixedDeadBytesAreZero(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "a C compiler (it runs the generated reader over the dead-byte corpus)")
	corpus := cFixedCorpus(t)
	schema := cReadSchema(t, "DeadBytes")
	snake := ir.RustSnake(vecRoot)

	for _, vec := range fixedvector.DeadBytes {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			file := filepath.Join(corpus, vec.File)
			body := cDeadBytesBody(vec, snake)
			out, err := cRunVersionProbeRoot(t, schema, nil, 0, vecRoot, file, body, "")
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
			t.Logf("%s", out)
		})
	}
}

// cDeadBytesBody is the probe's own claim for one vector: the refusal verdict,
// with every destination slot still holding its reset image, or the control's
// read and a save that reproduces the file byte for byte. It never names a
// refusal reason: the verdict is the contract (docs/rulings.json
// refusal-verdict-only).
func cDeadBytesBody(vec fixedvector.DeadVector, snake string) string {
	if !vec.Reads {
		return fmt.Sprintf(`
    if ( n != -1 || !r.refused ) { printf( "%[1]s: n=%%lld refused=%%d, want the whole-load refusal\n", (long long) n, (int) r.refused ); return 1; }
    for ( k = 0; k < 8; ++k )
    {
        if ( memcmp( &back[k], &fresh, sizeof( fresh ) ) != 0 ) { printf( "%[1]s: the refusal wrote destination slot %%d\n", k ); return 1; }
    }
`, vec.Name)
	}
	return fmt.Sprintf(`
    if ( n < 1 || r.refused ) { printf( "%[1]s: n=%%lld refused=%%d, want the control to read\n", (long long) n, (int) r.refused ); return 1; }
    {
        static uint8_t out[1 << 20];
        const int64_t need = %[2]s_fixed_measure( n );
        const int64_t wrote = %[2]s_fixed_save( back, n, out, (int64_t) sizeof( out ) );
        if ( need != len || wrote != len ) { printf( "%[1]s: measure=%%lld wrote=%%lld file=%%lld\n", (long long) need, (long long) wrote, (long long) len ); return 1; }
        if ( memcmp( out, data, (size_t) len ) != 0 ) { printf( "%[1]s: the control did not save back byte for byte\n" ); return 1; }
    }
`, vec.Name, snake)
}
