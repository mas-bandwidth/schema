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

func TestWideTextValidationBeforeClampElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir UTF-16 fields and arms validate before clamping")
	u := unitFrom(t, `package widetext
 union Choice { wide wstring(2) }
 table Root { text wstring(2)
 choice Choice }
 `)
	m := tabletext.NewModel(u)
	var probe strings.Builder
	probe.WriteString("alias Widetext.Table,as: T\n")
	for _, tc := range []struct {
		name, raw string
		malformed bool
		clamped   int
	}{
		{"empty", "", false, 0}, {"ascii", "61006200", false, 0},
		{"nul", "0000", true, 0}, {"odd", "61", true, 0},
		{"high", "00d8", true, 0}, {"low", "00dc", true, 0},
		{"nul-after-bound", "610062000000", true, 0},
		{"surrogate-after-bound", "6100620000d8", true, 0},
		{"pair", "3dd800de", false, 0},
		{"pair-at-clamp", "61003dd800de", false, 1},
		{"ascii-clamp", "610062006300", false, 1},
	} {
		raw, e := hex.DecodeString(tc.raw)
		if e != nil {
			t.Fatal(e)
		}
		for _, arm := range []bool{false, true} {
			var wire []byte
			var ids []uint64
			if arm {
				body := append([]byte{2, 33, byte(len(raw))}, raw...)
				wire = append([]byte{1, 1, 15}, body...)
				ids = []uint64{ir.TableWireId("choice"), ir.TableWireId("wide")}
			} else {
				wire = append([]byte{1, 1, 33, byte(len(raw))}, raw...)
				ids = []uint64{ir.TableWireId("text")}
			}
			wire = append(wire, 0)
			for _, id := range ids {
				wire = binary.LittleEndian.AppendUint64(wire, id)
			}
			wire = binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
			v := m.New(u.Tables["Root"])
			var report tabletext.Report
			_, e = tablewire.Decode(m, v, wire, &report)
			if e != nil || report.Malformed != tc.malformed || report.Clamped != tc.clamped {
				t.Fatalf("%s arm=%v neutral: %v %+v", tc.name, arm, e, report)
			}
			want := ""
			if !tc.malformed {
				b, e := tablewire.Encode(m, v)
				if e != nil {
					t.Fatal(e)
				}
				want = hex.EncodeToString(b)
			}
			fmt.Fprintf(&probe, `
wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
for result<-[T.load("Root",wire),T.load_retain("Root",wire,store,zero)] do
 r=elem(result,2)
 unless r.malformed==%t and r.clamped==%d,do: raise(%q<>" report "<>inspect(result))
 if %t do
  unless elem(result,0)==:error,do: raise("invalid wide text accepted")
 else
  unless elem(result,0)==:ok,do: raise("valid wide text rejected "<>inspect(result))
  {:ok,saved}=T.save("Root",elem(result,1))
  unless Base.encode16(saved,case: :lower)==%q,do: raise("wide clamp split or changed a code point")
 end
end
`, hex.EncodeToString(wire), tc.malformed, tc.clamped, fmt.Sprintf("%s arm=%v", tc.name, arm), tc.malformed, want)
		}
	}
	probe.WriteString("IO.puts(\"WIDE_TEXT_VALID_OK\")\n")
	if out := runCompleteUnitProbe(t, u, probe.String()); !strings.Contains(out, "WIDE_TEXT_VALID_OK") {
		t.Fatal(out)
	}
}

func TestBoundedBytesCountBeforePayloadElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir bounded bytes clamp count before requiring payload")
	u := unitFrom(t, `package widetext
 union Choice { wide bytes(2) }
 table Root { text bytes(2)
 choice Choice }
 `)
	m := tabletext.NewModel(u)
	var probe strings.Builder
	probe.WriteString("alias Widetext.Table,as: T\n")
	for _, tc := range []struct {
		name, raw string
		malformed bool
		clamped   int
	}{
		{"empty", "00", false, 0}, {"exact", "020102", false, 0},
		{"over-bound-prefix", "030102", false, 1},
		{"over-bound-full", "03010203", false, 1},
		{"over-bound-short", "0301", true, 1},
		{"within-bound-short", "0201", true, 0},
	} {
		raw, e := hex.DecodeString(tc.raw)
		if e != nil {
			t.Fatal(e)
		}
		for _, arm := range []bool{false, true} {
			var wire []byte
			var ids []uint64
			if arm {
				body := append([]byte{2, 14, byte(len(raw) + 1), 6}, raw...)
				wire = append([]byte{1, 1, 15}, body...)
				ids = []uint64{ir.TableWireId("choice"), ir.TableWireId("wide")}
			} else {
				wire = append([]byte{1, 1, 14, byte(len(raw) + 1), 6}, raw...)
				ids = []uint64{ir.TableWireId("text")}
			}
			wire = append(wire, 0)
			for _, id := range ids {
				wire = binary.LittleEndian.AppendUint64(wire, id)
			}
			wire = binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
			v := m.New(u.Tables["Root"])
			var report tabletext.Report
			_, e = tablewire.Decode(m, v, wire, &report)
			if e != nil || report.Malformed != tc.malformed || report.Clamped != tc.clamped {
				t.Fatalf("%s arm=%v neutral: %v %+v", tc.name, arm, e, report)
			}
			want := ""
			if !tc.malformed {
				b, e := tablewire.Encode(m, v)
				if e != nil {
					t.Fatal(e)
				}
				want = hex.EncodeToString(b)
			}
			fmt.Fprintf(&probe, `
wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
for result<-[T.load("Root",wire),T.load_retain("Root",wire,store,zero)] do
 r=elem(result,2)
 unless r.malformed==%t and r.clamped==%d,do: raise(%q<>" report "<>inspect(result))
 if %t do
  unless elem(result,0)==:error,do: raise("invalid bounded bytes accepted")
 else
  unless elem(result,0)==:ok,do: raise("valid bounded bytes rejected "<>inspect(result))
  {:ok,saved}=T.save("Root",elem(result,1))
  unless Base.encode16(saved,case: :lower)==%q,do: raise("bounded bytes prefix changed")
 end
end
`, hex.EncodeToString(wire), tc.malformed, tc.clamped, fmt.Sprintf("%s arm=%v", tc.name, arm), tc.malformed, want)
		}
	}
	probe.WriteString("IO.puts(\"BOUNDED_BYTES_OK\")\n")
	if out := runCompleteUnitProbe(t, u, probe.String()); !strings.Contains(out, "BOUNDED_BYTES_OK") {
		t.Fatal(out)
	}
}
