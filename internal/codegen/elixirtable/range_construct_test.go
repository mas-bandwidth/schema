package elixirtable

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The shared corpus states exact integer endpoints beyond float64 precision.
// Input wire is generated with no declared range, so the reader must clamp it.
func TestOneSidedRangeConstructElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir one-sided range native boundaries")
	base := "../../../testdata/conformance/feature-witnesses/one-sided-range/"
	schema, err := os.ReadFile(base + "Probe.schema")
	if err != nil {
		t.Fatal(err)
	}
	cases, err := os.ReadFile(base + "cases.tsv")
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(schema))
	rawSchema := string(schema)
	for _, bound := range []string{" | max = 18446744073709551610", " | min = 9007199254740993", " | max = -9007199254740993", " | min = -9223372036854775807"} {
		rawSchema = strings.ReplaceAll(rawSchema, bound, "")
	}
	rawU := unitFrom(t, rawSchema)
	m, rawM := tabletext.NewModel(u), tabletext.NewModel(rawU)
	var probe strings.Builder
	probe.WriteString(`defmodule Probe do
 def check(true,_), do: :ok
 def check(false,why), do: raise(why)
end
alias Probe.Table, as: T
alias Probe.ProbeFixed, as: F
alias Probe.TableMessage, as: M
{:ok,vocabulary}=M.announce_read(M.announce())
`)
	count := 0
	for line := range strings.SplitSeq(string(cases), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Fields(line)
		if len(cols) != 4 {
			t.Fatal(line)
		}
		field, input, expected := cols[0], cols[1], cols[2]
		clamps, err := strconv.Atoi(cols[3])
		if err != nil {
			t.Fatal(err)
		}
		value := m.New(u.Tables["Ranges"])
		raw := rawM.New(rawU.Tables["Ranges"])
		text := fmt.Sprintf(`{"%s":%s}`, field, input)
		var r tabletext.Report
		if !m.Read(value, fmt.Appendf(nil, `{"%s":%s}`, field, expected), &r) || !r.Silent() {
			t.Fatal(r)
		}
		if !rawM.Read(raw, []byte(text), &r) || !r.Silent() {
			t.Fatal(r)
		}
		wire, err := tablewire.Encode(rawM, raw)
		if err != nil {
			t.Fatal(err)
		}
		want, err := tablewire.Encode(m, value)
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
		index := -1
		for i, f := range u.Tables["Ranges"].Fields {
			if f.Name == field {
				index = i
			}
		}
		if index < 0 {
			t.Fatal(field)
		}
		fmt.Fprintf(&probe, `
{field,input,want,wire,le,be}={%q,%q,Base.decode16!(%q,case: :lower),Base.decode16!(%q,case: :lower),Base.decode16!(%q,case: :lower),Base.decode16!(%q,case: :lower)}
{:ok,value,r}=T.load("Ranges",wire)
Probe.check(value[field]==%s and r.clamped==%d and not r.malformed,field<>" wire clamp")
{:ok,^want}=T.save("Ranges",value)
{:ok,jvalue,r}=T.from_json("Ranges",input)
Probe.check(jvalue[field]==%s and r.clamped==%d and not r.malformed,field<>" JSON clamp")
{:ok,^want}=T.save("Ranges",jvalue)
{:ok,json}=T.to_json("Ranges",value)
{:ok,^value,r}=T.from_json("Ranges",json)
Probe.check(r.clamped==0,field<>" canonical JSON")
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
{:ok,held,r,store}=T.load_retain("Ranges",wire,store,zero)
Probe.check(held[field]==%s and r.clamped==%d,field<>" retained clamp")
{:ok,^want,_,_}=T.save_retain("Ranges",held,store,r)
{size,:ok}=T.cook_measure_reason("Ranges",value)
for {order,cook}<-[{:little,le},{:big,be}] do
 {true,^cook}=T.cook_from("Ranges",value,:binary.copy(<<0xA5>>,size),order)
end
fixed=struct(Probe.Ranges,Enum.map(value,fn {k,v}->{String.to_existing_atom(k),v} end))
file=F.ranges_fixed_save([fixed])
{:ok,[^fixed],r}=F.ranges_fixed_load(file)
Probe.check(r.clamped==0,field<>" fixed clean")
at=byte_size(file)-32+%d*8
head=binary_part(file,0,at)
tail=binary_part(file,at+8,byte_size(file)-at-8)
forged=<<head::binary,%s::little-signed-64,tail::binary>>
{:ok,[loaded],r}=F.ranges_fixed_load(forged)
Probe.check(Map.fetch!(loaded,String.to_existing_atom(field))==%s and r.clamped==%d,field<>" fixed forged clamp")
`, field, text, hex.EncodeToString(want), hex.EncodeToString(wire), hex.EncodeToString(le), hex.EncodeToString(be), expected, clamps, expected, clamps, expected, clamps, index, input, expected, clamps)
		if clamps > 0 {
			fmt.Fprintf(&probe, `
refused=try do
 F.ranges_fixed_save([Map.put(fixed,String.to_existing_atom(field),%s)])
 false
rescue
 ArgumentError -> true
end
Probe.check(refused,field<>" fixed invalid writer refusal")
`, input)
		}
		message, err := tablewire.EncodeMessage(m, value)
		if err != nil {
			t.Fatal(err)
		}
		body := expectedStructBody(t, u, ir.TableVocabularySlots(u), u.Tables["Ranges"], map[string]any{field: json.Number(expected)})
		fmt.Fprintf(&probe, `
message=Base.decode16!(%q,case: :lower)
{:ok,values}=M.load_messages(vocabulary,message)
Probe.check(values==[%s],field<>" message exact value")
{:ok,^message}=M.save_messages(vocabulary,values)
{:ok,message_size}=M.measure_messages(vocabulary,values)
Probe.check(message_size==byte_size(message),field<>" message measure")
`, hex.EncodeToString(message), body)
		count++
	}
	if count != 12 {
		t.Fatalf("got %d boundary cases", count)
	}
	probe.WriteString("IO.puts(\"RANGE_OK\")\n")
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe.String())
	if err != nil || !strings.Contains(out, "RANGE_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native ranges: %v\n%s", err, out)
	}
}

func TestOneSidedRangeClosureElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir one-sided ranges in optional and array slots")
	const schema = `package probe
 enum Key { A, B }
 fixed table Small { value int32 | min = -2 }
 fixed table Closure {
  items [2]int16 | min = -10
  counted [..3]uint16 | max = 9
  keyed [Key]int32 | max = 5
  maybe ?int64 | min = -7
  nested Small
 }
 `
	u := unitFrom(t, schema)
	rawSchema := schema
	for _, s := range []string{" | min = -2", " | min = -10", " | max = 9", " | max = 5", " | min = -7"} {
		rawSchema = strings.ReplaceAll(rawSchema, s, "")
	}
	rawU := unitFrom(t, rawSchema)
	m, rawM := tabletext.NewModel(u), tabletext.NewModel(rawU)
	input := `{"items":[-11,32767],"counted":[10,8],"keyed":{"A":6,"B":-20},"maybe":-8,"nested":{"value":-3}}`
	expected := `{"items":[-10,32767],"counted":[9,8],"keyed":{"A":5,"B":-20},"maybe":-7,"nested":{"value":-2}}`
	raw, value := rawM.New(rawU.Tables["Closure"]), m.New(u.Tables["Closure"])
	var r tabletext.Report
	if !rawM.Read(raw, []byte(input), &r) || !r.Silent() {
		t.Fatal(r)
	}
	if !m.Read(value, []byte(expected), &r) || !r.Silent() {
		t.Fatal(r)
	}
	wire, err := tablewire.Encode(rawM, raw)
	if err != nil {
		t.Fatal(err)
	}
	want, err := tablewire.Encode(m, value)
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
 want=Base.decode16!(%q,case: :lower)
 {:ok,value,r}=T.load("Closure",wire)
 Probe.check(r.clamped==5 and not r.malformed,"array/optional/nested wire clamps: "<>inspect(r))
 {:ok,^want}=T.save("Closure",value)
 {:ok,jvalue,r}=T.from_json("Closure",%q)
 Probe.check(r.clamped==5 and not r.malformed,"array/optional/nested JSON clamps: "<>inspect(r))
 {:ok,^want}=T.save("Closure",jvalue)
 {:ok,absent,r}=T.from_json("Closure","{}")
 Probe.check(absent["maybe"]==nil and r.clamped==0,"absent optional has no clamp")
 store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
 zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
 {:ok,held,r,store}=T.load_retain("Closure",wire,store,zero)
 Probe.check(r.clamped==5 and not r.malformed,"retained closure clamps")
 {:ok,^want,_,_}=T.save_retain("Closure",held,store,r)

 IO.puts("RANGE_CLOSURE_OK")
 `, hex.EncodeToString(wire), hex.EncodeToString(want), input)
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe)
	if err != nil || !strings.Contains(out, "RANGE_CLOSURE_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native range closure: %v\n%s", err, out)
	}
}
