# schema — conformance addendum

**Normative.** This addendum is part of [SPEC.md](SPEC.md) and
[SPEC-TABLES.md](SPEC-TABLES.md). The specification says what a conforming
implementation does; this addendum is the test cases it must pass, and states
how they are run and judged. Where the prose and a case disagree, one of them
is a bug (VERSIONING.md).

## 1. The corpus is part of the spec

The shared conformance corpus is the following, and nothing else decides
conformance:

- **The tables manifest and its data**: `testdata/conformance/tables/`
  (`MANIFEST.txt` is the manifest, `FORMAT.md` states every file shape; the
  wires, JSON texts, goldens, framing vectors, cook inputs and outputs, block
  images, hostile JSON and report expectations live in the directories
  beside it). The wider fixtures it names are under `testdata/conformance/`
  (`feature-witnesses`, `text`, `write-refusal-int`, `r19`,
  `cook-authored-optional-map`) and `testdata/golden`, `testdata/wire` and
  `testdata/jsontestsuite`.
- **The cards corpus**: `tables/cards/conformance/` (`MANIFEST.txt`,
  `FORMAT.md`, `cases/`, `expected/`).
- **The harness and driver contract**: `test/conformance/` (the harness in
  `test/conformance/harness`, the contract in `test/conformance/README.md`).

A port is **conforming only if it passes every case on every surface it
carries.** A case it cannot answer is an absence (sections 2 and 3), never a pass; a
wrong answer is a failure whatever the reason.

## 2. The surfaces

A surface is one question put to every case of the corpus that applies to it.
The tables surfaces are `wire`, `message`, `report`, `framing`, `retain`,
`retain-save`, `json-read`, `json-write`, `json-hostile`, `cook`,
`cook-write`, `cook-foreign`, `block`, `block-foreign`, `block-dump`,
`forgery`, `cook-forgery`, `write-refusal`, `write-refusal-fixed`,
`write-refusal-message` and `write-refusal-json`. The cards surfaces are
`load`, `save`, `render`, `parse`, `bounds`, `upgrade`, `edit`, `taskhash`
and `view`. `test/conformance/README.md` states what each one asks.

**What a new port owes** is stated in SPEC-TABLES.md §11 (WHAT A NEW PORT
OWES) and is repeated here as the rule for the suite:

- A new port carries every surface, over every construct the specification
  defines, EXCEPT the three items the section allows it to refuse by name:
  the gated constructs it has not declared (the eleven keys), the variable
  class's wire surface, and the text form's three options. A construct not
  listed there is not refusable: a port that drops one, silently or by name,
  does not conform.
- What a port may refuse it answers as an ABSENCE on the cases that need it
  (exit code 2 for a whole surface, `<case>.absent` for one case), and the
  matrix prints the absences as work left. An absence is never counted as a
  pass.
- A surface a port declares absent must be one the specification lets it
  refuse. An absence on a required surface is a failure, not a gap.

## 3. The driver protocol

A driver is a command, not a binary:

```
<command> <manifest> list
<command> <manifest> <surface> <outdir>
```

It runs with the repository root as working directory, reads the derived
manifest (every expected answer removed, so a driver cannot pass by reading
the answer), and writes one file per case into `<outdir>`. Exit 0 means the
surface ran, exit 2 means the port does not implement it, anything else is a
failure. `test/conformance/README.md` is the authority for the details: the
file shapes of every surface, the absence rules and the reference-leg rule.

## 4. How answers are judged

- **Refusals are judged by verdict only.** A case that requires a refusal
  passes when the port refuses and fails when it accepts. The reason name,
  the detection order, counters and diagnostic text are NOT compared; a port
  should still use the specification's reason names, because they are how a
  user finds the rule, but the suite does not enforce them.
- **Writes are judged by bytes, exactly.** A case that requires a write
  passes only when the bytes the port produces equal the committed golden,
  byte for byte: canonical order, padding, zeros in dead bytes, every
  length. There is no tolerance.
- **Accepted values are judged by the value** the golden states, in the
  canonical text form.

Expected answers come from the committed goldens and manifests, never from
another language's leg.

## 5. Running the suite alone

A new port runs the suite WITHOUT building any other language. `make
conformance` builds every leg first and is the repository's own gate; it is
not the way for a port to run the suite. Build the port's own driver, then:

