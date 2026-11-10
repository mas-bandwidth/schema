package elixirtable

// THE ELIXIR STRICT-100 CELLS (docs/roadmap-evidence/
// zz-strict-100-elixir.sexp). Each cell is the ONE READ (docs/SPEC-TABLES.md
// §4, §6.5): a read returns the whole value or refuses the whole load.
//
// The variable probe drives one rich root (fixed-class tables by value,
// enum-keyed and unbounded and pointer and union and optional arrays, maps,
// string, wide scalars, a blob and a pointer graph) through the generated
// Elixir load, load_retain, save, save_retain and text form: a lawful wire
// reads whole and re-saves byte for byte, and every damaged wire refuses
// plain and retaining with no value and the retaining store cleared. Every
// wire's verdict is the Go oracle's (tablewire.Decode) before the generated
// Elixir is asked. The fixed probe drives one fixed root through its form-3
// save/load: the value round-trips, and form 1, a truncated file, a ragged
// tail and an empty file each refuse the whole load.
//
// The negative control plants faults in copies of the real generated
// artifacts (the short-body tolerance, the count clamp and the fixed
// previous-form refusal) and requires the same probes to go red on the
// rows those faults touch: a gate that cannot see a plant in the real
// artifact proves nothing about the unplanted one.
//
// It compiles and runs the generated units with the real Elixir toolchain,
// so it is a functional test gated by slowtest.

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const cellsVTReaderSchema = `package cellsvt

enum Slot
{
    alpha
    beta
    gamma | was = "third"
}

fixed table Spot
{
    x   float32
    y   float32 = 1.5
    tag uint16
}

fixed table Config
{
    scale   float32 = 1.0
    name    string(16) = "hi"
    caption string(8)
    spot    Spot
}

table Leaf | was = "Node"
{
    id   uint32
    name string(8)
    next *Leaf
}

union Choice
{
    xs      [..4]int32
    leaf    *Leaf
    spot    Spot
    caption string(8)
}

table Root
{
    spot    Spot
    cfg     Config
    renamed int32 | was = "former"
    text    string(16)
    caption string(16)
    mood    Slot = beta
    keyed   [Slot]int32
    xs      []int32
    bounded [..3]Spot
    samples [..2]uint16
    ptrs    []*Leaf
    choice  Choice
    choices []Choice
    opt     ?[..4]int32
    maybe   ?int32
    lookup  map[int32]int32
    head    *Leaf
    blob    *bytes
    note    *string
    wide    int64
    uw      uint64
    big     float64
}
`

const cellsVTWriterSchema = `package cellsvt

enum Slot
{
    alpha
    beta
    gamma | was = "third"
}

fixed table Spot
{
    x   float32
    y   float32 = 1.5
    tag uint16
}

fixed table Config
{
    scale   float32 = 1.0
    name    string(16) = "hi"
    caption string(8)
    spot    Spot
}

table Leaf | was = "Node"
{
    id   uint32
    name string(8)
    next *Leaf
}

union Choice
{
    xs      [..4]int32
    leaf    *Leaf
    spot    Spot
    caption string(8)
}

table Root
{
    spot    Spot
    cfg     Config
    renamed int32 | was = "former"
    text    string(16)
    caption string(16)
    mood    Slot = beta
    keyed   [Slot]int32
    xs      []int32
    bounded [..3]Spot
    samples [..2]uint16
    ptrs    []*Leaf
    choice  Choice
    choices []Choice
    opt     ?[..4]int32
    maybe   ?int32
    lookup  map[int32]int32
    head    *Leaf
    blob    *bytes
    note    *string
    wide    int64
    uw      uint64
    big     float64
    extra   int32 = 0
    future  string(8) = ""
}
`

type cellsVector struct {
	name string
	wire []byte
	want bool
	// framing vectors carry a collection body too short for its header
	// or a node no pointer reaches: tolerated by the value load (§4, H2),
	// refused on the framing read.
	framing bool
}

func cellsJSONWire(t *testing.T, u *ir.Unit, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	var rep tabletext.Report
	if !m.Read(inst, []byte(text), &rep) || rep.Refused {
		t.Fatalf("oracle did not read the rich text: %+v", rep)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("oracle did not encode: %v", err)
	}
	return wire
}

