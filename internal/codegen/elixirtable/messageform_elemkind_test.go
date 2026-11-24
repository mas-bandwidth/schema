package elixirtable

import (
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestElixirMessageElementKindRefusesTheBatch pins docs/SPEC-TABLES.md §3.3
// and §4 ("An array's ELEMENT KIND is part of its identity") on the Elixir
// message read, at parity with the Go engine's messageWidens: a peer field
// whose announced kind agrees and whose ELEMENT kind does not refuses the
// whole batch kind_mismatch, at a counted array, a fixed array, an enum-keyed
// array, an array of records and a union arm; the element widening reads; and
// a moved field kind refuses by the same name, never as damage.
//
// The batches are the corpus's own message-prior rows (PriorWide written,
// PriorNarrow read; testdata/wire/tables/prior), so this probe and every
// leg's driver read the same bytes.
//
// Each rule has a REVERSED WITNESS: the emitted runtime with the old
// behaviour planted back (the element kind ignored, no kind check at an arm,
// the refusal spelled as damage) must turn the probe red on a check.
//
// It runs elixir, so it is a functional test behind slowtest.Gate.
func TestElixirMessageElementKindRefusesTheBatch(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	prior := func(name string) string {
		return elixirString(filepath.Join(root, "testdata", "wire", "tables", "prior", name))
	}
	probe := `
defmodule Probe do
  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end

m = Tblprior.TableMessage
{:ok, ann} = m.announce_read(File.read!(` + prior("tblprior_Prior_conn.bin") + `))
{:ok, vocab} = m.for_root(ann, "Prior")

for {name, path} <- [
      {"a counted array's int32 elements as uint8", ` + prior("prior_elems_kind_message.bin") + `},
      {"a fixed array's int32 elements as uint8", ` + prior("prior_elempair_kind_message.bin") + `},
      {"an enum-keyed array's int32 elements as uint8", ` + prior("prior_elemkeyed_kind_message.bin") + `},
      {"an array of Cell records as uint8", ` + prior("prior_elemcells_kind_message.bin") + `},
      {"an arm's int32 elements as uint8", ` + prior("prior_armelems_kind_message.bin") + `},
      {"a field's int32 as int16", ` + prior("prior_narrowed_kind_message.bin") + `}
    ] do
  got = m.load_messages(vocab, File.read!(path))
  Probe.check(got == {:error, :kind_mismatch}, "#{name} refuses the batch kind_mismatch: #{inspect(got)}")
end

got = m.load_messages(vocab, File.read!(` + prior("prior_elemgrown_inside_message.bin") + `))
Probe.check(match?({:ok, [_]}, got), "uint8 elements read as uint16, the element widening: #{inspect(got)}")
IO.puts("ok")
`
	u := loadUnit(t, filepath.Join(root, "test", "tables", "prior", "PriorNarrow.schema"))
	files := generateElixir(t, u)
	out, err := runArmProbe(t, elixirBin, files, probe)
	if err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
	plants := []struct {
		name     string
		old, new string
	}{
		{"element kind ignored",
			"from_elem == to_elem -> from == to or kind_widens?(from, to)",
			"true -> from == to or kind_widens?(from, to)"},
		{"no kind check at an arm",
			"if is_integer(own_arm) and not peer_kind_compatible?(entry, own_arm),",
			"if false and is_integer(own_arm) and not peer_kind_compatible?(entry, own_arm),"},
		{"the refusal spelled as damage",
			":kind_mismatch -> {:error, :kind_mismatch}",
			":kind_mismatch -> {:malformed, 0, []}"},
	}
	for _, p := range plants {
		t.Run(p.name, func(t *testing.T) {
			planted := maps.Clone(files)
			elixirPatch(t, planted, p.old, p.new)
			out, err := runArmProbe(t, elixirBin, planted, probe)
			if err == nil {
				t.Fatalf("REVERSED WITNESS FAILED: %s planted back left the probe green\n%s", p.name, out)
			}
			if !strings.Contains(out, "FAIL: ") {
				t.Fatalf("REVERSED WITNESS FAILED: the probe went red, but not on a check: %v\n%s", err, out)
			}
		})
	}
}
