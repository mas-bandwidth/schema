package elixirtable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// SPEC-TABLES §16.2: null alone means optional absence. Under the one strict
// read (45eff682b) an incompatible map value or a duplicate key is not read
// on as a counted present-default: the text is refused, as the engine
// refuses it. The verdict for each input is the engine's (the contract
// across legs); a read lands the value with presence intact and round-trips.
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
	m := tabletext.NewModel(u)
	cases := []struct{ input, field, want string }{
		{`{"ints":{"1":null}}`, "ints", "nil"},
		{`{"ints":{"1":"bad"}}`, "ints", "0"},
		{`{"ints":{"1":{}}}`, "ints", "0"},
		{`{"ints":{"1":7,"1":[]}}`, "ints", "0"},
		{`{"words":{"1":{}}}`, "words", `""`},
		{`{"words":{"1":"live","1":7}}`, "words", `""`},
		{`{"leaves":{"1":null}}`, "leaves", "nil"},
		{`{"leaves":{"1":[]}}`, "leaves", "%{}"},
		{`{"leaves":{"1":{"n":9},"1":true}}`, "leaves", "%{}"},
	}
	var probe strings.Builder
	probe.WriteString(`
 defmodule Probe do
  def check(true,_), do: :ok
  def check(false,what), do: raise(what)
 end
 alias Mapshape.Table, as: T
`)
	reads := 0
	for _, c := range cases {
		var report tabletext.Report
		read := m.Read(m.New(u.Tables["Root"]), []byte(c.input), &report)
		if read {
			reads++
			fmt.Fprintf(&probe, ` input=%q
 {:ok,value,r}=T.from_json("Root",input)
 Probe.check(value[%q]["1"]==%s,"presence/default: "<>input)
 Probe.check(r.reason=="","report: "<>input)
 {:ok,text}=T.to_json("Root",value)
 {:ok,wire}=T.save("Root",value)
 {:ok,again,_}=T.load("Root",wire)
 {:ok,^text}=T.to_json("Root",again)
`, c.input, c.field, c.want)
		} else {
			fmt.Fprintf(&probe, ` input=%q
 {:error,why,r}=T.from_json("Root",input)
 Probe.check(why==:malformed or r.reason != "","refusal: "<>input)
`, c.input)
		}
	}
	if reads == 0 {
		t.Fatal("no input reads on the engine: the presence rule is not exercised")
	}
	probe.WriteString(" IO.puts(\"MAP_SHAPE_OK\")\n")
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe.String())
	if err != nil || !strings.Contains(out, "MAP_SHAPE_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native map shape: %v\n%s", err, out)
	}
}