```
make conformance-port DRIVER=<path-to-driver> NAME=<name>
make conformance-port DRIVER=<path-to-driver> NAME=<name> CARDS_DRIVER='<cards driver command>'
```

The target builds only the harness (it needs a Go toolchain and nothing else
of any other language), writes a one-line registry naming the port, runs every
surface the driver lists, prints the matrix and exits nonzero on any failure
**and on any absence**: the target runs the harness with `--complete`, under
which an absent case or surface is counted, the line `tables conformance: NOT
CONFORMING: <n> case(s) answered absent` replaces the success footer, and the
run fails. A run without `--complete` (the harness's own `run`) still prints
absences as work left; `make conformance-port` is the claim that a port
conforms, and a stub that answers nothing can never make it (held by
`TestCompleteRunRefusesEveryAbsence`).
The second form also runs the cards corpus. `NAME` must not be `cpp`, which is
the name of the repository's reference leg.

## 6. Versioning

The suite is versioned with the specification. A change to the specification
that changes what a conforming implementation does is incomplete without a
matching row in the corpus (a case, a golden, a refusal row); a case added
to the corpus without a matching statement in the specification is likewise
incomplete. A port states the spec version it passes the suite of.

## 7. Regression rows (2026-11-22)

Every fix made on 2026-11-22 in any port is held by a shared row, so a new port
meets the same rule. The rules, their spec sections and the per-language tests
are in `docs/fix-ledger-1122.md` (FL ids). A rule the harness cannot ask yet
says so there; it is held per port until a surface carries it.

| rule | shared rows |
|---|---|
| FL-01 compressed -0 stored as +0 | `testdata/conformance/tables/feature-witnesses/compressed-negzero-go` |
| FL-05 enum `null` and `"None"` are one value | `sp-enum-none-name` |
| FL-07 comment bytes are well-formed UTF-8 with no NUL | `rr-comment-illformed-utf8`, `rr-comment-lone-surrogate`, `rr-comment-nul`, `rr-comment-wellformed-utf8` |
| FL-08 announcement over capacity refused before any entry | hostile corpus `announce-entries-too-large` |
| FL-09 map key bounded by its own `string(N)` | `sp-map-key-256-wide`, `sp-map-key-300-wide`, `sp-map-key-301-wide` |
| FL-10 the JSON read holds bytes to the UTF-8 rules | `str-illformed-at-bound`, `str-raw-non-ascii`, `str-lone-lead-surrogate` |
| FL-12 the read depth cap counts every container | `rr-depth-placed-union-map-128`, `rr-depth-placed-union-map-129`, `rr-depth-placed-keyed-map-128`, `rr-depth-placed-keyed-map-129`, `sp-skip-depth-129`, `sp-skip-depth-130` |
| FL-13 the sign of zero survives a long token | `rr-num-negative-zero-long-token` |
| FL-19 deprecated enum variant (owned by #4689) | `sp-enum-elem-deprecated-array` |
| FL-20 labels are exact 64-bit values | `rr-label-past-2-53-distinct`, `rr-label-past-2-53-undefined`, `label-overflow` |
| FL-21 every field is checked on write | the `write-refusal` surface |
| FL-23 float32 past range is refused by every writer | `rr_wref_f32_past_range`, `rr_wref_f32_max`, `rr_fref_f32_past_range`, `rr_fref_f32_max`, `rr_jref_f32_past_range`, `rr_jref_f32_max` |
| FL-24 text form refuses an ill-formed comment | `test/conformance/textform/refusals/comment-illformed` |
| FL-25 counted arrays born at their minimum (#4689; R10 inside a union arm, owned elsewhere) | `test/verify-tablegen` op 7; `feature-witnesses/counted-minimum-message`; `sp-count-min-*` |
| FL-26 a repeated binary map key is damage | `rr_map_key_repeated`, `map_descending` |
| FL-27 non-canonical base64 refuses | `base64-padding-*`, `base64-unpadded`, `sp-b64-escaped-noncanonical` |
| FL-30 nothing is silently omitted | the absence rule (§2) |
| FL-31 names some target cannot build are refused at check | `internal/check` buildable-names cases |
| FL-33, FL-34, FL-35 R2, R5, R9 | `internal/ci` `TestRulingsR2R5R9PortRows` |
| FL-37 R7 labels in a skipped value | `skipped-label-*` |
| FL-38 R8 id cap | `frame_entries_4096`, `frame_entries_4097`; `tablewire.IdCapSchema`, `tablewire.AnnounceSchema` per port |
| FL-39 a fixed-form file holds at least one record | `rr_fixed_zero_records` |
| FL-40 assert legs check writes in debug builds | the `write-refusal` surfaces |

Rows from open PRs land with them: `skipped-label-*` (#4676), `TestRulingsR2R5R9PortRows` (#4680), `IdCapSchema` (#4678), `AnnounceSchema` (#4685), the `sp-*` rows (#4688, #4689).

## 8. Spec-port gaps (2026-11-22)

Six ports were written from the specification alone — Swift (#4717), OCaml
(#4722), Objective-C (#4725), Zig (#4726), Haskell (#4681) and Common Lisp
(#4672) — and each recorded every question it had to guess at in
`experiments/spec-port-<lang>/SPEC-GAPS.md`. Every gap ends in one of three
states: a rule stated and held by a shared row (below), a rule the text
already stated (the second table), or a ruling owed (the third). In the
port columns, `Sw` is Swift, `OC` OCaml, `ObjC` Objective-C, `Zig` Zig, `Hs`
Haskell and `Li` Common Lisp, each with its gap number.

**Closed: a rule, and its row.**

| # | rule (where) | shared row | gaps |
|---|---|---|---|
| 1 | A port reads `.schema` text; the grammar is complete and normative, the IR is no interchange (SPEC.md §4.2) | `testdata/conformance/grammar` (`compiler/grammar_corpus_test.go`) | Sw1, OC1, ObjC1, Zig1, Hs1, Li2 |
| 2 | A declaration's qualification section also ends at its body's `{`; a variant's does not (SPEC.md §4.1) | grammar `accept/QualifiedBrace`, `refuse/QualifiedVariantBrace` | Sw3, ObjC2, Zig2, Hs2, Li3 |
| 3 | `fixed table`, `[]T`, `[N]?T`, bare `string`, `*bytes`, `*string` are productions (SPEC.md §4.2) | grammar `accept/TypeSpellings` | OC2, Zig2, Hs3 |
| 4 | An `if` with no `else` ends at its `}`; `else` may open the next line (SPEC.md §4.2, If) | grammar `accept/IfBlocks` | Sw14 |
| 5 | A table's qualification takes `integrity` and `json` too (SPEC.md §4.2) | grammar `accept/QualifiedBrace` | Hs5 |
| 6 | The checker's refusals are `schema check`'s; a runtime port takes its input as checked (SPEC.md §4.2) | every corpus unit checks clean (`make check`) | Sw2, Zig4 |
| 7 | A wire-surface refusal is `refused\n`, and the historical `.absent` still scores (test/conformance/README.md) | `TestWireRefusedSpelledRefusedScoresPass`; `blob_shared`, `blob_shared_string` | Sw4, OC6, ObjC3, Zig8, Hs12 |
| 8 | The json-write absence marker is `<case>.json.absent` (README) | `TestJsonWriteAbsentMarkerIsTheAnswerNamePlusAbsent` | Sw12, Hs13 |
| 9 | `make conformance-port` fails on any absence (§5); a port declares its gated keys by answering absent (README) | `TestCompleteRunRefusesEveryAbsence` | Zig3, OC14 |
| 10 | First use counts every reference, skipped bytes included (SPEC-TABLES §3) | `framing skipped_ids_first_use`, `skipped_ids_out_of_order` | Sw5 |
| 11 | A `tag` edit: `0` None, `n` the `n`-th arm; path steps through maps, keyed arrays and arms; `count` on an empty array takes the element default; `wide:`/`units` retired (FORMAT.md) | `wref_`/`fref_`/`mref_`/`jref_union_tag_none`; the `write-refusal` rows' map, keyed and arm paths | Sw6, Sw7, OC10, OC15, Zig10, Zig11, Zig12, Zig17 |
| 12 | The definitions digest, pinned: leaf kinds (enum `7`), roles, row order, one-sided fill, `was` ids (FIXED-FORM-ALGORITHM §5.2; SPEC-TABLES §21) | `testdata/conformance/fixed-digest/vector.txt` (`TestFixedDefinitionsDigestVector`) | Sw8, OC4, Zig15 |
| 13 | Synthetic layout ids; a timestamp lays as kind `5`; the union tag width (FIXED-FORM-ALGORITHM §1) | the digest vector's layout; every `schema.lock` `layout=` | OC3, OC5, ObjC10 |
| 14 | The `framing` surface reads its fixed root through the declaration's VARIABLE twin; every other read at a fixed root refuses form `1` (SPEC-TABLES §3.4; README) | the `framing` battery at `tblr1 Cfg`; `report previous_form_*` | Sw9, OC7, ObjC8, Zig6 |
| 15 | The LADDER is a normative table; never across signedness (FIXED-FORM-ALGORITHM §5) | `report fixedevo_v1_as_v3`, `fixedevo_v2_as_v3` | OC9 |
| 16 | A fixed root's binary read has batch capacity one; older layouts come from the unit's `schema.lock` (README) | `report fixedevo_v3_batch2`; `fixedevo_v1_as_v3`, `fixedevo_v2_as_v3` | OC11, ObjC6, Zig16 |
| 17 | A skipped value: label rules at every variable root, label grammar only at a fixed root; any other `&` key refuses at every root (SPEC-TABLES §16.7, R7) | `reserved-ampersand-in-skipped-value`, `skipped-label-*`, `skipped-ampersand-stray-key`, `skipped-ampersand-node-not-first` (#4676, on main) | OC8, OC12, ObjC5, ObjC9, Zig7, Hs10, Li10 |
| 18 | Every key in a skipped value is capped at 255 bytes (SPEC-TABLES §16.2) | `skipped-key-255-bytes`, `skipped-key-256-bytes` | ObjC4, Zig9, Hs9, Li8 |
| 19 | A deprecated variant where no default is declared reads `None`, in every form (SPEC-TABLES §16.2; FIXED-FORM-ALGORITHM §4) | `sp-enum-elem-deprecated-array` (#4689) | OC13, ObjC7, Li11 |
| 20 | A union and its structural payload are two levels (SPEC-TABLES §4) | `rr-depth-placed-union-map-128`, `-129` | Zig5, Hs8, Li4 |
| 21 | A refused read returns no value when it was handed none, and resets every instance it was handed (SPEC-TABLES §6.5, R4) | FL-03's per-leg rows (docs/fix-ledger-1122.md) | Sw R4 |
| 22 | A JSON array under its counted minimum refuses (SPEC-TABLES §16.2, R10) | `sp-count-min-*` (#4689) | Hs11 |
| 23 | Compressed-float range ends compare at the field's width (SPEC-TABLES §3.3) | `feature-witnesses/compressed-f32-ends` | Li6 |
| 24 | A float32 field is written as a float32 (SPEC-TABLES §16.2) | the `text_float*` rows | Sw11 |
| 25 | A node is reached along every kind-17 edge from the root (SPEC-TABLES §4) | `framing varevo_cycle_*` | Hs7 |
| 26 | A float literal is a float64 first, then the field's width (SPEC.md §4.2) | none yet: no corpus unit declares a literal within half a float64 ulp of a float32 tie | Hs4 |
| 27 | Names are ASCII; a `was` alias hashes its UTF-8 bytes (SPEC.md §4.2) | none yet: no corpus `was` is non-ASCII | Li1 |

Rows 26 and 27 hold their rule in the text only; a row is owed with the first
unit that reaches them.

**Already stated: the section that says it.**

| gap | where the text says it |
|---|---|
| Sw10, a present optional table rides even when empty | SPEC-TABLES §22.2 |
| Sw13, an `[E]T` arm payload | refused at check: SPEC-TABLES §2 ("What an arm may not be"); grammar `refuse/ArmKeyed` |
| Hs6, Li5, kind `16`'s header lies inside its own `L` | SPEC-TABLES §3; `framing keyed_header_crosses_L` |
| Li7, a count under the minimum on the wire | SPEC-TABLES §22.2 (`count_over_bound` in every form) |
| Li9, the read depth cap | SPEC-TABLES §16.2 (129 containers read, the 130th refuses); `sp-skip-depth-*` |
| Li12, the timestamp domain | SPEC-TABLES §16.2, the `timestamp` row (exact instants) |
| Zig13, what bounds a reader's memory | SPEC-TABLES §2.13 and the measure table (a count the body cannot cover is damage before any allocation) |
| Zig14, integer overflow | SPEC.md §4.2 (constants are arbitrary precision); every out-of-range value refuses (SPEC-TABLES §4) |

**Rulings owed.** None: every gap above has one reading in the text now.
