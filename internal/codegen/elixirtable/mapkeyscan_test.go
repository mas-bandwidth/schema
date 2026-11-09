package elixirtable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestMapKeyAdmissionElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir map key scan precedes value decode and preserves last-key semantics")
	u := unitFrom(t, `package keyscan
 table Root {
 numbers map[uint32]int32
 words map[string(3)]int32
 free map[string]int32
 tail int32
 }`)
	m := tabletext.NewModel(u)
	var probe strings.Builder
	probe.WriteString("alias Keyscan.Table,as: T\n")
	for _, tc := range []struct {
		name, field string
		entries     []string
		widened     int
		refused     bool
	}{
		{"duplicate-retains-only-last", "numbers", []string{"02080100000003040a00000005040b00000000", "02080100000003041400000005041500000000"}, 0, true},
		{"duplicate-retires-first", "numbers", []string{"02080100000003040a00000005040b00000000", "02080100000003041400000000"}, 0, true},
		{"duplicate-numeric", "numbers", []string{"02080100000003040a00000000", "02080100000003041400000000"}, 0, true},
		{"duplicate-text", "words", []string{"020c016103040a00000000", "020c016103041400000000"}, 0, true},
		{"duplicate-before-bad-key", "numbers", []string{"02080100000003040a00000000", "02080100000003041400000000", "0200"}, 0, true},
		{"unknown-kind", "numbers", []string{"0200"}, 0, true},
		{"bad-then-exact", "numbers", []string{"0209000000000000000002080700000003040900000000"}, 0, true},
		{"bad-then-widened", "numbers", []string{"0209000000000000000002060703040900000000"}, 0, true},
		{"exact-then-bad", "numbers", []string{"0208070000000209000000000000000000"}, 0, true},
		{"bad-then-framing", "numbers", []string{"020900000000000000000500"}, 0, true},
		{"good-then-framing", "numbers", []string{"0208070000000500"}, 0, true},
		{"two-widened-keys", "numbers", []string{"02060103040a00000000", "02060203041400000000"}, 1, false},
		{"prior-retention-then-bad", "numbers", []string{"02080100000003040a00000005040b00000000", "0200"}, 0, true},
		{"bad-key-skips-value", "numbers", []string{"02000300"}, 0, true},
		{"unbounded-key", "free", []string{"020c046c6f6e6703041700000000"}, 0, false},
		{"over-bound-between", "words", []string{"020c016103040100000000", "020c046c6f6e6703116300", "020c016203040200000000"}, 0, true},
		{"bad-utf8-before-later-key", "words", []string{"020c01ff020c016100"}, 0, true},
		{"bad-then-valid-text", "words", []string{"020607020c016103040900000000"}, 0, true},
	} {
		body := []byte{13, byte(len(tc.entries))}
		for _, s := range tc.entries {
			b, e := hex.DecodeString(s)
			if e != nil {
				t.Fatal(e)
			}
			body = append(body, byte(len(b)))
			body = append(body, b...)
		}
		wire := append([]byte{1, 1, 14, byte(len(body))}, body...)
		wire = append(wire, 4, 4, 7, 0, 0, 0, 0)
		ids := []uint64{ir.TableWireId(tc.field), ir.MapKeyWireId, ir.MapValueWireId, ir.TableWireId("tail"), ir.TableWireId("unknown")}
		for _, id := range ids {
			wire = binary.LittleEndian.AppendUint64(wire, id)
		}
		wire = binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
		v := m.New(u.Tables["Root"])
		store := tablewire.Retain{Capacity: 4096, IdCapacity: 256}
		var r, s tabletext.Report
		ok, e := tablewire.DecodeRetain(m, v, wire, &store, &r)
		if tc.refused {
			if ok || !tablewire.ReadRefusal(e) || !r.Refused || r.Reason == "" {
				t.Fatalf("%s neutral: ok=%v err=%v %+v", tc.name, ok, e, r)
			}
		} else if !ok || e != nil || r.Refused || r.Widened != tc.widened || r.Unknown != 0 || r.Retained != 0 || r.RetainLost != 0 {
			t.Fatalf("%s neutral: ok=%v err=%v %+v", tc.name, ok, e, r)
		}
		text, plain, saved := "", "", ""
		saveLost := 0
		if ok {
			j, e := m.Write(v)
			if e != nil {
				t.Fatal(e)
			}
			text = string(j) + "\n"
			p, e := tablewire.Encode(m, v)
			if e != nil {
				t.Fatal(e)
			}
			plain = hex.EncodeToString(p)
			p, e = tablewire.EncodeRetain(m, v, &store, &s)
			if e != nil || s.RetainLost != saveLost {
				t.Fatalf("%s save %v %+v", tc.name, e, s)
			}
			saved = hex.EncodeToString(p)
		}
		fmt.Fprintf(&probe, `
wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,widened: 0,retained: 0,retain_lost: 0,reason: "",missing_required: 0}
for retaining<-[false,true] do
 result=if retaining,do: T.load_retain("Root",wire,store,zero),else: T.load("Root",wire)
 r=elem(result,2)
 if %t do
  unless elem(result,0)==:error and r.reason != "",do: raise(%q<>" damaged key accepted "<>inspect(result))
 else
  unless r.widened==%d and r.unknown==0 and r.reason == "",do: raise(%q<>" report "<>inspect(result))
  unless elem(result,0)==:ok,do: raise("clean key refused "<>inspect(result))
  v=elem(result,1)
  {:ok,text}=T.to_json("Root",v)
  unless text==%q,do: raise(%q<>" value "<>text)
  if retaining do
   unless r.retained==0 and r.retain_lost==0,do: raise("load ownership "<>inspect(r))
   {:ok,bytes,sr,_}=T.save_retain("Root",v,elem(result,3),zero)
   unless Base.encode16(bytes,case: :lower)==%q and sr.retain_lost==%d,do: raise(%q<>" retained save "<>inspect({bytes,sr}))
  else
   {:ok,bytes}=T.save("Root",v)
   unless Base.encode16(bytes,case: :lower)==%q,do: raise("plain save")
  end
 end
end
`, hex.EncodeToString(wire), tc.refused, tc.name, tc.widened, tc.name, text, tc.name, saved, saveLost, tc.name, plain)
	}
	probe.WriteString("IO.puts(\"MAP_KEY_ADMISSION_OK\")\n")
	out := runCompleteUnitProbe(t, u, probe.String())
	if !strings.Contains(out, "MAP_KEY_ADMISSION_OK") {
		t.Fatal(out)
	}
}

func TestPinnedMapKeyKindElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir map key mismatch remains clean after independent wire discovery")
	paths, e := filepath.Glob("../../../tables/maps/*.schema")
	if e != nil || len(paths) == 0 {
		t.Fatal(e)
	}
	u := loadUnit(t, paths...)
	wire, e := os.ReadFile("../../../testdata/wire/tables/fuzz-vectors/map_key_kind_unknown.bin")
	if e != nil {
		t.Fatal(e)
	}
	m := tabletext.NewModel(u)
	v := m.New(u.Tables["Docs"])
	var report tabletext.Report
	ok, e := tablewire.Decode(m, v, wire, &report)
	if ok || !tablewire.ReadRefusal(e) || !report.Refused || report.Reason == "" {
		t.Fatalf("pin: %v %v %+v", ok, e, report)
	}
	probe := fmt.Sprintf(`alias Mapdemo.Table,as: T
wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,widened: 0,retained: 0,retain_lost: 0,reason: "",missing_required: 0}
for result<-[T.load("Docs",wire),T.load_retain("Docs",wire,store,zero)] do
 unless elem(result,0)==:error and elem(result,2).reason != "",do: raise("key pin "<>inspect(result))
end
IO.puts("MAP_KEY_PIN_OK")
`, hex.EncodeToString(wire))
	out := runCompleteUnitProbe(t, u, probe)
	if !strings.Contains(out, "MAP_KEY_PIN_OK") {
		t.Fatal(out)
	}
}

func TestPinnedMapDuplicateCounterElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir equal default map keys count duplicates after unknown key fields")
	paths, e := filepath.Glob("../../../tables/maps/*.schema")
	if e != nil || len(paths) == 0 {
		t.Fatal(e)
	}
	u := loadUnit(t, paths...)
	wire, e := os.ReadFile("../../../testdata/wire/tables/fuzz-vectors/map_key_default_duplicate.bin")
	if e != nil {
		t.Fatal(e)
	}
	m := tabletext.NewModel(u)
	v := m.New(u.Tables["Docs"])
	var report tabletext.Report
	ok, e := tablewire.Decode(m, v, wire, &report)
	if ok || !tablewire.ReadRefusal(e) || !report.Refused || report.Reason == "" {
		t.Fatalf("pin: %v %v %+v", ok, e, report)
	}
	probe := fmt.Sprintf(`alias Mapdemo.Table,as: T
wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,widened: 0,retained: 0,retain_lost: 0,reason: "",missing_required: 0}
for result<-[T.load("Docs",wire),T.load_retain("Docs",wire,store,zero)] do
 unless elem(result,0)==:error and elem(result,2).reason != "",do: raise("key pin "<>inspect(result))
end
IO.puts("MAP_KEY_PIN_OK")
`, hex.EncodeToString(wire))
	out := runCompleteUnitProbe(t, u, probe)
	if !strings.Contains(out, "MAP_KEY_PIN_OK") {
		t.Fatal(out)
	}
}
