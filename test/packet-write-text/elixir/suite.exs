defmodule PacketWriteTextTest do
  alias Packetwritetext.{Packet, Text}

  @cases Path.expand("../cases.txt", __DIR__)

  def fail(what) do
    IO.puts("FAILED: #{what}")
    Process.put(:failed, true)
  end

  def check(true, _what), do: :ok
  def check(_, what), do: fail(what)

  def bytes("-"), do: <<>>
  def bytes(text), do: text |> String.replace(" ", "") |> Base.decode16!(case: :lower)

  def refused(name, bytes) do
    value = %Packet{text: bytes}

    case Text.write_packet(value) do
      {:error, _message} -> :ok
      other -> fail("packet-write-text refused #{name}: #{inspect(other)}")
    end

    try do
      Text.write_packet!(value)
      fail("packet-write-text raised #{name}: did not raise")
    rescue
      ArgumentError -> :ok
    end
  end

  def written(name, text) do
    value = %Packet{text: text}

    case Text.write_packet(value) do
      {:ok, wire} ->
        check(Text.write_packet!(value) == wire, "packet-write-text bang #{name} differs")

        case Text.read_packet(wire, bit_size(wire)) do
          {:ok, out} ->
            check(out.text == text, "packet-write-text read back #{name}: #{inspect(out.text)}")

            check(
              Text.write_packet!(out) == wire,
              "packet-write-text rewrite #{name} is not byte exact"
            )

          other ->
            fail("packet-write-text read #{name}: #{inspect(other)}")
        end

      other ->
        fail("packet-write-text wrote #{name}: #{inspect(other)}")
    end
  end

  def case_line(line) do
    case String.split(line, "|") do
      [name, hex, verdict] ->
        name = String.trim(name)
        text = bytes(hex |> String.trim() |> String.downcase())

        case String.trim(verdict) do
          "refused" -> refused(name, text)
          "written" -> written(name, text)
          other -> fail("packet-write-text verdict #{name}: #{inspect(other)}")
        end

      _ ->
        :ok
    end
  end

  def run do
    @cases
    |> File.read!()
    |> String.split("\n")
    |> Enum.reject(fn line -> line == "" or String.starts_with?(line, "#") end)
    |> Enum.each(&case_line/1)

    if Process.get(:failed) do
      System.halt(1)
    end

    IO.puts("OK")
  end
end
