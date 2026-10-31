package elixirtable

import (
	"encoding/hex"
	"fmt"
	"os"
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
// THE REVERSED WITNESS takes the declared-kind check out of the measure's
// scan_field and the probe must go red.
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

	const guard = "true <- kind == expected or kind_widens?(kind, expected),"
	planted := map[string][]byte{}
	n := 0
	for name, data := range files {
		n += strings.Count(string(data), guard)
		planted[name] = []byte(strings.ReplaceAll(string(data), guard, "true <- true,"))
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

// TestPointerToAnotherTablesRecordIsAKindMismatchElixir pins a wire fuzzer
// misread of well-framed input (pass index, graphdemo.Scene): a `*T` slot
// whose node index names a record of ANOTHER table reads null and counts one
// kind_mismatch, as the engine's resolveCell (internal/tablewire) answers; the
// leg resolved it to the other table's record and counted nothing. The
// expected report is the oracle's own answer for this mutant.
//
// THE REVERSED WITNESS resolves the slot without the type check and the probe
// must go red.
func TestPointerToAnotherTablesRecordIsAKindMismatchElixir(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "tables", "pointers")
	u := loadUnit(t, filepath.Join(dir, "Graph.schema"), filepath.Join(dir, "Marks.schema"), filepath.Join(dir, "Parts.schema"))
	// graph_tree with one pointer's node index (byte 16) moved from record 2 to
	// record 6, a record of another table; the oracle answers the mutant here
	wire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "graph_tree.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(wire) < 17 || wire[14] != 0x03 || wire[15] != 0x11 || wire[16] != 0x02 {
		t.Fatalf("graph_tree.bin no longer opens with the pointer this probe moves: %x", wire[:min(len(wire), 20)])
	}
	mutantBytes := append([]byte{}, wire...)
	mutantBytes[16] = 0x06
	m := tabletext.NewModel(u)
	var orep tabletext.Report
	if _, err := tablewire.Decode(m, m.New(m.Lookup("Scene")), mutantBytes, &orep); err != nil {
		t.Fatal(err)
	}
	if orep.KindMismatch != 1 || orep.Malformed || orep.Unknown != 0 {
		t.Fatalf("the oracle does not answer the probe as one kind mismatch on well-framed input: %+v", orep)
	}
	mutant := hex.EncodeToString(mutantBytes)
	probe := fmt.Sprintf(`
{:ok, _v, r} = Graphdemo.Table.load("Scene", Base.decode16!(%q, case: :lower))
if {r.unknown, r.kind_mismatch, r.widened, r.clamped, r.malformed} != {0, 1, 0, 0, false} do
  IO.puts(:stderr, "FAIL: a pointer to another table's record: " <> inspect(r))
  System.halt(1)
end
IO.puts("ok")
`, mutant)
	files := generateElixir(t, u)
	if out, err := runArmProbe(t, elixirBin, files, probe); err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("the Elixir reader resolves a *T slot to another table's record: %v\n%s", err, out)
	}
	const check = "case table_node(nodes, idx, ids, f.elem) do"
	planted := map[string][]byte{}
	k := 0
	for name, data := range files {
		k += strings.Count(string(data), check)
		planted[name] = []byte(strings.ReplaceAll(string(data), check, "case node_value(nodes, idx, ids) do"))
	}
	if k == 0 {
		t.Fatal("the witness found no typed node resolution in the generated reader")
	}
	if out, err := runArmProbe(t, elixirBin, planted, probe); err == nil {
		t.Fatalf("the reader without its typed node resolution still passed: the probe does not see the rule\n%s", out)
	}
}