func cellsVectors(t *testing.T, u *ir.Unit) []cellsVector {
	t.Helper()
	m := tabletext.NewModel(u)

	makeWire := func(build func(ref func(uint64) []byte) []byte) []byte {
		var ids []uint64
		ref := func(id uint64) []byte {
			for i, v := range ids {
				if v == id {
					return binary.AppendUvarint(nil, uint64(i+1))
				}
			}
			ids = append(ids, id)
			return binary.AppendUvarint(nil, uint64(len(ids)))
		}
		body := build(ref)
		body = append(body, 0)
		wire := append([]byte{1}, body...)
		for _, id := range ids {
			wire = binary.LittleEndian.AppendUint64(wire, id)
		}
		return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
	}
	framed := func(ref func(uint64) []byte, name string, kind byte, payload []byte) []byte {
		out := append(ref(ir.TableWireId(name)), kind)
		out = binary.AppendUvarint(out, uint64(len(payload)))
		return append(out, payload...)
	}
	ptrField := func(ref func(uint64) []byte, name string, index uint64) []byte {
		out := append(ref(ir.TableWireId(name)), byte(ir.TableKindPointer))
		return binary.AppendUvarint(out, index)
	}
	type rec struct {
		tid  uint64
		body []byte
	}
	nodeTable := func(ref func(uint64) []byte, recs ...rec) []byte {
		nt := binary.AppendUvarint(nil, uint64(len(recs)))
		for _, r := range recs {
			nt = append(nt, ref(r.tid)...)
			nt = binary.AppendUvarint(nt, uint64(len(r.body)))
			nt = append(nt, r.body...)
		}
		out := append(ref(math.MaxUint64), byte(ir.TableKindString))
		out = binary.AppendUvarint(out, uint64(len(nt)))
		return append(out, nt...)
	}
	leafTID := ir.TableWireId(u.Tables["Leaf"].WireName())

	vectors := []cellsVector{
		{name: "valid-rich", want: true, wire: cellsJSONWire(t, u, `{"spot":{"x":1.5,"y":2.5,"tag":7},"cfg":{"scale":2,"name":"cfg","caption":"wide","spot":{"x":3,"y":4,"tag":8}},"renamed":42,"text":"hello","caption":"мир","mood":"gamma","keyed":{"alpha":1,"beta":2,"gamma":3},"xs":[5,6],"bounded":[{"x":1,"y":2,"tag":1}],"ptrs":[{"id":1,"name":"a","next":null},null],"choice":{"spot":{"x":9,"y":10,"tag":3}},"choices":[{"xs":[1,2]},{"caption":"hi"}],"opt":[7,8],"maybe":11,"lookup":{"3":30,"5":50},"head":{"id":9,"name":"h","next":{"id":10,"name":"n","next":null}},"blob":"AAEC","note":"note text","wide":-5000000000,"uw":18000000000000000000,"big":2.5}`)},
		{name: "valid-defaults", want: true, wire: cellsJSONWire(t, u, `{}`)},
		{name: "%s", want: false, framing: true, wire: makeWire(func(ref func(uint64) []byte) []byte { return framed(ref, "xs", byte(ir.TableKindArray), nil) })},
		{name: "%s", want: false, framing: true, wire: makeWire(func(ref func(uint64) []byte) []byte {
			return framed(ref, "xs", byte(ir.TableKindArray), []byte{byte(ir.TableKindI32)})
		})},
		{name: "%s", want: false, framing: true, wire: makeWire(func(ref func(uint64) []byte) []byte {
			return framed(ref, "bounded", byte(ir.TableKindArray), []byte{byte(ir.TableKindTable)})
		})},
		{name: "short-bytes-blob", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte {
			return framed(ref, "blob", byte(ir.TableKindPointer), []byte{1})
		})},
		{name: "%s", want: false, framing: true, wire: makeWire(func(ref func(uint64) []byte) []byte { return framed(ref, "opt", byte(ir.TableKindArray), nil) })},
		{name: "%s", want: false, framing: true, wire: makeWire(func(ref func(uint64) []byte) []byte { return framed(ref, "keyed", byte(ir.TableKindKeyed), nil) })},
		{name: "%s", want: false, framing: true, wire: makeWire(func(ref func(uint64) []byte) []byte {
			return framed(ref, "keyed", byte(ir.TableKindKeyed), []byte{byte(ir.TableKindI32)})
		})},
		{name: "%s", want: false, framing: true, wire: makeWire(func(ref func(uint64) []byte) []byte { return framed(ref, "lookup", byte(ir.TableKindArray), nil) })},
		{name: "%s", want: false, framing: true, wire: makeWire(func(ref func(uint64) []byte) []byte {
			return framed(ref, "choices", byte(ir.TableKindArray), []byte{byte(ir.TableKindUnion)})
		})},
		{name: "unreachable-table-node", want: false, framing: true, wire: makeWire(func(ref func(uint64) []byte) []byte { return nodeTable(ref, rec{leafTID, []byte{0}}) })},
		{name: "unreachable-blob", want: false, framing: true, wire: makeWire(func(ref func(uint64) []byte) []byte { return nodeTable(ref, rec{ir.BytesWireTypeId, []byte{1, 2, 3}}) })},
		{name: "node-index-out-of-range", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte { return ptrField(ref, "head", 99) })},
		{name: "count-past-bound", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte {
			payload := []byte{byte(ir.TableKindTable), 100}
			return framed(ref, "bounded", byte(ir.TableKindArray), payload)
		})},
		{name: "count-past-bound-samples", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte {
			payload := []byte{byte(ir.TableKindU16), 100, 1, 0, 2, 0}
			return framed(ref, "samples", byte(ir.TableKindArray), payload)
		})},
	}

	for _, tc := range vectors {
		var rep tabletext.Report
		ok, err := tablewire.Decode(m, m.New(u.Tables["Root"]), tc.wire, &rep)
		if ok != tc.want {
			t.Fatalf("oracle verdict mismatch on %s: got ok=%v err=%v rep=%+v, want %v", tc.name, ok, err, rep, tc.want)
		}
		if !tc.want && !tablewire.ReadRefusal(err) {
			t.Fatalf("oracle refusal on %s is not a ReadRefusal: %v", tc.name, err)
		}
	}
	return vectors
}

