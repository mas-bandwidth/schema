package pythontable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestPythonMessageLoadCapacityFixture reads the committed capacity fixture,
// testdata/batch_capacity_two.bin (two LoginRequest bodies of tables/backend,
// client_build 40, written by the Go engine and pinned to it by
// test/conformance/harness TestBatchCapacityFixtureIsTheGoEnginesInEveryLeg),
// through the generated Python's load_messages. A capacity of one owes "A
// refused batch returns a count of `0`" (docs/SPEC-TABLES.md §3.3): the
// Python read raises Refused("batch_too_large") and returns nothing, so no
// count and no body reaches the caller. A capacity of two owes both bodies,
// each carrying 40. A caller's capacity is a resource limit (§6.5), so this
// row runs here, in the leg's own tests, never in the shared suites.
func TestPythonMessageLoadCapacityFixture(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreter")
	root := filepath.Join("..", "..", "..")
	u := loadUnitPaths(t, filepath.Join(root, "tables", "backend", "Backend.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "batch_capacity_two.bin"))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "backend_conn.bin"))
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from backenddemo import LoginRequest, MessageVocabulary, announce_read, load_messages, Refused
with open('conn','rb') as f: conn=f.read()
with open('wire','rb') as f: wire=f.read()
v=MessageVocabulary();announce_read(v,conn)
try:
    load_messages(LoginRequest,v,wire,1)
except Refused as e:
    print('one refused',e.reason)
else:
    print('one delivered')
two=load_messages(LoginRequest,v,wire,2)
print('two',len(two),' '.join(str(x.client_build) for x in two))
`
	dir := t.TempDir()
	writeTree(t, dir, files)
	out, stderr, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe), "conn": conn, "wire": fixture})
	if got, want := strings.TrimSpace(out), "one refused batch_too_large\ntwo 2 40 40"; err != nil || got != want {
		t.Fatalf("capacity fixture: %v\n got  %q\n want %q\n%s", err, got, want, stderr)
	}
}
