package elixirtable

import (
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"strings"
	"testing"
)

// SPEC-TABLES §16.2: null alone means optional absence. Any other
// incompatible value is present-default and replaces a duplicate entry.
func TestElixirMapValueShapePreservesPresence(t *testing.T) {
	slowtest.Gate(t, "Elixir map value presence")
	u := unitFrom(t, `package mapshape
 table Leaf { n int32 }
 table Root {
  ints map[uint8]?int32
  words map[uint8]string(8)
  leaves map[uint8]?Leaf
 }
 `)
	probe := `
 defmodule Probe do
  def check(true,_), do: :ok
  def check(false,what), do: raise(what)
 end
 alias Mapshape.Table, as: T
 cases=[
  {"{\"ints\":{\"1\":null}}", "ints", nil, 0, 0},
  {"{\"ints\":{\"1\":\"bad\"}}", "ints", 0, 1, 0},
  {"{\"ints\":{\"1\":{}}}", "ints", 0, 1, 0},
  {"{\"ints\":{\"1\":7,\"1\":[]}}", "ints", 0, 1, 1},
  {"{\"words\":{\"1\":{}}}", "words", "", 1, 0},
  {"{\"words\":{\"1\":\"live\",\"1\":7}}", "words", "", 1, 1},
  {"{\"leaves\":{\"1\":null}}", "leaves", nil, 0, 0},
  {"{\"leaves\":{\"1\":[]}}", "leaves", %{}, 1, 0},
  {"{\"leaves\":{\"1\":{\"n\":9},\"1\":true}}", "leaves", %{}, 1, 1}
 ]
 for {input,field,want,km,dup}<-cases do
  {:ok,value,r}=T.from_json("Root",input)
  Probe.check(value[field]["1"]==want,"presence/default: "<>input)
  Probe.check(r.kind_mismatch==km and Map.get(r,:duplicate,0)==dup and true,"report: "<>input)
  {:ok,text}=T.to_json("Root",value)
  {:ok,wire}=T.save("Root",value)
  {:ok,again,_}=T.load("Root",wire)
  {:ok,^text}=T.to_json("Root",again)
 end
 IO.puts("MAP_SHAPE_OK")
 `
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe)
	if err != nil || !strings.Contains(out, "MAP_SHAPE_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native map shape: %v\n%s", err, out)
	}
}
