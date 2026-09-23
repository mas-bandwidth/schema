# BoolValuesValidData — Boolean values: valid-data write/read acceptance
# (docs/SPEC-TABLES.md §3.4, the bool kind).
#
# THE LAW (§3.4): a bool is one byte, written as 0 for false and 1 for true;
# a read accepts any nonzero byte as true and normalises it to true in the
# returned struct, and a write emits 1 for true and 0 for false.
#
# THE PRODUCTION PATH THIS ASSERTS:
#   Tabledemo.TablesFixed.weapon_config_fixed_load/2 reads the bool byte by
#   `f_homing != 0`. The write path is `if(v_homing, do: 1, else: 0)::unsigned-8`.
#
# THIS TEST REACHES THE GENERATED CODE THE WAY THE LEG'S OWN CONFORMANCE
# DRIVER DOES: the table beams compiled into build/elixir-tables-ebin by
# make/elixir.mk (build-conformance-elixir), loaded by the ebin path.
# It depends on no corpus file and no other rows/ test.

defmodule TestRowBoolValuesValidData do
  def run do
    ebin = System.get_env("EBIN", "build/elixir-tables-ebin")
    :code.add_path(String.to_charlist(ebin))

    # Load all the beams the conformance build compiled.
    _mods =
      Path.wildcard(ebin <> "/*.beam")
      |> Enum.map(fn path ->
        mod = path |> Path.basename(".beam") |> String.to_atom()
        Code.ensure_loaded(mod)
        mod
      end)

    tf = Tabledemo.TablesFixed
    fr = Tabledemo.FixedRuntime
    body_bytes = tf.weapon_config_fixed_body_bytes()
    layout = tf.weapon_config_fixed_layout()
    hash = tf.weapon_config_fixed_hash()
    header = fr.file_header(hash, byte_size(layout))
    file_header_size = fr.file_header_bytes()
    layout_size = byte_size(layout)
    record_hash_size = 8

    # The homing bool is at body offset 16 (4+4+4+4 for damage, speed,
    # penetration, channel). In the FILE it is offset by the header, layout
    # and the per-record hash.
    homing_body_offset = 16

    fails = []
    fails = check_false_roundtrip(fails, tf, header, layout, body_bytes, homing_body_offset, file_header_size, layout_size, record_hash_size)
    fails = check_true_roundtrip(fails, tf, header, layout, body_bytes, homing_body_offset, file_header_size, layout_size, record_hash_size)
    fails = check_hostile_normalisation(fails, tf, header, layout, body_bytes, homing_body_offset, file_header_size, layout_size, record_hash_size)

    if fails == [] do
      IO.puts("BoolValuesValidData: bool values write/read acceptance, in its own file")
      System.halt(0)
    else
      IO.puts("FAILED: #{Enum.join(Enum.reverse(fails), ", ")}")
      System.halt(1)
    end
  end

  defp build_file(tf, header, layout, body_bytes, homing_val) do
    # Build a one-record file: header + layout + one record (hash + body).
    # The body is all zeros except the homing byte.
    body = :binary.copy(<<0>>, body_bytes)
    body = :binary.bin_to_list(body)
    body = List.update_at(body, homing_val.offset, fn _ -> homing_val.byte end)
    body = :binary.list_to_bin(body)

    record = <<tf.weapon_config_fixed_hash()::little-unsigned-64>> <> body
    header <> layout <> record
  end

  defp check_false_roundtrip(fails, tf, header, layout, body_bytes, homing_body_offset, file_header_size, layout_size, record_hash_size) do
    _homing_offset = file_header_size + layout_size + record_hash_size + homing_body_offset

    file = build_file(tf, header, layout, body_bytes, %{offset: homing_body_offset, byte: 0})

    case tf.weapon_config_fixed_load(file) do
      {:ok, [loaded], report} ->
        fails =
          if loaded.homing == false,
            do: fails,
            else: ["false: homing should be false, got #{inspect(loaded.homing)}" | fails]

        fails =
          if not report.malformed and report.clamped == 0 and report.unknown == 0,
            do: fails,
            else: ["false: counters not clean: #{inspect(report)}" | fails]

        # The save/load round trip: save it back and verify byte identity.
        resaved = tf.weapon_config_fixed_save([loaded])

        fails =
          if resaved == file,
            do: fails,
            else: ["false: save-load-save is not byte identical" | fails]

        fails

      other ->
        ["false: weapon_config_fixed_load returned #{inspect(other)}" | fails]
    end
  end

  defp check_true_roundtrip(fails, tf, header, layout, body_bytes, homing_body_offset, file_header_size, layout_size, record_hash_size) do
    homing_offset = file_header_size + layout_size + record_hash_size + homing_body_offset

    file = build_file(tf, header, layout, body_bytes, %{offset: homing_body_offset, byte: 1})

    case tf.weapon_config_fixed_load(file) do
      {:ok, [loaded], report} ->
        fails =
          if loaded.homing == true,
            do: fails,
            else: ["true: homing should be true, got #{inspect(loaded.homing)}" | fails]

        fails =
          if not report.malformed and report.clamped == 0 and report.unknown == 0,
            do: fails,
            else: ["true: counters not clean: #{inspect(report)}" | fails]

        resaved = tf.weapon_config_fixed_save([loaded])

        fails =
          if resaved == file,
            do: fails,
            else: ["true: save-load-save is not byte identical" | fails]

        # Verify the byte at the homing offset is exactly 1 in the written file.
        hb = :binary.at(resaved, homing_offset)
        fails =
          if hb == 1,
            do: fails,
            else: ["true: homing byte in resaved file is not 1, got #{hb} at offset #{homing_offset} (file_header_size=#{file_header_size}, layout_size=#{layout_size}, homing_body_offset=#{homing_body_offset})" | fails]

        fails

      other ->
        ["true: weapon_config_fixed_load returned #{inspect(other)}" | fails]
    end
  end

  defp check_hostile_normalisation(fails, tf, header, layout, body_bytes, homing_body_offset, file_header_size, layout_size, record_hash_size) do
    homing_offset = file_header_size + layout_size + record_hash_size + homing_body_offset

    # Plant a hostile nonzero byte (0x7F) where the homing bool lives.
    file = build_file(tf, header, layout, body_bytes, %{offset: homing_body_offset, byte: 0x7F})

    case tf.weapon_config_fixed_load(file) do
      {:ok, [loaded], report} ->
        fails =
          if loaded.homing == true,
            do: fails,
            else: ["hostile 0x7F: homing should normalise to true, got #{inspect(loaded.homing)}" | fails]

        fails =
          if not report.malformed and report.clamped == 0 and report.unknown == 0,
            do: fails,
            else: ["hostile 0x7F: counters not clean (normalisation is not a clamp): #{inspect(report)}" | fails]

        # Resave: the write path must emit 1, not 0x7F.
        resaved = tf.weapon_config_fixed_save([loaded])

        fails =
          if :binary.at(resaved, homing_offset) == 1,
            do: fails,
            else: ["hostile 0x7F: resaved homing byte should be 1, got #{:binary.at(resaved, homing_offset)}" | fails]

        fails

      other ->
        ["hostile 0x7F: weapon_config_fixed_load returned #{inspect(other)}" | fails]
    end
  end
end

TestRowBoolValuesValidData.run()
