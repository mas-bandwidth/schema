# The write-refusal-packet probe for the Elixir leg (docs/SPEC.md 5.1). It reads
# rows.txt (path in argv), builds a fresh lawful PacketWriteRefusal per row,
# applies the row's one op, calls the generated packet write, and prints
# "<name> refused" or "<name> wrote". A refusal is exactly the ArgumentError
# the packet writer raises (SPEC 5.1 "how a refusal surfaces"); any other
# exception is not caught, so a crash or a typo can never pass as a refusal.
defmodule WriteRefusalPacketProbe do
  alias Packetrefusal.{Laser, Missile, PacketWriteRefusal, PacketWriteRefusalFile}

  def apply_op("scalar", n, v), do: %{v | scalar: String.to_integer(n)}
  def apply_op("narrow", n, v), do: %{v | narrow: String.to_integer(n)}

  def apply_op("grade", n, v) do
    grade = Map.fetch!(%{"1" => :bronze, "2" => :silver, "3" => :gold, "4" => 4}, n)
    %{v | grade: grade}
  end

  # The union value is {:arm, payload}; a tag past the declared arms has no arm
  # name, so it rides as an arm the union does not have.
  def apply_op("shot_tag", "0", v), do: %{v | shot: nil}
  def apply_op("shot_tag", "1", v), do: %{v | shot: {:laser, %Laser{}}}
  def apply_op("shot_tag", "2", v), do: %{v | shot: {:missile, %Missile{}}}
  def apply_op("shot_tag", "3", v), do: %{v | shot: {:tag3, %Laser{}}}

  def apply_op("window_count", n, v), do: %{v | window: List.duplicate(0, String.to_integer(n))}

  # The string's length is its byte count: a length past the bound is that many
  # bytes of fresh content.
  def apply_op("text_length", n, v), do: %{v | text: String.duplicate("a", String.to_integer(n))}
  def apply_op("text_bytes", hex, v), do: %{v | text: Base.decode16!(hex, case: :mixed)}
  def apply_op("text_fill", n, v), do: %{v | text: String.duplicate("a", String.to_integer(n))}
  def apply_op("fixed_constant", n, v), do: %{v | fixed_constant: String.to_integer(n)}
  def apply_op("reserved_zero", n, v), do: %{v | reserved_zero: String.to_integer(n)}

  # the BEAM holds no NaN or infinity float: a nonfinite ratio rides as the
  # non-float atom the runtime documents (:nan, :infinity), which the packet
  # writer must refuse.
  def apply_op("ratio", "nan", v), do: %{v | ratio: :nan}
  def apply_op("ratio", "inf", v), do: %{v | ratio: :infinity}
  def apply_op("ratio", "-inf", v), do: %{v | ratio: :neg_infinity}
  def apply_op("ratio", x, v), do: %{v | ratio: String.to_float(if String.contains?(x, "."), do: x, else: x <> ".0")}

  def verdict(value) do
    try do
      PacketWriteRefusalFile.write_packet_write_refusal!(value)
      "wrote"
    rescue
      ArgumentError -> "refused"
    end
  end

  def run(rows_path) do
    for line <- File.stream!(rows_path) |> Stream.map(&String.trim/1),
        line != "" and not String.starts_with?(line, "#") do
      [name, _verdict, op, arg] = String.split(line)
      value = apply_op(op, arg, %PacketWriteRefusal{})
      IO.puts(name <> " " <> verdict(value))
    end

    :ok
  end
end

WriteRefusalPacketProbe.run(List.first(System.argv()))
