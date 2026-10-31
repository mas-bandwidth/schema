package elixirtable

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestOptionalVariableClosureElixir holds docs/SPEC-TABLES.md §2.3's `?T` and
// by-value `?[N]T` over a VARIABLE closure (a member whose closure holds a
// pointer) on the Elixir variable form, BYTE FOR BYTE against the Go oracle
// (internal/tablewire): every walk that follows pointer edges gates on
// presence, so an absent value's table is not descended and a pointer it would
// hold costs no record.
//
// In this target an absent optional IS nil, so the rule is kept by
// representation. Each case is saved from a value the probe BUILDS, and the
// oracle's own wire is loaded and saved again; both must be the oracle's
// bytes. A loaded absent optional is a key holding nil, which is the shape the
// node walk must step over.
//
// THE REVERSED WITNESS plants the old walk back (a nested table read with
// `Map.get(value, f.name, %{})`, which hands nil to the walk): the reload of
// the absent case must then go red.
func TestOptionalVariableClosureElixir(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)

	u := unitFrom(t, optionalVariableProbeSrc)
	m := tabletext.NewModel(u)
	outer := m.Lookup("Outer")

	var probe strings.Builder
	probe.WriteString(`
defmodule Probe do
  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end

  def hex(bin), do: Base.encode16(bin, case: :lower)
end
`)
	for _, pc := range optionalVariableProbeCases {
		inst := m.New(outer)
		var rep tabletext.Report
		if !m.Read(inst, []byte(pc.text), &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("%s: the oracle did not read the probe text cleanly: %+v", pc.name, rep)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: the oracle did not encode: %v", pc.name, err)
		}
		want := hex.EncodeToString(wire)
		fmt.Fprintf(&probe, `
{:ok, built} = Ovprobe.Table.save("Outer", %s)
Probe.check(Probe.hex(built) == %q, %q <> " built: " <> Probe.hex(built))
{:ok, loaded, _report} = Ovprobe.Table.load("Outer", Base.decode16!(%q, case: :lower))
{:ok, again} = Ovprobe.Table.save("Outer", loaded)
Probe.check(Probe.hex(again) == %q, %q <> " reloaded: " <> Probe.hex(again))
`, pc.build, want, pc.name, want, want, pc.name)
	}
	probe.WriteString("IO.puts(\"ok\")\n")

	files := generateElixir(t, u)
	if out, err := runArmProbe(t, elixirBin, files, probe.String()); err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("the Elixir variable form does not keep §2.3 over a variable closure: %v\n%s", err, out)
	}

	// the reversed witness
	const fixed = "          case Map.get(value, f.name) do\n            nil -> acc\n            v -> collect_nodes(Map.fetch!(@tables, f.elem), v, acc)\n          end\n"
	const old = "          collect_nodes(Map.fetch!(@tables, f.elem), Map.get(value, f.name, %{}), acc)\n"
	planted := map[string][]byte{}
	n := 0
	for name, data := range files {
		s := string(data)
		n += strings.Count(s, fixed)
		planted[name] = []byte(strings.ReplaceAll(s, fixed, old))
	}
	if n == 0 {
		t.Fatal("the witness found no presence-gated table edge in the generated walk to plant the old one over")
	}
	if out, err := runArmProbe(t, elixirBin, planted, probe.String()); err == nil {
		t.Fatalf("the walk with nil handed to it still passed the probe: the probe does not see the rule\n%s", out)
	}
}

const optionalVariableProbeSrc = `package ovprobe

table Inner
{
    text *string
    n    int32
}

table Outer
{
    inner ?Inner
    row   ?[2]Inner
    tail  int32
}
`

// optionalVariableProbeCases are the probe's values: the oracle's text and the
// Elixir term that builds the same value directly.
var optionalVariableProbeCases = []struct{ name, text, build string }{
	{"absent", `{"tail":5}`, `%{"tail" => 5}`},
	{"present", `{"inner":{"text":"hi","n":3},"row":[{"text":"a"},{"n":7}],"tail":5}`,
		`%{"inner" => %{"text" => "hi", "n" => 3}, "row" => [%{"text" => "a"}, %{"n" => 7}], "tail" => 5}`},
	{"empty", `{"inner":{"text":""},"tail":5}`, `%{"inner" => %{"text" => ""}, "tail" => 5}`},
	{"row_absent", `{"inner":{"text":"x"},"tail":1}`, `%{"inner" => %{"text" => "x"}, "tail" => 1}`},
}
