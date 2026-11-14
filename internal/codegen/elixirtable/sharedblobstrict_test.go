package elixirtable

// A BLOB IS OWNED BY ONE SLOT (docs/SPEC-TABLES.md §2.5, §6.5). A wire in
// which one blob record is named from two slots is refused by every Elixir
// variable-form read: load/2, variable_load/2 and load_retain/4. No value is
// returned and nothing is retained. The unmodified map_docs wire is lawful and
// loads through all three reads and re-saves byte for byte.
//
// The Go engine is the oracle for each wire before the generated Elixir is
// asked. The test prints verdicts only, never a reason name.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func sharedBlobOracle(t *testing.T, u *ir.Unit, root string, wire []byte, wantRead bool) {
	t.Helper()
	m := tabletext.NewModel(u)
	var report tabletext.Report
	ok, err := tablewire.Decode(m, m.New(u.Tables[root]), wire, &report)
	if wantRead {
		if !ok || err != nil || report.Refused {
			t.Fatalf("oracle refused a lawful wire: ok=%v err=%v report=%+v", ok, err, report)
		}
		return
	}
	if ok || err == nil || !report.Refused || !tablewire.ReadRefusal(err) {
		t.Fatalf("oracle accepted a wire two slots name: ok=%v err=%v report=%+v", ok, err, report)
	}
}

// mapDocsShared re-points the second map entry's blob slot at the first
// entry's one blob record. The corpus wire's two map entries name blob nodes 2
// and 3, so the lawful wire differs from the shared one by one byte.
func mapDocsShared(t *testing.T, wire []byte) []byte {
	t.Helper()
	first := bytes.Index(wire, []byte{0x03, 0x11, 0x02, 0x00})
	second := bytes.Index(wire, []byte{0x03, 0x11, 0x03, 0x00})
	if first < 0 || second < 0 || first == second {
		t.Fatalf("map_docs no longer carries the two blob slots this row re-points")
	}
	out := append([]byte(nil), wire...)
	out[second+2] = out[first+2]
	return out
}

func sharedBlobProbe(mod, table, cleanHex, sharedHex string) string {
	var b strings.Builder
	b.WriteString(`
defmodule Probe do
  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end
zero = %{unknown: 0, widened: 0, retained: 0, retain_lost: 0, reason: "", missing_required: 0}
store = %{records: [], used: 0, cap: 4096, ids: [], id_cap: 256}
`)
	fmt.Fprintf(&b, `
shared = Base.decode16!(%q, case: :lower)
case %s.load(%q, shared) do
  {:error, _, _} -> :ok
  other -> Probe.check(false, "load accepted a shared blob: " <> inspect(other))
end
case %s.variable_load(%q, shared) do
  {:error, _, _} -> :ok
  other -> Probe.check(false, "variable_load accepted a shared blob: " <> inspect(other))
end
case %s.load_retain(%q, shared, store, zero) do
  {:error, _, _, rt} ->
    Probe.check(rt.records == [] and rt.used == 0 and rt.ids == [], "a shared-blob refusal retained something: " <> inspect(rt))
  other -> Probe.check(false, "load_retain accepted a shared blob: " <> inspect(other))
end
`, sharedHex, mod, table, mod, table, mod, table)
	if cleanHex != "" {
		fmt.Fprintf(&b, `
clean = Base.decode16!(%q, case: :lower)
{:ok, value, _} = %s.load(%q, clean)
{:ok, out} = %s.save(%q, value)
Probe.check(out == clean, "load/save does not reproduce map_docs")
{:ok, v2, _} = %s.variable_load(%q, clean)
{:ok, out2} = %s.save(%q, v2)
Probe.check(out2 == clean, "variable_load/save does not reproduce map_docs")
{:ok, v3, report3, rt3} = %s.load_retain(%q, clean, store, zero)
{:ok, out3, _, _} = %s.save_retain(%q, v3, rt3, report3)
Probe.check(out3 == clean, "load_retain/save_retain does not reproduce map_docs")
`, cleanHex, mod, table, mod, table, mod, table, mod, table, mod, table, mod, table)
	}
	b.WriteString(`
IO.puts("SHARED_BLOB_STRICT_OK")
`)
	return b.String()
}

func TestElixirEveryReadRefusesSharedBlob(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	catalog := loadUnit(t, filepath.Join(root, "tables", "blobs", "Assets.schema"))
	shared, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "blob_shared.bin"))
	if err != nil {
		t.Fatal(err)
	}
	sharedBlobOracle(t, catalog, "Catalog", shared, false)
	catalogMod := ir.GoExportName(catalog.Package) + ".Table"
	out := runCompleteUnitProbe(t, catalog, sharedBlobProbe(catalogMod, "Catalog", "", hex.EncodeToString(shared)))
	if !strings.Contains(out, "SHARED_BLOB_STRICT_OK") {
		t.Fatalf("the catalog shared blob did not refuse every read: %s", out)
	}

	docs := loadUnit(t, filepath.Join(root, "tables", "maps", "Docs.schema"))
	clean, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "map_docs.bin"))
	if err != nil {
		t.Fatal(err)
	}
	sharedDocs := mapDocsShared(t, clean)
	sharedBlobOracle(t, docs, "Docs", clean, true)
	sharedBlobOracle(t, docs, "Docs", sharedDocs, false)
	docsMod := ir.GoExportName(docs.Package) + ".Table"
	out = runCompleteUnitProbe(t, docs, sharedBlobProbe(docsMod, "Docs", hex.EncodeToString(clean), hex.EncodeToString(sharedDocs)))
	if !strings.Contains(out, "SHARED_BLOB_STRICT_OK") {
		t.Fatalf("the map_docs shared blob did not refuse every read: %s", out)
	}
}
