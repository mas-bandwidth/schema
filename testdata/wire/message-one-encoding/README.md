# Message form: one encoding for each value

This fixture holds the message form's canonical spelling and the three bodies a
conforming writer cannot emit (docs/SPEC-TABLES.md §3.3). The canonical, explicit-default and union-none batches encode `One` with
`mode` at its declared default `7`, `count` `5`, and `pick` at `None`. The
field-order case uses `mode` `8`, so it has no stored-default spelling.

`test/message-one-encoding/One.schema` is the unit. The announcement and the
canonical batch are written by `bin/schema pack --root One --message
--announce announcement.bin --out canonical.bin <tree> test/message-one-encoding`
from a tree holding `{"count": 5}`. The field-order case is constructed by `TestMessageFieldOrderVectorPinned`;
the other cases are derived from the canonical batch at the bit level.

| file | what it contains | verdict |
|---|---|---|
| `announcement.bin` | the unit's announcement: build version and message vocabulary | read |
| `canonical.bin` | the canonical batch of the value: `count` only, the default-valued `mode` and the `None` union elided | read |
| `explicit-default.bin` | the same value with `mode` written at its declared default `7` | refused |
| `fields-reordered.bin` | `count` before `mode`, with `mode` set to non-default `8` | refused |
| `union-none.bin` | the same value with the `pick` field written with arm `0` (`None`) | refused |

The verdict is the contract (docs/rulings.json `message-one-encoding`); the
refusal's reason is per-language diagnostic detail.
