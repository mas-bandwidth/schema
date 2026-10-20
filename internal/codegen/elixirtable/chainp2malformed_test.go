package elixirtable

// TestChainP2SetsMalformedElixir pins docs/SPEC-TABLES.md §3.1 on the Elixir
// leg: a node-table field arriving under a kind other than 12 makes the whole
// numbering malformed, so every pointer reads null and one malformed event is
// counted. The mutant is the wire-fuzz seed p1_as_p2 (tblp2.Chain) at run seed
// 24845619678, pass entry #5: its id table names the reserved node-table id
// where the link field's id stood, and the field rides under kind 13. The
// reference reads the name, leaves the link null, counts one malformed and
// re-saves the same 26 bytes.
//
// The test feeds the mutant to the generated Elixir and holds the loaded
// verdict, every report field and the saved bytes to internal/tablewire.
// Functional (elixirBinary): run with SCHEMA_REQUIRE_CORPUS=1.

import (
	"encoding/hex"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// chainP2MalformedMutant is the wire-fuzz mutant: 66 bytes and four id-table
// entries, whose second entry is the reserved node id.
const chainP2MalformedMutant = "01010c05636861696e020d0d030407000000040c037469700000861b638ebaadbcc4ffffffffffffffffea0ce83094fde47cf3a4484419abd7560400000000000000"

func TestChainP2SetsMalformedElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := hex.DecodeString(chainP2MalformedMutant)
	if err != nil {
		t.Fatal(err)
	}

	u := loadUnit(t, filepath.Join(root, "test", "tables", "P2.schema"))
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Chain"))
	var refReport tabletext.Report
	ok, err := tablewire.Decode(m, inst, wire, &refReport)
	if err != nil {
		t.Fatalf("reference refused the mutant: %v", err)
	}
	if !ok {
		t.Fatal("reference did not keep a value for the mutant")
	}
	refSaved, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("reference encode: %v", err)
	}

	files := generateElixir(t, u)
	dir := t.TempDir()
	names := writeModuleDir(t, dir, files)
	mod := ir.GoExportName(u.Package) + ".Table"
	probe := `
wire = Base.decode16!(` + strconv.Quote(chainP2MalformedMutant) + `, case: :lower)
case ` + mod + `.load("Chain", wire) do
  {:ok, value, report} ->
    IO.puts("loaded=true")
    IO.puts("unknown=" <> Integer.to_string(report.unknown))
    IO.puts("kind_mismatch=" <> Integer.to_string(report.kind_mismatch))
    IO.puts("widened=" <> Integer.to_string(report.widened))
    IO.puts("clamped=" <> Integer.to_string(report.clamped))
    IO.puts("duplicate=" <> Integer.to_string(Map.get(report, :duplicate, 0)))
    IO.puts("malformed=" <> to_string(report.malformed))
    IO.puts("retained=" <> Integer.to_string(report.retained))
    IO.puts("retain_lost=" <> Integer.to_string(report.retain_lost))
    IO.puts("reason=" <> report.reason)
    {:ok, saved} = ` + mod + `.save("Chain", value)
    IO.puts("saved=" <> Base.encode16(saved, case: :lower))

  other ->
    IO.puts("loaded=false")
    IO.puts("other=" <> inspect(other))
end
`
	out := runProbe(t, elixirBin, dir, names, probe)
	got := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, found := strings.Cut(line, "="); found {
			got[k] = v
		}
	}
	if got["loaded"] != "true" {
		t.Fatalf("the generated Elixir did not keep a value for the mutant: %s", out)
	}

	want := map[string]string{
		"unknown":       strconv.Itoa(refReport.Unknown),
		"kind_mismatch": strconv.Itoa(refReport.KindMismatch),
		"widened":       strconv.Itoa(refReport.Widened),
		"clamped":       strconv.Itoa(refReport.Clamped),
		"duplicate":     strconv.Itoa(refReport.Duplicate),
		"malformed":     strconv.FormatBool(refReport.Malformed),
		"retained":      strconv.Itoa(refReport.Retained),
		"retain_lost":   strconv.Itoa(refReport.RetainLost),
		"reason":        chainP2MalformedReason(refReport),
		"saved":         hex.EncodeToString(refSaved),
	}
	for _, key := range []string{"unknown", "kind_mismatch", "widened", "clamped", "duplicate", "malformed", "retained", "retain_lost", "reason", "saved"} {
		if got[key] != want[key] {
			t.Errorf("report %s: elixir %s, oracle %s\n%s", key, got[key], want[key], out)
		}
	}
}

// chainP2MalformedReason is the reference read's one verdict word (§4): a read
// that finished over damage says wire_damaged, and a clean read says nothing.
func chainP2MalformedReason(r tabletext.Report) string {
	if r.Malformed {
		return "wire_damaged"
	}
	return ""
}
