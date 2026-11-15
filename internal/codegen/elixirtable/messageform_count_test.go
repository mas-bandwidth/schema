package elixirtable

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestElixirSaveMessagesRefusesCountOffShape(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "an external toolchain")

	u := unitFrom(t, `package messagecount

fixed table Item
{
    value uint8 = 0
}

fixed table Root
{
    items [1..3]Item
}
`)
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	module, ok := tables[TableMessageModule+".ex"]
	if !ok {
		t.Fatalf("the unit grows no %s.ex; files are %v", TableMessageModule, keysOf(tables))
	}

	root := findTable(t, u, "Root")
	item := findTable(t, u, "Item")
	var itemsField *ir.Field
	for _, f := range root.Fields {
		if f.Name == "items" {
			itemsField = f
		}
	}
	if itemsField == nil {
		t.Fatal("Root is missing its test array")
	}
	if len(item.Fields) != 1 || item.Fields[0].Name != "value" {
		t.Fatal("Item is missing its test value")
	}
	slots := tablewire.MessageVocabularySlots(u)
	itemsRef := slots[ir.TableFieldEntry(itemsField).Key()]
	valueRef := slots[ir.TableFieldEntry(item.Fields[0]).Key()]
	if itemsRef == 0 || valueRef == 0 {
		t.Fatalf("test field has no message vocabulary slot: items=%d value=%d", itemsRef, valueRef)
	}

	probe := fmt.Sprintf(`defmodule Probe do
  alias Messagecount.TableMessage, as: M

  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    Process.put(:failed, true)
  end

  def run do
    {:ok, vocab} = M.announce_read(M.announce())
    items = %d
    value = %d
    rows = fn n -> List.duplicate([{value, 7}], n) end

    expect_refused(vocab, items, rows.(0), "counted array below min")
    expect_refused(vocab, items, rows.(4), "counted array above max")
    expect_round_trip(vocab, items, rows.(1), "counted array at min")
    expect_round_trip(vocab, items, rows.(3), "counted array at max")
  end

  defp expect_refused(vocab, ref, values, what) do
    case M.save_messages(vocab, [[{ref, values}]]) do
      {:error, _} -> :ok
      other -> check(false, what <> " was accepted: " <> inspect(other))
    end
  end

  defp expect_round_trip(vocab, ref, values, what) do
    case M.save_messages(vocab, [[{ref, values}]]) do
      {:ok, batch} ->
        case M.load_messages(vocab, batch) do
          {:ok, [[{^ref, ^values}]]} -> :ok
          other -> check(false, what <> " did not round-trip: " <> inspect(other))
        end

      other ->
        check(false, what <> " was refused: " <> inspect(other))
    end
  end
end

Probe.run()
if Process.get(:failed), do: System.halt(1)
`, itemsRef, valueRef)
	out, err := runElixirModuleProbe(t, module, probe)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
