package elixirtable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

func TestElixirStringMapWriteKeys(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated Elixir string-map writer")
	u := unitFrom(t, "package mapwrite\n\ntable Root { words map[string(8)]int32 }\n")
	out := runCompleteUnitProbe(t, u, `
alias Mapwrite.Table, as: T

pairs = [{"z", 3}, {"a", 1}, {"m", 2}]
{:ok, bytes} = T.save("Root", %{"words" => Map.new(pairs)})
{:ok, ^bytes} = T.save("Root", %{"words" => pairs})
{:ok, value, _report} = T.load("Root", bytes)
%{"words" => %{"a" => 1, "m" => 2, "z" => 3}} = value

{:ok, empty} = T.save("Root", %{"words" => %{}})
{:ok, ^empty} = T.save("Root", %{"words" => []})

{:error, :invalid_value} = T.save("Root", %{"words" => pairs ++ [{"z", 4}]})
{:error, :invalid_value} = T.save("Root", %{"words" => %{"a" => 1, 3 => 2}})
{:error, :invalid_value} = T.save("Root", %{"words" => [{"a", 1}, {3, 2}]})
IO.puts("STRING_MAP_WRITE_KEYS_OK")
`)
	if !strings.Contains(out, "STRING_MAP_WRITE_KEYS_OK") {
		t.Fatal(out)
	}
}
