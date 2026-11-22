package elixirtable

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestElixirMessageLoadCapacityFixture reads the committed capacity fixture,
// testdata/batch_capacity_two.bin (two LoginRequest bodies of tables/backend,
// client_build 40, written by the Go engine and pinned to it by
// test/conformance/harness TestBatchCapacityFixtureIsTheGoEnginesInEveryLeg),
// through the generated Elixir's load_messages. A capacity of one owes "A
// refused batch returns a count of `0`" (docs/SPEC-TABLES.md §3.3):
// {:error, :batch_too_large, 0}. Elixir hands back a fresh list rather than
// writing caller storage, so "untouched" is the refusal carrying no bodies. A
// capacity of two owes both bodies, each carrying 40. A caller's capacity is a
// resource limit (§6.5), so this row runs here, in the leg's own tests, never
// in the shared suites.
func TestElixirMessageLoadCapacityFixture(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := filepath.Abs(filepath.Join("testdata", "batch_capacity_two.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fixture); err != nil {
		t.Fatal(err)
	}
	u := messageLoadUnit(t, root, []string{"tables/backend"})
	ann := filepath.Join(root, "testdata", "wire", "tables", "backend_conn.bin")
	body := fmt.Sprintf(`    {:ok, announced} = M.announce_read(File.read!(file()))
    {:ok, vocab} = M.for_root(announced, "LoginRequest")
    wire = File.read!(%q)
    one = M.load_messages(vocab, wire, 1)
    check(one == {:error, :batch_too_large, 0}, "capacity one: #{inspect(one)}")
    case M.load_messages(vocab, wire, 2) do
      {:ok, [a, b]} ->
        check(a == b, "the two bodies differ: #{inspect({a, b})}")
        check(Enum.any?(a, fn {_ref, v} -> v == 40 end), "body one carries no 40: #{inspect(a)}")

      other ->
        check(false, "capacity two: #{inspect(other)}")
    end
`, fixture)
	out, err := runMessageProbe(t, u, ann, body)
	if err != nil {
		t.Fatalf("capacity fixture: %v\n%s", err, out)
	}
}
