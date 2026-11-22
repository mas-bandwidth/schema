package elixirtable

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// THE COMPRESSED -0 WRITER CORPUS ON THE ELIXIR LEG (docs/SPEC-TABLES.md §3.3;
// docs/rulings.json fixed-compressed-negzero; the corpus's own cases.tsv says
// what each row owes). Each row loads <base>.json through the generated text
// read, sets <path> to -0 IN THE NATIVE VALUE, saves <form> and owes
// <expect>.<form>.bin byte for byte. Beside it, every row holds the leg's other
// writers to the +0 twin: the same value with the cell at +0 writes the same
// text, the same cook in both byte orders, the same retaining save (a variable
// root) and the same message batch (the fixed root's own cells); the plain
// float p is the control, whose -0 differs from its +0 in every one.
//
// One row has no Elixir writer: var_fixed_table_leaf saves the fixed root Fix
// in the variable form, and no write at a declared fixed root writes form 1
// here (docs/SPEC-TABLES.md §3.4; Table.save("Fix", _) is the fixed save). The
// probe names it and the test requires exactly that row, and no other, skipped.
//
// It compiles and runs the generated unit under the Elixir toolchain, so it is
// a FUNCTIONAL test; `make conformance-elixir-compressed-negzero` runs it in the
// Elixir leg (make/elixir-compressed-negzero.mk).

const negZeroCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-negzero-go"

// negZeroCorpusProbe is the Elixir side; [ROWS] is the corpus.
const negZeroCorpusProbe = `
mod = Nzgo.Table
msg = Nzgo.TableMessage
<<nz::float>> = <<0x8000000000000000::64>>
<<pz::float>> = <<0::64>>
true = nz == pz and <<nz::float>> != <<pz::float>>

fail = fn what ->
  IO.puts("FAIL " <> what)
  Process.put(:failed, true)
end

set = fn
  value, {:field, f}, z -> Map.put(value, f, z)
  value, {:index, f, i}, z ->
    list = Map.get(value, f) || []
    list = list ++ List.duplicate(0.0, max(0, i + 1 - length(list)))
    Map.put(value, f, List.replace_at(list, i, z))
  value, {:key, f, k}, z -> Map.put(value, f, Map.put(Map.get(value, f) || %{}, k, z))
  value, {:arm, f, a}, z -> Map.put(value, f, {a, z})
end

cell = fn value, path -> Map.get(value, elem(path, 1)) end

retain = %{records: [], used: 0, cap: 1_048_576, ids: [], id_cap: 131_072}
report = %{unknown: 0, kind_mismatch: 0, widened: 0, clamped: 0, malformed: false, retained: 0, retain_lost: 0}

{:ok, announced} = msg.announce_read(msg.announce())
{:ok, vocab} = msg.for_root(announced, "Fix")

cook = fn root, v, order ->
  {size, :ok} = mod.cook_measure_reason(root, v)
  mod.cook_from(root, v, :binary.copy(<<0>>, size), order)
end

writers = fn root, path ->
  [
    {"json", fn v -> mod.to_json(root, v) end},
    {"cook-little", fn v -> cook.(root, v, :little) end},
    {"cook-big", fn v -> cook.(root, v, :big) end}
  ] ++
    if root == "Fix" do
      case elem(path, 1) do
        f when f in ["a", "z", "xs", "ys", "p"] ->
          ref = Enum.find_index(["a", "z", "xs", "ys", "ks", "pick", "p"], &(&1 == f)) + 1
          [{"message", fn v -> msg.save_messages(vocab, [[{ref, cell.(v, path)}]]) end}]
        _ -> []
      end
    else
      [{"save-retain", fn v -> mod.save_retain(root, v, retain, report) end}]
    end
end

for {name, form, root, base, path, expect, plain, skip} <- [ROWS] do
  {:ok, value, _} = mod.from_json(root, File.read!(base))
  minus = set.(value, path, nz)
  plus = set.(value, path, pz)

  if skip do
    IO.puts("skip #{name}: a fixed root writes no variable form")
  else
    got = mod.save(root, minus)
    want = File.read!(expect)

    if got == {:ok, want},
      do: IO.puts("ok #{name}"),
      else: fail.("#{name}: the #{form} save of -0 is not its expect: #{inspect(got)} want #{inspect(want)}")
  end

  for {writer, write} <- writers.(root, path) do
    m = write.(minus)
    p = write.(plus)

    cond do
      plain and m == p -> fail.("#{name} #{writer}: a plain float's -0 did not ride: #{inspect(m)}")
      not plain and m != p -> fail.("#{name} #{writer}: -0 was not written as +0:\n -0 #{inspect(m)}\n +0 #{inspect(p)}")
      true -> IO.puts("ok #{name} #{writer}")
    end
  end
end

if Process.get(:failed), do: System.halt(1), else: IO.puts("ok: the compressed -0 corpus")
`