func cellsVTProbe(vectors []cellsVector, retainWire []byte) string {
	var b strings.Builder
	b.WriteString(`
check = fn
  true, _ -> :ok
  false, label -> IO.puts(:stderr, "FAIL: " <> label); Process.put(:failed, true)
end
zero = %{unknown: 0, widened: 0, retained: 0, retain_lost: 0, reason: "", missing_required: 0}
store = %{records: [], used: 0, cap: 4096, ids: [], id_cap: 256}
`)
	for _, tc := range vectors {
		if tc.framing {
			fmt.Fprintf(&b, `
  wire = Base.decode16!(%q, case: :lower)
  label = %q
  case Cellsvt.Table.variable_load("Root", wire) do
    {:error, reason, r} when is_atom(reason) -> check.(r.reason != "", label <> ": framing refusal carries no reason")
    other -> check.(false, label <> ": framing read landed a short body: " <> inspect(other))
  end
`, hex.EncodeToString(tc.wire), tc.name)
			continue
		}
		fmt.Fprintf(&b, `
  wire = Base.decode16!(%q, case: :lower)
  want = %t
  label = %q
  case Cellsvt.Table.load("Root", wire) do
    {:ok, v, r} ->
      check.(want, label <> ": plain load refused a lawful wire " <> inspect(r))
      if want do
        check.(r.reason == "", label <> ": clean load carries a reason")
        case Cellsvt.Table.save("Root", v) do
          {:ok, out} -> check.(out == wire, label <> ": save does not reproduce the wire")
          other -> check.(false, label <> ": save failed " <> inspect(other))
        end
      end
    {:error, reason, r} when is_atom(reason) ->
      check.(not want, label <> ": plain load landed a damaged wire")
      check.(r.reason != "", label <> ": refusal carries no reason")
    other -> check.(false, label <> ": plain load answered " <> inspect(other))
  end
  case Cellsvt.Table.load_retain("Root", wire, store, zero) do
    {:ok, v, r, rt} ->
      check.(want, label <> ": retaining load refused a lawful wire " <> inspect(r))
      if want do
        case Cellsvt.Table.save_retain("Root", v, rt, r) do
          {:ok, out, _, _} -> check.(out == wire, label <> ": retaining save does not reproduce the wire")
          other -> check.(false, label <> ": retaining save failed " <> inspect(other))
        end
      end
    {:error, reason, r, rt} when is_atom(reason) ->
      check.(not want, label <> ": retaining load landed a damaged wire")
      check.(r.reason != "", label <> ": retaining refusal carries no reason")
      check.(rt.records == [], label <> ": refusal left the retaining store populated")
    other -> check.(false, label <> ": retaining load answered " <> inspect(other))
  end
`, hex.EncodeToString(tc.wire), tc.want, tc.name)
	}
	// Text form: the rich value writes to JSON and reads back to the wire.
	b.WriteString(`
  wire = Base.decode16!(` + fmt.Sprintf("%q", hex.EncodeToString(vectors[0].wire)) + `, case: :lower)
  {:ok, v, _} = Cellsvt.Table.load("Root", wire)
  case Cellsvt.Table.to_json("Root", v) do
    {:ok, text} ->
      case Cellsvt.Table.from_json("Root", text) do
        {:ok, back, _} ->
          case Cellsvt.Table.save("Root", back) do
            {:ok, out} -> check.(out == wire, "text-form: the text read back does not re-save to the wire")
            other -> check.(false, "text-form: re-save failed " <> inspect(other))
          end
        other -> check.(false, "text-form: from_json refused its own text " <> inspect(other))
      end
    other -> check.(false, "text-form: to_json failed " <> inspect(other))
  end
`)
	// Retain-unknown: a newer wire's added fields are retained and re-saved.
	fmt.Fprintf(&b, `
  wire = Base.decode16!(%q, case: :lower)
  case Cellsvt.Table.load_retain("Root", wire, store, zero) do
    {:ok, v, r, rt} ->
      check.(r.unknown >= 1, "retain-unknown: the added field was not counted unknown " <> inspect(r))
      check.(r.retained >= 1, "retain-unknown: the added field was not retained " <> inspect(r))
      case Cellsvt.Table.save_retain("Root", v, rt, r) do
        {:ok, out, _, _} -> check.(out == wire, "retain-unknown: the retained save does not reproduce the wire")
        other -> check.(false, "retain-unknown: save_retain failed " <> inspect(other))
      end
    other -> check.(false, "retain-unknown: the newer wire did not read " <> inspect(other))
  end
`, hex.EncodeToString(retainWire))
	b.WriteString(`
if Process.get(:failed, false), do: System.halt(1)
IO.puts("ELIXIR_CELLS_VT_OK")
`)
	return b.String()
}

