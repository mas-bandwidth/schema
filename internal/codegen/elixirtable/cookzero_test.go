package elixirtable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// cookZeroProbe cooks through the generated Elixir writer, in both byte
// orders. Two hand-built roots hold nothing live but zeros: a TurretConfig
// whose optional gunner is absent, and a RootConfig whose counted weapons and
// profiles arrays are empty. Every byte of each cook's data section must be
// zero, so an absent optional's payload and a counted array's dead slots are
// zero whatever the declared defaults are. keyed_config (absent optionals) and
// root_full (counted arrays below capacity) are loaded from their committed
// wires and cook byte for byte to the committed cook-write fixtures.
const cookZeroProbe = `
mod = Tabledemo.Table

check = fn
  true, _ -> :ok
  false, what ->
    IO.puts(:stderr, "FAIL " <> what)
    Process.put(:failed, true)
end

cook = fn table, value, order ->
  {size, :ok} = mod.cook_measure_reason(table, value)
  {true, out} = mod.cook_from(table, value, :binary.copy(<<0xA5>>, size), order)
  out
end

data = fn out, order ->
  <<_::binary-size(24), len::binary-size(8), _::binary>> = out
  n = if order == :big, do: :binary.decode_unsigned(len, :big), else: :binary.decode_unsigned(len, :little)
  binary_part(out, 64, n)
end

zero? = fn bytes -> bytes == :binary.copy(<<0>>, byte_size(bytes)) end

for order <- [:little, :big] do
  turret = %{"damage" => 0.0, "cooldown" => 0.0}
  check.(zero?.(data.(cook.("TurretConfig", turret, order), order)),
    "#{order}: an absent optional's payload is not zero")

  empty = %{"version_note" => "", "weapons" => [], "profiles" => []}
  check.(zero?.(data.(cook.("RootConfig", empty, order), order)),
    "#{order}: a counted array's dead slots are not zero")
end

for {name, table, wire, little, big} <- [ROWS] do
  {:ok, value, _report} = mod.load(table, File.read!(wire))

  for {order, want} <- [{:little, little}, {:big, big}] do
    check.(cook.(table, value, order) == File.read!(want),
      "#{name} #{order}: the cook differs from the committed fixture")
  end
end

if Process.get(:failed), do: System.halt(1), else: IO.puts("ok: zero dead and absent bytes")
`

// TestElixirCookWriteZeroesDeadAndAbsentBytes holds docs/SPEC-TABLES.md §7.2
// and §7.6 on the Elixir leg's cook writer: an absent optional's presence byte
// is zero and its payload stays zero bytes, and a counted array's slots past
// its live count are zero, in both byte orders. It runs the leg's own
// toolchain, so it is a FUNCTIONAL test.
func TestElixirCookWriteZeroesDeadAndAbsentBytes(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)
	elixirc := elixircBinary(t, elixirBin)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	files := generateElixir(t, loadUnit(t, schemaFilesIn(t, filepath.Join(root, "tables", "examples"))...))
	src, ebin := t.TempDir(), t.TempDir()
	var names []string
	for name, data := range files {
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".ex") {
			continue
		}
		if err := os.WriteFile(filepath.Join(src, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	env := append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	compile := exec.Command(elixirc, append([]string{"--ignore-module-conflict", "-o", ebin}, sortedRuntimeFirst(names)...)...)
	compile.Dir, compile.Env = src, env
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("elixirc: %v\n%s", err, out)
	}

	fixture := func(name string) string {
		return filepath.Join(root, "testdata", "conformance", "tables", "cook-write", name+".cook")
	}
	wire := func(name string) string {
		return filepath.Join(root, "testdata", "conformance", "tables", "wire", name+".bin")
	}
	var rows []string
	for _, r := range []struct{ name, table string }{{"keyed_config", "KeyedConfig"}, {"root_full", "RootConfig"}} {
		rows = append(rows, fmt.Sprintf("{%q, %q, %q, %q, %q}", r.name, r.table, wire(r.name), fixture(r.name), fixture(r.name+"-be")))
	}
	probe := filepath.Join(t.TempDir(), "probe.exs")
	if err := os.WriteFile(probe, []byte(strings.Replace(cookZeroProbe, "[ROWS]", "["+strings.Join(rows, ", ")+"]", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(elixirBin, "-pa", ebin, probe)
	run.Env = env
	out, err := run.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok: zero dead and absent bytes") {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
}
