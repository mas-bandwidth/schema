# R27 — the manifest is the wire (elixir leg guard).
#
# LAW (docs/FIXED-FORM-ALGORITHM.md:1081, §5.9 #32 at :1416, #37 at :1507):
# `build/fixedform-corpus/manifest.txt` is the single value oracle every leg reads to.
# One line per file — `file= row= side= root= records= values=` — written by the dump
# from the SAME values it writes into the bytes. `values=` is the LAST field and runs
# to the end of the line (a quoted value may carry spaces: split the head on spaces,
# take the rest whole). ASSERT THE MANIFEST, NEVER READ THE DUMP: the declared default
# is not the value on the wire — `int_widen`'s `lead`/`trail` are `2863311530` and
# `3149642683` (0xAAAAAAAA / 0xBBBBBBBB) there while the schema says 1 and 2 — and a
# field absent from a line carries its schema default.
#
# WHAT THIS FILE ASSERTS, and why it is this shape:
#  1. the manifest LINE GRAMMAR, against a line rebuilt from the law (the corpus itself
#     is unbuilt here: `make tables-fixedform-corpus` needs the ../serialize checkout,
#     absent from this bench — so per the card the vector is constructed in-file).
#     The int_widen line below is faithful: row/side/root/records and the three records'
#     widened values [-1, -32768, 32767] are the oracle the Go gate asserts
#     (internal/codegen/elixirtable/fixedversioning_test.go:151-159), and the brackets
#     are the spec's own numbers (:1081, :1507). The spaced-quote probe line is marked
#     synthetic: it pins the "runs to the end of the line" rule and nothing else.
#  2. wire != default: the schema file on disk says 1 and 2, the manifest line says
#     2863311530 and 3149642683 (test/tables/VOLD_int_widen.schema).
#  3. the PRODUCTION read lands the wire values, not the defaults: ProfileConfig's
#     `experience uint32 = 0` (tables/examples/Tables.schema) carries the manifest's
#     lead number through the generated `_fixed_save`/`_fixed_load` pair, and the
#     report is clean. An unset field lands its schema default (the absent-field half).
#
# PRODUCTION ENTRYPOINT: `profile_config_fixed_load/1` in the generated fixed surface,
# reached the way test/conformance/elixir/driver_impl.ex reaches generated code: the
# beams compiled by `make build-conformance-elixir` are searched (EBIN env, else
# build/elixir-tables-ebin) for the module exporting it. Complete path under test:
# fixed_load -> FixedRuntime.read_file_header (framing) -> R.select (lineage/floor/byte
# comparison, §5.3) -> identity lane over the records -> {:ok, values, report}.
defmodule R27 do
  # the manifest's numbers for int_widen's brackets (docs/FIXED-FORM-ALGORITHM.md:1081;
  # 0xAAAAAAAA / 0xBBBBBBBB per :1507). Decimal here because the manifest writes decimal.
  @wire_lead 2_863_311_530
  @wire_trail 3_149_642_683

  # the oracle line for old_int_widen.bin, rebuilt from the law (see header).
  @int_widen_line "file=old_int_widen.bin row=int_widen side=old root=IntWiden records=3 " <>
                    "values=r0.lead=2863311530,r0.v=-1,r0.trail=3149642683," <>
                    "r1.lead=2863311530,r1.v=-32768,r1.trail=3149642683," <>
                    "r2.lead=2863311530,r2.v=32767,r2.trail=3149642683"

  # SYNTHETIC grammar probe: shape per :1081, values invented to carry a spaced quote.
  @spaced_line ~s(file=probe.bin row=probe side=none root=Probe records=1 values=r0.lead=2863311530,r0.s="a b",r0.trail=3149642683)

  def run do
    results = [
      assert_manifest_grammar(),
      assert_wire_is_not_default(),
      assert_reader_lands_wire_values(),
      assert_absent_field_lands_default()
    ]

    Enum.each(results, fn {n, name, ok, detail} ->
      IO.puts("#{if ok, do: "ok", else: "FAIL"} #{n} #{name} #{detail}")
    end)

    if Enum.all?(results, fn {_, _, ok, _} -> ok end), do: 0, else: 1
  end

  # 1. values= is the last field and runs to the end of the line: split the head on
  #    spaces, take the rest whole (:1081); commas inside quotes do not split (:1428).
  defp assert_manifest_grammar do
    m = parse_manifest_line(@int_widen_line)

    spaced = parse_manifest_line(@spaced_line)

    ok =
      m["file"] == "old_int_widen.bin" and m["row"] == "int_widen" and
        m["side"] == "old" and m["root"] == "IntWiden" and m["records"] == "3" and
        values(m)["r0.lead"] == "2863311530" and values(m)["r0.trail"] == "3149642683" and
        values(m)["r1.v"] == "-32768" and values(m)["r2.v"] == "32767" and
        values(spaced)["r0.s"] == "\"a b\""

    {1, "manifest-grammar", ok, "r0.lead=#{values(m)["r0.lead"]} r0.s=#{values(spaced)["r0.s"]}"}
  end

  # 2. the declared default is not the value on the wire (:1081, :1507).
  defp assert_wire_is_not_default do
    schema = File.read!("test/tables/VOLD_int_widen.schema")

    default = fn field ->
      [_, n] = Regex.run(~r/#{field}\s+uint32\s*=\s*(\d+)/, schema)
      n
    end

    m = parse_manifest_line(@int_widen_line)
    lead_default = default.("lead")
    trail_default = default.("trail")
    lead_wire = values(m)["r0.lead"]
    trail_wire = values(m)["r0.trail"]

    ok =
      lead_default == "1" and trail_default == "2" and lead_wire == "2863311530" and
        trail_wire == "3149642683" and lead_wire != lead_default and
        trail_wire != trail_default

    {2, "wire-is-not-default", ok,
     "schema=#{lead_default},#{trail_default} manifest=#{lead_wire},#{trail_wire}"}
  end

  # 3. the generated reader lands the manifest (wire) value, not the schema default.
  defp assert_reader_lands_wire_values do
    mod = fixed_module!()
    struct_mod = Module.concat([unit_namespace(mod), "ProfileConfig"])

    value = struct(struct_mod, experience: @wire_lead, timestamp: @wire_trail)

    file = apply(mod, :profile_config_fixed_save, [[value]])

    ok =
      case apply(mod, :profile_config_fixed_load, [file]) do
        {:ok, [landed], report} ->
          landed.experience == @wire_lead and landed.timestamp == @wire_trail and
            report.malformed == false and
            report.unknown == 0 and report.kind_mismatch == 0 and report.clamped == 0 and
            report.widened == 0 and report.duplicate == 0

        _ ->
          false
      end

    {3, "reader-lands-wire-values", ok, "experience=#{@wire_lead} timestamp=#{@wire_trail}"}
  end

  # 4. a field absent from the write lands its schema default (:1081, last clause).
  defp assert_absent_field_lands_default do
    mod = fixed_module!()
    struct_mod = Module.concat([unit_namespace(mod), "ProfileConfig"])

    file = apply(mod, :profile_config_fixed_save, [[struct(struct_mod)]])

    ok =
      case apply(mod, :profile_config_fixed_load, [file]) do
        {:ok, [landed], _report} ->
          landed.experience == 0 and landed.tilt == 0 and landed.badge == 0 and
            landed.port == 0

        _ ->
          false
      end

    {4, "absent-field-lands-default", ok, "experience=0 tilt=0 badge=0 port=0"}
  end

  # ---- the manifest line parser: the spec's rule, executable ----

  defp parse_manifest_line(line) do
    [head, values] = String.split(line, "values=", parts: 2)

    fields =
      head |> String.split() |> Map.new(fn f -> split_kv!(f) end)

    Map.put(fields, "values", String.trim_trailing(values))
  end

  defp values(fields) do
    fields["values"] |> split_values() |> Map.new(fn p -> split_kv!(p) end)
  end

  defp split_kv!(f) do
    [k, v] = String.split(f, "=", parts: 2)
    {k, v}
  end

  # comma-split that does not split inside a quoted value (commas there are
  # escaped `\,` per :1428; the probe carries none, the rule stands regardless).
  defp split_values(s), do: split_values(s, "", [], false)

  defp split_values(<<>>, cur, acc, _), do: Enum.reverse([cur | acc])

  defp split_values(<<"\\", c, rest::binary>>, cur, acc, quoted) do
    split_values(rest, cur <> "\\" <> <<c>>, acc, quoted)
  end

  defp split_values(<<"\"", rest::binary>>, cur, acc, quoted) do
    split_values(rest, cur <> "\"", acc, not quoted)
  end

  defp split_values(<<",", rest::binary>>, cur, acc, false) do
    split_values(rest, "", [cur | acc], false)
  end

  defp split_values(<<c, rest::binary>>, cur, acc, quoted) do
    split_values(rest, cur <> <<c>>, acc, quoted)
  end

  # ---- generated-code lookup, the way the driver does it ----

  # driver_impl.ex loads the beams from EBIN (else build/elixir-tables-ebin) and
  # finds a root's reader BY SEARCH, not by a generated index. Same here: the
  # module exporting profile_config_fixed_load/1.
  defp fixed_module! do
    ebin = System.get_env("EBIN", "build/elixir-tables-ebin")
    Code.prepend_path(ebin)

    want = :profile_config_fixed_load

    mod =
      Path.wildcard(Path.join(ebin, "*.beam"))
      |> Enum.map(fn path -> path |> Path.basename(".beam") |> String.to_atom() end)
      |> Enum.find(fn m ->
        Code.ensure_loaded?(m) and function_exported?(m, want, 1)
      end)

    unless mod do
      IO.puts(
        :stderr,
        "R27: no module exports profile_config_fixed_load/1 under #{ebin} — run `make build-conformance-elixir` first"
      )

      System.halt(1)
    end

    mod
  end

  # the unit namespace is the module path minus the fixed surface (driver_impl.ex:
  # "a unit's namespace is the camel case of its manifest key").
  defp unit_namespace(mod) do
    mod |> Module.split() |> Enum.drop(-1) |> Module.concat()
  end
end

case R27.run() do
  0 -> :ok
  1 -> System.halt(1)
end
