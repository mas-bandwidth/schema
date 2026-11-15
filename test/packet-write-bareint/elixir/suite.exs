defmodule PacketWriteBareIntTest do
  # Each case of the shared list is written through the verdict form
  # write_<type>/1: a "refused" case must return {:error, _}, a "written" case
  # {:ok, wire} that reads back the same value. A float of each type is refused
  # too: a value that is not an integer is outside every bare width. The check
  # is the verdict only, never the message text.

  def check(true, _what), do: :ok

  def check(false, what) do
    IO.puts("FAILED: packet-write-bareint #{what}")
    Process.put(:failed, true)
  end

  def cases(path) do
    path
    |> File.read!()
    |> String.split("\n")
    |> Enum.map(&String.trim/1)
    |> Enum.reject(&(&1 == "" or String.starts_with?(&1, "#")))
    |> Enum.map(fn line ->
      [name, type, value, verdict] = String.split(line)
      {name, type, String.to_integer(value), verdict}
    end)
  end

  def write(type, value) do
    lower = String.downcase(type)
    module = Module.concat(Bareint, type)
    # the verdict form returns; anything it raises is a broken verdict
    try do
      apply(Bareint.BareInt, String.to_atom("write_" <> lower), [struct(module, value: value)])
    rescue
      e -> {:raised, e}
    end
  end

  def read(type, wire) do
    lower = String.downcase(type)
    apply(Bareint.BareInt, String.to_atom("read_" <> lower), [wire, byte_size(wire) * 8])
  end

  def verdict(type, value, "refused", label) do
    check(match?({:error, _}, write(type, value)), "#{label} refused")
  end

  def verdict(type, value, "written", label) do
    module = Module.concat(Bareint, type)
    want = struct(module, value: value)

    case write(type, value) do
      {:ok, wire} -> check(read(type, wire) == {:ok, want}, "#{label} reads back")
      _ -> check(false, "#{label} written")
    end
  end

  def run(path) do
    list = cases(path)
    check(list != [], "cases list is not empty")

    for {name, type, value, want} <- list do
      verdict(type, value, want, name)
    end

    for type <- list |> Enum.map(&elem(&1, 1)) |> Enum.uniq() do
      verdict(type, 1.5, "refused", "#{type} non-integer")
    end

    if Process.get(:failed, false), do: System.halt(1)

    IO.puts(
      "packet write bare-int Elixir: #{length(list)} cases; out-of-width and non-integer values refuse, boundaries round-trip"
    )
  end
end
