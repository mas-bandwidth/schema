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
	"os"
	"path/filepath"
	"testing"

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
