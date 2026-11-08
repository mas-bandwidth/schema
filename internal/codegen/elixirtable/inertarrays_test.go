package elixirtable

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestInertArrayHeadersElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir array bodies shorter than their two-byte header are inert in both read modes")
	u := unitFrom(t, `package inertarray
 enum Key { A, B }
 table Inner { value int32 }
 union Pick { child Inner }
 table Root {
 payload bytes(8)
 numbers [..2]int32
 children [..2]Inner
 pointers [..2]*Inner
 choices [..2]Pick
 keyed [Key]Inner
 lookup map[uint32]Inner
 }`)
	m := tabletext.NewModel(u)
	var probe strings.Builder
	probe.WriteString(`alias Inertarray.Table,as: T
Process.put(:failures,[])
check=fn yes,msg->unless yes,do: Process.put(:failures,[msg|Process.get(:failures)]) end
`)
	for _, f := range []struct {
		name          string
		kind, element byte
	}{
		{"payload", 14, 6}, {"numbers", 14, 4}, {"children", 14, 13}, {"pointers", 14, 17}, {"choices", 14, 15}, {"keyed", 16, 13}, {"lookup", 14, 13},
	} {
		for _, body := range [][]byte{nil, {f.element}, {255}, {255, 0}} {
			wire := append([]byte{1, 1, f.kind, byte(len(body))}, body...)
			wire = append(wire, 0)
			wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId(f.name))
			wire = binary.LittleEndian.AppendUint64(wire, 1)
			wantMismatch := 0
			if len(body) == 2 {
				wantMismatch = 1
			}
			v := m.New(u.Tables["Root"])
			var report tabletext.Report
			ok, e := tablewire.Decode(m, v, wire, &report)
			if e != nil || !ok || report.Refused {
				t.Fatalf("%s/%x oracle: %v %v %+v", f.name, body, ok, e, report)
			}
			text, e := m.Write(v)
			if e != nil {
				t.Fatal(e)
			}
			fmt.Fprintf(&probe, `wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
for result<-[T.load("Root",wire),T.load_retain("Root",wire,store,zero)] do
 report=elem(result,2)
 check.(elem(result,0)==:ok and true and report.kind_mismatch==%d,%q<>inspect(result))
 if elem(result,0)==:ok do
  {:ok,text}=T.to_json("Root",elem(result,1))
  check.(text==%q,"inert values "<>text)
 end
end
`, hex.EncodeToString(wire), wantMismatch, fmt.Sprintf("%s/%x ", f.name, body), string(text)+"\n")
		}
	}
	probe.WriteString(`case Process.get(:failures) do
 []->IO.puts("INERT_ARRAY_HEADERS_OK")
 errors->raise(Enum.join(Enum.reverse(errors),"\n"))
end
`)
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe.String())
	if e != nil || !strings.Contains(out, "INERT_ARRAY_HEADERS_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native inert headers: %v\n%s", e, out)
	}
}

func TestInertArrayKeepsRetainedOccurrenceElixir(t *testing.T) {
	slowtest.Gate(t, "an inert later array header preserves the previous body and its retained records")
	u := unitFrom(t, `package inertretain
 table Inner { value int32 }
 table Root { items []Inner }`)
	m := tabletext.NewModel(u)
	inner := binary.LittleEndian.AppendUint32([]byte{2, byte(ir.TableKindI32)}, 7)
	inner = append(inner, 0)
	body := append([]byte{13, 1, byte(len(inner))}, inner...)
	field := append([]byte{1, 14, byte(len(body))}, body...)
	finish := func(body []byte) []byte {
		wire := append([]byte{1}, body...)
		wire = append(wire, 0)
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId("items"))
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId("future"))
		return binary.LittleEndian.AppendUint64(wire, 2)
	}
	want := finish(field)
	var probe strings.Builder
	probe.WriteString("alias Inertretain.Table,as: T\n")
	for _, tail := range [][]byte{{0}, {1, 13}, {1, 255}} {
		fields := append(append([]byte(nil), field...), 1, 14)
		fields = append(fields, tail...)
		wire := finish(fields)
		v := m.New(u.Tables["Root"])
		store := tablewire.Retain{Capacity: 4096, IdCapacity: 256}
		var load, save tabletext.Report
		ok, e := tablewire.DecodeRetain(m, v, wire, &store, &load)
		if e != nil || !ok || load.Unknown != 1 || load.Retained != 1 || load.Refused {
			t.Fatalf("inert occurrence oracle: %v %v %+v", ok, e, load)
		}
		encoded, e := tablewire.EncodeRetain(m, v, &store, &save)
		if e != nil || !bytes.Equal(encoded, want) || save.RetainLost != 0 {
			t.Fatalf("literal retained occurrence changed: %x want %x %v %+v", encoded, want, e, save)
		}
		fmt.Fprintf(&probe, `wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
{:ok,value,report,store}=T.load_retain("Root",wire,store,zero)
unless report.unknown==1 and report.retained==1 and report.kind_mismatch==0 and true,do: raise("inert occurrence load "<>inspect(report))
{:ok,bytes,report,_}=T.save_retain("Root",value,store,zero)
unless Base.encode16(bytes,case: :lower)==%q and report.retain_lost==0,do: raise("inert occurrence save "<>inspect({bytes,report}))
`, hex.EncodeToString(wire), hex.EncodeToString(want))
	}
	probe.WriteString("IO.puts(\"INERT_RETAINED_OCCURRENCE_OK\")\n")
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe.String())
	if e != nil || !strings.Contains(out, "INERT_RETAINED_OCCURRENCE_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native inert occurrence: %v\n%s", e, out)
	}
}

func TestPinnedInertByteArrayHeaderElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir stream bytes header replays the permanent enumerated report divergence")
	u := loadUnit(t, "../../../tables/stream/Stream.schema")
	m := tabletext.NewModel(u)
	wire, e := os.ReadFile("../../../testdata/wire/tables/fuzz-vectors/inert_byte_array_header.bin")
	if e != nil {
		t.Fatal(e)
	}
	v := m.New(u.Tables["Feed"])
	var report tabletext.Report
	ok, e := tablewire.Decode(m, v, wire, &report)
	if e != nil || !ok || report.Refused {
		t.Fatalf("stream inert oracle: %v %v %+v", ok, e, report)
	}
	want, e := tablewire.Encode(m, v)
	if e != nil {
		t.Fatal(e)
	}
	probe := fmt.Sprintf(`alias Streamdemo.Table,as: T
wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
for result<-[T.load("Feed",wire),T.load_retain("Feed",wire,store,zero)] do
 unless elem(result,0)==:ok and elem(result,2).kind_mismatch==1,do: raise("stream inert report "<>inspect(result))
 {:ok,bytes}=T.save("Feed",elem(result,1))
 unless Base.encode16(bytes,case: :lower)==%q,do: raise("stream inert values "<>inspect(bytes))
end
IO.puts("PINNED_INERT_BYTES_OK")
`, hex.EncodeToString(wire), hex.EncodeToString(want))
	out := runCompleteUnitProbe(t, u, probe)
	if !strings.Contains(out, "PINNED_INERT_BYTES_OK") {
		t.Fatalf("native inert bytes pin: %s", out)
	}
}
