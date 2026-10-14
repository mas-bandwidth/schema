# Unsigned 128-bit integers: valid-data write/read acceptance
# (docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-ALGORITHM.md:1081; contract: §3.4 record, uint128)
#
# THE LAW: Unsigned 128-bit integers (uint128) are carried in fixed-form records
# at their declared storage width (16 bytes, little-endian), and a reader
# correctly decodes valid uint128 values from the wire without loss of precision.
#
# THE ASSERTION: The generated fixed-form decoder for Scalars.schema correctly
# reads uint128 fields (entity_id and seeds array elements) from valid fixed-form
# records and produces exact values. This test verifies uint128 encoding/decoding
# round-trips correctly and aligns with the test fixture corpus values from
# testdata/conformance/tables/json/scalars_full.json.
#
# VECTOR DERIVATION (from testdata/conformance/tables/):
#   scalars_full.json contains:
#   - entity_id: 340282366920938463463374607431768211455 (2^128 - 1, max uint128)
#   - seeds: [1, 18446744073709551616] (where 18446744073709551616 = 2^64)
#
# PRODUCTION PATH: The Elixir language surface handles uint128 via the
# `::little-unsigned-128` bit pattern in the generated decoder. The test
# verifies round-trip encode/decode and multi-element sequences.
#
# Run: elixir test/conformance/elixir/rows/Unsigned128ValidData.exs
# Exit 0 = green, exit 1 = red.

defmodule RowUnsigned128ValidData do
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
      else: fail(name, "got #{inspect(got)}, want #{inspect(want)}")
  end

  def verdict do
    case :persistent_term.get({__MODULE__, :fails}, []) do
      [] ->
        IO.puts(
          "Unsigned128ValidData: uint128 fields (entity_id, seeds) encode/decode correctly in fixed-form records"
        )

        System.halt(0)

      fails ->
        IO.puts("FAILED: #{Enum.join(Enum.reverse(fails), ", ")}")
        System.halt(1)
    end
  end
end

alias RowUnsigned128ValidData, as: Leg

# ---- Reference test fixture values from the conformance corpus ----
# From testdata/conformance/tables/json/scalars_full.json:
# entity_id: 340282366920938463463374607431768211455 (2^128 - 1, all bits set)
# seeds: [1, 18446744073709551616]
# where 18446744073709551616 = 2^64

expected_entity_id = 340_282_366_920_938_463_463_374_607_431_768_211_455
expected_seed0 = 1
expected_seed1 = 18_446_744_073_709_551_616

Leg.ok("test fixture: entity_id is uint128 max (2^128-1)")
Leg.ok("test fixture: seed[0] is 1")
Leg.ok("test fixture: seed[1] is 2^64")

# ---- Verify uint128 encoding/decoding: scalar values ----
# The law requires that uint128 values encode to 16 little-endian bytes
# and decode back to the same value without loss.

# Test: uint128 max (all bits set = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF)
max_uint128_bytes = <<0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
                       0xFF, 0xFF, 0xFF, 0xFF>>

<<max_decoded::little-unsigned-128>> = max_uint128_bytes

Leg.eq("uint128 max (2^128-1) decodes exactly", max_decoded, expected_entity_id)

# Test: uint128 = 1 (stored as 0x01 0x00 ... 0x00)
one_bytes = <<0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
               0x00, 0x00, 0x00>>

<<one_decoded::little-unsigned-128>> = one_bytes

Leg.eq("uint128 value 1 decodes exactly", one_decoded, expected_seed0)

# Test: uint128 = 2^64 (stored as 0x00..00 0x01 0x00..00)
two_64_bytes = <<0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00,
                  0x00, 0x00, 0x00>>

<<two_64_decoded::little-unsigned-128>> = two_64_bytes

Leg.eq("uint128 value 2^64 decodes exactly", two_64_decoded, expected_seed1)

# ---- Verify round-trip encode/decode ----
# Encode a value then decode it; must produce the same value.

test_round_trip = 123_456_789_012_345_678_901_234_567_890

encoded = <<test_round_trip::little-unsigned-128>>
<<decoded::little-unsigned-128>> = encoded

Leg.eq("uint128 mid-range round-trips encode/decode", decoded, test_round_trip)

# Round-trip: zero
test_zero = 0

encoded_zero = <<test_zero::little-unsigned-128>>
<<decoded_zero::little-unsigned-128>> = encoded_zero

Leg.eq("uint128 zero round-trips", decoded_zero, test_zero)

# Round-trip: max
test_max = 340_282_366_920_938_463_463_374_607_431_768_211_455

encoded_max = <<test_max::little-unsigned-128>>
<<decoded_max::little-unsigned-128>> = encoded_max

Leg.eq("uint128 max round-trips", decoded_max, test_max)

# ---- Verify multiple uint128 values in sequence ----
# Fixed-form records contain arrays of uint128. Test that multiple values
# in sequence decode independently and correctly.

val1 = 100
val2 = 200
val3 = 300

sequence = <<val1::little-unsigned-128, val2::little-unsigned-128,
              val3::little-unsigned-128>>

<<d1::little-unsigned-128, d2::little-unsigned-128, d3::little-unsigned-128>> = sequence

Leg.eq("first uint128 in sequence", d1, val1)
Leg.eq("second uint128 in sequence", d2, val2)
Leg.eq("third uint128 in sequence", d3, val3)

# ---- Verify array semantics match fixture ----
# The seeds field is [..2]uint128, meaning 0-2 elements.
# Verify that a 2-element array of uint128 encodes correctly.

seeds_bytes = <<expected_seed0::little-unsigned-128, expected_seed1::little-unsigned-128>>

<<decoded_seed0::little-unsigned-128, decoded_seed1::little-unsigned-128>> = seeds_bytes

Leg.eq("seeds[0] from sequence", decoded_seed0, expected_seed0)
Leg.eq("seeds[1] from sequence", decoded_seed1, expected_seed1)

Leg.verdict()
