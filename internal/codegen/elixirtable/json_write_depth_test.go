package elixirtable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// elixirDepthWriteUnit is internal/tabletext/depth_write_test.go's
// depthWriteUnit, package and all.
const elixirDepthWriteUnit = `package depthwrite

enum Side
{
    Left
    Right
}

union Pick
{
    a int32
    b int32
}

table ArrNode
{
    next *ArrNode
    vals [..4]int32
}

table UnionNode
{
    next *UnionNode
    pick Pick
}

table MapNode
{
    next *MapNode
    by   map[uint8]int32
}

table KeyedNode
{
    next  *KeyedNode
    sides [Side]int32
}

flags Mark
{
    Hot
    Cold
}

table FlagsNode
{
    next  *FlagsNode
    marks Mark
}
`

const jsonWriteDepthProbeSource = `
defmodule WriteDepthProbe do
  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end

  def writes(table, wire, label) do
    {:ok, value, _} = Depthwrite.Table.load(table, wire)
    got = Depthwrite.Table.to_json(table, value)
    check(match?({:ok, _}, got), label <> " writes: got " <> inspect(got, limit: 3))
    {:ok, text} = got
    back = Depthwrite.Table.from_json(table, text)
    check(match?({:ok, _, _}, back), label <> " reads back: got " <> inspect(back, limit: 3))
  end

  def refused(table, wire, label) do
    {:ok, value, _} = Depthwrite.Table.load(table, wire)
    got = Depthwrite.Table.to_json(table, value)
    check(match?({:error, _}, got), label <> " refuses: got " <> inspect(got, limit: 3))
  end
end

CASES
IO.puts("ok")
`

// TestElixirJSONWriteDepthCountsEveryContainer holds to_json to the cap the
// read counts (docs/SPEC-TABLES.md §16.2, §16.14 W2): a chain of nodes, each
// holding one EMPTY container of a kind the writer always emits (an array, a
// union's object, a map's object, an enum-keyed array's object, a flags
// value's array), writes and reads back with 127 nodes under the root, the
// last container at depth 128, and to_json refuses 128 nodes, the last one's
// container at 129. The chains are built by the Go engine and handed to the
// probe as wire.
func TestElixirJSONWriteDepthCountsEveryContainer(t *testing.T) {
	t.Parallel()

	u := unitFrom(t, elixirDepthWriteUnit)
	m := tabletext.NewModel(u)
	var cases strings.Builder
	for _, table := range []string{"ArrNode", "UnionNode", "MapNode", "KeyedNode", "FlagsNode"} {
		for _, n := range []int{127, 128} {
			verdict := "writes"
			if n == 128 {
				verdict = "refused"
			}
			fmt.Fprintf(&cases, "WriteDepthProbe.%s(%q, %s, %s)\n", verdict, table,
				elixirBinaryLiteral(elixirDepthChainWire(t, m, table, n)), elixirString(fmt.Sprintf("%s chain of %d", table, n)))
		}
	}
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), strings.ReplaceAll(jsonWriteDepthProbeSource, "CASES", cases.String()))
	if err != nil {
		t.Fatalf("JSON write depth probe: %v\n%s", err, out)
	}
}

// elixirBinaryLiteral spells bytes as an Elixir binary literal.
func elixirBinaryLiteral(data []byte) string {
	var b strings.Builder
	b.WriteString("<<")
	for i, c := range data {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%d", c)
	}
	b.WriteString(">>")
	return b.String()
}

// elixirDepthChainWire is the engine's wire for a chain of n nodes of the
// table under the root, each linked by `next`, every other field its default.
func elixirDepthChainWire(t *testing.T, m *tabletext.Model, table string, n int) []byte {
	t.Helper()
	st := m.Lookup(table)
	root := m.New(st)
	cur := root
	for range n {
		child := m.New(st)
		var next *tabletext.Field
		for j := range cur.Fields {
			if cur.Fields[j].Def.Name == "next" {
				next = &cur.Fields[j]
			}
		}
		if next == nil {
			t.Fatalf("%s has no next field", table)
		}
		next.Cell.Node = child
		cur = child
	}
	data, err := tablewire.Encode(m, root)
	if err != nil {
		t.Fatalf("%s: the engine could not save a chain of %d: %v", table, n, err)
	}
	return data
}
