package elixirtable

// TestFixedRoadmapElixirT14CompressedFloats is elixir/R30. The dequantize
// fixture is index 2 under min=-1, max=2, resolution=0.1 (count=30, delta=3):
// unfused bits 0xbf4ccccc, fused bits 0xbf4ccccd.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixir"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapElixirT14CompressedFloats(t *testing.T) {
	t.Parallel()
	t.Run("elixir/R30", func(t *testing.T) {
		t.Parallel()
		slowtest.Gate(t, "elixir (it compiles and runs the generated unit)")
		bin := elixirBinary(t)
		t14Cite(t)
		src := t14ElixirEmit(t, t14ElixirOld, nil)
		if !strings.Contains(src, "R.f32_bits(") {
			t.Fatal("the fixed write does not store the float32 bits")
		}
		if !strings.Contains(src, "def dequantize_bits") {
			t.Fatal("the leg emits no dequantize")
		}
		dir := t.TempDir()
		file := filepath.Join(dir, "old.bin")
		t14ElixirRun(t, bin, t14ElixirOld, nil, t14ElixirWrite(file))
		t14ElixirRun(t, bin, t14ElixirOld, nil, t14ElixirRead(file))
		t14ElixirRun(t, bin, t14ElixirFine, []string{t14ElixirOld}, t14ElixirRead(file))
		t14ElixirRun(t, bin, t14ElixirDropped, []string{t14ElixirOld}, t14ElixirRead(file))
		t14ElixirRun(t, bin, t14ElixirOld, nil, t14ElixirContract())
	})
}

const t14ElixirOld = `package cfold

fixed table Reading
{
    lead uint32 = 1
    aim float32 | min = -1, max = 2, resolution = 0.1
    trail uint32 = 2
}
`

const t14ElixirFine = `package cfold

fixed table Reading
{
    lead uint32 = 1
    aim float32 | min = -1, max = 2, resolution = 0.01
    trail uint32 = 2
}
`

const t14ElixirDropped = `package cfold

fixed table Reading
{
    lead uint32 = 1
    aim float32
    trail uint32 = 2
}
`

func t14Cite(t *testing.T) {
	t.Helper()
	b, err := os.ReadFile("../../lockfile/cfloatrange_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "resolution coarsened") {
		t.Fatal("the lockfile citation is gone")
	}
}

func t14ElixirEmit(t *testing.T, schema string, older []string) string {
	t.Helper()
	files := t14ElixirFiles(t, schema, older)
	var b strings.Builder
	for _, data := range files {
		b.Write(data)
	}
	return b.String()
}

func t14ElixirFiles(t *testing.T, schema string, older []string) map[string][]byte {
	t.Helper()
	u := unitFrom(t, schema)
	lineage := map[string][]FixedLineageEntry{}
	for _, src := range older {
		o := unitFrom(t, src)
		for _, st := range ir.TableFixedRoots(o) {
			e, ok := FixedLineageOf(o, st.Name)
			if !ok {
				t.Fatal("no lineage")
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
	}
	files, err := elixir.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	return files
}

func t14ElixirRun(t *testing.T, bin, schema string, older []string, body string) {
	t.Helper()
	files := t14ElixirFiles(t, schema, older)
	dir := t.TempDir()
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
	probe := fmt.Sprintf(`defmodule T14Probe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
  def run do
%s
    IO.puts("ok")
  end
end
T14Probe.run()
`, body)
	if err := os.WriteFile(filepath.Join(dir, "probe.exs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{}
	for _, n := range sortedRuntimeFirst(names) {
		args = append(args, "-r", n)
	}
	args = append(args, "probe.exs")
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("probe: %v\n%s", err, out)
	}
}

func t14ElixirWrite(file string) string {
	return fmt.Sprintf(`    <<aim::little-float-32>> = <<0x3E99999A::little-32>>
    v = %%Cfold.Reading{lead: 1, aim: aim, trail: 2}
    data = Cfold.ProbeFixed.reading_fixed_save([v])
    check(:binary.match(data, <<0x9A, 0x99, 0x99, 0x3E>>) != :nomatch, "float bits missing")
    File.write!(%q, data)
`, file)
}

func t14ElixirRead(file string) string {
	return fmt.Sprintf(`    data = File.read!(%q)
    {:ok, [r], report} = Cfold.ProbeFixed.reading_fixed_load(data)
    <<bits::little-unsigned-32>> = <<r.aim::little-float-32>>
    check(bits == 0x3E99999A and r.lead == 1 and r.trail == 2, "value moved")
    check(report.clamped == 0, "clamped")
`, file)
}

func t14ElixirContract() string {
	return `    # min=-1 max=2 res=0.1 count=30 index=2: unfused 0xbf4ccccc, fused 0xbf4ccccd.
    bits = Cfold.TableMessage.dequantize_bits(2, -1.0, 3.0, 30)
    check(bits == 0xBF4CCCCC, "dequantize #{Integer.to_string(bits, 16)}")
`
}
