package elixirtable

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestOptionalVariableClosureElixir holds docs/SPEC-TABLES.md §2.3's `?T` and
// by-value `?[N]T` over a VARIABLE closure (a member whose closure holds a
// pointer) on the Elixir variable form, BYTE FOR BYTE against the Go oracle
// (internal/tablewire): every walk that follows pointer edges gates on
// presence, so an absent value's table is not descended and a pointer it would
// hold costs no record.
//
// In this target an absent optional IS nil, so the rule is kept by
// representation. Each case is saved from a value the probe BUILDS, and the
// oracle's own wire is loaded and saved again; both must be the oracle's
// bytes. A loaded absent optional is a key holding nil, which is the shape the
// node walk must step over.
//
// THE REVERSED WITNESS plants the old walk back (a nested table read with
// `Map.get(value, f.name, %{})`, which hands nil to the walk): the reload of
// the absent case must then go red.
func TestOptionalVariableClosureElixir(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)

	u := unitFrom(t, optionalVariableProbeSrc)
	m := tabletext.NewModel(u)
	outer := m.Lookup("Outer")

	var probe strings.Builder
	probe.WriteString(`
defmodule Probe do
  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end

  def hex(bin), do: Base.encode16(bin, case: :lower)
end
`)
	for _, pc := range optionalVariableProbeCases {
		inst := m.New(outer)
		var rep tabletext.Report
		if !m.Read(inst, []byte(pc.text), &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("%s: the oracle did not read the probe text cleanly: %+v", pc.name, rep)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: the oracle did not encode: %v", pc.name, err)
		}
		want := hex.EncodeToString(wire)
		fmt.Fprintf(&probe, `
{:ok, built} = Ovprobe.Table.save("Outer", %s)
Probe.check(Probe.hex(built) == %q, %q <> " built: " <> Probe.hex(built))
{:ok, loaded, _report} = Ovprobe.Table.load("Outer", Base.decode16!(%q, case: :lower))
{:ok, again} = Ovprobe.Table.save("Outer", loaded)
Probe.check(Probe.hex(again) == %q, %q <> " reloaded: " <> Probe.hex(again))
`, pc.build, want, pc.name, want, want, pc.name)
	}
	probe.WriteString("IO.puts(\"ok\")\n")

	files := generateElixir(t, u)
	if out, err := runArmProbe(t, elixirBin, files, probe.String()); err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("the Elixir variable form does not keep §2.3 over a variable closure: %v\n%s", err, out)
	}

	// the reversed witness
	const fixed = "          case Map.get(value, f.name) do\n            nil -> acc\n            v -> collect_nodes(Map.fetch!(@tables, f.elem), v, acc)\n          end\n"
	const old = "          collect_nodes(Map.fetch!(@tables, f.elem), Map.get(value, f.name, %{}), acc)\n"
	planted := map[string][]byte{}
	n := 0
	for name, data := range files {
		s := string(data)
		n += strings.Count(s, fixed)
		planted[name] = []byte(strings.ReplaceAll(s, fixed, old))
	}
	if n == 0 {
		t.Fatal("the witness found no presence-gated table edge in the generated walk to plant the old one over")
	}
	if out, err := runArmProbe(t, elixirBin, planted, probe.String()); err == nil {
		t.Fatalf("the walk with nil handed to it still passed the probe: the probe does not see the rule\n%s", out)
	}
}

const optionalVariableProbeSrc = `package ovprobe

table Inner
{
    text *string
    n    int32
}

table Outer
{
    inner ?Inner
    row   ?[2]Inner
    tail  int32
}
`

// optionalVariableProbeCases are the probe's values: the oracle's text and the
// Elixir term that builds the same value directly.
var optionalVariableProbeCases = []struct{ name, text, build string }{
	{"absent", `{"tail":5}`, `%{"tail" => 5}`},
	{"present", `{"inner":{"text":"hi","n":3},"row":[{"text":"a"},{"n":7}],"tail":5}`,
		`%{"inner" => %{"text" => "hi", "n" => 3}, "row" => [%{"text" => "a"}, %{"n" => 7}], "tail" => 5}`},
	{"empty", `{"inner":{"text":""},"tail":5}`, `%{"inner" => %{"text" => ""}, "tail" => 5}`},
	{"row_absent", `{"inner":{"text":"x"},"tail":1}`, `%{"inner" => %{"text" => "x"}, "tail" => 1}`},
}

