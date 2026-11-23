package cpptable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestCppKindHeldEscapeRefuses: kind 31 is held by name and never written
// (R17, docs/SPEC-TABLES.md §3: the newer reader reads old data, never the
// other way). The C++ leg meets it only as damage, at a field it cannot name,
// at one it declares and in a union arm, on the region, builder and retaining
// loads alike, exactly as it meets 33 and 34: the whole load refuses. Red on
// the old escape, which skipped an unnamed 31 by its L and counted it unknown.
func TestCppKindHeldEscapeRefuses(t *testing.T) {
	t.Parallel()
	unknown, named := ir.TableWireId("zzz"), ir.TableWireId("a")
	cases := []struct {
		name string
		wire []byte
	}{
		{"unnamed field", cppElemFile([]uint64{unknown}, cppElemField(1, ir.TableKindHeldEscape, []byte{0xAA, 0xBB}))},
		{"unnamed field, L 0", cppElemFile([]uint64{unknown}, cppElemField(1, ir.TableKindHeldEscape, nil))},
		{"named field", cppElemFile([]uint64{named}, cppElemField(1, ir.TableKindHeldEscape, []byte{1, 0, 0, 0}))},
		{"union arm", cppElemFile([]uint64{unknown, named}, cppUnionField(1, []byte{2, ir.TableKindHeldEscape, 1, 7}))},
	}
	loads := []string{"region", "builder", "retaining"}
	var body strings.Builder
	body.WriteString("    alignas( 64 ) static uint8_t region[4096];\n    static uint8_t retain_bytes[4096];\n    static TableRetain::Id retain_ids[64];\n")
	for i, c := range cases {
		fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s };
        TableReport g; memset( &g, 0, sizeof( g ) );
        if ( RootLoad( region, (int64_t) sizeof( region ), wire, (int64_t) sizeof( wire ), &g ) != NULL || !g.damaged ) { return %d; }
        RootBuilder builder;
        TableReport h; memset( &h, 0, sizeof( h ) );
        if ( RootLoadBuilder( builder, wire, (int64_t) sizeof( wire ), &h ) || !h.damaged ) { return %d; }
        TableRetain store;
        store.bytes = retain_bytes; store.capacity = (int64_t) sizeof( retain_bytes ); store.ids = retain_ids; store.id_capacity = 64;
        TableReport k; memset( &k, 0, sizeof( k ) );
        if ( RootLoadRetain( region, (int64_t) sizeof( region ), wire, (int64_t) sizeof( wire ), &store, &k ) != NULL || !k.damaged || store.count != 0 ) { return %d; }
    }
`, cppBytes(c.wire), 3*i+1, 3*i+2, 3*i+3)
	}
	out, err := cppHeldRun(t, loadUnitFromSource(t, cppNestedElementKindsUnit), body.String())
	if err != nil {
		var key strings.Builder
		for i, c := range cases {
			for j, l := range loads {
				fmt.Fprintf(&key, " exit %d = %s, %s load;", 3*i+j+1, c.name, l)
			}
		}
		t.Fatalf("a kind 31 load was not refused as damage: %v (%s)\n%s", err, key.String(), out)
	}
}
