package elixirtable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// armMinSchema holds a counted [2..4] inside a union arm, once in a fixed table
// and once in a variable table (R10: docs/SPEC-TABLES.md section 3.3, a
// counted [A..B] holds A wherever the field sits).
const armMinSchema = `package armmin
union Pick {
    few [2..4]uint32
    other int32
}
fixed table FixedRoot { pick Pick }
table VarRoot { pick Pick }
fixed table FixedPlain { few [2..4]uint32 }
table VarPlain { few [2..4]uint32 }
`

func armMinFiles(t *testing.T) map[string][]byte {
	t.Helper()
	schema := filepath.Join(t.TempDir(), "ArmMin.schema")
	if err := os.WriteFile(schema, []byte(armMinSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	return generateElixir(t, loadUnit(t, schema))
}

func armMinText(files map[string][]byte) string {
	var b strings.Builder
	for _, data := range files {
		b.Write(data)
	}
	return b.String()
}

// TestArmMinEmitted pins the emitter's side without a toolchain: the arm's
// descriptor carries the minimum, and the fixed form's reader holds it. The
// old emitter wrote neither.
func TestArmMinEmitted(t *testing.T) {
	t.Parallel()
	all := armMinText(armMinFiles(t))
	for _, want := range []string{"min_count: 2", "R.held_min(c, n_few, 2)"} {
		if !strings.Contains(all, want) {
			t.Errorf("generated Elixir lacks %q: the arm's minimum is not carried", want)
		}
	}
}

// TestArmMinJSONRefusesElixir reads a count below A at a union arm through the
// text form, on both table forms, and must refuse it; the reversed witness
// plants the old reader back (no minimum) and must go red.
func TestArmMinJSONRefusesElixir(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	probe := `
defmodule Probe do
  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end

  def refused?(root, text) do
    case Armmin.Table.from_json(root, text) do
      {:ok, _, report} -> Map.get(report, :reason, "") == "wire_damaged"
      _ -> true
    end
  end
end

for root <- ["FixedRoot", "VarRoot"] do
  Probe.check(Probe.refused?(root, ~s({"pick":{"few":[7]}})), root <> " below A refuses")
  Probe.check(Probe.refused?(root, ~s({"pick":{"few":[]}})), root <> " empty refuses")
  Probe.check(not Probe.refused?(root, ~s({"pick":{"few":[7,9]}})), root <> " at A reads")
  Probe.check(not Probe.refused?(root, ~s({"pick":{"few":[1,2,3,4]}})), root <> " at B reads")
end

IO.puts("ok")
`
	files := armMinFiles(t)
	out, err := runArmProbe(t, elixirBinary(t), files, probe)
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
	elixirPatch(t, files, "if placed < Map.get(f, :min_count, 0), do: throw(:wire_damaged)", "_ = placed")
	out, err = runArmProbe(t, elixirBinary(t), files, probe)
	if err == nil {
		t.Fatalf("REVERSED WITNESS FAILED: no minimum left the probe green\n%s", out)
	}
	if !strings.Contains(out, "FAIL: ") {
		t.Fatalf("REVERSED WITNESS FAILED: red, but not on a check: %v\n%s", err, out)
	}
}

// armMinLooseSchema is the same declarations with no minimum on the arm: the
// writer of a wire the strict reader must refuse (a count below A is a wire no
// conforming writer produces, so the test borrows a writer that cannot refuse
// it; field ids hash from names, so the two units agree on the wire).
const armMinLooseSchema = `package armminloose
union Pick {
    few [..4]uint32
    other int32
}
table VarRoot { pick Pick }
`

// TestArmMinVariableWireElixir reaches the variable wire with a union arm whose
// count is below A: 0 and 1 refuse, 2 reads. The reversed witness plants the
// variable-wire minimum check out and must go red.
func TestArmMinVariableWireElixir(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	schema := filepath.Join(t.TempDir(), "Loose.schema")
	if err := os.WriteFile(schema, []byte(armMinLooseSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	files := armMinFiles(t)
	for name, data := range generateElixir(t, loadUnit(t, schema)) {
		files["loose_"+name] = data
	}
	probe := `
defmodule Probe do
  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end

  def wire(elements) do
    {:ok, bytes} = Armminloose.Table.save("VarRoot", %{"pick" => {"few", elements}})
    bytes
  end

  def refused?(bytes) do
    case Armmin.Table.load("VarRoot", bytes) do
      {:ok, _, _} -> false
      _ -> true
    end
  end
end

Probe.check(Probe.refused?(Probe.wire([])), "variable wire: count 0 in the arm refuses")
Probe.check(Probe.refused?(Probe.wire([7])), "variable wire: count 1 in the arm refuses")

case Armmin.Table.load("VarRoot", Probe.wire([7, 9])) do
  {:ok, v, _} -> Probe.check(v["pick"] == {"few", [7, 9]}, "variable wire: count 2 reads: #{inspect(v)}")
  other -> Probe.check(false, "variable wire: count 2 reads: #{inspect(other)}")
end

IO.puts("ok")
`
	out, err := runArmProbe(t, elixirBinary(t), files, probe)
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
	elixirPatch(t, files, "if n < Map.get(f, :min_count, 0), do: throw(:wire_damaged)", "_ = n")
	out, err = runArmProbe(t, elixirBinary(t), files, probe)
	if err == nil {
		t.Fatalf("REVERSED WITNESS FAILED: no variable-wire minimum left the probe green\n%s", out)
	}
	if !strings.Contains(out, "FAIL: ") {
		t.Fatalf("REVERSED WITNESS FAILED: red, but not on a check: %v\n%s", err, out)
	}
}

// TestArmMinSaveReturnsErrorElixir: the Table save of a value whose count is
// below A returns {:error, _} and never raises, a plain field and an arm
// alike, on the fixed and the variable form. The reversed witness removes the
// rescue and must go red.
func TestArmMinSaveReturnsErrorElixir(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	probe := `
defmodule Probe do
  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end

  def refused(label, root, value) do
    result =
      try do
        Armmin.Table.save(root, value)
      rescue
        e -> {:raised, e}
      end

    Probe.check(match?({:error, _}, result), label <> " answers {:error, _}: #{inspect(result)}")
  end
end

for n <- [0, 1] do
  few = Enum.to_list(1..n//1)
  Probe.refused("fixed arm count #{n}", "FixedRoot", %{"pick" => {"few", few}})
  Probe.refused("variable arm count #{n}", "VarRoot", %{"pick" => {"few", few}})
  Probe.refused("fixed plain count #{n}", "FixedPlain", %{"few" => few})
  Probe.refused("variable plain count #{n}", "VarPlain", %{"few" => few})
end

for {root, value} <- [{"FixedRoot", %{"pick" => {"few", [7, 9]}}}, {"VarRoot", %{"pick" => {"few", [7, 9]}}},
                      {"FixedPlain", %{"few" => [7, 9]}}, {"VarPlain", %{"few" => [7, 9]}}] do
  Probe.check(match?({:ok, _}, Armmin.Table.save(root, value)), root <> " at A saves")
end

IO.puts("ok")
`
	files := armMinFiles(t)
	out, err := runArmProbe(t, elixirBinary(t), files, probe)
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
	elixirPatch(t, files, "ArgumentError -> {:error, :invalid_value}", "ArgumentError -> reraise ArgumentError, __STACKTRACE__")
	out, err = runArmProbe(t, elixirBinary(t), files, probe)
	if err == nil {
		t.Fatalf("REVERSED WITNESS FAILED: a raising save left the probe green\n%s", out)
	}
	if !strings.Contains(out, "FAIL: ") {
		t.Fatalf("REVERSED WITNESS FAILED: red, but not on a check: %v\n%s", err, out)
	}
}

const countedMinElemsSchema = `package countedmin

table E
{
    n uint32
}

union U
{
    a uint32
    b int32
}

union P
{
    few [2..4]uint32
    other int32
}

table R
{
    es [2..4]E
}

table RU
{
    us [2..4]U
}

table RP
{
    p P
}
`

type countedMinCase struct {
	id, table, text, elixir string
}

var countedMinCases = []countedMinCase{
	{"table_born", "R", `{"es":[{},{}]}`, `%{"es" => [%{"n" => 0}, %{"n" => 0}]}`},
	{"table_three", "R", `{"es":[{},{},{}]}`, `%{"es" => [%{"n" => 0}, %{"n" => 0}, %{"n" => 0}]}`},
	{"table_one_set", "R", `{"es":[{},{"n":7}]}`, `%{"es" => [%{"n" => 0}, %{"n" => 7}]}`},
	{"union_born", "RU", `{"us":[{},{}]}`, `%{"us" => [nil, nil]}`},
	{"union_three", "RU", `{"us":[{},{},{}]}`, `%{"us" => [nil, nil, nil]}`},
	{"union_one_set", "RU", `{"us":[{},{"a":7}]}`, `%{"us" => [nil, {"a", 7}]}`},
	// selecting a counted arm with no payload gives it A default elements
	{"arm_selected", "RP", `{"p":{"few":[0,0]}}`, `%{"p" => {"few", nil}}`},
	{"arm_set", "RP", `{"p":{"few":[5,6]}}`, `%{"p" => {"few", [5, 6]}}`},
}

// TestCountedMinElementsMatchEngineElixir compares the Elixir table wire with
// the independent engine's (internal/tablewire) for a [2..4] array of tables
// or unions at its born state, one over and one set, and for a counted arm
// selected with no payload. The reversed witness plants the old judgement
// back (a born array always rides, a bare arm reads as []).
func TestCountedMinElementsMatchEngineElixir(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	schema := filepath.Join(t.TempDir(), "CountedMin.schema")
	if err := os.WriteFile(schema, []byte(countedMinElemsSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, schema)
	m := tabletext.NewModel(u)
	want := map[string]string{}
	var probe strings.Builder
	probe.WriteString("for {id, root, value} <- [\n")
	for _, c := range countedMinCases {
		v := m.New(m.Lookup(c.table))
		var rep tabletext.Report
		if !m.Read(v, []byte(c.text), &rep) {
			t.Fatalf("%s: the engine did not read %s: %+v", c.id, c.text, rep)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatalf("%s: the engine did not encode: %v", c.id, err)
		}
		want[c.id] = hex.EncodeToString(wire)
		fmt.Fprintf(&probe, "  {%q, %q, %s},\n", c.id, c.table, c.elixir)
	}
	probe.WriteString(`] do
  case Countedmin.Table.save(root, value) do
    {:ok, bytes} -> IO.puts("CROSS " <> id <> " " <> Base.encode16(bytes, case: :lower))
    other -> IO.puts("CROSS " <> id <> " " <> inspect(other))
  end
end
`)
	files := generateElixir(t, u)
	run := func() map[string]string {
		out, err := runArmProbe(t, elixirBinary(t), files, probe.String())
		if err != nil {
			t.Fatalf("probe failed: %v\n%s", err, out)
		}
		got := map[string]string{}
		for _, line := range strings.Split(out, "\n") {
			if parts := strings.Fields(line); len(parts) == 3 && parts[0] == "CROSS" {
				got[parts[1]] = parts[2]
			}
		}
		return got
	}
	got := run()
	for _, c := range countedMinCases {
		if got[c.id] != want[c.id] {
			t.Errorf("%s: Elixir %q, engine %q", c.id, got[c.id], want[c.id])
		}
	}
	// the reversed witnesses: the old judgements planted back, each in its own
	// copy of the generated files
	plant := func(old, repl string, ids ...string) {
		t.Helper()
		files = generateElixir(t, u)
		elixirPatch(t, files, old, repl)
		after := run()
		for _, id := range ids {
			if after[id] == want[id] {
				t.Errorf("REVERSED WITNESS FAILED: %s still matches the engine with %q planted back", id, old)
			}
		}
	}
	plant("list_born?(f, elements, idx_map, ids, order) or", "false or", "table_born", "union_born")
	plant("default: [0, 0]", "default: []", "arm_selected")
}
