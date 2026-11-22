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
// batch_too_large, and no records; load_messages hands back a fresh list, and
// load_messages_into resets the caller's list to empty on every refusal (ruling
// R4: framing, capacity and body refusals alike), where a success replaces it. A capacity of two
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
// A refused read resets the caller's list (ruling R4): every refusal, framing
// or body or capacity, answers a count of 0 and an EMPTY list, where the
// caller handed in three records all holding the sentinel 17.
def sentinels() -> [schema_rt.Record]:
    let held:[schema_rt.Record] = []
    for(3):
        let r = schema_rt.new_record(schema_rt.table_of("LoginRequest"))
        schema_rt.slot_named(r, "client_build").cell.i = 17
        push(held, r)
    return held
let refusals = [write_int8_le(copy(wire), 0, 3), substring(wire, 0, length(wire) - 1), write_int8_le(copy(wire), length(wire) - 1, 255 - read_uint8_le(wire, length(wire) - 1)), ""]
var ri = 0
for(refusals) bad:
    let held = sentinels()
    let c, k = schema_rt.load_messages_into("LoginRequest", v, bad, schema_rt.LoadReport {}, 2, held)
    print("refused {ri} {c} {k} {length(held)}")
    ri += 1
let over = sentinels()
let oc, ok3 = schema_rt.load_messages_into("LoginRequest", v, wire, schema_rt.LoadReport {}, 1, over)
print("refused over {oc} {ok3} {length(over)}")
let good = sentinels()
let gc, ok4 = schema_rt.load_messages_into("LoginRequest", v, wire, schema_rt.LoadReport {}, 2, good)
print("read {gc} {ok4} {length(good)} {schema_rt.slot_named(good[0], "client_build").cell.i} {schema_rt.slot_named(good[1], "client_build").cell.i}")
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", map[string][]byte{"conn": conn, "wire": fixture})))
	if want := "one 0 0 false batch_too_large\ntwo 2 2 true\nrefused 0 0 false 0\nrefused 1 0 false 0\nrefused 2 0 false 0\nrefused 3 0 false 0\nrefused over 0 false 0\nread 2 true 2 40 40"; got != want {
		t.Fatalf("capacity fixture:\n got  %q\n want %q", got, want)
	}
}
