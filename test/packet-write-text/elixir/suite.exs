defmodule PacketWriteTextTest do
  alias Packetwritetext.{Packet, Text}

  @cases Path.expand("../cases.txt", __DIR__)

  def fail(what) do
    IO.puts("FAILED: #{what}")
    Process.put(:failed, Process.get(:failed, 0) + 1)
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
        fail("packet-write-text malformed case line: #{inspect(line)}")
    end
  end

  def run do
    lines =
      @cases
      |> File.read!()
      |> String.split("\n")
      |> Enum.reject(fn line -> line == "" or String.starts_with?(line, "#") end)

    Enum.each(lines, &case_line/1)

    # One unit per shared case: tools/mustrun holds the count to the one the
    # recipe registers, so an emptied or shortened cases.txt is red.
    ran = length(lines)
    failed = Process.get(:failed, 0)
    IO.puts("packet-write-text Elixir: ran #{ran}, failed #{failed}")

    if failed != 0 or ran == 0 do
      System.halt(1)
    end
  end
end
