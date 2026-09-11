# The rows that are owed: every pair nobody probes yet

This is the LEDGER behind `compiler/everyrow_test.go`'s `TestEveryRowEveryLeg`.
`docs/FIXED-FORM-VERSIONING-TESTS.md` lists the fixed form's versioning edge cases, one per row; every
row with a NEW-READS-OLD column is owed a probe BY NAME in each of the nine legs' versioning harnesses.
The gate fails naming every (row, leg) pair nobody probes — and every pair below is one it is allowed to
name, for now, because somebody owes it.

**This file can only shrink.** The gate fails if a line here is for a pair that IS probed, for a row the
page does not have, for a leg outside the nine (the one other target is `corpus`, below), or for a row the
page marks LOCK-only with a `—` in its NEW-READS-OLD column. A row that lands on a leg is one line deleted.
A row ADDED to the page is owed on nine legs the moment it is written, and the gate says so the same minute.
**Nothing is added here without an owner**: the PR or the card that will probe it, or the word `unassigned`,
which is a standing ask for whoever reads this next to take the row.

The `leg` column takes one of the nine — `cpp c go rust dart js java cs elixir` — or `corpus`, which is the
second gate's: `TestEveryRowHasCorpusBytes` wants a `row=` for the row in `build/fixedform-corpus/manifest.txt`,
and a `corpus` line here says the dump does not write that pair yet.

