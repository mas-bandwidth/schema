package cpptable

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Reference zero is None/a body terminator; a nonzero reference to hash zero
// is an ordinary unknown ID. These are the six nested shapes also pinned by
// the corrected Go reference, with nonzero twins and actual no-ID controls.
func TestCppRetainNestedHashZero(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := loadHeldUnitPaths(t, root, []string{filepath.Join("tables", "pointers")})
	var body strings.Builder
	body.WriteString("    int failures = 0;\n")
	cases := []struct {
		name  string
		body  []byte
		inner bool
	}{
		{"enum", []byte{1, 30, 2, 0}, true},
		{"union", []byte{1, 15, 2, 6, 1, 7, 0}, true},
		{"body-field", []byte{1, 13, 4, 2, 6, 7, 0, 0}, true},
		{"keyed-slot", []byte{1, 16, 5, 6, 1, 2, 1, 7, 0}, true},
		{"enum-array-and-None", []byte{1, 14, 4, 30, 2, 2, 0, 0}, true},
		{"union-array-and-None", []byte{1, 14, 7, 15, 2, 2, 6, 1, 7, 0, 0}, true},
		{"enum-None", []byte{1, 30, 0, 0}, false},
		{"union-None", []byte{1, 15, 0, 0}, false},
		{"empty-body", []byte{1, 13, 1, 0, 0}, false},
	}
	for _, tc := range cases {
		ids := []uint64{ir.TableWireId("future_inner_hash"), 0}
		if !tc.inner {
			ids = ids[:1]
		}
		for _, id := range ids {
			name := tc.name
			if tc.inner {
				if id == 0 {
					name += "/hash-zero"
				} else {
					name += "/nonzero-control"
				}
			}
			wire := append([]byte{1}, tc.body...)
			wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId("future_outer_hash"))
			count := uint64(1)
			if tc.inner {
				wire = binary.LittleEndian.AppendUint64(wire, id)
				count++
			}
			wire = binary.LittleEndian.AppendUint64(wire, count)
			fmt.Fprintf(&body, `
    {
        const uint8_t wire[] = { %s };
        alignas( 64 ) static uint8_t region[1 << 18];
        static uint8_t store[1 << 16];
        TableRetain::Id ids[32];
        TableRetain retain;
        retain.bytes = store; retain.capacity = (int64_t) sizeof( store );
        retain.ids = ids; retain.id_capacity = 32;
        TableReport plain_report = {};
        if ( SceneLoad( region, sizeof( region ), wire, sizeof( wire ), &plain_report ) == NULL || plain_report.unknown != 1 )
            { printf( "%s plain load failed\n" ); failures++; }
        std::vector<uint8_t> current( wire, wire + sizeof( wire ) );
        for ( int pass = 0; pass < 2; pass++ )
        {
            TableReport report = {};
            const Scene * value = SceneLoadRetain( region, sizeof( region ), current.data(), (int64_t) current.size(), &retain, &report );
            if ( value == NULL || report.unknown != 1 || report.retained != 1 || report.retain_lost != 0 )
                { printf( "%s retain pass=%%d failed unknown=%%d retained=%%d lost=%%d\n", pass, (int) report.unknown, (int) report.retained, (int) report.retain_lost ); failures++; break; }
            std::vector<uint8_t> saved(1 << 18);
            TableReport save_report = {};
            const int64_t wrote = SceneSaveRetain( value, &retain, saved.data(), (int64_t) saved.size(), &save_report );
            if ( wrote != (int64_t) sizeof( wire ) || memcmp( saved.data(), wire, sizeof( wire ) ) != 0 || save_report.retain_lost != 0 )
                { printf( "%s save pass=%%d changed wire or lost data\n", pass ); failures++; break; }
            saved.resize( (size_t) wrote );
            current.swap( saved );
        }
    }
`, cppBytes(wire), name, name, name)
		}
	}
	body.WriteString("    if ( failures != 0 ) { return 1; }\n")
	out, err := cppHeldRun(t, u, body.String())
	if err != nil {
		t.Fatalf("nested hash-zero retention: %v\n%s", err, out)
	}
}