// TestNodeRecordEventsCountOnceElixir pins a wire fuzzer misread of
// well-framed input (pass reference, graphdemo.Scene): an event inside a NODE
// RECORD's body (here an unknown field, a ListNode record's first reference
// moved to an id no ListNode field has) counts in the load's report, once,
// as the engine's second pass over the node table counts it
// (internal/tablewire decodeVariable). The leg resolved the record where a
// pointer named it and dropped that body's events. The expected report is the
// oracle's own answer.
//
// THE REVERSED WITNESS takes the once-per-record events pass out of the load
// and the probe must go red.
func TestNodeRecordEventsCountOnceElixir(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "tables", "pointers")
	u := loadUnit(t, filepath.Join(dir, "Graph.schema"), filepath.Join(dir, "Marks.schema"), filepath.Join(dir, "Parts.schema"))
	wire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "graph_tree.bin"))
	if err != nil {
		t.Fatal(err)
	}
	// the node table's first record: type reference, L, then its first field
	// reference (8, ListNode.value), moved to 15, an id of another table's field
	at := strings.Index(string(wire), "\x06\x07\x0e\x08\x04")
	if at < 0 {
		t.Fatalf("graph_tree.bin no longer opens its node table with the record this probe moves: %x", wire)
	}
	mutantBytes := append([]byte{}, wire...)
	mutantBytes[at+3] = 0x0f
	m := tabletext.NewModel(u)
	var orep tabletext.Report
	if _, err := tablewire.Decode(m, m.New(m.Lookup("Scene")), mutantBytes, &orep); err != nil {
		t.Fatal(err)
	}
	if orep.Unknown != 1 || orep.Malformed || orep.KindMismatch != 0 {
		t.Fatalf("the oracle does not answer the probe as one unknown on well-framed input: %+v", orep)
	}
	probe := fmt.Sprintf(`
{:ok, _v, r} = Graphdemo.Table.load("Scene", Base.decode16!(%q, case: :lower))
if {r.unknown, r.kind_mismatch, r.widened, r.clamped, r.malformed} != {1, 0, 0, 0, false} do
  IO.puts(:stderr, "FAIL: an unknown field in a node record: " <> inspect(r))
  System.halt(1)
end
IO.puts("ok")
`, hex.EncodeToString(mutantBytes))
	files := generateElixir(t, u)
	if out, err := runArmProbe(t, elixirBin, files, probe); err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("the Elixir reader drops a node record's events: %v\n%s", err, out)
	}
	const pass = "report = merge_report(report, node_events(nodes, ids))"
	planted := map[string][]byte{}
	k := 0
	for name, data := range files {
		k += strings.Count(string(data), pass)
		planted[name] = []byte(strings.ReplaceAll(string(data), pass, "report = report"))
	}
	if k == 0 {
		t.Fatal("the witness found no node events pass in the generated load")
	}
	if out, err := runArmProbe(t, elixirBin, planted, probe); err == nil {
		t.Fatalf("the load without its node events pass still passed: the probe does not see the rule\n%s", out)
	}
}

// TestTimestampSkipsByItsWidthElixir pins a wire fuzzer misread of
// well-framed input (pass kind, graphdemo.Scene): a field whose kind byte is
// 36, the timestamp, is an eight-byte value (docs/SPEC-TABLES.md §3's skip
// table), so a reader that does not declare it there steps over it by its
// width and counts one kind mismatch, as the engine does; the leg's skip
// table had no row for 36 and called the record damaged. The expected report
// is the oracle's own answer.
//
// THE REVERSED WITNESS takes 36 back out of the skip table and the probe must
// go red.
func TestTimestampSkipsByItsWidthElixir(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "tables", "pointers")
	u := loadUnit(t, filepath.Join(dir, "Graph.schema"), filepath.Join(dir, "Marks.schema"), filepath.Join(dir, "Parts.schema"))
	wire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "graph_tree.bin"))
	if err != nil {
		t.Fatal(err)
	}
	// the node table's first record's first field, ListNode.value (int32,
	// kind 4), retyped to kind 36
	at := strings.Index(string(wire), "\x06\x07\x0e\x08\x04")
	if at < 0 {
		t.Fatalf("graph_tree.bin no longer opens its node table with the record this probe retypes: %x", wire)
	}
	mutantBytes := append([]byte{}, wire...)
	mutantBytes[at+4] = 36
	m := tabletext.NewModel(u)
	var orep tabletext.Report
	if _, err := tablewire.Decode(m, m.New(m.Lookup("Scene")), mutantBytes, &orep); err != nil {
		t.Fatal(err)
	}
	if orep.KindMismatch != 1 || orep.Malformed || orep.Unknown != 0 {
		t.Fatalf("the oracle does not answer the probe as one kind mismatch on well-framed input: %+v", orep)
	}
	probe := fmt.Sprintf(`
{:ok, _v, r} = Graphdemo.Table.load("Scene", Base.decode16!(%q, case: :lower))
if {r.unknown, r.kind_mismatch, r.widened, r.clamped, r.malformed} != {0, 1, 0, 0, false} do
  IO.puts(:stderr, "FAIL: a timestamp under an int32 field: " <> inspect(r))
  System.halt(1)
end
IO.puts("ok")
`, hex.EncodeToString(mutantBytes))
	files := generateElixir(t, u)
	if out, err := runArmProbe(t, elixirBin, files, probe); err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("the Elixir reader cannot step over a timestamp: %v\n%s", err, out)
	}
	const row = "when kind in [5, 9, 11, 23, 28, 36], do: take(bin, 8)"
	planted := map[string][]byte{}
	k := 0
	for name, data := range files {
		k += strings.Count(string(data), row)
		planted[name] = []byte(strings.ReplaceAll(string(data), row, "when kind in [5, 9, 11, 23, 28], do: take(bin, 8)"))
	}
	if k == 0 {
		t.Fatal("the witness found no timestamp row in the generated skip table")
	}
	if out, err := runArmProbe(t, elixirBin, planted, probe); err == nil {
		t.Fatalf("the reader without its timestamp row still passed: the probe does not see the rule\n%s", out)
	}
}

