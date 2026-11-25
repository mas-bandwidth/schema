package elixirtable

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// A COMPRESSED FLOAT'S DECLARED RANGE IS THE FIXED SAVE'S CONTRACT
// (docs/SPEC-TABLES.md §3.3, §3.4). The Go reference engine refuses a fixed
// save of a compressed float outside [min, max] (invalid_value) and writes
// nothing; this leg's write-side checks refuse in every build. The runtime's
// store raises ArgumentError on a breached contract, the way it already does
// for a ranged integer (R.ranged) and a count (R.count), and the fixed save
// answers that as {:error, :invalid_value}, never a raise (fixedclasswire.go,
// fixedRootSaveClause). The probe holds the VERDICT alone
// (docs/rulings.json refusal-verdict-only): a refused row answers
// {:error, :invalid_value}, and the reason text is never compared. Before
// this, the leaf, array, counted, keyed and union-arm stores went through
// cf32_bits alone and wrote the out-of-range value.
//
// testdata/conformance/feature-witnesses/compressed-range-write names the
// rows; internal/tablewire's TestCompressedRangeWriteCorpus pins its bytes to
// the reference engine.

const rangeWriteCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-range-write"

// TestElixirFixedCompressedStoresAreRanged is the fast twin of the corpus run:
// it reads the generated source. Every compressed store in the fixed save
// carries its declared bounds, every bound it names is defined, the plain
// float's store carries none (the control), and the runtime refuses a value
// outside the bounds.
func TestElixirFixedCompressedStoresAreRanged(t *testing.T) {
	t.Parallel()
	files := generateElixir(t, loadUnit(t, filepath.Join(rangeWriteCorpusDir, "Probe.schema")))
	fixed := string(files["ProbeFixed.ex"])
	if fixed == "" {
		t.Fatal("no ProbeFixed.ex generated")
	}
	stores := regexp.MustCompile(`R\.cf32_bits\(([^()]*)\)`).FindAllStringSubmatch(fixed, -1)
	if len(stores) != 5 {
		t.Errorf("ProbeFixed.ex: %d compressed stores, want 5 (a, xs, ys, ks, pick.v)", len(stores))
	}
	for _, s := range stores {
		_, attr, ok := strings.Cut(s[1], ", ")
		if !ok || !strings.HasPrefix(attr, "@r_") {
			t.Errorf("ProbeFixed.ex: a compressed float is stored with no declared range: %s", s[0])
			continue
		}
		if !strings.Contains(fixed, "  "+attr+" {") {
			t.Errorf("ProbeFixed.ex: %s names a range %s that is not defined", s[0], attr)
		}
	}
	if !strings.Contains(fixed, "R.f32_bits(v_p)::") {
		t.Error("ProbeFixed.ex: the plain float p is not stored through f32_bits with no range (the control)")
	}
	runtime := string(files["FixedRuntime.ex"])
	for _, want := range []string{
		// held at float32 (§3.3): the value is narrowed before the test
		"def cf32_bits(value, {lo, hi}) when is_float(value) do\n    bits = cf32_bits(value)\n    <<n::float-32-little>> = <<bits::little-32>>\n\n    if n < lo or n > hi do",
		"def cf64_bits(value, {lo, hi}) when is_float(value) and value >= lo and value <= hi,",
	} {
		if !strings.Contains(runtime, want) {
			t.Errorf("FixedRuntime.ex does not hold a compressed float to its range: no %q", want)
		}
	}
	if strings.Count(runtime, "is outside the declared range") < 3 {
		t.Error("FixedRuntime.ex: the compressed stores do not refuse a value outside the declared range")
	}
}

// rangeWriteProbe is the Elixir side; [ROWS] is the corpus.
const rangeWriteProbe = `
mod = Crw.Table

fail = fn what ->
  IO.puts("FAIL " <> what)
  Process.put(:failed, true)
end

set = fn
  value, {:field, f}, z -> Map.put(value, f, z)
  value, {:index, f, i}, z -> Map.put(value, f, List.replace_at(Map.get(value, f), i, z))
  value, {:key, f, k}, z -> Map.put(value, f, Map.put(Map.get(value, f) || %{}, k, z))
  value, {:arm, f, a}, z -> Map.put(value, f, {a, z})
end

{:ok, base, _} = mod.from_json("Fix", File.read!([BASE]))

for {name, path, v, expect} <- [ROWS] do
  # save/2 never raises: a raise is a failure of its own
  got =
    try do
      mod.save("Fix", set.(base, path, v))
    rescue
      e -> {:raised, Exception.message(e)}
    end

  # the refusal is the verdict (docs/SPEC.md §5.1); each refused row's lawful
  # twin at the same bound writes the reference bytes, so the refusal is the
  # range's
  case {expect, got} do
    {_, {:raised, why}} ->
      fail.("#{name}: the fixed save of #{v} raised instead of answering: #{why}")

    {:refused, {:error, :invalid_value}} ->
      IO.puts("ok #{name}")

    {:refused, other} ->
      fail.("#{name}: the fixed save of #{v} was not refused: #{inspect(other, limit: 8)}")

    {file, {:ok, bytes}} ->
      if bytes == File.read!(file),
        do: IO.puts("ok #{name}"),
        else: fail.("#{name}: the fixed save of #{v} is not #{Path.basename(file)}")

    {_, other} ->
      fail.("#{name}: the fixed save of #{v} did not write: #{inspect(other)}")
  end
end

if Process.get(:failed), do: System.halt(1), else: IO.puts("ok: the compressed range write corpus")
`

