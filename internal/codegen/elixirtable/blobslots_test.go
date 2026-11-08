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

func TestBlobOwnershipUsesFinalSlotsElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir blob ownership counts final slots after repeated field replacement")
	u := loadUnit(t, "../../../tables/blobs/Assets.schema")
	m := tabletext.NewModel(u)
	read := func(name string) []byte {
		b, e := os.ReadFile("../../../testdata/wire/tables/" + name + ".bin")
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	replace := func(wire []byte, name string, target byte) []byte {
		n := int(binary.LittleEndian.Uint64(wire[len(wire)-8:]))
		start := len(wire) - 8 - n*8
		var ref byte
		for i := range n {
			if binary.LittleEndian.Uint64(wire[start+i*8:]) == ir.TableWireId(name) {
				ref = byte(i + 1)
			}
		}
		if ref == 0 {
			t.Fatalf("missing %s id", name)
		}
		at := bytes.Index(wire[:start], []byte{ref, 17})
		if at < 0 {
			t.Fatalf("missing %s field", name)
		}
		end := at + 3
		out := append([]byte(nil), wire[:end]...)
		out = append(out, ref, 17, target)
		return append(out, wire[end:]...)
	}
	shared := read("blob_shared")
	duplicate := read("fuzz-vectors/blob_field_replaced")
	cases := []struct {
		name        string
		wire        []byte
		shared      int
		finalShared bool
	}{
		{"original", read("blob_small"), 0, false},
		{"duplicate", duplicate, 1, false},
		{"triplicate", replace(duplicate, "thumb", 6), 2, false},
		{"genuine", shared, 1, true},
		{"same_shared_slot", replace(shared, "alias", 2), 2, true},
		{"clear_second", replace(shared, "alias", 0), 1, false},
		{"clear_first", replace(shared, "thumb", 0), 1, false},
		{"equal_distinct", read("blob_equal_bytes"), 0, false},
		{"drop_table_owner", replace(replace(read("blob_small"), "thumb", 3), "head", 0), 1, false},
	}
	var probe strings.Builder
	probe.WriteString("alias Blobdemo.Table,as: T\nProcess.put(:failures,[])\ncheck=fn yes,msg->unless yes,do: Process.put(:failures,[msg|Process.get(:failures)]) end\n")
	for _, c := range cases {
		if c.name == "drop_table_owner" && !bytes.Equal(c.wire, read("fuzz-vectors/blob_table_owner_replaced")) {
			t.Fatal("literal unreachable-owner vector changed")
		}
		value := m.New(u.Tables["Catalog"])
		var report tabletext.Report
		ok, e := tablewire.Decode(m, value, c.wire, &report)
		if e != nil || !ok || report.Refused {
			t.Fatalf("%s independent slot count: %v %v %+v", c.name, ok, e, report)
		}
		want, e := tablewire.Encode(m, value)
		if (e != nil) != c.finalShared {
			t.Fatalf("%s writer: %v", c.name, e)
		}
		fmt.Fprintf(&probe, `wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
for result<-[T.load("Catalog",wire),T.load_retain("Catalog",wire,store,zero)] do
 check.(elem(result,0)==:ok and elem(result,2).blob_shared==%d,%q<>inspect(result))
 value=elem(result,1)
 written=if tuple_size(result)==4,do: T.save_retain("Catalog",value,elem(result,3),zero),else: T.save("Catalog",value)
 if not %t do
  check.(elem(written,0)==:ok and Base.encode16(elem(written,1),case: :lower)==%q,%q<>inspect(written))
 else
  check.(elem(written,0)==:error and elem(written,1)==:blob_shared,%q<>inspect(written))
 end
end
`, hex.EncodeToString(c.wire), c.shared, c.name+" load ", c.finalShared, hex.EncodeToString(want), c.name+" save ", c.name+" refusal ")
	}
	probe.WriteString("case Process.get(:failures) do\n []->IO.puts(\"FINAL_BLOB_SLOTS_OK\")\n errors->raise(Enum.join(Enum.reverse(errors),\"\\n\"))\nend\n")
	out := runCompleteUnitProbe(t, u, probe.String())
	if !strings.Contains(out, "FINAL_BLOB_SLOTS_OK") {
		t.Fatalf("native final slots: %s", out)
	}
}

func TestBlobOwnershipUsesLiveGuardsElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir blob writer ownership follows final nested guards")
	u := unitFrom(t, `package blobguards
 table Catalog {
 name string(32)
 outer bool
 if outer {
  inner bool
  if inner { thumb *bytes }
 }
 alias *bytes
 }`)
	m := tabletext.NewModel(u)
	wire, e := os.ReadFile("../../../testdata/wire/tables/blob_shared.bin")
	if e != nil {
		t.Fatal(e)
	}
	n := int(binary.LittleEndian.Uint64(wire[len(wire)-8:]))
	start := len(wire) - 8 - 8*n
	var probe strings.Builder
	probe.WriteString("alias Blobguards.Table,as: T\n")
	for _, bits := range [][2]byte{{0, 0}, {0, 1}, {1, 0}, {1, 1}} {
		b := []byte{1, byte(n + 1), 1, bits[0], byte(n + 2), 1, bits[1]}
		b = append(b, wire[1:start]...)
		b = append(b, wire[start:len(wire)-8]...)
		b = binary.LittleEndian.AppendUint64(b, ir.TableWireId("outer"))
		b = binary.LittleEndian.AppendUint64(b, ir.TableWireId("inner"))
		b = binary.LittleEndian.AppendUint64(b, uint64(n+2))
		value := m.New(u.Tables["Catalog"])
		var report tabletext.Report
		ok, e := tablewire.Decode(m, value, b, &report)
		if e != nil || !ok || report.Refused {
			t.Fatalf("guard %v oracle: %v %v %+v", bits, ok, e, report)
		}
		shared := bits[0] == 1 && bits[1] == 1
		want, e := tablewire.Encode(m, value)
		if (e != nil) != shared {
			t.Fatalf("guard %v writer %v", bits, e)
		}
		fmt.Fprintf(&probe, `wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
for result<-[T.load("Catalog",wire),T.load_retain("Catalog",wire,store,zero)] do
 unless elem(result,0)==:ok and elem(result,2).blob_shared==1,do: raise("guard load "<>inspect(result))
 value=elem(result,1)
 written=if tuple_size(result)==4,do: T.save_retain("Catalog",value,elem(result,3),zero),else: T.save("Catalog",value)
 if %t do
  unless elem(written,0)==:error and elem(written,1)==:blob_shared,do: raise("guard shared "<>inspect(written))
 else
  unless elem(written,0)==:ok and Base.encode16(elem(written,1),case: :lower)==%q,do: raise("guard write "<>inspect(written))
 end
end
`, hex.EncodeToString(b), shared, hex.EncodeToString(want))
	}
	probe.WriteString("IO.puts(\"LIVE_BLOB_GUARDS_OK\")\n")
	out := runCompleteUnitProbe(t, u, probe.String())
	if !strings.Contains(out, "LIVE_BLOB_GUARDS_OK") {
		t.Fatal(out)
	}
}
