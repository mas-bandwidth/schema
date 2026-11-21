package elixirtable

// THE LABEL RULES HOLD IN A SKIPPED VALUE (docs/SPEC-TABLES.md §16.7): in a
// pointered root, an object the read skips (here an unknown key's value) keeps
// the text's label numbering in text order. A label alone REFERS, and the text
// must have defined it and closed that definition before this point; a label
// with fields DEFINES, must be new to the whole text, and stays open until the
// skipped object closes, after which a reference to it reads null. The Go
// engine is the oracle: every row's verdict is asked of it first.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const elixirSkippedLabelSchema = `package skiplabel

table Node
{
    value int32 = 0
}

table Scene
{
    head *Node
}
`

// elixirSkippedLabelRows: name, Scene text, verdict.
var elixirSkippedLabelRows = [][3]string{
	{"placed-then-skipped-definition", `{"head": {"&node": 1, "value": 1}, "zz": {"&node": 1, "value": 2}}`, "refused"},
	{"skipped-reference-before-definition", `{"zz": {"&node": 5}, "head": {"&node": 5, "value": 1}}`, "refused"},
	{"skipped-reference-undefined", `{"zz": {"&node": 5}}`, "refused"},
	{"skipped-definition-then-placed-reference", `{"zz": {"&node": 5, "value": 1}, "head": {"&node": 5}}`, "read"},
	{"placed-reference-before-skipped-definition", `{"head": {"&node": 5}, "zz": {"&node": 5, "value": 1}}`, "refused"},
	{"skipped-definition-twice", `{"zz": {"&node": 5, "value": 1}, "yy": {"&node": 5, "value": 2}}`, "refused"},
	{"skipped-definition-then-skipped-reference", `{"zz": {"&node": 5, "value": 1}, "yy": {"&node": 5}}`, "read"},
	{"skipped-reference-inside-its-open-definition", `{"zz": {"&node": 5, "value": {"&node": 5}}}`, "refused"},
	{"placed-definition-then-skipped-reference", `{"head": {"&node": 1, "value": 1}, "zz": {"&node": 1}}`, "read"},
}

func TestElixirJsonSkippedLabelRules(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	u := unitFrom(t, elixirSkippedLabelSchema)
	model := tabletext.NewModel(u)
	st := model.Lookup("Scene")
	if st == nil {
		t.Fatal("no Scene in the unit")
	}

	var probe strings.Builder
	probe.WriteString(`
defmodule Probe do
 def check(true, _), do: :ok
 def check(false, what) do
  IO.puts("FAIL " <> what)
  Process.put(:failures, Process.get(:failures, 0) + 1)
 end
 def row(text, want_ok, name) do
  result = Skiplabel.Table.from_json("Scene", text)
  ok = match?({:ok, _, _}, result)
  check(ok == want_ok, name <> " verdict: " <> inspect(result))
 end
end
`)
	for _, row := range elixirSkippedLabelRows {
		var report tabletext.Report
		want := "refused"
		if model.Read(model.New(st), []byte(row[1]), &report) {
			want = "read"
		}
		if want != row[2] {
			t.Errorf("%s: the Go engine answered %q, the row %q", row[0], want, row[2])
		}
		fmt.Fprintf(&probe, "Probe.row(%s, %t, %s)\n", elixirString(row[1]), row[2] == "read", elixirString(row[0]))
	}
	probe.WriteString("if Process.get(:failures, 0) != 0, do: raise(\"skipped label assertions failed\")\nIO.puts(\"ok\")\n")

	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe.String())
	if err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("skipped-value label rules: %v\n%s", err, out)
	}
}