func TestElixirCompressedRangeWriteCorpus(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	elixirc := elixircBinary(t, elixirBin)
	dir, err := filepath.Abs(rangeWriteCorpusDir)
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(dir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows, names, refusedNames []string
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			t.Fatalf("cases.tsv:%d: %d fields, want 5", i+1, len(f))
		}
		name, path, value, expect := f[0], f[2], f[3], f[4]
		want := ":refused"
		if expect == "refused" {
			refusedNames = append(refusedNames, name)
		} else {
			want = fmt.Sprintf("%q", filepath.Join(dir, expect+".fixed.bin"))
		}
		var v float64
		if _, err := fmt.Sscan(value, &v); err != nil {
			t.Fatalf("%s: value %q: %v", name, value, err)
		}
		names = append(names, name)
		rows = append(rows, fmt.Sprintf("{%q, %s, %s, %s}", name, negZeroPath(t, path), elixirFloat(v), want))
	}
	if len(rows) == 0 || len(refusedNames) == 0 || len(refusedNames) == len(rows) {
		t.Fatalf("cases.tsv: %d rows, %d refused; want both kinds", len(rows), len(refusedNames))
	}

	files := generateElixir(t, loadUnit(t, filepath.Join(dir, "Probe.schema")))
	env := append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	body := strings.Replace(rangeWriteProbe, "[ROWS]", "[\n  "+strings.Join(rows, ",\n  ")+"\n]", 1)
	body = strings.Replace(body, "[BASE]", fmt.Sprintf("%q", filepath.Join(dir, "base.json")), 1)
	runProbe := func(files map[string][]byte) (string, error) {
		t.Helper()
		src, ebin := t.TempDir(), t.TempDir()
		var modules []string
		for name, data := range files {
			if strings.Contains(name, "/") || !strings.HasSuffix(name, ".ex") {
				continue
			}
			if err := os.WriteFile(filepath.Join(src, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
			modules = append(modules, name)
		}
		compile := testproc.Command(t, elixirc, append([]string{"--ignore-module-conflict", "-o", ebin}, sortedRuntimeFirst(modules)...)...)
		compile.Dir, compile.Env = src, env
		if out, err := compile.CombinedOutput(); err != nil {
			t.Fatalf("elixirc: %v\n%s", err, out)
		}
		probe := filepath.Join(t.TempDir(), "probe.exs")
		if err := os.WriteFile(probe, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		run := testproc.Command(t, elixirBin, "-pa", ebin, probe)
		run.Env = env
		out, err := run.CombinedOutput()
		return string(out), err
	}
	out, err := runProbe(files)
	if err != nil || !strings.Contains(out, "ok: the compressed range write corpus") {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	for _, name := range names {
		if !strings.Contains(out, "ok "+name+"\n") {
			t.Errorf("%s: the probe did not answer the row\n%s", name, out)
		}
	}
	t.Log(strings.TrimSpace(out))

	// REVERSED WITNESSES: the probe goes red, naming every refused row, when
	// the runtime's range check is planted out (the save writes the value),
	// and when the save raises instead of answering {:error, :invalid_value}.
	for _, plant := range []struct{ old, new, want string }{
		{"    if n < lo or n > hi do\n", "    if false and (n < lo or n > hi) do\n", "was not refused"},
		{"ArgumentError -> {:error, :invalid_value}", "ArgumentError -> reraise ArgumentError, __STACKTRACE__", "raised instead of answering"},
	} {
		planted := make(map[string][]byte, len(files))
		for name, data := range files {
			planted[name] = data
		}
		elixirPatch(t, planted, plant.old, plant.new)
		out, err := runProbe(planted)
		if err == nil {
			t.Errorf("REVERSED WITNESS FAILED: the plant %q left the probe green\n%s", plant.new, out)
			continue
		}
		for _, name := range refusedNames {
			_, line, _ := strings.Cut(out, "FAIL "+name+": ")
			line, _, _ = strings.Cut(line, "\n")
			if !strings.Contains(line, plant.want) {
				t.Errorf("REVERSED WITNESS FAILED: the plant %q did not turn %s red (%s)\n%s", plant.new, name, plant.want, out)
			}
		}
	}
}
