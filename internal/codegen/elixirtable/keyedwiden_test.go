package elixirtable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestKeyedElementWideningElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir keyed scalar elements widen once per field at incoming width")
	u := unitFrom(t, `package keyedwiden
 enum Slot { A, B }
 table Root {
 values [Slot]int32
 bounds [Slot]int32 | min = 0, max = 5
 positive [Slot]uint64
 real [Slot]float64
 wide [Slot]int128 | min = -1180591620717411303424, max = 1180591620717411303424
 }`)
	m := tabletext.NewModel(u)
	var probe strings.Builder
	probe.WriteString("alias Keyedwiden.Table,as: T\n")
	for _, tc := range []struct {
		name, field   string
		kind          byte
		slots         [][]byte
		widened       int
		refused       bool
		elixirRefused bool
	}{
		{"signed8", "values", ir.TableKindI8, [][]byte{{10}, {30}}, 1, false, false},
		{"signed16", "values", ir.TableKindI16, [][]byte{{252, 255}, {6, 0}}, 1, false, false},
		{"padded-slots", "values", ir.TableKindI8, [][]byte{{10, 0, 0, 0}, {30, 0, 0, 0}}, 1, true, false},
		{"unsigned", "positive", ir.TableKindU16, [][]byte{{250, 255}, {3, 0}}, 1, false, false},
		{"float", "real", ir.TableKindF32, [][]byte{binary.LittleEndian.AppendUint32(nil, math.Float32bits(1.5)), binary.LittleEndian.AppendUint32(nil, math.Float32bits(2.5))}, 1, false, false},
		{"wide", "wide", ir.TableKindI64, [][]byte{binary.LittleEndian.AppendUint64(nil, 1<<60+1)}, 1, false, false},
		{"range", "bounds", ir.TableKindI16, [][]byte{{254, 255}, {9, 0}}, 0, true, true},
		{"empty", "values", ir.TableKindI8, nil, 1, false, false},
		{"same-kind", "values", ir.TableKindI32, [][]byte{{7, 0, 0, 0}}, 0, false, false},
		{"narrowing", "values", ir.TableKindI64, [][]byte{binary.LittleEndian.AppendUint64(nil, 7)}, 0, true, true},
		{"short-slot", "values", ir.TableKindI16, [][]byte{{7}}, 0, true, true},
	} {
		body := []byte{tc.kind, byte(len(tc.slots))}
		for i, slot := range tc.slots {
			body = append(body, byte(i+2), byte(len(slot)))
			body = append(body, slot...)
		}
		wire := append([]byte{1, 1, 16, byte(len(body))}, body...)
		wire = append(wire, 0)
		ids := []uint64{ir.TableWireId(tc.field), ir.TableWireId(u.Enums["Slot"].VariantWireNameOf("A")), ir.TableWireId(u.Enums["Slot"].VariantWireNameOf("B"))}
		for _, id := range ids {
			wire = binary.LittleEndian.AppendUint64(wire, id)
		}
		wire = binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
		v := m.New(u.Tables["Root"])
		var report tabletext.Report
		ok, e := tablewire.Decode(m, v, wire, &report)
		if tc.refused {
			if ok || !tablewire.ReadRefusal(e) || !report.Refused || report.Reason == "" {
				t.Fatalf("%s oracle: %v %v %+v", tc.name, ok, e, report)
			}
		} else if !ok || e != nil || report.Refused || int(report.Widened) != tc.widened {
			t.Fatalf("%s oracle: %v %v %+v", tc.name, ok, e, report)
		}
		text := ""
		if ok {
			raw, e := m.Write(v)
			if e != nil {
				t.Fatal(e)
			}
			text = string(raw) + "\n"
		}
		fmt.Fprintf(&probe, `wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,widened: 0,retained: 0,retain_lost: 0,reason: "",missing_required: 0}
for result<-[T.load("Root",wire),T.load_retain("Root",wire,store,zero)] do
 report=elem(result,2)
 if %t do
  unless elem(result,0)==:error and report.reason != "",do: raise("keyed damage "<>inspect(result))
 else
  unless report.widened==%d and report.reason == "",do: raise(%q<>inspect(result))
  unless elem(result,0)==:ok,do: raise("keyed verdict "<>inspect(result))
  {:ok,text}=T.to_json("Root",elem(result,1))
  unless text==%q or %t,do: raise("keyed values "<>text)
 end
end
`, hex.EncodeToString(wire), tc.elixirRefused, tc.widened, tc.name+" ", text, tc.refused && !tc.elixirRefused)
	}
	probe.WriteString("IO.puts(\"KEYED_WIDENING_OK\")\n")
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe.String())
	if e != nil || !strings.Contains(out, "KEYED_WIDENING_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native keyed widening: %v\n%s", e, out)
	}
}

func TestPinnedKeyedElementWideningElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir keyed element widening replays the enumerated corpus failure")
	paths, e := filepath.Glob("../../../tables/examples/*.schema")
	if e != nil || len(paths) == 0 {
		t.Fatalf("examples unit: %v", e)
	}
	u := loadVariableRetainFixture(t, paths...)
	m := tabletext.NewModel(u)
	wire, e := os.ReadFile("../../../testdata/wire/tables/fuzz-vectors/keyed_element_widen.bin")
	if e != nil {
		t.Fatal(e)
	}
	want, e := os.ReadFile("../../../testdata/wire/tables/keyed_config.bin")
	if e != nil {
		t.Fatal(e)
	}
	v := m.New(u.Tables["KeyedConfig"])
	var report tabletext.Report
	ok, e := tablewire.Decode(m, v, wire, &report)
	if ok || !tablewire.ReadRefusal(e) || !report.Refused || report.Reason == "" {
		t.Fatalf("pinned keyed widening: %v %v %+v", ok, e, report)
	}
	probe := fmt.Sprintf(`alias Tabledemo.Table,as: T
wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,widened: 0,retained: 0,retain_lost: 0,reason: "",missing_required: 0}
for result<-[T.load("KeyedConfig",wire),T.load_retain("KeyedConfig",wire,store,zero)] do
 unless elem(result,0)==:ok,do: raise("keyed pin verdict "<>inspect(result))
 report=elem(result,2)
 unless report.widened==1 and report.reason == "",do: raise("keyed pin report "<>inspect(report))
 {:ok,bytes}=T.save("KeyedConfig",elem(result,1))
 unless Base.encode16(bytes,case: :lower)==%q,do: raise("keyed pin values "<>inspect(bytes))
end
IO.puts("PINNED_KEYED_WIDENING_OK")
`, hex.EncodeToString(wire), hex.EncodeToString(want))
	out := runCompleteUnitProbe(t, u, probe)
	if !strings.Contains(out, "PINNED_KEYED_WIDENING_OK") {
		t.Fatalf("native keyed pin: %s", out)
	}
}