// negZeroPath is a corpus path as the probe's set takes it.
func negZeroPath(t *testing.T, path string) string {
	t.Helper()
	if name, arm, ok := strings.Cut(path, "."); ok {
		return fmt.Sprintf("{:arm, %q, %q}", name, arm)
	}
	name, index, ok := strings.Cut(path, "[")
	if !ok {
		return fmt.Sprintf("{:field, %q}", name)
	}
	index = strings.TrimSuffix(index, "]")
	if n, err := strconv.Atoi(index); err == nil {
		return fmt.Sprintf("{:index, %q, %d}", name, n)
	}
	return fmt.Sprintf("{:key, %q, %q}", name, index)
}

func TestElixirCompressedNegZeroCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)
	elixirc := elixircBinary(t, elixirBin)
	dir, err := filepath.Abs(negZeroCorpusDir)
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(dir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows, names []string
	skipped := ""
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("cases.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		name, form, root, base, path, expect := f[0], f[1], f[2], f[3], f[4], f[5]
		skip := form == "variable" && root == "Fix"
		if skip {
			skipped += name + " "
		}
		names = append(names, name)
		rows = append(rows, fmt.Sprintf("{%q, %q, %q, %q, %s, %q, %t, %t}", name, form, root,
			filepath.Join(dir, base+".json"), negZeroPath(t, path),
			filepath.Join(dir, expect+"."+form+".bin"), path == "p", skip))
	}
	if len(rows) != 15 || skipped != "var_fixed_table_leaf " {
		t.Fatalf("cases.tsv: %d rows, skipped %q; want 15 and only var_fixed_table_leaf", len(rows), skipped)
	}

	files := generateElixir(t, loadUnit(t, filepath.Join(dir, "Probe.schema")))
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
	env := append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	compile := testproc.Command(t, elixirc, append([]string{"--ignore-module-conflict", "-o", ebin}, sortedRuntimeFirst(modules)...)...)
	compile.Dir, compile.Env = src, env
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("elixirc: %v\n%s", err, out)
	}
	probe := filepath.Join(t.TempDir(), "probe.exs")
	body := strings.Replace(negZeroCorpusProbe, "[ROWS]", "[\n  "+strings.Join(rows, ",\n  ")+"\n]", 1)
	if err := os.WriteFile(probe, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	run := testproc.Command(t, elixirBin, "-pa", ebin, probe)
	run.Env = env
	out, err := run.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok: the compressed -0 corpus") {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	// every row answered: its save (or its named skip) and its writer twins
	for _, name := range names {
		if !strings.Contains(string(out), "ok "+name+"\n") && !strings.Contains(string(out), "skip "+name+":") {
			t.Errorf("%s: the probe did not answer the row\n%s", name, out)
		}
		if !strings.Contains(string(out), "ok "+name+" json\n") || !strings.Contains(string(out), "ok "+name+" cook-big\n") {
			t.Errorf("%s: the probe did not hold the text and cook writers\n%s", name, out)
		}
	}
	t.Log(strings.TrimSpace(string(out)))
}
