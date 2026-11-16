# Python fixed-form files

The generated table classes expose `fixed_save(values)`, `fixed_load(data,
capacity=4096, report=None, plan_capacity=None)`, `fixed_measure(count)` and
`fixed_plan_max`. `fixed_load` returns the whole batch or raises `Refused`;
`fixed_save` returns bytes or raises `InvalidValue`. A file holds at least one
record. The default capacity is 4096; a caller can supply a larger bound.
`fixed_plan_max` is the exact largest plan emitted for the supported lineage,
and an explicitly smaller `plan_capacity` refuses a plan that does not fit.

For a root declared `fixed table`, the ordinary `load`, `save` and `measure`
methods use form 3 with one record. A fixed class used by value in another
root keeps that root's framing. A root beyond the fixed form's admission
bounds refuses its file methods rather than selecting form 1.

The schema compiler emits the layout bytes, definition hash, whole record
sizes and image-domain plans as static data in `_fixed.py`. Its own plan is
one whole-body copy. Locked older layouts are matched at generation, with
unguarded operations before guarded operations and copy coalescing inside
each half. The runtime selects a known hash and compares its layout bytes;
it never parses a file's layout as executable data.

Every record hash, live value and dead byte is checked before any record is
constructed or a caller's report changes. The writer starts each record as a
zeroed image and stores only its live values. Text is strict UTF-8 without
NUL, floats are finite, and a compressed float's negative zero is positive
zero. The fixed-form runtime is independent of the form-1 field codecs.

The source gate is `make python-fixed-form`, with a live C++ byte oracle,
both interpreter pins and the strict analyzer. `make
python-fixed-form-negative-control` plants a broken file discriminator and a
changed writer byte in copies of the real generated runtime; each must turn
its probe red by name. The shared proof runner is in
`internal/codegen/pythontable/fixed_proof_suite_test.go`; row files supply
constructors and assertions without creating another interpreter harness.

Historical declaration facts must come from the writer, rather than the
reader's widened bounds. `GenerateLineage` accepts an explicit `Writer`
snapshot and verifies its layout and hash before baking its bounds checks.
An entry whose hash names historical definition facts but has no such snapshot
is currently refused as `layout_unsupported`. The existing lock format does
not retain complete historical snapshots; that limitation still holds full
backward-reading acceptance. No roadmap completion is implied by the core
byte-oracle gate.
