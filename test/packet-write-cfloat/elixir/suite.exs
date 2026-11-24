defmodule PacketWriteCFloatTest do
  alias Cfloat.CFloat, as: C
  alias Cfloat.RatioEndpoint, as: R
  alias Cfloat.RatioPair, as: P
  alias Cfloat.PacketCFloat, as: W

  def fail(what) do
    IO.puts("FAILED: packet-write-cfloat #{what}")
    Process.put(:failed, Process.get(:failed, 0) + 1)
  end

  def check(true, _what), do: :ok
  def check(_, what), do: fail(what)

  # The shared case list names a NaN and the two infinities by their IEEE
  # names; a BEAM float holds neither, so they reach the writer as the atoms
  # the port already refuses. Every other value is an ordinary float literal.
  def value("NaN"), do: :nan
  def value("+Inf"), do: :infinity
  def value("-Inf"), do: :neg_infinity

  def value(text) do
    {v, ""} = Float.parse(text)
    v
  end

  def cases(path) do
    path
    |> File.read!()
    |> String.split("\n", trim: true)
    |> Enum.reject(&String.starts_with?(&1, "#"))
    |> Enum.map(&String.split/1)
    |> Enum.reject(&(&1 == []))
  end

  # The modifiers after the verdict, in pairs: reads-back <v>, type
  # RatioEndpoint or RatioPair, wire <hex>, bits <n>.
  def modifiers(text, rest) do
    rest
    |> Enum.chunk_every(2)
    |> Enum.reduce({:ok, %{want: value(text), type: :c_float, wire: nil, bits: nil}}, fn
      ["reads-back", v], {:ok, m} -> {:ok, %{m | want: value(v)}}
      ["type", "RatioEndpoint"], {:ok, m} -> {:ok, %{m | type: :ratio}}
      ["type", "RatioPair"], {:ok, m} -> {:ok, %{m | type: :pair}}
      ["wire", hex], {:ok, m} -> {:ok, %{m | wire: String.downcase(hex)}}
      ["bits", n], {:ok, m} -> bits_modifier(Integer.parse(n), m)
      other, _ -> {:error, other}
    end)
  end

  defp bits_modifier({n, ""}, m) when n >= 0, do: {:ok, %{m | bits: n}}
  defp bits_modifier(other, _m), do: {:error, other}

  def write(:c_float, value), do: W.write_c_float(%C{x: value})
  def write(:ratio, value), do: W.write_ratio_endpoint(%R{ratio: value})
  def write(:pair, value), do: W.write_ratio_pair(%P{first: value, second: value})

  def write!(:c_float, value), do: W.write_c_float!(%C{x: value})
  def write!(:ratio, value), do: W.write_ratio_endpoint!(%R{ratio: value})
  def write!(:pair, value), do: W.write_ratio_pair!(%P{first: value, second: value})

  # The writer returns a binary, not bits: a case that pins its bits holds
  # the declared width (the generated <t>_max_bits/0) to them, and the
  # bytes must read back through exactly that many bits.
  def max_bits(:c_float), do: W.c_float_max_bits()
  def max_bits(:ratio), do: W.ratio_endpoint_max_bits()
  def max_bits(:pair), do: W.ratio_pair_max_bits()

  def read_back(:c_float, wire, bits) do
    case W.read_c_float(wire, bits || bit_size(wire)) do
      {:ok, value} -> {:ok, value.x}
      other -> other
    end
  end

  def read_back(:ratio, wire, bits) do
    case W.read_ratio_endpoint(wire, bits || bit_size(wire)) do
      {:ok, value} -> {:ok, value.ratio}
      other -> other
    end
  end

  # a RatioPair reads back only when its two fields agree
  def read_back(:pair, wire, bits) do
    case W.read_ratio_pair(wire, bits || bit_size(wire)) do
      {:ok, %{first: v, second: v}} -> {:ok, v}
      {:ok, value} -> {:error, {:fields_differ, value.first, value.second}}
      other -> other
    end
  end

  def check_wire(_name, _form, _wire, nil), do: :ok

  def check_wire(name, form, wire, hex) do
    got = Base.encode16(wire, case: :lower)
    check(got == hex, "#{name}: the #{form} form wrote #{got}, want #{hex}")
  end

  def refused(name, value, m) do
    case write(m.type, value) do
      {:error, _message} -> :ok
      other -> fail("#{name}: the verdict form wrote #{inspect(other)}")
    end

    try do
      write!(m.type, value)
      fail("#{name}: the bang form wrote an off-range value")
    rescue
      _e in ArgumentError -> :ok
      e -> fail("#{name}: the bang form raised #{inspect(e.__struct__)}")
    end
  end

  def written(name, value, m) do
    want = m.want

    if m.bits != nil do
      check(
        max_bits(m.type) == m.bits,
        "#{name}: the declared width is #{max_bits(m.type)} bits, want #{m.bits}"
      )
    end

    case write(m.type, value) do
      {:ok, wire} ->
        check_wire(name, "verdict", wire, m.wire)

        case read_back(m.type, wire, m.bits) do
          {:ok, got} ->
            check(got == want, "#{name}: read back #{inspect(got)}, want #{inspect(want)}")

          other ->
            fail("#{name}: the verdict form's wire did not read: #{inspect(other)}")
        end

      other ->
        fail("#{name}: the verdict form refused a lawful value: #{inspect(other)}")
    end

    try do
      wire = write!(m.type, value)
      check_wire(name, "bang", wire, m.wire)

      case read_back(m.type, wire, m.bits) do
        {:ok, got} ->
          check(got == want, "#{name}: bang read back #{inspect(got)}, want #{inspect(want)}")

        other ->
          fail("#{name}: the bang form's wire did not read: #{inspect(other)}")
      end
    rescue
      e -> fail("#{name}: the bang form raised on a lawful value: #{inspect(e)}")
    end
  end

  def run(path) do
    list = cases(path)

    for fields <- list do
      failed_before = Process.get(:failed, 0)
      name = List.first(fields)

      case fields do
        [name, text, verdict | rest] when verdict in ["refused", "written"] ->
          case modifiers(text, rest) do
            {:ok, m} when verdict == "refused" -> refused(name, value(text), m)
            {:ok, m} -> written(name, value(text), m)
            {:error, bad} -> fail("#{name}: malformed modifier #{inspect(bad)}")
          end

        other ->
          fail("malformed case line: #{inspect(other)}")
      end

      # the case's answer tools/casegate reads (the FAILED line above says why)
      answer =
        if Process.get(:failed, 0) == failed_before,
          do: "ok",
          else: "FAILED: see the FAILED line above"

      IO.puts("case #{name} #{answer}")
    end

    # A summary; tools/casegate holds every case of the file to an answer.
    ran = length(list)
    failed = Process.get(:failed, 0)
    IO.puts("packet-write-cfloat Elixir: ran #{ran}, failed #{failed}")

    if failed != 0 or ran == 0 do
      System.halt(1)
    end
  end
end
