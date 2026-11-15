package elixirtable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestElixirRetainKeepsDepth128 holds docs/SPEC-TABLES.md §4's one nesting
// bound and §6.6's resolving walk at depth 128. A sound wire whose only field
// is unknown keeps that field through the retaining load and writes it back
// byte for byte, at every depth the plain read accepts, and a wire past the
// bound is refused by the retaining load too.
func TestElixirRetainKeepsDepth128(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(root, "tables", "pointers", "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("pointer schema paths: %v (%d files)", err, len(paths))
	}
	u := loadUnit(t, paths...)
	model := tabletext.NewModel(u)
	scene := model.Lookup("Scene")
	if scene == nil {
		t.Fatal("graphdemo declares no Scene")
	}

	framing := filepath.Join(root, "testdata", "wire", "tables", "framing")
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(framing, name+".bin"))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return data
	}

	reads := []string{"depth_tables_128", "depth_arrays_128", "depth_union_tables_64", "depth_keyed_128"}
	refused := []string{"depth_tables_129", "depth_arrays_129", "depth_union_tables_65", "depth_keyed_129", "depth_tables_5000"}
	for _, name := range reads {
		var report tabletext.Report
		ok, err := tablewire.Decode(model, model.New(scene), read(name), &report)
		if !ok || err != nil || report.Unknown != 1 {
			t.Fatalf("engine %s: ok=%v err=%v unknown=%d, want ok=true unknown=1", name, ok, err, report.Unknown)
		}
	}
	for _, name := range refused {
		var report tabletext.Report
		ok, err := tablewire.Decode(model, model.New(scene), read(name), &report)
		if ok || !tablewire.ReadRefusal(err) {
			t.Fatalf("engine %s: accepted a depth the binary bound refuses (err=%v)", name, err)
		}
	}

	var probe strings.Builder
	probe.WriteString(`zero = %{unknown: 0, widened: 0, retained: 0, retain_lost: 0, reason: "", missing_required: 0}
`)
	for _, name := range reads {
		fmt.Fprintf(&probe, `
data = File.read!(%q)
store = %%{records: [], used: 0, cap: 1048576, ids: [], id_cap: 131072}
{:ok, value, load, retain} = Graphdemo.Table.load_retain("Scene", data, store, zero)
if load.retain_lost != 0, do: raise("%s: the retaining load lost #{load.retain_lost} records")
if load.unknown != 1, do: raise("%s: the retaining load counted #{load.unknown} unknown records")
{:ok, saved, save, _} = Graphdemo.Table.save_retain("Scene", value, retain, zero)
if save.retain_lost != 0, do: raise("%s: the retaining save lost #{save.retain_lost} records")
if saved != data, do: raise("%s: the retaining save is #{byte_size(saved)} bytes and the file is #{byte_size(data)}")
`, filepath.Join(framing, name+".bin"), name, name, name, name)
	}
	for _, name := range refused {
		fmt.Fprintf(&probe, `
data = File.read!(%q)
store = %%{records: [], used: 0, cap: 1048576, ids: [], id_cap: 131072}
case Graphdemo.Table.load_retain("Scene", data, store, zero) do
  {:ok, _, _, _} -> raise("%s: the retaining load accepted a refused depth")
  _ -> :ok
end
`, filepath.Join(framing, name+".bin"), name)
	}
	probe.WriteString(`IO.puts("ok: Elixir retain walk depth 128")
`)

	files := generateElixir(t, u)
	if out, err := runArmProbe(t, elixirBin, files, probe.String()); err != nil || !strings.Contains(out, "ok: Elixir retain walk depth 128") {
		t.Fatalf("retain depth probe: %v\n%s", err, out)
	}
}
