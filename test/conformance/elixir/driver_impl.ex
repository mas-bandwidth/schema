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
# THE FIVE WIRE-CARRYING SURFACES ARE ABSENT, and not as a gap the leg counts:
# Elixir emits no table wire at all. The port that once did wrote the form that
# preceded the id-table wire and was removed rather than carried; schema#515
# brings the id-table wire to Elixir, and these surfaces with it. What this leg
# drives is the two ACCELERATORS the backend does emit — the block (§19) and
# the cook (§7), both read halves.

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
  # the two accelerators' surfaces and nothing else: Elixir emits no table
  # wire, so wire, report and the three text surfaces are not on this list
  # (schema#515 brings the id-table wire to Elixir)
  @surfaces ~w(cook cook-foreign block block-foreign block-dump forgery cook-forgery)

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

  defp run(surface, _rows, _outdir) do
    IO.puts(:stderr, "the Elixir leg does not implement #{surface}")
    System.halt(2)
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
