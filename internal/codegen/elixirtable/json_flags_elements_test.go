package elixirtable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// A flags array holds variant names only (docs/SPEC-TABLES.md §16.2): an
// element that is not a string is a kind mismatch and refuses the read, while
// a string naming no variant counts unknown and reads on. Compare the verdict
// alone with the Go engine's verdict on the same text.
func TestElixirJsonFlagsElementsAreStrings(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	paths, err := filepath.Glob("../../../tables/examples/*.schema")
	if err != nil || len(paths) == 0 {
		t.Fatalf("examples schema: %v", err)
	}
	u := loadUnit(t, paths...)
	m := tabletext.NewModel(u)
	rows := []struct{ name, elem string }{
		{"integer", "1"},
		{"boolean", "true"},
		{"null", "null"},
		{"array", "[]"},
		{"object", "{}"},
		{"name-then-integer", `"Turbo",1`},
		{"name", `"Turbo"`},
		{"unknown-name", `"NoSuchFlag"`},
	}
	var probe strings.Builder
	probe.WriteString(`
defmodule Probe do
 def check(true, _), do: :ok
 def check(false, name) do
  IO.puts("FAIL " <> name)
  Process.put(:failures, Process.get(:failures, 0) + 1)
 end
 def row(text, accepted, name) do
  case Tabledemo.Table.from_json("ProfileConfig", text) do
   {:ok, _value, _report} -> check(accepted, name <> " verdict")
   {:error, _reason, _report} -> check(not accepted, name <> " verdict")
  end
 end
end
`)
	for _, row := range rows {
		text := fmt.Sprintf(`{"loadout":{"perks":[%s]}}`, row.elem)
		var report tabletext.Report
		accepted := m.Read(m.New(u.Tables["ProfileConfig"]), []byte(text), &report)
		fmt.Fprintf(&probe, "Probe.row(%q, %t, %q)\n", text, accepted, row.name)
	}
	probe.WriteString("if Process.get(:failures, 0) != 0, do: raise(\"flags element assertions failed\")\nIO.puts(\"ok\")\n")
	out := runCompleteUnitProbe(t, u, probe.String())
	if !strings.Contains(out, "ok") {
		t.Fatalf("flags elements: %s", out)
	}
}
