package elixirtable

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A BOM is an entry prefix, not whitespace and not something to remove from
// stored strings. Compare accepted values and exact wire bytes with Go.
func TestJSONBOMEntryElixir(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	u := unitFrom(t, `package bomentry
table Root {
 value int32 = 7
 tag string(32)
}
`)
	model := tabletext.NewModel(u)
	const doc = "{\"value\":9,\"tag\":\"ok\"}\n"
	rows := []struct{ name, text string }{
		{"plain", doc},
		{"leading", "\ufeff" + doc},
		{"double", "\ufeff\ufeff" + doc},
		{"space-before", " \ufeff" + doc},
		{"newline-before", "\n\ufeff" + doc},
		{"comment-before", "/*layout*/\ufeff" + doc},
		{"inside-object", "{\ufeff\"value\":9}"},
		{"before-value", "{\"value\":\ufeff9}"},
		{"after-document", doc + "\ufeff"},
		{"truncated", "\xef\xbb" + doc},
		{"stored-raw", "{\"tag\":\"a\ufeffb\"}"},
		{"stored-escaped", "{\"tag\":\"a\\uFEFFb\"}"},
	}
	var probe strings.Builder
	probe.WriteString(`
defmodule Probe do
 def check(true, _), do: :ok
 def check(false, name) do
  IO.puts("FAIL " <> name)
  Process.put(:failures, Process.get(:failures, 0) + 1)
 end
 def row(text, accepted, json, wire, name) do
  case Bomentry.Table.from_json("Root", text) do
   {:ok, value, report} ->
    check(accepted, name <> " verdict")
    check(true, name <> " report")
    check(Bomentry.Table.to_json("Root", value) == {:ok, json}, name <> " canonical value")
    check(Bomentry.Table.save("Root", value) == {:ok, wire}, name <> " canonical wire")
   {:error, :malformed, _report} ->
    check(not accepted, name <> " verdict")
   other -> check(false, name <> " unexpected result: " <> inspect(other))
  end
 end
end
`)
	for _, row := range rows {
		inst := model.New(model.Lookup("Root"))
		report := tabletext.Report{}
		ok := model.Read(inst, []byte(row.text), &report)
		var canon, wire []byte
		if ok {
			var err error
			canon, err = model.Write(inst)
			if err != nil {
				t.Fatal(err)
			}
			canon = append(canon, '\n')
			wire, err = tablewire.Encode(model, inst)
			if err != nil {
				t.Fatal(err)
			}
		}
		fmt.Fprintf(&probe, "Probe.row(Base.decode16!(%q, case: :mixed), %t, Base.decode16!(%q, case: :mixed), Base.decode16!(%q, case: :mixed), %q)\n", hex.EncodeToString([]byte(row.text)), ok, hex.EncodeToString(canon), hex.EncodeToString(wire), row.name)
	}
	probe.WriteString("if Process.get(:failures, 0) != 0, do: raise(\"BOM entry assertions failed\")\nIO.puts(\"ok\")\n")
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe.String())
	if err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("BOM entry: %v\n%s", err, out)
	}
}
