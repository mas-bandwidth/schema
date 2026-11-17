## THE PACKET UNIONS

- **Unions have their own packet-void gate.** The union kind is carried, so
  the golden harness never skips a root for it, and `TestPythonPacketUnions`
  holds the leg's half of `test/packet-void`:
  `test/packet-void/python/Python.schema` keeps `Void.schema`'s union shapes
  in the kinds the core carries (its bit fields and arrays are later cards),
  and `test/packet-void/python/goldens.txt` is read and written back byte for
  byte under both pins, mypy --strict over the tree. A payload-free arm spends
  exactly its tag bits, a selected payload is rebuilt at its declared defaults
  before decoding, every out-of-contract write raises `InvalidValue`, and
  truncated or hostile input raises `Refused`. Beside it, a fixture schema of
  the test's own puts the union alone and beside the core's kinds (an enum,
  flags, integers and a bool in the payloads, a union inside a type and inside
  another union's payload), and the generated Go packet code is the reference:
  Python writes the bytes Go writes, reads them back to an equal value that
  writes back byte-identical, and gives every Go read verdict its own, the
  unused tags Go refuses among them. A read leaves every unselected arm at its
  declared defaults; a write validates and writes the selected arm only, as
  Go does. `TestPythonPacketUnionsNegativeControl` plants one extra set bit
  on a void arm's write, and a tag range that admits the unused tags on a
  read, each in a copy of the generated code, and each probe goes red by name.
