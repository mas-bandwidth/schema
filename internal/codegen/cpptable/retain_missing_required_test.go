package cpptable

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestCppRetainRefusesAMissingRequiredPointer holds the pointered LoadRetain
// to the strict Load (docs/SPEC-TABLES.md §2.3, §6.5, §6.6). The corpus seed
// surf_holder_null_required is a tblsurf.Holder whose `must *Leaf | required`
// rides as index 0, which both reads accept (FL-16). One bit flipped in
// must's id in the trailer turns it into an unknown field, so the required
// field is ABSENT: the strict Load refuses with missing_required, and
// LoadRetain returned the root, counting the field missing but accepting the
// file, where every other leg and the oracle refuse (found by the C++
// wire-fuzz retain arm once it drove tblsurf.Holder, W-ROSTER).
func TestCppRetainRefusesAMissingRequiredPointer(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := loadHeldUnitPaths(t, root, []string{filepath.Join("test", "tables", "surfaces")})
	wire, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "wire", "surf_holder_null_required.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(wire) < 12 || wire[11] != 0xa8 {
		t.Fatalf("surf_holder_null_required.bin no longer carries must's id at byte 11: the witness no longer aims at the field")
	}
	seed := append([]byte(nil), wire...)
	wire[11] ^= 1 // must's id becomes an unknown one: the required field is absent
	body := fmt.Sprintf(`
    {
        static const uint8_t seed[] = { %s };
        const int64_t need = HolderLoadMeasure( seed, (int64_t) sizeof( seed ), NULL, NULL );
        std::vector<uint8_t> region( need > 0 ? (size_t) need : 1 );
        static uint8_t store[1 << 16];
        static TableRetain::Id ids[1024];
        TableRetain retain;
        retain.bytes = store; retain.capacity = (int64_t) sizeof( store );
        retain.ids = ids; retain.id_capacity = 1024;
        TableReport report = {};
        if ( HolderLoadRetain( region.data(), need, seed, (int64_t) sizeof( seed ), &retain, &report ) == NULL ) { printf( "LoadRetain refused the null required pointer FL-16 accepts\n" ); return 1; }
    }`, cppBytes(seed)) + fmt.Sprintf(`
    static const uint8_t wire[] = { %s };
    static uint8_t store[1 << 16];
    static TableRetain::Id ids[1024];
    TableRetain retain;
    retain.bytes = store; retain.capacity = (int64_t) sizeof( store );
    retain.ids = ids; retain.id_capacity = 1024;
    const int64_t need = HolderLoadMeasure( wire, (int64_t) sizeof( wire ), NULL, NULL );
    std::vector<uint8_t> region( need > 0 ? (size_t) need : 1 );
    TableReport strict = {};
    const Holder * plain = HolderLoad( region.data(), need, wire, (int64_t) sizeof( wire ), &strict );
    TableReport report = {};
    const Holder * kept = HolderLoadRetain( region.data(), need, wire, (int64_t) sizeof( wire ), &retain, &report );
    if ( plain != NULL || strict.refuse_reason != missing_required ) { printf( "Load: root=%%d reason=%%d\n", plain != NULL, (int) strict.refuse_reason ); return 1; }
    if ( kept != NULL || report.refuse_reason != missing_required ) { printf( "LoadRetain: root=%%d reason=%%d missing=%%d\n", kept != NULL, (int) report.refuse_reason, (int) report.missing_required ); return 1; }
`, cppBytes(wire))
	out, err := cppHeldRun(t, u, body)
	if err != nil {
		t.Fatalf("a missing required pointer under LoadRetain: %v\n%s", err, out)
	}
}
