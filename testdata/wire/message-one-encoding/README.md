# Message form: one encoding for each value

This fixture holds the message form's canonical spelling and the three bodies a
conforming writer cannot emit (docs/SPEC-TABLES.md §3.3). Every batch encodes
the same value of `One`: `mode` at its declared default `7`, `count` `5`, and
`pick` at `None`.

`test/message-one-encoding/One.schema` is the unit. The announcement and the
canonical batch are written by `bin/schema pack --root One --message
--announce announcement.bin --out canonical.bin <tree> test/message-one-encoding`
from a tree holding `{"count": 5}`. Each case is that canonical batch edited by
hand at the bit level, and each still decodes to the same value on a reader that
does not refuse non-canonical bodies.

| file | what it contains | verdict |
|---|---|---|
| `announcement.bin` | the unit's announcement: build version and message vocabulary | read |
| `canonical.bin` | the canonical batch of the value: `count` only, the default-valued `mode` and the `None` union elided | read |
| `explicit-default.bin` | the same value with `mode` written at its declared default `7` | refused |
| `fields-reordered.bin` | the same value with the two integer fields written in the other order (`count` before `mode`) | refused |
| `union-none.bin` | the same value with the `pick` field written with arm `0` (`None`) | refused |

The verdict is the contract (docs/rulings.json `message-one-encoding`); the
refusal's reason is per-language diagnostic detail.
