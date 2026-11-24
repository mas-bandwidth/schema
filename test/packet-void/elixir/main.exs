# The Elixir leg of test/packet-void (SPEC §4.8): payload-free union arms
# cost exactly their tag bits. Answers every case of
# testdata/wire/packet-void/cases.txt, one "case <name> ok|FAILED: <why>"
# line each, for tools/casegate. The pins are the Go oracle's bytes, never
# this writer's.
#
#   elixir main.exs <testdata/wire/packet-void> [<generated directory>]
#
# Elixir values are immutable, so a read builds its value fresh: there is no
# reused storage to dirty. A selected payload is compared whole, so a read
# that does not construct it at its declared defaults shows.
[pins | rest] = System.argv()
base = List.first(rest) || Path.expand("../../../build/packet-void/elixir", __DIR__)
Code.require_file("PacketRuntime.ex", base)
Code.require_file("Void.ex", base)

defmodule PacketVoidCases do
  alias Packetvoid.{Batch, DefaultArm, Frame, Void}

  @types %{
    "Mixed" => "mixed",
    "Tags" => "tags",
    "Single" => "single",
    "Empty" => "empty",
    "Frame" => "frame",
    "Batch" => "batch"
  }

  # The value of each accept case, by name; :unknown for a name this driver
  # does not know, which is FAILED and never written with another value.
  def build("mixed-none"), do: {"Mixed", nil}
  def build("mixed-ping"), do: {"Mixed", :ping}
  # the payload arm is the type's declared defaults: entries empty, marker 5
  def build("mixed-payload"), do: {"Mixed", {:payload, %DefaultArm{}}}
  def build("tags-none"), do: {"Tags", nil}
  def build("tags-ping"), do: {"Tags", :ping}
  def build("tags-pong"), do: {"Tags", :pong}
  def build("single-none"), do: {"Single", nil}
  def build("single-ack"), do: {"Single", :ack}
  def build("empty-none"), do: {"Empty", nil}
  def build("frame-ping"), do: {"Frame", %Frame{lead: 5, choice: :ping, tail: 6}}
  def build("batch-empty"), do: {"Batch", %Batch{values: []}}
  def build("batch-ping"), do: {"Batch", %Batch{values: [:ping]}}

  def build("batch-ping-payload"),
    do: {"Batch", %Batch{values: [:ping, {:payload, %DefaultArm{}}]}}

  def build(_), do: :unknown

  defp call(fun, args), do: apply(Void, String.to_atom(fun), args)

  defp hex(<<>>), do: "-"
  defp hex(b), do: Base.encode16(b, case: :lower)

  def answer(dir, name, type, verdict, bits_text) do
    case Map.fetch(@types, type) do
      :error -> "unknown type " <> type
      {:ok, t} -> answer(dir, name, type, t, verdict, bits_text)
    end
  end

  defp answer(dir, name, type, t, verdict, bits_text) do
    pin = File.read!(Path.join(dir, name <> ".bin"))

    case verdict do
      "refuse" ->
        case call("read_" <> t, [pin, byte_size(pin) * 8]) do
          :error -> nil
          {:ok, v} -> "read accepted: #{inspect(v)}"
        end

      "accept" ->
        accept(name, type, t, pin, String.to_integer(bits_text))

      _ ->
        "verdict " <> verdict
    end
  end

  defp accept(name, type, t, pin, bits) do
    case build(name) do
      :unknown ->
        "unknown accept case"

      {built_type, _} when built_type != type ->
        "case type #{type}, the driver builds #{built_type}"

      {_, value} ->
        got = call("write_" <> t <> "!", [value])

        cond do
          got != pin ->
            "write #{hex(got)}, pin #{hex(pin)}"

          true ->
            read_back(t, value, pin, bits)
        end
    end
  end

  defp read_back(t, value, pin, bits) do
    case call("read_" <> t, [pin, byte_size(pin) * 8]) do
      :error ->
        "read refused"

      {:ok, back} when back != value ->
        "read value differs: #{inspect(back)}"

      {:ok, back} ->
        again = call("write_" <> t <> "!", [back])

        case call("read_" <> t <> "_bits", [pin, byte_size(pin) * 8]) do
          _ when again != pin ->
            "write what was read: #{hex(again)}, pin #{hex(pin)}"

          {:ok, _, read_bits} when read_bits != bits ->
            "read has #{read_bits} bits, want #{bits}"

          {:ok, ^value, _} ->
            nil

          other ->
            "read_#{t}_bits: #{inspect(other)}"
        end
    end
  end

  def run(dir) do
    File.read!(Path.join(dir, "cases.txt"))
    |> String.split("\n")
    |> Enum.each(fn line ->
      case String.split(line) do
        [] ->
          :ok

        ["#" <> _ | _] ->
          :ok

        [name | fields] when length(fields) < 3 ->
          IO.puts("case #{name} FAILED: a case line needs <name> <type> <verdict> <bits>")

        [name, type, verdict, bits | _] ->
          why =
            try do
              answer(dir, name, type, verdict, bits)
            rescue
              e -> "raised #{Exception.message(e)}"
            end

          IO.puts(if why, do: "case #{name} FAILED: #{why}", else: "case #{name} ok")
      end
    end)
  end
end

PacketVoidCases.run(pins)
