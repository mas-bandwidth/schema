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
		name, field                                                        string
		entries                                                            []string
		mismatch, widened, clamped, unknown, retained, saveLost, duplicate int
		malformed                                                          bool
	}{
		{"duplicate-retains-only-last", "numbers", []string{"02080100000003040a00000005040b00000000", "02080100000003041400000005041500000000"}, 0, 0, 0, 2, 2, 0, 1, false},
		{"duplicate-retires-first", "numbers", []string{"02080100000003040a00000005040b00000000", "02080100000003041400000000"}, 0, 0, 0, 1, 1, 0, 1, false},
		{"duplicate-numeric", "numbers", []string{"02080100000003040a00000000", "02080100000003041400000000"}, 0, 0, 0, 0, 0, 0, 1, false},
		{"duplicate-text", "words", []string{"020c016103040a00000000", "020c016103041400000000"}, 0, 0, 0, 0, 0, 0, 1, false},
		{"duplicate-before-bad-key", "numbers", []string{"02080100000003040a00000000", "02080100000003041400000000", "0200"}, 1, 0, 0, 0, 0, 0, 1, false},
		{"unknown-kind", "numbers", []string{"0200"}, 1, 0, 0, 0, 0, 0, 0, false},
		{"bad-then-exact", "numbers", []string{"0209000000000000000002080700000003040900000000"}, 1, 0, 0, 0, 0, 0, 0, false},
		{"bad-then-widened", "numbers", []string{"0209000000000000000002060703040900000000"}, 1, 1, 0, 0, 0, 0, 0, false},
		{"exact-then-bad", "numbers", []string{"0208070000000209000000000000000000"}, 1, 0, 0, 0, 0, 0, 0, false},
		{"bad-then-framing", "numbers", []string{"020900000000000000000500"}, 1, 0, 0, 0, 0, 0, 0, false},
		{"good-then-framing", "numbers", []string{"0208070000000500"}, 0, 0, 0, 0, 0, 0, 0, true},
		{"two-widened-keys", "numbers", []string{"02060103040a00000000", "02060203041400000000"}, 0, 1, 0, 0, 0, 0, 0, false},
		{"prior-retention-then-bad", "numbers", []string{"02080100000003040a00000005040b00000000", "0200"}, 1, 0, 0, 1, 1, 1, 0, false},
		{"bad-key-skips-value", "numbers", []string{"02000300"}, 1, 0, 0, 0, 0, 0, 0, false},
		{"unbounded-key", "free", []string{"020c046c6f6e6703041700000000"}, 0, 0, 0, 0, 0, 0, 0, false},
		{"over-bound-between", "words", []string{"020c016103040100000000", "020c046c6f6e6703116300", "020c016203040200000000"}, 0, 0, 1, 0, 0, 0, 0, false},
		{"bad-utf8-before-later-key", "words", []string{"020c01ff020c016100"}, 0, 0, 0, 0, 0, 0, 0, true},
		{"bad-then-valid-text", "words", []string{"020607020c016103040900000000"}, 1, 0, 0, 0, 0, 0, 0, false},
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
		if ok == tc.malformed || tablewire.ReadRefusal(e) != tc.malformed || r.Widened != tc.widened || r.Unknown != tc.unknown || r.Retained != tc.retained || r.RetainLost != 0 {
			t.Fatalf("%s neutral: ok=%v err=%v %+v", tc.name, ok, e, r)
		}
		text, plain, saved := "", "", ""
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
			if e != nil || s.RetainLost != tc.saveLost {
				t.Fatalf("%s save %v %+v", tc.name, e, s)
			}
			saved = hex.EncodeToString(p)
		}
		fmt.Fprintf(&probe, `
wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
for retaining<-[false,true] do
 result=if retaining,do: T.load_retain("Root",wire,store,zero),else: T.load("Root",wire)
 r=elem(result,2)
 unless r.kind_mismatch==%d and r.widened==%d and r.clamped==%d and r.unknown==%d and Map.get(r,:duplicate,0)==%d and r.malformed==%t,do: raise(%q<>" report "<>inspect(result))
 if %t do
  unless elem(result,0)==:error,do: raise("damaged key accepted")
 else
  unless elem(result,0)==:ok,do: raise("clean key refused "<>inspect(result))
  v=elem(result,1)
  {:ok,text}=T.to_json("Root",v)
  unless text==%q,do: raise(%q<>" value "<>text)
  if retaining do
   unless r.retained==%d and r.retain_lost==0,do: raise("load ownership "<>inspect(r))
   {:ok,bytes,sr,_}=T.save_retain("Root",v,elem(result,3),zero)
   unless Base.encode16(bytes,case: :lower)==%q and sr.retain_lost==%d,do: raise(%q<>" retained save "<>inspect({bytes,sr}))
  else
   {:ok,bytes}=T.save("Root",v)
   unless Base.encode16(bytes,case: :lower)==%q,do: raise("plain save")
  end
 end
end
`, hex.EncodeToString(wire), tc.mismatch, tc.widened, tc.clamped, tc.unknown, tc.duplicate, tc.malformed, tc.name, tc.malformed, text, tc.name, tc.retained, saved, tc.saveLost, tc.name, plain)
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
	if !ok || e != nil || report.Refused {
		t.Fatalf("pin: %v %v %+v", ok, e, report)
	}
	want, e := tablewire.Encode(m, v)
	if e != nil || len(want) != 10 {
		t.Fatalf("pin save %x %v", want, e)
	}
	probe := fmt.Sprintf(`alias Mapdemo.Table,as: T
wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
for result<-[T.load("Docs",wire),T.load_retain("Docs",wire,store,zero)] do
 unless elem(result,0)==:ok and elem(result,2).kind_mismatch==1 and true,do: raise("key pin "<>inspect(result))
 {:ok,written}=T.save("Docs",elem(result,1))
 unless Base.encode16(written,case: :lower)==%q,do: raise("key pin save")
end
IO.puts("MAP_KEY_PIN_OK")
`, hex.EncodeToString(wire), hex.EncodeToString(want))
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
	if !ok || e != nil || report.Refused || report.Unknown != 2 {
		t.Fatalf("pin: %v %v %+v", ok, e, report)
	}
	want, e := tablewire.Encode(m, v)
	if e != nil || len(want) != 64 {
		t.Fatalf("pin save %x %v", want, e)
	}
	probe := fmt.Sprintf(`alias Mapdemo.Table,as: T
wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
for result<-[T.load("Docs",wire),T.load_retain("Docs",wire,store,zero)] do
 unless elem(result,0)==:ok and elem(result,2).kind_mismatch==0 and elem(result,2).unknown==2 and Map.get(elem(result,2),:duplicate,0)==1 and true,do: raise("key pin "<>inspect(result))
 {:ok,written}=T.save("Docs",elem(result,1))
 unless Base.encode16(written,case: :lower)==%q,do: raise("key pin save")
end
IO.puts("MAP_KEY_PIN_OK")
`, hex.EncodeToString(wire), hex.EncodeToString(want))
	out := runCompleteUnitProbe(t, u, probe)
	if !strings.Contains(out, "MAP_KEY_PIN_OK") {
		t.Fatal(out)
	}
}
