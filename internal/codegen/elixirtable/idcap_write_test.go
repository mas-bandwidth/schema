package elixirtable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE WRITER REFUSES AN ID TABLE ITS READERS REFUSE (docs/SPEC-TABLES.md §3,
// ruling R8), on the Elixir leg, which refuses in every build. Every reader
// answers wire_damaged for an id table of more than 4096 entries, so save
// answers an error for a value that needs 4097 and writes the one that needs
// exactly 4096, which the read accepts. The unit is tablewire.IdCapSchema:
// 4095, 4096 and 4097 ids.
//
// A FUNCTIONAL TEST: it runs elixir, behind slowtest.Gate.
const idCapElixirProbe = `
defmodule IdCapProbe do
  alias Idcap.Table, as: V

  defp value(x, y) do
    keys = for i <- 0..4093, into: %{}, do: {"V#{i}", 1}
    %{"keys" => keys, "x" => x, "y" => y}
  end

  def run do
    for {x, y, ids} <- [{0, 0, 4095}, {1, 0, 4096}] do
      case V.save("Root", value(x, y)) do
        {:ok, wire} ->
          n = :binary.decode_unsigned(binary_part(wire, byte_size(wire) - 8, 8), :little)

          case V.load("Root", wire) do
            {:ok, _v, _report} when n == ids -> IO.puts("IDCAP #{ids} ok")
            other -> IO.puts("IDCAP BAD #{ids} count #{n} read #{inspect(other, limit: 3)}")
          end

        other ->
          IO.puts("IDCAP BAD #{ids} refused #{inspect(other)}")
      end
    end

    case V.save("Root", value(1, 1)) do
      {:error, _} -> IO.puts("IDCAP 4097 ok")
      {:ok, w} -> IO.puts("IDCAP BAD 4097 wrote #{byte_size(w)} bytes")
    end
  end
end

IdCapProbe.run()
`

func TestElixirWriterRefusesMoreThan4096Ids(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	files := map[string][]byte{}
	for name, data := range generateElixir(t, unitFrom(t, tablewire.IdCapSchema())) {
		if !strings.Contains(name, "/") && strings.HasSuffix(name, ".ex") {
			files["idcap_"+name] = data
		}
	}
	dir := t.TempDir()
	names := finiteModuleDir(t, dir, files)
	out := runProbe(t, elixirBin, dir, names, idCapElixirProbe)
	for _, want := range []string{"IDCAP 4095 ok", "IDCAP 4096 ok", "IDCAP 4097 ok"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "BAD") {
		t.Fatalf("probe reported BAD:\n%s", out)
	}
}
