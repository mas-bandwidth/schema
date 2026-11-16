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
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestElixirPointerNodesReadDeclaredDefaults holds docs/SPEC-TABLES.md §3,
// §3.1 and §4 on the Elixir pointered value: an absent field reads its declared
// default everywhere, in the root and in every node of the node directory, and
// in every table nested by value and every element inside one. A value loaded
// from a graph whose nodes are at all defaults or partly at defaults equals the
// Go engine's read of the same bytes field for field, and saving it returns the
// same bytes because the writer elides fields at their defaults.
func TestElixirPointerNodesReadDeclaredDefaults(t *testing.T) {
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
	files := elixirPointerNodeModules(t, u)
	model := tabletext.NewModel(u)

	cases := []struct {
		label string
		table string
		text  string
	}{
		{"listnode_all_defaults", "ListNode", `{"next": {}}`},
		{"listnode_partly_defaults", "ListNode", `{"value": 7, "next": {"name": "n"}}`},
		{"scene_settings_node_defaults", "Scene", `{"settings": {}}`},
		{"scene_nested_and_elements", "Scene", `{"ground": {}, "layers": [{}], "meta": {}}`},
		{"scene_partly_defaults", "Scene", `{"name": "scene", "version": 3, "ground": {"depth": 2, "head": {"value": 9}}, "layers": [{"head": {}}, {"depth": 4}], "settings": {"label": "loud"}}`},
		{"scene_absent_all", "Scene", `{}`},
		{"marker_fixed_node_defaults", "Marker", `{"note": {}}`},
	}

	var probe strings.Builder
	probe.WriteString(`
defmodule PointerNodeDefaultsProbe do
  def check(true, _what), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    Process.put(:failed, true)
  end

  def finish do
    if Process.get(:failed), do: System.halt(1)
    IO.puts("ok: Elixir pointer nodes read declared defaults")
  end
end
`)

	dir := t.TempDir()
	for _, c := range cases {
		wire, expected := pointerNodeWire(t, model, c.table, c.text)
		wirePath := filepath.Join(dir, c.label+".bin")
		jsonPath := filepath.Join(dir, c.label+".json")
		if err := os.WriteFile(wirePath, wire, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(jsonPath, expected, 0o600); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&probe, `
try do
  label = %[1]q
  wire = File.read!(%[2]q)
  {:ok, value, report} = Graphdemo.Table.load(%[3]q, wire)
  PointerNodeDefaultsProbe.check(report.reason == "", label <> ": load refused: " <> report.reason)
  {:ok, expected, _expected_report} = Graphdemo.Table.from_json(%[3]q, File.read!(%[4]q))
  PointerNodeDefaultsProbe.check(value == expected, label <> ": loaded value differs from the engine's read")
  {:ok, saved} = Graphdemo.Table.save(%[3]q, value)
  PointerNodeDefaultsProbe.check(saved == wire, label <> ": save did not return the same bytes")
rescue
  e -> PointerNodeDefaultsProbe.check(false, %[1]q <> ": " <> Exception.format(:error, e, __STACKTRACE__))
end
`, c.label, wirePath, c.table, jsonPath)
	}
	probe.WriteString("\nPointerNodeDefaultsProbe.finish()\n")

	out, err := runArmProbe(t, elixirBin, files, probe.String())
	if err != nil || !strings.Contains(out, "ok: Elixir pointer nodes read declared defaults") {
		t.Fatalf("native pointer node defaults probe: %v\n%s", err, out)
	}
	t.Log(strings.TrimSpace(out))
}

// pointerNodeWire builds one value through the shared model, saves it as the
// canonical wire, and returns the Go engine's text of the same bytes: the
// engine's read with every body at its declared defaults.
func pointerNodeWire(t *testing.T, model *tabletext.Model, table, text string) ([]byte, []byte) {
	t.Helper()
	value := model.New(model.Lookup(table))
	var read tabletext.Report
	if !model.Read(value, []byte(text), &read) {
		t.Fatalf("%s: read %s: %+v", table, text, read)
	}
	wire, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatalf("%s: encode %s: %v", table, text, err)
	}
	back := model.New(model.Lookup(table))
	var report tabletext.Report
	ok, err := tablewire.Decode(model, back, wire, &report)
	if err != nil || !ok {
		t.Fatalf("%s: decode %s: ok=%v err=%v report=%+v", table, text, ok, err, report)
	}
	if !report.Silent() {
		t.Fatalf("%s: decode %s is not silent: %+v", table, text, report)
	}
	expected, err := model.Write(back)
	if err != nil {
		t.Fatalf("%s: write %s: %v", table, text, err)
	}
	return wire, expected
}

// elixirPointerNodeModules generates the unit, or reads the modules a control
// re-emitted into a directory, so the control can exercise the same probe over
// a generated loader without touching a tracked file.
func elixirPointerNodeModules(t *testing.T, u *ir.Unit) map[string][]byte {
	t.Helper()
	dir := os.Getenv("ELIXIR_POINTER_NODE_DEFAULTS_DIR")
	if dir == "" {
		return generateElixir(t, u)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read generated modules %s: %v", dir, err)
	}
	files := map[string][]byte{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".ex") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read generated module %s: %v", entry.Name(), err)
		}
		files[entry.Name()] = data
	}
	if len(files) == 0 {
		t.Fatalf("generated module directory %s holds no .ex file", dir)
	}
	return files
}
