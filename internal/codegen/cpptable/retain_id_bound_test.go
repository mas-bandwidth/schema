package cpptable

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCppRetainIdTableBoundIsTheWires holds the retain family's MERGED
// trailer to the wire's id-table bound, id_table_max = 4096
// (docs/SPEC-TABLES.md §3, §6.6), and never to the generated table's own
// capacity. graphdemo's generated table names 47 ids, so a save that bounded
// the merged count by that capacity kept the first 47 retained fields of a
// file and dropped the rest with retain_lost. Each wire is a Scene whose body
// is n unknown uint8 fields and nothing else: LoadRetain keeps all n, and
// SaveRetain writes the file back byte for byte with no retain_lost. The
// cases are 48 ids (one past the generated capacity) and the pinned
// id_table_4096 vector (the wire's bound itself, the wire-fuzz seed that
// found it).
func TestCppRetainIdTableBoundIsTheWires(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := loadHeldUnitPaths(t, root, []string{filepath.Join("tables", "pointers")})
	pinned, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "fuzz-vectors", "id_table_4096.bin"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		n    int
		wire []byte
	}{
		{"one-past-the-generated-capacity", 48, retainUnknownIdsWire(48)},
		{"id_table_4096", 4096, pinned},
	}
	var body strings.Builder
	body.WriteString("    int failures = 0;\n")
	for _, tc := range cases {
		fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s };
        static uint8_t store[1 << 18];
        static TableRetain::Id ids[4096];
        TableRetain retain;
        retain.bytes = store; retain.capacity = (int64_t) sizeof( store );
        retain.ids = ids; retain.id_capacity = 4096;
        const int64_t need = SceneLoadMeasure( wire, (int64_t) sizeof( wire ), NULL, NULL );
        std::vector<uint8_t> region( need > 0 ? (size_t) need : 1 );
        TableReport report = {};
        const Scene * value = SceneLoadRetain( region.data(), need, wire, (int64_t) sizeof( wire ), &retain, &report );
        if ( value == NULL || report.unknown != %d || report.retained != %d || report.retain_lost != 0 )
            { printf( "%s: load unknown=%%d retained=%%d lost=%%d\n", (int) report.unknown, (int) report.retained, (int) report.retain_lost ); failures++; }
        else
        {
            const int64_t measured = SceneMeasureRetain( value, &retain );
            std::vector<uint8_t> saved( sizeof( wire ) + 64 );
            TableReport save_report = {};
            const int64_t wrote = SceneSaveRetain( value, &retain, saved.data(), (int64_t) saved.size(), &save_report );
            if ( measured != (int64_t) sizeof( wire ) || wrote != (int64_t) sizeof( wire ) || memcmp( saved.data(), wire, sizeof( wire ) ) != 0 || save_report.retain_lost != 0 )
                { printf( "%s: measure=%%lld saved=%%lld want %%lld, save retain_lost=%%d\n", (long long) measured, (long long) wrote, (long long) sizeof( wire ), (int) save_report.retain_lost ); failures++; }
        }
    }
`, cppBytes(tc.wire), tc.n, tc.n, tc.name, tc.name)
	}
	body.WriteString("    if ( failures != 0 ) { return 1; }\n")
	out, err := cppHeldRun(t, u, body.String())
	if err != nil {
		t.Fatalf("retained ids past the generated table's capacity: %v\n%s", err, out)
	}
}

// retainUnknownIdsWire is a file-form Scene whose body is n unknown uint8
// fields, each id distinct and used once, in first-use order.
func retainUnknownIdsWire(n int) []byte {
	out := []byte{1}
	for slot := 1; slot <= n; slot++ {
		out = binary.AppendUvarint(out, uint64(slot))
		out = append(out, 6, byte(slot))
	}
	out = append(out, 0)
	for i := range n {
		out = binary.LittleEndian.AppendUint64(out, 0x9e3779b97f4a7c15*uint64(i+1)+0x5eed)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(n))
}
