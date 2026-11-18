# Pointer target and root-closure cases

SPEC-TABLES §3.1 requires an exact target type. A row with `kind_mismatch`
above zero is REFUSED, the whole read, in every build of every leg: a
declared slot names a record of a type the reader can name that is not the
slot's target. The two reserved blob ids are types every reader names,
whatever its root reaches, and so is every table the reader's unit declares;
so a table or blob record outside the selected root's reachable closure,
named from a slot the reader declares, is refused too, not counted
`unknown` (docs/rulings.json `blob-target-outside-closure-refused`,
2026-11-17). Only a record whose type is a table the reader's unit does not
declare (a newer writer's type) reads null and counts `unknown`. Null names
no node, and a row of nulls reads.

`cases.json` records literal inputs, semantic outputs and report counters.
The Elixir regression runs both ordinary and retaining reads twice, compares
native writer bytes with the independent neutral writer, and checks the
explicit outcomes before accepting the neutral output as a byte/text pin.
The wrong-blob-kind case deliberately contains invalid UTF-8 in a bytes node:
it remains bytes, and must not be decoded as text through another slot.
