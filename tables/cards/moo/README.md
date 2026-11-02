# moo cards

`cards.moo` is the owning card library: `Load`, `FromJSON`, `ToJSON`, `Parse`,
`Render`, `Upgrade`, `ReadStrict`, `TaskHash`, and the `SetNeeds`, `SetPaths`,
`SetTier`, `SetPriority`, `SetColumn`, `SetHolder` edits. It uses the native
moo codecs generated from `tables/cards/Card.schema`.

Generate the unit with `make cards-moo-generate`, then place `cards.moo` and
`cards_codec.moo` beside the generated modules. The pinned moo commit `f60f0239f41c6c0a827ecee44a7f9539c5798068`
provides buffers and `bit32`; the emitted serialize.moo module supplies frozen
64-bit classes; no runtime library is added.

```lua
local cards = require("./cards")
local record, report = cards.FromJSON([=[{"id":"example","tier":"pro","body":"Hello\n"}]=])
assert(record, tostring(report))
local wire = record:Wire() -- immutable byte string
local ok, err = cards.SetHolder(record, "reader")
assert(ok, tostring(err))
local text = cards.ToJSON(record)
```

A refusal returns `nil, CardError`, whose `class` is the shared corpus's refusal
class and whose `report` holds available read counters. Successful reads return
`value, report`. Edits publish the new wire only after validation succeeds.
The facade enforces the 64 KiB body ceiling and 512-entry collection ceilings.
`Upgrade` retains unknown fields; strict reads, task hashing and edits refuse
lossy input. `ToJSON` is a known-field view, not an archival rewrite.

The codec translates descriptor-named generated buffers and enum ordinals into
the facade's internal values. Consumer `int64` fields remain frozen `serialize.Int64`
values throughout. Blob identity is preserved so conversion cannot hide a
shared-blob writer refusal. The retained store comes from the generated runtime and keys occurrences by
decoded object identity. The read-only upgrade keeps those exact objects alive
through its retaining write; it does not rebuild them from normalized values.

Run `make cards-moo-test cards-moo-conformance` for the native unit tests,
analyzer and all 43 shared cases across nine required surfaces. The Go executable
in `host/` only reads manifest inputs, stages modules, invokes the interpreter and
writes its emitted files. Expected answers never enter the conformance driver;
`UNIT_INPUTS.txt` supplies fixtures separately to the unit-test mode.

The three `cards-moo-conformance[-upgrade|-hash]-negative-control` targets copy
and mutate real facade code: renderer separators, retaining save, or hash length
framing. Each requires its own surface red and the other eight green. CI runs
the positive gates before these controls in the `moo-cards` group.
