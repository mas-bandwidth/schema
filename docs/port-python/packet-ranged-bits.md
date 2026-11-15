## THE PACKET RANGED INTEGERS AND BITS(N)

A ranged integer costs `bits_required(min, max)` bits, the bit length of
`max - min` (zero for `min == max`, which writes nothing and reads `min`), and
carries `value - min`, exactly as the Go emitter sizes and offsets it (SPEC
§4.3). Python's int has no width, so the generator only picks the runtime
call by where the range fits: `serialize_int` inside int32, `serialize_int64`
inside int64, and `serialize_int128` past it (a full-range `uint64`, which the
Go emitter writes as raw offset bits). All three compute the same width and
the same offset, so the bytes are the Go emitter's; a read offset above
`max - min` is refused as `VALUE_OUT_OF_RANGE` and a write outside the range
raises `InvalidValue` in every build. `bits(N)` is one `serialize_bits` call
of N bits holding an int in `[0, 2^N)`, the same straight-line shape as a bare
integer, so it adds no code shape to measure against a descriptor walk.
`TestPythonPacketRangedBits` holds both kinds byte-identical to the generated
Go code on its own fixture, and to the goldens whose kinds are only the
core's and these two. On a Linux x86_64 bench, `BenchBits` (eight `bits`
fields, 3 to 48 bits wide, 156 wire bits) read from
`testdata/wire/bench_bits.bin` and written back, 200,000 iterations after a
1,000-iteration warmup, five sittings: 4.01–4.24 µs/op read and 4.02–4.40
µs/op write under CPython 3.11.18, and 4.22–7.40 µs/op read and 5.48–7.23
µs/op write under CPython 3.15.2 (the bench was shared, and the 3.15.2 spread
is that load).
