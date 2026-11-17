## THE PACKET INT128

- **The 128-bit integers are the core's own shape.** `uint128` is one
  `serialize_uint128` call (128 bits, low 64-bit half first) and `int128`,
  always ranged, goes through the same ranged call the 64-bit kinds use,
  picked by the range (`serialize_int`, `serialize_int64` or
  `serialize_int128`, the same bytes wherever a narrower call fits). The
  write holds the value to its declared range in every build. There is no
  second code shape to measure: the emitter adds no hot path the core's
  straight-line form does not already have. With no C++-pinned golden that
  uses only these kinds and the core's (every examples128 golden declares
  `fixed` too), TestPythonPacketInt128 holds them to the generated Go unit
  over serialize.go on fixture schemas of its own.
