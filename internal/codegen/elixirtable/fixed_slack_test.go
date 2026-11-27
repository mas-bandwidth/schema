package elixirtable

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

// THE SLOTS PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED (docs/SPEC-TABLES.md
// §3.4; docs/SPEC-CONFORMANCE.md, "Counted-array slots after a lawful fixed
// load"), the recipe in test/tables/FixedSlack.schema's header. Elixir values
// are immutable and <root>_fixed_load returns fresh structs, so the caller
// has no value for a load to reuse: the leg's form of the rule is that every
// counted array comes back as a list of exactly its count, with no slot past
// it, whatever was loaded before, and that a refused load returns no value.
// The leg holds the rule already (each <T>_fixed_list walks only the live
// count); this test guards it.
func TestElixirFixedSlackPastCountHoldsDefaults(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "elixir (it runs the generated fixed load over FixedSlack.schema)")
	elixirBin := elixirBinary(t)
	// the probe builds its own files; the helper's file input is unused
	unused := filepath.Join(t.TempDir(), "unused.bin")
	if err := os.WriteFile(unused, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	body := `  cell = fn n -> %Tblslack.SlackCell{id: 77, subs: List.duplicate(%Tblslack.SlackSub{v: 77}, n)} end
  a = %Tblslack.SlackRoot{cells: List.duplicate(cell.(2), 3), nums: [77, 77, 77, 77]}
  b = %Tblslack.SlackRoot{cells: [cell.(1)], nums: [77]}
  fa = T.slack_root_fixed_save([a])
  fb = T.slack_root_fixed_save([b])
  check(is_binary(fa) and is_binary(fb), "the saves: #{inspect(fa)} #{inspect(fb)}")
  {:ok, [fresh], _} = load(fb)
  {:ok, [loaded_a], _} = load(fa)
  check(length(loaded_a.cells) == 3 and length(loaded_a.nums) == 4, "A read back #{inspect(loaded_a)}")
  {:ok, [after_a], _} = load(fb)
  want = %Tblslack.SlackRoot{cells: [%Tblslack.SlackCell{id: 77, subs: [%Tblslack.SlackSub{v: 77}]}], nums: [77]}
  check(fresh == want, "a load of B holds #{inspect(fresh)}, want exactly B's live elements #{inspect(want)}")
  check(after_a == want, "a load of B after A holds #{inspect(after_a)}, want #{inspect(want)}")
  short = binary_part(fb, 0, byte_size(fb) - 1)
  case load(short) do
    {:error, _reason, _report} -> check(true, "refused")
    other -> check(false, "the truncated file read: #{inspect(other)}")
  end`
	out, err := runRootProbe(t, elixirBin, "FixedSlack", "SlackRoot", nil, 0, unused, body)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	t.Logf("%s", out)
}
