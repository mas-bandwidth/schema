package elixirtable

import (
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"strings"
	"testing"
)

// SPEC-TABLES §16.2: null alone means optional absence. Any other value of
// the wrong shape refuses the whole read kind_mismatch, and a repeated key
// refuses it malformed (§4: the refusal is the verdict, no counter).
func TestElixirMapValueShapePreservesPresence(t *testing.T) {
	t.Parallel()
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
 absent=[
  {"{\"ints\":{\"1\":null}}", "ints"},
  {"{\"leaves\":{\"1\":null}}", "leaves"}
 ]
 for {input,field}<-absent do
  {:ok,value,r}=T.from_json("Root",input)
  Probe.check(Map.has_key?(value[field],"1") and value[field]["1"]==nil,"presence: "<>input)
  Probe.check(r.reason=="" and r.unknown==0 and r.widened==0,"report: "<>input)
  {:ok,text}=T.to_json("Root",value)
  {:ok,wire}=T.save("Root",value)
  {:ok,again,_}=T.load("Root",wire)
  {:ok,^text}=T.to_json("Root",again)
 end
 refused=[
  "{\"ints\":{\"1\":\"bad\"}}",
  "{\"ints\":{\"1\":{}}}",
  "{\"ints\":{\"1\":7,\"1\":[]}}",
  "{\"words\":{\"1\":{}}}",
  "{\"words\":{\"1\":\"live\",\"1\":7}}",
  "{\"leaves\":{\"1\":[]}}",
  "{\"leaves\":{\"1\":{\"n\":9},\"1\":true}}",
  "{\"ints\":{\"1\":7,\"1\":8}}"
 ]
 for input<-refused do
  Probe.check(match?({:error,_,_},T.from_json("Root",input)),"refused: "<>input)
 end
 IO.puts("MAP_SHAPE_OK")
 `
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe)
	if err != nil || !strings.Contains(out, "MAP_SHAPE_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native map shape: %v\n%s", err, out)
	}
}