// TestCyclicWireLoadTerminatesElixir pins a wire fuzzer stall (pass index,
// graphdemo.Scene): a ListNode record whose `next` names its own record is a
// BACK EDGE, a pointer to a node still open in the pre-order walk. No
// conforming writer produces one (the pack walk refuses a reference to an OPEN
// entry, docs/SPEC-TABLES.md §3.1), so it is damage, and damage discards the
// whole load: the load returns, within ten seconds, refused as malformed. The
// leg used to recurse without end.
//
// THE REVERSED WITNESS takes the open-record check out and the probe must go
// red (the load does not return).
func TestCyclicWireLoadTerminatesElixir(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "tables", "pointers")
	u := loadUnit(t, filepath.Join(dir, "Graph.schema"), filepath.Join(dir, "Marks.schema"), filepath.Join(dir, "Parts.schema"))
	wire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "graph_tree.bin"))
	if err != nil {
		t.Fatal(err)
	}
	// the node table's first record (node 2, a ListNode): its `next` (ref 9,
	// kind 17) names node 3; moved to name node 2, its own record
	at := strings.Index(string(wire), "\x09\x11\x03\x00")
	if at < 0 {
		t.Fatalf("graph_tree.bin no longer carries the ListNode next this probe moves: %x", wire)
	}
	mutantBytes := append([]byte{}, wire...)
	mutantBytes[at+2] = 0x02
	probe := fmt.Sprintf(`
task = Task.async(fn -> Graphdemo.Table.load("Scene", Base.decode16!(%q, case: :lower)) end)

case Task.yield(task, 10_000) do
  {:ok, {:error, :malformed, %%{malformed: true}}} ->
    :ok

  other ->
    IO.puts(:stderr, "FAIL: the cyclic wire's load did not return: " <> inspect(other))
    System.halt(1)
end

IO.puts("ok")
`, hex.EncodeToString(mutantBytes))
	files := generateElixir(t, u)
	if out, err := runArmProbe(t, elixirBin, files, probe); err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("the Elixir load does not refuse a cyclic wire as malformed: %v\n%s", err, out)
	}
	const check = "if MapSet.member?(open, idx) do"
	planted := map[string][]byte{}
	k := 0
	for name, data := range files {
		k += strings.Count(string(data), check)
		planted[name] = []byte(strings.ReplaceAll(string(data), check, "if false do"))
	}
	if k == 0 {
		t.Fatal("the witness found no open-record check in the generated reader")
	}
	if out, err := runArmProbe(t, elixirBin, planted, probe); err == nil {
		t.Fatalf("the reader without its open-record check still passed: the probe does not see the rule\n%s", out)
	}
}

// TestNonCanonicalNodeIndexIsDamageElixir pins a wire fuzzer crash (pass
// canonical, graphdemo.Scene over graph_shared): a pointer's node index that is
// not one canonical LEB128 (here 0x82 0x00) is framing damage the report names
// (docs/SPEC-TABLES.md §3, every number is one canonical LEB128); the leg's
// pointer read had no answer for it and raised a CaseClauseError.
//
// THE REVERSED WITNESS takes the pointer read's damage answer out and the
// probe must go red.
func TestNonCanonicalNodeIndexIsDamageElixir(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "tables", "pointers")
	u := loadUnit(t, filepath.Join(dir, "Graph.schema"), filepath.Join(dir, "Marks.schema"), filepath.Join(dir, "Parts.schema"))
	const mutant = "01010c06736861726564021102031103041102050d0b0604010000000211820000070c2904080d090407000000010c036f6e65000a070b11040c1105000a040b1105000a080d0c046c6561660000861b638ebaadbcc4030c9a5fcc128f0a95b31e51efb8255bb746a665bb2092507e69be95239e8360ea96b20076e9d875ffffffffffffffffa99fa6a599c80ef6ea0ce83094fde47c1d434c78a3907eb9b01c04a2ad70b02405d8145753aaaa763d62cb8fecfcf7390d00000000000000"
	probe := fmt.Sprintf(`
case Graphdemo.Table.load("Scene", Base.decode16!(%q, case: :lower)) do
  {:ok, _v, %%{malformed: true}} -> :ok
  {:error, :malformed, _r} -> :ok
  other ->
    IO.puts(:stderr, "FAIL: a non-canonical node index: " <> inspect(other))
    System.halt(1)
end
IO.puts("ok")
`, mutant)
	files := generateElixir(t, u)
	if out, err := runArmProbe(t, elixirBin, files, probe); err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("the Elixir reader does not answer a non-canonical node index as damage: %v\n%s", err, out)
	}
	const answer = "        # a node index that is not one canonical LEB128 is framing damage (§3)\n        {:cont, nil, %{report | malformed: true}}\n"
	planted := map[string][]byte{}
	k := 0
	for name, data := range files {
		k += strings.Count(string(data), answer)
		planted[name] = []byte(strings.ReplaceAll(string(data), answer, "        {:cont, nil, report}\n"))
	}
	if k == 0 {
		t.Fatal("the witness found no damage answer in the generated pointer read")
	}
	if out, err := runArmProbe(t, elixirBin, planted, probe); err == nil {
		t.Fatalf("the pointer read without its damage answer still passed: the probe does not see the rule\n%s", out)
	}
}
