package elixirtable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestFixedCountedArrayBelowMinRefuses pins the fixed-read cell of rule 2 in
// the 2026-11-21 review's verify-values.md: a counted array `[Min..Max]T`
// whose count is below Min is refused on read (docs/SPEC.md §5.1 row 3, §4.2;
// the Go engine at internal/tablewire/fixed.go). The fixed reader held the
// count to [0, Max] only, so a count of 1 in a `[2..4]` read as one element.
// It now holds [Min, Max], on the identity read and on a read through a
// lineage (the older writer's layout).
//
// It runs elixir, so it is a functional test behind slowtest.Gate.
func TestFixedCountedArrayBelowMinRefuses(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Elixir (fixed counted array below its Min)")
	bin := elixirBinary(t)
	old := "package slack\nfixed table Root { vals [2..4]uint8 }"
	newer := "package slack\nfixed table Root { vals [2..4]uint16 }"
	dir := t.TempDir()
	write := `
    dir = ` + elixirString(dir) + `
    header = binary_part(Slack.ProbeFixed.root_fixed_save([%Slack.Root{vals: [7, 8]}]), 0, Slack.ProbeFixed.root_fixed_measure(0))
    wire = fn n, live ->
      header <> <<Slack.ProbeFixed.root_fixed_hash()::little-unsigned-64, n::little-signed-32>> <>
        live <> :binary.copy(<<0>>, 4 - byte_size(live))
    end
    File.write!(Path.join(dir, "min.bin"), wire.(2, <<7, 8>>))
    File.write!(Path.join(dir, "one.bin"), wire.(1, <<7>>))
    File.write!(Path.join(dir, "zero.bin"), wire.(0, <<>>))
`
	t14ElixirRun(t, bin, old, nil, write)
	for _, read := range []struct {
		name, schema string
		lineage      []string
	}{
		{"identity", old, nil},
		{"lineage", newer, []string{old}},
	} {
		t.Run(read.name, func(t *testing.T) {
			body := `
      dir = ` + elixirString(dir) + `
      {:ok, [value], _report} = Slack.ProbeFixed.root_fixed_load(File.read!(Path.join(dir, "min.bin")))
      check(value.vals == [7, 8], "a [2..4] at its Min did not read: #{inspect(value.vals)}")
      for name <- ["one", "zero"] do
        got = Slack.ProbeFixed.root_fixed_load(File.read!(Path.join(dir, name <> ".bin")))
        check(match?({:error, _, _}, got), "a [2..4] count of #{name} was read: #{inspect(got)}")
      end
`
			t14ElixirRun(t, bin, read.schema, read.lineage, body)
		})
	}
}

// TestFixedCountedArrayHoldsMinEmitted is the fast-lane half: the fixed
// reader's count check carries the declared Min.
func TestFixedCountedArrayHoldsMinEmitted(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, "package fixmin\n\nfixed table Root\n{\n    vals [2..4]uint8\n}\n")
	var all strings.Builder
	for _, src := range generateElixir(t, u) {
		all.Write(src)
	}
	text := all.String()
	if !strings.Contains(text, ", 2, 4)") || !strings.Contains(text, "R.clamps(c, n_") {
		t.Errorf("the fixed reader does not hold a [2..4] count to [2, 4]")
	}
	if strings.Contains(text, ", 0, 4)") {
		t.Errorf("the fixed reader still holds a [2..4] count to [0, 4]")
	}
}
