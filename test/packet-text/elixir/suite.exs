defmodule PacketTextTest do
  alias Packettext.Text

  def hex(<<>>), do: "-"
  def hex(bytes), do: Base.encode16(bytes, case: :lower)

  def run do
    for line <- IO.stream(:stdio, :line) do
      text = String.trim(line)
      wire = if text == "-", do: <<>>, else: Base.decode16!(text, case: :mixed)

      case Text.read_narrow(wire, byte_size(wire) * 8) do
        :error ->
          IO.puts("REFUSE")

        {:ok, value} ->
          bits = Text.measure_narrow(value)

          if Text.read_narrow(wire, bits) != {:ok, value} or
               Text.read_narrow(wire, bits - 1) != :error do
            raise "exact bit bound"
          end

          encoded = Text.write_narrow(value)
          IO.puts("OK #{bits} #{hex(value.text)} #{bits} #{hex(encoded)}")
      end
    end
  end
end
