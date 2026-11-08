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

func TestTextTotalConstructElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir text-list total prefix and streaming discarded tail")
	source := elixirTextListSource + `
 table Totals {
 a [..3]string(5) | total=5
 b [..3]?string(5) | total=5
 c []string(5) | total=5
 d []?string(5) | total=5
 }
 `
	u := unitFrom(t, source)
	wide := unitFrom(t, strings.ReplaceAll(strings.ReplaceAll(elixirTextListSource, "string(5)", "string(16)"), "[..3]", "[..8]"))
	m := tabletext.NewModel(wide)
	v := m.New(wide.Tables["Lists"])
	input := `{"a":["ééé","a","z","tail"],"b":[null,"ééé","abc","tail"],"c":["ééé","a","z"],"d":[null,"ééé","abc","x"]}`
	var r tabletext.Report
	if !m.Read(v, []byte(input), &r) {
		t.Fatal(r)
	}
	wire, e := tablewire.Encode(m, v)
	if e != nil {
		t.Fatal(e)
	}
	var p strings.Builder
	fmt.Fprintf(&p, `defmodule ProbeCheck do
 def ok(true,_),do: :ok
 def ok(false,why),do: raise(why)
end
alias ProbeCheck,as: P
alias Probe.Table,as: T
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
for field<-["a","b","c","d"] do
 bad=%%{field=>[%%{"value"=>"abc"},%%{"value"=>"abc"}]}
 P.ok(T.save("Totals",bad)=={:error,:invalid_value},"total wire refusal "<>field)
 P.ok(T.to_json("Totals",bad)=={:error,:invalid_value},"total JSON refusal "<>field)
 P.ok(T.cook_measure("Totals",bad)==-1,"total cook refusal "<>field)
end
wire=Base.decode16!(%q,case: :lower)
for {result,count}<-[{T.load("Totals",wire),10},{T.from_json("Totals",%q),10}] do
 {:ok,v,r}=result
 expected=%%{"a"=>["éé","a"],"b"=>[nil,"éé"],"c"=>["éé","a"],"d"=>[nil,"éé"]}
 actual=Map.new(expected,fn {key,_}->{key,Enum.map(v[key],& &1["value"])} end)
 P.ok(actual==expected and r.clamped==count and true,"total clamp order "<>inspect({actual,r}))
 {:ok,saved}=T.save("Totals",v)
 {:ok,back,_}=T.load("Totals",saved)
 {:ok,^saved}=T.save("Totals",back)
end
{:ok,v,r,ret}=T.load_retain("Totals",wire,store,zero)
P.ok(r.clamped==10 and true,"total retaining clamps")
{:ok,saved,_,_}=T.save_retain("Totals",v,ret,r)
{:ok,plain,_}=T.load("Totals",wire)
{:ok,^saved}=T.save("Totals",plain)
`, hex.EncodeToString(wire), input)
	for _, field := range []string{"a", "b", "c", "d"} {
		v := m.New(wide.Tables["Lists"])
		var r tabletext.Report
		input := fmt.Sprintf(`{%q:["abcde","xyz","toolong"]}`, field)
		if !m.Read(v, []byte(input), &r) {
			t.Fatal(r)
		}
		wire, e := tablewire.Encode(m, v)
		if e != nil {
			t.Fatal(e)
		}
		at := strings.Index(string(wire), "toolong")
		if at < 0 {
			t.Fatal("tail marker")
		}
		wire[at] = 255
		fmt.Fprintf(&p, `{:ok,v,r}=T.from_json("Totals",%q)
P.ok(Enum.map(v[%q],& &1["value"])==["abcde"] and r.clamped==1,"ignored overbound tail")
wire=Base.decode16!(%q,case: :lower)
{:ok,v,r}=T.load("Totals",wire)
P.ok(Enum.map(v[%q],& &1["value"])==["abcde"] and r.clamped==1 and true,"ignored malformed discarded tail")
{:ok,v,r,_}=T.load_retain("Totals",wire,store,zero)
P.ok(Enum.map(v[%q],& &1["value"])==["abcde"] and r.clamped==1 and true,"retaining ignored tail")
`, input, field, hex.EncodeToString(wire), field, field)
		fmt.Fprintf(&p, `{:ok,v,r}=T.from_json("Totals",%q)
P.ok(Enum.map(v[%q],& &1["value"])==["abcde"] and r.clamped==1 and true,"ignored JSON NUL tail")
`, strings.Replace(input, "toolong", `\u0000`, 1), field)
		if field == "b" || field == "d" {
			wire[at] = 't'
			wire[at+len("toolong")] = 255
			fmt.Fprintf(&p, `wire=Base.decode16!(%q,case: :lower)
{:ok,v,r}=T.load("Totals",wire)
P.ok(Enum.map(v[%q],& &1["value"])==["abcde"] and r.clamped==1 and true,"ignored damaged optional body")
{:ok,v,r,_}=T.load_retain("Totals",wire,store,zero)
P.ok(Enum.map(v[%q],& &1["value"])==["abcde"] and r.clamped==1 and true,"retaining ignored damaged optional body")
`, hex.EncodeToString(wire), field, field)
		}
	}
	p.WriteString("IO.puts(\"TEXT_TOTAL_OK\")\n")
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), p.String())
	if e != nil || !strings.Contains(out, "TEXT_TOTAL_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native text-list totals: %v\n%s", e, out)
	}
}
