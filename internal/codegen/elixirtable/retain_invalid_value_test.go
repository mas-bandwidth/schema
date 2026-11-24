package elixirtable

import (
	"path/filepath"
	"testing"
)

// TestRetainSaveRefusesInvalidValues pins docs/SPEC-TABLES.md section 3: the
// retaining writer refuses an invalid value through its public API, just like
// the plain writer, instead of letting the internal refusal escape its case.
func TestRetainSaveRefusesInvalidValues(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(root, "tables", "cards", "Card.schema"))
	files := generateElixir(t, u)
	dir := t.TempDir()
	names := writeModuleDir(t, dir, files)
	probe := `
retain = %{records: [], used: 0, cap: 1048576, ids: [], id_cap: 131072}
report = %{unknown: 0, widened: 0, retained: 0, retain_lost: 0, reason: "", missing_required: 0}
valid = %{"id" => "a"}
{:ok, wire, _report, _retain} = Cards.Table.save_retain("Card", valid, retain, report)
unless byte_size(wire) > 0 and Cards.Table.measure_retain("Card", valid, retain) == byte_size(wire),
  do: raise("valid retained save and measure disagree")
invalid = Map.put(valid, "id", [])
unless Cards.Table.measure_retain("Card", invalid, retain) == -1,
  do: raise("measure_retain did not refuse the invalid value")
unless Cards.Table.save_retain("Card", invalid, retain, report) == {:error, :invalid_value, report, retain},
  do: raise("save_retain did not refuse the invalid value through its public API")
IO.puts("ok")
`
	runProbe(t, elixirBin, dir, names, probe)
}
