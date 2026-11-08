package elixirtable

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestRetainedReplacementCorpusElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir retained replacement follows the independent C++ shared corpus")
	base := "../../../testdata/conformance/feature-witnesses/retained-replacement"
	for _, shape := range []string{"table", "union", "fixed_table", "fixed_union", "keyed_table"} {
		t.Run(shape, func(t *testing.T) {
			u := loadUnit(t, filepath.Join(base, shape, "Probe.schema"))
			m := tabletext.NewModel(u)
			raw, e := os.ReadFile(filepath.Join(base, shape, "cases.json"))
			if e != nil {
				t.Fatal(e)
			}
			var rows []struct {
				Name, Root, Wire, Saved string
				Unknown                 int
				KindMismatch            int `json:"kind_mismatch"`
				Retained                int
				LoadLost                int `json:"load_lost"`
				SaveLost                int `json:"save_lost"`
			}
			if e = json.Unmarshal(raw, &rows); e != nil {
				t.Fatal(e)
			}
			if len(rows) != 3 {
				t.Fatalf("shared corpus has %d rows, want 3", len(rows))
			}
			var probe strings.Builder
			probe.WriteString("alias Probe.Table,as: T\n")
			for _, row := range rows {
				wire, e := hex.DecodeString(row.Wire)
				if e != nil {
					t.Fatal(e)
				}
				value := m.New(u.Tables[row.Root])
				store := tablewire.Retain{Capacity: 4096, IdCapacity: 256}
				var read, save tabletext.Report
				ok, e := tablewire.DecodeRetain(m, value, wire, &store, &read)
				if e != nil || !ok || read.Refused || read.Unknown != row.Unknown || read.Retained != row.Retained || read.RetainLost != row.LoadLost {
					t.Fatalf("%s independent load: %v %v %+v", row.Name, ok, e, read)
				}
				out, e := tablewire.EncodeRetain(m, value, &store, &save)
				if e != nil || hex.EncodeToString(out) != row.Saved || save.RetainLost != row.SaveLost {
					t.Fatalf("%s independent save: %x %v %+v", row.Name, out, e, save)
				}
				text, e := m.Write(value)
				if e != nil {
					t.Fatal(e)
				}
				plain, e := tablewire.Encode(m, value)
				if e != nil {
					t.Fatal(e)
				}
				fmt.Fprintf(&probe, `wire=Base.decode16!(%q,case: :lower)
{:ok,plain,_}=T.load(%q,wire)
{:ok,text}=T.to_json(%q,plain)
unless text==%q,do: raise(%q<>" plain value "<>text)
{:ok,bytes}=T.save(%q,plain)
unless Base.encode16(bytes,case: :lower)==%q,do: raise(%q<>" plain save "<>inspect(bytes))
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
{:ok,value,report,store}=T.load_retain(%q,wire,store,zero)
unless true and report.unknown==%d and report.kind_mismatch==%d and report.retained==%d and report.retain_lost==%d,do: raise(%q<>" load "<>inspect(report))
{:ok,text}=T.to_json(%q,value)
unless text==%q,do: raise(%q<>" value "<>text)
{:ok,bytes,report,_}=T.save_retain(%q,value,store,zero)
unless Base.encode16(bytes,case: :lower)==%q and report.retain_lost==%d,do: raise(%q<>" save "<>inspect({Base.encode16(bytes,case: :lower),report}))
`, row.Wire, row.Root, row.Root, string(text)+"\n", row.Name, row.Root, hex.EncodeToString(plain), row.Name, row.Root, row.Unknown, row.KindMismatch, row.Retained, row.LoadLost, row.Name, row.Root, string(text)+"\n", row.Name, row.Root, row.Saved, row.SaveLost, row.Name)
			}
			probe.WriteString("IO.puts(\"RETAINED_REPLACEMENT_OK\")\n")
			out := runCompleteUnitProbe(t, u, probe.String())
			if !strings.Contains(out, "RETAINED_REPLACEMENT_OK") {
				t.Fatal(out)
			}
		})
	}
}
