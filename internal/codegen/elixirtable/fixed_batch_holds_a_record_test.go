package elixirtable

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestElixirFixedBatchHoldsARecord holds the Elixir leg to the rule that a
// fixed-form file holds at least one record (docs/FIXED-FORM-ALGORITHM.md §5.3
// step 9, docs/SPEC-TABLES.md §22.6, docs/rulings.json fixed-form-edges),
// against the testdata/wire/fixed-empty-batch/ vector. The no-record file is
// refused by every fixed read entry, the one-record file still reads, and a
// save of no record is refused by the leg's own refusal value without raising
// (docs/rulings.json write-refusal). Only the verdict is compared, never a
// reason (refusal-verdict-only).
//
// This is a functional test: it runs the generated Elixir behind SCHEMA_SLOW.
func TestElixirFixedBatchHoldsARecord(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Elixir (native fixed batch holds a record)")
	bin := elixirBinary(t)
	vector := filepath.Join("..", "..", "..", "testdata", "wire", "fixed-empty-batch")
	schema, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixed-empty-batch", "Batch.schema"))
	if err != nil {
		t.Fatal(err)
	}
	one, err := filepath.Abs(filepath.Join(vector, "one-record.bin"))
	if err != nil {
		t.Fatal(err)
	}
	none, err := filepath.Abs(filepath.Join(vector, "no-record.bin"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{one, none} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("the fixed-empty-batch vector is missing: %v", err)
		}
	}
	body := fmt.Sprintf(`
    one = File.read!(%q)
    none = File.read!(%q)
    f = Fixedemptybatch.ProbeFixed
    table = Fixedemptybatch.Table

    {:ok, [value], _} = f.batch_fixed_load(one)
    check(value.a == 7 and value.name == "hello", "the one-record file read a different value: " <> inspect(value))
    check(match?({:ok, [_], _}, f.batch_fixed_load(one, copy: true)), "the one-record file did not read with copy")
    check(match?({:ok, [_], _}, f.batch_fixed_load(one, batch_capacity: 1)), "the one-record file did not read at capacity 1")
    check(match?({:ok, _, _}, table.load("Batch", one)), "Table.load refused the one-record file")

    for {entry, result} <- [
          {"batch_fixed_load/1", f.batch_fixed_load(none)},
          {"batch_fixed_load/2 copy", f.batch_fixed_load(none, copy: true)},
          {"batch_fixed_load/2 capacity", f.batch_fixed_load(none, batch_capacity: 1)},
          {"Table.load", table.load("Batch", none)}
        ] do
      check(match?({:error, _, _}, result), entry <> " read the no-record file: " <> inspect(result))
    end

    saved = f.batch_fixed_save([value])
    check(saved == one, "a save of the one record did not write the vector's bytes")
    refused = f.batch_fixed_save([])
    check(match?({:error, _}, refused), "a save of no record was not refused: " <> inspect(refused))
`, one, none)
	t14ElixirRun(t, bin, string(schema), nil, body)
}