// TestScalarListUnderATableHeaderElixir pins a wire fuzzer escape (pass kind,
// tabledemo.WideBlob): a SCALAR list whose element-kind byte reads 13, the
// table kind, must not be measured as a list of tables. The region measure
// (load_measure) descended into table elements by the wire's byte alone and
// raised a KeyError on the scalar list's descriptor, which has no element
// table; the engine (internal/tablewire extent.go) places nothing for a header
// whose element kind is not the declared one, and the read answers it as a
// kind mismatch. Both calls must return, never raise.
//
// THE REVERSED WITNESS takes the declared-kind guard back out and the probe
// must go red.
func TestScalarListUnderATableHeaderElixir(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)

	u := unitFrom(t, scalarListProbeSrc)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Holder"))
	var rep tabletext.Report
	if !m.Read(inst, []byte(`{"samples":[1,2,3],"tail":9}`), &rep) {
		t.Fatal("the oracle did not read the probe text")
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	// the list payload is the element-kind byte (7, uint16) then the count (3)
	at := strings.Index(string(wire), "\x07\x03")
	if at < 0 {
		t.Fatalf("no uint16 list header in the probe wire %x", wire)
	}
	mutant := append([]byte{}, wire...)
	mutant[at] = 13
	probe := fmt.Sprintf(`
data = Base.decode16!(%q, case: :lower)
measure = Slprobe.Table.load_measure("Holder", data)
true = is_integer(measure)
case Slprobe.Table.load("Holder", data) do
  {:ok, _v, _r} -> :ok
  {:error, _reason, _r} -> :ok
end
IO.puts("ok")
`, hex.EncodeToString(mutant))

	files := generateElixir(t, u)
	if out, err := runArmProbe(t, elixirBin, files, probe); err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("a scalar list under a table header escaped the Elixir reader: %v\n%s", err, out)
	}

	const guard = "<<13, count_rest::binary>> when field.elem_kind == 13 ->"
	planted := map[string][]byte{}
	n := 0
	for name, data := range files {
		n += strings.Count(string(data), guard)
		planted[name] = []byte(strings.ReplaceAll(string(data), guard, "<<13, count_rest::binary>> ->"))
	}
	if n == 0 {
		t.Fatal("the witness found no declared-kind guard in the generated measure")
	}
	if out, err := runArmProbe(t, elixirBin, planted, probe); err == nil {
		t.Fatalf("the measure without its declared-kind guard still passed: the probe does not see the rule\n%s", out)
	}
}

const scalarListProbeSrc = `package slprobe

table Holder
{
    samples []uint16
    tail    int32
}
`

// TestCountPastTheBoundClampsBeforeReadingElixir pins a wire fuzzer divergence
// on clean input (pass count, tabledemo.WideBlob): a bounded array whose count
// is past its declared bound reads the first bound elements and counts one
// clamped (docs/SPEC-TABLES.md §4), and the elements past the bound are never
// asked of the body, so a body holding only the bound's elements is NOT damage.
// The answer is the oracle's own (internal/tablewire Decode).
//
// THE REVERSED WITNESS reads the whole wire count before clamping, and the
// probe must go red (the leg then runs off the body and reports malformed).
func TestCountPastTheBoundClampsBeforeReadingElixir(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)

	u := unitFrom(t, boundedListProbeSrc)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Holder"))
	var rep tabletext.Report
	if !m.Read(inst, []byte(`{"samples":[1,2],"tail":9}`), &rep) {
		t.Fatal("the oracle did not read the probe text")
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	at := strings.Index(string(wire), "\x07\x02")
	if at < 0 {
		t.Fatalf("no uint16 list header in the probe wire %x", wire)
	}
	mutant := append([]byte{}, wire...)
	mutant[at+1] = 100 // the count, past the bound of 2, one LEB byte
	var orep tabletext.Report
	if _, err := tablewire.Decode(m, m.New(m.Lookup("Holder")), mutant, &orep); err != nil {
		t.Fatal(err)
	}
	if orep.Clamped != 1 || orep.Malformed {
		t.Fatalf("the oracle does not answer the probe as one clamp on clean input: %+v", orep)
	}
	probe := fmt.Sprintf(`
{:ok, v, r} = Blprobe.Table.load("Holder", Base.decode16!(%q, case: :lower))
if v["samples"] != [1, 2] or v["tail"] != 9 or r.clamped != 1 or r.malformed do
  IO.puts(:stderr, "FAIL: " <> inspect({v, r}))
  System.halt(1)
end
IO.puts("ok")
`, hex.EncodeToString(mutant))

	files := generateElixir(t, u)
	if out, err := runArmProbe(t, elixirBin, files, probe); err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("a count past the bound is not clamped before the elements are read: %v\n%s", err, out)
	}

	const clamp = "{n, report} = clamp_count(n, f, report)"
	planted := map[string][]byte{}
	k := 0
	for name, data := range files {
		k += strings.Count(string(data), clamp)
		planted[name] = []byte(strings.ReplaceAll(string(data), clamp, "{n, report} = {n, report}"))
	}
	if k == 0 {
		t.Fatal("the witness found no clamp-before-read in the generated reader")
	}
	if out, err := runArmProbe(t, elixirBin, planted, probe); err == nil {
		t.Fatalf("the reader without its clamp-before-read still passed: the probe does not see the rule\n%s", out)
	}
}

