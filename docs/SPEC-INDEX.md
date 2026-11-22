# Spec index, 2026-11-22

Glenn, 2026-11-22: "the next step I would like to see is you getting the spec
finalized with everything we have fixed, and the test cases in it."

This page proves, rule by rule, that every rule decided or fixed between
2026-11-20 and 2026-11-22 has (a) a sentence in the specification and (b) a
shared test case, and says (c) where each piece lives today: on `main`, or
only in an open pull request. It is an index, not a second statement of any
rule: the sentence it points at is the rule.

**Base.** `origin/main` at `21ece9a124`, plus the open pull requests named in
each row, read on 2026-11-22.

**How it was checked.** A program read every rule below with a phrase from its
sentence and the ids of its rows, and looked for each at `main` and at the
head of every open pull request in the sources list, in that order: the
phrase in `docs/SPEC.md`, `docs/SPEC-TABLES.md` and `docs/SPEC-CONFORMANCE.md`
(whitespace and bold marks ignored), and, where none of those holds it, in the
three side documents the addendum names as authorities
(`docs/FIXED-FORM-ALGORITHM.md`, `test/conformance/README.md`,
`testdata/conformance/tables/FORMAT.md`); a row id in
`testdata/conformance/tables/MANIFEST.txt`, the hostile corpus
`internal/ci/hostilecorpus/corpus/manifest.tsv` and the cards manifest; a path
in the tree; a Go test or schema by name. The first place a piece was found is
the place this page names.

