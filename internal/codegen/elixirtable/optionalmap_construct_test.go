package elixirtable

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"os"
	"strings"
	"testing"
)

func TestOptionalMapConstructElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir optional-map native wire, retain, JSON and cook")
	bin := elixirBinary(t)
	base := "../../../testdata/conformance/feature-witnesses/optional-map/"
	for _, unit := range []struct {
		schema, root string
		cases        []string
	}{
		{"Probe.schema", "M", []string{"absent", "empty", "populated"}},
		{"Closure.schema", "Maps", []string{"closure-absent", "closure-empty", "closure-populated"}},
	} {
		t.Run(unit.root, func(t *testing.T) {
			source, err := os.ReadFile(base + unit.schema)
			if err != nil {
				t.Fatal(err)
			}
			u := unitFrom(t, string(source))
			m := tabletext.NewModel(u)
			var probe strings.Builder
			probe.WriteString(`defmodule Probe do
 def check(true,_), do: :ok
 def check(false,why), do: raise(why)
end
alias Probe.Table, as: T
for path <- Path.wildcard("*.ex") do
 raw=File.read!(path)
 Probe.check(raw==IO.iodata_to_binary(Code.format_string!(raw))<>"\n","raw generated module format: "<>path)
end
`)
			for _, name := range unit.cases {
				input, err := os.ReadFile(base + name + ".json")
				if err != nil {
					t.Fatal(err)
				}
				v := m.New(u.Tables[unit.root])
				var report tabletext.Report
				if !m.Read(v, input, &report) || !report.Silent() {
					t.Fatalf("%s input: %+v", name, report)
				}
				wire, err := tablewire.Encode(m, v)
				if err != nil {
					t.Fatal(err)
				}
				if unit.root == "M" {
					pin, err := os.ReadFile(base + name + ".bin")
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(wire, pin) {
						t.Fatal("independent Go/C++ presence pin differs")
					}
				}
				text, err := m.Write(v)
				if err != nil {
					t.Fatal(err)
				}
				le, err := tablecook.Cook(m, v, tablecook.Options{})
				if err != nil {
					t.Fatal(err)
				}
				be, err := tablecook.Cook(m, v, tablecook.Options{Big: true})
				if err != nil {
					t.Fatal(err)
				}
				fmt.Fprintf(&probe, `
{name,root,input,text,wire,le,be}={%q,%q,%q,%q,Base.decode16!(%q,case: :lower),Base.decode16!(%q,case: :lower),Base.decode16!(%q,case: :lower)}
{:ok,value,r}=T.from_json(root,input)
Probe.check(not r.malformed and r.kind_mismatch==0,name<>" JSON report")
{:ok,^text}=T.to_json(root,value)
{:ok,^wire}=T.save(root,value)
{:ok,loaded,_}=T.load(root,wire)
{:ok,^text}=T.to_json(root,loaded)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
{:ok,held,r,store}=T.load_retain(root,wire,store,zero)
{:ok,^wire,_,_}=T.save_retain(root,held,store,r)
{size,:ok}=T.cook_measure_reason(root,value)
Probe.check(size==byte_size(le),name<>" cook measure")
for {order,want}<-[{:little,le},{:big,be}] do
 {true,^want}=T.cook_from(root,value,:binary.copy(<<0xA5>>,size),order)
 Probe.check(T.cook_from(root,value,:binary.copy(<<0xA5>>,size-1),order)==false,name<>" short cook refusal")
end
`, name, unit.root, string(input), string(text)+"\n", hex.EncodeToString(wire), hex.EncodeToString(le), hex.EncodeToString(be))
				if unit.root == "M" {
					fmt.Fprintf(&probe, `{:ok,cooked}=Probe.ProbeCook.cook_open_m(le)
Probe.check(Probe.ProbeCook.m_node_tags_present?(cooked.region,0)==%t,name<>" cooked reader presence")
`, name != "absent")
				}
			}
			probe.WriteString("IO.puts(\"OPTIONAL_MAP_OK\")\n")
			out, err := runArmProbe(t, bin, generateElixir(t, u), probe.String())
			if err != nil || !strings.Contains(out, "OPTIONAL_MAP_OK") || strings.Contains(out, "warning:") {
				t.Fatalf("native optional map: %v\n%s", err, out)
			}
		})
	}
}
