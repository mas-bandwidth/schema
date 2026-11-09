package elixirtable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestUnionArrayElementKindsElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir array-arm inner kind mismatch clears the selected union in both readers")
	u := unitFrom(t, `package armarraykind
 enum Mode { A }
 type Cell { value int32 }
 union Leaf { count int32 }
 union Choice {
 numbers [2]int32
 codes [..2]Mode
 items [..2]Cell
 variants [..2]Leaf
 raw bytes(2)
 }
 union Wrap { selected Choice }
 table Root {
 leaf Leaf
 choice Choice
 batch [..2]Choice
 nested Wrap
 }`)
	m := tabletext.NewModel(u)
	var probe strings.Builder
	probe.WriteString("alias Armarraykind.Table,as: T\n")
	for _, a := range []struct {
		name string
		kind byte
	}{{"numbers", 4}, {"codes", 30}, {"items", 13}, {"variants", 15}, {"raw", 6}} {
		for _, p := range []struct {
			body     []byte
			selected bool
			mismatch int
		}{{[]byte{0, 0}, false, 1}, {[]byte{255, 0}, false, 1}, {[]byte{a.kind, 0}, true, 0}, {nil, true, 0}, {[]byte{a.kind}, true, 0}} {
			for _, field := range []string{"choice", "batch", "nested"} {
				arm := append([]byte{2, 14, byte(len(p.body))}, p.body...)
				wire := []byte{1, 1}
				switch field {
				case "choice":
					wire = append(wire, 15)
					wire = append(wire, arm...)
				case "batch":
					body := append([]byte{15, 1}, arm...)
					wire = append(wire, 14, byte(len(body)))
					wire = append(wire, body...)
				case "nested":
					wire = append(wire, 15, 3, 15, byte(len(arm)))
					wire = append(wire, arm...)
				}
				wire = append(wire, 0)
				ids := []string{field, a.name}
				if field == "nested" {
					ids = append(ids, "selected")
				}
				for _, id := range ids {
					wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId(id))
				}
				wire = binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
				value := m.New(u.Tables["Root"])
				var report tabletext.Report
				ok, e := tablewire.Decode(m, value, wire, &report)
				label := fmt.Sprintf("%s/%s/%x", field, a.name, p.body)
				// Go oracle: strict readers refuse a kind mismatch (and any
				// truncated arm payload they cannot read); a lawful arm reads whole.
				var wantText, want []byte
				if ok {
					if e != nil || report.Refused {
						t.Fatalf("%s oracle: %v %v %+v", label, ok, e, report)
					}
					wantText, e = m.Write(value)
					if e != nil {
						t.Fatal(e)
					}
					if strings.Contains(string(wantText), strconv.Quote(a.name)+":") != p.selected {
						t.Fatalf("%s/%s/%x arm presence: %s", field, a.name, p.body, wantText)
					}
					want, e = tablewire.Encode(m, value)
					if e != nil {
						t.Fatal(e)
					}
				} else if e == nil || !report.Refused || report.Reason == "" || !tablewire.ReadRefusal(e) {
					t.Fatalf("%s oracle refusal: %v %v %+v", label, ok, e, report)
				}
				if p.mismatch != 0 {
					fmt.Fprintf(&probe, `wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,widened: 0,retained: 0,retain_lost: 0,reason: "",missing_required: 0}
for result<-[T.load("Root",wire),T.load_retain("Root",wire,store,zero)] do
 unless elem(result,0)==:error and is_atom(elem(result,1)) and elem(result,2).reason != "",do: raise(%q<>" refusal "<>inspect(result))
 if tuple_size(result)==4,do: (unless elem(result,3).records==[] and elem(result,3).used==0 and elem(result,3).ids==[],do: raise(%q<>" retain not empty "<>inspect(result)))
end
`, hex.EncodeToString(wire), label, label)
					continue
				}
				if ok {
					fmt.Fprintf(&probe, `wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,widened: 0,retained: 0,retain_lost: 0,reason: "",missing_required: 0}
for result<-[T.load("Root",wire),T.load_retain("Root",wire,store,zero)] do
 unless elem(result,0)==:ok and elem(result,2).reason=="",do: raise(%q<>" report "<>inspect(result))
 value=elem(result,1)
 {:ok,text}=T.to_json("Root",value)
 unless text==%q,do: raise(%q<>" value "<>text)
 written=if tuple_size(result)==4,do: T.save_retain("Root",value,elem(result,3),zero),else: T.save("Root",value)
 unless elem(written,0)==:ok and Base.encode16(elem(written,1),case: :lower)==%q,do: raise(%q<>" save "<>inspect(written))
end
`, hex.EncodeToString(wire), label, string(wantText)+"\n", label, hex.EncodeToString(want), label)
				} else {
					// The Go reader refuses a truncated arm payload the
					// Elixir reader still reads as an empty arm; assert the
					// Elixir value and a successful save without a Go oracle.
					fmt.Fprintf(&probe, `wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,widened: 0,retained: 0,retain_lost: 0,reason: "",missing_required: 0}
for result<-[T.load("Root",wire),T.load_retain("Root",wire,store,zero)] do
 unless elem(result,0)==:ok and elem(result,2).reason=="",do: raise(%q<>" report "<>inspect(result))
 value=elem(result,1)
 {:ok,text}=T.to_json("Root",value)
 unless String.contains?(text,%q)==%t,do: raise(%q<>" value "<>text)
 written=if tuple_size(result)==4,do: T.save_retain("Root",value,elem(result,3),zero),else: T.save("Root",value)
 unless elem(written,0)==:ok,do: raise(%q<>" save "<>inspect(written))
end
`, hex.EncodeToString(wire), label, strconv.Quote(a.name)+":", p.selected, label, label)
				}
			}
		}
	}
	probe.WriteString("IO.puts(\"UNION_ARRAY_KINDS_OK\")\n")
	out := runCompleteUnitProbe(t, u, probe.String())
	if !strings.Contains(out, "UNION_ARRAY_KINDS_OK") {
		t.Fatal(out)
	}
}

func TestPinnedUnionArrayElementKindElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir permanent union-array mismatched element kind replay")
	u := loadVariableRetainFixture(t, "../../../tables/messages/Messages.schema")
	m := tabletext.NewModel(u)
	wire, e := os.ReadFile("../../../testdata/wire/tables/fuzz-vectors/union_array_element_kind.bin")
	if e != nil {
		t.Fatal(e)
	}
	value := m.New(u.Tables["ToolMessage"])
	var report tabletext.Report
	ok, e := tablewire.Decode(m, value, wire, &report)
	if ok || e == nil || !report.Refused || report.Reason == "" || !tablewire.ReadRefusal(e) {
		t.Fatalf("pin oracle refusal: %v %v %+v", ok, e, report)
	}
	probe := fmt.Sprintf(`alias Messagedemo.Table,as: T
wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,widened: 0,retained: 0,retain_lost: 0,reason: "",missing_required: 0}
for result<-[T.load("ToolMessage",wire),T.load_retain("ToolMessage",wire,store,zero)] do
 unless elem(result,0)==:error and is_atom(elem(result,1)) and elem(result,2).reason != "",do: raise("pin refusal "<>inspect(result))
 if tuple_size(result)==4,do: (unless elem(result,3).records==[] and elem(result,3).used==0 and elem(result,3).ids==[],do: raise("pin retain not empty "<>inspect(result)))
end
IO.puts("PINNED_UNION_ARRAY_KIND_OK")
`, hex.EncodeToString(wire))
	out := runCompleteUnitProbe(t, u, probe)
	if !strings.Contains(out, "PINNED_UNION_ARRAY_KIND_OK") {
		t.Fatal(out)
	}
}
