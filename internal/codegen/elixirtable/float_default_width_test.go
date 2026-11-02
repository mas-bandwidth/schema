package elixirtable

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// The reference stores float32 values at their declared width. Authored BEAM
// floats, text reads and wire reads must agree on elision at that same width;
// signed zero remains a distinct stored value in every scalar container.
func TestFloatDefaultWidthElixir(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	u := unitFrom(t, `package floatdefault
enum Slot { A, B }
fixed table Child {
 value float32 = 0.2
}
table Root {
 scalar float32 = 0.2
 keyed [Slot]float32
 samples [2]float32
 children [Slot]Child
 rows [2]Child
 child ?Child
}
`)
	m := tabletext.NewModel(u)
	rows := []struct{ name, text, authored string }{
		{"omitted", `{}`, `%{}`},
		{"declared-default", `{"scalar":0.2}`, `%{"scalar" => 0.2}`},
		{"rounded-default", `{"scalar":0.2}`, `%{"scalar" => 0.20000000298023224}`},
		{"nested-defaults", `{"children":{"A":{"value":0.2}},"rows":[{"value":0.2},{"value":0.2}],"child":{"value":0.2}}`, `%{"children" => %{"A" => %{"value" => 0.2}}, "rows" => [%{"value" => 0.2}, %{"value" => 0.2}], "child" => %{"value" => 0.2}}`},
		{"positive-underflow", `{"keyed":{"A":1e-50},"samples":[1e-50,0]}`, `%{"keyed" => %{"A" => 1.0e-50}, "samples" => [1.0e-50, 0.0]}`},
		{"negative-zero", `{"scalar":-0.0,"keyed":{"A":-0.0},"samples":[-0.0,0]}`, `%{"scalar" => -0.0, "keyed" => %{"A" => -0.0}, "samples" => [-0.0, 0.0]}`},
		{"negative-underflow", `{"keyed":{"A":-1e-50},"samples":[-1e-50,0]}`, `%{"keyed" => %{"A" => -1.0e-50}, "samples" => [-1.0e-50, 0.0]}`},
		{"nondefault", `{"scalar":0.3,"keyed":{"A":0.3},"samples":[0.3,0.2]}`, `%{"scalar" => 0.3, "keyed" => %{"A" => 0.3}, "samples" => [0.3, 0.2]}`},
	}
	var probe strings.Builder
	probe.WriteString(`
defmodule Probe do
 alias Floatdefault.Table, as: T
 def bits(value) do
  <<bits::unsigned-64>> = <<value::float-64>>
  bits
 end
 def check(true, _), do: :ok
 def check(false, name) do
  IO.puts("FAIL " <> name)
  Process.put(:failures, Process.get(:failures, 0) + 1)
 end
 def row(name, text, authored, want) do
  store = %{records: [], used: 0, cap: 4096, ids: [], id_cap: 256}
  zero = %{unknown: 0, kind_mismatch: 0, widened: 0, clamped: 0, malformed: false, retained: 0, retain_lost: 0}
  {:ok, parsed, report} = T.from_json("Root", text)
  check(not report.malformed, name <> " JSON report")
  {:ok, loaded, report} = T.load("Root", want)
  check(not report.malformed, name <> " wire report")
  {:ok, retained, report, loaded_store} = T.load_retain("Root", want, store, zero)
  check(not report.malformed, name <> " retained wire report")
  for {kind, value, retain} <- [{"authored", authored, store}, {"JSON", parsed, store}, {"wire", loaded, store}, {"retained-wire", retained, loaded_store}] do
   check(T.save("Root", value) == {:ok, want}, name <> " " <> kind <> " plain canonical wire")
   case T.save_retain("Root", value, retain, zero) do
    {:ok, wire, report, _} ->
     check(wire == want and not report.malformed, name <> " " <> kind <> " retained canonical wire")
    _ -> check(false, name <> " " <> kind <> " retained save refused")
   end
  end
 end
end
`)
	wantDefaultBits := math.Float64bits(float64(float32(0.2)))
	fmt.Fprintf(&probe, "Probe.check(Probe.bits(%%Floatdefault.Child{}.value) == %d, \"fixed constructor native float32 default\")\n", wantDefaultBits)
	fmt.Fprintf(&probe, "{:ok, defaults, _} = Floatdefault.Table.from_json(\"Root\", \"{}\")\nProbe.check(Probe.bits(defaults[\"scalar\"]) == %d, \"form1 text default native float32\")\n", wantDefaultBits)
	wires := map[string][]byte{}
	for _, row := range rows {
		value := m.New(m.Lookup("Root"))
		var report tabletext.Report
		if !m.Read(value, []byte(row.text), &report) || report != (tabletext.Report{}) {
			t.Fatalf("%s oracle text: %+v", row.name, report)
		}
		wire, err := tablewire.Encode(m, value)
		if err != nil {
			t.Fatalf("%s oracle encode: %v", row.name, err)
		}
		wires[row.name] = wire
		if row.name == "omitted" {
			fmt.Fprintf(&probe, "{:ok, defaults, _} = Floatdefault.Table.load(\"Root\", Base.decode16!(%q, case: :lower))\nProbe.check(Probe.bits(defaults[\"scalar\"]) == %d, \"form1 wire default native float32\")\n", hex.EncodeToString(wire), wantDefaultBits)
		}

		fmt.Fprintf(&probe, "Probe.row(%s, %s, %s, Base.decode16!(%q, case: :lower))\n", elixirString(row.name), elixirString(row.text), row.authored, hex.EncodeToString(wire))
	}
	if !bytes.Equal(wires["omitted"], wires["declared-default"]) || !bytes.Equal(wires["omitted"], wires["positive-underflow"]) {
		t.Fatal("oracle must elide declared/default-width values")
	}
	if bytes.Equal(wires["positive-underflow"], wires["negative-underflow"]) {
		t.Fatal("oracle must preserve the sign of rounded zero")
	}
	probe.WriteString("if Process.get(:failures, 0) != 0, do: raise(\"float default width assertions failed\")\nIO.puts(\"ok\")\n")
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe.String())
	if err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("float default width: %v\n%s", err, out)
	}
}
