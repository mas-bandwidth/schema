package lobstertable_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The last repeated key chooses the entry's key even when an earlier key
// had an incompatible kind (SPEC-TABLES section 2.8). Compare reports and
// complete values against the independent wire reader.
func TestLobsterMapRepeatedKeyLastKindWins(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lobster")
	path := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(path, []byte("package probe\ntable Root { entries map[uint32]int32 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	var rows []loadRow
	for _, tc := range []struct {
		name string
		keys []byte
	}{
		{"bad_then_widened", []byte{2, 9, 0, 0, 0, 0, 0, 0, 0, 0, 2, 6, 7}},
		{"widened_then_bad", []byte{2, 6, 7, 2, 9, 0, 0, 0, 0, 0, 0, 0, 0}},
		{"bad_then_exact", []byte{2, 9, 0, 0, 0, 0, 0, 0, 0, 0, 2, 8, 7, 0, 0, 0}},
	} {
		entry := append(append([]byte(nil), tc.keys...), 0)
		payload := append([]byte{13, 1, byte(len(entry))}, entry...)
		wire := append([]byte{1, 1, 14, byte(len(payload))}, payload...)
		wire = append(wire, 0)
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(g.u.Tables["Root"].Fields[0]))
		wire = binary.LittleEndian.AppendUint64(wire, ir.MapKeyWireId)
		wire = binary.LittleEndian.AppendUint64(wire, 2)
		rows = append(rows, loadRow{name: tc.name, root: "Root", data: wire})
	}
	got, reports, values := runCollectionsLoad(t, g, rows)
	for i, row := range rows {
		if got[2*i] != reports[i] || got[2*i+1] != values[i] {
			t.Fatalf("%s: report %s want %s; value %s want %s", row.name, got[2*i], reports[i], got[2*i+1], values[i])
		}
	}
	// Removing the reset must restore the original misdecode, not merely
	// provoke a compiler error or an unrelated runtime failure.
	runtime := filepath.Join(g.dir, "load.lobster")
	src, err := os.ReadFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "            if kind != want and widens(kind, want):\n                kind_bad = false\n"
	if strings.Count(string(src), anchor) != 1 {
		t.Fatal("missing unique widening reset")
	}
	if err := os.WriteFile(runtime, []byte(strings.Replace(string(src), anchor, "            if kind != want and widens(kind, want):\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	broken, _, _ := runCollectionsLoad(t, g, rows)
	if broken[0] == reports[0] && broken[1] == values[0] {
		t.Fatal("removing reset did not expose repeated-key regression")
	}
	for i := 1; i < len(rows); i++ {
		if broken[2*i] != reports[i] || broken[2*i+1] != values[i] {
			t.Fatalf("control changed unrelated case %s", rows[i].name)
		}
	}
}

func TestLobsterMapRepeatedKeySharedVector(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lobster")
	paths, err := filepath.Glob(filepath.Join(root, "tables/maps/*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("map schemas: %v", err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatal(err)
	}
	const wire = "testdata/wire/tables/fuzz-vectors/map_repeated_key_widen.bin"
	rows := []loadRow{{name: "map_repeated_key_widen", root: "Pairs", wire: wire}}
	got, reports, values := runCollectionsLoad(t, g, rows)
	if got[0] != reports[0] || got[1] != values[0] {
		t.Fatalf("shared vector: got %v, want report %s value %s", got, reports[0], values[0])
	}
	r := retainRow{name: rows[0].name, root: "Pairs", wire: wire, capacity: "full", ids: "full"}
	retained := runRetainRows(t, g, []retainRow{r})[0]
	want := goRetainRow(t, g, r, 1<<20, 1<<20)
	if retained != want {
		t.Fatalf("retained shared vector: got %q, want %q", retained, want)
	}
}
