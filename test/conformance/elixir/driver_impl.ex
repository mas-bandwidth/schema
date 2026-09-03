# The ELIXIR leg of the tables conformance harness (test/conformance/README.md).
#
# One process per surface. The manifest is the DERIVED one the harness writes —
# the committed rows with the materialised fixture paths folded in and every
# expected answer removed — so this driver cannot pass by reading the answer.
#
# Dispatch is by SEARCH, not by a generated index: a unit's namespace is the
# camel case of its manifest key, and the module holding a root's codecs is
# whichever <Ns>.<Base>Table module exports load_<root>/1. That keeps the leg
# free of a generated name nothing else in the language needs.

# The block's canonical ROW dump (docs/SPEC-TABLES.md §19.2).
#
# The `block` surface says only that an image OPENS, which a reader passes by
# checking the prologue and stopping. This is the value-for-value read, so two
# implementations' reads of the same bytes are byte-compared — and it is
# produced from §8's DESCRIPTORS and nothing else, no generated row accessor,
# because that is the claim §19.2 makes for them.
#
# A FLOAT is its IEEE-754 BIT PATTERN. A block row is a byte-identical
# projection, so its bits are the fact; a decimal spelling would be a rounding
# rule two languages have to agree on for no gain.
defmodule BlockDump do
  def dump(base, info) do
    IO.iodata_to_binary([
      "projection #{info.name} @0\n",
      record(base, 0, info, ""),
      Enum.map(info.fields, fn f -> if f.out_of_line, do: array(base, f), else: [] end)
    ])
  end

  defp array(base, f) do
    offset_of = u(base, f.offset_of_offset, 8)
    count = u(base, f.count_offset, 4)
    stride = u(base, f.stride_offset, 4)
    row = apply(elem(f.element, 0), elem(f.element, 1), [])

    [
      "array #{f.name} #{row.name} @#{offset_of} count=#{count} stride=#{stride}\n",
      Enum.map(0..(count - 1)//1, fn r ->
        at = offset_of + r * stride
        ["row #{r} @#{at}\n", record(base, at, row, "")]
      end)
    ]
  end

  # One record's leaves, at two spaces, in descriptor order. Out-of-line arrays
  # are the caller's business: they are a section of their own, not a leaf.
  defp record(base, at, info, path) do
    Enum.map(info.fields, fn f ->
      cond do
        f.out_of_line ->
          []

        f.counted ->
          # a string or a `bytes`: the used length lives at count_offset
          used = i32(base, at + f.count_offset)

          if used < 0 or used > f.array_bound do
            raise "#{info.name}.#{f.name} carries a used length of #{used}, " <>
                    "outside [ 0, #{f.array_bound} ]"
          end

          ["  #{join(path, f.name)} = #{text(base, at + f.offset, used)}\n", present(base, at, f, path)]

        true ->
          slots = if f.is_array, do: f.array_bound, else: 1

          [
            Enum.map(0..(slots - 1)//1, fn slot ->
              name = if f.is_array, do: "#{join(path, f.name)}[#{slot}]", else: join(path, f.name)
              value = at + f.offset + slot * f.elem_size

              case f.element do
                nil -> "  #{name} = #{scalar(base, value, f.kind, f.elem_size)}\n"
                {m, fun} -> record(base, value, apply(m, fun, []), name)
              end
            end),
            present(base, at, f, path)
          ]
      end
    end)
  end

  defp present(base, at, f, path) do
    if f.optional do
      "  #{join(path, f.name)}#present = #{u(base, at + f.present_offset, 1) != 0}\n"
    else
      []
    end
  end

  defp join("", name), do: name
  defp join(prefix, name), do: prefix <> "." <> name

  defp scalar(base, at, kind, width) do
    cond do
      kind == 1 -> if u(base, at, 1) != 0, do: "true", else: "false"
      kind == 10 -> "0x" <> pad(Integer.to_string(u(base, at, 4), 16), 8)
      kind == 11 -> "0x" <> pad(Integer.to_string(u(base, at, 8), 16), 16)
      kind >= 2 and kind <= 5 -> Integer.to_string(s(base, at, width))
      true -> Integer.to_string(u(base, at, width))
    end
  end

  defp pad(text, width), do: String.pad_leading(String.downcase(text), width, "0")

  defp text(base, at, used) do
    body =
      Enum.map(0..(used - 1)//1, fn i ->
        c = :binary.at(base, at + i)

        if c >= 0x20 and c < 0x7F and c != ?" and c != ?\\ do
          <<c>>
        else
          "\\x" <> pad(Integer.to_string(c, 16), 2)
        end
      end)

    IO.iodata_to_binary(["\"", body, "\" len=#{used}"])
  end

  defp u(data, at, width) do
    <<_::binary-size(^at), v::little-unsigned-size(^width)-unit(8), _::binary>> = data
    v
  end

  defp s(data, at, width) do
    <<_::binary-size(^at), v::little-signed-size(^width)-unit(8), _::binary>> = data
    v
  end

  defp i32(data, at), do: s(data, at, 4)
