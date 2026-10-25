defmodule ElixirViewMain do
  def hex16(n) do
    n
    |> Integer.to_string(16)
    |> String.downcase()
    |> String.pad_leading(16, "0")
  end

  def flatten(doc, true), do: doc

  def flatten(doc, false) do
    doc
    |> String.replace("\\", "\\\\")
    |> String.replace("\n", "\\n")
  end

  def annotation(doc, tags, unflattened) do
    "tags=[#{Enum.join(tags, ",")}] doc=#{flatten(doc, unflattened)}"
  end

  def vocabulary(what, v, row, unflattened) do
    IO.puts(
      "#{what} #{v.name} file=#{v.file} max=#{v.max} bits=#{v.storage_bits} variants=#{v.num_variants} #{annotation(v.doc, v.tags, unflattened)}"
    )

    for r <- v.variants do
      IO.puts(
        "#{what} #{v.name} #{row} #{r.value} #{r.name} id=#{hex16(r.id)} #{annotation(r.doc, r.tags, unflattened)}"
      )
    end
  end

  def find_holder(unit, union_name) do
    Enum.find_value(unit.tables ++ unit.types, fn entry ->
      Enum.find_value(entry.type.fields, fn f ->
        if f.arms != nil and f.arms.union_name == union_name, do: f.arms, else: nil
      end)
    end)
  end

  def union_vocabulary(unit, v, unflattened) do
    IO.puts(
      "union #{v.name} file=#{v.file} max=#{v.max} bits=#{v.storage_bits} variants=#{v.num_variants} #{annotation(v.doc, v.tags, unflattened)}"
    )

    holder = find_holder(unit, v.name)

    for r <- v.variants do
      payload = if r.payload_name == nil, do: "-", else: r.payload_name
      record = if r.payload != nil, do: "yes", else: "no"
      field = if r.field != nil, do: "yes", else: "no"

      {kind, bound, overlay} =
        if r.field != nil do
          ov =
            if holder != nil do
              Integer.to_string(r.field.offset - holder.payload_offset)
            else
              "-"
            end

          {Integer.to_string(r.field.kind), Integer.to_string(r.field.array_bound), ov}
        else
          {"-", "-", "-"}
        end

      IO.puts(
        "union #{v.name} arm #{r.value} #{r.name} id=#{hex16(r.id)} payload=#{payload} record=#{record} field=#{field} kind=#{kind} bound=#{bound} overlay=#{overlay} #{annotation(r.doc, r.tags, unflattened)}"
      )
    end
  end

  def declaration(what, entry, unflattened) do
    type = entry.type

    IO.puts(
      "#{what} #{entry.name} file=#{entry.file} fields=#{type.num_fields} #{annotation(entry.doc, entry.tags, unflattened)}"
    )

    for f <- type.fields do
      json = if f.json == nil, do: "-", else: f.json

      IO.puts(
        "#{what} #{entry.name} field #{f.name} json=#{json} type=#{f.type_name} id=#{hex16(f.id)} optional=#{f.optional} #{annotation(f.doc, f.tags, unflattened)}"
      )
    end
  end

  def main(args) do
    {pkg, rest} =
      case args do
        [p | r] -> {p, r}
        [] -> raise "usage: view_main.exs <package> [--unflattened]"
      end

    unflattened = "--unflattened" in rest
    mod = Module.concat([Macro.camelize(pkg), "UnitView"])
    unit = mod.unit_view()

    IO.puts("unit package=#{unit.package} protocol=#{hex16(unit.protocol_id)}")

    for c <- unit.constants do
      bits =
        if c.is_float do
          <<u::unsigned-big-integer-size(64)>> = <<c.float_value::float-big-size(64)>>
          u
        else
          0
        end

      IO.puts(
        "constant #{c.name} file=#{c.file} type=#{c.type_name} float=#{c.is_float} int=#{c.int_value} real=#{hex16(bits)} #{annotation(c.doc, c.tags, unflattened)}"
      )
    end

    for e <- unit.enums, do: vocabulary("enum", e, "variant", unflattened)
    for f <- unit.flags, do: vocabulary("flags", f, "bit", unflattened)
    for u <- unit.unions, do: union_vocabulary(unit, u, unflattened)
    for t <- unit.types, do: declaration("type", t, unflattened)
    for t <- unit.tables, do: declaration("table", t, unflattened)
  end
end

ElixirViewMain.main(System.argv())
