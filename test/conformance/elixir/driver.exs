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

defmodule Driver do
  @surfaces ~w(wire report json-read json-write json-hostile)

  def main([manifest, "list"]) do
    _ = manifest
    Enum.each(@surfaces, &IO.puts/1)
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
          Path.wildcard("build/elixir-tables-ebin/*.beam")
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

  defp run("block-dump", rows, outdir) do
    for [name, unit, image] <- rows(rows, "block") do
      root = block_table(name)
      {mod, open} = accessor(unit, root, "block_open", 1)

      case apply(mod, open, [File.read!(image)]) do
        {:ok, block} ->
          {dumper, dump} = accessor(unit, root, "block_dump", 1)
          write(outdir, name, apply(dumper, dump, [block]))

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
          {dumper, dump} = accessor(unit, root, "cook_dump", 1)
          write(outdir, name, apply(dumper, dump, [cook]))

        :refuse ->
          write(outdir, name, "refuse\n")
      end
    end
  end

  defp run("cook-forgery", rows, outdir) do
    for [name, kind, subject, file, extent, pointer] <- rows(rows, "forgery"), kind == "cook" do
      unit = cook_unit(rows, subject)
      {mod, open} = accessor(unit, subject, "cook_open", 1)

      verdict =
        case claim(file, extent, pointer) do
          :no_buffer ->
            "refuse\n"

          bytes ->
            case apply(mod, open, [bytes]) do
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

  defp block_verdict(unit, root, bytes) do
    {mod, open} = accessor(unit, root, "block_open", 1)

    case apply(mod, open, [bytes]) do
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

Driver.main(System.argv())
