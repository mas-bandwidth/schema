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

Historical declaration facts come from the writer. Version-9 locks retain each
entry's recorded digest scheme, complete writer snapshot and exact wire key.
The compiler passes those facts through `GenerateLineage`; Python recomputes
the snapshot's digest under that recorded scheme, compares every digest byte,
verifies layout and whole-record size, and hashes exactly the stored layout
and digest. A current hasher never rewrites an older key. Prospective version-2
definition digests bind full-width bounds, field paths and count minima.

Every supported historical entry needs a checked writer snapshot. An empty
legacy digest is not proof that the writer had no constraints. Missing facts
hold generation with a diagnostic naming the root and key; the lock command's
`--writer` and `--from` import supplies explicit historical source. A genuine
older layout-only key whose digest omitted facts requires the separate
`--legacy-layout-only` trust decision. That marker is rejected for a nonempty
prospective digest, and does not authorize changing the old layout or key.

`make python-fixed-identity-negative-control` reads actual pre-v2 writer files,
widens a 128-bit bound through the public lock and generation commands, and
runs both interpreter pins. Clean old data remains readable; the captured old
writer's bounds still reject a forgery. A prospective wider writer emits a
new key and bytes equal to a live C++ writer. Generated checks widened to the
reader's bounds and a writer changed back to the old key must each fail their
named probe. The same gate covers explicit layout-only source import and
refusal of unknown, erased or invalidly trusted digest schemes.

Older collisions remain an information limit. The historical fixtures include
a lawful larger writer's file that is byte-identical to a smaller writer's
forgery. With the smaller writer snapshot configured, both identical inputs
refuse. Source provenance records which contract a legacy key serves; it
cannot recover the creator of indistinguishable bytes. New version-2 keys
prevent that particular loss of full-width bounds and field attribution.

Unknown and retired layout diagnostics include the offending fixed-width
`0x` hash while leaving caller reports untouched. Deprecated enum variants
read as the reader field or element's declared default, including array and
active union payload values, without incrementing `unknown`.

A missing field inside an active union arm, present optional or live counted
array element is prefilled from its own declared default image under the
writer's guard chain. The root's inactive storage is not a substitute: its
zero bytes would otherwise erase the active child's newly added defaults.
These precomputed guarded fills are counted in `fixed_plan_max` and obey the
same caller plan capacity as the copy and widening operations.

`make python-fixed-form-proof` connects the envelope, bounded batch, plan
selection, compiled-plan, definition-hash, closure, live-extent, writing and
round-trip rows to the shared C++ corpus. `make python-fixed-form-bench`
first compares an eight-byte, two-int32 record against a live C++ writer,
then measures 5000 calls in each of five samples after 100 warmup calls,
with collection disabled during the samples. Median wall-clock times on
Linux, in microseconds per one-record file:

| Interpreter | FixedLoad | FixedSave |
|---|---:|---:|
| CPython 3.11.18 | 15.69 | 6.33 |
| CPython 3.15.2 | 14.19 | 7.23 |

These measure the complete checked file operation, including allocation,
envelope handling and the bounds pass. They are observations for this small
fixture, rather than throughput guarantees for other record shapes.

`make python-fixed-form-conformance` answers every `write-refusal-fixed`
recipe through the complete generated conformance unit under both pins. It
requires both refused inputs and lawful boundaries to match the manifest; a
missing generated root fails. Its negative control corrupts the real driver's
refusal answer and requires the manifest assertion to fail by name.
