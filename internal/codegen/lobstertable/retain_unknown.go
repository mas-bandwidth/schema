package lobstertable

// RETAIN-UNKNOWN (docs/SPEC-TABLES.md §6.6; docs/PORT-LOBSTER.md, THE LOAD)
// is not carried in this checkout, and this card stops HOLD rather than claim
// it. The reason is that neither landed walker exposes the per-kind hook this
// group joins, so a runtime file here would be unreachable dead code.
//
// THE LOAD. runtime/load.lobster's `body_at` is the one place an unknown field
// id reaches the wire: its `if not found:` branch (load.lobster:1472-1477)
// counts `unknown` and calls `skip`, so the payload is walked past and never
// offered to a construct runtime. There is no dispatch key for an id no
// descriptor names; the Shape registry is consulted only after `slot_by_id`
// succeeds.
//
// THE SAVE. runtime/schema_save.lobster's `encode_body_fields` (line 695)
// walks the declared `Record.fields` only, and `encode_body` appends the
// terminator; there is no call after the known fields where the retained tail
// can be written. `schema_save.lobster` names the Shape registry in its header
// comment but never calls it, so the per-kind hook the header promises does
// not exist on this side.
//
// THE SMALLEST HOOK THAT WORKS. Two one-line calls are not enough on their
// own, because a construct runtime in its own module cannot be named from the
// walkers without an import cycle (retain_unknown imports load and
// schema_save; load cannot import retain_unknown). The hook therefore has to
// be a registration the walkers own:
//
//   1. load.lobster: `class RetainCapture` with `def capture(r:Reader,
//      rec:Record, id:int, kind:int) -> bool`, a `let retain_capture:
//      RetainCapture? = nil`, and `def register_retain_capture(h:RetainCapture)`.
//      In `body_at`'s unknown branch: `if retain_capture and
//      retain_capture.capture(r, rec, id, kind): continue` before the
//      `rep.unknown++`/`skip` pair.
//   2. schema_save.lobster: `class RetainTail` with `def tail(e:SaveEncoder,
//      w:SaveOut, rec:Record)`, a `let retain_tail:RetainTail? = nil`, and
//      `def register_retain_tail(h:RetainTail)`. At the end of
//      `encode_body_fields`: `if retain_tail: retain_tail.tail(e, w, rec)`
//      (after every declared field, before `encode_body`'s zero reference).
//
// Both registries live in the files that define the types their signatures
// name (Reader in load, SaveEncoder in schema_save), so retain_unknown.lobster
// imports both and registers its two classes from module-level `let`s; no
// cycle is introduced. This is the change a dependency card must land before
// retain_unknown.go, runtime/retain_unknown.lobster and
// TestLobsterRetainUnknown can be written here.
