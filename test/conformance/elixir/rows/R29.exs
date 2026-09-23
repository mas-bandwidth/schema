# R29: the band case — widening across the 65536 ceiling, and the bounds pass
# clamping to the writer's bounds
#
# Law: docs/FIXED-FORM-ALGORITHM.md §5.7, the uint_widen versioning row. When
# widening uint16 → uint32, the value 0xFFFF (65535, the max uint16, just
# below the 2^16 = 65536 ceiling) must ZERO-extend to 65535, not sign-extend
# to 0xFFFFFFFF (4294967295). The widened counter fires once; the clamped
# counter does not fire because the value is within the writer's declared
# bounds (uint16 implicit range: 0–65535). The bounds pass clamps to the
# WRITER's bounds, not the reader's — and since 0xFFFF is at the writer's
# own ceiling, no clamp fires.
#
# Byte vector derivation (the law, not a dump):
#   VOLD schema: fixed table UintWiden { lead uint32=1, v uint16=0, trail uint32=2 }
#   VNEW schema: fixed table UintWiden { lead uint32=1, v uint32=0, trail uint32=2 }
#   The dump convention writes lead=0xAAAAAAAA, v=0xFFFF, trail=0xBBBBBBBB.
#   Old body (10 bytes): <<0xAAAAAAAA::u32, 0xFFFF::u16, 0xBBBBBBBB::u32>>
#   New body (12 bytes): <<0xAAAAAAAA::u32, 65535::u32, 0xBBBBBBBB::u32>>
#   Plan: copy lead (4), widen v (u16→u32 unsigned), copy trail (4).
#   The widen entry format is {:widen, src, dst, src_size, dst_size, signed}
#   as emitted by internal/codegen/elixirtable/fixedruntime.go:1103.

Code.prepend_path("build/elixir-tables-ebin")

alias Tabledemo.FixedRuntime, as: R

defmodule R29 do
  def run do
    IO.puts("== R29: the band case — widening across the 65536 ceiling")

    body = <<
      0xAAAAAAAA::little-unsigned-32,
      0xFFFF::little-unsigned-16,
      0xBBBBBBBB::little-unsigned-32
    >>

    plan = [
      {:copy, 0, 0, 4},
      {:widen, 4, 4, 2, 4, false},
      {:copy, 6, 8, 4}
    ]

    prefill = <<
      1::little-unsigned-32,
      0::little-unsigned-32,
      2::little-unsigned-32
    >>

    {image, report} = R.run(plan, body, prefill, R.report())

    <<lead::little-unsigned-32, v::little-unsigned-32, trail::little-unsigned-32>> = image

    check("lead bracket preserved", lead == 0xAAAAAAAA)
    check("v zero-extended to 65535 (not sign-extended to 0xFFFFFFFF)", v == 65535)
    check("trail bracket preserved", trail == 0xBBBBBBBB)
    check("widened counter fired exactly once", report.widened == 1)
    check("clamped counter did not fire (value within writer's bounds)", report.clamped == 0)
    check("no damage", report.malformed == false)

    # NEGATIVE CONTROL: the same body read through a SIGNED widen would
    # sign-extend 0xFFFF to 0xFFFFFFFF. If this assertion fails, the widen
    # op no longer distinguishes signed from unsigned.
    {image_signed, report_signed} =
      R.run(
        [{:copy, 0, 0, 4}, {:widen, 4, 4, 2, 4, true}, {:copy, 6, 8, 4}],
        body,
        prefill,
        R.report()
      )

    <<_::little-unsigned-32, v_signed::little-unsigned-32, _::little-unsigned-32>> = image_signed
    check("NEGATIVE CONTROL: signed widen gives 0xFFFFFFFF", v_signed == 0xFFFFFFFF)
    check("NEGATIVE CONTROL: signed widen also fires widened", report_signed.widened == 1)
  end

  defp check(name, true), do: IO.puts("  ok   #{name}")

  defp check(name, false) do
    IO.puts(:stderr, "  FAIL #{name}")
    Process.put(:failed, true)
  end
end

R29.run()

if Process.get(:failed) do
  System.halt(1)
else
  IO.puts("\nok — R29: the band case passes")
end
