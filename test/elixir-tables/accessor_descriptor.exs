# The Elixir leg's accessor/descriptor agreement gate (docs/PORTING.md §J1,
# issue #421). The leg emits two derivations of one layout — the TYPED row/slot
# accessors beside the block-record and cook descriptors — and this script holds
# them against each other by reading the same synthetic bytes TWICE and
# requiring agreement.
#
# VALUE agreement, not offset agreement. On Go the accessor's offset is a number
# `unsafe.Offsetof` will tell you; on the BEAM the accessor's offset is a literal
# baked into the body of a generated function, and nothing can read it back out.
# So this leg measures the two ANSWERS: read the bytes through the accessor,
# read the same bytes through a reader written here from the descriptor's own
# columns, and compare.
#
# The bytes are SYNTHETIC on purpose: this needs no fixture, no manifest, no
# harness run. A block row is `size` bytes of `rem(i * 37 + 11, 251)`, which a
# four-byte shift cannot reproduce (a repeating fill would let a moved offset
# read the same value and pass). A cook region is the same pattern with a
# distinct eight-byte signed little-endian delta written at each pointer slot, so
# every slot resolves to a different target and a slot read eight bytes off
# lands on another slot's delta or on pattern bytes and disagrees either way.
#
# SCOPE: the walk holds PLAIN SCALAR block-record fields and POINTER cook slots,
# and nothing else — exactly the two things the two controls move. Every other
# descriptor class is counted and named `not yet held`, never silently skipped,
# and a class this script does not recognize is refused by name.
#
# Prints the four counts (modules, descriptors, comparisons, not-yet-held), then
# OK and exit 0 — or the disagreement line and exit 1. A run that loads no
# beams, walks no descriptor or compares no field is a false green and exits
# non-zero.

defmodule AccessorDescriptor do
  def pattern(i), do: rem(i * 37 + 11, 251)

  def filled(size) do
    for i <- 0..(size - 1)//1, into: <<>>, do: <<pattern(i)>>
  end

  # The descriptor half of a block scalar, written HERE in plain binary matching
  # rather than through B.* — reading the emitter's own arithmetic twice is the
  # very defect this gate exists to catch.
  def u(data, at, width) do
    <<_::binary-size(^at), v::little-unsigned-size(^width)-unit(8), _::binary>> = data
    v
  end

  def s(data, at, width) do
    <<_::binary-size(^at), v::little-signed-size(^width)-unit(8), _::binary>> = data
    v
  end

  def scalar(data, at, kind, width) do
    cond do
      kind == 1 -> u(data, at, 1) != 0
      kind in 2..5 -> s(data, at, width)
      kind in 6..9 -> u(data, at, width)
      kind in 10..11 -> u(data, at, width)
      true -> raise "a plain scalar rode kind #{kind}, which this gate does not hold"
    end
  end

  # The descriptor half of a cook pointer slot: the eight-byte signed self-
  # relative delta read at `at`, resolved the same way CookRuntime.deref/2
  # resolves it — but written here, not through C.*.
  def deref(region, at) do
    delta = s(region, at, 8)

    cond do
      delta == 0 ->
        :null

      true ->
        target = at + delta
        if target < 0 or target >= byte_size(region), do: :error, else: {:ok, target}
    end
  end

  def classify_block(f) do
    cond do
      f.out_of_line -> {:noth, "out_of_line"}
      f.counted -> {:noth, "counted"}
      f.is_array -> {:noth, "array"}
      f.optional -> {:noth, "optional"}
      f.element != nil -> {:noth, "record"}
      true -> {:hold, f.kind, f.elem_size}
    end
  end

  def classify_cook(f) do
    cond do
      f.is_pointer ->
        {:hold}

      f.storage == :string ->
        {:noth, "string"}

      f.storage == :bytes ->
        {:noth, "bytes"}

      f.storage == :record ->
        {:noth, "record"}

      f.is_array ->
        {:noth, "array"}

      f.storage in [:unsigned, :signed, :float, :bool] ->
        {:noth, "scalar"}

      true ->
        raise "a cook slot rode storage #{inspect(f.storage)}, which this gate does not hold"
    end
  end

  # A generous region filled with the pattern, with a DISTINCT signed delta
  # written at each pointer slot so each resolves to a different in-bounds
  # target. The deltas are placed in the upper half of the region, far from the
  # record's own bytes.
  def cook_region(_info, pointers) do
    size = 4096
    base = filled(size)

    Enum.reduce(Enum.with_index(pointers), base, fn {f, i}, region ->
      off = f.offset
      target = 2048 + i * 16

      <<head::binary-size(^off), _::binary-size(8), tail::binary>> = region
      <<head::binary, target - off::little-signed-64, tail::binary>>
    end)
  end
