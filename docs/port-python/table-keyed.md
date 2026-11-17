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
  skips and counts a key it cannot name, walking that slot's element with the
  declaration-free wire check every skipped payload gets (so a damaged table
  slot refuses as the engine refuses it, skipped is not unread), and
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
- **The roadmap row is held over the conformance manifest**
  (`port/python/vt/enum-keyed-arrays`; `TestPythonVtKeyedRows`,
  `make python-vt-keyed-rows`). Every instance and report row whose root
  reaches a keyed array is named, and a completeness check turns red on a
  corpus row that reaches one and is not: 41 rows, among them the keyed demo,
  the rename pair, the scalar state, the versioned config pair and the keyed
  arrays inside map values. Each is read then written under both pinned
  interpreters: an instance saves back byte for byte with its measure its
  length, a report row answers reports.txt's verdict, and a refusal leaves the
  caller's buffer and report as they were. A root declared `fixed table` is
  read as form 3, and each accepted instance at such a root is also read on
  form 1, the engine's variable-form bytes of the same value at the same root
  declared variable, so the keyed body (kind 16) is held on the manifest's
  own data. The map rows' roots (`Depth` and `Slots` in mapdemo) are declared
  `table`, so they are read in their declared form 1 and get no separate
  copy. luatable's construct for
  the row (`unions_maps_keyed`) rides beside them. The manifest found no gap
  in `kind_keyed.py`. `TestPythonVtKeyedRowsNegativeControl`
  (`make python-vt-keyed-rows-negative-control`) writes the variable form's
  slots in descending order, then reads the fixed form's keyed slots in
  reverse, in a copy of the runtime; each turns the rows with two or more
  distinct present slots on its form red by name.
