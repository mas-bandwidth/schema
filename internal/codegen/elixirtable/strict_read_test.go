package elixirtable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE STRICT READ'S ONE TYPED REASON (docs/SPEC-TABLES.md §6.5, §11):
// Tblv1.Table.read_strict answers {:ok, value} for a clean wire and, for
// every other wire, {:error, reason} with the one atom err/1 decides, in the
// precedence the reference legs use. The wires are the conformance corpus's
// own two generations of Cfg, read by the first: its own wire reads, the
// second generation's clamps and kind-mismatches, a cut id table and an empty
// wire are damage, and a form byte this read does not carry is :unknown_form
// whichever form it is. The tolerant load still reads the clamped and
// kind-mismatched wires, so the probe also holds the strict read apart from it.
func TestElixirStrictReadReason(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	files, probe := strictReadProbe(t)
	if out, err := runArmProbe(t, elixirBinary(t), files, probe); err != nil {
		t.Fatalf("strict read probe: %v\n%s", err, out)
	}
}

// Swapping the clamped and kind_mismatch atoms in err/1 must red exactly the
// two cases that name them.
func TestElixirStrictReadReasonNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	files, probe := strictReadProbe(t)
	elixirPatch(t, files, "when n > 0, do: :clamped", "when n > 0, do: :strict_swap_sentinel")
	elixirPatch(t, files, "when n > 0, do: :kind_mismatch", "when n > 0, do: :clamped")
	elixirPatch(t, files, ":strict_swap_sentinel", ":kind_mismatch")
	out, err := runArmProbe(t, elixirBinary(t), files, probe)
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: swapped clamped and kind_mismatch stayed green\n%s", out)
	}
	for _, label := range []string{"clamped strict", "kind_mismatch strict"} {
		if !strings.Contains(out, label) {
			t.Errorf("negative control missed %s:\n%s", label, out)
		}
	}
}

func strictReadProbe(t *testing.T) (map[string][]byte, string) {
	t.Helper()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	files := generateElixir(t, loadUnit(t, filepath.Join(repo, "test/tables/V1.schema")))
	wire := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	own := wire("v1_cfg.bin")
	cut := append([]byte(nil), own[:len(own)-1]...)
	newer := append([]byte{9}, own[1:]...)
	message := append([]byte{2}, own[1:]...)
	cases := []struct {
		name     string
		wire     []byte
		reason   string
		tolerant bool
	}{
		{"own", own, "ok", true},
		{"clamped", wire("v2_cfg.bin"), "clamped", true},
		{"kind_mismatch", wire("v2_seams.bin"), "kind_mismatch", true},
		{"cut_id_table", cut, "wire_damaged", false},
		{"empty", nil, "wire_damaged", false},
		{"newer_form", newer, "unknown_form", false},
		{"message_form", message, "unknown_form", false},
	}
	var probe strings.Builder
	probe.WriteString(strictReadElixir)
	for _, c := range cases {
		fmt.Fprintf(&probe, "Probe.case(%q, %s, :%s, %t)\n", c.name, elixirCountBytes(c.wire), c.reason, c.tolerant)
	}
	probe.WriteString("if Process.get(:strict_failed), do: System.halt(1)\n")
	return files, probe.String()
}

const strictReadElixir = `
defmodule Probe do
  def check(true, _label), do: :ok

  def check(false, label) do
    IO.puts(:stderr, "FAIL: " <> label)
    Process.put(:strict_failed, true)
  end

  def case(label, wire, want, tolerant) do
    strict =
      case Tblv1.Table.read_strict("Cfg", wire) do
        {:ok, _value} -> :ok
        {:error, why} -> why
      end

    check(strict == want, label <> " strict got=" <> inspect(strict))

    {verdict, report} =
      case Tblv1.Table.load("Cfg", wire) do
        {:ok, _value, report} -> {:ok, report}
        {:error, _refusal, report} -> {:error, report}
      end

    named = Tblv1.Table.err(report) || if(verdict == :ok, do: :ok, else: :wire_damaged)
    check(named == strict, label <> " err got=" <> inspect(named))
    check((verdict == :ok) == tolerant, label <> " tolerant got=" <> inspect(verdict))
  end
end
`