func cellsRetainWire(t *testing.T) []byte {
	t.Helper()
	reader := unitFrom(t, cellsVTReaderSchema)
	writer := unitFrom(t, cellsVTWriterSchema)
	_ = reader
	m := tabletext.NewModel(writer)
	inst := m.New(m.Lookup("Root"))
	var rep tabletext.Report
	if !m.Read(inst, []byte(`{"text":"hi","extra":42,"future":"soon"}`), &rep) || rep.Refused {
		t.Fatalf("oracle did not read the writer text: %+v", rep)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("oracle did not encode the writer wire: %v", err)
	}
	return wire
}

func runCellsVTProbe(t *testing.T, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	u := unitFrom(t, cellsVTReaderSchema)
	vectors := cellsVectors(t, u)
	files := generateElixir(t, u)
	if edit != nil {
		edit(files)
	}
	return runArmProbe(t, elixirBinary(t), files, cellsVTProbe(vectors, cellsRetainWire(t)))
}

func TestElixirCellsStrict(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	out, err := runCellsVTProbe(t, nil)
	if err != nil || !strings.Contains(out, "ELIXIR_CELLS_VT_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("the variable cells do not hold the strict read: %v\n%s", err, out)
	}
	out, err = runCellsFixedProbe(t, nil)
	if err != nil || !strings.Contains(out, "ELIXIR_CELLS_FIXED_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("the fixed cells do not hold the strict read: %v\n%s", err, out)
	}
}

const cellsFixedSchema = `package cellsfx

enum Slot
{
    alpha
    beta
    gamma
}

fixed table Inner
{
    a int32 = 1
    b int32 = 2
}

fixed table Root
{
    name    string(16) = "hi"
    caption string(8)
    mood    Slot = beta
    vals    [..4]int32
    fixeds  [3]int16
    keyed   [Slot]int32
    blob    bytes(6)
    inner   Inner
    opt     ?int32
    nested  ?Inner
    wide    int64
    ratio   float64
    n       int32 = 0 | min = 0, max = 10
}
`

