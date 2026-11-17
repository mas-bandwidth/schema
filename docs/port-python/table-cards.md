## Cards

`tables/cards/python/facade.py` owns a card record as its original immutable
wire, the decoded `Card`, and the read report. `load`, `read_strict`, `from_json`, `parse`,
`render`, `to_json`, `upgrade`, `task_hash`, and the `set_*` edits are the
customer surface. A failed edit leaves the original record untouched. The
facade enforces the 65,536-byte body and 512-entry collection ceilings before
publishing bytes. An edit or task hash refuses a record with unknown data;
upgrade uses the retained-field reader and writer and refuses retention loss.

`tables/cards/python/driver` is the conformance harness adapter. Its view uses
the generated public `UnitView`, and the nine surfaces are checked against the
shared card corpus. `TestPythonCards` drives both pinned interpreters and checks
mypy on the generated package, runtime and facade. The C++ and Go checks compare
independent wire and JSON outputs with the card pins. The facade control damages
its body handling in a copy; the render, upgrade and hash controls each damage
one answer and require that surface to fail while the other eight stay green.
