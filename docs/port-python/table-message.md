# Python table messages

The message form carries a batch of one to 256 values of one declared fixed
table. The generated package exposes `ANNOUNCEMENT`, `MessageVocabulary`,
`announce_read`, `save_messages`, `measure_messages`, and `load_messages`.
A connection accepts one announcement; a refused first announcement remains
terminal. The optional strict vocabulary rejects a different build version.
A save validates the batch before producing bytes, and a read returns the
whole batch or raises `Refused`. `load_messages` takes a capacity and an
optional `LoadReport`. A root declared with `integrity = "crc32c"` seals
the complete batch with one CRC-32C trailer, checks it before decoding, and
includes the trailer in the measured size.

The announcement, field slots, and payload shapes are compiler-settled facts.
The writer emits one continuous low-bit-first stream and verifies or writes
zero alignment padding where the wire requires it. On a read, a sender's
announced shape supplies the payload width; the reader applies its own bounds.
A same-build field's writer default and declaration order are inferred only
when its full announced shape also matches. A differing shape is read under
its own framing, including for older peers whose build versions did not
encode a counted array's minimum.
Unknown enum variants, deprecated enum variants, unknown union arms, and
unknown keyed slots can substitute defaults during evolution. The reader keeps
that distinct from a sender explicitly writing a default, including inside a
nested body or keyed element.

## One encoding for each value

A read refuses a batch no conforming writer emits (SPEC-TABLES §3.3,
`docs/rulings.json` `message-one-encoding` and `reads-refuse-non-canonical`).
Under the reader's own build version it refuses a field the record does not
declare; under the same build version and the same announced field shape it
refuses fields out of the writer's declaration order and a field stored at the
default the writer elides. At every build version it refuses a union field
written with arm reference 0, on the read and on the skip path alike, while a
None element of an array of unions reads. Under another build version order
and defaults are not judged, and an undeclared field is skipped and counted.
`TestPythonMessageReadsRefuseNonCanonical` holds every message and
announcement row of `testdata/wire/non-canonical/MANIFEST.tsv` to the Go
engine's verdict under the announcement as written and under another build;
its negative control removes each refusal from the generated runtime in turn
and requires the gate to go red on the row that refusal holds.

The order and default checks already require the field's full announced shape
to match the reader's own, which the spec states and the engine does not yet
check on its own; on the shared vectors the shapes match and the two agree.

The package retains a schema's existing public export if its name overlaps a
message API spelling. In that case the complete message API is available
from `package._schema._message_unit`.

## Writer shape

The root writer is emitted as a straight-line field walk with settled slot
numbers; nested payloads use descriptor walks. For a two-field body, including
whole-value validation and final bytes, a Linux benchmark using the minimum of
five 3,000-call samples measured the descriptor writer at 13,732 ns versus the
emitted root writer at 12,870 ns on CPython 3.15.2, and 14,112 ns versus
13,142 ns on CPython 3.11.18. Both shapes produced identical bytes. These are
single-case measurements, not a claim about every schema.
