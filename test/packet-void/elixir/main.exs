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
    "Late" => "late",
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
  def build("late-payload"), do: {"Late", {:payload, %DefaultArm{}}}
  def build("late-pong"), do: {"Late", :pong}
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

  # The refuse-write cases: the type's default value with the one change the
  # case names. A tag is an atom here, so a negative tag, a text tag and an
  # out-of-range tag are the integers and the string standing where the atom
  # goes. The writer's refusal is {:error, message} (ArgumentError in the bang
  # form); any other raise is not the writer's refusal and is FAILED.
  def refusal("mixed-write-tag-negative"), do: {"Mixed", -1}
  def refusal("mixed-write-tag-text"), do: {"Mixed", "ping"}
  def refusal("mixed-write-tag-3"), do: {"Mixed", 3}
  def refusal("mixed-write-payload-type"), do: {"Mixed", {:payload, 1}}
  def refusal("mixed-write-marker-8"), do: {"Mixed", {:payload, %DefaultArm{marker: 8}}}
  def refusal("tags-write-tag-3"), do: {"Tags", 3}
  def refusal("empty-write-tag-1"), do: {"Empty", 1}
  def refusal(_), do: :unknown

  defp refuse_write(name, type, t) do
    case refusal(name) do
      :unknown ->
        "unknown refuse-write case"

      {built_type, _} when built_type != type ->
        "case type #{type}, the driver builds #{built_type}"

      {_, value} ->
        try do
          call("write_" <> t, [value])
        rescue
          e ->
            "not the writer's refusal: #{e |> Exception.message() |> String.replace("\n", " ")}"
        else
          {:error, _} -> nil
          {:ok, got} -> "write accepted: #{hex(got)}"
        end
    end
  end

  # selectPayload reads mixed-ping's pin, then selects the payload arm the
  # Elixir API's way: a value is its arm, so selecting is building
  # {:payload, %DefaultArm{}}, never what reading the void arm left behind.
  defp select_payload(dir) do
    pin = File.read!(Path.join(dir, "mixed-ping.bin"))

    case call("read_mixed", [pin, byte_size(pin) * 8]) do
      {:ok, :ping} -> {:ok, call("write_mixed!", [{:payload, %DefaultArm{}}])}
      other -> {:error, "read mixed-ping: #{inspect(other)}"}
    end
  end

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
    case verdict do
      "refuse-write" ->
        refuse_write(name, type, t)

      "select" ->
        select(dir, name)

      _ ->
        answer_pin(dir, name, type, t, verdict, bits_text)
    end
  end

  defp select(dir, name) do
    pin = File.read!(Path.join(dir, name <> ".bin"))

    cond do
      name != "mixed-ping-select-payload" ->
        "unknown select case"

      true ->
        case select_payload(dir) do
          {:error, why} -> why
          {:ok, ^pin} -> nil
          {:ok, got} -> "write #{hex(got)}, pin #{hex(pin)}"
        end
    end
  end

  defp answer_pin(dir, name, type, t, verdict, bits_text) do
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
