package elixirtable

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestRetainingUnionArmWideningElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir retaining union arms share widening and malformed-width rules with plain reads")
	u := unitFrom(t, `package armwiden
 union Pick {
 number int32
 positive uint32
 real float64
 bounded int32 | min = 0, max = 5
 }
 union Outer {
 nested Pick
 }
 table Root {
 choice Pick
 choices [..2]Pick
 wrapped Outer
 }`)
	m := tabletext.NewModel(u)
	var probe strings.Builder
	probe.WriteString("alias Armwiden.Table,as: T\n")
	for _, field := range []string{"choice", "choices", "wrapped"} {
		for _, tc := range []struct {
			name, arm                  string
			kind                       byte
			payload                    []byte
			widened, clamped, mismatch int
			malformed                  bool
		}{
			{"signed", "number", ir.TableKindI16, []byte{7, 0}, 1, 0, 0, false},
			{"unsigned", "positive", ir.TableKindU16, []byte{250, 255}, 1, 0, 0, false},
			{"float", "real", ir.TableKindF32, binary.LittleEndian.AppendUint32(nil, math.Float32bits(1.5)), 1, 0, 0, false},
			{"clamped", "bounded", ir.TableKindI16, []byte{7, 0}, 1, 1, 0, false},
			{"narrowing", "number", ir.TableKindI64, binary.LittleEndian.AppendUint64(nil, 7), 0, 0, 1, false},
			{"incompatible", "number", ir.TableKindBool, []byte{1}, 0, 0, 1, false},
			{"short", "number", ir.TableKindI16, []byte{7}, 0, 0, 0, true},
			{"long", "number", ir.TableKindI16, []byte{7, 0, 0}, 0, 0, 0, true},
		} {
			ids := []string{field, tc.arm}
			ref := byte(2)
			if field == "wrapped" {
				ids = []string{field, "nested", tc.arm}
				ref = 3
			}
			arm := append([]byte{ref, tc.kind, byte(len(tc.payload))}, tc.payload...)
			kind := byte(15)
			payload := arm
			if field == "choices" {
				kind = 14
				payload = append([]byte{15, 1}, arm...)
			}
			if field == "wrapped" {
				payload = append([]byte{2, 15, byte(len(arm))}, arm...)
			}
			wire := []byte{1, 1, kind}
			if kind == 14 {
				wire = append(wire, byte(len(payload)))
			}
			wire = append(wire, payload...)
			wire = append(wire, 0)
			for _, name := range ids {
				wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId(name))
			}
			wire = binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
			v := m.New(u.Tables["Root"])
			var report tabletext.Report
			ok, e := tablewire.Decode(m, v, wire, &report)
			if e != nil || ok == tc.malformed || report.Malformed != tc.malformed || int(report.Widened) != tc.widened || int(report.Clamped) != tc.clamped || int(report.KindMismatch) != tc.mismatch {
				t.Fatalf("%s/%s independent report: %v %v %+v", field, tc.name, ok, e, report)
			}
			canonical := ""
			if ok {
				text, e := m.Write(v)
				if e != nil {
					t.Fatal(e)
				}
				canonical = string(text) + "\n"
			}
			fmt.Fprintf(&probe, `wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
for result<-[T.load("Root",wire),T.load_retain("Root",wire,store,zero)] do
 report=elem(result,2)
 unless report.widened==%d and report.clamped==%d and report.kind_mismatch==%d and report.malformed==%t,do: raise(%q<>inspect(result))
 if %t do
  unless elem(result,0)==:error and elem(result,1)==:malformed,do: raise("damage verdict "<>inspect(result))
 else
  unless elem(result,0)==:ok,do: raise("clean verdict "<>inspect(result))
  {:ok,text}=T.to_json("Root",elem(result,1))
  unless text==%q,do: raise("widened value "<>text)
 end
end
`, hex.EncodeToString(wire), tc.widened, tc.clamped, tc.mismatch, tc.malformed, field+"/"+tc.name+" ", tc.malformed, canonical)
		}
	}
	probe.WriteString("IO.puts(\"UNION_ARM_WIDENING_OK\")\n")
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe.String())
	if e != nil || !strings.Contains(out, "UNION_ARM_WIDENING_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native union arm widening: %v\n%s", e, out)
	}
}

func TestPinnedUnionArmWideningElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir known-arm widening replays its permanent plain and retaining input")
	u := loadVariableRetainFixture(t, "../../../tables/messages/Messages.schema")
	m := tabletext.NewModel(u)
	wire, e := os.ReadFile("../../../testdata/wire/tables/fuzz-vectors/retain_union_arm_widen.bin")
	if e != nil {
		t.Fatal(e)
	}
	v := m.New(u.Tables["ToolMessage"])
	store := tablewire.Retain{Capacity: 4096, IdCapacity: 256}
	var report tabletext.Report
	ok, e := tablewire.DecodeRetain(m, v, wire, &store, &report)
	if e != nil || !ok || report.Malformed || report.Widened != 1 || report.KindMismatch != 0 || report.Clamped != 0 || report.RetainLost != 0 {
		t.Fatalf("pinned widening oracle: %v %v %+v", ok, e, report)
	}
	// Canonical output uses the declared uint32 arm with the same value17.
	want := []byte{1, 1, 15, 2, 15, 7, 3, byte(ir.TableKindU32), 4, 17, 0, 0, 0, 0}
	for _, name := range []string{"body", "origin", "pid"} {
		want = binary.LittleEndian.AppendUint64(want, ir.TableWireId(name))
	}
	want = binary.LittleEndian.AppendUint64(want, 3)
	encoded, e := tablewire.Encode(m, v)
	if e != nil || !bytes.Equal(encoded, want) {
		t.Fatalf("literal widened pin: %x want %x, %v", encoded, want, e)
	}
	probe := fmt.Sprintf(`alias Messagedemo.Table,as: T
wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
for result<-[T.load("ToolMessage",wire),T.load_retain("ToolMessage",wire,store,zero)] do
 unless elem(result,0)==:ok,do: raise("widening verdict "<>inspect(result))
 report=elem(result,2)
 unless report.widened==1 and report.kind_mismatch==0 and report.clamped==0 and report.retain_lost==0,do: raise("widening report "<>inspect(report))
 {:ok,saved}=T.save("ToolMessage",elem(result,1))
 unless Base.encode16(saved,case: :lower)==%q,do: raise("widening value "<>inspect(saved))
end
IO.puts("PINNED_UNION_WIDENING_OK")
`, hex.EncodeToString(wire), hex.EncodeToString(want))
	out := runCompleteUnitProbe(t, u, probe)
	if !strings.Contains(out, "PINNED_UNION_WIDENING_OK") {
		t.Fatalf("native widening pin: %s", out)
	}
}
