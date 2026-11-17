package elixirtable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestElixirJsonWriterRefusesZeroByte pins docs/SPEC.md §5.1 row 8 and
// docs/SPEC-TABLES.md §16.3 on the Elixir text writer: a value whose string
// field, text-written bytes, text-list element or map key holds a zero byte is
// refused with the leg's own refusal {:error, :invalid_value}, never raised and
// never spelled \u0000, which the reader refuses. The same value without the
// zero writes the Go engine's canonical text.
func TestElixirJsonWriterRefusesZeroByte(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	const source = `package zerojson

table Root
{
    s string(8)
    p *string
    l [..3]string(8)
    m map[string(8)]int32
}
`
	u := unitFrom(t, source)
	m := tabletext.NewModel(u)

	const clean = `{"s":"abc","p":"pb","l":["one","two"],"m":{"key":7}}`
	inst := m.New(u.Tables["Root"])
	var report tabletext.Report
	if !m.Read(inst, []byte(clean), &report) || !report.Silent() {
		t.Fatalf("the Go reference did not read the clean value: %+v", report)
	}
	want, err := m.Write(inst)
	if err != nil {
		t.Fatalf("the Go reference ToJson: %v", err)
	}

	var probe strings.Builder
	probe.WriteString(`defmodule ZeroProbe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end

  def refused(value, what) do
    got =
      try do
        Zerojson.Table.to_json("Root", value)
      rescue
        error -> {:raised, error}
      end

    check(got == {:error, :invalid_value}, what <> ": got " <> inspect(got))
  end

  def writes(value, want, what) do
    got =
      try do
        Zerojson.Table.to_json("Root", value)
      rescue
        error -> {:raised, error}
      end

    check(got == {:ok, want}, what <> ": got " <> inspect(got))
  end
end
`)
	zeros := []struct{ name, literal string }{
		{"front", `<<0, 0x61, 0x62>>`},
		{"middle", `<<0x61, 0, 0x62>>`},
		{"end", `<<0x61, 0x62, 0>>`},
	}
	base := func(s, p, l, m string) string {
		return fmt.Sprintf(`%%{"s" => %s, "p" => %s, "l" => [%%{"value" => "one"}, %%{"value" => %s}], "m" => %s}`, s, p, l, m)
	}
	for _, zero := range zeros {
		fmt.Fprintf(&probe, "ZeroProbe.refused(%s, %s)\n",
			base(zero.literal, `"pb"`, `"two"`, `%{"key" => 7}`),
			elixirString("string field zero at the "+zero.name))
		fmt.Fprintf(&probe, "ZeroProbe.refused(%s, %s)\n",
			base(`"abc"`, zero.literal, `"two"`, `%{"key" => 7}`),
			elixirString("text-written bytes zero at the "+zero.name))
		fmt.Fprintf(&probe, "ZeroProbe.refused(%s, %s)\n",
			base(`"abc"`, `"pb"`, zero.literal, `%{"key" => 7}`),
			elixirString("text-list element zero at the "+zero.name))
		fmt.Fprintf(&probe, "ZeroProbe.refused(%s, %s)\n",
			base(`"abc"`, `"pb"`, `"two"`, fmt.Sprintf(`%%{%s => 7}`, zero.literal)),
			elixirString("map key zero at the "+zero.name))
	}
	fmt.Fprintf(&probe, "ZeroProbe.writes(%s, %s, %s)\n",
		base(`"abc"`, `"pb"`, `"two"`, `%{"key" => 7}`),
		elixirString(string(want)+"\n"),
		elixirString("the same value without a zero writes the canonical text"))
	probe.WriteString("IO.puts(\"ZERO_JSON_OK\")\n")

	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe.String())
	if err != nil || !strings.Contains(out, "ZERO_JSON_OK") {
		t.Fatalf("Elixir JSON zero-byte writer: %v\n%s", err, out)
	}
}