end

# The cook's canonical NODE dump (docs/SPEC-TABLES.md §7.5): the walk every
# reader makes through its OWN derefs, written as text, so two implementations'
# walks are byte-compared rather than merely both succeeding.
#
# A node is visited ONCE: sharing and a back-reference are the same fact (§6.3).
# A float has no canonical cross-language spelling here and the corpus this
# covers has none, so a dump that meets one REFUSES rather than inventing one.
defmodule CookDump do
  def dump(cook) do
    {_reached, out} = node(cook, 0, info(cook.info), 0, %{}, [])
    IO.iodata_to_binary(Enum.reverse(out))
  end

  defp info({m, f}), do: apply(m, f, [])

  defp node(cook, offset, rec, depth, reached, out) do
    if depth > 4096 do
      raise "the walk nested past any depth a region can hold — a cycle the deref did not close"
    end

    case Map.fetch(reached, offset) do
      {:ok, name} ->
        if name != rec.name do
          raise "two references name the node at offset #{offset} as two different tables: " <>
                  "#{name} and #{rec.name}"
        end

        {reached, out}

      :error ->
        if offset > cook.region_length or rec.size > cook.region_length - offset do
          raise "the node at offset #{offset} (#{rec.name}, size #{rec.size}) does not fit " <>
                  "inside the region's #{cook.region_length} bytes"
        end

        index = map_size(reached)
        reached = Map.put(reached, offset, rec.name)
        out = ["node #{index} #{rec.name} @#{offset}\n" | out]
        storage(cook, offset, rec, depth, "", reached, out)
    end
  end

  defp storage(cook, at, rec, depth, path, reached, out) do
    Enum.reduce(rec.fields, {reached, out}, fn f, {reached, out} ->
      name = join(path, f.name)

      # every COUNT COMPANION, against its declared bound, and a negative one
      # refuses too — an extent is never negative, and a walker handed one
      # indexes backwards out of the region (§7.4's pass two)
      used =
        if f.count_offset >= 0 do
          u = i32(cook.region, at + f.count_offset)

          if u < 0 or u > f.array_bound do
            raise "#{rec.name}.#{f.name} carries a count companion of #{u}, " <>
                    "outside [ 0, #{f.array_bound} ]"
          end

          u
        else
          -1
        end

      {reached, out} =
        cond do
          f.is_pointer ->
            pointer(cook, at, f, name, depth, reached, out)

          f.storage in [:string, :bytes] ->
            {reached, [line(name, text(cook.region, at + f.offset, used)) | out]}

          f.storage == :record ->
            # a nested record — by value, or every slot of an array of them. A
            # COUNTED array writes all N slots (§7.2), and a slot past the live
            # count holds the value-initialised element.
            Enum.reduce(0..(slots(f) - 1)//1, {reached, out}, fn slot, {reached, out} ->
              storage(cook, at + f.offset + slot * f.elem_size, info(f.record), depth,
                      slot_path(f, name, slot), reached, out)
            end)

          true ->
            Enum.reduce(0..(slots(f) - 1)//1, {reached, out}, fn slot, {reached, out} ->
              value = scalar(cook.region, at + f.offset + slot * f.elem_size, f.storage, f.elem_size)
              {reached, [line(slot_path(f, name, slot), value) | out]}
            end)
        end

      out =
        if f.count_offset >= 0 and f.storage not in [:string, :bytes] do
          [line(name <> "#count", Integer.to_string(used)) | out]
        else
          out
        end

      out =
        if f.present_offset >= 0 do
          present = u(cook.region, at + f.present_offset, 1) != 0
          [line(name <> "#present", if(present, do: "true", else: "false")) | out]
        else
          out
        end

      {reached, out}
    end)
  end

  defp pointer(cook, at, f, name, depth, reached, out) do
    slot = at + f.offset
    delta = s(cook.region, slot, 8)

    if delta == 0 do
      # NULL IN A REGION IS A DELTA OF ZERO (§6.3)
      {reached, [line(name, "null") | out]}
    else
      target = slot + delta

      if target < 0 or target >= cook.region_length do
        raise "#{name} resolves outside the region — a delta of #{delta}"
      end

      out = [line(name, "-> @#{target}") | out]
      node(cook, target, info(f.record), depth + 1, reached, out)
    end
  end

  # the number of storage slots a field has, which is what a cook writes: a
  # COUNTED array writes all N slots (§7.2), a keyed array writes one per named
  # variant, and a fixed array writes N
  defp slots(f), do: if(f.is_array, do: f.array_bound, else: 1)

  defp slot_path(f, name, slot), do: if(f.is_array, do: "#{name}[#{slot}]", else: name)

  defp line(path, value), do: "  #{path} = #{value}\n"

  defp join("", name), do: name
  defp join(prefix, name), do: prefix <> "." <> name

  defp scalar(_region, _at, :float, _width) do
    raise "the dump met a float, whose canonical cross-language spelling this gate does not fix"
  end

  defp scalar(region, at, :bool, _width) do
    if u(region, at, 1) != 0, do: "true", else: "false"
  end

  defp scalar(region, at, :signed, width), do: Integer.to_string(s(region, at, width))
  defp scalar(region, at, _storage, width), do: Integer.to_string(u(region, at, width))

  defp text(region, at, used) do
    body =
      Enum.map(0..(used - 1)//1, fn i ->
        c = :binary.at(region, at + i)

        if c >= 0x20 and c < 0x7F and c != ?" and c != ?\\ do
          <<c>>
        else
          "\\x" <> String.pad_leading(String.downcase(Integer.to_string(c, 16)), 2, "0")
        end
      end)

    IO.iodata_to_binary(["\"", body, "\" len=#{used}"])
  end

  defp u(data, at, width) do
    <<_::binary-size(^at), v::little-unsigned-size(^width)-unit(8), _::binary>> = data
    v
  end

  defp s(data, at, width) do
    <<_::binary-size(^at), v::little-signed-size(^width)-unit(8), _::binary>> = data
    v
  end

  defp i32(data, at), do: s(data, at, 4)
