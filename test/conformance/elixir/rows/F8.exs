# cell elixir/F8 (§schema#876 row fixed_form_ragged_tail):
# a record region that is not a whole number of records is `malformed`,
# not a named refusal. docs/FIXED-FORM-ALGORITHM.md:894.
#
# VECTOR DERIVATION:
#   pack_config_fixed_save/1 creates a valid form-3 file:
#     header (20 bytes) + layout + records
#   Each record is record_bytes = @record_bytes:
#     8-byte layout hash + body
#   Appending 1..record_bytes-1 bytes makes rest mod record_bytes != 0,
#   which is the ragged tail — the split's catch-all clause answers
#   {:error, :malformed}.
#   Appending EXACTLY record_bytes bytes is a whole extra record whose
#   hash (0x5A..) names no layout: the reader answers {:error, :no_layout},
#   not malformed — the negative control.

defmodule RowF8 do
  @moduledoc false

  # THE VALID FILE: one record of default values
  # pack_config_fixed_save/1 writes a header, layout, and one record.
  # The record bytes = pack_config_fixed_record_bytes/0 (hash + body).
  def run do
    fails = []
    fails = check_good(fails)
    fails = check_ragged_tail(fails)
    fails = check_whole_record_negative_control(fails)

    if fails == [] do
      IO.puts("cell elixir/F8 OK: ragged tail is malformed, whole extra record is not")
      System.halt(0)
    else
      IO.puts("cell elixir/F8 FAILED: #{Enum.join(Enum.reverse(fails), "\n  ")}")
      System.halt(1)
    end
  end

  defp check_good(fails) do
    data = Tabledemo.PackFixed.pack_config_fixed_save([%Tabledemo.PackConfig{}])

    case Tabledemo.PackFixed.pack_config_fixed_load(data) do
      {:ok, _values, report} ->
        if report.malformed == false, do: fails, else: ["valid file must not set malformed: #{inspect(report)}" | fails]
      other ->
        ["valid file must open: #{inspect(other)}" | fails]
    end
  end

  defp check_ragged_tail(fails) do
    data = Tabledemo.PackFixed.pack_config_fixed_save([%Tabledemo.PackConfig{}])
    record_bytes = Tabledemo.PackFixed.pack_config_fixed_record_bytes()

    Enum.reduce(1..(record_bytes - 1), fails, fn extra, fails ->
      ragged = data <> :binary.copy(<<0x5A>>, extra)

      case Tabledemo.PackFixed.pack_config_fixed_load(ragged) do
        {:error, :malformed, report} ->
          if report.malformed == true and report.unknown == 0 and report.kind_mismatch == 0 and
               report.clamped == 0 and report.widened == 0 and report.duplicate == 0 and report.layout_hash == 0 do
            fails
          else
            ["ragged tail at +#{extra} has wrong counters: #{inspect(report)}" | fails]
          end

        {:error, reason, _report} ->
          ["ragged tail at +#{extra} should be :malformed, got: #{inspect(reason)}" | fails]

        {:ok, _values, _report} ->
          ["ragged tail at +#{extra} must not open" | fails]
      end
    end)
  end

  # NEGATIVE CONTROL: extra == record_bytes is one WHOLE extra record, not a
  # ragged tail. The appended hash (0x5A^8) names no layout, so the reader
  # answers :no_layout — and NEVER malformed.
  defp check_whole_record_negative_control(fails) do
    data = Tabledemo.PackFixed.pack_config_fixed_save([%Tabledemo.PackConfig{}])
    record_bytes = Tabledemo.PackFixed.pack_config_fixed_record_bytes()

    whole = data <> :binary.copy(<<0x5A>>, record_bytes)

    case Tabledemo.PackFixed.pack_config_fixed_load(whole) do
      {:error, :no_layout, report} ->
        if report.malformed == false, do: fails, else:
          ["extra == record_bytes must NOT set malformed: #{inspect(report)}" | fails]

      {:error, :malformed, _report} ->
        ["extra == record_bytes is a whole record, not a ragged tail: expected :no_layout, got :malformed" | fails]

      {:ok, _values, _report} ->
        ["extra == record_bytes must refuse" | fails]
    end
  end
end

RowF8.run()