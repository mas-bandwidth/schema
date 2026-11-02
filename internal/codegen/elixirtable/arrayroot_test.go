package elixirtable

import (
	"encoding/hex"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"os"
	"strings"
	"testing"
)

// The populated file and JSON are pinned by compiled generated Go, outside
// this emitter. The native BEAM probe also checks empty roots, clamping and
// wrong root shapes rather than crediting declarations or successful compile.
func TestArrayRootElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir array-root runtime")
	bin := elixirBinary(t)
	u := unitFrom(t, `package featurewitnesses
 table ArrayRoot | json="array" { values []int32 }
 table Bounded | json="array" { values [..2]int32 }
 table ObjectRoot { values []int32 }
 union Selection { items ArrayRoot }
 table Nested { direct ArrayRoot
 rows [..2]ArrayRoot
 mapping map[uint8]ArrayRoot
 choice Selection }

 `)
	wire, err := os.ReadFile("../../../testdata/conformance/feature-witnesses/full/ArrayRoot.bin")
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile("../../../testdata/conformance/tables/json/feature_array_root.json")
	if err != nil {
		t.Fatal(err)
	}
	probe := fmt.Sprintf(`
 defmodule Probe do
   def check(true, _), do: :ok
   def check(false, reason), do: raise(reason)
 end
 raw=File.read!("Table.ex")
 Probe.check(raw==IO.iodata_to_binary(Code.format_string!(raw))<>"\n","raw generated module format")
 alias Featurewitnesses.Table, as: T
 want=Base.decode16!(%q, case: :lower)
 text=%q
 value=%%{"values" => [3,17,-5]}
 {:ok, ^want}=T.save("ArrayRoot",value)
 {:ok, ^value, report}=T.load("ArrayRoot",want)
 Probe.check(report.clamped==0 and not report.malformed,"wire clean")
 {:ok, ^text}=T.to_json("ArrayRoot",value)
 {:ok, ^value, report}=T.from_json("ArrayRoot",text)
 Probe.check(report.kind_mismatch==0 and report.clamped==0,"JSON clean")
 {:ok, "[]\n"}=T.to_json("ArrayRoot",%%{"values"=>[]})
 {:ok, %%{"values"=>[]}, _}=T.from_json("ArrayRoot","[]")
 for wrong <- ["{}","{\"values\":[1]}","null","1","true","\"x\"","[] []"] do
  Probe.check(match?({:error,_,_},T.from_json("ArrayRoot",wrong)),"array root accepts "<>wrong)
 end
 Probe.check(match?({:error,_,_},T.from_json("ObjectRoot","[]")),"object root accepted bare array")
 {:ok, %%{"values"=>[1,2]}, report}=T.from_json("Bounded","[1,2,3]")
 Probe.check(report.clamped==1,"bounded root clamp")
 {:ok, %%{"values"=>[2147483647,-2147483648]}, report}=T.from_json("ArrayRoot","[2147483648,-2147483649]")
 Probe.check(report.clamped==2,"scalar storage clamps")
 clean="{\"direct\":[1],\"rows\":[[2],[3]],\"mapping\":{\"1\":[4]},\"choice\":{\"items\":[5]}}"
 {:ok, nested, report}=T.from_json("Nested",clean)
 Probe.check(report.kind_mismatch==0 and not report.malformed,"nested arrays clean")
 Probe.check(nested["choice"]=={"items",%%{"values"=>[5]}},"array union arm selected")
 {:ok,nested_text}=T.to_json("Nested",nested)
 Probe.check(not String.contains?(nested_text,"values"),"nested arrays have no object wrapper")
 {:ok,nested_again,report}=T.from_json("Nested",nested_text)
 Probe.check(nested_again==nested and report.kind_mismatch==0,"nested array JSON roundtrip")
 {:ok,nested_wire}=T.save("Nested",nested)
 {:ok,loaded,_}=T.load("Nested",nested_wire)
 {:ok,^nested_text}=T.to_json("Nested",loaded)
 wrong="{\"direct\":{},\"rows\":[{}],\"mapping\":{\"1\":{}},\"choice\":{\"items\":{}}}"
 {:ok, nested, report}=T.from_json("Nested",wrong)
 Probe.check(report.kind_mismatch==4 and not report.malformed,"nested shape mismatch counts")
 Probe.check(nested["choice"]==nil,"wrong-shaped array union arm stays None")
 {:error,:json_root_not_array,report}=T.from_json("ArrayRoot","{}")
 Probe.check(report.malformed and report.reason=="json_root_not_array: ArrayRoot","named root refusal")
 IO.puts("ARRAY_ROOT_OK")
 `, hex.EncodeToString(wire), string(text))
	out, err := runArmProbe(t, bin, generateElixir(t, u), probe)
	if err != nil || !strings.Contains(out, "ARRAY_ROOT_OK") {
		t.Fatalf("native array root: %v\n%s", err, out)
	}
	if strings.Contains(out, "warning:") {
		t.Fatalf("array-root emitted compiler warnings:\n%s", out)
	}
	ordinary := unitFrom(t, `package ordinary
table Root { value int32 }`)
	out, err = runArmProbe(t, bin, generateElixir(t, ordinary), `IO.puts("ORDINARY_OK")`)
	if err != nil || !strings.Contains(out, "ORDINARY_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("ordinary unit compile: %v\n%s", err, out)
	}

}
