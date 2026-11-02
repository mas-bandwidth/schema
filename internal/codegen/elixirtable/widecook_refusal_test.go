package elixirtable_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixirtable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// The cook's authoring API refuses invalid wide text rather than returning an
// image no conforming reader can accept. A conversion error must not escape the
// same invalid_value result. The existing map/list gate owns canonical parity.
func TestElixirWideCookRefusesInvalidText(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	t.Parallel()
	elixirBin := elixirtable.ElixirBinaryForTest(t)
	dir := t.TempDir()
	schema := filepath.Join(dir, "Caption.schema")
	if err := os.WriteFile(schema, []byte("package widecook\ntable Caption { text wstring(6)\n tail int32 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	unit, err := c.Load([]string{schema})
	if err != nil {
		t.Fatal(err)
	}
	files, err := c.Generate(unit, "elixir", compiler.Options{})
	if err != nil {
		t.Fatal(err)
	}
	names := elixirtable.WriteModuleDirForTest(t, dir, files)
	probe := `
defmodule WideCookProbe do
  def check(true, _what), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL " <> what)
    Process.put(:failed, true)
  end
  def result(f) do
    try do
      f.()
    rescue
      e -> {:raised, e.__struct__}
    catch
      kind, reason -> {:caught, kind, reason}
    end
  end
end

for text <- ["", "Aé中😀Z", "😀"] do
  value = %{"text" => text, "tail" => 17}
  {size, :ok} = Widecook.Table.cook_measure_reason("Caption", value)
  # §7.2: 14 text bytes, padding to four, int32 unit count, int32 tail.
  WideCookProbe.check(size == 104, "canonical root and directory extent")
  for order <- [:little, :big] do
    buffer = :binary.copy(<<0xa5>>, size + 8)
    {true, out} = Widecook.Table.cook_from("Caption", value, buffer, order)
    utf16 = :unicode.characters_to_binary(text, :utf8, {:utf16, order})
    units = div(byte_size(utf16), 2)
    expected = utf16 <> :binary.copy(<<0>>, 14 - byte_size(utf16))
    count = if order == :little, do: <<units::little-signed-32>>, else: <<units::big-signed-32>>
    WideCookProbe.check(binary_part(out, 64, 14) == expected, "UTF16 units, terminator and zero slack #{order}")
    WideCookProbe.check(binary_part(out, 80, 4) == count, "unit count #{order}")
    WideCookProbe.check(binary_part(out, size, 8) == :binary.copy(<<0xa5>>, 8), "caller tail retained #{order}")
    short = :binary.copy(<<0xa5>>, size - 1)
    WideCookProbe.check(Widecook.Table.cook_from("Caption", value, short, order) == false, "short output refused #{order}")
  end
end

bad = [
  {"NUL", <<0>>}, {"embedded NUL", <<65, 0, 66>>},
  {"invalid byte", <<255>>}, {"overlong NUL", <<0xc0, 0x80>>},
  {"high surrogate", <<0xed, 0xa0, 0x80>>}, {"low surrogate", <<0xed, 0xb0, 0x80>>},
  {"truncated pair", <<0xf0, 0x9f, 0x98>>}, {"over bound", "1234567"}
]
for {name, text} <- bad do
  value = %{"text" => text, "tail" => 17}
  measured = WideCookProbe.result(fn -> Widecook.Table.cook_measure_reason("Caption", value) end)
  WideCookProbe.check(measured == {-1, :invalid_value}, "#{name} measure: #{inspect(measured)}")
  for order <- [:little, :big] do
    buffer = :binary.copy(<<0xa5>>, 256)
    result = WideCookProbe.result(fn -> Widecook.Table.cook_from("Caption", value, buffer, order) end)
    WideCookProbe.check(result == false, "#{name} write #{order}: #{inspect(result, limit: 3)}")
  end
end
if Process.get(:failed), do: System.halt(1)
`
	elixirtable.RunProbeForTest(t, elixirBin, dir, names, probe)
}
