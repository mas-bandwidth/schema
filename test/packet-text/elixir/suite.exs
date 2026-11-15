defmodule PacketTextTest do
  alias Packettext.Text

  # The driver reads through read_narrow_bits/2, the composable read that
  # reports the bits it consumed and runs no packet end check: this
  # comparison checks UTF-8 text handling only, against a C++ reader that
  # runs no end check. The end check is tested by packet-end-elixir.

  def hex(<<>>), do: "-"
  def hex(bytes), do: Base.encode16(bytes, case: :lower)

  def run do
    for line <- IO.stream(:stdio, :line) do
      text = String.trim(line)
      wire = if text == "-", do: <<>>, else: Base.decode16!(text, case: :mixed)

      case Text.read_narrow_bits(wire, byte_size(wire) * 8) do
        :error ->
          IO.puts("REFUSE")

        {:ok, value, bits} ->
          if Text.read_narrow_bits(wire, bits) != {:ok, value, bits} or
               Text.read_narrow_bits(wire, bits - 1) != :error do
            raise "exact bit bound"
          end

          encoded = Text.write_narrow!(value)
          {:ok, _, written_bits} = Text.read_narrow_bits(encoded, bit_size(encoded))
          IO.puts("OK #{bits} #{hex(value.text)} #{written_bits} #{hex(encoded)}")
      end
    end
  end
end
