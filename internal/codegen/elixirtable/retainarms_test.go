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

func TestRetainedContainerUnionArmsElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir container union arms retain their nested body records")
	u := unitFrom(t, `package armretain
 enum Mode { A, B }
 type Inner { value int32 }
 union Nested {
 alpha Inner
 }
 union Pick {
 rows [..2]Inner
 variants [..2]Nested
 modes [..2]Mode
 nested Nested
 }
 table Root { choice Pick }`)
	m := tabletext.NewModel(u)
	inner := binary.LittleEndian.AppendUint32([]byte{3, byte(ir.TableKindI32)}, 7)
	inner = append(inner, 0)
	nestedInner := append([]byte(nil), inner...)
	nestedInner[0] = 4
	array := func(kind byte, p []byte) []byte { return append([]byte{kind, 1}, p...) }
	arm := func(ref, kind byte, p []byte) []byte { return append([]byte{ref, kind, byte(len(p))}, p...) }
	for _, tc := range []struct {
		name           string
		kind           byte
		payload        []byte
		retained, lost int
		ids            []string
	}{
		{"rows", 14, array(13, append([]byte{byte(len(inner))}, inner...)), 1, 0, []string{"choice", "rows", "future"}},
		{"variants", 14, array(15, arm(3, 13, nestedInner)), 1, 0, []string{"choice", "variants", "alpha", "future"}},
		{"modes", 14, array(30, []byte{3}), 0, 1, []string{"choice", "modes", "future"}},
		{"nested", 15, arm(3, 13, nestedInner), 1, 0, []string{"choice", "nested", "alpha", "future"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := append([]byte{1, 1, 15}, arm(2, tc.kind, tc.payload)...)
			wire = append(wire, 0)
			for _, name := range tc.ids {
				wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId(name))
			}
			wire = binary.LittleEndian.AppendUint64(wire, uint64(len(tc.ids)))
			v := m.New(u.Tables["Root"])
			retain := tablewire.Retain{Capacity: 4096, IdCapacity: 256}
			var load, save tabletext.Report
			ok, e := tablewire.DecodeRetain(m, v, wire, &retain, &load)
			if e != nil || !ok || load.Malformed || load.Unknown != 1 || int(load.Retained) != tc.retained || int(load.RetainLost) != tc.lost {
				t.Fatalf("oracle load: %v %v %+v", ok, e, load)
			}
			want, e := tablewire.EncodeRetain(m, v, &retain, &save)
			if e != nil || save.RetainLost != 0 {
				t.Fatalf("oracle save: %v %+v", e, save)
			}
			if tc.retained == 1 && !bytes.Equal(wire, want) {
				t.Fatalf("literal body pin changed: %x != %x", wire, want)
			}
			probe := fmt.Sprintf(`alias Armretain.Table,as: T
wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
{:ok,value,report,store}=T.load_retain("Root",wire,store,zero)
unless report.unknown==1 and report.retained==%d and report.retain_lost==%d,do: raise("load "<>inspect(report))
{:ok,saved,report,_}=T.save_retain("Root",value,store,zero)
unless Base.encode16(saved,case: :lower)==%q and report.retain_lost==0,do: raise("save "<>inspect({saved,report}))
{:ok,_,edited,_}=T.save_retain("Root",%%{"choice"=>nil},store,zero)
unless edited.retain_lost==%d,do: raise("removed arm "<>inspect(edited))
IO.puts("RETAINED_CONTAINER_ARM_OK")
`, hex.EncodeToString(wire), tc.retained, tc.lost, hex.EncodeToString(want), tc.retained)
			out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe)
			if e != nil || !strings.Contains(out, "RETAINED_CONTAINER_ARM_OK") || strings.Contains(out, "warning:") {
				t.Fatalf("native retained arm: %v\n%s", e, out)
			}
		})
	}
}

func TestPinnedNestedUnionRetainElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir nested union retention replays the permanent corpus vector")
	u := loadVariableRetainFixture(t, "../../../tables/messages/Messages.schema")
	m := tabletext.NewModel(u)
	wire, e := os.ReadFile("../../../testdata/wire/tables/fuzz-vectors/retain_nested_union_arm.bin")
	if e != nil {
		t.Fatal(e)
	}
	v := m.New(u.Tables["ToolMessage"])
	store := tablewire.Retain{Capacity: 4096, IdCapacity: 256}
	var load, save tabletext.Report
	ok, e := tablewire.DecodeRetain(m, v, wire, &store, &load)
	if e != nil || !ok || load.Malformed || load.Unknown != 1 || load.Retained != 1 || load.RetainLost != 0 {
		t.Fatalf("pinned nested union: %v %v %+v", ok, e, load)
	}
	want, e := tablewire.EncodeRetain(m, v, &store, &save)
	if e != nil || save.RetainLost != 0 {
		t.Fatalf("pinned nested save: %v %+v", e, save)
	}
	probe := fmt.Sprintf(`alias Messagedemo.Table,as: T
wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
{:ok,value,report,store}=T.load_retain("ToolMessage",wire,store,zero)
unless report.unknown==1 and report.retained==1 and report.retain_lost==0,do: raise("nested union load "<>inspect(report))
{:ok,bytes,report,_}=T.save_retain("ToolMessage",value,store,zero)
unless Base.encode16(bytes,case: :lower)==%q and report.retain_lost==0,do: raise("nested union save "<>inspect({bytes,report}))
IO.puts("PINNED_NESTED_UNION_OK")
`, hex.EncodeToString(wire), hex.EncodeToString(want))
	out := runCompleteUnitProbe(t, u, probe)
	if !strings.Contains(out, "PINNED_NESTED_UNION_OK") {
		t.Fatalf("native pinned nested union: %s", out)
	}
}
