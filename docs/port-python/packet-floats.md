## THE PACKET FLOATS

The codec now also carries `float32`, `float64`, the compressed `float32` and
`fixed(I, F)` / `ufixed(I, F)` (P4's floats, compressed-float and fixed-point
rows); the core cut's list of refused kinds ([PORT-PYTHON.md](../PORT-PYTHON.md),
THE PACKET CORE CUT) no longer includes them.
Each kind is its own emitter file (`kind_float.go`, `kind_fixed.go`)
registered in the core's kind table.

- **Reads go through the bit layer alone.** `serialize_float` and
  `serialize_double` refuse a non-finite pattern by its raw bits
  (`non_finite_float`); a compressed float is read by
  `serialize_compressed_float_precomputed`, so serialize.py's own quantizer
  dequantizes it and refuses an index in the bit headroom; a fixed point field
  is read by `serialize_fixed`, which refuses an offset past the raw span and
  reads a zero-bit range as its one value.
- **Writes validate first, inline.** `_s_validate` holds each float field to
  `type(v) is float` and a chained range test against the largest finite
  value of its width (a compressed float: its declared `[min, max]` rounded
  to float32). A value that passes is finite and in range, so the common case
  never calls out; one that fails goes to `validate_float32`,
  `validate_float64` or `validate_compressed_float` in the runtime, which
  decide exactly (a double just above the largest float32 that rounds down to
  it is accepted and rides as that float32) and raise `InvalidValue` naming
  the field. A fixed point field is an `int` held to `[min << F, max << F]`.
- **The quantizer is the runtime's.** The generator folds only the
  declaration's constants (the step count, the wire width, the float32
  `delta` and `min`) into the `_precomputed` call; the runtime checks the
  tuple against the declaration's shape on every call and does every float32
  operation of the step index itself.

D1's two shapes were measured for the float hot path on a fleet bench (Linux
x86_64, a shared bench under other load), on a type of four `float32`, two
`float64` and two compressed `float32` fields, 100,000 reads and writes after
a 1,000-iteration warmup, five interleaved sittings per interpreter, in
µs/op as minimum and median over the sittings:

| interpreter | shape | read min / median | write min / median |
|---|---|---|---|
| CPython 3.11.18 | inline check, precomputed compressed (kept) | 23.8 / 27.0 | 29.2 / 30.7 |
| CPython 3.11.18 | inline check, declaration compressed | 25.6 / 26.8 | 28.9 / 31.5 |
| CPython 3.11.18 | runtime check on every field | 24.9 / 25.3 | 33.5 / 35.2 |
| CPython 3.15.2 | inline check, precomputed compressed (kept) | 24.4 / 26.0 | 29.4 / 30.2 |
| CPython 3.15.2 | inline check, declaration compressed | 24.3 / 26.3 | 30.3 / 32.2 |
| CPython 3.15.2 | runtime check on every field | 24.2 / 25.3 | 34.3 / 36.6 |

The inline check writes 15–21% faster than a runtime call per field on both
pins; reads do not validate, so the read columns differ only by noise. The
precomputed compressed call writes 3–7% faster at the median than the
declaration call, which re-derives the step count from `(min, max,
resolution)` on every field. The generator emits the kept shape.

The gates are `TestPythonPacketFloats` (fixture schemas of its own: every
float kind alone and beside `bool` and `uint8`, its wire laid out
independently in Go from SPEC §4.3 and written byte for byte; every
out-of-contract write refused naming the field; every truncated or hostile
input refused; the C++-pinned goldens whose kinds are all carried, today
`compressed_probe`, `rigidbody_at_rest` and `rigidbody_moving`), and
`packet-finite-python` (test/packet-finite/python/main.py under both pins),
with `packet-finite-python-negative-control`. A golden that also needs a kind
another packet card carries stays on the harness's skip list, named by that
kind.
