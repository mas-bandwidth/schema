# packet-void: the shared case file

SPEC §4.8: a payload-free union arm costs exactly its tag bits, and a
selected payload is constructed with its declared defaults before decoding.
`cases.txt` names every case once; every leg's driver under
`test/packet-void/<leg>/` reads it and answers each case, and
`tools/casegate` (`internal/casegate` for the Go-test legs python and
lobster) refuses a case with no answer. `owed.txt` is the exact-set ledger
of cases a leg does not answer yet. The schema is
`test/packet-void/Void.schema`.

## The pins

Each case's wire is `<case>.bin`. The accept pins are the Go oracle's bytes:
`make packet-void-pins` writes them from the generated Go writer (and
`make packet-void-go` then compares the checked-in bytes and bit counts).
They match the values the family once carried as hand-calculated constants,
bit fields packed least-significant-bit first without alignment:

| Case | Calculation | Bytes | Bits |
|---|---|---|---|
| mixed-none | tag 0 | `00` | 2 |
| mixed-ping | tag 1 | `01` | 2 |
| mixed-payload | `2 + (0 << 2) + (5 << 4)` | `52` | 7 |
| tags-none, tags-ping, tags-pong | tag 0, 1, 2 | `00`, `01`, `02` | 2 |
| single-none, single-ack | tag 0, 1 | `00`, `01` | 1 |
| empty-none | no bits | none | 0 |
| frame-ping | `5 + (1 << 3) + (6 << 5)` | `cd` | 8 |
| batch-empty | count 0 | `00` | 2 |
| batch-ping | `1 + (1 << 2)` | `05` | 4 |
| batch-ping-payload | `2 + (1 << 2) + (0x52 << 4)` | `26 05` | 11 |

The refuse pins cannot come from a writer; they are hand-made:

| Case | Bytes | Why the one-packet read refuses |
|---|---|---|
| mixed-tag-3 | `03` | tag 3 is above MixedTypeMax 2 |
| tags-tag-3 | `03` | tag 3 is above TagsTypeMax 2 |
| mixed-missing, single-missing, frame-missing | none | no tag bits |
| mixed-padding | `05` | ping, then a set bit in the final byte's padding |
| mixed-trailing | `01 00` | ping, then a whole byte after the packet |
| empty-trailing | `00` | a whole byte after a value of no bits |
| mixed-count-3 | `0e` | payload entries count 3 is above `[..2]` |
| mixed-short | `0a` | payload entries count 2, and the entries cut off |
| batch-count-3 | `03` | values count 3 is above `[..2]` |