end

defmodule Driver do
  @surfaces ~w(wire report json-read json-write json-hostile
               cook cook-foreign block block-foreign block-dump forgery cook-forgery)

  def main([manifest, "list"]) do
    _ = manifest
    Enum.each(@surfaces, &IO.puts/1)
  end

  def main([manifest, "alloc-audit"]) do
    Audit.run(parse(File.read!(manifest)))
  end

  def main([manifest, "soak", seconds]) do
    Audit.soak(parse(File.read!(manifest)), String.to_integer(seconds))
  end

  def main([manifest, "fuzz", iterations]) do
    Fuzz.run(parse(File.read!(manifest)), String.to_integer(iterations))
  end

  def main([manifest, surface, outdir]) do
    rows = parse(File.read!(manifest))
    run(surface, rows, outdir)
  end

  def main(_), do: usage()

  defp usage do
    IO.puts(:stderr, "usage: driver <manifest> list | <manifest> <surface> <outdir>")
    System.halt(64)
  end

  # ---- the manifest (testdata/conformance/tables/FORMAT.md) ----

  defp parse(text) do
    text
    |> String.split("\n")
    |> Enum.map(&String.trim/1)
    |> Enum.reject(fn line -> line == "" or String.starts_with?(line, "#") end)
    |> Enum.map(&String.split(&1, ~r/[ \t]+/))
    |> Enum.group_by(&hd/1, &tl/1)
  end

  defp rows(rows, kind), do: Map.get(rows, kind, [])

  # ---- module lookup ----

  defp namespace(unit), do: Module.concat([Macro.camelize(unit)])

  # the module exporting a root's codecs: whichever <Ns>.<Base>Table module has
  # the function. Loading is explicit because an .exs script runs with no
  # application to load the beams for it.
  def codec_module(unit, root), do: codec(unit, root)

  defp codec(unit, root) do
    want = String.to_atom("load_" <> Macro.underscore(root))
    prefix = Atom.to_string(namespace(unit)) <> "."

    Enum.find_value(modules(), fn mod ->
      name = Atom.to_string(mod)

      if String.starts_with?(name, prefix) and String.ends_with?(name, "Table") and
           function_exported?(mod, want, 1) do
        mod
      end
    end) || raise "no Elixir module exports #{want}/1 for unit #{unit}"
  end

  defp accessor(unit, root, prefix, arity) do
    want = String.to_atom(prefix <> "_" <> Macro.underscore(root))
    ns = Atom.to_string(namespace(unit)) <> "."

    Enum.find_value(modules(), fn mod ->
      name = Atom.to_string(mod)

      if String.starts_with?(name, ns) and function_exported?(mod, want, arity) do
        {mod, want}
      end
    end) || raise "no Elixir module exports #{want}/#{arity} for unit #{unit}"
  end

  defp modules do
    case :persistent_term.get(:driver_modules, nil) do
      nil ->
        mods =
          Path.wildcard(System.get_env("EBIN", "build/elixir-tables-ebin") <> "/*.beam")
          |> Enum.map(fn path ->
            mod = path |> Path.basename(".beam") |> String.to_atom()
            Code.ensure_loaded(mod)
            mod
          end)

        :persistent_term.put(:driver_modules, mods)
        mods

      mods ->
        mods
    end
  end

  # ---- the surfaces ----

  defp run("wire", rows, outdir) do
    for [name, unit, root, wire] <- rows(rows, "instance") do
      mod = codec(unit, root)
      lo = Macro.underscore(root)
      {value, _report} = apply(mod, String.to_atom("load_" <> lo), [File.read!(wire)])
      write(outdir, name, apply(mod, String.to_atom("save_" <> lo), [value]))
    end
  end

  defp run("report", rows, outdir) do
    for [name, unit, root, wire] <- rows(rows, "report") do
      mod = codec(unit, root)
      lo = Macro.underscore(root)
      {_value, report} = apply(mod, String.to_atom("load_" <> lo), [File.read!(wire)])
      write(outdir, name, counters(report))
    end
  end

  defp run("json-read", rows, outdir) do
    for [name, unit, root, _wire] <- rows(rows, "instance") do
      mod = codec(unit, root)
      lo = Macro.underscore(root)
      text = File.read!("testdata/conformance/tables/json/#{name}.json")
      {value, _report} = apply(mod, String.to_atom("from_json_" <> lo), [text])
      write(outdir, name, apply(mod, String.to_atom("save_" <> lo), [value]))
    end
  end

  defp run("json-write", rows, outdir) do
    for [name, unit, root, wire] <- rows(rows, "instance") do
      mod = codec(unit, root)
      lo = Macro.underscore(root)
      {value, _report} = apply(mod, String.to_atom("load_" <> lo), [File.read!(wire)])
      write(outdir, name <> ".json", apply(mod, String.to_atom("to_json_" <> lo), [value]))
    end
  end

  defp run("json-hostile", rows, outdir) do
    for [name, unit, root, tree] <- rows(rows, "json-hostile") do
      mod = codec(unit, root)
      lo = Macro.underscore(root)
      text = File.read!(Path.join(tree, root <> ".json"))
      {_value, report} = apply(mod, String.to_atom("from_json_" <> lo), [text])
      write(outdir, name, if(report.malformed, do: "refused\n", else: counters(report)))
    end
  end

  defp run("block", rows, outdir) do
    for [name, unit, image] <- rows(rows, "block") do
      write(outdir, name, block_verdict(unit, block_table(name), File.read!(image)))
    end
  end

  # THE FOREIGN-ORDER REFUSAL (test/conformance/README.md): the driver makes the
  # file foreign to ITSELF by reversing the eight bytes at offset 0 — the magic —
  # so whatever this build's order is, the magic it now reads is not this
  # build's. It is the check §19.1 and §7.1 put FIRST, for exactly this.
  defp run("block-foreign", rows, outdir) do
    for [name, unit, image] <- rows(rows, "block") do
      write(outdir, name, block_verdict(unit, block_table(name), foreign(File.read!(image))))
    end
  end

  defp run("cook-foreign", rows, outdir) do
    for [name, unit, root, file] <- rows(rows, "cook") do
      {mod, open} = accessor(unit, root, "cook_open", 1)

      verdict =
        case apply(mod, open, [foreign(File.read!(file))]) do
          {:ok, _cook} -> "open\n"
          :refuse -> "refuse\n"
        end

      write(outdir, name, verdict)
    end
  end

  defp run("block-dump", rows, outdir) do
    for [name, unit, image] <- rows(rows, "block") do
      root = block_table(name)
      mod = block_module(unit, root)
      lo = Macro.underscore(root)

      case apply(mod, String.to_atom("block_open_" <> lo), [File.read!(image)]) do
        {:ok, block} ->
          info = apply(mod, String.to_atom("block_info_" <> lo), [])
          write(outdir, name, BlockDump.dump(block.base, info))

        :refuse ->
          write(outdir, name, "refuse\n")
      end
    end
  end

  defp run("forgery", rows, outdir) do
    for [name, kind, subject, file, extent, pointer] <- rows(rows, "forgery"), kind == "block" do
      verdict =
        case claim(file, extent, pointer) do
          :no_buffer -> "refuse\n"
          bytes -> block_verdict(block_unit(subject), block_table(subject), bytes)
        end

      write(outdir, name, verdict)
    end
  end

  defp run("cook", rows, outdir) do
    for [name, unit, root, file] <- rows(rows, "cook") do
      {mod, open} = accessor(unit, root, "cook_open", 1)

      case apply(mod, open, [File.read!(file)]) do
        {:ok, cook} ->
          write(outdir, name, CookDump.dump(cook))

        :refuse ->
          raise "the cook #{root} did not open — the tool wrote it and this build cannot point at it"
      end
    end
  end

  defp run("cook-forgery", rows, outdir) do
    for [name, kind, subject, file, extent, pointer] <- rows(rows, "forgery"), kind == "cook" do
      unit = cook_unit(rows, subject)
      {mod, open} = accessor(unit, subject, "cook_open", 2)

      verdict =
        case claim(file, extent, pointer) do
          :no_buffer ->
            "refuse\n"

          bytes ->
            # the POINTER column is the lead of the buffer the caller holds, and
            # Open takes it because a BEAM binary cannot carry it (§7's base
            # alignment, and CookRuntime.open/5's note)
            case apply(mod, open, [bytes, String.to_integer(pointer)]) do
              {:ok, _cook} -> "open\n"
              :refuse -> "refuse\n"
            end
        end

      write(outdir, name, verdict)
    end
  end

  defp run(surface, _rows, _outdir) do
    IO.puts(:stderr, "the Elixir leg does not implement #{surface}")
    System.halt(2)
  end

  defp foreign(<<magic::binary-size(8), rest::binary>>) do
    magic |> :binary.bin_to_list() |> Enum.reverse() |> :binary.list_to_bin() |> Kernel.<>(rest)
  end

  def block_table_of(name), do: block_table(name)
  def block_module_of(unit, root), do: block_module(unit, root)
  def accessor_of(unit, root, prefix, arity), do: accessor(unit, root, prefix, arity)

  defp block_module(unit, root) do
    {mod, _} = accessor(unit, root, "block_open", 1)
    mod
  end

  defp block_verdict(unit, root, bytes) do
    open = String.to_atom("block_open_" <> Macro.underscore(root))

    case apply(block_module(unit, root), open, [bytes]) do
      {:ok, _block} -> "open\n"
      :refuse -> "refuse\n"
    end
  end

  # THE FIXTURE NAMES ARE THE DRIVER'S OWN, exactly as they are in the reference
  # leg: which table a block image projects is a property of the FIXTURE, and
  # the conformance data deliberately does not carry it.
  @block_tables %{"block_render" => {"blockdemo", "RenderFrame"},
                  "block_padded" => {"blockdemo", "PaddedFrame"}}

  defp block_table(name) do
    {_unit, root} = Map.fetch!(@block_tables, name)
    root
  end

  defp block_unit(name) do
    {unit, _root} = Map.fetch!(@block_tables, name)
    unit
  end

  defp cook_unit(rows, subject) do
    case Enum.find(rows(rows, "cook"), fn [_case, _unit, root, _file] -> root == subject end) do
      [_case, unit, _root, _file] -> unit
      nil -> raise "forgery subject #{subject} names no cook root"
    end
  end

  # A forgery line carries an EXTENT and a POINTER, and neither is a fact a file
  # can hold. The extent is the length the caller CLAIMS — larger than the file,
  # or shorter, which is what a truncation is. A driver allocates EXACTLY the
  # claim, copies what fits and zeroes the rest.
  #
  # THE POINTER COLUMN HAS NO ELIXIR MEANING and that is a fact about the
  # language, not a gap in the leg: a BEAM binary has no address a caller can
  # place, so an unaligned base is unexpressible here. The one pointer value the
  # language CAN express is `null`, which is no buffer at all, and that refuses.
  defp claim(file, extent, pointer) do
    if pointer == "null" do
      :no_buffer
    else
      bytes = File.read!(file)
      want = String.to_integer(extent)

      cond do
        want < 0 -> bytes
        want <= byte_size(bytes) -> binary_part(bytes, 0, want)
        true -> bytes <> :binary.copy(<<0>>, want - byte_size(bytes))
      end
    end
  end

  defp counters(report) do
    "#{report.unknown},#{report.kind_mismatch},#{report.clamped},#{report.duplicate}," <>
      "#{report.malformed}\n"
  end

  defp write(outdir, name, data) when is_binary(data) do
    File.write!(Path.join(outdir, name), data)
  end

  defp write(_outdir, name, :refused) do
    IO.puts(:stderr, "#{name}: the writer refused the value")
    System.halt(1)
  end
