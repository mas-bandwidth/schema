package elixirtable

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A COMPRESSED FLOAT'S -0 IS WRITTEN AS +0 BY EVERY ELIXIR WRITER
// (docs/SPEC-TABLES.md §3.3; docs/rulings.json fixed-compressed-negzero): the
// variable save and its retaining twin, the fixed save, the cook and the text
// writer store the bits +0 stores, in every build; a plain float's -0 is
// carried bit-exact. TestElixirCompressedNegZeroCorpus runs the writers over
// the compressed -0 corpus under the Elixir toolchain; this is its fast twin,
// which reads the generated source rather than running it: every clause that
// stores a float hands the value through stored_float (the variable unit) or
// cf32_bits (the fixed save), and the plain float's descriptor and fixed
// segment carry neither (the control).

// negZeroCorpusSchema is the corpus's own unit, read from its committed file.
const negZeroCorpusSchema = "../../../testdata/conformance/feature-witnesses/compressed-negzero-go/Probe.schema"

// negZeroFloatWriters are the clause heads that store a float, each with the
// number of clauses the Probe unit's Table.ex carries.
var negZeroFloatWriters = []struct {
	name string
	head *regexp.Regexp
	call string
}{
	{"the variable save's scalar leaf", regexp.MustCompile(`^  defp emit_field\(%\{t: :float\}`), "stored_float("},
	{"the variable save's arrays", regexp.MustCompile(`^  defp emit_field\(%\{t: :list\} = f, value, idx_map`), "stored_floats("},
	{"the variable save's keyed slot", regexp.MustCompile(`^  defp keyed_elem\(item, v, idx_map`), "stored_float("},
	{"the retaining save's keyed slot", regexp.MustCompile(`^  defp keyed_elem_retain\(item, v, \{`), "stored_float("},
	{"the variable save's union arm", regexp.MustCompile(`^  defp arm_content\(%\{t: :float\}`), "stored_float("},
	{"the variable save's array element", regexp.MustCompile(`^  defp emit_element\(%\{elem_kind: kind\}.*when kind in \[10, 11\]`), "stored_float("},
	{"the text writer's leaf, slot and arm", regexp.MustCompile(`^  defp json_field\(%\{t: :float\}`), "stored_float("},
	{"the text writer's array element", regexp.MustCompile(`^  defp json_list_elem\(%\{elem_kind: kind\}.*when kind in \[10, 11\]`), "stored_float("},
	{"the cook's leaf, slot and arm", regexp.MustCompile(`^  defp cook_value\(%\{t: :float,`), "stored_float("},
	{"the cook's array element", regexp.MustCompile(`^  defp cook_elem\(%\{elem_t: :float,`), "stored_float("},
}

// clauseAt is the clause starting at line i: up to the next two-space def.
func clauseAt(lines []string, i int) string {
	end := i + 1
	for end < len(lines) && !strings.HasPrefix(lines[end], "  def") {
		end++
	}
	return strings.Join(lines[i:end], "\n")
}

func TestElixirCompressedFloatStoresAreGuarded(t *testing.T) {
	t.Parallel()
	files := generateElixir(t, loadUnit(t, filepath.FromSlash(negZeroCorpusSchema)))
	table := string(files["Table.ex"])
	if table == "" {
		t.Fatal("the unit emitted no Table.ex")
	}
	lines := strings.Split(table, "\n")
	for _, w := range negZeroFloatWriters {
		found := 0
		for i, line := range lines {
			if !w.head.MatchString(line) {
				continue
			}
			found++
			if body := clauseAt(lines, i); !strings.Contains(body, w.call) {
				t.Errorf("Table.ex:%d: %s stores a compressed float's -0 as held (no %s):\n%s", i+1, w.name, w.call, body)
			}
		}
		if found == 0 {
			t.Errorf("Table.ex: no clause for %s (%s)", w.name, w.head)
		}
	}
	for _, want := range []string{
		"defp stored_float(%{min: _}, v) when is_float(v) and v == 0.0, do: 0.0",
		"defp stored_float(%{elem_min: _}, v) when is_float(v) and v == 0.0, do: 0.0",
		"defp stored_float(_f, v), do: v",
	} {
		if !strings.Contains(table, want) {
			t.Errorf("Table.ex does not carry %q", want)
		}
	}
	// THE CONTROL: the plain float p's descriptor carries no range, so
	// stored_float passes its -0 through bit-exact.
	if i := strings.Index(table, "non_finite: \"Var.p\""); i < 0 {
		t.Error("Table.ex: no descriptor for Var.p")
	} else if start := strings.LastIndex(table[:i], "%{"); strings.Contains(table[start:i], "min:") {
		t.Errorf("Table.ex: the plain float's descriptor carries a range: %s", table[start:i])
	}

	// THE FIXED SAVE: every compressed cell (a, z, xs, ys, ks and the arm v)
	// rides f32_ranged, whose store is cf32_bits, and only the plain p rides
	// f32_bits.
	fixed := string(files["ProbeFixed.ex"])
	if got := strings.Count(fixed, "R.f32_ranged("); got != 6 {
		t.Errorf("ProbeFixed.ex: %d compressed stores through f32_ranged, want 6 (a, z, xs, ys, ks, pick.v)", got)
	}
	if got := strings.Count(fixed, "R.f32_bits("); got != 1 || !strings.Contains(fixed, "R.f32_bits(v_p)") {
		t.Errorf("ProbeFixed.ex: %d stores through f32_bits, want exactly the plain float's (v_p)", got)
	}
	runtime := string(files["FixedRuntime.ex"])
	if !strings.Contains(runtime, "def f32_ranged(value, {lo, hi}) do\n    bits = cf32_bits(value)\n") {
		t.Error("FixedRuntime.ex: f32_ranged does not store through cf32_bits")
	}
	if !strings.Contains(runtime, "def cf32_bits(value) when is_float(value) and value == 0.0, do: 0") ||
		!strings.Contains(runtime, "def cf64_bits(value) when is_float(value) and value == 0.0, do: 0") {
		t.Error("FixedRuntime.ex does not store a compressed float's -0 as +0 (cf32_bits, cf64_bits)")
	}
}
