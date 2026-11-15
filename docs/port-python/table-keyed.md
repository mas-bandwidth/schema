## THE TABLE KEYED ARRAYS

- **`[E]T` rides through `kind_keyed.py`** (SPEC-TABLES §2.4, §3.2) for
  elements that are bools, integers, fixed-point or tables by value; any other
  element keeps `kind_unsupported.py`. The field's descriptor carries `elem`,
  the element's own descriptor, and `keys`, the key variants' wire ids in slot
  order. The write emits present slots in ascending variant order, elides a
  default slot (a table slot whose body is only its terminator) and writes no
  field when no slot is present; each slot's key id is used before its
  element's ids, and an elided slot's ids are taken back, so the id table is
  the engine's byte for byte. The read places each triple by its key's id,
  skips and counts a key it cannot name (checking a scalar slot's width), and
  refuses a key reference of 0 or past the id table, a repeated key, an
  element kind off the keyed list, a different element kind, and any byte left
  inside a slot's or the body's L. Slot order is not judged on read: the Go
  engine accepts any order, and the verdict is the engine's. A container kind
  that holds tables by value answers `tables()`, and `save.py` validates and
  numbers the pointers of those tables in place. The keyed array keeps the
  descriptor walk, as every construct after the frame does.
- **A table field's id is used before its body's.** The header rides first,
  so the id table's first-use order is the field's id, then the ids inside the
  body, as the Go engine writes it; a nested table holding fields saves to
  the engine's bytes.
