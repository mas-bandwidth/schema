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

func TestUnboundedTextConstructElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir unbounded text wire JSON retaining inline blobs and text map keys")
	source := strings.ReplaceAll(elixirTextListSource, "string(5)", "string") + `
 table Keys { names map[string]int32 }
 table Tiny { values [..2]string }
 `
	u := unitFrom(t, source)
	m := tabletext.NewModel(u)
	long := strings.Repeat("é-", 100)
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
		{"Tiny", `{"values":["x"]}`},
		{"Lists", `{}`},
		{"Lists", fmt.Sprintf(`{"a":["","é",%q],"b":[null,"",%q],"c":[%q,"x"],"d":["",null,%q]}`, long, long, long, long)},
		{"Nested", fmt.Sprintf(`{"lists":{"a":[%q]},"ptr":{"d":[null,%q]}}`, long, long)},
		{"ArrayRoot", fmt.Sprintf(`["","é",%q]`, long)},
		{"Required", `{"values":[]}`},
		{"Keys", fmt.Sprintf(`{"names":{"":1,%q:2,%q:3}}`, long+"a", long+"z")},
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
		canonical, e := m.Write(v)
		if e != nil {
			t.Fatal(e)
		}
		region, whole, refused := tablewire.FileRegionMeasure(u, u.Tables[row.root], wire)
		if !whole || refused {
			t.Fatal("region")
		}
		if row.root == "Tiny" && region != 56 {
			t.Fatalf("independent Tiny arithmetic:24 record+16 blob+16 directory=%d", region)
		}
		fmt.Fprintf(&p, `name=%q
wire=Base.decode16!(%q,case: :lower)
{:ok,v,r}=T.from_json(name,%q)
P.ok(r.clamped==0 and r.kind_mismatch==0 and not r.malformed,"unbounded JSON report")
{:ok,text}=T.to_json(name,v)
P.ok(text==%q,"unbounded canonical "<>name<>" "<>text)
{:ok,^wire}=T.save(name,v)
{:ok,back,r}=T.load(name,wire)
P.ok(r.clamped==0 and r.kind_mismatch==0 and not r.malformed,"unbounded wire report")
{:ok,^wire}=T.save(name,back)
{:ok,rv,rr,ret}=T.load_retain(name,wire,store,zero)
{:ok,^wire,_,_}=T.save_retain(name,rv,ret,rr)
`, row.root, hex.EncodeToString(wire), row.input, string(canonical)+"\n")
		// Map extent closure is qualified independently; its shared worker owns
		// the dispatch, while this test pins the unbounded key's cooked storage.
		if row.root != "Keys" {
			fmt.Fprintf(&p, `P.ok(T.load_measure(name,wire)==%d,"unbounded extent "<>inspect({name,T.load_measure(name,wire),%d}))
`, region, region)
		}
		for _, big := range []bool{false, true} {
			cooked, e := tablecook.Cook(m, v, tablecook.Options{Big: big})
			if e != nil {
				t.Fatal(e)
			}
			order := "little"
			if big {
				order = "big"
			}
			fmt.Fprintf(&p, `cook=Base.decode16!(%q,case: :lower)
P.ok(T.cook_measure(name,v)==byte_size(cook),"unbounded cook measure "<>name)
{true,actual}=T.cook_from(name,v,:binary.copy(<<0>>,byte_size(cook)),:%s)
P.ok(actual==cook,"unbounded cook bytes "<>name)
`, hex.EncodeToString(cooked), order)
			if !big && row.root == "Keys" {
				fmt.Fprintf(&p, `{:ok,%%{region: region}}=Probe.ProbeCook.cook_open_keys(cook)
for {key,want}<-[{"",1},{%q,2},{%q,3}] do
 {:ok,at}=Probe.ProbeCook.keys_node_names_find(region,0,key)
 P.ok(Probe.ProbeCook.keys_names_entry_node_key(region,at)==key,"unbounded map key accessor")
 P.ok(Probe.ProbeCook.keys_names_entry_node_value(region,at)==want,"unbounded map find")
end
P.ok(Probe.ProbeCook.keys_node_names_find(region,0,%q)==:error,"absent prefix key")
`, long+"a", long+"z", long)
			}
		}
	}
	p.WriteString(`for field<-["a","b","c","d"] do
 for bad<-["x"<><<0>><>"y",<<255>>] do
  value=%{field=>[%{"value"=>bad}]}
  P.ok(T.save("Lists",value)=={:error,:invalid_value},"bad unbounded wire")
  P.ok(T.to_json("Lists",value)=={:error,:invalid_value},"bad unbounded JSON")
  P.ok(T.cook_measure("Lists",value)==-1,"bad unbounded cook")
 end
end
for bad<-["x"<><<0>><>"y",<<255>>] do
 value=%{"names"=>%{bad=>1}}
 P.ok(T.save("Keys",value)=={:error,:invalid_value},"bad unbounded key wire")
 P.ok(T.to_json("Keys",value)=={:error,:invalid_value},"bad unbounded key JSON")
 P.ok(T.cook_measure("Keys",value)==-1,"bad unbounded key cook")
end
{:ok,v,r}=T.from_json("ArrayRoot","[\"\\ud800\"]")
P.ok(Enum.map(v["values"],& &1["value"])==["�"] and not r.malformed,"active surrogate replacement contract")
for field<-["a","b","c","d"] do
 {:ok,wire}=T.save("Lists",%{field=>[%{"value"=>"xyz"}]})
 {at,3}=:binary.match(wire,"xyz")
 <<head::binary-size(^at),_,tail::binary>>=wire
 {:error,:malformed,r}=T.load("Lists",<<head::binary,255,tail::binary>>)
 P.ok(r.malformed,"unbounded malformed UTF8 read")
end
IO.puts("UNBOUNDED_TEXT_OK")
`)
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), p.String())
	if e != nil || !strings.Contains(out, "UNBOUNDED_TEXT_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native unbounded text: %v\n%s", e, out)
	}
}

func TestRetainedTextListPathsElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir retained text-list indices and discarded-element records")
	u := unitFrom(t, `package probe
 table Elem { value string(16)
 extra int32 }
 table Writer { values []Elem }
 table Plain { values []?string(16) }
 table Limited { values []?string(16) | total=5 }
 `)
	probe := `defmodule ProbeCheck do
 def ok(true,_),do: :ok
 def ok(false,why),do: raise(why)
end
alias ProbeCheck,as: P
alias Probe.Table,as: T
store=%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
{:ok,source,_}=T.from_json("Writer",~s({"values":[{"value":"abcde","extra":1},{"value":"xyz","extra":2},{"value":"tail","extra":3}]}))
{:ok,wire}=T.save("Writer",source)
{:ok,v,r,ret}=T.load_retain("Plain",wire,store,zero)
P.ok(r.unknown==3 and r.retained==3 and length(ret.records)==3,"retained all indices")
{:ok,saved,_,_}=T.save_retain("Plain",v,ret,r)
{:ok,back,_}=T.load("Writer",saved)
P.ok(Enum.map(back["values"],& &1["extra"])==[1,2,3],"retained records belong to distinct indices")
{:ok,v,r,ret}=T.load_retain("Limited",wire,store,zero)
P.ok(r.unknown==2 and r.retained==2 and r.clamped==1 and length(ret.records)==1,"discarded overflow element retains no records "<>inspect({r,ret}))
{:ok,saved,_,_}=T.save_retain("Limited",v,ret,r)
{:ok,back,_}=T.load("Writer",saved)
P.ok(Enum.map(back["values"],& &1["extra"])==[1],"only prefix retained data survives")
IO.puts("RETAINED_TEXT_PATHS_OK")
`
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe)
	if e != nil || !strings.Contains(out, "RETAINED_TEXT_PATHS_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("retained text paths: %v\n%s", e, out)
	}
}

func TestUnboundedTextSmallUnitsElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir unbounded text minimal generated units compile without warnings")
	for _, field := range []string{"a []string", "a [..2]?string", "a map[string]int32"} {
		t.Run(field, func(t *testing.T) {
			u := unitFrom(t, "package probe\ntable R { "+field+" }\n")
			out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), `alias Probe.Table,as: T
{:ok,v,_}=T.from_json("R","{}")
{:ok,wire}=T.save("R",v)
{:ok,_,_}=T.load("R",wire)
true=T.cook_measure("R",v)>0
IO.puts("SMALL_UNBOUNDED_OK")
`)
			if e != nil || !strings.Contains(out, "SMALL_UNBOUNDED_OK") || strings.Contains(out, "warning:") {
				t.Fatalf("small unbounded unit: %v\n%s", e, out)
			}
		})
	}
}
