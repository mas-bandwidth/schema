package elixirtable

// I13 (docs/PORTING.md, "The text differential against a third
// implementation") on the Elixir leg. The leg emits N random float-only
// instances as (wire, text) through the generated Table.save and Table.to_json;
// the compiler's own engine (internal/tabletext and internal/tablepack, written
// from the spec and from neither backend) reads the same wire and writes its
// text, and the two texts are byte-compared; then the engine packs the leg's
// text back and that wire is byte-compared with the leg's wire.
//
// The pinned corpus never reaches a float tie, and a float tie is where text
// writers disagree, so the instances are drawn here from a splitmix64 stream.
// Non-finite floats have no JSON spelling and both writers refuse one, so the
// probe redraws a non-finite pattern (BEAM cannot hold one anyway). Instance 0
// is the all-defaults value.
//
// The probe's shape is the Go leg's (internal/codegen/gotable/
// textdifferential_test.go) with `table` where that one says `fixed table`:
// the fixed class is not on the Elixir table wire yet, and the variable class
// carries an unbounded list besides the bounded one.
//
// This test runs elixir, so it is a functional test: make
// tables-elixir-json-differential runs it under SCHEMA_REQUIRE_CORPUS=1, where
// a missing toolchain fails by name; without that variable and without Elixir
// it skips by name (elixirBinary).

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const elixirTextDifferentialSchema = `package textdiff
type Sample {
 s float32
 d float64
}
table Root {
 a float32
 b float64
 pair Sample
 many [..8]float32
 wide []float32
 deep []float64
}
`

const (
	elixirTextDifferentialSeed = 24845619678
	elixirTextDifferentialN    = 200
)

// elixirTextDifferentialProbe draws the instances and writes inst-<i>.wire and
// inst-<i>.json into dir. The directory, the seed and the count are spelled
// into the source, so the outer test owns them without touching the process
// environment (the tests run in parallel).
func elixirTextDifferentialProbe(dir string, seed uint64, n int) string {
	return `
defmodule TextDiffProbe do
  import Bitwise
  @mask 0xFFFFFFFFFFFFFFFF

  # splitmix64, the Go probe's generator, over unbounded integers masked to 64 bits.
  def next(state) do
    s = band(state + 0x9E3779B97F4A7C15, @mask)
    z = s
    z = band(bxor(z, bsr(z, 30)) * 0xBF58476D1CE4E5B9, @mask)
    z = band(bxor(z, bsr(z, 27)) * 0x94D049BB133111EB, @mask)
    {bxor(z, bsr(z, 31)), s}
  end

  # f32 redraws an all-ones exponent (an infinity or a NaN): neither has a JSON
  # spelling, and BEAM has no term for either.
  def f32(state) do
    {z, s} = next(state)
    bits = band(z, 0xFFFFFFFF)

    if band(bits, 0x7F800000) == 0x7F800000 do
      f32(s)
    else
      <<f::float-32>> = <<bits::32>>
      {f, s}
    end
  end

  def f64(state) do
    {z, s} = next(state)

    if band(z, 0x7FF0000000000000) == 0x7FF0000000000000 do
      f64(s)
    else
      <<f::float-64>> = <<z::64>>
      {f, s}
    end
  end

  def many(state, 0, _draw), do: {[], state}

  def many(state, k, draw) do
    {f, s} = draw.(state)
    {rest, s} = many(s, k - 1, draw)
    {[f | rest], s}
  end

  def instance(0, state), do: {%{}, state}

  def instance(_i, s) do
    {a, s} = f32(s)
    {b, s} = f64(s)
    {ps, s} = f32(s)
    {pd, s} = f64(s)
    {many, s} = many(s, 8, &f32/1)
    {wide, s} = many(s, 8, &f32/1)
    {deep, s} = many(s, 8, &f64/1)
    {%{"a" => a, "b" => b, "pair" => %{"s" => ps, "d" => pd}, "many" => many, "wide" => wide, "deep" => deep}, s}
  end

  def run(dir, seed, n) do
    Enum.reduce(0..(n - 1), seed, fn i, s ->
      {v, s} = instance(i, s)
      {:ok, wire} = Textdiff.Table.save("Root", v)
      {:ok, text} = Textdiff.Table.to_json("Root", v)
      File.write!(Path.join(dir, "inst-#{i}.wire"), wire)
      File.write!(Path.join(dir, "inst-#{i}.json"), text)
      s
    end)

    IO.puts("wrote #{n}")
  end
end

TextDiffProbe.run(` + elixirString(dir) + `, ` + strconv.FormatUint(seed, 10) + `, ` + strconv.Itoa(n) + `)
`
}

