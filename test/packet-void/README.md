# Payload-free packet union arms

`make packet-void-cpp packet-void-c` checks SPEC §4.8 in C++ and C. This is
packet wire coverage for #503; the older payload-free table conformance rows
exercise a different encoding.

The reader inputs below are literal constants, calculated from the spec.
They are never produced by the writer under test and have no golden updater.
The writer must independently match both their bytes and exact bit counts.
Bit fields are packed least-significant-bit first without alignment here.

| Value | Calculation | Byte | Bits |
|---|---|---|---|
| Mixed None | tag 0 | `00` | 2 |
| Mixed Ping | tag 1 | `01` | 2 |
| Mixed Payload, no entries, marker 5 | `2 + (0 << 2) + (5 << 4)` | `52` | 7 |
| Frame, lead 5, Ping, tail 6 | `5 + (1 << 3) + (6 << 5)` | `CD` | 8 |
| Batch, one Ping | `1 + (1 << 2)` | `05` | 4 |
| Tags Pong, all arms void | tag 2 | `02` | 2 |
| Single Ack | tag 1 | `01` | 1 |
| Empty None, no declared arms | no bits | none | 0 |

The mixed union deliberately declares its void arm first. The harnesses also
check tag counts/debug names, maximum bit sizes, fresh payload defaults on
repeated selection, invalid tags and missing tag input. They never inspect
inactive C/C++ payload storage. Runtime buffers have the required aligned,
padded allocations while the logical input lengths remain exact.

Four named `tools/sabotage` controls add one bit to a void arm on just the
write or read path in one carrier. Each compiler and consumer must build,
the unaffected direction must pass, and the altered direction must fail on
its exact tag-bit-count assertion. A failed read or wrong tag exits before
that assertion. The controls are dependencies of `make test` (C++) and
`make test-c` (C), together with the positive checks.
