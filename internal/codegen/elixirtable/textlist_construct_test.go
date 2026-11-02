package elixirtable

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const elixirTextListSource = `package probe
 table Lists {
 a [..3]string(5)
 b [..3]?string(5)
 c []string(5)
 d []?string(5)
 }
 table Nested { lists Lists
 ptr *Lists }
 table ArrayRoot | json="array" { values []string(5) }
 table Required { values []?string(5) | required }
 `

func TestTextListConstructElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir four text-list shapes native populated round trips")
	u := unitFrom(t, elixirTextListSource)
	m := tabletext.NewModel(u)
	var p strings.Builder
	p.WriteString(`defmodule ProbeCheck do
 def ok(true,_),do: :ok
 def ok(false,why),do: raise(why)
end
alias ProbeCheck,as: P
alias Probe.Table,as: T
store=%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
`)
	for _, row := range []struct{ root, input string }{
		{"Lists", `{}`},
		{"Lists", `{"a":["","é","abc"],"b":[null,"","abc"],"c":["é","x"],"d":["",null,"abc"]}`},
		{"Nested", `{"lists":{"a":["é"]},"ptr":{"d":[null,"abc"]}}`},
		{"ArrayRoot", `["","é","abc"]`},
		{"Required", `{"values":[]}`},
	} {
		v := m.New(u.Tables[row.root])
		var r tabletext.Report
		if !m.Read(v, []byte(row.input), &r) || !r.Silent() {
			t.Fatal(r)
		}
		wire, e := tablewire.Encode(m, v)
		if e != nil {
			t.Fatal(e)
		}
		text, e := m.Write(v)
		if e != nil {
			t.Fatal(e)
		}
		cook, e := tablecook.Cook(m, v, tablecook.Options{})
		if e != nil {
			t.Fatal(e)
		}
		region, whole, refused := tablewire.FileRegionMeasure(u, u.Tables[row.root], wire)
		if !whole || refused {
			t.Fatal("region")
		}
		fmt.Fprintf(&p, `name=%q
want=Base.decode16!(%q,case: :lower)
{:ok,v,r}=T.from_json(name,%q)
P.ok(r.kind_mismatch==0 and r.clamped==0 and not r.malformed,"text list JSON report "<>name)
{:ok,text}=T.to_json(name,v)
P.ok(text==%q,"text list canonical "<>name<>" "<>text)
{:ok,^want}=T.save(name,v)
{:ok,back,r}=T.load(name,want)
P.ok(r.kind_mismatch==0 and r.clamped==0 and not r.malformed,"text list wire report")
{:ok,^want}=T.save(name,back)
{:ok,rv,rr,ret}=T.load_retain(name,want,store,zero)
{:ok,^want,_,_}=T.save_retain(name,rv,ret,rr)
P.ok(T.load_measure(name,want)==%d,"text list extent measure "<>inspect({name,T.load_measure(name,want),%d}))
cook=Base.decode16!(%q,case: :lower)
P.ok(T.cook_measure(name,v)==byte_size(cook),"text list cook measure")
{true,^cook}=T.cook_from(name,v,:binary.copy(<<0>>,byte_size(cook)),:little)
`, row.root, hex.EncodeToString(wire), row.input, string(text)+"\n", region, region, hex.EncodeToString(cook))
	}
	p.WriteString(`for field<-["a","b","c","d"] do
 for bad<-["abcdef","x"<><<0>><>"y",<<255>>] do
  v=%{field=>[%{"value"=>bad}]}
  P.ok(T.save("Lists",v)=={:error,:invalid_value},"invalid text wire "<>field)
  P.ok(T.to_json("Lists",v)=={:error,:invalid_value},"invalid text JSON "<>field)
  P.ok(T.cook_measure("Lists",v)==-1,"invalid text cook "<>field)
 end
end
for field<-["a","b"] do
 v=%{field=>List.duplicate(%{"value"=>""},4)}
 P.ok(T.save("Lists",v)=={:error,:invalid_value},"text list count wire")
 P.ok(T.to_json("Lists",v)=={:error,:invalid_value},"text list count JSON")
end
IO.puts("TEXT_LIST_OK")
`)
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), p.String())
	if e != nil || !strings.Contains(out, "TEXT_LIST_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native text lists: %v\n%s", e, out)
	}
}

func TestTextListClampsAndDamageElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir text-list UTF8 count clamps and malformed-data refusals")
	u := unitFrom(t, elixirTextListSource)
	wide := unitFrom(t, strings.ReplaceAll(strings.ReplaceAll(elixirTextListSource, "string(5)", "string(16)"), "[..3]", "[..8]"))
	m := tabletext.NewModel(wide)
	v := m.New(wide.Tables["Lists"])
	input := `{"a":["ééé","a","z","tail","tail"],"b":[null,"ééé","abc","tail","tail"],"c":["ééé","a","z"],"d":[null,"ééé","abc","x"]}`
	var r tabletext.Report
	if !m.Read(v, []byte(input), &r) {
		t.Fatal(r)
	}
	wire, e := tablewire.Encode(m, v)
	if e != nil {
		t.Fatal(e)
	}
	p := fmt.Sprintf(`defmodule ProbeCheck do
 def ok(true,_),do: :ok
 def ok(false,why),do: raise(why)
end
alias ProbeCheck,as: P
alias Probe.Table,as: T
wire=Base.decode16!(%q,case: :lower)
for result<-[T.load("Lists",wire),T.from_json("Lists",%q)] do
 {:ok,v,r}=result
 expected=%%{"a"=>["éé","a","z"],"b"=>[nil,"éé","abc"],"c"=>["éé","a","z"],"d"=>[nil,"éé","abc","x"]}
 actual=Map.new(expected,fn {key,_}->{key,Enum.map(v[key],& &1["value"])} end)
 P.ok(actual==expected and r.clamped==6 and not r.malformed,"UTF8/count clamps "<>inspect({actual,r}))
 {:ok,_}=T.save("Lists",v)
end
for field<-["a","b","c","d"] do
 {:error,:malformed,r}=T.from_json("Lists","{\""<>field<>"\":[\"x\\u0000y\"]}")
 P.ok(r.malformed,"JSON zero refusal")
 {:ok,wire}=T.save("Lists",%%{field=>[%%{"value"=>"xyz"}]})
 {at,3}=:binary.match(wire,"xyz")
 <<head::binary-size(^at),_old,tail::binary>>=wire
 for byte<-[0,255] do
  {:error,:malformed,r}=T.load("Lists",<<head::binary,byte,tail::binary>>)
  P.ok(r.malformed,"wire damaged text")
 end
end
for path<-Path.wildcard("*.ex") do
 raw=File.read!(path)
 formatted=IO.iodata_to_binary(Code.format_string!(raw))<>"\n"
 if raw != formatted, do: IO.inspect(Enum.find(Enum.zip(String.split(raw,"\n"),String.split(formatted,"\n")),fn {a,b}->a != b end),label: path)
 P.ok(raw==formatted,"raw format "<>path)
end
IO.puts("TEXT_LIST_DAMAGE_OK")
`, hex.EncodeToString(wire), input)
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), p)
	if e != nil || !strings.Contains(out, "TEXT_LIST_DAMAGE_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native text-list damage: %v\n%s", e, out)
	}
}

func TestListWidenedRegionMeasureElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir primitive widened list region arithmetic")
	u := unitFrom(t, `package probe
 table R { values []int32 }`)
	old := unitFrom(t, `package probe
 table R { values []int16 }`)
	m := tabletext.NewModel(old)
	v := m.New(old.Tables["R"])
	var r tabletext.Report
	if !m.Read(v, []byte(`{"values":[2,3]}`), &r) {
		t.Fatal(r)
	}
	wire, e := tablewire.Encode(m, v)
	if e != nil {
		t.Fatal(e)
	}
	region, whole, refused := tablewire.FileRegionMeasure(u, u.Tables["R"], wire)
	if !whole || refused || region != 40 {
		t.Fatalf("root16 + elements8 + directory16: %d %v %v", region, whole, refused)
	}
	p := fmt.Sprintf(`alias Probe.Table,as: T
wire=Base.decode16!(%q,case: :lower)
if T.load_measure("R",wire)!=40,do: raise("widened list region measure")
{:ok,%%{"values"=>[2,3]},r}=T.load("R",wire)
if r.widened != 1,do: raise("widened list values")
IO.puts("WIDENED_REGION_OK")
`, hex.EncodeToString(wire))
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), p)
	if e != nil || !strings.Contains(out, "WIDENED_REGION_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native widened region: %v\n%s", e, out)
	}
}
