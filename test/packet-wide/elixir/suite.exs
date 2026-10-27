defmodule PacketWideTest do
  alias Wide.WideText, as: W
  alias Wideprobe.Shapes, as: P

  def check(true, _message), do: :ok
  def check(false, message), do: raise(message)
  def hex(bytes), do: Base.encode16(bytes, case: :lower)

  # the exact wire bits of a written value: what read_<name>_bits reports
  # consuming from it (there is no generated measure, SPEC §6.1)
  def bits_of(wire, read_bits) do
    {:ok, _, bits} = read_bits.(wire, bit_size(wire))
    bits
  end

  def units(<<>>), do: "-"

  def units(bytes) do
    for <<unit::little-16 <- bytes>>, into: "" do
      unit |> Integer.to_string(16) |> String.downcase() |> String.pad_leading(4, "0")
    end
  end

  def contracts do
    for text <- [<<1>>, <<0, 0>>, :binary.copy(<<1, 0>>, 8)] do
      refused =
        try do
          W.write_wide_seven!(%Wide.WideSeven{text: text})
          false
        rescue
          ArgumentError -> true
        end

      check(refused, "writer odd/bounds/null")
    end

    wire = W.write_wide_seven!(%Wide.WideSeven{text: <<0xD800::little-16>>})
    check(W.read_wide_seven(wire, 35) == :error, "unpaired high")
    v = %Wide.WideSeven{text: <<0xFFFF::little-16>>}
    wire = W.write_wide_seven!(v)
    check(W.read_wide_seven(wire, 35) == {:ok, v}, "FFFF binary")
    c = %Wideprobe.Conditional{enabled: true, text: <<0xD800::little-16, 0xDC00::little-16>>}
    wire = P.write_conditional!(c)

    check(
      bits_of(P.write_conditional!(c), &P.read_conditional_bits/2) == 68 and
        Bitwise.band(:binary.at(wire, 0), 15) == 5,
      "unaligned groups"
    )

    check(P.read_conditional(wire, 68) == {:ok, c}, "conditional")
    wire = P.write_conditional!(%{c | enabled: false})
    check(P.read_conditional(wire, 1) == {:ok, %Wideprobe.Conditional{}}, "branch zero")
    text = %Wideprobe.Text{value: <<0xFFFF::little-16>>}
    choice = {:text, text}
    wire = P.write_choice!(choice)

    for _ <- 1..2 do
      check(
        P.read_choice(wire, bits_of(P.write_choice!(choice), &P.read_choice_bits/2)) ==
          {:ok, choice},
        "repeated union"
      )
    end

    box = %Wideprobe.Box{}
    check(length(box.counted) == 1, "born count")
    box = %{box | items: [text, %Wideprobe.Text{}], counted: [text], choice: choice}
    wire = P.write_box!(box)
    bits = bits_of(P.write_box!(box), &P.read_box_bits/2)
    check(P.read_box(wire, bits) == {:ok, box}, "composition")
    check(P.read_box(wire, bits - 1) == :error, "composition truncation")
  end

  def run do
    for line <- IO.stream(:stdio, :line) do
      [bound, raw] = line |> String.trim() |> String.split(" ")
      wire = if raw == "-", do: <<>>, else: Base.decode16!(raw, case: :mixed)

      {read, write, read_bits} =
        case bound do
          "7" -> {&W.read_wide_seven/2, &W.write_wide_seven!/1, &W.read_wide_seven_bits/2}
          "4" -> {&W.read_wide_four/2, &W.write_wide_four!/1, &W.read_wide_four_bits/2}
        end

      case read.(wire, byte_size(wire) * 8) do
        :error ->
          IO.puts("REFUSE")

        {:ok, value} ->
          bits = bits_of(write.(value), read_bits)

          check(
            read.(wire, bits) == {:ok, value} and read.(wire, bits - 1) == :error,
            "exact bit bound"
          )

          encoded = write.(value)
          IO.puts("OK #{bits} #{units(value.text)} #{bits} #{hex(encoded)}")
      end
    end
  end
end
