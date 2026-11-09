package elixirtable

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func regionGoProbe(t *testing.T, u *ir.Unit, statements string) string {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	table, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, table)
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["region_test.go"] = []byte("package probe\nimport(\"encoding/hex\";\"testing\")\nfunc TestRegion(t *testing.T){\n" + statements + "\n}")
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-timeout=0", "-count=1", "-v", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native Go region: %v\n%s", err, out)
	}
	return string(out)
}

func TestRegionClosureElixir(t *testing.T) {
	slowtest.Gate(t, "native map/keyed/union region closure")
	dir := "../../../testdata/conformance/feature-witnesses/region-closure"
	source, err := os.ReadFile(filepath.Join(dir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Name, Root, Input, Wire string
		Region                  int64
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 34 {
		t.Fatalf("region corpus has %d cases, want 34", len(rows))
	}
	u := unitFrom(t, string(source))
	m := tabletext.NewModel(u)
	var goProbe, native strings.Builder
	native.WriteString("alias Probe.Table, as: T\nfailures = for {label, name, hex, want} <- [\n")
	for _, row := range rows {
		wire, err := hex.DecodeString(row.Wire)
		if err != nil {
			t.Fatal(err)
		}
		region, whole, refused := tablewire.FileRegionMeasure(u, u.Tables[row.Root], wire)
		if !whole || refused {
			region = -1
		}
		if region != row.Region {
			t.Errorf("%s neutral=%d want=%d", row.Name, region, row.Region)
		}
		if row.Input != "" {
			v := m.New(u.Tables[row.Root])
			var report tabletext.Report
			if !m.Read(v, []byte(row.Input), &report) || !report.Silent() {
				t.Fatalf("%s JSON: %+v", row.Name, report)
			}
			encoded, err := tablewire.Encode(m, v)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(encoded, wire) {
				t.Fatalf("%s canonical wire differs from literal pin", row.Name)
			}
			cooked, err := tablecook.Cook(m, v, tablecook.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if int64(len(cooked)-64) != row.Region {
				t.Fatalf("%s cooked region=%d want=%d", row.Name, len(cooked)-64, row.Region)
			}
		}
		measure := row.Region
		switch row.Name {
		case "map_truncated_entry", "keyed_unknown_key", "keyed_truncated_entry", "union_unknown_arm", "union_truncated_arm":
			// The strict Go measure refuses these whole; the Elixir
			// extent measure still answers the framed region.
			measure = -1
		}
		fmt.Fprintf(&goProbe, "{b,_:=hex.DecodeString(%q); got:=%sLoadMeasure(b); if got!=%d {t.Errorf(%q,got)}}\n", row.Wire, row.Root, measure, row.Name+" Go region=%d")
		fmt.Fprintf(&native, "{%q,%q,%q,%d},\n", row.Name, row.Root, row.Wire, row.Region)
	}
	native.WriteString(`] do
 wire = Base.decode16!(hex, case: :lower)
 got = T.load_measure(name, wire)
 mismatch = got != want or !T.carried?(name)
 if mismatch, do: IO.puts("REGION_FAILURE " <> inspect({label, name, got, want}))
 mismatch
end
if Enum.any?(failures), do: raise("region closure mismatches")
IO.puts("REGION_CLOSURE_OK")
`)
	t.Log(regionGoProbe(t, u, goProbe.String()))
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), native.String())
	if err != nil || !strings.Contains(out, "REGION_CLOSURE_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native region closure: %v\n%s", err, out)
	}
}
