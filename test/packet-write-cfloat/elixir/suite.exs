defmodule PacketWriteCFloatTest do
  alias Cfloat.CFloat, as: C
  alias Cfloat.CFloatFile, as: W

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
  end

  def expected(_text, ["reads-back", reads_back | _]), do: value(reads_back)
  def expected(text, _rest), do: value(text)

  def read_back(wire) do
    case W.read_c_float(wire, bit_size(wire)) do
      {:ok, value} -> {:ok, value.x}
      other -> other
    end
  end

  def refused(name, value) do
    case W.write_c_float(%C{x: value}) do
      {:error, _message} -> :ok
      other -> fail("#{name}: the verdict form wrote #{inspect(other)}")
    end

    try do
      W.write_c_float!(%C{x: value})
      fail("#{name}: the bang form wrote an off-range value")
    rescue
      _e in ArgumentError -> :ok
      e -> fail("#{name}: the bang form raised #{inspect(e.__struct__)}")
    end
  end

  def written(name, value, want) do
    case W.write_c_float(%C{x: value}) do
      {:ok, wire} ->
        case read_back(wire) do
          {:ok, got} ->
            check(got == want, "#{name}: read back #{inspect(got)}, want #{inspect(want)}")

          other ->
            fail("#{name}: the verdict form's wire did not read: #{inspect(other)}")
        end

      other ->
        fail("#{name}: the verdict form refused a lawful value: #{inspect(other)}")
    end

    try do
      wire = W.write_c_float!(%C{x: value})

      case read_back(wire) do
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
      case fields do
        [name, text, "refused" | _rest] -> refused(name, value(text))
        [name, text, "written" | rest] -> written(name, value(text), expected(text, rest))
        other -> fail("malformed case line: #{inspect(other)}")
      end
    end

    # One unit per shared case: tools/mustrun holds the count to the one the
    # recipe registers, so an emptied or shortened cases.txt is red.
    ran = length(list)
    failed = Process.get(:failed, 0)
    IO.puts("packet-write-cfloat Elixir: ran #{ran}, failed #{failed}")

    if failed != 0 or ran == 0 do
      System.halt(1)
    end
  end
end
