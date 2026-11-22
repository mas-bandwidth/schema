package elixirtable

// TestElixirCompressedF32EndsCorpus runs the cross-port corpus for A
// COMPRESSED FLOAT'S RANGE IS HELD AT FLOAT32 (docs/SPEC-TABLES.md §3.3) on the
// generated Elixir: testdata/conformance/feature-witnesses/compressed-f32-ends.
// Every cases.tsv row goes through the generated unit in one BEAM: a read row
// loads the variable or fixed file (or the JSON) under its root and, where it
// owes acceptance, saves the value again in the root's own form; a write row
// loads ends.json, sets one float to the bits the row names in the native
// value and saves the row's form. The probe prints one line per row and the
// test holds each verdict, and each saved file byte for byte, to the row
// (internal/tablewire's TestCompressedF32EndsCorpus is the reference runner).
//
// It compiles and runs the generated unit, so it is a functional test: without
// SCHEMA_SLOW it skips by name.
import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const elixirF32EndsCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-f32-ends"

type elixirF32EndsRow struct {
	name, op, form, root, input, path, bits, expect string
}

func elixirF32EndsRows(t *testing.T) []elixirF32EndsRow {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(elixirF32EndsCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []elixirF32EndsRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 8 {
			t.Fatalf("cases.tsv:%d: %d fields, want 8", i+1, len(f))
		}
		rows = append(rows, elixirF32EndsRow{f[0], f[1], f[2], f[3], f[4], f[5], f[6], f[7]})
	}
	return rows
}

func TestElixirCompressedF32EndsCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	corpus, err := filepath.Abs(elixirF32EndsCorpusDir)
	if err != nil {
		t.Fatal(err)
	}
	files := generateElixir(t, loadUnit(t, filepath.Join(corpus, "Probe.schema")))
	rows := elixirF32EndsRows(t)
	formOf := map[string]string{"V": "variable", "F": "fixed"}

	var p strings.Builder
	p.WriteString(`defmodule F32EndsProbe do
  alias F32ends.Table, as: T

  def hex(bin), do: Base.encode16(bin, case: :lower)

  def f32(bits) do
    <<v::float-32>> = <<bits::unsigned-32>>
    v
  end

  def set(v, "r", x), do: Map.put(v, "r", x)
  def set(v, "xs[" <> rest, x) do
    {i, "]"} = Integer.parse(rest)
    Map.update!(v, "xs", &List.replace_at(&1, i, x))
  end

  def say(name, {:ok, bin}), do: IO.puts("ROW #{name} accepted #{hex(bin)}")
  def say(name, other), do: IO.puts("ROW #{name} refused #{inspect(other)}")

  def again(name, root, {:ok, v, _report}), do: say(name, safe(fn -> T.save(root, v) end))
  def again(name, _root, other), do: IO.puts("ROW #{name} refused #{inspect(elem(other, 1))}")

  def write(name, root, json, path, bits) do
    case safe(fn -> T.from_json(root, File.read!(json)) end) do
      {:ok, v, _} -> say(name, safe(fn -> T.save(root, set(v, path, f32(bits))) end))
      other -> IO.puts("ROW #{name} loadfail #{inspect(other)}")
    end
  end

  def safe(f) do
    try do
      f.()
    rescue
      e -> {:raised, Exception.message(e)}
    catch
      k, e -> {:caught, k, e}
    end
  end
end
`)
	for _, r := range rows {
		in := filepath.Join(corpus, r.input)
		switch {
		case r.op == "read" && r.form == "json":
			fmt.Fprintf(&p, "F32EndsProbe.again(%q, %q, F32EndsProbe.safe(fn -> F32ends.Table.from_json(%q, File.read!(%q)) end))\n", r.name, r.root, r.root, in+".json")
		case r.op == "read":
			if formOf[r.root] != r.form {
				t.Fatalf("%s: form %s under root %s", r.name, r.form, r.root)
			}
			fmt.Fprintf(&p, "F32EndsProbe.again(%q, %q, F32EndsProbe.safe(fn -> F32ends.Table.load(%q, File.read!(%q)) end))\n", r.name, r.root, r.root, in+"."+r.form+".bin")
		case r.op == "write":
			if formOf[r.root] != r.form {
				t.Fatalf("%s: form %s under root %s", r.name, r.form, r.root)
			}
			fmt.Fprintf(&p, "F32EndsProbe.write(%q, %q, %q, %q, 0x%s)\n", r.name, r.root, in+".json", r.path, r.bits)
		default:
			t.Fatalf("%s: op %q", r.name, r.op)
		}
	}
	out, err := runArmProbe(t, elixirBinary(t), files, p.String())
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	got := map[string][2]string{}
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.SplitN(strings.TrimSpace(line), " ", 4)
		if len(f) >= 3 && f[0] == "ROW" {
			rest := ""
			if len(f) == 4 {
				rest = f[3]
			}
			got[f[1]] = [2]string{f[2], rest}
		}
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			g, ok := got[r.name]
			if !ok {
				t.Fatalf("the probe printed no verdict for this row:\n%s", out)
			}
			if g[0] == "loadfail" {
				t.Fatalf("%s.json: the reader refuses it: %s", r.input, g[1])
			}
			if r.expect == "refused" {
				if g[0] != "refused" {
					t.Errorf("not refused: saved %s", g[1])
				}
				return
			}
			if g[0] != "accepted" {
				t.Fatalf("refused: %s", g[1])
			}
			form := formOf[r.root]
			want, err := os.ReadFile(filepath.Join(corpus, r.expect+"."+form+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			if have, err := hex.DecodeString(g[1]); err != nil || !bytes.Equal(have, want) {
				t.Errorf("the %s save:\n have %s\n want %s", form, g[1], hex.EncodeToString(want))
			}
		})
	}
}
