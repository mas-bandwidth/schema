package elixirtable

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedunlawful"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// elixirUnlawfulAnswer is the elixir leg's answer to one case of the shared
// unlawful-lineage corpus (internal/fixedunlawful).
type elixirUnlawfulAnswer struct {
	// refusal is the leg's OWN refusal name for the case (refusal-verdict-only:
	// the shared harness never compares names; this leg pins its own).
	refusal string
	// defaults is an Elixir boolean over the reader's decoded prefill `d`: the
	// fields the case changes, at their declared defaults (lead = 1, trail = 2
	// are always checked beside it).
	defaults string
}

var elixirUnlawfulAnswers = map[string]elixirUnlawfulAnswer{
	"unknown_census":      {"layout_malformed", `Enum.all?(d.items, &(&1.a == 0))`},
	"unknown_census_many": {"layout_malformed", `Enum.all?(d.items, &(&1.a == 0))`},
	"kind_moved":          {"layout_malformed", `d.v == 0.0`},
	"arm_removed":         {"layout_malformed", `true`},
	"variant_removed":     {"layout_malformed", `true`},
	"key_removed":         {"layout_malformed", `true`},
	"array_shrunk":        {"layout_malformed", `length(d.vals) == 2 and Enum.all?(d.vals, &(&1 == 0))`},
	"counted_shrunk":      {"layout_malformed", `d.vals == []`},
	"text_narrowed":       {"layout_malformed", `byte_size(d.text) == 0`},
	"bytes_narrowed":      {"layout_malformed", `byte_size(d.blob) == 0`},
	// The LAWFUL control loads: no refusal name, no defaults check. Elixir
	// hands back a fresh list and never writes caller rows, so there is no
	// row to poison; the verdict is the values the load landed.
	"lawful_control": {"", ``},
}

// TestElixirFixedUnlawfulCorpus is the elixir leg's driver for the shared
// unlawful-lineage corpus. The probe writes its verdict to a file, and the
// driver answers what the probe wrote: the LAWFUL control's loaded values, or
// "refused" once every check below held. Per UNLAWFUL case: the NEW build,
// handed the OLD layout as a lineage entry, refuses the OLD file before any
// record, every counter zero, no values handed back, and the reader's declared
// defaults standing, on a first load and a second (docs/FIXED-FORM-ALGORITHM.md
// §5.2, §5.4; SPEC-TABLES §6.5). Elixir hands back a fresh list and never
// writes caller rows, so "rows at their declared defaults" is a refusal
// carrying no values beside the reader's decoded prefill. The refusal's name
// is pinned per case below.
func TestElixirFixedUnlawfulCorpus(t *testing.T) {
	t.Parallel()
	elixirBin := lineageElixir(t)
	fixedunlawful.Run(t, "elixirtable", elixirUnlawfulAnswers, func(t *testing.T, c fixedunlawful.Case, a elixirUnlawfulAnswer) string {
		t.Parallel()
		file := lineageCorpus(t, c.File)
		verdict := filepath.Join(t.TempDir(), "verdict.txt")
		var body string
		if c.Lawful {
			body = fmt.Sprintf(`  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok and length(values) > 0, "%[1]s: the LAWFUL control refused: #{inspect({tag, values})} #{inspect(report)}")
  [v | _] = values
  File.write!(%[2]s, "loaded #{length(values)}: lead=#{v.lead} v=#{v.v} trail=#{v.trail}")`, c.Name, elixirString(verdict))
		} else {
			snake := ir.RustSnake(c.Root)
			body = fmt.Sprintf(`  data = File.read!(file())
  for pass <- [1, 2] do
    {tag, why, report} = load(data)
    check(tag == :error and why == :%[2]s,
      "%[1]s load #{pass}: the unlawful entry was not refused %[2]s: #{inspect({tag, why})} #{inspect(report)}")
    check(not is_list(why), "%[1]s load #{pass}: a refusal handed back values: #{inspect(why)}")
    check(is_map(report) and report.unknown == 0 and report.widened == 0 and report.retained == 0 and report.retain_lost == 0,
      "%[1]s load #{pass}: a refusal moved a counter: #{inspect(report)}")
  end
  {d, _c, _m} = T.%[3]s_fixed_decode(prefill(), 0, false)
  check(d.lead == 1 and d.trail == 2 and (%[4]s),
    "%[1]s: the reader's declared defaults moved: #{inspect(d)}")
  if Process.get(:failed) == nil, do: File.write!(%[5]s, "refused")`, c.Name, a.refusal, snake, a.defaults, elixirString(verdict))
		}
		out, err := runVersionProbe(t, elixirBin, c.New, []string{c.Old}, 0, file, body)
		if err != nil {
			t.Fatalf("%s: %v\n%s", c.Name, err, out)
		}
		got, err := os.ReadFile(verdict)
		if err != nil {
			t.Fatalf("%s: the probe wrote no verdict: %v", c.Name, err)
		}
		return string(got)
	})
}
