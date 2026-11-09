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
	slowtest.Gate(t, "Elixir text-list total over-bound and discarded-tail refusals")
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
zero=%%{unknown: 0,widened: 0,retained: 0,retain_lost: 0,reason: "",missing_required: 0}
for field<-["a","b","c","d"] do
 bad=%%{field=>[%%{"value"=>"abc"},%%{"value"=>"abc"}]}
 P.ok(T.save("Totals",bad)=={:error,:invalid_value},"total wire refusal "<>field)
 P.ok(T.to_json("Totals",bad)=={:error,:invalid_value},"total JSON refusal "<>field)
 P.ok(T.cook_measure("Totals",bad)==-1,"total cook refusal "<>field)
end
wire=Base.decode16!(%q,case: :lower)
# Over-total input refuses the whole load under the strict contract:
# no clamped prefix value is returned, plain or retaining.
for result<-[T.load("Totals",wire),T.from_json("Totals",%q)] do
 P.ok(match?({:error,a,%%{reason: reason}} when a == :malformed or (is_binary(reason) and reason != ""),result),"total over-bound refusal "<>inspect(result))
end
case T.load_retain("Totals",wire,store,zero) do
 {:error,a,r,ret} -> P.ok((a == :malformed or r.reason != "") and ret.records==[] and ret.used==0,"total retaining refusal "<>inspect({r,ret}))
 other -> P.ok(false,"total retaining refusal "<>inspect(other))
end
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
		fmt.Fprintf(&p, `case T.from_json("Totals",%q) do
 {:error,a,r} -> P.ok(a == :malformed or r.reason != "","overbound tail refusal %s")
 other -> P.ok(false,"overbound tail refusal %s "<>inspect(other))
end
wire=Base.decode16!(%q,case: :lower)
case T.load("Totals",wire) do
 {:error,a,r} -> P.ok(a == :malformed or r.reason != "","malformed discarded tail refusal %s")
 other -> P.ok(false,"malformed discarded tail refusal %s "<>inspect(other))
end
case T.load_retain("Totals",wire,store,zero) do
 {:error,a,r,ret} -> P.ok((a == :malformed or r.reason != "") and ret.records==[] and ret.used==0,"retaining tail refusal %s")
 other -> P.ok(false,"retaining tail refusal %s "<>inspect(other))
end
`, input, field, field, hex.EncodeToString(wire), field, field, field, field)
		fmt.Fprintf(&p, `case T.from_json("Totals",%q) do
 {:error,a,r} -> P.ok(a == :malformed or r.reason != "","JSON NUL tail refusal %s")
 other -> P.ok(false,"JSON NUL tail refusal %s "<>inspect(other))
end
`, strings.Replace(input, "toolong", `\u0000`, 1), field, field)
		if field == "b" || field == "d" {
			wire[at] = 't'
			wire[at+len("toolong")] = 255
			fmt.Fprintf(&p, `wire=Base.decode16!(%q,case: :lower)
case T.load("Totals",wire) do
 {:error,a,r} -> P.ok(a == :malformed or r.reason != "","damaged optional body refusal %s")
 other -> P.ok(false,"damaged optional body refusal %s "<>inspect(other))
end
case T.load_retain("Totals",wire,store,zero) do
 {:error,a,r,ret} -> P.ok((a == :malformed or r.reason != "") and ret.records==[] and ret.used==0,"retaining damaged optional body refusal %s")
 other -> P.ok(false,"retaining damaged optional body refusal %s "<>inspect(other))
end
`, hex.EncodeToString(wire), field, field, field, field)
		}
	}
	p.WriteString("IO.puts(\"TEXT_TOTAL_OK\")\n")
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), p.String())
	if e != nil || !strings.Contains(out, "TEXT_TOTAL_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native text-list totals: %v\n%s", e, out)
	}
}
