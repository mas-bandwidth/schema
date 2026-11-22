package elixirtable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
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
