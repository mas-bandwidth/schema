package elixirtable

import (
	"bytes"
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
	const source = `package enumref
 enum Mode { A, B }
 union Pick {
 mode Mode
 Nothing
 }
 table Root {
  items []Mode
  bounded [..2]Mode
  slots [2]Mode
  keyed [Mode]Mode
  choice Pick
  choices []Pick
 }`
	u := unitFrom(t, source)
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
	for _, field := range []string{"choice", "choices", "items", "bounded", "slots", "keyed"} {
		for _, c := range cases {
			ref := c.ref
			ids := []uint64{ir.TableWireId(field), ir.TableWireId(u.Enums["Mode"].VariantWireNameOf("A")), ir.TableWireId("foreign")}
			kind := byte(14)
			payload := []byte{30, 1}
			if field == "keyed" {
				kind = 16
				payload = append(payload, 2)
				payload = binary.AppendUvarint(payload, uint64(len(ref)))
			}
			if field == "choice" || field == "choices" {
				ids = append(ids, ir.TableWireId("mode"))
				if c.name == "above_table" {
					ref = []byte{5}
				}
				arm := binary.AppendUvarint([]byte{4, 30}, uint64(len(ref)))
				if field == "choice" {
					kind = 15
					payload = arm
				} else {
					payload = append([]byte{15, 1}, arm...)
				}
			}
			payload = append(payload, ref...)
			wire := []byte{1, 1, kind}
			if kind != 15 {
				wire = binary.AppendUvarint(wire, uint64(len(payload)))
			}
			wire = append(wire, payload...)
			wire = append(wire, 0)
			for _, id := range ids {
				wire = binary.LittleEndian.AppendUint64(wire, id)
			}
			wire = binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
			v := m.New(u.Tables["Root"])
			var report tabletext.Report
			ok, e := tablewire.Decode(m, v, wire, &report)
			if ok == c.damaged || tablewire.ReadRefusal(e) != c.damaged || (!c.damaged && report.Unknown != c.unknown) {
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
  unless elem(result,0)==:ok and true and elem(result,2).unknown==%d and elem(result,2).kind_mismatch==0,do: raise(%q<>inspect(result))
  expected_lost=if tuple_size(result)==4,do: elem(result,2).unknown,else: 0
  unless elem(result,2).retain_lost==expected_lost,do: raise("excluded enum loss "<>inspect(result))
  {:ok,text}=T.to_json("Root",elem(result,1))
  unless text==%q,do: raise("enum value "<>text)
 end
end
`, hex.EncodeToString(wire), c.damaged, field+"/"+c.name+" ", c.unknown, field+"/"+c.name+" ", canonical)
		}
	}
	probe.WriteString("IO.puts(\"ENUM_REFERENCES_OK\")\n")
	for _, src := range []string{source, strings.Replace(source, " Nothing\n", "", 1)} {
		out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, unitFrom(t, src)), probe.String())
		if e != nil || !strings.Contains(out, "ENUM_REFERENCES_OK") || strings.Contains(out, "warning:") {
			t.Fatalf("native enum references: %v\n%s", e, out)
		}
	}
}

// Union-array elements have both an element-index step and an arm step in
// their retain path. A replaced occurrence discards its old records without
// loss; an edited surviving array loses only records belonging to removed items.
func TestRetainedUnionArrayBodiesElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir retaining union arrays preserve and replace nested record paths")
	u := unitFrom(t, `package unionretain
 table Inner { value int32 }
 union Pick {
 alpha Inner
 Nothing
 }
 table Root { items []Pick }`)
	m := tabletext.NewModel(u)
	wireFor := func(occurrences ...[]int) []byte {
		wire := []byte{1}
		for _, values := range occurrences {
			body := []byte{15, byte(len(values))}
			for _, v := range values {
				if v < 0 {
					body = append(body, 0)
					continue
				}
				inner := []byte{}
				if v > 0 {
					inner = []byte{3, byte(ir.TableKindI32)}
					inner = binary.LittleEndian.AppendUint32(inner, uint32(v))
				}
				inner = append(inner, 0)
				body = append(body, 2, 13, byte(len(inner)))
				body = append(body, inner...)
			}
			wire = append(wire, 1, 14, byte(len(body)))
			wire = append(wire, body...)
		}
		wire = append(wire, 0)
		for _, name := range []string{"items", "alpha", "future"} {
			wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId(name))
		}
		return binary.LittleEndian.AppendUint64(wire, 3)
	}
	original := wireFor([]int{7, -1, 9})
	cases := []struct {
		name     string
		wire     []byte
		retained int
	}{
		{"intact", original, 2},
		{"replaced-shorter", wireFor([]int{7, -1, 9}, []int{0}), 2},
		{"replaced-empty", wireFor([]int{7, -1, 9}, []int{}), 2},
	}
	var probe strings.Builder
	probe.WriteString("alias Unionretain.Table, as: T\n")
	for _, c := range cases {
		v := m.New(u.Tables["Root"])
		store := tablewire.Retain{Capacity: 4096, IdCapacity: 256}
		var load, save tabletext.Report
		ok, e := tablewire.DecodeRetain(m, v, c.wire, &store, &load)
		if e != nil || !ok || load.Refused || load.Retained != c.retained || load.Unknown != 2 || load.RetainLost != 0 {
			t.Fatalf("%s load: %v %v %+v", c.name, ok, e, load)
		}
		want, e := tablewire.EncodeRetain(m, v, &store, &save)
		if e != nil || save.RetainLost != 0 {
			t.Fatalf("%s save: %v %+v", c.name, e, save)
		}
		if c.name == "intact" && !bytes.Equal(want, original) {
			t.Fatalf("independent literal round trip changed: %x != %x", want, original)
		}
		fmt.Fprintf(&probe, `wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
{:ok,value,report,store}=T.load_retain("Root",wire,store,zero)
unless report.retained==2 and report.unknown==2 and report.retain_lost==0, do: raise("load %s "<>inspect(report))
{:ok,saved,report,_}=T.save_retain("Root",value,store,zero)
unless Base.encode16(saved,case: :lower)==%q and report.retain_lost==0, do: raise("save %s "<>inspect({saved,report}))
if %t do
 {:ok,_edited,report,_}=T.save_retain("Root",Map.update!(value,"items",&Enum.take(&1,1)),store,zero)
 unless report.retain_lost==1, do: raise("removed item loss "<>inspect(report))
end
`, hex.EncodeToString(c.wire), c.name, hex.EncodeToString(want), c.name, c.name == "intact")
	}
	probe.WriteString("IO.puts(\"UNION_ARRAY_RETAIN_OK\")\n")
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe.String())
	if e != nil || !strings.Contains(out, "UNION_ARRAY_RETAIN_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native union array retain: %v\n%s", e, out)
	}
}
