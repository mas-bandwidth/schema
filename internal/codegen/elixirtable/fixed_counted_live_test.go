package elixirtable

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// Compilation emits the bound-many widening entries; execution counts only
// the live elements. Hostile slack is neither content nor a counter event.
func TestFixedCountedArrayLiveExtent(t *testing.T) {
	slowtest.Gate(t, "Elixir (native counted-array slack and widening)")
	bin := elixirBinary(t)
	for _, tc := range []struct {
		name, old, new, values string
		width                  int
	}{
		{"integer", "int16", "int32", "[-7, 9, 3, 4]", 2},
		{"float", "float32", "float64", "[-7.0, 9.0, 3.0, 4.0]", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := fmt.Sprintf("package slack\nfixed table Root { vals [..4]%s }", tc.old)
			newer := fmt.Sprintf("package slack\nfixed table Root { vals [..4]%s }", tc.new)
			dir := t.TempDir()
			write := fmt.Sprintf(`
    dir = %q
    header = Slack.ProbeFixed.root_fixed_save([])
    for n <- [0, 1, 4] do
      value = %%Slack.Root{vals: Enum.take(%s, n)}
      wire = Slack.ProbeFixed.root_fixed_save([value])
      body = binary_part(wire, byte_size(header) + 8, 4 + 4 * %d)
      <<count::little-unsigned-32, payload::binary>> = body
      check(count == n, "writer count")
      slack_bytes = (4 - n) * %d
      check(binary_part(payload, n * %d, slack_bytes) == :binary.copy(<<0>>, slack_bytes), "canonical writer left nonzero slack")
      live = binary_part(payload, 0, n * %d)
      hostile = header <> <<Slack.ProbeFixed.root_fixed_hash()::little-unsigned-64, n::little-unsigned-32>> <> live <> :binary.copy(<<255>>, slack_bytes)
      File.write!(Path.join(dir, "clean#{n}.bin"), wire)
      File.write!(Path.join(dir, "hostile#{n}.bin"), hostile)
    end
`, dir, tc.values, tc.width, tc.width, tc.width, tc.width)
			if tc.name == "float" {
				write += fmt.Sprintf(`
    bad = header <> <<Slack.ProbeFixed.root_fixed_hash()::little-unsigned-64, 1::little-unsigned-32>> <> :binary.copy(<<255>>, 16)
    File.write!(Path.join(%q, "active-nan.bin"), bad)
`, dir)
			}
			t14ElixirRun(t, bin, old, nil, write)
			for _, read := range []struct {
				name, schema string
				lineage      []string
				widen        bool
			}{
				{"identity", old, nil, false},
				{"lineage", newer, []string{old}, true},
			} {
				t.Run(read.name, func(t *testing.T) {
					body := fmt.Sprintf(`
      for n <- [0, 1, 4], kind <- ["clean", "hostile"] do
        data = File.read!(Path.join(%q, "#{kind}#{n}.bin"))
        {:ok, [value], report} = Slack.ProbeFixed.root_fixed_load(data)
        check(value.vals == Enum.take(%s, n), "live values changed")
        expected = if %t, do: n, else: 0
        check(report.widened == expected, "widened counted slack: #{inspect(report)}")
        check(report.clamped == 0 and report.unknown == 0 and report.kind_mismatch == 0 and not report.malformed, "slack changed counters")
        saved = Slack.ProbeFixed.root_fixed_save([value])
        empty = Slack.ProbeFixed.root_fixed_save([])
        body = binary_part(saved, byte_size(empty) + 8, byte_size(saved) - byte_size(empty) - 8)
        <<count::little-unsigned-32, payload::binary>> = body
        check(count == n, "reader writer count")
        width = div(byte_size(payload), 4)
        check(binary_part(payload, n * width, (4 - n) * width) == :binary.copy(<<0>>, (4 - n) * width), "resaved slack is not zero")
      end
`, dir, tc.values, read.widen)
					if tc.name == "float" {
						body += fmt.Sprintf(`
      active = File.read!(Path.join(%q, "active-nan.bin"))
      check(match?({:error, :non_finite_float, _}, Slack.ProbeFixed.root_fixed_load(active)), "live NaN was accepted")
`, dir)
					}
					t14ElixirRun(t, bin, read.schema, read.lineage, body)
				})
			}
		})
	}
}
