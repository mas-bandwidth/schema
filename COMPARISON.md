# Size comparison

The README claims general-purpose formats pay for their generality on the wire.
This is that claim, measured, on one representative message.

**Every number here is produced by running the real encoder.** Nothing is
computed by hand, and nothing is read out of a compiler constant. The schemas,
the input values and the script are all in [`comparison/`](comparison/) — run
`./comparison/measure.sh` and you should get this table.

**One message is not a benchmark suite.** It is a worked example you can check
line by line, which is worth more than a table of numbers you cannot.

## The message

A ship-spawn packet: type, quantized position, quantized rotation, quantized
velocity, some flags, a team, health and thrust. It is the corpus's
`ShipCreate` from [`examples/Types.schema`](examples/Types.schema), unchanged.

```
type ShipCreate {
    ship_type       ShipType                        // 6 wire values incl. None
    position        QuantizedPosition               // 3 x int32, each ±8388608
    rotation        QuantizedRotation               // 4 x int16, each ±1024
    linear_velocity QuantizedVelocity               // 3 x int32, each ±2097152
    has_flags       bool
    if has_flags {
        flags       ShipFlags                       // 4 flag bits
    }
    team            Team                            // 3 wire values incl. None
    health          int16 [min = 0, max = MaxHealth]  // MaxHealth = 1000
    thrust          int8  [min = 0, max = 100]
}
```

The exact values encoded are in [`comparison/VALUES.md`](comparison/VALUES.md).
Two choices there deliberately work *against* schema: the `has_flags` branch is
taken (the longest wire path), and the values are large and non-zero (varints
and Cap'n Proto packing both shrink on small or zero values, so encoding zeroes
would have flattered schema considerably).

## Results

| format | bytes | vs schema |
|---|---:|---:|
| **schema** | **28** | — |
| Cap'n Proto (packed) | 52 | 1.9× |
| Protobuf (proto3) | 56 | 2.0× |
| FlatBuffers | 72 | 2.6× |
| Cap'n Proto (unpacked) | 96 | 3.4× |

Measured with `protoc` 35.1, `capnp` 1.5.0, `flatc` 25.12.19 and the schema
compiler at this commit.

## Where schema's 28 bytes go

The measurement program encodes the message, round-trips it back, and reports
the writer's own byte count — 219 bits, which is exactly the compiler's
`ShipCreateMaxBits`. Those bits:

| field | bits | why |
|---|---:|---|
| `ship_type` | 3 | 6 wire values |
| `position` (3 × 25) | 75 | 16777217 values per axis |
| `rotation` (4 × 12) | 48 | 2049 values per component |
| `linear_velocity` (3 × 23) | 69 | 4194305 values per axis |
| `has_flags` | 1 | |
| `flags` | 4 | one bit per declared flag |
| `team` | 2 | 3 wire values |
| `health` | 10 | `[0, 1000]` is 1001 values |
| `thrust` | 7 | `[0, 100]` is 101 values |
| **total** | **219 bits = 28 bytes** | |

Nothing is spent identifying a field, because both sides were generated from
the same schema. `health` is 10 bits because it was declared `[min = 0, max =
1000]`, not because anyone hand-packed it.

## What the others are buying

The gap is not waste. Each format is spending those bytes on something schema
does not offer, and if you need that thing the price is fair.

**Protobuf — 56 bytes — buys field-number evolution.** Every field carries a
tag, so old readers skip unknown fields and missing fields take defaults. That
is why Protobuf is right for service APIs that version independently over
years. schema's message wire has no tags at all; it has a protocol id and the
rule that both peers deploy together.

**FlatBuffers — 72 bytes — buys zero-copy access.** The root offset, vtable and
alignment padding are what make reading a field without parsing the buffer
possible. If you `mmap` a large asset and touch three fields, FlatBuffers wins
outright and this comparison is the wrong one to be reading.

**Cap'n Proto — 52 packed, 96 unpacked — buys the same in-place model**, plus an
RPC system schema has no answer to. Its packed encoding is a general-purpose
zero-suppression pass over the unpacked layout; it does well here (52) and would
do dramatically better on a mostly-zero message. Note that packing costs a
compression pass, so the 52 is not free the way the 96 is.

schema wins this table by knowing the bounds. `health` cannot exceed 1000, so
it is 10 bits — no general-purpose format can make that inference, because you
never told it. That is the whole trick, and it stops working the moment your
values are unbounded.

## Reproducing this

```bash
./comparison/measure.sh
```

Needs `protoc`, `capnp` and `flatc` on `PATH`, a C++ compiler, the
[serialize](https://github.com/mas-bandwidth/serialize) runtime as a sibling
checkout, and `make` run once at the repository root to emit the generated C++.

## What this does and does not show

It shows the wire cost of one gameplay message, which is the case schema is
built for: small, highly-constrained values sent at high frequency.

It does not show throughput — see [PERFORMANCE.md](PERFORMANCE.md) for that —
and it does not generalise. A message of strings and unconstrained 64-bit
integers would narrow the gap sharply, and a mostly-zero message would favour
Cap'n Proto's packing far more than it favours schema. Bit-packing wins where
values are *bounded*, and bounds are what a game's data has and a
general-purpose format cannot assume.

## A note on an earlier version of this file

An earlier revision listed Protobuf at 50 bytes and FlatBuffers at 68. The
Protobuf figure was arithmetic over the wire spec rather than a measurement,
and it was wrong — the real encoder produces 56. The FlatBuffers figure came
from a slightly different schema shape. Both are now measured by the committed
script, which is why the script is committed.
