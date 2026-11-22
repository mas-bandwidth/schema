## THE TABLE ID TABLE

- **Enums ride the id-table wire.** The runtime carries the id-table wire's
  ENUM kind (30): a table enum is the generated class of int constants, the
  field's descriptor holds its variants' wire ids by ordinal (`variant_ids`,
  shared with evolution's `was` renames) and the two dicts built from them once
  at import (value to wire id, wire id to value), and the wire
  carries the variant name's hash as a reference (SPEC-TABLES §3, §5).
  `kind_enum.py` owns the codec.
- **A skipped field is still validated where the engine validates it.** An
  unknown enum field's reference, and each element of an unknown array of
  enums, must name an id-table entry, or the read refuses `wire_damaged`.
- **The escape and payload-free kinds.** The ESCAPE (31) and PAYLOAD-FREE (32)
  kinds are framed by a field the reader cannot name: the escape is skipped
  by its `L`, the payload-free kind must have `L = 0`, and both count
  `unknown`. A field the reader can name that rides under either kind is the
  ordinary kind mismatch.
- **Non-canonical id tables are refused, as the engine refuses them.** The
  reader refuses an id table that holds an unused entry or one whose entries
  are out of first-use order with `Refused("wire_damaged")`, which is what the
  Go engine does (`internal/tablewire/wirecheck.go`, `firstUseMatches`):
  SPEC-TABLES lists both as non-canonical spellings to refuse.
  `TestPythonIDOrder` holds both to that refusal, on the reordered and unused
  spellings, and on a structurally mismatched field beside them (the id damage
  is reported first).

### The roadmap row over the manifest

`TestPythonVtIdtableRows` (make `python-vt-idtable-rows`) holds the
id-table-wire row over the twelve conformance-manifest cases luatable's
`vtcells_test.go` names for it, under both pins. Every instance root
(`vocab_wide`, `vocab_low`, `peer_a`, `peer_b`, `vocab9_wide`, `vocab9_low`,
`login_full`, `match_full`) is a fixed table, so its plain `load` and `save`
are form 3, as luatable reads a declared fixed root through `ReadFixed`: each
reference wire loads, saves back byte for byte, and measures its length. The
four report rows (`escape_unnamed_field`, `escape_named_field`,
`escape_element`, `nopayload_named_field`) are form-1 wires of the fixed root
`ToolMessage`: the plain load refuses them `previous_form`, as the Go engine's
`Decode` does, and the variable reader (`load_table`), where the held kind 31
and the payload-free kind 32 are judged, answers the engine's `DecodeVariable` verdict on
the same bytes. Kind 31 is held by name and never written (R17: the newer
reader reads old data, never the other way), so every escape row refuses
`wire_damaged`, at a field the reader cannot name as at one it declares, and
`nopayload_named_field` refuses `kind_mismatch`. `TestPythonVtIdtableRowsNegativeControl` plants, in a copy
of the runtime, the fixed save's layout hash one bit off and the fixed read's
record-hash test inverted (every instance case red under each), and the
variable read skipping an unnamed kind 31 by its L as the removed escape did
(`escape_unnamed_field` red).

### Enum lookup bench

The enum codec maps a value to its variant's wire id on save and a wire id to
its value on load. Two shapes: a scan of the field's variant wire ids
per field, and a dict built once in the field descriptor. `TestPythonEnumLookupBench`
runs both on a two-field table over a three-variant enum (`pair`) and an
eight-field table over a sixteen-variant enum with values spread across it
(`many`), under both pins, on a shared fleet host (other work was running on
it, so the figures carry its noise). Each figure is the minimum of five rounds
of 10,000 iterations after warm-up, in nanoseconds per operation, from the
quieter of two sittings:

| operation | 3.15.2 dict | 3.15.2 scan | 3.11.18 dict | 3.11.18 scan |
|---|---:|---:|---:|---:|
| pair save | 3,585 | 4,169 | 3,331 | 4,033 |
| pair load | 3,818 | 3,843 | 4,182 | 4,188 |
| many save | 10,141 | 13,953 | 9,011 | 13,657 |
| many load | 10,299 | 11,321 | 10,681 | 11,531 |

The dict save is 14–17% faster on `pair` and 27–34% faster on `many` in this
sitting, and 10–22% and 19–29% in the second, on both pins: the scan was the
save's cost, because the validation and the write each walked the variants.
The dict load is level on `pair` and 7–9% faster on `many` in this sitting; in
the second, busier sitting it was 1–4% faster except `many` on 3.11.18, where
it was 12% slower, within that sitting's noise (its figures ran 25–65% above
these). The dict is kept.
