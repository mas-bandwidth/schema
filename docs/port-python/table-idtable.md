## THE TABLE ID TABLE

- **Enums ride the id-table wire.** The runtime carries the id-table wire's
  ENUM kind (30): a table enum is the generated class of int constants, the
  field's descriptor holds its `(value, wire id)` pairs and the two dicts built
  from them once at import (value to wire id, wire id to value), and the wire
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
- **Non-canonical id tables read as the engine reads them.** The reader
  accepts an id table that holds an unused entry or entries out of first-use
  order, as the Go engine does today: SPEC-TABLES lists both as non-canonical
  spellings to refuse, and that refusal reaches this leg through its own card
  once the engine refuses them.

### Enum lookup bench

The enum codec maps a value to its variant's wire id on save and a wire id to
its value on load. Two shapes: a scan of the field's `(value, wire id)` pairs
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
