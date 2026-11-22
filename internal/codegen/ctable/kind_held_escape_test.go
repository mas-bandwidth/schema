package ctable

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestCKindHeldEscapeRefuses: kind 31 is held by name and never written (R17,
// docs/SPEC-TABLES.md §3: the newer reader reads old data, never the other
// way). The C leg meets it only as damage, at a field it cannot name, at one
// it declares and in a union arm, on the value, region, retaining and builder
// loads alike, exactly as it meets 33 and 34: the whole load refuses
// wire_damaged. The control is the same unnamed field under kind 12, which
// every load reads, so the framing of the vectors is not what refuses. Red on
// the old escape, which skipped an unnamed 31 by its L and counted it unknown.
func TestCKindHeldEscapeRefuses(t *testing.T) {
	slowtest.Gate(t, "the C compiler (cc)")
	t.Parallel()
	unknown, named := ir.TableWireId("zzz"), ir.TableWireId("a")
	cases := []struct {
		name string
		read bool
		wire []byte
	}{
		{"control: unnamed field under kind 12", true, heldFile([]uint64{unknown}, heldField(1, ir.TableKindString, []byte{0x61, 0x62}))},
		{"unnamed field", false, heldFile([]uint64{unknown}, heldField(1, ir.TableKindHeldEscape, []byte{0xAA, 0xBB}))},
		{"unnamed field, L 0", false, heldFile([]uint64{unknown}, heldField(1, ir.TableKindHeldEscape, nil))},
		{"named field", false, heldFile([]uint64{named}, heldField(1, ir.TableKindHeldEscape, []byte{1, 0, 0, 0}))},
		{"union arm", false, heldFile([]uint64{unknown, named}, append([]byte{1, ir.TableKindUnion, 2, ir.TableKindHeldEscape, 1, 7}, 0))},
	}
	loads := []string{"value", "region", "retaining", "builder"}
	var body strings.Builder
	body.WriteString("    uint8_t * region = (uint8_t *) malloc( 4096 );\n    static uint8_t store[1 << 12]; static TableRetainId ids[16];\n    if ( region == NULL ) { return 99; }\n")
	for i, c := range cases {
		fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s };
        const int want = %t;
        Root v; TableReport r; TableRetain retain; GraphBuilder b; int ok;
        root_reset( &v ); memset( &r, 0, sizeof( r ) );
        ok = root_load( &v, wire, (int64_t) sizeof( wire ), &r );
        if ( ok != want || ( !want && r.refuse_reason != SCHEMA_TABLE_REFUSE_WIRE_DAMAGED ) ) { return %d; }
        memset( &r, 0, sizeof( r ) );
        ok = graph_load( region, 4096, wire, (int64_t) sizeof( wire ), &r ) != NULL;
        if ( ok != want || ( !want && r.refuse_reason != SCHEMA_TABLE_REFUSE_WIRE_DAMAGED ) ) { return %d; }
        memset( &retain, 0, sizeof( retain ) ); retain.bytes = store; retain.capacity = sizeof( store ); retain.ids = ids; retain.id_capacity = 16;
        memset( &r, 0, sizeof( r ) );
        ok = graph_load_retain( region, 4096, wire, (int64_t) sizeof( wire ), &retain, &r ) != NULL;
        if ( ok != want || ( !want && ( r.refuse_reason != SCHEMA_TABLE_REFUSE_WIRE_DAMAGED || retain.count != 0 ) ) ) { return %d; }
        if ( !graph_builder_init( &b ) ) { return 98; }
        memset( &r, 0, sizeof( r ) );
        ok = graph_load_builder( &b, wire, (int64_t) sizeof( wire ), &r ) != 0;
        graph_builder_shutdown( &b );
        if ( ok != want || ( !want && r.refuse_reason != SCHEMA_TABLE_REFUSE_WIRE_DAMAGED ) ) { return %d; }
    }
`, cBytes(c.wire), c.read, 4*i+1, 4*i+2, 4*i+3, 4*i+4)
	}
	body.WriteString("    free( region );\n")
	out, err := cHeldRun(t, unitFrom(t, discardProbeSchema), body.String())
	if err != nil {
		var key strings.Builder
		for i, c := range cases {
			for j, l := range loads {
				fmt.Fprintf(&key, " exit %d = %s, %s load;", 4*i+j+1, c.name, l)
			}
		}
		t.Fatalf("a kind 31 load was not refused as damage: %v (%s)\n%s", err, key.String(), out)
	}
}

// heldFile frames one root body with its id table (docs/SPEC-TABLES.md §3).
func heldFile(ids []uint64, body []byte) []byte {
	out := append([]byte{ir.TableWireForm}, body...)
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(ids)))
}

// heldField is one field under an L-framed kind, then the body terminator.
func heldField(ref uint64, kind byte, content []byte) []byte {
	out := binary.AppendUvarint([]byte{byte(ref), kind}, uint64(len(content)))
	out = append(out, content...)
	return append(out, 0)
}
