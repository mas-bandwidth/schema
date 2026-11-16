## THE PACKET TEXT KINDS

`string(N)` and `bytes(N)` are one wire (SPEC §4.7): the length over [0, N],
align, then the used bytes.

- **`string(N)` is serialize's `serialize_string` over a buffer of N + 1**,
  the wire oracle SPEC §4.7 names. The length, the align, the interior-NUL
  refusal and the strict UTF-8 decode (Unicode Table 3-7: overlong forms,
  encoded surrogates, code points above `10FFFF`, truncated sequences, `0xFE`
  and `0xFF`) are the runtime's, in every build. The bound is N bytes of
  UTF-8. A write validates first: an ASCII `str` of at most N characters with
  no NUL passes inline; anything else goes to `validate_string`, which accepts
  what is valid and raises `InvalidValue` naming the field for a value that is
  not a `str`, holds a NUL, holds a lone surrogate or is longer than N bytes.
- **`bytes(N)` composes `serialize_int` over [0, N] and `serialize_bytes`.** A
  read returns `bytes`; a write takes `bytes`, a `bytearray` or a
  one-dimensional `"B"` `memoryview` of at most N bytes (`validate_bytes`).
- **wstring.** SPEC §4.12 reserves `wstring` and names no type, so the
  parser refuses it and the leg has nothing to carry.
- **The gates.** `TestPythonPacketText` reads the goldens whose kinds are all
  the core's and these, runs the shared string corpus through
  `test/packet-text/python/main.py`, and holds a fixture of its own to a Go
  reference writer and to every refusal; `make packet-utf8-python` runs the
  shared harness against the C++ oracle, every bit flip included. Each has a
  control in a copy of the generated tree.

The bench for the string hot path: a `string(15)` holding six ASCII bytes,
read from its wire and written back, 200,000 iterations after a
1,000-iteration warmup, over five interleaved sittings per interpreter on a
shared fleet bench, against the same codec composed in generated code from
`serialize_int` and `serialize_bytes` with the NUL scan and the decode
inline:

| interpreter | shape | read | write |
|---|---|---|---|
| CPython 3.11.18 (floor) | `serialize_string` (kept) | 3.36–3.55 µs/op | 1.90–2.16 µs/op |
| CPython 3.11.18 (floor) | composed | 3.56–3.73 µs/op | 3.00–3.24 µs/op |
| CPython 3.15.2 (current) | `serialize_string` (kept) | 2.07–3.49 µs/op | 1.22–1.96 µs/op |
| CPython 3.15.2 (current) | composed | 2.14–3.45 µs/op | 1.85–3.11 µs/op |

The one runtime call is as fast or faster on every row, and the generator
emits it.
