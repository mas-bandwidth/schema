package elixirtable

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestPointerTargetKindsElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir root closure and exact pointer target ids in plain and retaining reads")
	dir := "../../../testdata/conformance/feature-witnesses/pointer-targets"
	source, e := os.ReadFile(filepath.Join(dir, "Probe.schema"))
	if e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(filepath.Join(dir, "cases.json"))
	if e != nil {
		t.Fatal(e)
	}
	var rows []struct {
		Name, Writer, Reader string
		Input, Expected      json.RawMessage
		KindMismatch         int `json:"kind_mismatch"`
		Unknown              int
	}
	if e = json.Unmarshal(data, &rows); e != nil {
		t.Fatal(e)
	}
	if len(rows) != 10 {
		t.Fatalf("pointer target corpus has %d cases, want 10", len(rows))
	}
	u := unitFrom(t, string(source))
	m := tabletext.NewModel(u)
	var p strings.Builder
	p.WriteString(`alias Probe.Table,as: T
store=%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
`)
	for _, row := range rows {
		v := m.New(u.Tables[row.Writer])
		var r tabletext.Report
		if !m.Read(v, row.Input, &r) || !r.Silent() {
			t.Fatalf("%s writer: %+v", row.Name, r)
		}
		wire, e := tablewire.Encode(m, v)
		if e != nil {
			t.Fatal(e)
		}
		dest := m.New(u.Tables[row.Reader])
		r = tabletext.Report{}
		ok, e := tablewire.Decode(m, dest, wire, &r)
		if !ok || e != nil || int(r.Unknown) != row.Unknown || r.Refused {
			t.Fatalf("%s SPEC3.1 oracle: %+v %v", row.Name, r, e)
		}
		canonical, e := m.Write(dest)
		if e != nil {
			t.Fatal(e)
		}
		var got, want any
		if e = json.Unmarshal(canonical, &got); e != nil {
			t.Fatal(e)
		}
		if e = json.Unmarshal(row.Expected, &want); e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s semantic oracle: %s != %s", row.Name, canonical, row.Expected)
		}
		fmt.Fprintf(&p, `wire=Base.decode16!(%q,case: :lower)
{:ok,original,_}=T.from_json(%q,%q)
{:ok,^wire}=T.save(%q,original)
for _<-1..2 do
 for result<-[T.load(%q,wire),T.load_retain(%q,wire,store,zero)] do
  v=elem(result,1);r=elem(result,2)
  unless elem(result,0)==:ok and r.kind_mismatch==%d and r.unknown==%d and r.blob_shared==0 and true,do: raise(%q<>" "<>inspect(result))
  {:ok,text}=T.to_json(%q,v)
  unless text==%q,do: raise(%q<>" value "<>text)
 end
end
`, hex.EncodeToString(wire), row.Writer, string(row.Input), row.Writer, row.Reader, row.Reader, row.KindMismatch, row.Unknown, row.Name, row.Reader, string(canonical)+"\n", row.Name)
	}
	p.WriteString("IO.puts(\"POINTER_TARGET_KINDS_OK\")\n")
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), p.String())
	if e != nil || !strings.Contains(out, "POINTER_TARGET_KINDS_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native exact pointer types: %v\n%s", e, out)
	}
}

func TestNodeTableWrongKindElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir reserved node-table kind rejects the pinned enumerated mutant")
	source, e := os.ReadFile("../../../testdata/conformance/feature-witnesses/full/Features.schema")
	if e != nil {
		t.Fatal(e)
	}
	wire, e := os.ReadFile("../../../testdata/wire/tables/fuzz-vectors/node_table_wrong_kind.bin")
	if e != nil {
		t.Fatal(e)
	}
	u := unitFrom(t, string(source))
	m := tabletext.NewModel(u)
	v := m.New(u.Tables["ArrayRoot"])
	var r tabletext.Report
	ok, e := tablewire.Decode(m, v, wire, &r)
	if ok || !tablewire.ReadRefusal(e) {
		t.Fatalf("reserved node kind oracle: %v %+v %v", ok, r, e)
	}
	p := fmt.Sprintf(`alias Featurewitnesses.Table,as: T
wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
for result<-[T.load("ArrayRoot",wire),T.load_retain("ArrayRoot",wire,store,zero)] do
 unless elem(result,0)==:error and elem(result,1)==:malformed and elem(result,2).malformed,do: raise("reserved node kind "<>inspect(result))
end
IO.puts("NODE_TABLE_KIND_OK")
`, hex.EncodeToString(wire))
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), p)
	if e != nil || !strings.Contains(out, "NODE_TABLE_KIND_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("reserved node kind: %v\n%s", e, out)
	}
}
