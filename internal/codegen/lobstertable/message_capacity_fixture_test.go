package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// TestLobsterMessageLoadCapacityFixture reads the committed capacity fixture,
// testdata/batch_capacity_two.bin (two LoginRequest bodies of tables/backend,
// client_build 40, written by the Go engine and pinned to it by
// test/conformance/harness TestBatchCapacityFixtureIsTheGoEnginesInEveryLeg),
// through the emitted message runtime's load_messages. A capacity of one owes
// "A refused batch returns a count of `0`" (docs/SPEC-TABLES.md §3.3): count 0,
// batch_too_large, and no records; Lobster hands back a fresh list rather than
// writing caller storage, so "untouched" is the empty list. A capacity of two
// owes both records, each carrying 40. A caller's capacity is a resource limit
// (§6.5), so this row runs here, in the leg's own tests, never in the shared
// suites.
func TestLobsterMessageLoadCapacityFixture(t *testing.T) {
	t.Parallel()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "tables", "backend")})
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("unit backenddemo is refused: %v", err)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "batch_capacity_two.bin"))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "backend_conn.bin"))
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\nimport .message\n")
	b.WriteString(keyedRegistrations(g.u))
	b.WriteString(`let v = schema_rt.new_vocabulary(schema_rt.message_entries_here())
let announcement = read_file("conn")
assert announcement
assert schema_rt.announce_read(v, announcement, schema_rt.LoadReport {})
let wire = read_file("wire")
assert wire
let rep = schema_rt.LoadReport {}
let one, n, ok = schema_rt.load_messages("LoginRequest", v, wire, rep, 1)
print("one {n} {length(one)} {ok} {rep.reason}")
let two, m, ok2 = schema_rt.load_messages("LoginRequest", v, wire, schema_rt.LoadReport {}, 2)
print("two {m} {length(two)} {ok2}")
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", map[string][]byte{"conn": conn, "wire": fixture})))
	if want := "one 0 0 false batch_too_large\ntwo 2 2 true"; got != want {
		t.Fatalf("capacity fixture:\n got  %q\n want %q", got, want)
	}
}
