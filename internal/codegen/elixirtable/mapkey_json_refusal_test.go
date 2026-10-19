package elixirtable_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixir"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixirtable"
)

// TestMapKeyJSONRefusal pins the nightly-fuzz finding that a JSON object key
// that is not the decimal spelling of an integer on an integer-keyed map is
// refused whole, never handed to the writer's String.to_integer to raise
// (binary_to_integer("roster") on mapdemo Depth). The generated Table module
// must carry the integer-key validation on the JSON read boundary; before the
// fix the guard was absent and the source read on. The saved input is replayed
// by TestGeneratorRuntimeWitnesses, which executes the generated Elixir and is
// red at BASE; this test keeps the source contract beside it.
func TestMapKeyJSONRefusal(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	schemaDir := filepath.Join(root, "tables", "maps")
	paths, err := compiler.GatherPaths([]string{schemaDir})
	if err != nil {
		t.Fatalf("mapdemo gather: %v", err)
	}
	c := compiler.New()
	unit, err := c.Load(paths)
	if err != nil {
		t.Fatalf("mapdemo load: %v", err)
	}
	files, err := c.Generate(unit, "elixir", compiler.Options{})
	if err != nil {
		t.Fatalf("mapdemo generate elixir: %v", err)
	}
	src, ok := files["Table.ex"]
	if !ok {
		t.Fatal("mapdemo generated no Table.ex")
	}
	text := string(src)

	if !strings.Contains(text, "defp bad_map_key?(") {
		t.Error("generated Table.ex lacks the integer-key JSON refusal on the map read boundary")
	}
	if strings.Contains(text, "bad_map_key?_") {
		t.Error("generated Table.ex has an unguarded map-key path")
	}
}

