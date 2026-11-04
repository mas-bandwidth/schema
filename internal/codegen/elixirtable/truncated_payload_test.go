package elixirtable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestTruncatedFieldPayloadElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir nested scalar framing must refuse without decoding a synthetic empty payload")
	u := unitFrom(t, `package truncated
 table Child {
 flag bool
 small float32
 large float64
 number int32
 wide uint128
 clock timestamp
 }
 table Root {
 child Child
 children []Child
 }`)
	m := tabletext.NewModel(u)
	var probe strings.Builder
	probe.WriteString("alias Truncated.Table,as: T\n")
	rows := []struct {
		name  string
		kind  byte
		width int
	}{{"flag", 1, 1}, {"small", 10, 4}, {"large", 11, 8}, {"number", 4, 4}, {"wide", 19, 16}, {"clock", 36, 8}, {"large", 10, 4}, {"unknown", 10, 4}, {"flag", 10, 4}}
	for _, row := range rows {
		for _, array := range []bool{false, true} {
			for n := 0; n < row.width; n++ {
				inner := append([]byte{2, row.kind}, make([]byte, n)...)
				field, kind, payload := "child", byte(13), inner
				if array {
					field, kind = "children", 14
					payload = binary.AppendUvarint([]byte{13, 1}, uint64(len(inner)))
					payload = append(payload, inner...)
				}
				wire := binary.AppendUvarint([]byte{1, 1, kind}, uint64(len(payload)))
				wire = append(wire, payload...)
				wire = append(wire, 0)
				wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId(field))
				wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId(row.name))
				wire = binary.LittleEndian.AppendUint64(wire, 2)
				var report tabletext.Report
				ok, e := tablewire.Decode(m, m.New(u.Tables["Root"]), wire, &report)
				if ok || !tablewire.ReadRefusal(e) {
					t.Fatalf("%s/%d oracle: %v %+v %v", row.name, n, ok, report, e)
				}
				fmt.Fprintf(&probe, `wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
for result<-[T.load("Root",wire),T.load_retain("Root",wire,store,zero)] do
 unless elem(result,0)==:error and elem(result,1)==:malformed and elem(result,2).malformed,do: raise(%q<>inspect(result))
end
`, hex.EncodeToString(wire), fmt.Sprintf("truncated %s/%s/kind%d/%d ", field, row.name, row.kind, n))
			}
		}
	}
	probe.WriteString("IO.puts(\"TRUNCATED_FIELDS_OK\")\n")
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe.String())
	if e != nil || !strings.Contains(out, "TRUNCATED_FIELDS_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native truncated field: %v\n%s", e, out)
	}
}

func TestPinnedNestedFloatFramingElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir nested float framing replays the permanent enumerated mutant")
	paths, e := filepath.Glob("../../../tables/examples/*.schema")
	if e != nil || len(paths) == 0 {
		t.Fatalf("examples schema: %v", e)
	}
	u := loadVariableRetainFixture(t, paths...)
	m := tabletext.NewModel(u)
	wire, e := os.ReadFile("../../../testdata/wire/tables/fuzz-vectors/arm_length_empty_float.bin")
	if e != nil {
		t.Fatal(e)
	}
	var report tabletext.Report
	ok, e := tablewire.Decode(m, m.New(u.Tables["RootConfig"]), wire, &report)
	if ok || !tablewire.ReadRefusal(e) {
		t.Fatalf("pinned oracle: %v %+v %v", ok, report, e)
	}
	probe := fmt.Sprintf(`alias Tabledemo.Table,as: T
wire=Base.decode16!(%q,case: :lower)
store=%%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
for result<-[T.load("RootConfig",wire),T.load_retain("RootConfig",wire,store,zero)] do
 unless elem(result,0)==:error and elem(result,1)==:malformed and elem(result,2).malformed,do: raise("pinned nested float "<>inspect(result))
end
IO.puts("PINNED_NESTED_FLOAT_OK")
`, hex.EncodeToString(wire))
	enumWire, e := os.ReadFile("../../../testdata/wire/tables/fuzz-vectors/enum_array_foreign_variant.bin")
	if e != nil {
		t.Fatal(e)
	}
	var enumReport tabletext.Report
	enumStore := tablewire.Retain{Capacity: 4096, IdCapacity: 256}
	enumOK, e := tablewire.DecodeRetain(m, m.New(u.Tables["RootConfig"]), enumWire, &enumStore, &enumReport)
	if e != nil || !enumOK || enumReport.Refused || enumReport.Unknown != 1 || enumReport.RetainLost != 1 || enumReport.Retained != 0 {
		t.Fatalf("pinned enum loss oracle: %v %+v %v", enumOK, enumReport, e)
	}
	probe += fmt.Sprintf(`
wire=Base.decode16!(%q,case: :lower)
{:ok,_,plain}=T.load("RootConfig",wire)
unless plain.unknown==1 and plain.retain_lost==0,do: raise("plain enum pin "<>inspect(plain))
{:ok,_,kept,_}=T.load_retain("RootConfig",wire,store,zero)
unless kept.unknown==1 and kept.retain_lost==1 and kept.retained==0,do: raise("retained enum pin "<>inspect(kept))
`, hex.EncodeToString(enumWire))

	out := runCompleteUnitProbe(t, u, probe)
	if !strings.Contains(out, "PINNED_NESTED_FLOAT_OK") {
		t.Fatalf("native pinned payloads: %s", out)
	}
}

func runCompleteUnitProbe(t *testing.T, u *ir.Unit, probe string) string {
	t.Helper()

	// Compile the complete unit together: its fixed structs have cross-file
	// dependencies, so alphabetic serial requires are not a valid build.
	dir := t.TempDir()
	names := writeModuleDir(t, dir, generateElixir(t, u))
	ebin := filepath.Join(dir, "ebin")
	if e := os.Mkdir(ebin, 0o700); e != nil {
		t.Fatal(e)
	}
	strict, e := filepath.Abs("../../../make/elixir-strict.sh")
	if e != nil {
		t.Fatal(e)
	}
	elixir := elixirBinary(t)
	args := append([]string{strict, filepath.Join(filepath.Dir(elixir), "elixirc"), "--warnings-as-errors", "-o", ebin}, names...)
	cmd := exec.Command("sh", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, e := cmd.CombinedOutput(); e != nil || strings.Contains(string(out), "warning:") {
		t.Fatalf("generated pinned unit must compile without warnings: %v\n%s", e, out)
	}
	cmd = exec.Command(elixir, "-pa", ebin, "-e", probe)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	raw, e := cmd.CombinedOutput()
	out := string(raw)
	if e != nil || strings.Contains(out, "warning:") {
		t.Fatalf("native complete unit: %v\n%s", e, out)
	}
	return out
}
