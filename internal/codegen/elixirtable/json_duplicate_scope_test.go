package elixirtable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// Field repeats preserve accepted values on a rejected shape. Map values use
// their direct reader: wrong shapes are malformed, while inexact numbers place
// a fresh default entry. Keyed duplicates count even before accepted placement.
func TestJSONDuplicateScopeElixir(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	u := unitFrom(t, `package duplicatescope
enum Slot { A, B }
union Pick { value int32 }
table Child { m map[string(2)]int32 }
table Root {
 keyed [Slot]int32
 m map[string(2)]int32
 ints map[int32]int32
 pick Pick
 optionalInts map[string(2)]?int32
 optionalEnums map[string(2)]?Slot
 children [Slot]Child
 rows []Child
 bounded [..1]Child
}
`)
	m := tabletext.NewModel(u)
	var probe strings.Builder
	probe.WriteString(`
defmodule Probe do
 def check(true, _), do: :ok
 def check(false, what) do
  IO.puts("FAIL " <> what)
  Process.put(:failures, Process.get(:failures, 0) + 1)
 end
 def row(text, want_ok, want_json, want_report, name) do
  {ok, value, report} = case Duplicatescope.Table.from_json("Root", text) do
   {:ok, value, report} -> {true, value, report}
   {:error, _, report} -> {false, %{}, report}
  end
  check(ok == want_ok, name <> " read verdict")
  for {key, want} <- want_report do
   check(Map.get(report, key, 0) == want, name <> " report " <> Atom.to_string(key) <> ": " <> inspect(report))
  end
  try do
   got = Duplicatescope.Table.to_json("Root", value)
   check(got == {:ok, want_json}, name <> " canonical value: got=" <> inspect(got) <> " want=" <> inspect(want_json))
  rescue
   error -> check(false, name <> " to_json raised " <> Exception.message(error))
  end
  try do
    case Duplicatescope.Table.save("Root", value) do
    {:ok, wire} when is_binary(wire) ->
     case Duplicatescope.Table.load("Root", wire) do
      {:ok, loaded, report} ->
       check(not report.malformed, name <> " plain load report")
       check(Duplicatescope.Table.to_json("Root", loaded) == {:ok, want_json}, name <> " plain load canonical value")
      result -> check(false, name <> " plain load: " <> inspect(result))
     end
     store = %{records: [], used: 0, cap: 4096, ids: [], id_cap: 256}
     zero = %{unknown: 0, kind_mismatch: 0, widened: 0, clamped: 0, malformed: false, retained: 0, retain_lost: 0}
     case Duplicatescope.Table.load_retain("Root", wire, store, zero) do
      {:ok, loaded, report, _store} ->
       check(not report.malformed, name <> " retaining load report")
       check(Duplicatescope.Table.to_json("Root", loaded) == {:ok, want_json}, name <> " retaining load canonical value")
      result -> check(false, name <> " retaining load: " <> inspect(result))
     end
    result -> check(false, name <> " accepted/default value saves: " <> inspect(result))
   end
  rescue
   error -> check(false, name <> " save raised " <> Exception.message(error))
  end
 end
end
`)
	rows := []struct{ name, text string }{
		{"keyed-rejected-first", `{"keyed":{"A":"bad","A":7}}`},
		{"map-rejected-first", `{"m":{"a":"bad"}}`},
		{"map-valid-then-rejected", `{"m":{"a":7,"a":"bad"}}`},
		{"map-rejected-then-valid", `{"m":{"a":"bad","a":7}}`},
		{"union-field-rejected-repeat", `{"pick":{"value":7},"pick":"bad"}`},
		{"integer-map-invalid-key", `{"ints":{"bad":7}}`},
		{"integer-map-out-of-range-key", `{"ints":{"2147483648":7}}`},
		{"string-map-over-bound-key", `{"m":{"long":7}}`},
		{"map-inexact-first", `{"m":{"a":1.5}}`},
		{"map-valid-then-inexact", `{"m":{"a":7,"a":1.5}}`},
		{"map-inexact-then-valid", `{"m":{"a":1.5,"a":7}}`},
		{"integer-map-canonical-repeat", `{"ints":{"2.0":3,"2":7}}`},
		{"optional-map-null", `{"optionalInts":{"a":null}}`},
		{"optional-map-inexact", `{"optionalInts":{"a":1.5}}`},
		{"optional-map-valid-then-inexact", `{"optionalInts":{"a":7,"a":1.5}}`},
		{"optional-enum-null", `{"optionalEnums":{"a":null}}`},
		{"optional-enum-unknown", `{"optionalEnums":{"a":"missing"}}`},
		{"optional-enum-none", `{"optionalEnums":{"a":"None"}}`},
		{"keyed-table-malformed-prefix", `{"children":{"A":{"m":{"a":"bad"}},"B":{"unknown":7}}}`},
		{"keyed-table-malformed-duplicate", `{"children":{"A":{"m":{"a":"bad"}},"A":{}}}`},
		{"keyed-table-malformed-unknown", `{"children":{"A":{"m":{"a":"bad"}},"Z":{}}}`},
		{"list-malformed-prefix", `{"rows":[{"m":{"a":"bad"}},"bad"]}`},
		{"bounded-list-malformed-prefix", `{"bounded":[{"m":{"a":"bad"}},{}]}`},
	}
	for _, row := range rows {
		inst := m.New(m.Lookup("Root"))
		report := tabletext.Report{}
		ok := m.Read(inst, []byte(row.text), &report)
		t.Logf("%s oracle: ok=%t report=%+v", row.name, ok, report)
		json, err := m.Write(inst)
		if err != nil {
			t.Fatalf("%s oracle writer: %v", row.name, err)
		}
		fmt.Fprintf(&probe, "Probe.row(%s, %t, %s, %%{unknown: %d, kind_mismatch: %d, clamped: %d, widened: %d, duplicate: %d, malformed: %t}, %s)\n", elixirString(row.text), ok, elixirString(string(json)+"\n"), report.Unknown, 0, 0, report.Widened, 0, report.Refused, elixirString(row.name))
	}
	probe.WriteString(`
# Present enum None and an absent optional are distinct cook storage states.
present_none = %{"optionalEnums" => %{"a" => "None"}}
absent = %{"optionalEnums" => %{"a" => nil}}
for order <- [:little, :big] do
  case {Duplicatescope.Table.cook_measure_reason("Root", present_none),
        Duplicatescope.Table.cook_measure_reason("Root", absent)} do
    {{present_size, :ok}, {absent_size, :ok}} when present_size >= 0 and absent_size >= 0 ->
      present_cook = Duplicatescope.Table.cook_from("Root", present_none, :binary.copy(<<0>>, present_size), order)
      absent_cook = Duplicatescope.Table.cook_from("Root", absent, :binary.copy(<<0>>, absent_size), order)
      Probe.check(match?({true, out} when is_binary(out), present_cook), "present None cooks " <> inspect(order))
      Probe.check(match?({true, out} when is_binary(out), absent_cook), "absent optional cooks " <> inspect(order))
      Probe.check(present_cook != absent_cook, "cook retains optional None presence " <> inspect(order))
    result -> Probe.check(false, "optional None cook measure " <> inspect(order) <> ": " <> inspect(result))
  end
end
`)
	probe.WriteString("if Process.get(:failures, 0) != 0, do: raise(\"duplicate/admission assertions failed\")\nIO.puts(\"ok\")\n")
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe.String())
	if err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("JSON duplicate/admission scope: %v\n%s", err, out)
	}
}