// TestGeneratorRuntimeWitnesses executes the generated Elixir on one saved
// input per carried generator repair in variableform.go. Each subtest is red
// at BASE (the repair absent) and green at the fixed head:
//
//   - enum nil: a decoded unknown variant of a field whose declared default is
//     not nil (messages OpenDocument mode = Read) saves by eliding, not by
//     raising Map.get(nil).
//   - UTF-16: a JSON wstring value with bytes that are not UTF-8 converts to
//     its valid prefix and saves; BASE calls byte_size/1 on the error tuple.
//   - integer map key: a JSON object key that is not a decimal integer is
//     refused as :malformed; BASE returns it and save raises String.to_integer.
//   - retain list: a list whose elements are not tables (listdemo Save
//     scores []int32) saves through save_retain; BASE demands a table element
//     descriptor and raises.
func TestGeneratorRuntimeWitnesses(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	witness := filepath.Join(root, "internal", "ci", "testdata", "schema-elixir-nightly-fuzz", "witness")
	elixirBin := runtimeElixir(t)

	cases := []struct {
		name     string
		schema   string
		input    string
		probe    string
		mustFail string
	}{
		{
			name:   "enum_nil",
			schema: filepath.Join(root, "tables", "messages"),
			input:  filepath.Join(witness, "enum-nil.json"),
			probe: `
json = File.read!(` + elixirString(filepath.Join(witness, "enum-nil.json")) + `)
case Messagedemo.Table.from_json("OpenDocument", json) do
  {:ok, value, _report} ->
    case Messagedemo.Table.save("OpenDocument", value) do
      {:ok, _bytes} -> IO.puts("ok")
      other -> IO.inspect(other, label: "save"); System.halt(1)
    end
  other -> IO.inspect(other, label: "from_json"); System.halt(1)
end
`,
		},
		{
			name:   "utf16_invalid_utf8",
			schema: filepath.Join(root, "tables", "maps"),
			input:  filepath.Join(witness, "wstring-invalid-utf8.json"),
			probe: `
json = File.read!(` + elixirString(filepath.Join(witness, "wstring-invalid-utf8.json")) + `)
case Mapdemo.Table.from_json("Text", json) do
  {:ok, value, _report} ->
    case Mapdemo.Table.save("Text", value) do
      {:ok, _bytes} -> IO.puts("ok")
      other -> IO.inspect(other, label: "save"); System.halt(1)
    end
  other -> IO.inspect(other, label: "from_json"); System.halt(1)
end
`,
		},
		{
			name:   "integer_map_key_refused",
			schema: filepath.Join(root, "tables", "maps"),
			input:  filepath.Join(witness, "mapkey-int.json"),
			probe: `
json = File.read!(` + elixirString(filepath.Join(witness, "mapkey-int.json")) + `)
case Mapdemo.Table.from_json("Depth", json) do
  {:error, :malformed, _report} -> IO.puts("ok")
  {:ok, value, _report} ->
    case Mapdemo.Table.save("Depth", value) do
      {:ok, _bytes} -> IO.puts("BAD: accepted a non-integer map key"); System.halt(1)
      other -> IO.inspect(other, label: "save"); System.halt(1)
    end
  other -> IO.inspect(other, label: "from_json"); System.halt(1)
end
`,
		},
		{
			name:   "retain_list_non_table",
			schema: filepath.Join(root, "tables", "lists"),
			input:  filepath.Join(witness, "retain-list.json"),
			probe: `
json = File.read!(` + elixirString(filepath.Join(witness, "retain-list.json")) + `)
retain = %{records: [], used: 0, cap: 1048576, ids: [], id_cap: 131072}
report = %{unknown: 0, kind_mismatch: 0, widened: 0, clamped: 0, malformed: false, retained: 0, retain_lost: 0}
{:ok, value, _report} = Listdemo.Table.from_json("Save", json)
case Listdemo.Table.save_retain("Save", value, retain, report) do
  {:ok, _bytes, _report, _retain} -> IO.puts("ok")
  other -> IO.inspect(other, label: "save_retain"); System.halt(1)
end
`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			paths, err := compiler.GatherPaths([]string{tc.schema})
			if err != nil {
				t.Fatalf("gather %s: %v", tc.schema, err)
			}
			c := compiler.New()
			unit, err := c.Load(paths)
			if err != nil {
				t.Fatalf("load %s: %v", tc.schema, err)
			}
			files, err := elixir.Generate(unit)
			if err != nil {
				t.Fatalf("generate packet: %v", err)
			}
			tables, err := elixirtable.Generate(unit)
			if err != nil {
				t.Fatalf("generate table: %v", err)
			}
			for name, data := range tables {
				files[name] = data
			}
			dir := t.TempDir()
			names := writeRuntimeModules(t, dir, files)
			runRuntimeProbe(t, elixirBin, dir, names, tc.probe)
		})
	}
}

func runtimeElixir(t *testing.T) string {
	t.Helper()
	if env := os.Getenv("ELIXIR"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env
		}
		if bin, err := exec.LookPath(env); err == nil {
			return bin
		}
	}
	bin, err := filepath.Abs("../../../dist/elixir-1.20.4/bin/elixir")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bin); err == nil {
		return bin
	}
	found, err := exec.LookPath("elixir")
	if err != nil {
		t.Fatalf("no Elixir toolchain: not at %s and not on PATH", bin)
	}
	return found
}

func runtimeOTPBin(t *testing.T) string {
	t.Helper()
	if env := os.Getenv("ERL_BIN"); env != "" {
		return env
	}
	p, err := filepath.Abs("../../../dist/otp-29.0.5/bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p, "erl")); err == nil {
		return p
	}
	found, err := exec.LookPath("erl")
	if err != nil {
		return p
	}
	return filepath.Dir(found)
}

func writeRuntimeModules(t *testing.T, dir string, files map[string][]byte) []string {
	t.Helper()
	var names []string
	for name, data := range files {
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".ex") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	ordered := []string{elixirtable.BuildVersionModule + ".ex"}
	for _, name := range names {
		if name != elixirtable.BuildVersionModule+".ex" {
			ordered = append(ordered, name)
		}
	}
	return ordered
}

func runRuntimeProbe(t *testing.T, elixirBin, dir string, names []string, probe string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "probe.exs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{}
	for _, name := range names {
		args = append(args, "-r", name)
	}
	args = append(args, "probe.exs")
	cmd := exec.Command(elixirBin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+runtimeOTPBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("probe did not reach ok:\n%s", out)
	}
}

func elixirString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