func cellsFixedProbe() string {
	return `
alias Cellsfx.ProbeFixed, as: F
check = fn
  true, _ -> :ok
  false, label -> IO.puts(:stderr, "FAIL: " <> label); Process.put(:failed, true)
end
value = %Cellsfx.Root{
  name: "cells",
  caption: "wide",
  mood: :gamma,
  vals: [1, 2, 3],
  fixeds: [4, 5, 6],
  keyed: [1, 0, 3],
  blob: <<0, 1, 2, 3, 4, 5>>,
  inner: %Cellsfx.Inner{a: 7, b: 8},
  opt_present: true,
  opt: 11,
  nested_present: true,
  nested: %Cellsfx.Inner{a: 9, b: 10},
  wide: -5000000000,
  ratio: 2.5,
  n: 10
}
wire = F.root_fixed_save([value])
check.(is_binary(wire) and byte_size(wire) > 0 and :binary.at(wire, 0) == 3, "fixed save did not write form 3")
case F.root_fixed_load(wire) do
  {:ok, [back], report} ->
    check.(back == value, "fixed value did not round-trip: " <> inspect(back))
    check.(report.unknown == 0 and report.widened == 0, "fixed round-trip moved a counter: " <> inspect(report))
  other -> check.(false, "fixed wire did not read: " <> inspect(other))
end
for {label, damaged} <- [
  {"fixed-form1", <<1>> <> binary_part(wire, 1, byte_size(wire) - 1)},
  {"fixed-truncated", binary_part(wire, 0, byte_size(wire) - 1)},
  {"fixed-ragged", wire <> <<0>>},
  {"fixed-empty", <<>>}
] do
  case F.root_fixed_load(damaged) do
    {:error, reason, _} when is_atom(reason) -> :ok
    other -> check.(false, label <> " landed a partial value: " <> inspect(other))
  end
end
case F.root_fixed_load(<<1>> <> binary_part(wire, 1, byte_size(wire) - 1)) do
  {:error, :previous_form, _} -> :ok
  other -> check.(false, "fixed-form1 was not refused as previous_form: " <> inspect(other))
end
if Process.get(:failed, false), do: System.halt(1)
IO.puts("ELIXIR_CELLS_FIXED_OK")
`
}

func runCellsFixedProbe(t *testing.T, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	u := unitFrom(t, cellsFixedSchema)
	files := generateElixir(t, u)
	if edit != nil {
		edit(files)
	}
	return runArmProbe(t, elixirBinary(t), files, cellsFixedProbe())
}

// TestElixirCellsStrictNegativeControl plants faults in copies of the real
// generated artifacts and requires the strict probes to go red on the rows
// those faults touch.
func TestElixirCellsStrictNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")

	// Clean first: the unplanted artifacts must hold, or a red below is noise.
	if out, err := runCellsVTProbe(t, nil); err != nil || !strings.Contains(out, "ELIXIR_CELLS_VT_OK") {
		t.Fatalf("the unplanted variable artifacts already fail the gate: %v\n%s", err, out)
	}
	if out, err := runCellsFixedProbe(t, nil); err != nil || !strings.Contains(out, "ELIXIR_CELLS_FIXED_OK") {
		t.Fatalf("the unplanted fixed artifacts already fail the gate: %v\n%s", err, out)
	}

	// Plant the short-body tolerance on the framing read: variable_load
	// no longer raises the strict framing flag, so the short rows land
	// inert partial values there.
	out, err := runCellsVTProbe(t, func(files map[string][]byte) {
		elixirPatch(t, files,
			"Process.put(:elixir_strict_framing, true)",
			"Process.put(:elixir_strict_framing, false)")
	})
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: the short-body plant left the variable probe green\n%s", out)
	}
	if !strings.Contains(out, "FAIL:") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the short-body plant failed for another reason: %v\n%s", err, out)
	}

	// Plant the count clamp: a count past the declared bound is clamped
	// instead of refusing, so count-past-bound lands the bound's elements.
	out, err = runCellsVTProbe(t, func(files map[string][]byte) {
		elixirPatch(t, files,
			"defp clamp_count(n, %{max_count: max}, _report) when n > max,\n    do: {max, throw(:wire_damaged)}",
			"defp clamp_count(n, %{max_count: max}, report) when n > max,\n    do: {max, report}")
	})
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: the count-clamp plant left the variable probe green\n%s", out)
	}
	if !strings.Contains(out, "FAIL:") && !strings.Contains(out, "count-past-bound") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the count-clamp plant failed for another reason: %v\n%s", err, out)
	}

	// Plant the fixed previous-form confusion: form 1 is named newer_form
	// instead of previous_form.
	out, err = runCellsFixedProbe(t, func(files map[string][]byte) {
		patched := 0
		for name, data := range files {
			s := string(data)
			if strings.Contains(s, ":previous_form") {
				files[name] = []byte(strings.ReplaceAll(s, ":previous_form", ":newer_form"))
				patched++
			}
		}
		if patched == 0 {
			t.Fatal("NEGATIVE CONTROL FAILED: no previous_form refusal in the generated fixed artifact")
		}
	})
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: the previous-form plant left the fixed probe green\n%s", out)
	}
	if !strings.Contains(out, "FAIL:") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the previous-form plant failed for another reason: %v\n%s", err, out)
	}
}
