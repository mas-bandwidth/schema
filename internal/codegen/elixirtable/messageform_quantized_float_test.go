package elixirtable

// A quantized float32 rides as its step index at bits_required(0, count), and
// an enum-keyed array rides as the count of present slots then one (key
// reference, element) pair per slot (docs/SPEC-TABLES.md §3.2, §3.3). The
// message batch matches the reference codec's bytes. A reader generated
// without the float still loads the writer's announcement and batch: the
// float is carried at the announced width, and the keyed slots come back.

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const elixirQuantizedFloatSchema = `package probe

enum Slot { red, green, blue }

fixed table Root
{
    speed float32 | min = 0.0, max = 10.0, resolution = 0.01
    by [Slot]int32
}
`

const elixirQuantizedFloatReaderSchema = `package probe

enum Slot { red, green, blue }

fixed table Root
{
    by [Slot]int32
}
`

const elixirQuantizedFloatJSON = `{"speed":3.5,"by":{"red":11,"blue":22}}`

func TestMessageQuantizedFloatAndEnumKeyedArray(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Elixir toolchain")

	unit := unitFrom(t, elixirQuantizedFloatSchema)
	model := tabletext.NewModel(unit)
	inst := model.New(model.Lookup("Root"))
	var report tabletext.Report
	if !model.Read(inst, []byte(elixirQuantizedFloatJSON), &report) || !report.Silent() {
		t.Fatalf("source: %+v", report)
	}
	batch, err := tablewire.EncodeMessages(model, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	announce := tablewire.Announce(unit)
	shape := elixirQuantizedFloatShape(t, unit)
	if shape.Bits != 10 {
		t.Fatalf("quantized width %d, want 10", shape.Bits)
	}
	if got := ir.TableMessageQuantize(shape, float32(3.5)); got != 350 {
		t.Fatalf("step index %d, want 350", got)
	}
	speed, by, red, blue := elixirQuantizedFloatSlots(t, unit)

	writer := elixirQuantizedFloatModule(t, unit)
	elixirQuantizedFloatRoundTrip(t, writer, announce, batch, speed, by, red, blue)

	readerUnit := unitFrom(t, elixirQuantizedFloatReaderSchema)
	reader := elixirQuantizedFloatModule(t, readerUnit)
	elixirQuantizedFloatSkips(t, reader, announce, batch)
}

func elixirQuantizedFloatShape(t *testing.T, unit *ir.Unit) ir.TableMessageShape {
	t.Helper()
	st := unit.Tables["Root"]
	if st == nil {
		t.Fatal("no Root")
	}
	for _, f := range st.Fields {
		if f.Name != "speed" {
			continue
		}
		e := ir.TableFieldEntry(f)
		if e.Kind != ir.TableKindF32 || e.Shape.Packing != ir.TableMessageQuantized {
			t.Fatalf("speed kind %d packing %d", e.Kind, e.Shape.Packing)
		}
		return e.Shape
	}
	t.Fatal("Root declares no speed")
	return ir.TableMessageShape{}
}

func elixirQuantizedFloatSlots(t *testing.T, unit *ir.Unit) (speed, by, red, blue uint64) {
	t.Helper()
	st := unit.Tables["Root"]
	var enumRef *ir.Enum
	for _, f := range st.Fields {
		switch f.Name {
		case "speed":
			speed = elixirVocabSlot(unit, ir.TableFieldWireId(f), ir.TableKindF32)
		case "by":
			by = elixirVocabSlot(unit, ir.TableFieldWireId(f), ir.TableKindKeyed)
			enumRef = f.KeyEnumRef
		}
	}
	if enumRef == nil || len(enumRef.Variants) != 3 {
		t.Fatalf("Slot has %v variants", enumRef)
	}
	red = elixirVocabSlot(unit, ir.TableWireId(enumRef.VariantWireName(0)), 0)
	blue = elixirVocabSlot(unit, ir.TableWireId(enumRef.VariantWireName(2)), 0)
	if speed == 0 || by == 0 || red == 0 || blue == 0 {
		t.Fatalf("slots speed %d by %d red %d blue %d", speed, by, red, blue)
	}
	return speed, by, red, blue
}

func elixirVocabSlot(unit *ir.Unit, id uint64, kind uint8) uint64 {
	for i, e := range tablewire.MessageVocabulary(unit) {
		if e.Id == id && e.Kind == kind {
			return uint64(i + 1)
		}
	}
	return 0
}

func elixirQuantizedFloatModule(t *testing.T, unit *ir.Unit) []byte {
	t.Helper()
	tables, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	module, ok := tables[TableMessageModule+".ex"]
	if !ok {
		t.Fatalf("the unit grows no %s.ex", TableMessageModule)
	}
	return module
}

func elixirQuantizedFloatRoundTrip(t *testing.T, module, announce, batch []byte, speed, by, red, blue uint64) {
	t.Helper()
	ns := "Probe"
	body := fmt.Sprintf(`
    ann = Base.decode16!("%s", case: :lower)
    batch = Base.decode16!("%s", case: :lower)
    vocab =
      case M.announce_read(ann) do
        {:ok, v} -> v
        other -> raise "announce_read refused: #{inspect(other)}"
      end

    written = [{%d, 3.5}, {%d, [{%d, 11}, {%d, 22}]}]

    case M.save_messages(vocab, [written]) do
      {:ok, saved} ->
        if saved != batch, do: raise "save bytes #{Base.encode16(saved, case: :lower)} want #{Base.encode16(batch, case: :lower)}"

      other ->
        raise "save refused #{inspect(other)}"
    end

    case M.load_messages(vocab, batch) do
      {:ok, [loaded]} ->
        case M.save_messages(vocab, [loaded]) do
          {:ok, again} ->
            if again != batch, do: raise "load/save bytes #{Base.encode16(again, case: :lower)} want #{Base.encode16(batch, case: :lower)}"

          other ->
            raise "resave refused #{inspect(other)}"
        end

        unless keyed_slots?(loaded, 11, 22), do: raise "loaded keyed slots #{inspect(loaded)}"

      other ->
        raise "load refused #{inspect(other)}"
    end
`, hex.EncodeToString(announce), hex.EncodeToString(batch), speed, by, red, blue)
	probe := elixirQuantizedFloatProbe(ns, body)
	out, err := runElixirModuleProbe(t, module, probe)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func elixirQuantizedFloatSkips(t *testing.T, module, announce, batch []byte) {
	t.Helper()
	body := fmt.Sprintf(`
    ann = Base.decode16!("%s", case: :lower)
    batch = Base.decode16!("%s", case: :lower)
    vocab =
      case M.announce_read(ann) do
        {:ok, v} -> v
        other -> raise "announce_read refused: #{inspect(other)}"
      end

    case M.load_messages(vocab, batch) do
      {:ok, [loaded]} ->
        unless keyed_slots?(loaded, 11, 22), do: raise "skipped batch lost the keyed slots #{inspect(loaded)}"

        case M.save_messages(vocab, [loaded]) do
          {:ok, again} ->
            if again != batch, do: raise "reader save #{Base.encode16(again, case: :lower)} want #{Base.encode16(batch, case: :lower)}"

          other ->
            raise "reader save refused #{inspect(other)}"
        end

      other ->
        raise "reader load refused #{inspect(other)}"
    end
`, hex.EncodeToString(announce), hex.EncodeToString(batch))
	out, err := runElixirModuleProbe(t, module, elixirQuantizedFloatProbe("Probe", body))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func elixirQuantizedFloatProbe(ns, body string) string {
	return fmt.Sprintf(`defmodule Probe do
  alias %s.TableMessage, as: M

  def keyed_slots?(body, a, b) when is_list(body) do
    Enum.any?(body, fn
      {_ref, pairs} when is_list(pairs) ->
        has = fn n -> Enum.any?(pairs, fn {_k, v} -> v == n end) end
        has.(a) and has.(b) and length(pairs) == 2

      _ ->
        false
    end)
  end

  def run do
%s
    IO.puts("ok")
  end
end

Probe.run()
`, ns, indentBody(body))
}
