# Size comparison

The README claims general-purpose formats pay for their generality on the
wire. This is that claim, measured, on one representative message.

**One message is not a benchmark suite.** It is a worked example you can check
line by line, which is worth more than a table of numbers you cannot.

## The message

A ship-spawn packet: type, quantized position, quantized rotation, quantized
velocity, some flags, a team, health and thrust. It is the corpus's
`ShipCreate`, unchanged.

```
message ShipCreate {
    ship_type       ShipType                        // 4 variants
    position        QuantizedPosition               // 3 x int32, ±16.7M units
    rotation        QuantizedRotation               // 4 x int32, ±2048 units
    linear_velocity QuantizedVelocity               // 3 x int32, ±4.2M units
    has_flags       bool
    if has_flags {
        flags       ShipFlags                       // 4 flag bits
    }
    team            Team                            // 2 variants
    health          int16 [min = 0, max = MaxHealth]
    thrust          int8  [min = 0, max = 100]
}
```

## Results

| format | bytes | how it was obtained |
|---|---:|---|
| **schema** | **28** | measured — the emitted `write_bits` widths |
| Protobuf (proto3) | 50 | computed from the wire spec, working shown below |
| FlatBuffers | 68 | measured — `flatc --binary` on the equivalent schema |

Cap'n Proto is absent because its toolchain was not available here. Its packed
encoding would land nearer Protobuf than FlatBuffers; adding a measured number
is welcome.

## Where schema's 28 bytes go

Straight from the generated C++ — every one of these is a `write_bits` call you
can read in `generated/cpp/TypesWire.h`:

| field | bits |
|---|---:|
| `ship_type` | 3 |
| `position` (3 × 25) | 75 |
| `rotation` (4 × 12) | 48 |
| `linear_velocity` (3 × 23) | 69 |
| `has_flags` | 1 |
| `flags` | 4 |
| `team` | 2 |
| `health` | 10 |
| `thrust` | 7 |
| **total** | **219 bits = 28 bytes** |

Nothing is spent identifying a field, because both sides were generated from
the same schema. `health` is 10 bits because it was declared `[min = 0, max =
1000]`, not because anyone hand-packed it.

## Where Protobuf's 50 bytes go

Computed from the proto3 wire format — a tag byte per field (field numbers 1–15
fit one byte), then a varint or a length-delimited submessage. Values are the
same ones used for the FlatBuffers measurement:

| field | bytes | |
|---|---:|---|
| `ship_type` | 2 | tag + varint |
| `position` | 14 | tag + len + 3 × (tag + zigzag varint) |
| `rotation` | 14 | tag + len + 4 × (tag + zigzag varint) |
| `linear_velocity` | 11 | tag + len + 3 × (tag + zigzag varint) |
| `flags` | 2 | tag + varint |
| `team` | 2 | tag + varint |
| `health` | 3 | tag + varint |
| `thrust` | 2 | tag + varint |
| **total** | **50** | |

That overhead is not waste — it is what buys field-number evolution, which is
Protobuf's central feature and something schema's message wire deliberately
does not have. If you need independent client and server versioning, you are
buying something real with those bytes.

## Where FlatBuffers' 68 bytes go

Measured with `flatc --binary` on the closest equivalent: `Vec3i`/`Quat16` as
inline structs (the efficient choice — a table per vector would be far larger),
everything else as table fields. The overhead is the root offset, the vtable,
and alignment padding, which is what makes in-place access without parsing
possible.

Again: not waste. It buys zero-copy reads, which schema does not offer. If you
`mmap` a large asset and touch three fields, FlatBuffers wins outright and this
comparison is the wrong one to be reading.

## Reproducing this

```
# schema — read the widths out of the generated code
schema generate --lang cpp --out /tmp/gen examples
grep -A20 'inline bool WriteShipCreate' /tmp/gen/TypesWire.h

# FlatBuffers
flatc --binary ship.fbs ship.json && wc -c ship.bin
```

The `.fbs` and `.json` used are in this document's history; the Protobuf figure
is arithmetic over the wire spec and every term is in the table above.

## What this does and does not show

It shows the wire cost of one gameplay message, which is the case schema is
built for: small, highly-constrained values sent at high frequency.

It does not show throughput, and it does not generalise. A message of strings
and unconstrained 64-bit integers would narrow the gap sharply — bit-packing
wins where values are *bounded*, and bounds are what a game's data has and a
general-purpose format cannot assume. Nor does it account for what the other
two give you that schema does not: evolution, and zero-copy.
