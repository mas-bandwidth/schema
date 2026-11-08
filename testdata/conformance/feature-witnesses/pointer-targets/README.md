# Pointer target and root-closure cases

SPEC-TABLES §3.1 requires an exact target type: recognized table and blob
records of another type read null and count `kind_mismatch`. A node type
outside the selected root's reachable closure is instead `unknown`; its
body is skipped and its index remains reserved. Null names no node. Pointer
arrays preserve their count while mismatched elements read null.

`cases.json` records literal inputs, semantic outputs and report counters.
The Elixir regression runs both ordinary and retaining reads twice, compares
native writer bytes with the independent neutral writer, and checks the
explicit outcomes before accepting the neutral output as a byte/text pin.
The wrong-blob-kind case deliberately contains invalid UTF-8 in a bytes node:
it remains bytes, and must not be decoded as text through another slot.