**Sources.** Rulings R1 to R10 (`rulings-1122`); the fix ledger
(`docs/fix-ledger-1122.md`, FL ids, #4706); the spec-port gap closures (the
addendum, section 8, G ids, #4737, which folds #4706); the silent-failure audit
(`docs/silent-failure-audit-1122.md`, SF ids, #4740); the harness rules of
#4732 and #4733; every pull request merged since 2026-11-20 whose title names a
fix, a rule, a ruling, a refusal, conformance or the spec; and the open #4685,
#4688, #4689, #4690 and #4712.

**Legend.** `main`: on `origin/main`. `#NNNN`: only on that open pull
request. *none*: not found anywhere. A row column of *none* names why when the
source gave a reason. Status is one of **main** (sentence and rows on main),
**PR** (complete, but a piece is only in an open pull request), **no row**
(no shared row anywhere), **no sentence**, or **both**.

## The finding first: "merged" is not "on main"

73 of the pull requests merged since 2026-11-20 were merged into another
branch, not into `main`, and 70 of them are still not on `main`. Their rules
are in this index at the open pull request that carries them:

| merged into | open as | pull requests |
|---|---|---|
| `rowan/review-1121` | #4501 | #4430 #4431 #4433 #4436 #4437 #4439 #4440 #4441 #4442 #4443 #4446 #4475 #4477 #4479 #4506 #4509 #4532 #4560 #4568 #4570 #4572 #4583 #4592 #4605 #4610 #4612 #4613 #4614 #4615 #4616 #4618 #4619 #4620 #4621 #4622 #4625 |
| `rowan/review-1121-corpus` | #4636 | #4512 #4517 #4521 #4522 #4526 #4533 #4546 #4579, and through #4546 the `rowan/fix-nested-count-bounds` group #4554 #4555 #4557 #4561 #4562 |
| `rowan/spec-rows-roots-1122` | #4689 | #4692 #4693 #4695 #4696 #4698 #4699 #4701 #4702 #4703 #4704 #4705 #4715 |
| `rowan/ruling-R4-refusal-defaults` | #4690 | #4694 |
| `rowan/check-unbuildable-names` | none open; #4691 carries the rule | #4638 |
| `rowan/footgun-zero-selected` | none open | #4728 |
| `stream/findable` (branch gone) | none | #4424 #4425 #4427 #4428 #4434 #4435 (docs and install; no spec rule) |

`rowan/fix-counted-default-message` (#4564 #4635 #4653) reached `main`
through #4687. #4501 alone carries 86 changed lines of SPEC-TABLES, 42 of
SPEC, 15 of VERSIONING, 29 of the driver contract and 278 manifest rows that
`main` does not have.

## Rulings R1 to R10

| id | rule | (a) sentence | (b) shared rows | (c) status |
|---|---|---|---|---|
| R1 | a kind-`16` count header crossing `L` refuses (`wire_damaged`) | SPEC-TABLES §3.2 (main) | `keyed_header_crosses_L`, `keyed_header_crosses_L_kind_mismatch` (main) | **main** |
| R2 | a fixed `?T` read into `T` refuses `kind_mismatch`; `T` into `?T` lands present | SPEC-TABLES §3.4, THE ONE READER PATH (**added by this PR**) | *none*: a per-port table, `TestRulingsR2R5R9PortRows` and `TestFixedLineageOptionalIntoBareRefusesKindMismatch` (#4680); a harness row needs a hand-made lineage | **both** before this PR; **no row** after |
| R3 | strict announcement mode in every port, one language-neutral shape | SPEC-TABLES §3.3, STRICT MODE IS OPT-IN (main) | *none*: no announcement-strict surface (FL-36); per-port tests in #4674, #4675 (main) | **no row** |
| R4 | after any refusal every instance handed in holds its declared defaults, every form | SPEC-TABLES §6.5, THE CALLER S VALUES AFTER A REFUSAL (main); §3.4 load steps (main) | *none* in the harness: per-leg rows (`test/conformance/<leg>/rows/{F8,F10,R8,R26,W4}`) and `tla/FixedBatch.tla` (main); the remaining legs are #4690 | **no row** |
| R5 | `LoadMeasure` reports data and attribution separately | SPEC-TABLES §6.3 (main) | *none*: `TestRulingsR2R5R9PortRows` (#4680); the Go split is #4680 | **no row** |
| R6 | the per-language entry-point table is normative | SPEC-TABLES, ENTRY POINTS, PER LANGUAGE (main, before §1) | the generated goldens (`testdata/golden/tables`, main) | **main** |
| R7 | labels inside a skipped JSON value obey the label rules | SPEC-TABLES §16.7 (main); which root holds which rule: §16.7 (#4737) | `skipped-label-*`, `reserved-ampersand-in-skipped-value`, `skipped-ampersand-stray-key`, `skipped-ampersand-node-not-first` (main) | **main** |
| R8 | the id table holds 4096 ids on read and on write; a 4097th retained id is lost and counted | SPEC-TABLES §3, THE WRITER HOLDS THE SAME BOUND (main) | `frame_entries_4096`, `frame_entries_4097`; `tablewire.IdCapSchema` per port (main) | **main** |
| R8b | a sender refuses an announcement of more than 4096 entries | SPEC-TABLES §3.3, the sender holds the same default (#4737) | `tablewire.AnnounceSchema` per port (#4685); no harness row | **PR** |
| R9 | a caller bound of 0 bytes refuses and never means the default | SPEC-TABLES §3.3, the announcement bounds (main) | *none*: `TestRulingsR2R5R9PortRows` (#4680); the Go, JS, Rust and Java changes are #4680 | **no row** |
| R10 | a counted `[A..B]` minimum holds inside a union arm, every form | SPEC-TABLES §3, the union payload rows, and §8.1 (#4712); §16.2, the `[..N]T` row (#4737) | `arm-min-*`, `armmin_*`, `plain-min-*`, `plainmin_*` (#4712) | **PR** |

## The fix ledger (FL)

FL-03, FL-33, FL-34, FL-35, FL-36, FL-37 and FL-38 are R4, R2, R5, R9, R3,
R7 and R8 above.

| id | rule | (a) sentence | (b) shared rows | (c) status |
|---|---|---|---|---|
| FL-01 | a compressed float `-0` is stored as `+0` by every writer | SPEC-TABLES §3.3, the special values (main) | `feature-witnesses/compressed-negzero-go` (main) | **main** |
| FL-02 | every read zeroes the report it is handed | SPEC-TABLES §6.5 (main) | *none*: the harness hands every case a fresh report | **no row** |
| FL-04 | a writer refuses a bare `intN`/`uintN` off its width | SPEC §5.1 row 11 (main) | *none*: a typed leg cannot build the value | **no row** |
| FL-05 | an enum `null` and `"None"` are one value | SPEC-TABLES §16.2, the enum row (main) | `sp-enum-none-name` (#4688) | **PR** |
| FL-06 | the text writer refuses a flags value that is not a mask of named bits | SPEC §5.1 row 9 (main) | *none*: a typed mask cannot hold a non-mask | **no row** |
| FL-07 | JSON comment bytes are well-formed UTF-8 with no NUL | SPEC-TABLES §16.2, COMMENTS (main) | `rr-comment-illformed-utf8`, `rr-comment-lone-surrogate`, `rr-comment-nul`, `rr-comment-wellformed-utf8` (#4737) | **PR** |
| FL-08 | an announcement over capacity refuses before any entry is written | SPEC-TABLES §3.3, the announcement (main) | hostile corpus `announce-entries-too-large` (main) | **main** |
| FL-09 | a string map key is bounded by its own `string(N)`, never by the 255-byte name cap | SPEC-TABLES §16.2 (main) | `sp-map-key-256-wide`, `-300-wide`, `-301-wide` (#4688) | **PR** |
| FL-10 | the JSON read takes bytes and validates UTF-8; a surrogate is ill-formed (#4602) | SPEC-TABLES §16.2 (main) | `str-illformed-at-bound`, `str-raw-non-ascii`, `str-lone-lead-surrogate` (main) | **main** |
| FL-11 | the text writer depth cap counts table and type values only (W2) | SPEC-TABLES §16.2 (main) | *none*: no registered root writes 129 deep | **no row** |
| FL-12 | the text read depth cap counts every container, placed or skipped | SPEC-TABLES §16.2 (main) | `rr-depth-placed-union-map-128`/`-129`, `rr-depth-placed-keyed-map-128`/`-129` (#4737); `sp-skip-depth-129`/`-130` (#4688); the other container kinds (#4735) | **PR** |
| FL-13 | the sign of zero survives a number token of any length | SPEC-TABLES §16.2 (#4737) | `rr-num-negative-zero-long-token` (#4737) | **PR** |
| FL-14 | float text never reads the process locale | SPEC-TABLES §16.1 (#4737) | *none*: the locale is not a harness input | **no row** |
| FL-15 | a text writer into a caller buffer writes only the bytes it returns | SPEC-TABLES §16.1 (#4737) | *none*: a caller-buffer API | **no row** |
| FL-16 | a null `*T \| required` pointer rides as index `0` and reads null and present | SPEC-TABLES §2.3 (#4737) | *none*: no registered root declares one | **no row** |
| FL-17 | a fixed load with no stated capacity is bounded by the file | SPEC-TABLES §3.4 (#4737) | *none*: every driver states its capacity | **no row** |
| FL-18 | `SaveMessages` needs no announcement in hand; a refused one writes nothing | SPEC-TABLES §3.3 (#4737) | *none*: no surface drives a sender without an announcement | **no row** |
| FL-19 | a deprecated enum variant reads as the default; every writer refuses one, JSON included (#4701 to #4705, #4715) | SPEC-TABLES §2.15 and §16.2 (main); the row coverage note in §2.15 (#4689) | `sp-enum-elem-deprecated-array`, `sp-enum-scalar-deprecated`, `sp_enum_fixed_deprecated`, `sp_enum_scalar_fixed_deprecated`, `jref_enum_deprecated`, `jref_enum_deprecated_ok`, `jref_enum_scalar_deprecated` (#4689) | **PR** |
| FL-20 | an `&node` label is its exact unsigned 64-bit value | SPEC-TABLES §16.7 (#4737) | `rr-label-past-2-53-distinct`, `rr-label-past-2-53-undefined` (#4737); `label-overflow` (main) | **PR** |
| FL-21 | every field is checked on write, `required` or not | SPEC §5.1 (#4737) | `wref_range_*`, `wref_bits_wide*` (main) | **PR** |
| FL-22 | writers refuse a key held twice, a `[N]T` handed more than `N`, an unknown or deprecated enum | SPEC §5.1 (#4737) | *none*: a typed leg cannot hold these shapes | **no row** |
| FL-23 | a float32 past float32 range is refused by every writer, text included | SPEC-TABLES §16.3 (#4737) | `rr_wref_f32_past_range`, `rr_wref_f32_max`, `rr_fref_f32_past_range`, `rr_fref_f32_max`, `rr_jref_f32_past_range`, `rr_jref_f32_max` (#4737) | **PR** |
| FL-24 | the hand-editable text form holds its limits and refuses an ill-formed comment | SPEC-TABLES §16.9.1 (main) | `test/conformance/textform/refusals/comment-illformed` (#4737); the limits have no isolating vector | **PR** |
| FL-25 | a counted array is born at its minimum and elides there (#4687, #4611, #4564) | SPEC §4.2 and SPEC-TABLES §22.2 (main) | `test/verify-tablegen` op 7, `feature-witnesses/counted-minimum-message` (main); `sp-count-min-*` (#4689) | **main** |
| FL-26 | a binary map repeating a key is damage | SPEC-TABLES §2.8 (main) | `rr_map_key_repeated` (#4737) | **PR** |
| FL-27 | only canonical base64 reads | SPEC-TABLES §16.2 and §16.8 (main) | `base64-padding-*`, `base64-unpadded` (main); `sp-b64-escaped-noncanonical` (#4688) | **main** |
| FL-28 | a cook handed a cyclic graph refuses `data_cycle` in every build | SPEC-TABLES §6.5 and §7.6 (main) | *none*: no driven read delivers a cycle | **no row** |
| FL-29 | a read refuses through its report, never by throwing | SPEC-TABLES §6.5 (main) | *none*: the harness reads a caught throw as a refusal | **no row** |
| FL-30 | every construct is carried or refused by name | SPEC-TABLES §11, WHAT A NEW PORT OWES (main) | the absence rule of the addendum (main) | **main** |
| FL-31 | `schema check` refuses every name some target cannot build (#4638, #4691) | SPEC §4.6, every target can spell every basename (#4691) | `internal/check/buildable_names_test.go` (#4691) | **PR** |
| FL-32 | refusal reason names are recommended, never judged | SPEC-TABLES §3.2 and §6.5 (main) | not judged by design (addendum §4) | **main** |
| FL-39 | a fixed-form file holds at least one record | SPEC-TABLES §3.4, load step 9 (main) | `rr_fixed_zero_records` (#4737) | **PR** |
| FL-40 | an assert leg checks writes in its debug build only | SPEC §5.1 (main) | the `write-refusal` surfaces, `wref_*` (main) | **main** |

## The spec-port gaps (G, the addendum section 8, all in #4737)

| id | rule | (a) sentence | (b) shared rows | (c) status |
|---|---|---|---|---|
| G1 | the grammar is complete and normative; a port reads `.schema` text | SPEC §4.2 (#4737) | `testdata/conformance/grammar` (#4737) | **PR** |
| G2 | a declaration qualification also ends at its body `{`; a variant one does not | SPEC §4.1 (#4737) | grammar `accept/QualifiedBrace`, `refuse/QualifiedVariantBrace` (#4737) | **PR** |
| G3 | `fixed table`, `[]T`, `[N]?T`, bare `string`, `*bytes`, `*string` are productions | SPEC §4.2 (#4737) | grammar `accept/TypeSpellings` (#4737) | **PR** |
| G4 | an `if` with no `else` ends at its `}`; `else` may open the next line | SPEC §4.2, `If` (#4737) | grammar `accept/IfBlocks` (#4737) | **PR** |
| G5 | a table qualification takes `integrity` and `json` | SPEC §4.2 (#4737) | grammar `accept/QualifiedBrace` (#4737) | **PR** |
| G6 | the checker refusals are those of `schema check`; a runtime port takes its input as checked | SPEC §4.2 (#4737) | every corpus unit checks clean (`make check`, main) | **PR** |
| G7 | a wire-surface refusal is spelled `refused` | driver contract, exit codes (#4737) | `TestWireRefusedSpelledRefusedScoresPass` (#4737); `blob_shared`, `blob_shared_string` (main) | **PR**; see contradiction C6 |
| G8 | the json-write absence marker is `<case>.json.absent` | driver contract (#4737) | `TestJsonWriteAbsentMarkerIsTheAnswerNamePlusAbsent` (#4737) | **PR** |
| G9 | `make conformance-port` fails on any absence (`--complete`) | addendum §5 (#4737) | `TestCompleteRunRefusesEveryAbsence` (#4737) | **PR** |
| G10 | id-table first use counts every reference, skipped bytes included | SPEC-TABLES §3 (#4737) | `skipped_ids_first_use`, `skipped_ids_out_of_order` (#4737) | **PR** |
| G11 | a write-refusal `tag` edit: `0` None, `n` the n-th arm; paths through maps, keyed arrays and arms | `FORMAT.md` (#4737) | `wref_`, `fref_`, `mref_`, `jref_union_tag_none` (#4737) | **PR** |
| G12 | the definitions digest is pinned byte for byte and computed from the declaration | SPEC-TABLES §3.4, the framing (#4737); FIXED-FORM-ALGORITHM.md §5.2 | `testdata/conformance/fixed-digest/vector.txt` (#4737) | **PR** |
| G13 | synthetic layout ids; a timestamp lays as kind `5`; the union tag width | FIXED-FORM-ALGORITHM.md §1 (#4737) only | the digest vector (#4737) | **PR** |
| G14 | `framing` reads a fixed root through its variable twin; every other read refuses form `1` | SPEC-TABLES, The three wires (#4737) | the `framing` battery at `tblr1 Cfg`; `report previous_form_*` (main) | **PR** |
| G15 | the LADDER is a normative table; never across signedness | FIXED-FORM-ALGORITHM.md §5, THE LADDER, NORMATIVE (#4737) only | `fixedevo_v1_as_v3`, `fixedevo_v2_as_v3` (main) | **PR** |
| G16 | a fixed root binary read has batch capacity one | driver contract, the surfaces (#4737) | `fixedevo_v3_batch2` (main) | **PR** |
| G17 | a skipped value: label rules at a variable root, label grammar at a fixed root, any other `&` key refuses (R7) | SPEC-TABLES §16.7 (#4737) | `reserved-ampersand-in-skipped-value`, `skipped-ampersand-*` (main) | **PR** |
| G18 | every key inside a skipped value is capped at 255 bytes | SPEC-TABLES §16.2 (#4737) | hostile corpus `skipped-key-255-bytes`, `skipped-key-256-bytes` (#4737); `sp-skip-key-*` (#4688) | **PR** |
| G19 | a deprecated variant with no declared default of its own reads `None`, every form | SPEC-TABLES §16.2 (#4737) | `sp-enum-elem-deprecated-array`, `sp_enum_evolved_deprecated` (#4689) | **PR** |
| G20 | a union and its structural payload are two depth levels | SPEC-TABLES §4 (#4737) | `rr-depth-placed-union-map-128`/`-129` (#4737) | **PR** |
| G21 | a read handed no instance returns none on a refusal (R4) | SPEC-TABLES §6.5 (#4737) | *none*: FL-03 per-leg rows only | **no row** |
| G22 | a JSON array under its counted minimum refuses, a union arm included (R10) | SPEC-TABLES §16.2 (main; the arm clause #4737) | `sp-count-min-*` (#4689); `arm-min-*` (#4712) | **PR** |
| G23 | compressed-float range ends compare at the field width (#4631) | SPEC-TABLES §3.3 (#4737) | `feature-witnesses/compressed-f32-ends` (main) | **PR** |
| G24 | a float32 field is written as a float32 | SPEC-TABLES §16.2 (#4737) | `text_float*` (main) | **PR** |
| G25 | a node is reached along every kind-`17` edge from the root | SPEC-TABLES §4 (#4737) | `varevo_cycle_*` (main) | **PR** |
| G26 | a float literal is a float64 first, then the field width | SPEC §4.2 (#4737) | *none*: no corpus unit declares a literal near a float32 tie | **no row** |
| G27 | names are ASCII; a `was` alias hashes its UTF-8 bytes | SPEC §4.2 (#4737) | *none*: no corpus `was` is non-ASCII | **no row** |

## The silent-failure audit (SF, #4740)

Each SF finding is a port that broke a rule; the rules are these. The open
findings (SF-01, 02, 05, 06, 16, 22, 23, 24, 27 to 38) are port work; the rule
each breaks is below.

| id | rule (findings) | (a) sentence | (b) shared rows | (c) status |
|---|---|---|---|---|
| SF-W1 | the variable `Save` and `Measure` check every field, not only `\| required` (SF-03, 04, 36) | FL-21 | FL-21 | **PR** (as FL-21); a case on a non-required field of a variable root is owed |
| SF-W2 | the packet `Write` checks rows 8 and 10 in C# and C# (unity) (SF-07, 08) | SPEC §5.1, the C# packet line (main) | `write-refusal-packet` for C# and C# (unity) (#4658) | **PR** |
| SF-W3 | a write check reaches every carrier: optional, element, arm, keyed slot (SF-09 to 15, 34, 35) | SPEC §5.1, the Rust line (#4740); the C, Dart and Elixir lines (#4501) | `wref_*` (main) hold the plain field only; a case per carrier is owed | **no row** |
| SF-W4 | `SaveMessages` checks row 10 and never moves a float onto an endpoint (SF-17) | SPEC §5.1 row 10 (main); the Java line (#4740) | *none*: a `write-refusal-message` float case is owed | **no row** |
| SF-W5 | the message form refuses a flags value above its width, never masks it (SF-18, 19) | SPEC §5.1 row 9 (main) | *none*: a `write-refusal-message` flags case is owed | **no row** |
| SF-W6 | a `[N]T` or `[E]T` handed to a writer holds exactly N values, never cut or padded (SF-20, 24, 38) | SPEC §5.1 (#4740; and FL-22, #4737) | *none*: cases on `write-refusal-fixed` and `write-refusal-message` are owed | **no row** |
| SF-W7 | a writer refuses a counted array below its `Min` (SF-21, 22) | SPEC §5.1 row 3 (main) | *none* on the write side; the read side is SF-R1 | **no row** |
| SF-W8 | `Cook` and `CookMeasure` hold the write contract `Save` holds (SF-25, 26, 29, 37) | SPEC-TABLES §7.6 (#4740, written for Lobster) | *none*: a `cook-write` refusal case is owed | **no row** |
| SF-R1 | a count below `Min` refuses on every read, every form (SF-01, 02) | SPEC-TABLES §22.2 (main) | `sp-count-min-below`, `sp_count_min_below`, `sp_count_min_evolved_below` (#4689) | **PR** |
| SF-R2 | a read handed no report still refuses, never silently (SF-16) | SPEC-TABLES §6.5, a nil report refuses the same way (main) | *none* | **no row** |
| SF-R3 | each value is held to the range of its own declaration (SF-23) | SPEC §5.1 row 1 (main) | *none* | **no row** |
| SF-R4 | a block `Begin` refuses a count past its slot and never clamps (SF-31) | SPEC-TABLES §19.1 (main) | *none* | **no row** |

SF-27 is FL-19; SF-30 is FL-02; SF-32 and SF-33 are tooling and state no wire
rule.

## The harness rules (H)

A harness rule is held by the harness tests that name it; those are its rows.

| id | rule | (a) sentence | (b) rows | (c) status |
|---|---|---|---|---|
| H1 | every must-refuse row has a lawful twin in its group (surface, unit, root) (#4732) | addendum §4 (#4732); driver contract (#4732) | `TestEveryMustRefuseRowHasALawfulTwin`, the `twin_*` rows (#4732) | **PR**; see C7 |
| H2 | a refusal is credited only beside an answered twin; the score line `credited C of T; absent A; refusal-only R; failed F` (#4732) | addendum §4 (#4732) | `harness/discriminate.go` tests (#4732) | **PR**; see C7 |
| H3 | nothing a driver leaves out reads as a pass (#4733) | addendum §3 (#4733) | `test/conformance/harness/footgun_test.go` (#4733) | **PR**; see C6 |
| H4 | refusals are judged by verdict only (#4556, #4612) | addendum §4 (main) | the hostile runners (main) | **main** |
| H5 | exit 0 ran, exit 2 absent, anything else fails (#4568) | addendum §3 (main) | the harness (main) | **main** |
| H6 | a ticked roadmap cell may not be answered absent (#4456) | driver contract (main) | the roadmap renderer (main) | **main** |
| H7 | an empty `.absent` on a must-refuse row counts absent, never PASS; a `.absent` naming a reason is a refusal (#4479, #4568) | driver contract (#4501) | *none* named | **no row**; see C6 |
| H8 | a refusal row and its accepted twin are one claim, the twin found by name (#4501) | driver contract (#4501) | `harness/twins.go` (#4501) | **PR**; see C7 |

## Other rules from the merged pull requests (P)

| id | rule (pull requests) | (a) sentence | (b) shared rows | (c) status |
|---|---|---|---|---|
| P1 | a `[N]T` of a type or table never elides (#4659) | SPEC-TABLES §22.2 (main) | `fixed_array_rides_default`, `fixed_array_rides_live`, `fixed_array_rides_absent` (main) | **main** |
| P2 | a form-`3` file at a variable root refuses `newer_form` (#4470, #4632) | SPEC-TABLES §3 (main) | `frame_form_three_at_variable_root` (main) | **main** |
| P3 | a grown `[N]T` bound gives each new slot a default element of its own, every form (F-092: #4544, #4558, #4563, #4574, #4593) | SPEC-TABLES §21.2 for the fixed form (main); every form (**added by this PR**) | `widenbound_msg`, `widenbound_evodoc` (main) | **PR** (this one) |
| P4 | a counted minimum holds one record down, whatever the parent declares (#4546, #4554 to #4562) | SPEC §5.1 row 3 (main) | the 48-row `test/tables/inner` corpus (#4636) | **PR** |
| P5 | a skipped message union field written with arm `0` refuses (#4441, #4481) | SPEC-TABLES §4 (main) | *none* | **no row** |
| P6 | a decimal literal with a leading zero is refused (#4451) | SPEC §4.1 (main) | *none* in the shared corpus | **no row** |
| P7 | a cook directory entry naming an offset outside the region refuses, judged before any arithmetic (#4430) | SPEC-TABLES §7.5 (main) | `cook_open_directory_entry` (main); `internal/tablecook/directory_offset_test.go` (#4501) | **main** |
| P8 | generated code reaches platform names qualified; a shadowing name is emitted, not broken (#4551, #4577, #4578) | SPEC §4.6 (main) | *none*: generator goldens only | **no row** |
| P9 | a refused message batch answers a count of `0` (#4534, #4640) | SPEC-TABLES §6.5 (main) | *none*: the harness does not read the count | **no row** |

## Rules carried only by #4501 and #4636 (RV)

| id | rule (pull requests) | (a) sentence | (b) shared rows | (c) status |
|---|---|---|---|---|
| RV1 | the message read holds `base + offset` to the announced kind domain, never wrapped (#4441, #4481) | SPEC-TABLES §3.3 (#4501) | *none* | **no row**; see C4 |
| RV2 | a message string or count past the ANNOUNCED max refuses, named or skipped alike (#4441, #4506, #4509) | SPEC-TABLES §3.3 (#4501) | *none* | **no row** |
| RV3 | a pointer or timestamp kind change refuses the whole load, never counted (#4440, #4475) | SPEC-TABLES §2.3 and §3 (#4501) | *none* named | **no row**; see C1 to C3 |
| RV4 | a `///` doc line holding a line-end character or ill-formed UTF-8 is refused (#4437, #4475) | SPEC §4.1 (#4501) | *none* | **no row** |
| RV5 | the baseline warns on a `bits(N)` narrowed inside one storage bucket (#4436) | SPEC-TABLES §18.1 (main) | *none*: baseline tests only | **no row** |
| RV6 | the baseline passes `T` to `?T` (#4436) | VERSIONING.md (#4501) | `TestBaselineAllowsOptionalEvolution` (main) | **PR**; see C5 |
| RV7 | a brace ending the qualification line is accepted for every declaration kind (#4437, #4475) | SPEC §4.2 (#4501; G2 states the same in #4737) | grammar `accept/QualifiedBrace` (#4737) | **PR** |
| RV8 | the C, Dart and Elixir write checks reach every field, element and arm payload (#4501) | SPEC §5.1 (#4501) | `wref_*` (main) | **PR** |
| RV9 | a prior writer message past a tightened range refuses, the whole batch (#4501 corpus) | SPEC-TABLES §4, a value outside the reader range (main) | `message-prior prior_*` (#4501) | **PR** |

Pull requests whose title names a fix but whose change is CI, the build, the
harness plumbing, a driver, a test runner, a release gate or docs pages, and
which state no rule a conforming implementation keeps: #4398 to #4413, #4415,
#4416, #4417, #4421, #4422, #4423, #4429, #4447, #4449, #4453, #4454, #4459,
#4462, #4465, #4468, #4471, #4482 to #4485, #4489 to #4494, #4500, #4504,
#4505, #4507, #4508, #4510, #4518, #4519, #4525, #4527, #4536, #4537, #4539,
#4540, #4541, #4553, #4565, #4566, #4592, #4596, #4597, #4599, #4600,
#4601, #4603, #4609, #4613 to #4615, #4617, #4621, #4623 to #4626, #4682,
#4684, #4697, #4719, #4720, #4723, #4724, #4727, #4728, #4736. Per-port fixes
of a rule already above are indexed at that rule.

## Summary

109 rules. 18 are complete on `main`. 52 are complete only with an open pull
request. 39 have no shared row anywhere. Before this PR one rule had no
sentence at all (R2) and one had a sentence for one form only (P3); this PR
writes both, so after it every rule has a sentence.

### A rule with no sentence

- **R2**: written by this PR, SPEC-TABLES §3.4, THE ONE READER PATH: a `?T`
  in an older layout never lands in a plain `T` (`kind_mismatch`); the reverse
  lands present.
- **P3**: SPEC-TABLES §21.2 stated it for the fixed form only; this PR states
  it for every form.

### A rule with no row (fixers add these)

The harness has no surface for: a reused report (FL-02, SF-R2), a value
handed in (R4, G21), a caller bound or capacity (R9, FL-17), a strict
announcement (R3), a sender without an announcement (FL-18), a process locale
(FL-14), a caller buffer (FL-15), a thrown error (FL-29), a message batch count
after a refusal (P9), a cycle in a cook graph (FL-28), a hand-made lineage
(R2, R5 also needs a `LoadMeasure` surface). The corpus has no registered root
for: a typed-impossible value (FL-04, FL-06, FL-22), a `*T \| required`
pointer (FL-16), a container written 129 deep (FL-11), a float literal near a
float32 tie (G26), a non-ASCII `was` (G27). Write-refusal cases owed:
a non-required field and every carrier (SF-W3), a message float (SF-W4), a
message flags value above its width (SF-W5), a `[N]T` of the wrong length on
the fixed and message surfaces (SF-W6), a counted array below `Min` on write
(SF-W7), a `cook-write` refusal (SF-W8), a value held to the wrong declaration
(SF-R3), a block count past its slot (SF-R4). Message rows owed: a skipped
union field at arm `0` (P5), `base + offset` past the kind (RV1), a string or
count past the announced max (RV2). Other rows owed: a pointer or timestamp
kind change refusing (RV3), a `///` doc line with a line end (RV4), a
leading-zero literal in the grammar corpus (P6), the baseline `bits(N)`
warning (RV5), platform-name collisions across targets (P8), and the harness
rule for an empty `.absent` (H7).

### Sentences that contradict another

Left unresolved; both readings are stated.

- **C1, main.** SPEC-TABLES, the opening of the table wire: "any reader reads
  any data", every difference reported and none fatal. SPEC-TABLES §4 and
  §18: a changed kind, a value past a tightened bound and more refuse the
  whole load. #4501 rewrites the opening to the second reading.
- **C2, main.** SPEC-TABLES §2.3: a field moved between a pointer and another
  kind is a REPORTED edit, counted, the field at its default and the body read
  on. SPEC-TABLES §4, the edits table: a field moved to or from `*T` refuses
  `kind_mismatch`. #4501 takes the second.
- **C3, main.** SPEC-TABLES §3, kind `36`: a field respelled between `int64`
  and `timestamp` is a kind mismatch, counted. SPEC-TABLES §4: a kind change
  refuses the load. #4501 takes the second.
- **C4, main against #4501.** SPEC-TABLES §3.3 on main: an announcement whose
  ranged `base + 2^bits - 1` leaves the announced kind refuses, so
  `uint8 \| min = 200, max = 250` cannot be announced. #4501: the base alone is
  held at the announcement and `base + offset` is held on each body, so that
  field is lawful.
- **C5, main docs against main code and #4501.** VERSIONING.md and
  SPEC-TABLES §18 on main: the baseline refuses `T` to `?T`
  (`TestBaselineRefusesTToOptional`, which no longer exists). The code on
  main (`TestBaselineAllowsOptionalEvolution`) and #4501: `T` to `?T` passes,
  in both directions.
- **C6, #4737 against #4733, #4732 and #4501.** #4737 (driver contract and
  addendum section 8, row 7): a historical `.absent` on a must-refuse row
  still transports the refusal, so it scores PASS. #4733 and #4732: nothing a
  driver leaves out is a pass, and absent is never credited; #4501: an EMPTY
  `.absent` on a must-refuse row counts absent, never PASS.
- **C7, #4732 against #4501.** #4732 (`harness/discriminate.go`): the twin is
  any accepted row of the same group (surface, unit, root), and a refusal
  without an answered twin is REFUSAL-ONLY, `+Nr`. #4501 (`harness/twins.go`):
  the twin is found by NAME (`<case>_ok`, `past`/`at`, `illformed`/
  `wellformed`), and the passing half of a pair whose other half is
  unanswered counts ABSENT.

Not contradictions, but the same lines are edited twice and will conflict:
the qualification-brace grammar in SPEC §4.1 and §4.2 (#4737 G2 and #4501
RV7), and the leg lines of SPEC §5.1 (#4740 for Rust and Java, #4501 for C,
Dart and Elixir).

### A rule present only in a pull request

52 rules, by the pull request that completes them: #4737, which folds #4706
(FL-07, 12, 13, 20, 21, 23, 24, 26, 39; R8b, R10 in part; G1 to G20, G22 to
G25); #4688 (FL-05, 09, 12); #4689, which holds the 12 port fixes merged
into it (FL-19, G19, G22, SF-R1); #4712 (R10, G22); #4685 (R8b); #4691, which
carries #4638 (FL-31); #4732 and #4733 (H1 to H3); #4658 (SF-W2); #4740
(SF-W1 through FL-21); #4501 (H8, RV6 to RV9; and the sentences of
RV1 to RV4, which also lack rows); #4636 (P4); and this PR (P3). The rows of
R2, R5 and R9 (#4680) and of R4 (#4690) are per-port tables, not harness rows,
so those rules are counted under no row.
