package elixirtable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestArmRetypeElixir pins docs/SPEC-TABLES.md §3 and §4 at a UNION ARM and at
// an ARRAY-OF-POINTERS SLOT on the Elixir variable form, over the corpus's own
// hand-built wires (reports.txt pins every one on every leg):
//
//   - a1_arm_moved_width: an int32 arm read by an int64 arm is WIDENED, the
//     payload decoded at its own width and the value the writer's own;
//   - a2_arm_length_off_width: an arm whose kind agrees but whose L is not its
//     kind's width is that arm's own framing damage, malformed, the union None
//     and the parent reading on past L;
//   - parts_index_out_of_range: a slot naming a node index past the node table
//     is malformed and THAT slot reads null, the array kept at its count.
//
// Each has a REVERSED WITNESS: the emitted runtime with the old behaviour
// planted back (a retyped arm always a kind mismatch, no length check at an
// agreeing arm, an out-of-range slot read null silently) must turn the probe
// red, so the probe is shown to see the rule it pins.
func TestArmRetypeElixir(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	wire := func(name string) string {
		return elixirString(filepath.Join(root, "testdata", "wire", "tables", name+".bin"))
	}
	a2 := filepath.Join(root, "test", "tables", "A2.schema")
	stream := filepath.Join(root, "tables", "stream", "Stream.schema")

	armProbe := `
defmodule Probe do
  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end

  def counters(r), do: {r.unknown, r.kind_mismatch, r.widened, r.clamped, r.malformed}
end

{:ok, v, r} = Tbla2.Table.load("Root", File.read!(` + wire("a1_arm_moved_width") + `))
Probe.check(v["value"] == {"a", 7}, "the widened arm lands the writer's value: #{inspect(v)}")
Probe.check(Probe.counters(r) == {0, 0, 1, 0, false}, "one widened, nothing else: #{inspect(r)}")
Probe.check(v["id"] == 11 and v["tail"] == 22, "the siblings read: #{inspect(v)}")

{:ok, v, r} = Tbla2.Table.load("Root", File.read!(` + wire("a2_arm_length_off_width") + `))
Probe.check(v["value"] == nil, "a damaged arm reads None: #{inspect(v)}")
Probe.check(Probe.counters(r) == {0, 0, 0, 0, true}, "the arm's L off its width is malformed: #{inspect(r)}")
Probe.check(v["tail"] == 22, "the parent reads on past L: #{inspect(v)}")

{:ok, v, r} = Tbla2.Table.load("Root", File.read!(` + wire("a1_arm_one_width") + `))
Probe.check(v["value"] == nil and Probe.counters(r) == {0, 1, 0, 0, false},
  "int32 under a float32 arm stays a kind mismatch: #{inspect({v, r})}")

{:ok, v, r} = Tbla2.Table.load("Root", File.read!(` + wire("a1_arm_body") + `))
Probe.check(v["value"] == {"e", %{"n" => 5}} and Probe.counters(r) == {0, 0, 0, 0, false},
  "the sane body arm still round-trips: #{inspect({v, r})}")
IO.puts("ok")
`
	slotProbe := `
defmodule Probe do
  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end

{:ok, v, r} = Streamdemo.Table.load("Feed", File.read!(` + wire("parts_index_out_of_range") + `))
Probe.check(r.malformed == true, "a slot past the node table is malformed: #{inspect(r)}")
Probe.check(r.kind_mismatch == 0 and r.unknown == 0, "and no other event: #{inspect(r)}")
Probe.check(length(v["parts"]) == 2 and Enum.all?(v["parts"], &is_nil/1),
  "the slot reads null and the array keeps its count: #{inspect(v)}")
IO.puts("ok")
`

	cases := []struct {
		name, schema, probe string
		plant               [][2]string // the old behaviour, planted back
		// crash, when set, is the one uncaught error that also counts as red on
		// the rule: the planted behaviour reaching a decoder that only matches
		// its exact width, so the unchecked length ends the load in that
		// function instead of on a probe check.
		crash string
	}{
		{"widened arm", a2, armProbe, [][2]string{{
			"retyped_arm(a, name, k, binary_part(content, 0, l), report)",
			"{:cont, nil, %{report | kind_mismatch: report.kind_mismatch + 1}}",
		}}, ""},
		{"arm length off width", a2, armProbe, [][2]string{{
			"if arm_framing_damaged?(a, inner) do",
			"if false and arm_framing_damaged?(a, inner) do",
		}}, "(FunctionClauseError) no function clause matching in Tbla2.Table.float_of/3"},
		{"slot out of range", stream, slotProbe, [][2]string{{
			":out_of_range -> {nil, %{report | malformed: true}}",
			":out_of_range -> {nil, report}",
		}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			u := loadUnit(t, c.schema)
			files := generateElixir(t, u)
			out, err := runArmProbe(t, elixirBin, files, c.probe)
			if err != nil {
				t.Fatalf("probe failed: %v\n%s", err, out)
			}
			for _, p := range c.plant {
				elixirPatch(t, files, p[0], p[1])
			}
			out, err = runArmProbe(t, elixirBin, files, c.probe)
			if err == nil {
				t.Fatalf("REVERSED WITNESS FAILED: the old behaviour planted back left the probe green\n%s", out)
			}
			if !strings.Contains(out, "FAIL: ") && (c.crash == "" || !strings.Contains(out, c.crash)) {
				t.Fatalf("REVERSED WITNESS FAILED: the probe went red, but not on a check: %v\n%s", err, out)
			}
		})
	}
}

// runArmProbe is runProbe returning the failure rather than failing on it, so
// a reversed witness can require red.
func runArmProbe(t *testing.T, elixirBin string, files map[string][]byte, probe string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	names := writeModuleDir(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "probe.exs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{}
	for _, n := range names {
		args = append(args, "-r", n)
	}
	args = append(args, "probe.exs")
	cmd := exec.Command(elixirBin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}
