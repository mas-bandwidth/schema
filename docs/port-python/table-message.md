# Python table messages

The message form carries a batch of one to 256 values of one declared fixed
table. The generated package exposes `ANNOUNCEMENT`, `MessageVocabulary`,
`announce_read`, `save_messages`, `measure_messages`, and `load_messages`.
A connection accepts one announcement; a refused first announcement remains
terminal. The optional strict vocabulary rejects a different build version.
A save validates the batch before producing bytes, and a read returns the
whole batch or raises `Refused`. `load_messages` takes a capacity and an
optional `LoadReport`.

The announcement, field slots, and payload shapes are compiler-settled facts.
The writer emits one continuous low-bit-first stream and verifies or writes
zero alignment padding where the wire requires it. On a read, a sender's
announced shape supplies the payload width; the reader applies its own bounds.
A same-build field's writer default and declaration order are inferred only
when its full announced shape also matches. A differing shape is read under
its own framing, including for older peers whose build versions did not
encode a counted array's minimum.

The package retains a schema's existing public export if its name overlaps a
message API spelling. In that case the complete message API is available
from `package._schema._message_unit`.

## Writer shape

The root writer is emitted as a straight-line field walk with settled slot
numbers; nested payloads use descriptor walks. For a two-field body, including
whole-value validation and final bytes, a Linux benchmark using the minimum of
five 3,000-call samples measured the descriptor writer at 13,283 ns versus the
emitted root writer at 12,408 ns on CPython 3.15.2, and 12,842 ns versus
11,897 ns on CPython 3.11.18. Both shapes produced identical bytes. These are
single-case measurements, not a claim about every schema.
