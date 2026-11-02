package elixirtable

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// Bounded text cannot be a direct list element in this schema language, so
// exercise the same wstring boundary inside the supported list-of-table form.
func TestJSONWideBoundaryElixir(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	u := unitFrom(t, `package wideboundary
table Child {
 one wstring(2)
 two wstring(3)
}
table Root {
 one wstring(2)
 two wstring(3)
 children []Child
}
`)
	m := tabletext.NewModel(u)
	var probe strings.Builder
	probe.WriteString(`
defmodule Probe do
 def check(true, _), do: :ok
 def check(false, what) do
  IO.puts("FAIL " <> what)
  Process.put(:failures, Process.get(:failures, 0) + 1)
 end
end
`)
	for i, token := range []string{`"😀"`, `"\ud83d\ude00"`, `"a😀"`, `"a\ud83d\ude00"`, `"ab😀"`, `"ab\ud83d\ude00"`} {
		text := fmt.Sprintf(`{"one":%s,"two":%s,"children":[{"one":%s,"two":%s}]}`, token, token, token, token)
		inst := m.New(m.Lookup("Root"))
		report := tabletext.Report{}
		if !m.Read(inst, []byte(text), &report) || report.Malformed || report.KindMismatch != 0 {
			t.Fatalf("engine input %s report=%+v", text, report)
		}
		one, _ := inst.FieldByKey("one")
		two, _ := inst.FieldByKey("two")
		children, _ := inst.FieldByKey("children")
		childOne, _ := children.Elems[0].Tab.FieldByKey("one")
		childTwo, _ := children.Elems[0].Tab.FieldByKey("two")
		want := []string{string(utf16.Decode(one.Cell.Units)), string(utf16.Decode(two.Cell.Units)), string(utf16.Decode(childOne.Cell.Units)), string(utf16.Decode(childTwo.Cell.Units))}
		fmt.Fprintf(&probe, "{:ok, value, report} = Wideboundary.Table.from_json(\"Root\", %s)\n", elixirString(text))
		for j, expr := range []string{`value["one"]`, `value["two"]`, `hd(value["children"])["one"]`, `hd(value["children"])["two"]`} {
			fmt.Fprintf(&probe, "Probe.check(%s == %s, \"case %d field %d pair-safe clamp\")\n", expr, elixirString(want[j]), i, j)
		}
		fmt.Fprintf(&probe, "Probe.check(not report.malformed and report.kind_mismatch == 0 and report.clamped == %d, \"case %d report\")\n", report.Clamped, i)
	}
	probe.WriteString("if Process.get(:failures, 0) != 0, do: raise(\"boundary assertions failed\")\nIO.puts(\"ok\")\n")
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe.String())
	if err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("wide boundary read: %v\n%s", err, out)
	}
}
