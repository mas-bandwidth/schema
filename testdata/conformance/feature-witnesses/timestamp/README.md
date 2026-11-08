# Timestamp witnesses

Bare table fields only, per SPEC-TABLES §2.11. Cases pin strict RFC3339 date and offset grammar, nanosecond truncation, negative time, and both signed 64-bit limits. Invalid strings refuse; wrong JSON shapes count kind mismatch. Native consumers also check kind36 does not widen from integer kind5 and fixed/message/cook forms preserve all 64 bits.
