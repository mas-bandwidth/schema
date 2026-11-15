package elixirtable

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// An integer token of any length is answered by its value or refused
// (docs/SPEC-TABLES.md §16.2 Numbers); the read never raises out of from_json.
// Every row is compared with the Go engine's verdict on the same text.
func TestElixirJsonLongIntegerAnswersWithoutRaising(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	u := unitFrom(t, `package longint
table Root {
 experience uint32 = 0
}
`)
	model := tabletext.NewModel(u)
	const bigDigits = 10 * 1024 * 1024
	big := strings.Repeat("9", bigDigits)
	rows := []struct {
		name   string
		text   string
		elixir string
	}{
		{"integer-10mb", `{"experience":` + big + `}`, fmt.Sprintf(`"{\"experience\":" <> String.duplicate("9", %d) <> "}"`, bigDigits)},
		{"integer-10mb-unknown", `{"other":` + big + `}`, fmt.Sprintf(`"{\"other\":" <> String.duplicate("9", %d) <> "}"`, bigDigits)},
		{"scaled", `{"experience":1` + strings.Repeat("0", 600) + `e-600}`, ""},
		{"uint32-max", `{"experience":4294967295}`, ""},
		{"uint32-past", `{"experience":4294967296}`, ""},
	}
	var probe strings.Builder
	probe.WriteString(`
defmodule Probe do
 def check(true, _), do: :ok
 def check(false, name) do
  IO.puts("FAIL " <> name)
  Process.put(:failures, Process.get(:failures, 0) + 1)
 end
 def row(text, accepted, unknown, canon, name) do
  case Longint.Table.from_json("Root", text) do
   {:ok, value, report} ->
    check(accepted, name <> " verdict")
    check(report.unknown == unknown, name <> " unknown")
    check(Longint.Table.to_json("Root", value) == {:ok, canon}, name <> " canonical value")
   {:error, _reason, _report} ->
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
		var canon []byte
		if ok {
			var err error
			canon, err = model.Write(inst)
			if err != nil {
				t.Fatal(err)
			}
			canon = append(canon, '\n')
		}
		textExpr := row.elixir
		if textExpr == "" {
			textExpr = fmt.Sprintf("Base.decode16!(%q, case: :mixed)", hex.EncodeToString([]byte(row.text)))
		}
		fmt.Fprintf(&probe, "Probe.row(%s, %t, %d, Base.decode16!(%q, case: :mixed), %q)\n",
			textExpr, ok, report.Unknown, hex.EncodeToString(canon), row.name)
	}
	probe.WriteString("if Process.get(:failures, 0) != 0, do: raise(\"long integer assertions failed\")\nIO.puts(\"ok\")\n")
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe.String())
	if err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("long integer answers: %v\n%s", err, out)
	}
}
