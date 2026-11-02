package elixirtable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestEnumCollectionReferencesElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir enum collection references distinguish None, foreign ids, and damaged references")
	u := unitFrom(t, `package enumref
 enum Mode { A, B }
 table Root {
  items []Mode
  bounded [..2]Mode
  keyed [Mode]Mode
 }`)
	m := tabletext.NewModel(u)
	var probe strings.Builder
	probe.WriteString("alias Enumref.Table,as: T\n")
	cases := []struct {
		name    string
		ref     []byte
		damaged bool
		unknown int
	}{
		{"none", []byte{0}, false, 0}, {"declared", []byte{2}, false, 0}, {"foreign", []byte{3}, false, 1},
		{"above_table", []byte{4}, true, 0}, {"huge", binary.AppendUvarint(nil, ^uint64(0)), true, 0}, {"unfinished", []byte{128}, true, 0}, {"noncanonical", []byte{128, 0}, true, 0},
	}
	for _, field := range []string{"items", "bounded", "keyed"} {
		for _, c := range cases {
			kind := byte(14)
			payload := []byte{30, 1}
			if field == "keyed" {
				kind = 16
				payload = append(payload, 2)
				payload = binary.AppendUvarint(payload, uint64(len(c.ref)))
			}
			payload = append(payload, c.ref...)
			wire := binary.AppendUvarint([]byte{1, 1, kind}, uint64(len(payload)))
			wire = append(wire, payload...)
			wire = append(wire, 0)
			for _, id := range []uint64{ir.TableWireId(field), ir.TableWireId(u.Enums["Mode"].VariantWireNameOf("A")), ir.TableWireId("foreign")} {
				wire = binary.LittleEndian.AppendUint64(wire, id)
			}
			wire = binary.LittleEndian.AppendUint64(wire, 3)
			v := m.New(u.Tables["Root"])
			var report tabletext.Report
			ok, e := tablewire.Decode(m, v, wire, &report)
			if e != nil || ok == c.damaged || report.Malformed != c.damaged || (!c.damaged && int(report.Unknown) != c.unknown) {
				t.Fatalf("%s/%s oracle: %v %+v %v", field, c.name, ok, report, e)
			}
			canonical := ""
			if ok {
				text, e := m.Write(v)
				if e != nil {
					t.Fatal(e)
				}
				canonical = string(text) + "\n"
				if c.name == "declared" && !strings.Contains(canonical, "\"A\"") {
					t.Fatalf("declared value missing: %s", canonical)
				}
			}
			fmt.Fprintf(&probe, `wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
for result<-[T.load("Root",wire),T.load_retain("Root",wire,store,zero)] do
 if %t do
  unless elem(result,0)==:error and elem(result,1)==:malformed and elem(result,2).malformed,do: raise(%q<>inspect(result))
 else
  unless elem(result,0)==:ok and not elem(result,2).malformed and elem(result,2).unknown==%d and elem(result,2).kind_mismatch==0,do: raise(%q<>inspect(result))
  {:ok,text}=T.to_json("Root",elem(result,1))
  unless text==%q,do: raise("enum value "<>text)
 end
end
`, hex.EncodeToString(wire), c.damaged, field+"/"+c.name+" ", c.unknown, field+"/"+c.name+" ", canonical)
		}
	}
	probe.WriteString("IO.puts(\"ENUM_REFERENCES_OK\")\n")
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe.String())
	if e != nil || !strings.Contains(out, "ENUM_REFERENCES_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native enum references: %v\n%s", e, out)
	}
}