end

# ---------------------------------------------------------------------------
# THE ALLOCATION AUDIT, and the soak beside it
# ---------------------------------------------------------------------------
#
# The instrument first, because the number is worth what the instrument is.
#
# A soak that gates on heap DRIFT is a LEAK instrument, and it cannot see a
# per-iteration allocation that is collected: a path that allocates and frees
# the same words every iteration reads +0 drift for an hour and is still
# allocating. What "the read path costs this much" actually claims is a COUNT
# per iteration, so that is what this gates on.
#
# On the BEAM the count is measurable exactly, and this is how: run the loop in
# a process whose heap is large enough that NO GARBAGE COLLECTION HAPPENS, and
# read :erlang.process_info(:total_heap_size) before and after. With no GC in
# between, the heap grows by exactly the words the loop allocated. Refc
# binaries live off that heap, so :erlang.memory(:binary) is read beside it —
# the two together are the whole of what a read or a write allocates.
#
# THE FLOOR IS NOT ZERO AND IS NOT CLAIMED TO BE. Elixir has no caller-owned
# buffer and no mutable struct: a decoded value IS an allocation, and a
# sub-binary over the caller's bytes is a small one. What the gate holds is
# that the figure does not MOVE — the budget is pinned per case in
# test/conformance/elixir/alloc-budget.txt and re-pinned deliberately, exactly
# as a wire golden is — so an allocation somebody adds shows up as a diff
# rather than as nothing at all.
#
# The negative control is SOAK_SABOTAGE=1: one extra allocation per iteration,
# which must take every case over its budget.
defmodule Audit do
  @budget "test/conformance/elixir/alloc-budget.txt"

  @iterations 200

  # THE TOLERANCE, and why it is not zero. The count is taken at a garbage
  # collection boundary, and where that boundary falls moves the figure by a
  # handful of words between runs — measured at plus or minus three over four
  # consecutive runs of the whole corpus. The negative control adds about a
  # hundred and forty, so sixty-four separates a real allocation from the
  # boundary's jitter and the gate stays a gate.
  #
  # THE TWO COLUMNS ARE NOT GATED ALIKE, and the reason is what each one is. A
  # heap WORD count is a property of the terms the code builds: the same source
  # on the same OTP allocates the same words on any 64-bit machine, so it is
  # gated tight. REDUCTIONS are the scheduler's own accounting and a different
  # OTP build can count them differently, so that column is gated at a quarter —
  # loose enough to cross a machine, tight enough that an accidental quadratic
  # cannot hide in it. A number pinned tighter than it can be reproduced is a
  # gate that reds for the machine rather than for the code.
  @tolerance 64

  def run(rows) do
    cases = cases(rows)
    measured = Enum.map(cases, fn c -> {c.name, measure(c)} end)

    if System.get_env("ELIXIR_ALLOC_PIN") == "1" do
      text = Enum.map_join(measured, fn {name, {words, r}} -> "#{name} #{words} #{r}\n" end)
      File.write!(@budget, header() <> text)
      IO.puts("alloc-audit: pinned #{length(measured)} budgets into #{@budget}")
    else
      compare(measured)
    end
  end

  defp header do
    """
    # THE ALLOCATION BUDGET of the Elixir table leg, pinned (make tables-elixir-alloc-pin).
    #
    # <case> <heap words per iteration> <reductions per iteration>
    #
    # One iteration is one wire load, one wire save, one text read and one text
    # write of that instance. The count comes from the BEAM's own cumulative
    # figure for the heap words a collection RECLAIMED: a loop bracketed by two
    # full collections allocated exactly that, plus whatever is still live at the
    # end. It is a COUNT per iteration and not a drift figure — a path that
    # allocates and frees the same words every iteration reads +0 drift for an
    # hour and is still allocating.
    #
    # The floor is not zero and is not claimed to be — Elixir has no caller-owned
    # buffer and no mutable struct, so a decoded value IS an allocation. What this
    # file holds is that the figure does not MOVE.
    #
    # THE TWO COLUMNS ARE GATED DIFFERENTLY, and the reason is what each one is.
    # A heap WORD count is a property of the terms the code builds: the same
    # source on the same OTP allocates the same words on any 64-bit machine, so
    # it is gated at 64 words — under the negative control's ~140, and well over
    # the collection boundary's own jitter, measured at +/- 3 over four
    # consecutive runs of the whole corpus. REDUCTIONS are the scheduler's own
    # accounting and a different OTP build counts them differently, so that
    # column is gated at a quarter: loose enough to cross a machine, tight enough
    # that an accidental quadratic cannot hide in it.
    #
    # What is NOT counted, named rather than implied: a refc binary's payload,
    # which lives off the process heap and has no cumulative counter of its own.
    # The soak (make tables-elixir-soak) is that half's instrument.
    """
  end

  defp compare(measured) do
    budget =
      @budget
      |> File.read!()
      |> String.split("\n")
      |> Enum.reject(fn l -> l == "" or String.starts_with?(l, "#") end)
      |> Map.new(fn l ->
        [name, words, reductions] = String.split(l, " ")
        {name, {String.to_integer(words), String.to_integer(reductions)}}
      end)

    over =
      Enum.filter(measured, fn {name, {words, reductions}} ->
        case Map.fetch(budget, name) do
          {:ok, {w, r}} -> words > w + @tolerance or reductions > r + div(r, 4) + @tolerance
          :error -> true
        end
      end)

    Enum.each(measured, fn {name, {words, reductions}} ->
      IO.puts("  #{String.pad_trailing(name, 24)} #{words} words  #{reductions} reductions")
    end)

    if over == [] do
      IO.puts("alloc-audit: every case is inside its pinned budget (#{length(measured)} cases)")
    else
      Enum.each(over, fn {name, {words, bin}} ->
        want = Map.get(budget, name, {0, 0})

        IO.puts(
          :stderr,
          "#{name}: #{words} heap words and #{bin} reductions per iteration, over the pinned " <>
            "#{inspect(want)} — the generated code allocates more than it did; re-pin " <>
            "deliberately (make tables-elixir-alloc-pin) or find what was added"
        )
      end)

      System.halt(1)
    end
  end

  # the soak: the whole corpus, read and written in a loop, with the bytes
  # compared every iteration so a run that drifted STOPS rather than merely
  # getting slower — and with the process's live heap and the system's binary
  # memory printed each interval, which is the LEAK half of the instrument
  def soak(rows, seconds) do
    cases = cases(rows)
    if cases == [], do: raise("the manifest names no instance to soak")
    deadline = System.monotonic_time(:millisecond) + seconds * 1000
    IO.puts("soak: #{length(cases)} instances, #{seconds}s")
    # one warm pass, so nothing lazily built is counted as drift
    Enum.each(cases, &once/1)
    :erlang.garbage_collect()
    base = {live(), :erlang.memory(:binary)}
    loop(cases, deadline, base, 0, System.monotonic_time(:millisecond))
  end

  defp loop(cases, deadline, base, iterations, next_print) do
    now = System.monotonic_time(:millisecond)

    cond do
      now >= deadline ->
        report(base, iterations)
        verdict(base, iterations)

      now >= next_print ->
        report(base, iterations)
        loop(cases, deadline, base, iterations, now + 10_000)

      true ->
        Enum.each(cases, &once/1)
        loop(cases, deadline, base, iterations + 1, next_print)
    end
  end

  # THE VERDICT, and it is a CHECK rather than a sentence. The live heap must
  # not have moved at all; the system's binary memory is allowed the slack a
  # LARGE REFC BINARY IN FLIGHT takes, which is what a sample caught mid-corpus
  # sees — the corpus carries a 70 KB payload — so the reading is the MINIMUM of
  # three, which no in-flight allocation survives.
  @binary_slack 65_536

  defp verdict({base_live, base_bin}, iterations) do
    :erlang.garbage_collect()
    live_drift = live() - base_live

    bin_drift =
      Enum.min(
        Enum.map(1..3, fn _ ->
          Process.sleep(100)
          :erlang.garbage_collect()
          :erlang.memory(:binary) - base_bin
        end)
      )

    cond do
      live_drift > 0 ->
        IO.puts(:stderr, "SOAK FAILED: the live heap grew #{live_drift} words over #{iterations} iterations")
        System.halt(1)

      bin_drift > @binary_slack ->
        IO.puts(:stderr, "SOAK FAILED: binary memory grew #{bin_drift} bytes over #{iterations} iterations")
        System.halt(1)

      true ->
        IO.puts(
          "soak: #{iterations} iterations, heap flat (#{live_drift} words) and binary memory " <>
            "flat (#{bin_drift} bytes against the warm baseline)"
        )
    end
  end

  defp report({base_live, base_bin}, iterations) do
    :erlang.garbage_collect()

    IO.puts(
      "  #{iterations} iterations: live heap #{live() - base_live} words, " <>
        "binary #{:erlang.memory(:binary) - base_bin} bytes (both against the warm baseline)"
    )
  end

  defp live do
    {:total_heap_size, words} = :erlang.process_info(self(), :total_heap_size)
    words
  end

  # ONE case, measured.
  #
  # THE INSTRUMENT, and its limits. The BEAM keeps a cumulative count of the
  # heap words a garbage collection RECLAIMED, so a loop bracketed by two full
  # collections allocated exactly the words reclaimed between them plus whatever
  # is still live at the end. That is a COUNT per iteration, not a drift figure,
  # and it is deterministic: the same corpus measures the same number every run,
  # which is what lets it be pinned like a golden.
  #
  # WHAT IT DOES NOT COUNT, named rather than implied: the payload of a REFC
  # binary, which lives off the process heap and has no cumulative counter of
  # its own. The soak beside this is that half's instrument — refc bytes that
  # are allocated and freed every iteration read flat there, and ones that
  # accumulate do not.
  defp measure(c) do
    once(c)
    :erlang.garbage_collect()
    {_, reclaimed0, _} = :erlang.statistics(:garbage_collection)
    live0 = live()
    {:reductions, r0} = :erlang.process_info(self(), :reductions)
    spin(c, @iterations)
    :erlang.garbage_collect()
    {_, reclaimed1, _} = :erlang.statistics(:garbage_collection)
    {:reductions, r1} = :erlang.process_info(self(), :reductions)
    live1 = live()
    words = div(reclaimed1 - reclaimed0 + (live1 - live0), @iterations)
    {words, div(r1 - r0, @iterations)}
  end

  defp spin(_c, 0), do: :ok
  defp spin(c, n), do: (once(c); spin(c, n - 1))

  # one iteration: one wire load, one wire save, one text read, one text write —
  # every path the generated code has, with the bytes compared so a run that
  # drifted stops rather than getting slower
  defp once(c) do
    {value, _r} = apply(c.mod, c.load, [c.wire])
    if apply(c.mod, c.save, [value]) != c.wire, do: raise("#{c.name}: the wire round trip drifted")
    {from_text, _r2} = apply(c.mod, c.from_json, [c.text])
    if apply(c.mod, c.save, [from_text]) != c.wire, do: raise("#{c.name}: the text read drifted")
    if apply(c.mod, c.to_json, [value]) != c.text, do: raise("#{c.name}: the text write drifted")

    if System.get_env("SOAK_SABOTAGE") == "1" do
      # THE NEGATIVE CONTROL: one extra allocation per iteration, which must
      # take every case over its pinned budget
      _ = :binary.copy(<<0>>, 64)
      _ = List.duplicate(0, 64)
    end

    :ok
  end

  def cases(rows) do
    for [name, unit, root, wire] <- Map.get(rows, "instance", []) do
      lo = Macro.underscore(root)
      mod = Driver.codec_module(unit, root)

      %{
        name: name,
        mod: mod,
        load: String.to_atom("load_" <> lo),
        save: String.to_atom("save_" <> lo),
        from_json: String.to_atom("from_json_" <> lo),
        to_json: String.to_atom("to_json_" <> lo),
        wire: File.read!(wire),
        text: File.read!("testdata/conformance/tables/json/#{name}.json")
      }
    end
  end
