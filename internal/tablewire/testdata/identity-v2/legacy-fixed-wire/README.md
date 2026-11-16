# Retained pre-v2 fixed files

These 60 original owned conformance fixtures were retained byte-for-byte before
regenerating the current `testdata/conformance/tables/wire/` pins for the
prospective WireLaw 3 identity epoch. They are historical writer evidence, not
new writes with their hashes reassigned.

The migration reads each original with a checked writer snapshot and writes
its value through the current reference encoder. Existing corpus locks supply
the strictly imported historical snapshots. Unlocked one-file fixtures use
explicitly archived checked source from pre-v2 main
`68a273746bfd60abec07397761d8af0184fcd1f0` in temporary owned-fixture locks;
layout, record size, recorded v1 digest/key and unchanged declaration projection
must match. No layout-only trust option or reader-derived bound fallback is used.

`TestFixedIdentityV2CorpusPreservesOldBodies` checks that current pins differ
only in the header and record keys: reserved bytes, complete layout, file size
and every record body remain identical. Separate lock and reader tests cover
strict imports and historical writer-bound validation. Retaining these bytes
does not make irreducibly colliding legacy files distinguishable.
