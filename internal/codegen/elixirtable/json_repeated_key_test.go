package elixirtable

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// A repeated key refuses the read malformed in every object of the text
// (docs/SPEC-TABLES.md §16.2, §16.12): a known field's key, an unknown key, and
// a key inside a value the reader only skips, at any depth. The verdict is
// compared with the Go engine's verdict on the same text.
func TestElixirJsonRepeatedKeyEveryObject(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(root, "tables", "examples", "Tables.schema"))
	model := tabletext.NewModel(u)
	long := strings.Repeat("a", 300)
	rows := []struct {
		name   string
		text   string
		accept bool
	}{
		{"unknown-repeat", `{"zz":1,"zz":2}`, false},
		{"empty-repeat", `{"":1,"":2}`, false},
		{"reserved-name-repeat", `{"__proto__":1,"__proto__":2}`, false},
		{"unknown-object-repeat", `{"zz":{"a":1,"a":2}}`, false},
		{"long-unknown-repeat", fmt.Sprintf(`{"%s":1,"%s":2}`, long, long), false},
		{"escaped-repeat", `{"zz":1,"\u007az":2}`, false},
		{"skipped-array-object-repeat", `{"zz":[{"a":1,"a":2}]}`, false},
		{"distinct-unknown", `{"zz":1,"yy":2}`, true},
		{"same-key-two-objects", `{"zz":{"a":1},"yy":{"a":2}}`, true},
	}
	var probe strings.Builder
	probe.WriteString(`
defmodule Probe do
 def check(true, _), do: :ok
 def check(false, name) do
  IO.puts("FAIL " <> name)
  Process.put(:failures, Process.get(:failures, 0) + 1)
 end
 def row(text, want_ok, name) do
  case Tabledemo.Table.from_json("ProfileConfig", text) do
   {:ok, _value, _report} -> check(want_ok, name <> " verdict")
   {:error, _reason, _report} -> check(not want_ok, name <> " verdict")
   other -> check(false, name <> " unexpected result: " <> inspect(other))
  end
 end
end
`)
	for _, row := range rows {
		inst := model.New(model.Lookup("ProfileConfig"))
		report := tabletext.Report{}
		ok := model.Read(inst, []byte(row.text), &report)
		if ok != row.accept {
			t.Fatalf("%s: Go verdict %t, want %t: %+v", row.name, ok, row.accept, report)
		}
		fmt.Fprintf(&probe, "Probe.row(Base.decode16!(%q, case: :mixed), %t, %q)\n", hex.EncodeToString([]byte(row.text)), row.accept, row.name)
	}
	probe.WriteString("if Process.get(:failures, 0) != 0, do: raise(\"repeated-key assertions failed\")\nIO.puts(\"ok\")\n")
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe.String())
	if err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("JSON repeated key: %v\n%s", err, out)
	}
}
