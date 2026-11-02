package elixirtable

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestTimestampConstructElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir timestamp RFC3339, wire, message, fixed and cook")
	base := "../../../testdata/conformance/feature-witnesses/timestamp/rust-boundaries/"
	source, err := os.ReadFile(base + "Probe.schema")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(base + "cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Input, Value, Canonical string
		Accepted                      bool
		Mismatch                      int
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 60 {
		t.Fatalf("got %d timestamp cases", len(cases))
	}
	u := unitFrom(t, string(source))
	m := tabletext.NewModel(u)
	var probe strings.Builder
	probe.WriteString(`defmodule Probe do
 def check(true,_),do: :ok
 def check(false,why),do: raise(why)
end
alias Probe.Table,as: T
`)
	for _, c := range cases {
		fmt.Fprintf(&probe, "name=%q\nresult=T.from_json(\"Clock\",%q)\n", c.Name, c.Input)
		if !c.Accepted {
			probe.WriteString(`{:error,_,r}=result
Probe.check(r.malformed and r.reason=="timestamp_not_rfc3339: at",name<>" timestamp refusal")
`)
			continue
		}
		value, err := strconv.ParseInt(c.Value, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		inst := m.New(u.Tables["Clock"])
		inst.Fields[0].Cell.I = value
		inst.Fields[0].Cell.U = uint64(value)
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatal(err)
		}
		le, err := tablecook.Cook(m, inst, tablecook.Options{})
		if err != nil {
			t.Fatal(err)
		}
		be, err := tablecook.Cook(m, inst, tablecook.Options{Big: true})
		if err != nil {
			t.Fatal(err)
		}
		text := fmt.Sprintf("{\n  \"at\": %q\n}\n", c.Canonical)
		fmt.Fprintf(&probe, `{:ok,value,r}=result
Probe.check(value["at"]==%s and r.kind_mismatch==%d and not r.malformed,name<>" timestamp value")
wire=Base.decode16!(%q,case: :lower)
{:ok,^wire}=T.save("Clock",value)
{:ok,loaded,r}=T.load("Clock",wire)
Probe.check(loaded==value and not r.malformed and r.kind_mismatch==0,name<>" timestamp wire")
{:ok,text}=T.to_json("Clock",value)
Probe.check(text==%q,name<>" canonical timestamp: "<>text)
{size,:ok}=T.cook_measure_reason("Clock",value)
for {order,cook}<-[{:little,Base.decode16!(%q,case: :lower)},{:big,Base.decode16!(%q,case: :lower)}] do
 {true,^cook}=T.cook_from("Clock",value,:binary.copy(<<0xA5>>,size),order)
end
`, c.Value, c.Mismatch, hex.EncodeToString(wire), text, hex.EncodeToString(le), hex.EncodeToString(be))
	}
	probe.WriteString(`alias Probe.ProbeFixed,as: F
alias Probe.TableMessage,as: M
{:ok,vocabulary}=M.announce_read(M.announce())
`)
	for _, n := range []int64{0, 1, -1, -9223372036854775808, 9223372036854775807} {
		inst := m.New(u.Tables["Tick"])
		inst.Fields[0].Cell.I = n
		inst.Fields[0].Cell.U = uint64(n)
		wire, err := tablewire.EncodeMessage(m, inst)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&probe, `value=%%Probe.Tick{at: %d}
file=F.tick_fixed_save([value])
{:ok,[^value],r}=F.tick_fixed_load(file)
Probe.check(r.clamped==0 and not r.malformed,"fixed timestamp")
message=Base.decode16!(%q,case: :lower)
{:ok,values}=M.load_messages(vocabulary,message)
`, n, hex.EncodeToString(wire))
		if n == 0 {
			probe.WriteString("Probe.check(values==[[]],\"zero message elision\")\n")
		} else {
			fmt.Fprintf(&probe, "[[{_,n}]]=values\nProbe.check(n==%d,\"signed timestamp message\")\n", n)
		}
		probe.WriteString("{:ok,^message}=M.save_messages(vocabulary,values)\n")
	}
	probe.WriteString(`for path<-Path.wildcard("*.ex") do
 raw=File.read!(path)
 formatted=IO.iodata_to_binary(Code.format_string!(raw))<>"\n"
 if raw != formatted do
  IO.inspect(Enum.find(Enum.zip(String.split(raw,"\n"),String.split(formatted,"\n")),fn {a,b}->a != b end),label: path)
 end
 Probe.check(raw==formatted,"raw format: "<>path)
end
IO.puts("TIMESTAMP_OK")
`)
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe.String())
	if err != nil || !strings.Contains(out, "TIMESTAMP_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native timestamp: %v\n%s", err, out)
	}
}

func TestTimestampClosureElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir timestamp collection and kind identity")
	u := unitFrom(t, `package probe
 enum Key { A, B }
 union When { value Clock }
 table Clock { at timestamp }
 table Integer { at int64 }
 table Empty { other int32 }
 table Root {
  points []Clock
  samples [..3]Clock
  stable [2]Clock
  keyed [Key]Clock
  maybe ?Clock
  by_time map[uint8]Clock
  child Clock
  choice When
 }
 `)
	m := tabletext.NewModel(u)
	input := `{"points":[{"at":"1969-12-31T23:59:59.999999999Z"},{"at":"2262-04-11T23:47:16.854775807Z"}],"samples":[{"at":"1970-01-01T00:00:00.000000001Z"}],"stable":[{"at":"1677-09-21T00:12:43.145224192Z"},{"at":"1970-01-01T00:00:00Z"}],"keyed":{"A":{"at":"1969-12-31T23:59:59.999999999Z"}},"maybe":{"at":"1970-01-01T00:00:00.000000001Z"},"by_time":{"7":{"at":"1969-12-31T23:59:59.999999999Z"}},"child":{"at":"1970-01-01T00:00:00.000000001Z"},"choice":{"value":{"at":"1969-12-31T23:59:59.999999999Z"}}}`
	value := m.New(u.Tables["Root"])
	var r tabletext.Report
	if !m.Read(value, []byte(input), &r) || !r.Silent() {
		t.Fatal(r)
	}
	wire, err := tablewire.Encode(m, value)
	if err != nil {
		t.Fatal(err)
	}
	text, err := m.Write(value)
	if err != nil {
		t.Fatal(err)
	}
	le, err := tablecook.Cook(m, value, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	be, err := tablecook.Cook(m, value, tablecook.Options{Big: true})
	if err != nil {
		t.Fatal(err)
	}
	clock := m.New(u.Tables["Clock"])
	clock.Fields[0].Cell.I = 1
	clock.Fields[0].Cell.U = 1
	clockWire, err := tablewire.Encode(m, clock)
	if err != nil {
		t.Fatal(err)
	}
	probe := fmt.Sprintf(`
 defmodule Probe do
  def check(true,_),do: :ok
  def check(false,why),do: raise(why)
 end
 alias Probe.Table,as: T
 wire=Base.decode16!(%q,case: :lower)
 {:ok,value,r}=T.from_json("Root",%q)
 Probe.check(not r.malformed and r.kind_mismatch==0,"timestamp closure JSON")
 {:ok,^wire}=T.save("Root",value)
 {:ok,loaded,r}=T.load("Root",wire)
 Probe.check(not r.malformed and r.kind_mismatch==0,"timestamp closure wire")
 {:ok,text}=T.to_json("Root",loaded)
 Probe.check(text==%q,"timestamp closure canonical")
 store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
 zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
 {:ok,held,r,store}=T.load_retain("Root",wire,store,zero)
 {:ok,^wire,_,_}=T.save_retain("Root",held,store,r)
 {size,:ok}=T.cook_measure_reason("Root",value)
 for {order,cook}<-[{:little,Base.decode16!(%q,case: :lower)},{:big,Base.decode16!(%q,case: :lower)}] do
  {true,^cook}=T.cook_from("Root",value,:binary.copy(<<0xA5>>,size),order)
 end
 clock=Base.decode16!(%q,case: :lower)
 <<1,1,36,tail::binary>>=clock
 for kind<-[5,9] do
  {:ok,empty,r}=T.load("Clock",<<1,1,kind,tail::binary>>)
  Probe.check(empty["at"]==0 and r.kind_mismatch==1 and r.widened==0 and not r.malformed,"integer cannot widen into timestamp")
 end
 {:ok,integer,r}=T.load("Integer",clock)
 Probe.check(integer["at"]==0 and r.kind_mismatch==1 and r.widened==0 and not r.malformed,"timestamp cannot widen into integer")
 {:ok,_,r}=T.load("Empty",clock)
 Probe.check(r.unknown==1 and not r.malformed,"unknown timestamp skip")
 for input <- [
  ~s({"child":{"at":"bad"}}),
  ~s({"points":[{"at":"bad"}]}),
  ~s({"samples":[{"at":"bad"}]}),
  ~s({"stable":[{"at":"bad"},{}]}),
  ~s({"keyed":{"A":{"at":"bad"}}}),
  ~s({"maybe":{"at":"bad"}}),
  ~s({"by_time":{"7":{"at":"bad"}}}),
  ~s({"choice":{"value":{"at":"bad"}}})
 ] do
  {:error,_,r}=T.from_json("Root",input)
  Probe.check(r.malformed and r.reason=="timestamp_not_rfc3339: at","nested named timestamp refusal: "<>inspect(r))
 end
 IO.puts("TIMESTAMP_CLOSURE_OK")
 `, hex.EncodeToString(wire), input, string(text)+"\n", hex.EncodeToString(le), hex.EncodeToString(be), hex.EncodeToString(clockWire))
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe)
	if err != nil || !strings.Contains(out, "TIMESTAMP_CLOSURE_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("timestamp closure: %v\n%s", err, out)
	}
}
