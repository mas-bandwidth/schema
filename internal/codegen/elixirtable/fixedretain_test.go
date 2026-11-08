package elixirtable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRootRetainRefusalElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir fixed-root retention refusals and variable-root controls")
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprintf("mixed_%t", mixed), func(t *testing.T) {
			source := "package fixedretain\nfixed table Stamp { value int32 }\n"
			if mixed {
				source += "table Plain { value int32 }\ntable Nested { stamp Stamp }\ntable Graph { child *Stamp }\n"
			}
			u := unitFrom(t, source)
			if !mixed {
				generated := string(generateElixir(t, u)[VariableTableModule+".ex"])
				for _, machinery := range []string{"@known_ids", "defp decode_directory_retain", "defp save_retain_internal", "defp retain_record"} {
					if strings.Contains(generated, machinery) {
						t.Fatalf("all-fixed unit emitted private retention machinery %q", machinery)
					}
				}
			}
			m := tabletext.NewModel(u)
			stamp := m.New(u.Tables["Stamp"])
			var parsed tabletext.Report
			if !m.Read(stamp, []byte(`{"value":17}`), &parsed) {
				t.Fatal(parsed)
			}
			wire, err := tablewire.Encode(m, stamp)
			if err != nil {
				t.Fatal(err)
			}
			var probe strings.Builder
			fmt.Fprintf(&probe, `alias Fixedretain.Table,as: T
wire=Base.decode16!(%q,case: :lower)
store=%%{records: [{[],99,4,<<1,0,0,0>>}],used: 13,cap: 4096,ids: [99],id_cap: 256}
report=%%{unknown: 3,kind_mismatch: 5,widened: 7,clamped: 11,malformed: false,retained: 13,retain_lost: 17,reason: "preserved"}
expected={:error,:fixed_root,report,store}
for data<-[wire,<<>>,<<255>>] do
 result=T.load_retain("Stamp",data,store,report)
 unless result==expected,do: raise("fixed load must refuse before inspecting input or resetting stores: "<>inspect(result))
end
unless T.measure_retain("Stamp",%%{"value"=>17},store)=={:error,:fixed_root},do: raise("fixed measure must refuse by name")
unless T.save_retain("Stamp",%%{"value"=>17},store,report)==expected,do: raise("fixed save must refuse by name without changing report or stores")
{:ok,value,_}=T.load("Stamp",wire)
{:ok,^wire}=T.save("Stamp",value)
`, hex.EncodeToString(wire))
			if mixed {
				for _, tc := range []struct{ root, input string }{
					{"Plain", `{"value":17}`},
					{"Nested", `{"stamp":{"value":17}}`},
					{"Graph", `{"child":{"value":17}}`},
				} {
					value := m.New(u.Tables[tc.root])
					var report tabletext.Report
					if !m.Read(value, []byte(tc.input), &report) {
						t.Fatal(report)
					}
					want, err := tablewire.Encode(m, value)
					if err != nil {
						t.Fatal(err)
					}
					fmt.Fprintf(&probe, `wire=Base.decode16!(%q,case: :lower)
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
{:ok,value,loaded,kept}=T.load_retain(%q,wire,store,zero)
unless kept.records==[] and kept.used==0 and kept.ids==[],do: raise("variable load must reset the prior stores")
unless T.measure_retain(%q,value,kept)==byte_size(wire),do: raise("variable retain measure changed")
{:ok,^wire,_,_}=T.save_retain(%q,value,kept,loaded)
`, hex.EncodeToString(want), tc.root, tc.root, tc.root)
				}
			}
			probe.WriteString("IO.puts(\"FIXED_ROOT_RETAIN_REFUSAL_OK\")\n")
			out := runCompleteUnitProbe(t, u, probe.String())
			if !strings.Contains(out, "FIXED_ROOT_RETAIN_REFUSAL_OK") {
				t.Fatal(out)
			}
		})
	}
}

// The published form-1 input pins also exercise fields under a variable root.
// Reparse an ordinary-table counterpart rather than asking a fixed root to
// retain (§6.6). Keep every field, arm, bound and wire byte; converting the
// whole fixture unit avoids a fixed parent holding a newly variable child.
func loadVariableRetainFixture(t *testing.T, paths ...string) *ir.Unit {
	t.Helper()
	declarations := regexp.MustCompile(`(?m)^fixed table `)
	dir := t.TempDir()
	var copies []string
	changed := 0
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		changed += len(declarations.FindAllIndex(data, -1))
		data = declarations.ReplaceAll(data, []byte("table "))
		copy := filepath.Join(dir, filepath.Base(path))
		if err := os.WriteFile(copy, data, 0600); err != nil {
			t.Fatal(err)
		}
		copies = append(copies, copy)
	}
	if changed == 0 {
		t.Fatal("retaining counterpart fixture contained no fixed table declarations")
	}
	u := loadUnit(t, copies...)
	for name, st := range u.Tables {
		if st.FixedDeclared {
			t.Fatalf("retaining fixture root %s is still fixed", name)
		}
	}
	return u
}

func TestFixedMessageRootRetainRefusalElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir published fixed ToolMessage keeps plain wire behavior and refuses retention")
	u := loadUnit(t, "../../../tables/messages/Messages.schema")
	wire, err := os.ReadFile("../../../testdata/wire/tables/fuzz-vectors/union_array_element_kind.bin")
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	value := m.New(u.Tables["ToolMessage"])
	var report tabletext.Report
	ok, err := tablewire.Decode(m, value, wire, &report)
	if err != nil || !ok || report.Refused {
		t.Fatalf("fixed pin: %v %v %+v", ok, err, report)
	}
	want, err := tablewire.Encode(m, value)
	if err != nil || len(want) != 82 {
		t.Fatalf("fixed writer pin: %d %v", len(want), err)
	}
	probe := fmt.Sprintf(`alias Messagedemo.Table,as: T
wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
{:ok,value,report}=T.load("ToolMessage",wire)
unless report.kind_mismatch==1 and not report.malformed,do: raise("fixed plain report")
{:ok,written}=T.save("ToolMessage",value)
unless Base.encode16(written,case: :lower)==%q,do: raise("fixed plain canonical wire")
{:error,:fixed_root,^zero,^store}=T.load_retain("ToolMessage",wire,store,zero)
{:error,:fixed_root}=T.measure_retain("ToolMessage",value,store)
{:error,:fixed_root,^zero,^store}=T.save_retain("ToolMessage",value,store,zero)
IO.puts("FIXED_MESSAGE_RETAIN_REFUSAL_OK")
`, hex.EncodeToString(wire), hex.EncodeToString(want))
	out := runCompleteUnitProbe(t, u, probe)
	if !strings.Contains(out, "FIXED_MESSAGE_RETAIN_REFUSAL_OK") {
		t.Fatal(out)
	}
}