| row | leg | owed by | since |
|---|---|---|---|
| `array_bounded_grow` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `array_elem_widen` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `array_fixed_grow` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `bits_grow` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `bytes_grow` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `constant_grow` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `enum_append` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `enum_width` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `field_append` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `field_deprecate` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `fixed_I_grow` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `fixed_I_grow_element` | cpp | unassigned — `fixed_I_grow_element` was written into the page with no card | 2026-09-11 |
| `fixed_I_grow_element` | c | unassigned — `fixed_I_grow_element` was written into the page with no card | 2026-09-11 |
| `fixed_I_grow_element` | go | unassigned — `fixed_I_grow_element` was written into the page with no card | 2026-09-11 |
| `fixed_I_grow_element` | rust | unassigned — `fixed_I_grow_element` was written into the page with no card | 2026-09-11 |
| `fixed_I_grow_element` | dart | unassigned — `fixed_I_grow_element` was written into the page with no card | 2026-09-11 |
| `fixed_I_grow_element` | js | unassigned — `fixed_I_grow_element` was written into the page with no card | 2026-09-11 |
| `fixed_I_grow_element` | java | unassigned — `fixed_I_grow_element` was written into the page with no card | 2026-09-11 |
| `fixed_I_grow_element` | cs | unassigned — `fixed_I_grow_element` was written into the page with no card | 2026-09-11 |
| `fixed_I_grow_element` | elixir | unassigned — `fixed_I_grow_element` was written into the page with no card | 2026-09-11 |
| `fixed_I_grow_element` | corpus | unassigned — `fixed_I_grow_element` was written into the page with no card | 2026-09-11 |
| `flags_append` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `float_widen` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `forged_ordinal_both_plans` | cpp | unassigned — the page's divergence section names the reference and the dump as Johnny's | 2026-09-11 |
| `forged_ordinal_both_plans` | c | unassigned | 2026-09-11 |
| `forged_ordinal_both_plans` | go | unassigned | 2026-09-11 |
| `forged_ordinal_both_plans` | rust | unassigned | 2026-09-11 |
| `forged_ordinal_both_plans` | dart | unassigned | 2026-09-11 |
| `forged_ordinal_both_plans` | js | unassigned | 2026-09-11 |
| `forged_ordinal_both_plans` | java | unassigned | 2026-09-11 |
| `forged_ordinal_both_plans` | cs | unassigned | 2026-09-11 |
| `forged_ordinal_both_plans` | elixir | unassigned | 2026-09-11 |
| `forged_ordinal_both_plans` | corpus | unassigned — the page's divergence section names the reference and the dump as Johnny's | 2026-09-11 |
| `int_widen` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `keyed_array_enum_append` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `nested_append` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `optional_add` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `range_widen` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `refuse_writes_nothing` | cpp | unassigned — the page's divergence section names the reference and the dump as Johnny's | 2026-09-11 |
| `refuse_writes_nothing` | c | unassigned | 2026-09-11 |
| `refuse_writes_nothing` | go | unassigned | 2026-09-11 |
| `refuse_writes_nothing` | rust | unassigned | 2026-09-11 |
| `refuse_writes_nothing` | dart | unassigned | 2026-09-11 |
| `refuse_writes_nothing` | js | unassigned | 2026-09-11 |
| `refuse_writes_nothing` | java | unassigned | 2026-09-11 |
| `refuse_writes_nothing` | cs | unassigned | 2026-09-11 |
| `refuse_writes_nothing` | elixir | unassigned | 2026-09-11 |
| `refuse_writes_nothing` | corpus | unassigned — the page's divergence section names the reference and the dump as Johnny's | 2026-09-11 |
| `rename_without_was` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `string_grow` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `uint_widen` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `union_append` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `union_arm_payload_widen` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |
| `unknown_census` | cpp | unassigned — the page's divergence section names the reference and the dump as Johnny's | 2026-09-11 |
| `unknown_census` | c | unassigned | 2026-09-11 |
| `unknown_census` | go | unassigned | 2026-09-11 |
| `unknown_census` | rust | unassigned | 2026-09-11 |
| `unknown_census` | dart | unassigned | 2026-09-11 |
| `unknown_census` | js | unassigned | 2026-09-11 |
| `unknown_census` | java | unassigned | 2026-09-11 |
| `unknown_census` | cs | unassigned | 2026-09-11 |
| `unknown_census` | elixir | unassigned | 2026-09-11 |
| `unknown_census` | corpus | unassigned — the page's divergence section names the reference and the dump as Johnny's | 2026-09-11 |
| `writer_bound_count` | cpp | unassigned — the page's divergence section names the reference and the dump as Johnny's | 2026-09-11 |
| `writer_bound_count` | c | unassigned | 2026-09-11 |
| `writer_bound_count` | go | unassigned | 2026-09-11 |
| `writer_bound_count` | rust | unassigned | 2026-09-11 |
| `writer_bound_count` | dart | unassigned | 2026-09-11 |
| `writer_bound_count` | js | unassigned | 2026-09-11 |
| `writer_bound_count` | java | unassigned | 2026-09-11 |
| `writer_bound_count` | cs | unassigned | 2026-09-11 |
| `writer_bound_count` | elixir | unassigned | 2026-09-11 |
| `writer_bound_count` | corpus | unassigned — the page's divergence section names the reference and the dump as Johnny's | 2026-09-11 |
| `wstring_grow` | java | #920 (`rowan/java-reads-backward`) | 2026-09-11 |

## What the three groups are

- **`java`, twenty-four rows, #920.** The Java leg has no `internal/codegen/javatable/fixedversioning_test.go`
  at all; PR #920 (*java leg: §5, the fixed table reads backward*) writes it and names these rows in its body.
  Those twenty-four lines go when it merges.
- **`fixed_I_grow_element`, every leg and the corpus.** The page's row — `[4]fixed(12,4)` widening to
  `[4]fixed(28,4)`, the element's I, per slot — is probed nowhere and the dump writes no pair for it. Its
  scalar sibling `fixed_I_grow` is on every leg, so the row is a port of a case that exists, not new work.
- **The four divergence rows, every leg and the corpus.** `writer_bound_count`, `refuse_writes_nothing`,
  `unknown_census` and `forged_ordinal_both_plans` were written into the page for §5.8's rows 4, 9, 11 and 12
  and are probed nowhere yet. They are the rows whose counters are asserted EXACTLY rather than `>= 1`, so a
  leg that counts twice is only caught here. The page says the reference and the dump are Johnny's and that
  every other leg builds its fixture from the page's text and the row's manifest line, never from
  `fixedform_dump.cpp`.
