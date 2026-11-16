# Enum values in form-1 keyed arrays

Python form-1 table fields keyed by an enum can carry enum values, including
fields hosted in packet types. The generated keyed wrapper and hosted packet
list use the same positional enum ordinals. The writer elides default slots and
encodes nondefault values using their wire IDs; the reader maps known IDs back
to ordinals and treats unknown or deprecated values according to table evolution
rules. Validation refuses deprecated or invalid enum values before writing.

The keyed enum tests compare generated Python output against the Go engine and
generated C++ for default, sparse, full, and hosted arrays. They also cover
unknown and deprecated IDs, invalid references, and validation failures on both
pinned Python versions. The registered negative control mutates generated
metadata or runtime behavior and requires the corresponding probes to fail.