// runElixirTextDifferential generates the probe unit, applies an optional
// in-memory edit to the emitted files, runs the probe, then runs both
// directions of the engine comparison. It returns the divergence messages and
// a non-nil error when any instance differs, so the negative control asserts
// on the same output the row produces. A probe that fails to compile or run is
// fatal through runProbe, never a divergence.
func runElixirTextDifferential(t *testing.T, edit func(map[string][]byte)) ([]byte, error) {
	t.Helper()
	elixirBin := elixirBinary(t)
	u := unitFrom(t, elixirTextDifferentialSchema)
	files := generateElixir(t, u)
	if edit != nil {
		edit(files)
	}
	modDir := t.TempDir()
	names := writeModuleDir(t, modDir, files)
	out := t.TempDir()
	runProbe(t, elixirBin, modDir, names, elixirTextDifferentialProbe(out, elixirTextDifferentialSeed, elixirTextDifferentialN))
	m := tabletext.NewModel(u)
	msgs, err := compareElixirTextDifferential(m, out, elixirTextDifferentialN)
	if err != nil {
		return nil, err
	}
	if len(msgs) > 0 {
		return []byte(strings.Join(msgs, "\n\n")), fmt.Errorf("%d of %d instance(s) differ", len(msgs), elixirTextDifferentialN)
	}
	return nil, nil
}

// TestTextDifferentialElixir is the row: the generated Elixir float writer and
// the engine's agree, byte for byte, over 200 random float-only instances, in
// both directions.
func TestTextDifferentialElixir(t *testing.T) {
	t.Parallel()
	out, err := runElixirTextDifferential(t, nil)
	if err != nil {
		t.Fatalf("text differential: %v\n%s", err, out)
	}
}

// elixirTextDifferentialAnchor is the float32 digit range in the emitted
// json_float (variableform.go): six to nine significant digits, §16.7's %.*g
// loop over 32 bits.
const elixirTextDifferentialAnchor = "json_float_digits(4), do: {6, 9}"

// TestTextDifferentialNegativeControlElixir caps a float32's text at SIX
// significant digits, so a float32 that needs seven, eight or nine renders
// short and only the cross-implementation comparison sees it. The gate must go
// red on the text comparison, not on a compile error (a compile error is fatal
// in runProbe and fails this test instead).
func TestTextDifferentialNegativeControlElixir(t *testing.T) {
	t.Parallel()
	out, err := runElixirTextDifferential(t, func(files map[string][]byte) {
		total := 0
		for _, data := range files {
			total += strings.Count(string(data), elixirTextDifferentialAnchor)
		}
		if total != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage anchor %q appeared %d times across the emitted files, want 1", elixirTextDifferentialAnchor, total)
		}
		elixirPatch(t, files, elixirTextDifferentialAnchor, "json_float_digits(4), do: {6, 6} # SABOTAGED")
	})
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: a float32 writer capped at six significant digits left the gate GREEN\n%s", out)
	}
	if !bytes.Contains(out, []byte("leg text and engine text differ")) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on the text comparison: %v\n%s", err, out)
	}
	t.Logf("control red as required: %v", err)
}

// elixirPatch replaces one substring across every generated file and refuses a
// control whose sabotage patched nothing.
func elixirPatch(t *testing.T, files map[string][]byte, old, new string) {
	t.Helper()
	n := 0
	for name, data := range files {
		s := string(data)
		if !strings.Contains(s, old) {
			continue
		}
		files[name] = []byte(strings.ReplaceAll(s, old, new))
		n++
	}
	if n == 0 {
		t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage %q patched nothing", old)
	}
}

