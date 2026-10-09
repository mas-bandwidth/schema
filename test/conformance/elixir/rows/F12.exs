# F12 — a second layout for a held hash (docs/SPEC-TABLES.md §5.3, the file
# and the framings of the fixed form).
#
# THE LAW (docs/SPEC-TABLES.md:7032): "a layout is NAMED BY ITS HASH and a
# second layout for a second hash amends nothing. A second layout for a hash
# already held is refused by name and changes nothing."
#
# THE PRODUCTION PATH THIS ASSERTS: Tblfx1.FX1Fixed.fx_root_fixed_load/2 is the
# leg's reader entrypoint. It calls FixedRuntime.read_file_header/1 to split the
# file, then fx_root_fixed_records/5, which calls FixedRuntime.select/4 with the
# build's own lineage. The header's hash is TAKEN AS GIVEN (the definitions
# digest is not on the wire, so nothing re-derives it); against a hash this
# build HOLDS, the layout bytes the file carries are COMPARED to the bytes the
# lock recorded. Same bytes: the identity lane opens. Different bytes: a lie
# about a known version, one name — `:layout_malformed` — refused before a
# record byte is touched, no counter moved.
#
# THIS TEST REACHES THE GENERATED CODE THE WAY THE LEG'S OWN GATE DOES: the
# fixed-form beams compiled into build/elixir-fixedform/ebin (make/elixir.mk,
# ELIXIR_FIXED_UNITS), loaded by code path, exactly as
# `tables-elixir-fixed-form` runs test/elixir-fixedform/main.exs with
# `-pa build/elixir-fixedform/ebin`. It depends on no corpus file and no other
# rows/ test.

Code.prepend_path("build/elixir-fixedform/ebin")

alias Tblfx1.FX1Fixed
alias Tblfx1.FixedRuntime, as: R

defmodule TestRowF12 do
  def ok(name), do: IO.puts("ok   #{name}")

  def fail(name, why) do
    :persistent_term.put({__MODULE__, :fails}, [
      name | :persistent_term.get({__MODULE__, :fails}, [])
    ])

    IO.puts("FAIL #{name}: #{why}")
  end

  def eq(name, got, want) do
    if got == want,
      do: ok(name),
      else: fail(name, "got #{inspect(got, limit: 12)}, want #{inspect(want, limit: 12)}")
  end

  def verdict do
    case :persistent_term.get({__MODULE__, :fails}, []) do
      [] ->
        IO.puts("F12: a second layout for a held hash is refused by name, in its own file")
        System.halt(0)

      fails ->
        IO.puts("FAILED: #{Enum.join(Enum.reverse(fails), ", ")}")
        System.halt(1)
    end
  end
end

alias TestRowF12, as: Leg

# THE VECTOR, BUILT FROM THE LAW AND THE LEG'S OWN FACTS — no corpus file. The
# held hash and the lock's own layout bytes are this build's own; the header
# carries the hash and the LENGTH, and the layout rides behind it. A record's
# body is just the declared byte count, because the refusal F12 names happens
# in `select`, BEFORE any record is split off (order is §5.3 step 4 vs step 7).
hash = FX1Fixed.fx_root_fixed_hash()
layout = FX1Fixed.fx_root_fixed_layout()
body_bytes = FX1Fixed.fx_root_fixed_body_bytes()

header = R.file_header(hash, byte_size(layout))
record = <<hash::little-unsigned-64, 0::size(body_bytes)-unit(8)>>
clean = header <> layout <> record

# bend ONE byte inside the layout, past the header, without touching the
# header's own hash: the SAME held hash now sits over DIFFERENT layout bytes.
plant = fn data, at, byte ->
  <<binary_part(data, 0, at)::binary, byte,
    binary_part(data, at + 1, byte_size(data) - at - 1)::binary>>
end

held_over_other = plant.(clean, byte_size(header) + 5, 0x99)

# CONTROL: the reader HOLDS this layout, and its bytes match, so the identity
# lane opens it — the refusal below is about the DIFFERENCE, not the file.
case FX1Fixed.fx_root_fixed_load(clean) do
  {:ok, _values, _report} ->
    Leg.ok("CONTROL: the un-bent layout, under its held hash, opens")

  other ->
    Leg.fail(
      "CONTROL: the un-bent layout, under its held hash, opens",
      "got #{inspect(other, limit: 12)}"
    )
end

# F12: the same held hash over different layout bytes. The hash selects the
# held entry; the byte comparison fails; one name.
f12 = fn ->
  case FX1Fixed.fx_root_fixed_load(held_over_other) do
    {:error, :layout_malformed, report} ->
      Leg.ok("F12: a second layout for a held hash is refused by name")

      # "AND CHANGES NOTHING": a refusal by name decodes nothing and moves no
      # counter, and a named refusal is never `malformed`.
      Leg.eq("F12: the refusal is not damage", report.malformed, false)
      Leg.eq("F12: nothing was decoded — no clamp counter", report.clamped, 0)
      Leg.eq("F12: no unknown counter", report.unknown, 0)
      Leg.eq("F12: no kind-mismatch counter", report.kind_mismatch, 0)
      Leg.eq("F12: no widened counter", report.widened, 0)

    other ->
      Leg.fail(
        "F12: a second layout for a held hash is refused by name",
        "got #{inspect(other, limit: 12)}"
      )
  end
end

f12.()

# THE CONTRAST THAT MAKES F12 DISCRIMINATING: a hash in NO lineage entry is a
# different answer — `:layout_newer`, the file's hash reported — which is why a
# held-hash-over-different-bytes must be its own name and not that one.
unknown_hash = hash + 1
unknown_file = R.file_header(unknown_hash, byte_size(layout)) <> layout <> record

case FX1Fixed.fx_root_fixed_load(unknown_file) do
  {:error, :layout_newer, newer_report} ->
    Leg.ok("F12: an unknown hash is `layout_newer`, a different name")

    Leg.eq(
      "F12: and `layout_newer` reports the file's hash",
      newer_report.layout_hash,
      unknown_hash
    )

  other ->
    Leg.fail(
      "F12: an unknown hash is `layout_newer`, a different name",
      "got #{inspect(other, limit: 12)}"
    )
end

Leg.verdict()