const boundedListProbeSrc = `package blprobe

table Holder
{
    samples [..2]uint16
    tail    int32
}
`

// TestNestedBodyEndingEarlyIsDamageElixir pins a wire fuzzer finding (pass
// reference, tblp1.Chain): a by-value nested body whose zero terminator is not
// its last byte is DAMAGE (docs/SPEC-TABLES.md §3: a body ends at its
// terminator or it is damage), and a read that meets damage reports it. The
// leg read this mutant clean. The VALUE a damaged read returns is undefined
// (the discard-on-damage ruling), so only the report is held.
//
// THE REVERSED WITNESS disables the early-terminator check and the probe must
// go red.
func TestNestedBodyEndingEarlyIsDamageElixir(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(root, "test", "tables", "P1.schema"))
	// chain_value with the nested body's first reference turned into the zero
	// reference: the body ends at its first byte, twelve bytes inside its L
	const mutant = "01010c05636861696e020d0d000407000000040c037469700000861b638ebaadbcc409484f69ad9b4bbfea0ce83094fde47cf3a4484419abd7560400000000000000"
	probe := fmt.Sprintf(`
case Tblp1.Table.load("Chain", Base.decode16!(%q, case: :lower)) do
  {:ok, _v, r} ->
    if not r.malformed do
      IO.puts(:stderr, "FAIL: a nested body ending early read clean: " <> inspect(r))
      System.halt(1)
    end

  {:error, _reason, _r} ->
    :ok
end
IO.puts("ok")
`, mutant)
	files := generateElixir(t, u)
	if out, err := runArmProbe(t, elixirBin, files, probe); err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("the Elixir reader misses a nested body that ends early: %v\n%s", err, out)
	}
	const check = "if ends_early?(sub, ids),"
	planted := map[string][]byte{}
	k := 0
	for name, data := range files {
		k += strings.Count(string(data), check)
		planted[name] = []byte(strings.ReplaceAll(string(data), check, "if false,"))
	}
	if k == 0 {
		t.Fatal("the witness found no early-terminator check in the generated reader")
	}
	if out, err := runArmProbe(t, elixirBin, planted, probe); err == nil {
		t.Fatalf("the reader without its early-terminator check still passed: the probe does not see the rule\n%s", out)
	}
}

// TestReservedNodeIdInANestedBodyIsDamageElixir pins a wire fuzzer finding
// (pass entry, tblp1.Chain): the reserved node-table id
// 0xFFFFFFFFFFFFFFFF belongs to a variable ROOT's body alone (docs/SPEC-TABLES.md
// §3.1); named inside a nested body it is a second numbering, which cannot
// exist, so it is DAMAGE the report names, never an unknown field skipped. The
// value a damaged read returns is undefined (the discard-on-damage ruling).
//
// THE REVERSED WITNESS lets a nested body carry the id again, and the probe
// must go red.
func TestReservedNodeIdInANestedBodyIsDamageElixir(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(root, "test", "tables", "P1.schema"))
	// chain_value with the nested body's third id-table entry turned into the
	// reserved node id
	const mutant = "01010c05636861696e020d0d030407000000040c037469700000861b638ebaadbcc409484f69ad9b4bbffffffffffffffffff3a4484419abd7560400000000000000"
	probe := fmt.Sprintf(`
case Tblp1.Table.load("Chain", Base.decode16!(%q, case: :lower)) do
  {:ok, _v, r} ->
    if not r.malformed do
      IO.puts(:stderr, "FAIL: the node id in a nested body read clean: " <> inspect(r))
      System.halt(1)
    end

  {:error, _reason, _r} ->
    :ok
end
IO.puts("ok")
`, mutant)
	files := generateElixir(t, u)
	if out, err := runArmProbe(t, elixirBin, files, probe); err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("the Elixir reader takes the node id in a nested body for an unknown field: %v\n%s", err, out)
	}
	const check = "(id == @node_id and transport == :nested)"
	planted := map[string][]byte{}
	k := 0
	for name, data := range files {
		k += strings.Count(string(data), check)
		planted[name] = []byte(strings.ReplaceAll(string(data), check, "(false and transport == :nested)"))
	}
	if k == 0 {
		t.Fatal("the witness found no nested node-id check in the generated reader")
	}
	if out, err := runArmProbe(t, elixirBin, planted, probe); err == nil {
		t.Fatalf("the reader without its nested node-id check still passed: the probe does not see the rule\n%s", out)
	}
}
