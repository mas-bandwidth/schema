defmodule PacketWideTest do
  alias Wide.WideText, as: W
  alias Wideprobe.Shapes, as: P

  def check(true, _message), do: :ok
  def check(false, message), do: raise(message)
  def hex(bytes), do: Base.encode16(bytes, case: :lower)
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
          W.write_wide_seven(%Wide.WideSeven{text: text})
          false
        rescue
          ArgumentError -> true
        end

      check(refused, "writer odd/bounds/null")
    end

    wire = W.write_wide_seven(%Wide.WideSeven{text: <<0xD800::little-16>>})
    check(W.read_wide_seven(wire, 35) == :error, "unpaired high")
    v = %Wide.WideSeven{text: <<0xFFFF::little-16>>}
    wire = W.write_wide_seven(v)
    check(W.read_wide_seven(wire, 35) == {:ok, v}, "FFFF binary")
    c = %Wideprobe.Conditional{enabled: true, text: <<0xD800::little-16, 0xDC00::little-16>>}
    wire = P.write_conditional(c)

    check(
      P.measure_conditional(c) == 68 and Bitwise.band(:binary.at(wire, 0), 15) == 5,
      "unaligned groups"
    )

    check(P.read_conditional(wire, 68) == {:ok, c}, "conditional")
    wire = P.write_conditional(%{c | enabled: false})
    check(P.read_conditional(wire, 1) == {:ok, %Wideprobe.Conditional{}}, "branch zero")
    text = %Wideprobe.Text{value: <<0xFFFF::little-16>>}
    choice = %Wideprobe.Choice{type: 1, text: text}
    wire = P.write_choice(choice)

    for _ <- 1..2 do
      check(P.read_choice(wire, P.measure_choice(choice)) == {:ok, choice}, "repeated union")
    end

    box = %Wideprobe.Box{}
    check(length(box.counted) == 1, "born count")
    box = %{box | items: [text, %Wideprobe.Text{}], counted: [text], choice: choice}
    wire = P.write_box(box)
    bits = P.measure_box(box)
    check(P.read_box(wire, bits) == {:ok, box}, "composition")
    check(P.read_box(wire, bits - 1) == :error, "composition truncation")
  end

  def run do
    for line <- IO.stream(:stdio, :line) do
      [bound, raw] = line |> String.trim() |> String.split(" ")
      wire = if raw == "-", do: <<>>, else: Base.decode16!(raw, case: :mixed)

      {read, write, measure} =
        case bound do
          "7" -> {&W.read_wide_seven/2, &W.write_wide_seven/1, &W.measure_wide_seven/1}
          "4" -> {&W.read_wide_four/2, &W.write_wide_four/1, &W.measure_wide_four/1}
        end

      case read.(wire, byte_size(wire) * 8) do
        :error ->
          IO.puts("REFUSE")

        {:ok, value} ->
          bits = measure.(value)

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
