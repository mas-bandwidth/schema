package ctable

// THE STRIDE OF A LINEAGE ENTRY'S STATIC PLAN.
//
// An older lineage entry's plan is compiled once into static storage whose
// capacity is this build's choice. A flat array is ONE identity leaf, but a
// compiled plan walks an evolved element once per element, so a stride folded
// from this build's own leaf count is too small for a valid older file: the
// build records plan_too_large on that entry and every load of the file refuses
// forever, whatever buffer the caller supplies. This test pins the older file
// loading under the generated C reader.

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedLineageEvolvedArrayElementPlanFits(t *testing.T) {
	t.Parallel()
	const oldSchema = `package probe
type Item { x int32 = 0 }
fixed table Root { items [200]Item }
`
	const newSchema = `package probe
type Item {
    x int32 = 0
    y int32 = 0
}
fixed table Root { items [200]Item }
`
	old := cUnitOf(t, oldSchema)
	root := cFixedRoot(t, old)
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(root))
	hash := ir.TableFixedLayoutHash(layout, root)
	body := make([]byte, ir.TableFixedTypeBytes(root))
	for i := 0; i < 200; i++ {
		binary.LittleEndian.PutUint32(body[i*4:], uint32(1000+i))
	}
	file := make([]byte, 0, 16+4+len(layout)+8+len(body))
	file = append(file, 3, 0, 0, 0, 0, 0, 0, 0)
	var hashBytes [8]byte
	binary.LittleEndian.PutUint64(hashBytes[:], hash)
	file = append(file, hashBytes[:]...)
	var layoutBytes [4]byte
	binary.LittleEndian.PutUint32(layoutBytes[:], uint32(len(layout)))
	file = append(file, layoutBytes[:]...)
	file = append(file, layout...)
	file = append(file, hashBytes[:]...)
	file = append(file, body...)
	path := filepath.Join(t.TempDir(), "old.bin")
	if err := os.WriteFile(path, file, 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := cRunVersionProbe(t, newSchema, []string{oldSchema}, 0, path, `
    if ( n != 1 ) { printf( "the older file did not load: n=%lld reason=%d\n", (long long) n, (int) r.reason ); return 1; }
    if ( r.refused ) { printf( "the older file was refused: reason=%d\n", (int) r.reason ); return 1; }
    {
        int k;
        for ( k = 0; k < 200; ++k )
        {
            if ( back[0].items[k].x != (int32_t) ( 1000 + k ) )
                { printf( "slot %d did not land the older writer's value: %d\n", k, (int) back[0].items[k].x ); return 1; }
            if ( back[0].items[k].y != 0 )
                { printf( "slot %d's appended field is not its default: %d\n", k, (int) back[0].items[k].y ); return 1; }
        }
    }
`, "")
	if err != nil {
		t.Fatalf("the generated reader refused a valid evolved layout: %v\n%s", err, out)
	}
}

// A union's tag width belongs to the READER. The writer's entry at the same
// walk index can be a different field — here a large array — and reading the
// writer's size for the reader's tag makes the sizing walk believe each tag
// entry lands the array's whole span, so it undercounts the prefill ranges and
// sizes the pool too small. The older file then refuses plan_too_large.
func TestFixedLineageEvolvedUnionPlanFits(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler (it compiles and runs the generated units)")
	const oldSchema = `package probe
union U {
    a int32
    b int32
}
fixed table Root {
    blob [500]uint8
    u U
}
`
	const newSchema = `package probe
union U {
    a int32
    b int32
}
fixed table Root {
    u U
    tail int32 = 0
}
`
	path := filepath.Join(t.TempDir(), "old.bin")
	writer := fmt.Sprintf(`
int main( void )
{
    Root v;
    uint8_t data[1 << 16];
    int64_t n;
    int i;
    FILE * f;
    memset( &v, 0, sizeof v );
    root_reset( &v );
    for ( i = 0; i < 500; ++i ) { v.blob[i] = (uint8_t) i; }
    v.u.type = 1;
    v.u.as.a = 12345;
    n = root_fixed_save( &v, 1, data, (int64_t) sizeof data );
    if ( n < 0 ) { printf( "save failed\n" ); return 1; }
    f = fopen( %q, "wb" );
    if ( f == NULL || fwrite( data, 1, (size_t) n, f ) != (size_t) n ) { printf( "write failed\n" ); return 1; }
    fclose( f );
    return 0;
}
`, path)
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	t14Run(t, cc, oldSchema, nil, writer, "")
	out, err := cRunVersionProbe(t, newSchema, []string{oldSchema}, 0, path, `
    if ( n != 1 ) { printf( "the older file did not load: n=%lld reason=%d\n", (long long) n, (int) r.reason ); return 1; }
    if ( r.refused ) { printf( "the older file was refused: reason=%d\n", (int) r.reason ); return 1; }
    if ( back[0].u.type != 1 || back[0].u.as.a != 12345 ) { printf( "the union moved\n" ); return 1; }
    if ( back[0].tail != 0 ) { printf( "the appended field is not its default: %d\n", (int) back[0].tail ); return 1; }
`, "")
	if err != nil {
		t.Fatalf("the generated reader refused a valid evolved layout: %v\n%s", err, out)
	}
}
