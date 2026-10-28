defmodule PacketFiniteTest do
  import Bitwise

  alias Packetfinite.Finite, as: F
  alias Packetfinite.Pair, as: P
  alias Packetfinite.FiniteFile, as: W

  # Wire bit offsets of the base instance (one element in each list): the
  # aligned float64 at 0, the float32 at 64, the flag at 96, the unaligned
  # float64 at 97, the singles count at 161, singles[0] at 164, the doubles
  # count at 196, doubles[0] at 199.
  @aligned 0
  @single 64
  @unaligned 97
  @single0 164
  @double0 199

  @nan32 [0x7FC00000, 0x7F800001, 0xFFC00001, 0x7FFFFFFF, 0xFFBFFFFF]
  @inf32 [0x7F800000, 0xFF800000]
  @nan64 [0x7FF8000000000000, 0x7FF0000000000001, 0xFFF8000000000001, 0x7FFFFFFFFFFFFFFF]
  @inf64 [0x7FF0000000000000, 0xFFF0000000000000]

  def base do
    %F{
      aligned: 1.5,
      single: 2.5,
      flag: true,
      unaligned: -3.25,
      singles: [4.5],
      doubles: [5.75],
      quantized: 1.0
    }
  end

  def fail(what) do
    IO.puts("FAILED: #{what}")
    Process.put(:failed, true)
  end

  def check(true, _what), do: :ok
  def check(_, what), do: fail(what)

  # The wire as one little-endian integer: bit k of the stream is bit k of
  # the integer, so a field at bit offset o of width w is (n >>> o) &&& mask.
  def splice(wire, offset, width, bits) do
    n = :binary.decode_unsigned(wire, :little)
    mask = (1 <<< width) - 1
    n = (n &&& bnot(mask <<< offset)) ||| bits <<< offset
    <<n::little-size(byte_size(wire))-unit(8)>>
  end

  def field(wire, offset, width) do
    :binary.decode_unsigned(wire, :little) >>> offset &&& (1 <<< width) - 1
  end

  def f32(x) do
    <<b::32>> = <<x::float-32>>
    b
  end

  def f64(x) do
    <<b::64>> = <<x::float-64>>
    b
  end

  # A read verdict that never raises: a raise is itself a broken refusal.
  def read(fun, wire) do
    fun.(wire, bit_size(wire))
  rescue
    e -> {:raised, e}
  end

  def refused_write(value, write, write!, what) do
    case write.(value) do
      {:error, "non_finite_float" <> _} -> :ok
      other -> fail("non_finite_float write #{what}: #{inspect(other)}")
    end

    try do
      write!.(value)
      fail("non_finite_float write! #{what}: did not raise")
    rescue
      e in ArgumentError ->
        check(
          String.starts_with?(Exception.message(e), "non_finite_float"),
          "non_finite_float write! #{what}: #{Exception.message(e)}"
        )
    end
  end

  def write_refusals do
    bad = [:nan, :infinity, :neg_infinity, {:nonfinite, 0x7FC00000}, nil]
    w = &W.write_finite/1
    w! = &W.write_finite!/1

    for b <- bad do
      refused_write(%{base() | single: b}, w, w!, "float32 field #{inspect(b)}")
      refused_write(%{base() | aligned: b}, w, w!, "aligned float64 field #{inspect(b)}")
      refused_write(%{base() | unaligned: b}, w, w!, "unaligned float64 field #{inspect(b)}")
      refused_write(%{base() | singles: [1.0, b]}, w, w!, "float32 element #{inspect(b)}")
      refused_write(%{base() | doubles: [1.0, b]}, w, w!, "float64 element #{inspect(b)}")
      refused_write(%{base() | quantized: b}, w, w!, "compressed float #{inspect(b)}")

      refused_write(
        %P{values: [b, 1.0]},
        &W.write_pair/1,
        &W.write_pair!/1,
        "aligned float64 element #{inspect(b)}"
      )
    end

    # a finite float that overflows float32 is an infinity at the wire's width
    refused_write(%{base() | single: 1.0e39}, w, w!, "float32 overflow")
    refused_write(%{base() | singles: [-1.0e39]}, w, w!, "float32 element overflow")
  end

  def read_refusals do
    wire = W.write_finite!(base())
    r = &W.read_finite/2
    check(match?({:ok, _}, read(r, wire)), "base instance reads")

    sites = [
      {"aligned float64", @aligned, 64, @nan64 ++ @inf64},
      {"float32", @single, 32, @nan32 ++ @inf32},
      {"unaligned float64", @unaligned, 64, @nan64 ++ @inf64},
      {"float32 element", @single0, 32, @nan32 ++ @inf32},
      {"float64 element", @double0, 64, @nan64 ++ @inf64}
    ]

    for {what, offset, width, patterns} <- sites, bits <- patterns do
      crafted = splice(wire, offset, width, bits)
      verdict = read(r, crafted)

      check(
        verdict == :error,
        "non_finite_float read #{what} 0x#{Integer.to_string(bits, 16)}: #{inspect(verdict)}"
      )
    end

    pair = W.write_pair!(%P{values: [1.0, 2.0]})

    for slot <- [0, 64], bits <- @nan64 ++ @inf64 do
      crafted = splice(pair, slot, 64, bits)
      verdict = read(&W.read_pair/2, crafted)

      check(
        verdict == :error,
        "non_finite_float read aligned element @#{slot} 0x#{Integer.to_string(bits, 16)}: #{inspect(verdict)}"
      )
    end
  end

  # -0.0 and the subnormals are values: written and read bit exact.
  def bit_exact do
    sub32 = 1.401298464324817e-45
    sub64 = 5.0e-324

    for {s, d} <- [{-0.0, -0.0}, {sub32, sub64}, {-sub32, -sub64}] do
      value = %{base() | single: s, aligned: d, unaligned: d, singles: [s], doubles: [d]}
      wire = W.write_finite!(value)
      check(field(wire, @single, 32) == f32(s), "float32 #{s} writes bit exact")
      check(field(wire, @aligned, 64) == f64(d), "aligned float64 #{d} writes bit exact")
      check(field(wire, @unaligned, 64) == f64(d), "unaligned float64 #{d} writes bit exact")
      check(field(wire, @single0, 32) == f32(s), "float32 element #{s} writes bit exact")
      check(field(wire, @double0, 64) == f64(d), "float64 element #{d} writes bit exact")

      case read(&W.read_finite/2, wire) do
        {:ok, out} ->
          check(f32(out.single) == f32(s), "float32 #{s} reads bit exact")
          check(f64(out.aligned) == f64(d), "aligned float64 #{d} reads bit exact")
          check(f64(out.unaligned) == f64(d), "unaligned float64 #{d} reads bit exact")
          check(Enum.map(out.singles, &f32/1) == [f32(s)], "float32 element #{s} reads bit exact")
          check(Enum.map(out.doubles, &f64/1) == [f64(d)], "float64 element #{d} reads bit exact")

        other ->
          fail("#{s}/#{d} does not read: #{inspect(other)}")
      end

      pair = W.write_pair!(%P{values: [d, -d]})
      {:ok, out} = W.read_pair(pair, bit_size(pair))
      check(Enum.map(out.values, &f64/1) == [f64(d), f64(-d)], "aligned elements #{d} bit exact")
    end
  end

  def run do
    write_refusals()
    read_refusals()
    bit_exact()

    if Process.get(:failed) do
      System.halt(1)
    end

    IO.puts("OK")
  end
end
