package elixirtable

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestDamageDiscardsTheWholeLoadElixir holds docs/SPEC-TABLES.md §4, DAMAGE
// DISCARDS THE WHOLE LOAD, at the Elixir leg's read entry points: every framing
// vector the conformance manifest answers `malformed`
// (testdata/conformance/tables/MANIFEST.txt, docs/LAYERS/1-framing.md) is
// loaded through the plain read and the retaining read, and each must return
// NO VALUE: `{:error, :malformed, report}` with the report malformed, and the
// retaining read's stores emptied. A damaged text refuses the text read the
// same way. (The message batch's whole-batch discard is held by
// TestMessageFormBatchElixir; the fixed form refuses every damage by name.)
//
// Its control, tables-elixir-discard-negative-control, removes the plain
// read's discard from a copy of the emitter and requires this test red.
func TestDamageDiscardsTheWholeLoadElixir(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	// The framing rows this gate holds are the DAMAGE verdicts on VARIABLE
	// roots: the strict read discards the whole load as a malformed refusal,
	// and a fixed root refuses a form-1 file `previous_form` before any damage
	// is met, so those rows owe a different refusal and belong to the fixed
	// form's gates. The manifest spells the verdict `refused` for both.
	units := map[string]struct{ module, dir string }{
		"listdemo":  {"Listdemo.Table", filepath.Join("tables", "lists")},
		"graphdemo": {"Graphdemo.Table", filepath.Join("tables", "pointers")},
	}
	rows := map[string][][3]string{} // unit -> {case, root, wire}
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		w := strings.Fields(sc.Text())
		if len(w) >= 6 && w[0] == "framing" && w[5] == "refused" {
			if _, ok := units[w[2]]; !ok {
				continue
			}
			rows[w[2]] = append(rows[w[2]], [3]string{w[1], w[3], filepath.Join(root, w[4])})
		}
	}
	f.Close()
	total := 0
	for _, r := range rows {
		total += len(r)
	}
	if total < 20 {
		t.Fatalf("the manifest gave %d damaged framing rows on variable roots; the scan, not the rule, is what broke", total)
	}
	names := make([]string, 0, len(rows))
	for n := range rows {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, unit := range names {
		t.Run(unit, func(t *testing.T) {
			t.Parallel()
			var paths []string
			if units[unit].dir == "" {
				paths = []string{filepath.Join(root, "test", "tables", "R1.schema")}
			} else {
				paths, _ = filepath.Glob(filepath.Join(root, units[unit].dir, "*.schema"))
			}
			u := loadUnit(t, paths...)
			variable := ir.VariableTables(u)
			var varRows [][3]string
			for _, r := range rows[unit] {
				if variable[r[1]] {
					varRows = append(varRows, r)
				}
			}
			if len(varRows) == 0 {
				t.Fatalf("unit %s names no variable-root framing damage in the manifest", unit)
			}
			files := generateElixir(t, u)
			var probe strings.Builder
			probe.WriteString(`
defmodule Probe do
  def fail(what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end

  def retain, do: %{records: [], used: 0, cap: 1_048_576, ids: [], id_cap: 131_072}

  def report,
    do: %{unknown: 0, kind_mismatch: 0, widened: 0, clamped: 0, malformed: false, retained: 0, retain_lost: 0}
end
`)
			for _, r := range varRows {
				fmt.Fprintf(&probe, `
data = File.read!(%q)

case %s.variable_load(%q, data) do
  {:error, :malformed, %%{reason: "wire_damaged"}} -> :ok
  other -> Probe.fail(%q <> ": the plain read handed back " <> inspect(other, limit: 8))
end
`, r[2], units[unit].module, r[1], r[0])
			}
			probe.WriteString("IO.puts(\"ok\")\n")
			if out, err := runArmProbe(t, elixirBin, files, probe.String()); err != nil || !strings.Contains(out, "ok") {
				t.Fatalf("damage did not discard the whole load: %v\n%s", err, out)
			}
		})
	}
}
