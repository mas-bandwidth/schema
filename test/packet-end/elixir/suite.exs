defmodule PacketEndTest do
  alias Packetend.{End, EndFile}

  def check(true, _what), do: :ok

  def check(false, what) do
    IO.puts("FAILED: packet-end verdict #{what}")
    Process.put(:failed, true)
  end

  def vector(dir, name) do
    File.read!(Path.join(dir, name))
  end

  def bits(dir, name) do
    File.read!(Path.join(dir, Path.rootname(name) <> ".bits"))
    |> String.trim()
    |> String.to_integer()
  end

  def run(dir) do
    cases = [
      {"exact.bin", vector(dir, "exact.bin"), {:ok, %End{value: 3}}},
      {"trailing.bin", vector(dir, "trailing.bin"), :error},
      {"padding.bin", vector(dir, "padding.bin"), :error}
    ]

    for {name, packet, verdict} <- cases do
      check(
        EndFile.read_end(packet, bits(dir, name)) == verdict,
        "#{name} at its bit count"
      )

      check(
        EndFile.read_end(packet, byte_size(packet) * 8) == verdict,
        "#{name} at its byte length"
      )
    end

    trailing = vector(dir, "trailing.bin")

    check(
      EndFile.read_end_bits(trailing, 3) == {:ok, %End{value: 3}, 3},
      "read_end_bits/2 reports the bits consumed without ending the packet"
    )

    if Process.get(:failed, false), do: System.halt(1)

    IO.puts(
      "packet end Elixir: the exact packet reads and a trailing byte or non-zero padding refuses"
    )
  end
end
