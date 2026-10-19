# driver.exs — the Elixir card conformance driver (tables/cards/conformance/
# FORMAT.md). It answers the driver contract test/conformance/README.md states —
# `<driver> <manifest> list` and `<driver> <manifest> <surface> <outdir>` — over
# the nine card surfaces (load, save, render, parse, bounds, upgrade, edit,
# taskhash, view) through the facade in tables/cards/elixir/cards.ex. Absence
# is not an answer: every surface is listed and run.
defmodule Cards.Driver do
  @surfaces ~w(load save render parse bounds upgrade edit taskhash view)

  def main([_manifest, "list"]) do
    Enum.each(@surfaces, &IO.puts/1)
  end

  def main([manifest, surface, outdir]) do
    if surface in @surfaces do
      run(surface, parse(File.read!(manifest)), outdir)
    else
      IO.puts(:stderr, "the Elixir card leg does not implement #{surface}")
      System.halt(2)
    end
  end

  def main(_), do: usage()

  defp usage do
    IO.puts(:stderr, "usage: driver <manifest> list | <manifest> <surface> <outdir>")
    System.halt(64)
  end

  # ---- the manifest (tables/cards/conformance/FORMAT.md) ----

  defp parse(text) do
    text
    |> String.split("\n")
    |> Enum.map(&String.trim/1)
    |> Enum.reject(fn line -> line == "" or String.starts_with?(line, "#") end)
    |> Enum.map(&String.split(&1, ~r/[ \t]+/))
  end

  defp run(surface, rows, outdir) do
    File.mkdir_p!(outdir)

    for [s, name, input | rest] <- rows, s == surface do
      one(surface, name, input, rest, outdir)
    end
  end

  defp one("load", name, input, _edit, outdir) do
    wire = File.read!(input)
    {:ok, record, report} = Cards.Cards.load(wire)
    {:ok, json, _} = Cards.Cards.to_json(record)
    write(outdir, name, "json", json)
    write(outdir, name, "report", format_report(report))
  end

  defp one("save", name, input, _edit, outdir) do
    text = File.read!(input)

    case Cards.Cards.from_json(text) do
      {:ok, record} -> write(outdir, name, "wire", Cards.Cards.wire(record))
      {:error, reason} -> refuse(outdir, name, refusal_class(reason))
    end
  end

  defp one("render", name, input, _edit, outdir) do
    wire = File.read!(input)
    {:ok, record, _} = Cards.Cards.load(wire)
    {:ok, txt, _} = Cards.Cards.render(record)
    write(outdir, name, "txt", negative(txt))
  end

  defp one("parse", name, input, _edit, outdir) do
    text = File.read!(input)

    case Cards.Cards.parse(text) do
      {:ok, record} -> write(outdir, name, "wire", Cards.Cards.wire(record))
      {:error, reason} -> refuse(outdir, name, refusal_class(reason))
    end
  end

  defp one("bounds", name, input, _edit, outdir) do
    text = File.read!(input)

    case Cards.Cards.from_json(text) do
      {:ok, record} -> write(outdir, name, "wire", Cards.Cards.wire(record))
      {:error, reason} -> refuse(outdir, name, refusal_class(reason))
    end
  end

  defp one("upgrade", name, input, _edit, outdir) do
    wire = File.read!(input)

    case Cards.Cards.upgrade(wire) do
      {:ok, out, report} ->
        write(outdir, name, "wire", out)
        write(outdir, name, "report", format_report(report))

      {:error, reason, _report} ->
        refuse(outdir, name, refusal_class(reason))

      {:error, reason} ->
        refuse(outdir, name, refusal_class(reason))
    end
  end

  defp one("edit", name, input, edit, outdir) do
    wire = File.read!(input)
    {:ok, record, _} = Cards.Cards.load(wire)

    case apply_edit(record, edit) do
      {:ok, edited} -> write(outdir, name, "wire", Cards.Cards.wire(edited))
      {:error, reason} -> refuse(outdir, name, refusal_class(reason))
    end
  end

  defp one("taskhash", name, input, _edit, outdir) do
    wire = File.read!(input)
    {:ok, record, _} = Cards.Cards.load(wire)
    {:ok, hash} = Cards.Cards.task_hash(record)
    write(outdir, name, "hex", Base.encode16(hash, case: :lower))
  end

  defp one("view", name, _input, _edit, outdir) do
    write(outdir, name, "txt", Cards.Cards.view())
  end

  # apply_edit applies one edit line through the Set* edit of the same name
  # (tables/cards/edit.go; tables/cards/cmd/cards-driver/main.go: applyEdit).
  defp apply_edit(record, [edit]) do
    case String.split(edit, "=", parts: 2) do
      ["needs", v] -> Cards.Cards.set_needs(record, split_list(v))
      ["paths", v] -> Cards.Cards.set_paths(record, split_list(v))
      ["tier", v] -> Cards.Cards.set_tier(record, v)
      ["priority", v] -> Cards.Cards.set_priority(record, v)
      ["column", v] -> Cards.Cards.set_column(record, v)
      ["holder", v] -> Cards.Cards.set_holder(record, v)
      _ -> {:error, "cards-driver: unknown edit"}
    end
  end

  defp apply_edit(_record, _), do: {:error, "cards-driver: an edit line carries exactly one edit"}

  defp split_list(""), do: []
  defp split_list(v), do: String.split(v, ",")

  # negative flips one rendered byte under the test-only switch, so the negative
  # control can require render to go red while the other surfaces pass.
  defp negative(txt) do
    if System.get_env("CARDS_ELIXIR_NEGATIVE") == "1" do
      <<b, rest::binary>> = txt
      <<Bitwise.bxor(b, 0x01), rest::binary>>
    else
      txt
    end
  end

  defp write(outdir, name, ext, data) do
    File.write!(Path.join(outdir, name <> "." <> ext), data)
  end

  defp refuse(outdir, name, class) do
    write(outdir, name, "refused", class <> "\n")
  end

  # format_report writes the read report, one Name=value line per counter of the
  # Go TableReport in declaration order (tables/cards/conformance/FORMAT.md).
  defp format_report(report) do
    [
      "Retained=#{Map.get(report, :retained, 0)}\n",
      "RetainLost=#{Map.get(report, :retain_lost, 0)}\n",
      "Widened=#{Map.get(report, :widened, 0)}\n",
      "Unknown=#{Map.get(report, :unknown, 0)}\n",
      "KindMismatch=#{Map.get(report, :kind_mismatch, 0)}\n",
      "Clamped=#{Map.get(report, :clamped, 0)}\n",
      "Duplicate=#{Map.get(report, :duplicate, 0)}\n",
      "Malformed=#{Map.get(report, :malformed, false)}\n"
    ]
  end

  # refusal_class maps a facade error to one class of the closed list FORMAT.md
  # fixes, never free text (tables/cards/cmd/cards-driver/main.go: refusalClass).
  defp refusal_class(reason) do
    cond do
      String.contains?(reason, "past the 65536-byte ceiling") ->
        "body-bound 65536"

      String.contains?(reason, "past the 512-entry ceiling") ->
        "list-bound " <> field_name(reason) <> " 512"

      String.contains?(reason, "stranded header key") ->
        "stranded-key " <> quoted_word(reason)

      String.contains?(reason, "retained field") ->
        "retain-lost"

      String.contains?(reason, "loss events") ->
        "unknown-present"

      true ->
        "malformed"
    end
  end

  defp field_name(reason) do
    rest = String.replace_prefix(reason, "cards: ", "")
    rest |> String.split(" has ", parts: 2) |> hd()
  end

  defp quoted_word(reason) do
    case Regex.run(~r/"([^"]*)"/, reason) do
      [_, word] -> word
      _ -> ""
    end
  end
end