end

ebin = System.get_env("ELIXIR_TABLES_EBIN", "build/elixir-tables-ebin")
Code.append_path(ebin)

mods =
  Path.wildcard(ebin <> "/*.beam")
  |> Enum.flat_map(fn path ->
    mod = path |> Path.basename(".beam") |> String.to_atom()

    case Code.ensure_loaded(mod) do
      {:module, _} -> [mod]
      _ -> []
    end
  end)

block_records =
  Enum.flat_map(mods, fn mod ->
    for {name, 0} <- mod.__info__(:functions),
        String.starts_with?(Atom.to_string(name), "block_record_"),
        do: {mod, name}
  end)

cook_infos =
  Enum.flat_map(mods, fn mod ->
    for {name, 0} <- mod.__info__(:functions),
        String.starts_with?(Atom.to_string(name), "cook_info_"),
        do: {mod, name}
  end)

{block_held, block_noth, block_fails} =
  Enum.reduce(block_records, {0, %{}, []}, fn {mod, fun}, {held, noth, fails} ->
    info = apply(mod, fun, [])
    lo = String.replace_prefix(Atom.to_string(fun), "block_record_", "")
    row = AccessorDescriptor.filled(info.size)

    Enum.reduce(info.fields, {held, noth, fails}, fn f, {held, noth, fails} ->
      case AccessorDescriptor.classify_block(f) do
        {:hold, kind, width} ->
          av = apply(mod, String.to_atom("#{lo}_row_#{f.name}"), [row])
          dv = AccessorDescriptor.scalar(row, f.offset, kind, width)

          if av == dv do
            {held + 1, noth, fails}
          else
            msg =
              "#{lo}.#{f.name}: the accessor and the descriptor disagree about the value: " <>
                "accessor #{inspect(av)} != descriptor #{inspect(dv)}"

            IO.puts(msg)
            {held + 1, noth, [msg | fails]}
          end

        {:noth, class} ->
          {held, Map.update(noth, class, 1, &(&1 + 1)), fails}
      end
    end)
  end)

{cook_held, cook_noth, cook_fails} =
  Enum.reduce(cook_infos, {0, %{}, []}, fn {mod, fun}, {held, noth, fails} ->
    info = apply(mod, fun, [])
    lo = String.replace_prefix(Atom.to_string(fun), "cook_info_", "")
    pointers = Enum.filter(info.fields, & &1.is_pointer)
    region = AccessorDescriptor.cook_region(info, pointers)

    Enum.reduce(info.fields, {held, noth, fails}, fn f, {held, noth, fails} ->
      case AccessorDescriptor.classify_cook(f) do
        {:hold} ->
          av = apply(mod, String.to_atom("#{lo}_node_#{f.name}"), [region, 0])
          dv = AccessorDescriptor.deref(region, f.offset)

          if av == dv do
            {held + 1, noth, fails}
          else
            msg =
              "#{lo}.#{f.name}: the slot accessor and the descriptor disagree about the delta: " <>
                "accessor #{inspect(av)} != descriptor #{inspect(dv)}"

            IO.puts(msg)
            {held + 1, noth, [msg | fails]}
          end

        {:noth, class} ->
          {held, Map.update(noth, class, 1, &(&1 + 1)), fails}
      end
    end)
  end)

modules = Enum.uniq_by(block_records ++ cook_infos, fn {m, _} -> m end) |> length()
descriptors = length(block_records) + length(cook_infos)
comparisons = block_held + cook_held
noth = Map.merge(block_noth, cook_noth, fn _k, a, b -> a + b end)
noth_total = Enum.sum(Map.values(noth))

IO.puts("modules=#{modules} descriptors=#{descriptors} comparisons=#{comparisons}")

IO.puts(
  "not yet held: #{noth_total} fields in #{map_size(noth)} classes: " <>
    (noth
     |> Enum.sort()
     |> Enum.map_join(" ", fn {k, v} -> "#{k}=#{v}" end))
)

fails = block_fails ++ cook_fails

cond do
  modules == 0 ->
    IO.puts(
      :stderr,
      "the gate loaded no module exporting a block_record_ or cook_info_ descriptor"
    )

    System.halt(1)

  descriptors == 0 ->
    IO.puts(:stderr, "the gate walked no descriptors")
    System.halt(1)

  comparisons == 0 ->
    IO.puts(:stderr, "the gate compared no fields")
    System.halt(1)

  fails != [] ->
    IO.puts(:stderr, "#{length(fails)} accessor/descriptor disagreement(s)")
    System.halt(1)

  true ->
    IO.puts("OK")
end
