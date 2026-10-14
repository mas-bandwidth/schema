# Assertion for elixir/W8: bytes(N) writes length units, never all Max.
# docs/FIXED-FORM-ALGORITHM.md:202-203
#
# "Arrays write count elements and text and bytes length units, never all Max"
#
# The fixed-form writer for a bytes(N) field writes byte_size(value)::little-signed-32
# followed by value::binary, then zero-pads to the declared bound. The reader takes
# only the used portion via binary_part/3. This test proves the round-trip: bytes
# shorter than Max stay short.
Code.append_path("build/elixir-tables-ebin")

{:module, _} = Code.ensure_loaded(Blockdemo.PaddedFixed)
{:module, _} = Code.ensure_loaded(Blockdemo.PaddedFrame)

alias Blockdemo.PaddedFixed, as: PF
alias Blockdemo.PaddedFrame, as: Frame

defmodule TestRowW8 do
  @moduledoc false

  @b12 <<0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11>>

  def run do
    results = [
      case1(),
      case2(),
      case3()
    ]

    failed = Enum.count(results, fn {:fail, _} -> true; _ -> false end)

    Enum.each(results, fn
      {:fail, msg} -> IO.puts(msg)
      {:pass, msg} -> IO.puts(msg)
    end)

    if failed > 0 do
      IO.puts("FAIL: #{failed} assertion(s) failed")
      System.halt(1)
    else
      IO.puts("OK: #{length(results)} assertions passed")
    end
  end

  defp case1 do
    frame = %Frame{marker: 0, stamp: 0, rows: [], blob: <<1, 2, 3>>}
    body = PF.padded_frame_fixed_save([frame])
    {:ok, [got], _report} = PF.padded_frame_fixed_load(body)

    if got.blob == <<1, 2, 3>> do
      {:pass, "PASS bytes(12) <<1,2,3>> rounds trip as 3 bytes (not 12)"}
    else
      {:fail, "FAIL bytes(12) <<1,2,3>>: expected <<1,2,3>>, got #{inspect(got.blob)}"}
    end
  end

  defp case2 do
    frame = %Frame{marker: 0, stamp: 0, rows: [], blob: <<>>}
    body = PF.padded_frame_fixed_save([frame])
    {:ok, [got], _report} = PF.padded_frame_fixed_load(body)

    if got.blob == <<>> do
      {:pass, "PASS bytes(12) <<>> rounds trip as 0 bytes (not 12)"}
    else
      {:fail, "FAIL bytes(12) <<>>: expected <<>>, got #{inspect(got.blob)}"}
    end
  end

  defp case3 do
    frame = %Frame{marker: 0, stamp: 0, rows: [], blob: @b12}
    body = PF.padded_frame_fixed_save([frame])
    {:ok, [got], _report} = PF.padded_frame_fixed_load(body)

    if got.blob == @b12 do
      {:pass, "PASS bytes(12) full 12-byte round trip"}
    else
      {:fail, "FAIL bytes(12) full: expected 12 bytes, got #{inspect(got.blob)}"}
    end
  end
end

TestRowW8.run()