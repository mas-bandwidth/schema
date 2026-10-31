# The ELIXIR leg of the tables conformance harness (test/conformance/README.md).
#
# One process per surface. The manifest is the DERIVED one the harness writes —
# the committed rows with the materialized fixture paths folded in and every
# expected answer removed — so this driver cannot pass by reading the answer.
#
# Dispatch is by SEARCH, not by a generated index: a unit's namespace is the
# camel case of its manifest key, and the module holding a root's reader is
# whichever module under it exports block_open_<root> or cook_open_<root>. That
# keeps the leg free of a generated name nothing else in the language needs.
#
# The backend emits the VARIABLE FORM for the whole variable class (the
# id-table wire's codec, descriptors and text form of docs/SPEC-TABLES.md §3,
# §16) beside the two ACCELERATORS it emits — the block (§19) and the cook
# (§7), both read halves. The variable class's codec answers the driver's wire,
# json-read, json-write and report surfaces; the block and cook answer the rest.

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
            # count holds the value-initialized element.
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
  # the accelerators' surfaces beside the variable form's: wire, json-read,
  # json-write, json-hostile and report ride the <Unit>.Table codec, the rest
  # the block and cook readers
  @surfaces ~w(wire message retain retain-save json-read json-write json-hostile report framing cook cook-write cook-foreign block block-foreign block-dump forgery cook-forgery cook-reason block-reason)

  def main([manifest, "list"]) do
    _ = manifest
    Enum.each(@surfaces, &IO.puts/1)
  end

  def main([manifest, "block-lead"]) do
    BlockLead.run(parse(File.read!(manifest)))
  end

  def main([manifest, "fuzz", iterations]) do
    Fuzz.run(parse(File.read!(manifest)), String.to_integer(iterations))
  end

  def main(["wire-fuzz"]) do
    WireFuzz.run()
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

  # the module exporting a root's reader: whichever module under the unit's
  # namespace has the function. Loading is explicit because an .exs script
  # runs with no application to load the beams for it.
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
          :error -> "refuse\n"
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

        :error ->
          write(outdir, name, "refuse\n")
      end
    end
  end

  defp run("forgery", rows, outdir) do
    for [name, kind, subject, file, extent, pointer] <- rows(rows, "forgery"), kind == "block" do
      verdict =
        case claim(file, extent, pointer) do
          :no_buffer ->
            "refuse\n"

          bytes ->
            # the POINTER column is the lead of the buffer the caller holds, and
            # block_open takes it for the same reason cook_open does (§19.2's
            # base alignment, and BlockRuntime's note). The block battery's rows
            # all carry 0 today; passing it through is what makes a row that
            # does not an answer rather than a silence.
            block_verdict(block_unit(subject), block_table(subject), bytes,
                          String.to_integer(pointer))
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

        :error ->
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
              :error -> "refuse\n"
            end
        end

      write(outdir, name, verdict)
    end
  end

  # THE REASON BESIDE THE REFUSAL (test/conformance/README.md, docs/SPEC-TABLES.md
  # §7, §19.2): the same forged file the two forgery surfaces open, answered
  # with Open's FIRST FAILING CLAUSE as the reference leg's TableRefuseReason
  # value name (internal/codegen/gotable/refusal.go), or `ok`. The reason is the
  # generated reader's own (cook_open_reason_<root>, block_open_reason_<root>),
  # never one this driver derives. A `null` pointer is no buffer at all, which
  # the reader takes as nil and names an unaligned base, as the reference does.
  defp run("cook-reason", rows, outdir) do
    for [name, kind, subject, file, extent, pointer] <- rows(rows, "forgery"), kind == "cook" do
      {mod, reason} = accessor(cook_unit(rows, subject), subject, "cook_open_reason", 2)
      write(outdir, name, reason_line(mod, reason, file, extent, pointer))
    end
  end

  defp run("block-reason", rows, outdir) do
    for [name, kind, subject, file, extent, pointer] <- rows(rows, "forgery"), kind == "block" do
      {mod, reason} =
        accessor(block_unit(subject), block_table(subject), "block_open_reason", 2)

      write(outdir, name, reason_line(mod, reason, file, extent, pointer))
    end
  end

  # ---- the VARIABLE FORM's surfaces (docs/SPEC-TABLES.md §3, §16) ----

  # the variable-form codec a (unit, root) names: <Unit>.Table, which exports
  # load/2, save/2, to_json/2 and from_json/2. nil when the leg has no codec.
  # a manifest unit key names its module by the unit's package, which is the
  # key itself except where the schema's package line says otherwise
  @unit_packages %{
    "scalars" => "scalardemo",
    "tblscalars2" => "scalardemo2",
    "widedemo" => "wide"
  }

  # the namespace a manifest unit key names: the key itself except where the
  # schema's package line says otherwise.
  def unit_package(unit), do: Map.get(@unit_packages, unit, unit)

  defp table_module(unit) do
    mod = Module.concat([Macro.camelize(unit_package(unit)), "Table"])

    if Code.ensure_loaded?(mod) and function_exported?(mod, :load, 2) and
         function_exported?(mod, :save, 2),
       do: mod,
       else: nil
  end

  defp run("wire", rows, outdir) do
    for [name, unit, root, wire | _] <- rows(rows, "instance") do
      answer_load_save(table_module(unit), root, wire, outdir, name)
    end
  end

  # ---- retain and retain-save (docs/SPEC-TABLES.md §6.6): RT1's Node loaded
  # with retention at the row's two capacities, measured and saved back. `retain`
  # answers `<retained>,<retain_lost>,<unknown> <save lost>`, `retain-save` the
  # saved bytes. `short` is ONE BYTE SHORT OF THE LAST RECORD: a roomy load says
  # what this port's own records cost, and the second load gets one byte less
  # (test/conformance/README.md). The id list's capacity is a count.
  defp run(surface, rows, outdir) when surface in ["retain", "retain-save"] do
    saves = surface == "retain-save"

    for row <- rows(rows, "retain"), not (saves and List.last(row) == "-") do
      retain_row(row, saves, outdir)
    end
  end

  defp retain_row([name, unit, root, wire, capacity, ids | _], saves, outdir) do
    mod = table_module(unit)
    data = File.read!(wire)
    roomy = 1_048_576
    id_cap = if ids == "full", do: div(roomy, 8), else: String.to_integer(ids)
    fresh = fn cap -> %{records: [], used: 0, cap: cap, ids: [], id_cap: id_cap} end
    empty = %{unknown: 0, kind_mismatch: 0, widened: 0, clamped: 0, malformed: false, retained: 0, retain_lost: 0}

    cap =
      if capacity == "short" do
        {:ok, _v, _r, probe} = mod.load_retain(root, data, fresh.(roomy), empty)
        max(probe.used - 1, 0)
      else
        roomy
      end

    {:ok, value, report, retain} = mod.load_retain(root, data, fresh.(cap), empty)
    size = mod.measure_retain(root, value, retain)
    {:ok, out, save, retain} = mod.save_retain(root, value, retain, empty)

    if byte_size(out) != size, do: raise("#{name}: retaining measure and save disagree")
    {:ok, again, second, _} = mod.save_retain(root, value, retain, empty)
    if again != out or second.retain_lost != save.retain_lost, do: raise("#{name}: retaining save is not repeatable")

    if saves do
      write(outdir, name, out)
    else
      write(outdir, name, "#{report.retained},#{report.retain_lost},#{report.unknown} #{save.retain_lost}\n")
    end
  end

  defp run("json-read", rows, outdir) do
    for [name, unit, root, _wire | rest] <- rows(rows, "instance"), rest == [] do
      json = Path.join("testdata/conformance/tables/json", name <> ".json")
      mod = table_module(unit)

      case mod do
        nil ->
          write_absent(outdir, name)

        _ ->
          case mod.from_json(root, File.read!(json)) do
            {:ok, value, _report} ->
              case mod.save(root, value) do
                {:ok, bytes} -> write(outdir, name, bytes)
                _ -> write_absent(outdir, name)
              end

            _ ->
              write_absent(outdir, name)
          end
      end
    end
  end

  defp run("json-write", rows, outdir) do
    for [name, unit, root, wire | rest] <- rows(rows, "instance"), rest == [] do
      mod = table_module(unit)

      case mod do
        nil ->
          write_absent(outdir, name <> ".json")

        _ ->
          case mod.load(root, File.read!(wire)) do
            {:ok, value, _report} ->
              case mod.to_json(root, value) do
                {:ok, text} -> write(outdir, name <> ".json", text)
                _ -> write_absent(outdir, name <> ".json")
              end

            _ ->
              write_absent(outdir, name <> ".json")
          end
      end
    end
  end

  defp run("cook-write", rows, outdir) do
    instances =
      Map.new(rows(rows, "instance"), fn [name | rest] -> {name, rest} end)

    for [name | _] <- rows(rows, "cook-write") do
      case Map.get(instances, name) do
        [unit, root, wire | _] ->
          cook_one(table_module(unit), root, wire, outdir, name)

        _ ->
          write_absent(outdir, name)
          write_absent(outdir, name <> "-be")
      end
    end
  end

  # cook-write (docs/SPEC-TABLES.md §7.6): load the instance, measure, refuse a
  # short buffer, then write both byte orders. A root the cook writer does not
  # lay out (cook_carried?/1: one that reaches a map, which `schema cook`
  # refuses by name, §7.2) writes <case>.absent rather than a wrong byte.
  defp cook_one(nil, _root, _wire, outdir, name) do
    write_absent(outdir, name)
    write_absent(outdir, name <> "-be")
  end

  defp cook_one(mod, root, wire, outdir, name) do
    if not function_exported?(mod, :cook_from, 4) or not mod.cook_carried?(root) do
      write_absent(outdir, name)
      write_absent(outdir, name <> "-be")
    else
      case mod.load(root, File.read!(wire)) do
        {:ok, value, report} ->
          if Map.get(report, :malformed, false) do
            write_absent(outdir, name)
            write_absent(outdir, name <> "-be")
          else
            case mod.cook_measure_reason(root, value) do
              {size, :ok} when is_integer(size) and size > 0 ->
                for {order, suffix} <- [{:little, ""}, {:big, "-be"}] do
                  stamp = :binary.copy(<<0xA5>>, size)
                  short = binary_part(stamp, 0, size - 1)

                  if mod.cook_from(root, value, short, order) != false do
                    raise "short cook buffer wrote #{name}#{suffix}"
                  end

                  case mod.cook_from(root, value, stamp, order) do
                    {true, out} -> write(outdir, name <> suffix, binary_part(out, 0, size))
                    _ -> raise "cook write refused #{name}#{suffix}"
                  end
                end

              _ ->
                write_absent(outdir, name)
                write_absent(outdir, name <> "-be")
            end
          end

        _ ->
          write_absent(outdir, name)
          write_absent(outdir, name <> "-be")
      end
    end
  end

  defp run("report", rows, outdir) do
    for [name, unit, root, wire] <- rows(rows, "report") do
      case report_line(table_module(unit), root, wire) do
        :absent -> write_absent(outdir, name)
        line -> write(outdir, name, line)
      end
    end
  end

  # THE FRAMING VECTORS (docs/LAYERS/1-framing.md): the read report as the
  # `report` surface prints it, then the §16 text of the value the read left,
  # unless the row says `no-value` or the read was refused. A derived line is
  # `framing <case> <unit> <root> <wire> [no-value]`. A damaged read owes the
  # refusal alone (§4, DAMAGE DISCARDS THE WHOLE LOAD), so its value is not
  # written.
  defp run("framing", rows, outdir) do
    for [name, unit, root, wire | rest] <- rows(rows, "framing") do
      case table_module(unit) do
        nil ->
          write_absent(outdir, name)

        mod ->
          case mod.load(root, File.read!(wire)) do
            {:ok, value, report} ->
              line = report_counters(report) <> ",read\n"

              cond do
                Map.get(report, :malformed, false) or rest == ["no-value"] ->
                  write(outdir, name, line)

                true ->
                  case mod.to_json(root, value) do
                    {:ok, text} -> write(outdir, name, line <> IO.iodata_to_binary(text))
                    _ -> raise "#{name}: to_json refused the value"
                  end
              end

            {:error, _refusal, %{reason: "unknown_form"}} ->
              write(outdir, name, "0,0,0,0,0,false,refused\n")

            {:error, _refusal, %{reason: "wire_damaged"} = report} ->
              write(outdir, name, report_counters(report) <> ",read\n")

            _ ->
              write_absent(outdir, name)
          end
      end
    end
  end

  defp run("json-hostile", rows, outdir) do
    for [name, unit, root, tree | _] <- rows(rows, "json-hostile") do
      json = Path.join(tree, root <> ".json")
      mod = table_module(unit)

      if mod == nil do
        write_absent(outdir, name)
      else
        case mod.from_json(root, File.read!(json)) do
          {:ok, value, report} ->
            if Map.get(report, :malformed, false) do
              write(outdir, name, "refused\n")
            else
              # a value the writer refuses (a float32 past its range, §3.3)
              # answers refused
              case mod.save(root, value) do
                {:ok, wire} ->
                  {:ok, _back, _r2} = mod.load(root, wire)
                  write(outdir, name, report_counters(report) <> ",read\n")

                {:error, _} ->
                  write(outdir, name, "refused\n")
              end
            end

          _ ->
            write(outdir, name, "refused\n")
        end
      end
    end
  end

  defp answer_load_save(mod, root, wire, outdir, name) do
    case mod do
      nil ->
        write_absent(outdir, name)

      _ ->
        case mod.load(root, File.read!(wire)) do
          {:ok, value, _report} ->
            case mod.save(root, value) do
              {:ok, bytes} ->
                write(outdir, name, bytes)

              # THE NAMED WRITER REFUSAL (docs/SPEC-TABLES.md §2.5): a blob the
              # tolerant read found named from two slots refuses the save by
              # name, and the case answers that name beside its absence
              {:error, :blob_shared} ->
                File.write!(Path.join(outdir, name <> ".absent"), "blob_shared")

              _ ->
                write_absent(outdir, name)
            end

          _ ->
            write_absent(outdir, name)
        end
    end
  end

  # A unit with no variable-form codec, and a root the codec names uncarried,
  # write <case>.absent (docs/SPEC-TABLES.md §11). A line that says "absent"
  # is a wrong answer against the reference's "read"; the sidecar is the
  # absence the harness already counts as a missing feature.
  defp report_line(nil, _root, _wire), do: :absent

  # The verdict is the REPORT'S REASON (docs/SPEC-TABLES.md §4, §4.2): a load
  # that finished is a read, its counters as they stand (wire_damaged there
  # is damage below the root, carried by the malformed flag); a refusal's
  # unknown_form is a refused form, and its wire_damaged framing damage, which
  # reports.txt spells as a read with malformed set.
  defp report_line(mod, root, wire) do
    case mod.load(root, File.read!(wire)) do
      {:ok, _value, report} ->
        report_counters(report) <> ",read\n"

      {:error, _refusal, %{reason: "unknown_form"}} ->
        "0,0,0,0,0,false,refused\n"

      {:error, _refusal, %{reason: "wire_damaged"} = report} ->
        # a damaged load returns no value, and its report stands
        report_counters(report) <> ",read\n"

      _ ->
        :absent
    end
  end

  defp reason_line(mod, reason, file, extent, pointer) do
    answer =
      case claim(file, extent, pointer) do
        :no_buffer -> apply(mod, reason, [nil, 0])
        bytes -> apply(mod, reason, [bytes, String.to_integer(pointer)])
      end

    Atom.to_string(answer) <> "\n"
  end

  defp report_counters(report) do
    [
      Map.get(report, :unknown, 0),
      Map.get(report, :kind_mismatch, 0),
      Map.get(report, :widened, 0),
      Map.get(report, :clamped, 0),
      Map.get(report, :duplicate, 0),
      if(Map.get(report, :malformed, false), do: "true", else: "false")
    ]
    |> Enum.join(",")
  end

  defp write_absent(outdir, name) do
    File.write!(Path.join(outdir, name <> ".absent"), "")
  end

  # ---- the MESSAGE FORM's surface (docs/SPEC-TABLES.md §3.3) ----

  # THE ONE SURFACE THAT READS TWO FILES (test/conformance/README.md): each
  # `message` row is joined BY KEY to the `connection` line naming its unit and
  # announcement; the announcement read gives the vocabulary, the batch loads
  # against it, and the saved bytes are the answer. A row the leg cannot answer
  # writes <case>.absent and says why on stderr.
  defp run("message", rows, outdir) do
    connections =
      Map.new(rows(rows, "connection"), fn [key, unit, _hash, announcement | _] ->
        {key, {unit, announcement}}
      end)

    for [name, connection, root, _file, batch | _] <- rows(rows, "message") do
      case message_answer(Map.get(connections, connection), root, batch) do
        {:refused, why} ->
          File.write!(Path.join(outdir, name <> ".absent"), why)

        {:ok, bytes} ->
          write(outdir, name, bytes)

        {:absent, why} ->
          IO.puts(:stderr, "message #{name}: absent: #{why}")
          write_absent(outdir, name)
      end
    end
  end

  defp run(surface, _rows, _outdir) do
    IO.puts(:stderr, "the Elixir leg does not implement #{surface}")
    System.halt(2)
  end

  # the unit's message module, <Package>.TableMessage, resolved the way
  # table_module resolves <Package>.Table. nil when the leg has none.
  defp message_module(unit) do
    mod =
      Module.concat([Macro.camelize(Map.get(@unit_packages, unit, unit)), "TableMessage"])

    if Code.ensure_loaded?(mod) and function_exported?(mod, :announce_read, 1) and
         function_exported?(mod, :load_messages, 2) and function_exported?(mod, :save_messages, 2),
       do: mod,
       else: nil
  end

  # a named refusal row: the root is a variable table, and the message form is
  # fixed tables only (docs/SPEC-TABLES.md §3.3)
  defp message_answer(_connection, _root, "-"),
    do: {:refused, "variable table: the message form is fixed tables only (SPEC-TABLES §3.3)"}

  defp message_answer(nil, _root, _batch),
    do: {:absent, "the row names no connection the manifest defines"}

  defp message_answer({unit, announcement}, root, batch) do
    case message_module(unit) do
      nil ->
        {:absent, "unit #{unit} has no TableMessage module"}

      mod ->
        with {:announce_read, {:ok, vocab}} <-
               {:announce_read, mod.announce_read(File.read!(announcement))},
             {:load_messages, {:ok, values}} <-
               {:load_messages, mod.load_messages(vocab, File.read!(batch))},
             {:save_messages, {:ok, bytes}} <-
               {:save_messages, mod.save_messages(vocab, values)} do
          {:ok, bytes}
        else
          {verb, answer} -> {:absent, "#{root}: #{verb} answered #{inspect(answer)}"}
        end
    end
  end

  defp foreign(<<magic::binary-size(8), rest::binary>>) do
    magic |> :binary.bin_to_list() |> Enum.reverse() |> :binary.list_to_bin() |> Kernel.<>(rest)
  end

  def block_table_of(name), do: block_table(name)
  def block_rows(rows), do: rows(rows, "block")
  def block_verdict_at(unit, root, bytes, lead), do: block_verdict(unit, root, bytes, lead)
  def block_module_of(unit, root), do: block_module(unit, root)
  def accessor_of(unit, root, prefix, arity), do: accessor(unit, root, prefix, arity)

  defp block_module(unit, root) do
    {mod, _} = accessor(unit, root, "block_open", 1)
    mod
  end

  defp block_verdict(unit, root, bytes), do: block_verdict(unit, root, bytes, 0)

  defp block_verdict(unit, root, bytes, lead) do
    open = String.to_atom("block_open_" <> Macro.underscore(root))

    case apply(block_module(unit, root), open, [bytes, lead]) do
      {:ok, _block} -> "open\n"
      :error -> "refuse\n"
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

  defp write(outdir, name, data) when is_binary(data) do
    File.write!(Path.join(outdir, name), data)
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
    # THE SEED IS A KNOB, as it is on every other leg (SEED=$(SEED) in the
    # Makefile). A fixed seed with a fixed N is a regression test wearing a
    # fuzzer's clothes: CI would explore the same mutants forever. The default
    # is fixed so a failing run is reproducible from its own output.
    seed = String.to_integer(System.get_env("SEED", "20260903"))
    :rand.seed(:exsss, {seed, seed + 369, seed + 7})
    total = Enum.reduce(1..iterations, 0, fn i, opened -> opened + one(subjects, i) end)

    IO.puts(
      "elixir fuzz: #{iterations} mutants over #{length(subjects)} fixtures at seed #{seed} — " <>
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
        :error ->
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

# ---------------------------------------------------------------------------
# THE BASE-ALIGNMENT GATE (docs/SPEC-TABLES.md §19.1, §19.2)
# ---------------------------------------------------------------------------
#
# §19.1 lays a block at a 64-byte aligned base and §19.2 checks it. A BEAM
# binary has no address a caller can observe or place, so block_open takes the
# lead the caller states — and a stated fact nothing checks is a comment. This
# opens every committed block image at every lead in 0..64 and requires the
# alignment rule exactly: 0 and 64 open, 1..63 refuse.
#
# IT IS LEG-LOCAL, and the reason is worth naming rather than leaving to be
# found. The shared block forgery battery carries `pointer 0` on every row by
# its own statement (testdata/conformance/tables/manifest.txt) and is PINNED
# from the reference driver's own table, whose block rows print the column as a
# constant; the Rust driver refuses a block row with any other value by design,
# and the C# driver's block path reads the extent alone. So a `block_lead_*`
# row is a change to the reference leg and to three other drivers, not to this
# one. This leg's driver passes the column through already; until the battery
# carries a row, this gate holds the property.
defmodule BlockLead do
  def run(rows) do
    images = Driver.block_rows(rows)
    if images == [], do: raise("the manifest names no block image")

    for [name, unit, image] <- images do
      data = File.read!(image)
      root = Driver.block_table_of(name)

      for lead <- 0..64 do
        want = if rem(lead, 64) == 0, do: "open\n", else: "refuse\n"
        got = Driver.block_verdict_at(unit, root, data, lead)

        if got != want do
          IO.puts(
            :stderr,
            "BASE ALIGNMENT GATE FAILED: #{name} at lead #{lead} answered " <>
              String.trim(got) <> ", wanted " <> String.trim(want) <>
              " — §19.2 checks the base's alignment and lead is how this leg carries it"
          )

          System.halt(1)
        end
      end
    end

    IO.puts(
      "elixir base-alignment gate: #{length(images)} block image(s) x 65 leads — " <>
        "0 and 64 open, 1..63 refuse"
    )
  end
end

# ---------------------------------------------------------------------------
# THE WIRE-FUZZ DRIVER (test/conformance/README.md, "The wire-fuzz driver")
# ---------------------------------------------------------------------------
#
# One BEAM process for the whole run, on a pipe, that only reads. The harness
# owns the mutants and the oracle; this module binds a roster entry to its
# generated variable-form codec and answers each mutant's load measure, report
# and saved bytes — through the retaining pair when the roster names it
# (docs/SPEC-TABLES.md §6.6), with the two retention counters beside the six.
# It decides nothing about what a mutant means.
#
# The stream is little-endian throughout, exactly as the harness's
# test/conformance/harness/wirefuzz.go spells it: a u32 roster count, then per
# root a u16 unit key, u16 root name, a form byte and a retain byte; one byte
# per roster entry in reply (1 when the codec is present), then per mutant a u32
# index, u32 length and the bytes, answered by the header and the saved bytes.
defmodule WireFuzz do
  def run do
    # the wire stream is BYTES, and the standard_io device must not Unicode-
    # encode them (§3): latin1 passes every byte through untouched.
    :io.setopts(:standard_io, encoding: :latin1)
    roster = read_roster()
    stdout_write(for(r <- roster, do: <<avail(r)>>))
    write_unopened(roster)
    loop(roster)
  end

  # THE ROOTS THIS LEG CANNOT OPEN, BY NAME (make/elixir.mk,
  # tables-elixir-wire-fuzz-all). The harness counts the seeds a roster 0
  # leaves out and names none of them, so a root that stopped opening would
  # only move a count. When ELIXIR_WIRE_FUZZ_ABSENT names a file, the driver
  # writes one line per roster entry it answers 0 for: `<unit>.<root> form=<f>`
  # and why (no-codec: the unit has no generated module for the entry's form;
  # uncarried: the variable form refuses the root, docs/SPEC-TABLES.md §2.6,
  # §2.8, §11). The gate compares that list with its pinned one. Unset, nothing
  # is written.
  defp write_unopened(roster) do
    case System.get_env("ELIXIR_WIRE_FUZZ_ABSENT") do
      nil ->
        :ok

      "" ->
        :ok

      path ->
        lines =
          for r <- roster, avail(r) == 0 do
            reason = if codec(r) == nil, do: "no-codec", else: "uncarried"
            "#{r.unit}.#{r.root} form=#{r.form} #{reason}\n"
          end

        File.write!(path, lines)
    end
  end

  # THE CODEC A ROSTER ENTRY NAMES (docs/SPEC-TABLES.md §3, §3.3). The roster's
  # form byte says which wire the entry will be sent: form 1 is the FILE form,
  # whose codec is the unit's `<Package>.Table`; form 2 is the MESSAGE form,
  # whose codec is the unit's `<Package>.TableMessage` (announcement plus the
  # batch verbs). Reading the form here is what keeps an entry from being
  # answered by a codec that refuses its wire: the file codec answers
  # `:newer_form` for byte 2 (variableform.go's open/1), so a form-2 entry
  # bound to it was available and then diverged instead of being fuzzed.
  # The namespace is the unit's PACKAGE, not its manifest key
  # (Driver.table_module does the same mapping).
  defp codec(%{form: 2} = r), do: message_codec(r.unit, r.root)
  defp codec(r), do: file_codec(r.unit, r.root)

  # the FILE form's codec, docs/SPEC-TABLES.md §3: load_measure/2 and load/2.
  defp file_codec(unit, _root) do
    mod = Module.concat([Macro.camelize(Driver.unit_package(unit)), "Table"])

    if Code.ensure_loaded?(mod) and function_exported?(mod, :load_measure, 2) and
         function_exported?(mod, :load, 2),
       do: mod,
       else: nil
  end

  # the MESSAGE form's codec, docs/SPEC-TABLES.md §3.3: the unit's announced
  # vocabulary and the batch verbs load_messages/2, save_messages/2 and
  # measure_messages/2.
  defp message_codec(unit, _root) do
    mod = Module.concat([Macro.camelize(Driver.unit_package(unit)), "TableMessage"])

    if Code.ensure_loaded?(mod) and function_exported?(mod, :announce, 0) and
         function_exported?(mod, :announce_read, 1) and
         function_exported?(mod, :load_messages, 2) and
         function_exported?(mod, :save_messages, 2) and
         function_exported?(mod, :measure_messages, 2),
       do: mod,
       else: nil
  end

  # carried?/1 is the variable form's own answer (docs/SPEC-TABLES.md §2.6,
  # §2.8, §11): a root that reaches a map or a union is uncarried. Roster 0
  # means the harness never sends it a mutant. An error return while the root
  # is marked available would be scored against the oracle. A form-2 entry is
  # the fixed table's announced wire (§3.3) and is carried whenever its
  # TableMessage module is present.
  defp avail(%{form: 2} = r) do
    if message_codec(r.unit, r.root) == nil, do: 0, else: 1
  end

  defp avail(r) do
    case file_codec(r.unit, r.root) do
      nil ->
        0

      mod ->
        if function_exported?(mod, :carried?, 1) and not mod.carried?(r.root) do
          0
        else
          1
        end
    end
  end

  defp read_roster do
    count = read_u32()

    for _ <- 1..count do
      unit = read_string()
      root = read_string()
      form = read_u8()
      retain = read_u8()
      %{unit: unit, root: root, form: form, retain: retain}
    end
  end

  defp loop(roster) do
    case stdin_read(4) do
      <<index::little-32>> ->
        case stdin_read(4) do
          <<size::little-32>> ->
            wire = read_bytes(size)
            answer(roster, index, wire)
            loop(roster)

          _ ->
            :ok
        end

      _ ->
        :ok
    end
  end

  defp answer(roster, index, wire) do
    r = Enum.at(roster, index)

    if r.form == 2 do
      # THE MESSAGE FORM IS ITS OWN ARM (docs/SPEC-TABLES.md §3.3): the batch
      # verbs read and write it, and the file codec never sees a form-2 byte.
      answer_message(r, wire)
    else
      mod = file_codec(r.unit, r.root)

      if r.retain == 1 do
        answer_retain(mod, r, wire)
      else
        answer_plain(mod, r, wire)
      end
    end
  end

  # the MESSAGE arm (docs/SPEC-TABLES.md §3.3): resolve the unit's own
  # announcement once, read the batch, and write body one back as the batch of
  # one — the same single body the oracle encodes (test/conformance/harness/
  # wirefuzz.go, the `r.message` branch). A clean batch reports no counter and
  # a damaged one reports `malformed`, which is the one counter the batch verbs
  # themselves name; the message surface carries no per-kind counters, so those
  # stay zero here rather than being invented.
  defp answer_message(r, wire) do
    case message_codec(r.unit, r.root) do
      nil ->
        stdout_write(header(0, %{}, -1) <> <<-1::little-signed-64>>)

      mod ->
        case vocab(mod) do
          nil ->
            stdout_write(header(0, %{}, -1) <> <<-1::little-signed-64>>)

          vocab ->
            case mod.load_messages(vocab, wire) do
              {:ok, bodies} ->
                message_reply(mod, vocab, bodies, false)

              {:malformed, _count, bodies} ->
                message_reply(mod, vocab, bodies, true)

              {:error, :non_finite_float} ->
                stdout_write(header(0, %{refused: true}, -1) <> <<-1::little-signed-64>>)

              _ ->
                stdout_write(header(0, %{}, -1) <> <<-1::little-signed-64>>)
            end
        end
    end
  end

  # THE ANNOUNCEMENT IS A CONSTANT OF THE UNIT (§3.3), so it is resolved once
  # per unit and kept for the run: the vocabulary is a pure function of the
  # build version and the driver makes a fresh copy never.
  defp vocab(mod) do
    key = {__MODULE__, mod}

    case Process.get(key) do
      nil ->
        case mod.announce_read(mod.announce()) do
          {:ok, vocab} ->
            Process.put(key, vocab)
            vocab

          _ ->
            nil
        end

      vocab ->
        vocab
    end
  end

  defp message_reply(mod, vocab, bodies, malformed) do
    report = %{malformed: malformed}

    measure =
      case mod.measure_messages(vocab, bodies) do
        {:ok, n} -> n
        _ -> 0
      end

    case bodies do
      [first | _] ->
        case mod.save_messages(vocab, [first]) do
          {:ok, bytes} ->
            stdout_write(header(1, report, measure) <> <<byte_size(bytes)::little-signed-64>>)
            stdout_write(bytes)

          _ ->
            stdout_write(header(1, report, measure) <> <<-1::little-signed-64>>)
        end

      [] ->
        # damage before body one: the batch delivered no body to save
        stdout_write(header(1, report, measure) <> <<-1::little-signed-64>>)
    end
  end

  # the plain arm (docs/SPEC-TABLES.md §4.2): load, report, save.
  # the report a LoadMeasure refusal answers: the load's own refusal, which
  # names a newer form or says malformed
  defp measure_refusal(mod, root, wire) do
    case mod.load(root, wire) do
      {:error, :newer_form, report} -> Map.put(report, :refused, true)
      {:error, _reason, report} -> %{report | malformed: true}
      _ -> %{malformed: true}
    end
  end

  defp answer_plain(mod, r, wire) do
    measure = mod.load_measure(r.root, wire)

    if measure < 0 do
      # LoadMeasure refused the framing: no root and nothing saved, and the
      # refusal is DAMAGE, which discards the whole load (Glenn, 2026-10-31):
      # the report says malformed, or names the form a newer writer used
      stdout_write(header(0, measure_refusal(mod, r.root, wire), measure) <> <<-1::little-signed-64>>)
    else
      case mod.load(r.root, wire) do
        {:ok, value, report} ->
          case mod.save(r.root, value) do
            {:ok, bytes} ->
              stdout_write(header(1, report, measure) <> <<byte_size(bytes)::little-signed-64>>)
              stdout_write(bytes)

            _ ->
              stdout_write(header(1, report, measure) <> <<-1::little-signed-64>>)
          end

        {:error, :non_finite_float, _report} ->
          # A NaN OR AN INFINITY IS REFUSED BY EVERY READ (docs/SPEC-TABLES.md
          # §3.3), and the refusal is the answer the oracle compares
          stdout_write(header(0, %{refused: true}, measure) <> <<-1::little-signed-64>>)

        {:error, :newer_form, report} ->
          # A FORM THIS READER DOES NOT KNOW IS REFUSED BY NAME (docs/SPEC-TABLES.md
          # §3): nothing is decoded, the verdict is the answer, and the
          # defaults the load stands on are not compared
          stdout_write(header(1, Map.put(report, :refused, true), measure) <> <<-1::little-signed-64>>)

        {:error, _reason, report} ->
          # A LOAD THAT MEETS DAMAGE REFUSES AND SAYS SO (Glenn, 2026-10-31:
          # damage discards the whole load): its report, malformed among it,
          # is the answer the oracle compares
          stdout_write(header(0, report, measure) <> <<-1::little-signed-64>>)
      end
    end
  end

  # THE RETENTION ARM (docs/SPEC-TABLES.md §6.6): the same mutant read and
  # written back through the retaining pair, with the two caller-owned stores
  # the arm declares — one MiB of bytes and one id per eight bytes, the same
  # capacities the harness fixes (test/conformance/harness/wirefuzz.go). The
  # report accumulates across load and save, so the two retention counters the
  # header carries are the pair's whole answer.
  defp answer_retain(mod, r, wire) do
    measure = mod.load_measure(r.root, wire)
    retain = %{records: [], used: 0, cap: 1048576, ids: [], id_cap: 131072}
    report = %{unknown: 0, kind_mismatch: 0, widened: 0, clamped: 0, malformed: false, retained: 0, retain_lost: 0}

    if measure < 0 do
      # LoadMeasure refused the framing: no root, an empty report, nothing saved
      stdout_write(header(0, measure_refusal(mod, r.root, wire), measure) <> <<-1::little-signed-64>>)
    else
      case mod.load_retain(r.root, wire, retain, report) do
        {:ok, value, report, retain} ->
          case mod.save_retain(r.root, value, retain, report) do
            {:ok, bytes, report, _retain} ->
              stdout_write(header(1, report, measure) <> <<byte_size(bytes)::little-signed-64>>)
              stdout_write(bytes)

            _ ->
              stdout_write(header(1, report, measure) <> <<-1::little-signed-64>>)
          end

        {:error, :non_finite_float, _report, _retain} ->
          stdout_write(header(0, %{refused: true}, measure) <> <<-1::little-signed-64>>)

        {:error, _reason, report, _retain} ->
          # A LOAD THAT MEETS DAMAGE REFUSES AND SAYS SO (Glenn, 2026-10-31:
          # damage discards the whole load): its report, malformed among it,
          # is the answer the oracle compares
          stdout_write(header(0, report, -1) <> <<-1::little-signed-64>>)
      end
    end
  end

  # the reply header: loaded, the five counters, malformed, refused, the measure,
  # the two retention counters and the saved length. The two retention counters
  # ride the report when the entry retains, and are zero on every other entry
  # (docs/SPEC-TABLES.md §6.6).
  defp header(loaded, report, measure) do
    unknown = Map.get(report, :unknown, 0)
    kind_mismatch = Map.get(report, :kind_mismatch, 0)
    widened = Map.get(report, :widened, 0)
    clamped = Map.get(report, :clamped, 0)
    duplicate = Map.get(report, :duplicate, 0)
    malformed = if Map.get(report, :malformed, false), do: 1, else: 0
    refused = if Map.get(report, :refused, false), do: 1, else: 0
    retained = Map.get(report, :retained, 0)
    retain_lost = Map.get(report, :retain_lost, 0)

    <<loaded::8, unknown::little-signed-32, kind_mismatch::little-signed-32,
      widened::little-signed-32, clamped::little-signed-32, duplicate::little-signed-32,
      malformed::8, refused::8, measure::little-signed-64, retained::little-signed-32,
      retain_lost::little-signed-32>>
  end

  defp read_u32 do
    case stdin_read(4) do
      <<v::little-32>> -> v
      _ -> 0
    end
  end

  defp read_u8 do
    case stdin_read(1) do
      <<v::8>> -> v
      _ -> 0
    end
  end

  defp read_string do
    n = read_u16()
    read_bytes(n)
  end

  defp read_u16 do
    case stdin_read(2) do
      <<v::little-16>> -> v
      _ -> 0
    end
  end

  defp read_bytes(n) do
    case stdin_read(n) do
      b when is_binary(b) -> b
      _ -> <<>>
    end
  end

  # the raw stdin/stdout devices: the wire-fuzz driver runs WITHOUT -noinput,
  # so the standard_io device is live and reads/writes bytes directly.
  # a zero-length read is the empty binary: :file.read/2 of 0 bytes does not
  # answer one, so an empty mutant (a truncation to nothing) would stall the leg
  defp stdin_read(0), do: <<>>

  defp stdin_read(n) do
    case :file.read(:standard_io, n) do
      {:ok, data} -> data
      _ -> :eof
    end
  end

  defp stdout_write(data), do: :file.write(:standard_io, data)
end