end

# ---------------------------------------------------------------------------
# THE FUZZER'S ORACLE over the two READERS (docs/SPEC-TABLES.md §7.4, §19.2)
# ---------------------------------------------------------------------------
#
# The oracle is one sentence: for ANY bytes, Open either REFUSES or opens, and
# an opened image is one every accessor can walk without leaving the buffer.
# An index out of bounds is a REFUSAL, never an exception that escapes — which
# on the BEAM is a claim with teeth, because a bad binary match raises and a
# raise that reaches the caller is exactly the failure this exists to find.
#
# The mutants are the corpus's own fixtures with bytes flipped, words
# overwritten and the file truncated — the shapes the forgery battery pins one
# at a time, generated in bulk. A find here lands in the battery as a row.
defmodule Fuzz do
  import Bitwise

  def run(rows, iterations) do
    subjects = subjects(rows)
    if subjects == [], do: raise("the manifest names no block or cook to fuzz")
    :rand.seed(:exsss, {1, 2, 3})
    total = Enum.reduce(1..iterations, 0, fn i, opened -> opened + one(subjects, i) end)

    IO.puts(
      "elixir fuzz: #{iterations} mutants over #{length(subjects)} fixtures — " <>
        "#{total} opened and walked inside the buffer, the rest refused — no read left it"
    )
  end

  defp subjects(rows) do
    blocks =
      for [name, unit, image] <- Map.get(rows, "block", []) do
        %{kind: :block, unit: unit, root: Driver.block_table_of(name), bytes: File.read!(image)}
      end

    cooks =
      for [_case, unit, root, file] <- Map.get(rows, "cook", []) do
        %{kind: :cook, unit: unit, root: root, bytes: File.read!(file)}
      end

    blocks ++ Enum.uniq_by(cooks, & &1.root)
  end

  # THE ORACLE'S TWO OUTCOMES, and the line between them is the whole point.
  #
  # A DELIBERATE refusal is fine and is most of what a mutant gets: Open refuses
  # by its check list, or — because Open is O(1) and validates no graph (§7.4) —
  # the walk meets a delta or a count the region cannot hold and refuses THAT.
  # Both are the reader saying no.
  #
  # AN ESCAPING EXCEPTION IS THE FAILURE. On the BEAM a read past the end of a
  # binary raises MatchError, ArgumentError or FunctionClauseError, and one of
  # those reaching a caller is precisely "an index out of bounds became an
  # exception instead of a refusal". The two are told apart by CLASS: a
  # deliberate refusal is a RuntimeError this leg raised with a sentence in it,
  # and everything else is the runtime saying the reader left the buffer.
  defp one(subjects, i) do
    subject = Enum.at(subjects, rem(i, length(subjects)))
    bytes = mutate(subject.bytes)

    try do
      case open(subject, bytes) do
        :refuse ->
          0

        {:ok, handle} ->
          # OPENED: every accessor must now stay inside the buffer, and the
          # descriptor walk is every accessor.
          walk(subject, handle)
          1
      end
    rescue
      _ in RuntimeError ->
        # the walk's own refusal, with a sentence in it
        0

      e ->
        reraise "the #{subject.kind} #{subject.root} reader left the buffer on mutant #{i} — " <>
                  "an index out of bounds must be a refusal, never an escaping " <>
                  Atom.to_string(e.__struct__) <> ": " <> Exception.message(e),
                __STACKTRACE__
    end
  end

  defp open(%{kind: :block} = s, bytes) do
    mod = Driver.block_module_of(s.unit, s.root)
    apply(mod, String.to_atom("block_open_" <> Macro.underscore(s.root)), [bytes])
  end

  defp open(%{kind: :cook} = s, bytes) do
    {mod, fun} = Driver.accessor_of(s.unit, s.root, "cook_open", 1)
    apply(mod, fun, [bytes])
  end

  defp walk(%{kind: :block} = s, block) do
    mod = Driver.block_module_of(s.unit, s.root)
    info = apply(mod, String.to_atom("block_info_" <> Macro.underscore(s.root)), [])
    BlockDump.dump(block.base, info)
  end

  defp walk(%{kind: :cook}, cook), do: CookDump.dump(cook)

  # the shapes the forgery battery pins one at a time, generated in bulk: a
  # flipped byte, an overwritten word, and a truncation
  defp mutate(bytes) do
    size = byte_size(bytes)

    case :rand.uniform(3) do
      1 ->
        at = :rand.uniform(size) - 1
        <<head::binary-size(^at), b, tail::binary>> = bytes
        <<head::binary, bxor(b, 1 <<< (:rand.uniform(8) - 1)), tail::binary>>

      2 ->
        at = min(:rand.uniform(size) - 1, size - 8)
        <<head::binary-size(^at), _::binary-size(8), tail::binary>> = bytes
        <<head::binary, :rand.uniform(0xFFFFFFFFFFFFFFFF)::little-unsigned-64, tail::binary>>

      3 ->
        binary_part(bytes, 0, :rand.uniform(size))
    end
  end
end