// compareElixirTextDifferential runs both directions for every instance the
// probe wrote. Direction one: the engine reads the leg's wire and writes its
// text, byte-compared with the leg's text. Direction two: the engine packs the
// leg's text back to wire, byte-compared with the leg's wire. Divergences are
// collected, not fatal on the first.
func compareElixirTextDifferential(m *tabletext.Model, dir string, n int) ([]string, error) {
	var msgs []string
	for i := range n {
		wire, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.wire", i)))
		if err != nil {
			return msgs, fmt.Errorf("read inst-%d.wire: %w", i, err)
		}
		legJSON, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.json", i)))
		if err != nil {
			return msgs, fmt.Errorf("read inst-%d.json: %w", i, err)
		}

		d1, err := os.MkdirTemp(dir, "d1-")
		if err != nil {
			return msgs, err
		}
		report, err := tablepack.UnpackOneFile(m, "Root", wire, d1)
		if err != nil {
			return msgs, fmt.Errorf("instance %d: UnpackOneFile: %w", i, err)
		}
		if report != (tabletext.Report{}) {
			return msgs, fmt.Errorf("instance %d: UnpackOneFile report not clean: %+v", i, report)
		}
		engineJSON, err := os.ReadFile(filepath.Join(d1, "Root.json"))
		if err != nil {
			return msgs, fmt.Errorf("instance %d: read engine Root.json: %w", i, err)
		}
		if !bytes.Equal(engineJSON, legJSON) {
			off, win := elixirTextWindow(legJSON, engineJSON)
			msgs = append(msgs, fmt.Sprintf("instance %d: leg text and engine text differ at byte %d\n%s\nfloats: %s", i, off, win, elixirFloatBitsOf(m, wire)))
			continue
		}

		d2, err := os.MkdirTemp(dir, "d2-")
		if err != nil {
			return msgs, err
		}
		if err := os.WriteFile(filepath.Join(d2, "Root.json"), legJSON, 0o600); err != nil {
			return msgs, err
		}
		wireBack, _, report2, err := tablepack.Pack(m, "Root", d2)
		if err != nil {
			return msgs, fmt.Errorf("instance %d: Pack: %w", i, err)
		}
		if report2 != (tabletext.Report{}) {
			return msgs, fmt.Errorf("instance %d: Pack report not clean: %+v", i, report2)
		}
		if !bytes.Equal(wireBack, wire) {
			msgs = append(msgs, fmt.Sprintf("instance %d: engine wire packed from leg text differs from leg wire: engine=%x leg=%x", i, wireBack, wire))
		}
	}
	return msgs, nil
}

// elixirFloatBitsOf decodes one wire with the engine and spells every float's
// bits, so a divergence replays from the bits alone.
func elixirFloatBitsOf(m *tabletext.Model, wire []byte) string {
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if _, err := tablewire.Decode(m, inst, wire, &report); err != nil {
		return fmt.Sprintf("<decode error: %v>", err)
	}
	var b strings.Builder
	var walk func(fields []tabletext.Field, prefix string)
	walk = func(fields []tabletext.Field, prefix string) {
		for i := range fields {
			f := &fields[i]
			name := prefix + f.Def.Name
			emit := func(c tabletext.Cell, slot string) {
				switch f.Def.Type.Kind {
				case ir.TFloat32:
					fmt.Fprintf(&b, "%s=0x%08x(f32) ", slot, math.Float32bits(float32(c.F)))
				case ir.TFloat64:
					fmt.Fprintf(&b, "%s=0x%016x(f64) ", slot, math.Float64bits(c.F))
				}
			}
			if len(f.Elems) > 0 {
				for j := 0; j < f.Count && j < len(f.Elems); j++ {
					c := f.Elems[j]
					if c.Tab != nil {
						walk(c.Tab.Fields, fmt.Sprintf("%s[%d].", name, j))
					} else {
						emit(c, fmt.Sprintf("%s[%d]", name, j))
					}
				}
				continue
			}
			if f.Cell.Tab != nil {
				walk(f.Cell.Tab.Fields, name+".")
				continue
			}
			emit(f.Cell, name)
		}
	}
	walk(inst.Fields, "")
	return strings.TrimSpace(b.String())
}

// elixirTextWindow returns the first byte offset where two texts differ and a
// window of each around it.
func elixirTextWindow(a, b []byte) (int, string) {
	n := min(len(b), len(a))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	lo := max(i-32, 0)
	hiA := min(i+32, len(a))
	hiB := min(i+32, len(b))
	return i, fmt.Sprintf("leg[%d:%d]=%q engine[%d:%d]=%q", lo, hiA, a[lo:hiA], lo, hiB, b[lo:hiB])
}
