## THE TABLE PACKET TYPES

- **A packet type is carried by value.** A table field, an array element, a
  union arm or a map's value whose type is a packet `type` is carried on the
  form-1 table wire as SPEC-TABLES states for a type by value: the field's
  descriptor names the packet class, the field rides kind 13, and an array of
  the type rides kind 14 with element kind 13 (`kind_array.py`) while a union
  arm that holds it rides kind 15 (`kind_union.py`); a map's value of the type
  is the map entry's value field, the same kind-13 body (`kind_map.py`,
  `TestPythonTablePacketTypesMapValue`). A packet-type array element and a
  packet-type union arm are carried. The class itself is the packet core's
  own generated class and codec, hosted in the same per-file module
  (`python.Host`), and the runtime ships the packet core's serialize.py beside
  the table runtime so that class's `read`/`write` still work. The table
  descriptor is the one the table walk reads; it is registered by name only,
  because a packet type by value never rides the node table's type ids.
  A packet type whose fields are float or fixed-point kinds, which the packet
  wire carries, is generated, imports and passes the analyzer, and its own
  `read`/`write` work; on the table wire its body walk refuses those fields by
  name through `kind_unsupported.py`, the whole value or nothing, until the
  table runtime carries floats (`TestPythonTablePacketTypesFloatKinds`).
- **Nothing the unit declares is dropped.** In a unit with tables, every
  packet type and packet union is generated, reached by a table or not,
  through the same `python.Host`: the class and codec the packet leg emits
  for a packet-only unit, in its file's module beside the tables, with no
  second runtime. Only the ones a table reaches carry a table descriptor.
- **An uncarried packet kind is refused by name.** A packet type or packet
  union of the unit, reached by a table or not, whose own fields use a kind
  the PACKET wire does not carry (a `string`, `bytes`, `bits`, an array,
  ...) is refused at generate time, naming the type, the field, the kind and
  P4 the packet wire. It is never a blanket refusal of the unit, so a unit is
  refused only for the packet kind that is still missing.
