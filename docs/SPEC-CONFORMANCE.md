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
- **The packet-defaults pins**: `testdata/wire/packet-defaults/*.bin` with
  `.bits` (every driver of `test/packet-defaults` reads every pin, never
  skipping one; the pin directory is checked, so an unread `.bin` is a
  failure that names it). The two constant-default pins come from
  `test/conformance/packet/testdata/constant-defaults/<name>/Defaults.schema`
  (each declares package `arithmetic` with type `Value`, so each generates into
  its own tree): `hexadecimal-addition` (`const Next = 0xffffffff + 1`) and
  `unsigned-subtraction` (`const Half`, beside the field `half`). A fresh
  `Value` writes exactly the pinned bytes and bits, and the pin reads back
  over zeroed storage. `unsigned-subtraction` is the row for SPEC §4.2's rule
  that a field never shadows a same-named constant: a target that emits
  `half = half` fails to compile (Dart did, caught by this pin).
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
- **Strict-compile row (C).** Generated C table output compiles under
  `-std=c11 -Wall -Wextra -Werror` alone and when included by a unit that
  sets `_POSIX_C_SOURCE 200809L` first; included late without it, it refuses
  by name (SPEC-TABLES.md §16.5, A STRICT C COMPILE).
- Every pinned wire-fuzz vector is RUN. `testdata/wire/tables/fuzz-vectors/INDEX.txt`
  names every `*.bin` beside it, and every line of it names a file that exists;
  `harness wire-fuzz` refuses the run, naming the file, when a vector is on disk
  and not in the index or in the index and not on disk (SPEC-TABLES.md §4.2).
  A new vector is added to the index in the commit that adds the file.

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
conforms, and a stub that answers nothing can never make it (its test is
owed: `GAP-9` in §7).
The second form also runs the cards corpus. `NAME` must not be `cpp`, which is
the name of the repository's reference leg.

## 6. Versioning

The suite is versioned with the specification. A change to the specification
that changes what a conforming implementation does is incomplete without a
matching row in the corpus (a case, a golden, a refusal row); a case added
to the corpus without a matching statement in the specification is likewise
incomplete. A port states the spec version it passes the suite of.

## 7. Rules decided 2026-11-20..22, and the rows they owe

Every rule decided or fixed from 2026-11-20 to 2026-11-22 is stated in the
specification now. The code that implements it, and the shared row or test
that holds every port to it, land later, in the pull requests named below.
Until they land this table is the ONE place that links each rule to them: a
row the corpus does not hold yet is never cited as a holding row anywhere
else in this addendum.

- **entry** names the rule. `FL-nn` is a row of the fix ledger
  (`docs/fix-ledger-1122.md`, carried by #4757); `GAP-n` is a question a
  spec-only port had to guess at (`experiments/spec-port-<lang>/SPEC-GAPS.md`
  of Swift, OCaml, Objective-C, Zig, Haskell and Common Lisp: `Sw`, `OC`,
  `ObjC`, `Zig`, `Hs` and `Li` with the gap number); `SF-nn` is a row of the
  silent-failure audit (`docs/silent-failure-audit-1122.md`, carried by
  #4740); `Rnn` is a ruling; `INDEX` and `retain-id-bound-is-the-wires` are
  single rules.
- **rule (spec)** is the rule and the section that states it.
- **held by, as decided** is the shared row or test that holds the rule once
  its PR lands, as the PR states it.
- **not on main yet** names every id the PR adds, or the verdict it changes.
- **PR** names every pull request whose head carries an id of that column.
- **state** is `owed` while any of it is.

**THE TABLE IS HELD BY TEST, OFFLINE** (`internal/docgate`
`TestOwedRowsAreLinked`, which reads git and the tree, never the network):

- An entry that names no PR fails, and so does one that owes no id.
- Every owed id is one of three kinds, matched whole and only where the
  repository defines that kind: a corpus ROW, as the case field of
  `testdata/conformance/tables/MANIFEST.txt`, the first field of
  `internal/ci/hostilecorpus/corpus/manifest.tsv` or of a
  `test/conformance/<surface>/rows.txt`; a `Test` func, as `func Name(` at
  the start of a line of a `_test.go` file; a PATH, as a tracked file or
  directory. A backticked word of no kind fails as malformed and is never
  looked up. One known looseness: a `func Name(` that starts a line inside a
  `/* */` comment or a raw string also counts as present. It errs toward
  failing an entry, never toward passing one.
- The PR column names EVERY open PR whose head holds an id the entry owes,
  stacked PRs included, derived from the heads and never by hand.
- An `owed` entry fails once a PR it names has merged, read from the
  FIRST-PARENT history of `HEAD` only: a merge commit `Merge pull request #N
  from ...`, or a squash commit ending `(#N)`. A commit of a merged branch
  never counts. A REBASE MERGE carries no number and is NOT detected this
  way; the repository allows rebase merging, so the next rule is what
  catches it.
- An `owed` entry fails once ANY id it owes is in the tree. Every owed id is
  absent from main when the entry is written, so an id that appears means
  its code landed, by whatever merge method.

So the PR that lands a row links it in the same change: it cites the row
where the rule is stated and deletes the entry, or, while another PR named
still owes part of it, takes its own number and its own ids off the entry.

| entry | rule (spec) | held by, as decided | not on main yet | PR | state |
|---|---|---|---|---|---|
| FL-05 | enum `null` and `"None"` are one value (SPEC-TABLES §16.2 (enum row)) | `sp-enum-none-name` | `sp-enum-none-name` | #4688 | owed |
| FL-07 | comment bytes are well-formed UTF-8 with no NUL (SPEC-TABLES §16.2 (COMMENTS)) | `rr-comment-illformed-utf8`, `rr-comment-lone-surrogate`, `rr-comment-nul`, `rr-comment-wellformed-utf8` | `rr-comment-illformed-utf8`, `rr-comment-lone-surrogate`, `rr-comment-nul`, `rr-comment-wellformed-utf8` | #4706, #4751, #4752, #4754, #4757 | owed |
| FL-09 | map key bounded by its own `string(N)` (SPEC-TABLES §16.2 (A SCHEMA-NAME STRING IS CAPPED AT 255 BYTES; map row)) | `sp-map-key-256-wide`, `sp-map-key-300-wide`, `sp-map-key-301-wide` | `TestTextMapKeyAboveNameCapReadsUnderItsOwnBound`, `sp-map-key-256-wide`, `sp-map-key-300-wide`, `sp-map-key-301-wide` | #4688 | owed |
| FL-12 | the read depth cap counts every container (SPEC-TABLES §16.2) | `rr-depth-placed-union-map-128`, `rr-depth-placed-union-map-129`, `rr-depth-placed-keyed-map-128`, `rr-depth-placed-keyed-map-129`, `sp-skip-depth-129`, `sp-skip-depth-130`, **ADDED** `rr-depth-skipped-union-arm-128`, `-129`, `rr-depth-skipped-keyed-slot-128`, `-129` (a skipped value under an unknown union arm or enum key), **ADDED** `rr-depth-placed-table-map`, `rr-depth-placed-array-map`, `rr-depth-placed-list-map`, `rr-depth-placed-pointer-chain`, `rr-depth-placed-pointer-array`, `rr-depth-skipped-table-key-array`, `rr-depth-skipped-table-key-object` (each `-128` reads, `-129` refuses); every kind of container counts: a by-value table, an array, a list, a map, an enum-keyed object, a union, a pointer and an array of pointers, and a value skipped under an unknown table key, enum key or union arm. A flags array or text list is a container too, but has NO row yet: no registered root nests one 128 deep, so it needs a recursive root registered in every driver | `TestDepthRowsAnsweredByTheEngine`, `TestReadDepthCapCountsPlacedContainers`, `rr-depth-placed-array-map-128`, `rr-depth-placed-array-map-129`, `rr-depth-placed-keyed-map-128`, `rr-depth-placed-keyed-map-129`, `rr-depth-placed-list-map-128`, `rr-depth-placed-list-map-129`, `rr-depth-placed-pointer-array-128`, `rr-depth-placed-pointer-array-129`, `rr-depth-placed-pointer-chain-128`, `rr-depth-placed-pointer-chain-129`, `rr-depth-placed-table-map-128`, `rr-depth-placed-table-map-129`, `rr-depth-placed-union-map-128`, `rr-depth-placed-union-map-129`, `rr-depth-skipped-keyed-slot-128`, `rr-depth-skipped-keyed-slot-129`, `rr-depth-skipped-table-key-array-128`, `rr-depth-skipped-table-key-array-129`, `rr-depth-skipped-table-key-object-128`, `rr-depth-skipped-table-key-object-129`, `rr-depth-skipped-union-arm-128`, `rr-depth-skipped-union-arm-129`, `sp-skip-depth-129`, `sp-skip-depth-130` | #4688, #4706, #4751, #4752, #4754, #4757 | owed |
| FL-13 | the sign of zero survives a long token (SPEC-TABLES §16.2) | `rr-num-negative-zero-long-token` | `rr-num-negative-zero-long-token` | #4706, #4751, #4752, #4754, #4757 | owed |
| FL-19 | deprecated enum variant (owned by #4689) (SPEC-TABLES §2.15) | `sp-enum-elem-deprecated-array` | `sp-enum-elem-deprecated-array` | #4689, #4753 | owed |
| FL-20 | labels are exact 64-bit values (SPEC-TABLES §16.7) | `rr-label-past-2-53-distinct`, `rr-label-past-2-53-undefined`, `label-overflow` | `rr-label-past-2-53-distinct`, `rr-label-past-2-53-undefined` | #4706, #4751, #4752, #4754, #4757 | owed |
| FL-23 | float32 past range is refused by every writer (SPEC-TABLES §16.3) | `rr_wref_f32_past_range`, `rr_wref_f32_max`, `rr_fref_f32_past_range`, `rr_fref_f32_max`, `rr_jref_f32_past_range`, `rr_jref_f32_max` | `TestJsJsonWriterRefusesFloat32PastRange`, `rr_fref_f32_max`, `rr_fref_f32_past_range`, `rr_jref_f32_max`, `rr_jref_f32_past_range`, `rr_wref_f32_max`, `rr_wref_f32_past_range` | #4706, #4751, #4752, #4754, #4757 | owed |
| FL-24 | text form refuses an ill-formed comment (SPEC-TABLES §16.9.1, §16.9.4) | `test/conformance/textform/refusals/comment-illformed` | `test/conformance/textform/refusals/comment-illformed.reason`, `test/conformance/textform/refusals/comment-illformed.sexp` | #4706, #4751, #4752, #4754, #4757 | owed |
| FL-25 | counted arrays born at their minimum (#4689; R10 inside a union arm, owned elsewhere) (SPEC-TABLES §22.2, §2.13) | `test/verify-tablegen` op 7; `feature-witnesses/counted-minimum-message`; `sp-count-min-*` | `sp-count-min-at`, `sp-count-min-below`, `sp-count-min-empty`, `sp-count-min-max`, `sp-count-min-over` | #4689, #4753 | owed |
| FL-26 | a repeated binary map key is damage (SPEC-TABLES §3 (a map's entries ascend with no key twice), §2.8) | `rr_map_key_repeated`, `map_descending` | `rr_map_key_repeated` | #4706, #4751, #4752, #4754, #4757 | owed |
| FL-27 | non-canonical base64 refuses (SPEC-TABLES §16.8, §16.2) | `base64-padding-*`, `base64-unpadded`, `sp-b64-escaped-noncanonical` | `sp-b64-escaped-noncanonical` | #4688 | owed |
| FL-31 | names some target cannot build are refused at check (SPEC.md §4.6) | `internal/check` buildable-names cases | `internal/check/buildable_names_test.go` | #4691 | owed |
| FL-33, FL-34, FL-35 | R2, R5, R9 (SPEC-TABLES §3.4 (the presence entry)) | `internal/ci` `TestRulingsR2R5R9PortRows` | `TestFixedLineageOptionalIntoBareRefusesKindMismatch`, `TestRulingsR2R5R9PortRows` | #4680 | owed |
| FL-37 | R7 labels in a skipped value (SPEC-TABLES §16.7) | `skipped-label-*` | `skipped-label-fraction`, `skipped-label-misspelled`, `skipped-label-not-first`, `skipped-label-undefined-pointer-free` | #4501, #4636, #4757, #4764, #4769, #4773 | owed |
| FL-38 | R8 id cap (SPEC-TABLES §3 (the id table); §3.3 (the sender)) | `frame_entries_4096`, `frame_entries_4097`; `tablewire.IdCapSchema`, `tablewire.AnnounceSchema` per port, proved by `TestGoAnnouncementRefusesMoreThan4096Entries` and `TestCppAnnouncementRefusesMoreThan4096Entries` (#4685) | `TestCppAnnouncementRefusesMoreThan4096Entries`, `TestGoAnnouncementRefusesMoreThan4096Entries` | #4685 | owed |
| FL-39 | a fixed-form file holds at least one record (SPEC-TABLES §3.4 (load step 9)) | `rr_fixed_zero_records` | `rr_fixed_zero_records` | #4706, #4751, #4752, #4754, #4757 | owed |
| GAP-1 | A port reads `.schema` text; the grammar is complete and normative, the IR is no interchange (SPEC.md §4.2); gaps Sw1, OC1, ObjC1, Zig1, Hs1, Li2 | `testdata/conformance/grammar` (`compiler/grammar_corpus_test.go`) | `compiler/grammar_corpus_test.go`, `testdata/conformance/grammar` | #4706, #4751, #4752, #4754, #4757 | owed |
| GAP-2 | A declaration's qualification section also ends at its body's `{`; a variant's does not (SPEC.md §4.1); gaps Sw3, ObjC2, Zig2, Hs2, Li3 | grammar `accept/QualifiedBrace`, `refuse/QualifiedVariantBrace` | `testdata/conformance/grammar/accept/QualifiedBrace.schema`, `testdata/conformance/grammar/refuse/QualifiedVariantBrace.schema` | #4706, #4751, #4752, #4754, #4757 | owed |
| GAP-3 | `fixed table`, `[]T`, `[N]?T`, bare `string`, `*bytes`, `*string` are productions (SPEC.md §4.2); gaps OC2, Zig2, Hs3 | grammar `accept/TypeSpellings` | `testdata/conformance/grammar/accept/TypeSpellings.schema` | #4706, #4751, #4752, #4754, #4757 | owed |
| GAP-4 | An `if` with no `else` ends at its `}`; `else` may open the next line (SPEC.md §4.2, If); gaps Sw14 | grammar `accept/IfBlocks` | `testdata/conformance/grammar/accept/IfBlocks.schema` | #4706, #4751, #4752, #4754, #4757 | owed |
| GAP-5 | A table's qualification takes `integrity` and `json` too (SPEC.md §4.2); gaps Hs5 | grammar `accept/QualifiedBrace` | `testdata/conformance/grammar/accept/QualifiedBrace.schema` | #4706, #4751, #4752, #4754, #4757 | owed |
| GAP-7 | A refusal is `refused\n` on every surface, a wire-refused row and a message row with no message wire included; a `.absent` there is an absence and never the refusal (ruling R13; test/conformance/README.md); gaps Sw4, OC6, ObjC3, Zig8, Hs12 | `TestWireRefusedSpelledRefusedScoresPass`; `blob_shared`, `blob_shared_string` | `TestWireRefusedSpelledRefusedScoresPass` | #4706, #4751, #4752, #4754, #4757 | owed |
| GAP-8 | The json-write absence marker is `<case>.json.absent` (README); gaps Sw12, Hs13 | `TestJsonWriteAbsentMarkerIsTheAnswerNamePlusAbsent` | `TestJsonWriteAbsentMarkerIsTheAnswerNamePlusAbsent` | #4706, #4751, #4752, #4754, #4757 | owed |
| GAP-9 | `make conformance-port` fails on any absence (§5); a port declares its gated keys by answering absent (README); gaps Zig3, OC14 | `TestCompleteRunRefusesEveryAbsence` | `TestCompleteRunRefusesEveryAbsence` | #4706, #4751, #4752, #4754, #4757 | owed |
| GAP-10 | First use counts every reference, skipped bytes included (SPEC-TABLES §3); gaps Sw5 | `framing skipped_ids_first_use`, `skipped_ids_out_of_order` | `skipped_ids_first_use`, `skipped_ids_out_of_order` | #4706, #4751, #4752, #4754, #4757 | owed |
| GAP-11 | A `tag` edit: `0` None, `n` the `n`-th arm; path steps through maps, keyed arrays and arms; `count` on an empty array takes the element default; `wide:`/`units` retired (FORMAT.md); gaps Sw6, Sw7, OC10, OC15, Zig10, Zig11, Zig12, Zig17 | `wref_`/`fref_`/`mref_`/`jref_union_tag_none`; the `write-refusal` rows' map, keyed and arm paths | `fref_union_tag_none`, `jref_union_tag_none`, `mref_union_tag_none`, `wref_union_tag_none` | #4706, #4751, #4752, #4754, #4757 | owed |
| GAP-12 | The definitions digest, pinned: leaf kinds (enum `7`), roles, row order, one-sided fill, `was` ids (FIXED-FORM-ALGORITHM §5.2; SPEC-TABLES §21); gaps Sw8, OC4, Zig15 | `testdata/conformance/fixed-digest/vector.txt` (`TestFixedDefinitionsDigestVector`) | `TestFixedDefinitionsDigestVector`, `testdata/conformance/fixed-digest/vector.txt` | #4706, #4751, #4752, #4754, #4757 | owed |
| GAP-13 | Synthetic layout ids; a timestamp lays as kind `5`; the union tag width (FIXED-FORM-ALGORITHM §1); gaps OC3, OC5, ObjC10 | the digest vector's layout; every `schema.lock` `layout=` | `testdata/conformance/fixed-digest/vector.txt` | #4706, #4751, #4752, #4754, #4757 | owed |
| GAP-17 | A skipped value: label rules at every variable root, label grammar only at a fixed root; any other `&` key refuses at every root (SPEC-TABLES §16.7, R7); gaps OC8, OC12, ObjC5, ObjC9, Zig7, Hs10, Li10 | `reserved-ampersand-in-skipped-value`, `skipped-label-*`, `skipped-ampersand-stray-key`, `skipped-ampersand-node-not-first`, `skipped-ampersand-key-variable`, `skipped-label-undefined-pointer-free` | `skipped-ampersand-key-variable`, `skipped-label-fraction`, `skipped-label-misspelled`, `skipped-label-not-first`, `skipped-label-undefined-pointer-free` | #4501, #4636, #4757, #4764, #4769, #4773 | owed |
| GAP-18 | Every key in a skipped value is capped at 255 bytes (SPEC-TABLES §16.2); gaps ObjC4, Zig9, Hs9, Li8 | `skipped-key-255-bytes`, `skipped-key-256-bytes` | `skipped-key-255-bytes`, `skipped-key-256-bytes` | #4706, #4751, #4752, #4754, #4757 | owed |
| GAP-19 | A deprecated variant where no default is declared reads `None`, in every form (SPEC-TABLES §16.2; FIXED-FORM-ALGORITHM §4); gaps OC13, ObjC7, Li11 | `sp-enum-elem-deprecated-array` (#4689) | `sp-enum-elem-deprecated-array` | #4689, #4753 | owed |
| GAP-20 | A union and its structural payload are two levels (SPEC-TABLES §4); gaps Zig5, Hs8, Li4 | `rr-depth-placed-union-map-128`, `-129` | `rr-depth-placed-union-map-128`, `rr-depth-placed-union-map-129` | #4706, #4751, #4752, #4754, #4757 | owed |
| GAP-22 | A JSON array under its counted minimum refuses (SPEC-TABLES §16.2, R10); gaps Hs11 | `sp-count-min-*` (#4689) | `sp-count-min-at`, `sp-count-min-below`, `sp-count-min-empty`, `sp-count-min-max`, `sp-count-min-over` | #4689, #4753 | owed |
| GAP-26 | A float literal is a float64 first, then the field's width (SPEC.md §4.2); gaps Hs4 | `testdata/conformance/frontend/vector.txt` (`TestFrontEndVector`): a literal just past a float32 tie, as a default, a range end and inside an expression | `TestFrontEndVector`, `testdata/conformance/frontend/vector.txt` | #4757 | owed |
| GAP-27 | Names are ASCII; a `was` alias hashes its UTF-8 bytes (SPEC.md §4.2); gaps Li1 | `testdata/conformance/frontend/vector.txt`: non-ASCII `was` on a table, a field and an arm | `testdata/conformance/frontend/vector.txt` | #4757 | owed |
| SF-03, SF-04 | the variable `Save` and `Measure` check rows 1–2 on every field, not only on `\| required` fields (SPEC.md §5.1 rows 1–2) | `write-refusal` (`wref_range_*`, `wref_bits_wide*` on a fixed root); `sfv_bits_wide` / `sfv_bits_ok` on the variable root `TortureCook` (a plain non-required `bits(13)`, 8192 refused and 8191 written); a ranged non-required `int` on a variable root is owed: no corpus unit with a `min` / `max` on a variable root has a write-refusal path in the hand-coded legs | `TestCVariableWriteRangeAssertCompiled`, `TestCVariableWriteRangeAssert`, `TestCppVariableWriteRangeAssertCompiled`, `TestCppVariableWriteRangeAssert`, `sfv_bits_ok`, `sfv_bits_wide` | #4740, #4743, #4744, #4753, #4771, #4772 | owed |
| SF-07, SF-08 | the packet `Write` checks row 8 (text) and row 10 (compressed float) in every C# target, unity included (SPEC.md §5.1 rows 8, 10) | `write-refusal-packet` (`text_illformed`, `text_zero`, `text_long`); a compressed-float row (SF-08) is owed: `PacketWriteRefusal` declares no compressed float, and no C# leg runs this surface yet | `TestUnityPacketWriteAssertsRows8And10` | #4740, #4743, #4744, #4753, #4771, #4772 | owed |
| SF-09 to SF-15 | a write check reaches every carrier: a plain field, an optional, an array element, a union arm, a keyed slot (SPEC.md §5.1 rows 1–4, 8) | `write-refusal` (`wref_*`); on the variable root `TortureCook`, SF-10 `sfv_opt_bits_wide` / `sfv_opt_bits_ok` (an optional `?bits(37)`) and SF-12 `sfv_elem_bits_wide` / `sfv_elem_bits_ok` (a `bits(37)` element of `arr_bits`, a `[]T` array); owed because no corpus unit declares the shape on a variable root: SF-09 (a plain `bytes(N)`), SF-13 (an over-long optional array, arm array or arm `bytes(N)`), SF-14's `string(N)` arm and SF-15 (a keyed-array scalar slot with a range or width); SF-14's scalar arm is owed too: the only variable-root arm that carries a width is `bound_unions[13].u_bits`, an arm inside a list element, which the walkers of the Lua, Luau, MOO, Goose, Squirrel and Lobster drivers do not reach | `TestRustWriteRefusalReachesEveryCarrier`, `sfv_elem_bits_ok`, `sfv_elem_bits_wide`, `sfv_opt_bits_ok`, `sfv_opt_bits_wide`, `wref_enum_1_ok`, `wref_enum_256`, `wref_enum_4294967296`, `wref_enum_9`, `wref_shapes_octets_count_ok`, `wref_shapes_octets_count`, `wref_shapes_pick_tag_ok`, `wref_shapes_pick_tag`, `wref_shapes_ranged_high_ok`, `wref_shapes_ranged_high`, `wref_shapes_ranged_low_ok`, `wref_shapes_ranged_low`, `wref_shapes_recs_count_ok`, `wref_shapes_recs_count`, `wref_shapes_seventh_below_ok`, `wref_shapes_seventh_below`, `wref_shapes_tenth_past_ok`, `wref_shapes_tenth_past`, `wref_shapes_tones_enum_ok`, `wref_shapes_tones_enum`, `wref_shapes_zero_nan`, `wref_shapes_zero_ok`, `wref_union_tag_0_ok`, `wref_union_tag_1_ok`, `wref_union_tag_256`, `wref_union_tag_4294967296`, `wref_union_tag_9`, `wref_union_tag_none`, `wref_var_arm_length_ok`, `wref_var_arm_length`, `wref_var_bits13_neg`, `wref_var_bits13_ok`, `wref_var_bits13_wide`, `wref_var_bits37_ok`, `wref_var_bits37_wide`, `wref_var_count_ok`, `wref_var_count`, `wref_var_enum_ok`, `wref_var_enum`, `wref_var_i32_low`, `wref_var_i32_ok`, `wref_var_i32_wide`, `wref_var_layers_count_ok`, `wref_var_layers_count`, `wref_var_list_range_ok`, `wref_var_list_range`, `wref_var_list_tag_ok`, `wref_var_list_tag`, `wref_var_nested_range_ok`, `wref_var_nested_range`, `wref_var_range_high_ok`, `wref_var_range_high`, `wref_var_range_low_ok`, `wref_var_range_low`, `wref_var_str_length_ok`, `wref_var_str_length`, `wref_var_u64_ok`, `wref_var_u64_wide`, `wref_var_u8_ok`, `wref_var_u8_wide`, `wref_var_union_tag_ok`, `wref_var_union_tag` | #4501, #4636, #4706, #4740, #4743, #4744, #4751, #4752, #4753, #4754, #4757, #4764, #4769, #4771, #4772, #4773 | owed |
| SF-17 | `SaveMessages` checks row 10 and never moves an off-range float onto an endpoint (SPEC.md §5.1 row 10) | `write-refusal-message`; a float case is owed: no corpus unit declares a ranged or compressed float, so no row can carry one | `smref_big_ratio_high`, `smref_big_ratio_max` | #4808 | owed |
| SF-18, SF-19 | the message form refuses a flags value above its declared width and never masks it (SPEC.md §5.1 row 9; SPEC-TABLES §3.3) | a `write-refusal-message` flags case is owed: the only message-capable root with a flags field is `messagedemo` `ToolMessage` (the `caps` arm), which has no `connection` row and no write-refusal-message root in the hand-coded legs (C, C++, Rust, Dart) | `TestGooseMessageFormRefusesFlagsBitAboveWidth`, `TestJsWritersRefuseSilentDrops`, `smref_big_mask_max`, `smref_big_mask_wide`, `smref_mask_named`, `smref_mask_unnamed` | #4740, #4743, #4744, #4753, #4771, #4772, #4808 | owed |
| SF-20 | a writer refuses a `[N]T` or `[E]T` whose length is not N and never cuts or pads it (SPEC.md §5.1) | a case on `write-refusal-fixed` and `write-refusal-message` is owed: the `count` kind needs a companion count field and a `[N]T` has none, so every leg's `count` handler must first learn to answer `unrepresentable` or resize a fixed array | `TestJsWritersRefuseSilentDrops`, `sref_fixed_long`, `sref_fixed_ok`, `sref_fixed_short`, `sref_keyed_long`, `sref_keyed_ok`, `sref_keyed_short`, `sfref_fixed_long`, `sfref_fixed_ok`, `sfref_fixed_short`, `sfref_keyed_long`, `sfref_keyed_ok`, `sfref_keyed_short`, `sjref_fixed_long`, `sjref_fixed_ok`, `sjref_fixed_short`, `sjref_keyed_long`, `sjref_keyed_ok`, `sjref_keyed_short`, `smref_fixed_long`, `smref_fixed_ok`, `smref_fixed_short`, `smref_keyed_long`, `smref_keyed_ok`, `smref_keyed_short`, `cref_fixed_long`, `cref_fixed_ok` | #4740, #4743, #4744, #4753, #4771, #4772, #4808 | owed |
| SF-21 | a writer refuses a counted array below its `Min` (SPEC.md §5.1 row 3) | a case on `write-refusal-fixed` is owed (`sp-count-min-*` carries the read side): no corpus unit declares a `[Min..N]T` | `TestJsWritersRefuseSilentDrops`, `sp-count-min-at`, `sp-count-min-below`, `sp-count-min-empty`, `sp-count-min-max`, `sp-count-min-over` | #4689, #4740, #4743, #4744, #4753, #4771, #4772 | owed |
| SF-25, SF-26 | `Cook` and `CookMeasure` hold the write contract that `Save` holds; a deprecated enum variant is refused on write (SPEC-TABLES §7.6, §2.15) | `cook-write`; a refusal case is owed: `cook-write` has no refusal verdict and no corpus unit declares a deprecated enum variant | `TestLobsterCookHoldsTheWriteContract`, `cref_count_at_max`, `cref_count_at_min`, `cref_count_below_min`, `cref_count_over`, `cref_cycle`, `cref_shared_ok`, `cref_finite_ok`, `cref_infinite`, `cref_non_finite` | #4740, #4743, #4744, #4753, #4771, #4772, #4808 | owed |
| SF-22, SF-28 | a variable save and a text (JSON) writer refuse a counted array below its `Min`, and a list field holding a non-list (SPEC.md §5.1 row 3 and "A writer refuses in EVERY form") | a `write-refusal` `count` row below a `[Min..N]` Min is owed: no corpus unit declares a counted array with `Min > 0` on a variable root, so it needs a new unit; SF-28 cannot be a row (a statically typed leg cannot hold a list field that is no list) and is held per port | `TestElixirVariableWritersRefuseCountBelowMinAndNonList`, `sref_count_at_min`, `sref_count_below_min`, `sref_count_not_list`, `sjref_count_at_min`, `sjref_count_below_min`, `sjref_count_not_list` | #4740, #4771, #4772, #4808 | owed |
| SF-23 | `SaveMessages` holds a value to the rule of the declaration it is written under, not to the looser rule of a same-named field elsewhere (SPEC-TABLES §3.3) | a `write-refusal-message` `int` row on two declarations that share a field name and kind at different ranges is owed (new unit) | `TestElixirSaveMessagesHoldsEachDeclarationToItsOwnRange`, `smref_narrow_hp_5`, `smref_narrow_hp_6`, `smref_wide_hp_10`, `smref_wide_hp_11` | #4740, #4753, #4771, #4772, #4808 | owed |
| SF-27 | the text read maps a deprecated enum variant to the field default (an element: None) (SPEC-TABLES §2.15 READ) | owed: the text-form corpus (`testdata/conformance/text`) carries strings only, so an enum text case needs the text surface extended | `TestLobsterTextReadHoldsDeprecatedVariants` | #4740, #4771, #4772 | owed |
| SF-29, SF-37 | a cook refuses a loaded count outside `[Min, bound]` and a NaN or an infinity in a plain float (SPEC-TABLES §7.6; SPEC.md §5.1 row 7) | `cook-write`; a refusal case is owed; SF-29 cannot be a row (a caller-edited loaded value, not an instance) | `TestLobsterCookHoldsTheWriteContract`, `cref_count_at_min`, `cref_count_below_min`, `cref_count_over`, `cref_finite_ok`, `cref_infinite`, `cref_non_finite` | #4740, #4743, #4744, #4753, #4771, #4772, #4808 | owed |
| SF-31 | the block builder refuses a count or a used length past its maximum by name in every build (SPEC-TABLES §19.1) | none: no corpus surface builds a block from values (`block` surfaces read images) | `TestLobsterBlockBuildRefusesByName`, `bref_elems_long`, `bref_elems_ok`, `bref_length`, `bref_length_ok`, `bref_range`, `bref_range_ok` | #4740, #4771, #4772, #4808 | owed |
| SF-32, SF-33 | tooling (moo-analyze run with no output, Luau bitpacker probe) (none: not a wire rule) | none | `TestAnalyzeOutcomeRefusesSilentFailure` | #4740, #4771, #4772 | owed |
| R14 | R14: a field whose type changed refuses the WHOLE load `kind_mismatch`; only a widening reads (SPEC-TABLES.md opening, §2.3, §2.11, kind `36`, §4 THE ONE WIDENING RULE and its edits table) | refuses `message-prior prior_restring_kind` (int64 read as string), twin `message-prior prior_kept64_inside` (int64 read as int64); refuses `message-prior prior_stamp_kind` (int64 read as timestamp), twin `message-prior prior_stamped_inside`; refuses `report r14_at_int64_as_timestamp` (int64 read as timestamp, file form), twin `report r14_features_twin`; refuses `report r14_optional_pointer_as_optional` (from `*T`), twin `report r16_optional_plain_as_optional`; refuses `report r14_child_value_as_pointer` (to `*T`), twin `report r14_features_twin` | `prior_kept64_inside`, `prior_restring_kind`, `prior_stamp_kind`, `prior_stamped_inside`, `r14_at_int64_as_timestamp`, `r14_child_value_as_pointer`, `r14_features_twin`, `r14_optional_pointer_as_optional`, `r16_optional_plain_as_optional` | #4764, #4769, #4773 | owed |
| R15 | R15: a ranged announcement is checked per value: the base at the announcement, each value on the body (SPEC-TABLES.md §3.3, A RANGED ANNOUNCEMENT IS CHECKED PER VALUE) | refuses `message-prior prior_band_past` (245 against the reader's 200..240), twin `message-prior prior_band_inside` (the writer announces `uint8 \| min = 200, max = 250`, and 230 reads) | `prior_band_inside`, `prior_band_past` | #4764, #4769, #4773 | owed |
| R16 | R16: optional is a widening and runs one way: `T` to `?T` passes, `?T` to `T` is refused by the baseline naming the field (SPEC-TABLES.md §2.3, §4, §18.2, §21.1; VERSIONING.md) | refuses `internal/baseline` `TestBaselineOptionalIsOneWay`, `TestRefusals/an_optional_field_made_plain`, `TestEvolutionTableFrames`; the lock, `TestLockOptionalAddRefuses`, twin `report r16_optional_plain_as_optional` (`Part` read as `?Part`, present) | `TestBaselineOptionalIsOneWay`, `r16_optional_plain_as_optional` | #4764, #4769, #4773 | owed |
| R17 (widening) | R17: a new reader reads old data, never the other way: the baseline passes every widening of §4 (an integer kind to a wider one of the same signedness, `f32` to `f64`, `T` to `?T`; at a field, an arm, an element and a map key) and refuses a narrowing, a retype and `?T` to `T`; an old reader given a widened file refuses it `kind_mismatch` by name, lawfully (SPEC-TABLES.md §4 THE ONE WIDENING RULE and its edits table, §18.2; VERSIONING.md) | refuses `internal/baseline` `TestBaselinePassesEveryWidening` (every narrowing and retype refuses, naming the field; with the old refusal planted, every widening refuses), `TestRefusals/a_field's_kind_changed`, twin `internal/baseline` `TestBaselinePassesEveryWidening` (every widening passes in silence), `TestEvolutionTableFrames/a_scalar_kind_widened` (the read counts `widened`, the baseline passes) | `TestBaselinePassesEveryWidening`; the verdict passes on every widening row named | #4769, #4773 | owed |
| R17 (kind 31) | R17: the newer reader reads old data, never the other way, so kind `31` (the escape) is HELD BY NAME AND NEVER WRITTEN, and a reader that meets it ANYWHERE (a field it cannot name, one it names, an arm, an element kind, a message announcement or body, under retain-unknown) refuses the WHOLE load, as for `33` and `34` (SPEC-TABLES.md §3 kind table, `31` IS HELD BY NAME, WHERE THE READER MEETS KIND `32`, §3.3 entry shapes; VERSIONING.md promise 6) | refuses `framing varevo_kind31_escape` (unnamed field, `refused`); `report escape_unnamed_field`, `escape_named_field`, `escape_arm`, `escape_element`, `varevo_kind31_report`, twin `framing fixed_array_rides_absent` (same unit and root, reads); refuses `retain varevo_kind31_retain`, `retain varevo_2hop_step1_v1`, `retain varevo_2hop_step2_v2` (owe `refused`), twin `retain varevo_retain_v3_via_v1`; refuses an announcement entry under kind `31`: `test/conformance/harness` `TestTheRetiredAndReservedKindsRefuseTheAnnouncement`; the engine, `internal/tablewire` `TestKindHeldEscapeRefuses`, twin `report varevo_v3_compat_as_v1`; `instance message_open` (`messagedemo ToolMessage`, the escape rows' unit, reads) | `TestKindHeldEscapeRefuses`; the verdict refused on every kind-31 row named | #4773 | owed |
| R17 (flags) | R17: a mask bit at or above the reader's W refuses the load, file and message (SPEC-TABLES.md §4, §3.3; SPEC.md §4.2) | refuses `report r17_flags_past_width` (VarEvo2's Read and Exec read by tblvarevo1, variable form), twin `report r17_flags_inside_width` (Read and Write from the same writer); refuses `report r17_fixed_flags_past_width` (bit 17 of a 17-variant mask, fixed form), twin `report r17_fixed_flags_inside_width` (the unforged file); refuses `message-prior prior_flags_past` (the writer's Exec, message form), twin `message-prior prior_flags_inside` | `prior_flags_inside`, `prior_flags_past`, `r17_fixed_flags_inside_width`, `r17_fixed_flags_past_width`, `r17_flags_inside_width`, `r17_flags_past_width`; the R17 flag verdicts above | #4773 | owed |

**Held on main.** These rules, decided in the same days, already have every
row and test named on main, so they owe nothing:

| entry | rule (spec) | held by |
|---|---|---|
| INDEX | every pinned wire-fuzz vector is run: `harness wire-fuzz` refuses a vector on disk and not in the index, or in the index and not on disk, naming the file (§2; SPEC-TABLES.md §4.2) | `TestWireVectorIndexAndDirectoryAgree` (#4765, merged) |
| retain-id-bound-is-the-wires | a retaining save numbers the generated table's ids and the retained ids into one trailer bounded by `id_table_max` = 4096, never by the generated table's own capacity; a file of 4096 retained ids saves back byte for byte (SPEC-TABLES.md §6.6, THE MERGED TRAILER IS BOUNDED BY THE WIRE) | `wire-fuzz --retain` on the pinned seed `id_table_4096` (graphdemo.Scene); C++ `TestCppRetainIdTableBoundIsTheWires` (#4767, merged) |
| FL-01 | compressed -0 stored as +0 (SPEC-TABLES §3.3; rulings `fixed-compressed-negzero`) | `testdata/conformance/feature-witnesses/compressed-negzero-go` |
| FL-08 | announcement over capacity refused before any entry (SPEC-TABLES §3.3 (the announcement)) | hostile corpus `announce-entries-too-large` |
| FL-10 | the JSON read holds bytes to the UTF-8 rules (SPEC-TABLES §16.2, §16.3) | `str-illformed-at-bound`, `str-raw-non-ascii`, `str-lone-lead-surrogate` |
| FL-21 | every field is checked on write (SPEC.md §5.1) | the `write-refusal` surface |
| FL-30 | nothing is silently omitted (SPEC-TABLES §11) | the absence rule (§2) |
| FL-40 | assert legs check writes in debug builds (SPEC.md §5.1) | the `write-refusal` surfaces |
| GAP-6 | The checker's refusals are `schema check`'s; a runtime port takes its input as checked (SPEC.md §4.2); gaps Sw2, Zig4 | every corpus unit checks clean (`make check`) |
| GAP-14 | The `framing` surface reads its fixed root through the declaration's VARIABLE twin; every other read at a fixed root refuses form `1` (SPEC-TABLES §3.4; README); gaps Sw9, OC7, ObjC8, Zig6 | the `framing` battery at `tblr1 Cfg`; `report previous_form_*` |
| GAP-15 | The LADDER is a normative table; never across signedness (FIXED-FORM-ALGORITHM §5); gaps OC9 | `report fixedevo_v1_as_v3`, `fixedevo_v2_as_v3` |
| GAP-16 | A fixed root's binary read has batch capacity one; older layouts come from the unit's `schema.lock` (README); gaps OC11, ObjC6, Zig16 | `report fixedevo_v3_batch2`; `fixedevo_v1_as_v3`, `fixedevo_v2_as_v3` |
| GAP-21 | A refused read returns no value when it was handed none, and resets every instance it was handed (SPEC-TABLES §6.5, R4); gaps Sw R4 | FL-03's per-leg rows (docs/fix-ledger-1122.md) |
| GAP-23 | Compressed-float range ends compare at the field's width (SPEC-TABLES §3.3); gaps Li6 | `feature-witnesses/compressed-f32-ends` |
| GAP-24 | A float32 field is written as a float32 (SPEC-TABLES §16.2); gaps Sw11 | the `text_float*` rows |
| GAP-25 | A node is reached along every kind-17 edge from the root (SPEC-TABLES §4); gaps Hs7 | `framing varevo_cycle_*` |
| json-int128-every-root | a 128-bit integer field is carried by the text form in every root that has one, fixed or variable, at a field, an array element and a table element alike: written as a string of decimal digits, read from that string only (a bare number refuses `kind_mismatch`), exact over all 128 bits, and a value past the kind or off its declared range refused (SPEC-TABLES.md §16.2, the `int128`, `uint128` row) | `verify-tablegen` pin rows `pin:{f,c}{i128,u128,wrec}:*:fix_json` (op 7); Goose `TestGooseJsonWideIntegersAreStrings` rows `typed128-*` (#4810) |
| generated-code-lint-clean | generated code compiles clean under the warnings-as-errors build of its leg: a Rust bool element written through its binding is `if *e`, never `if (*e)` (`unused_parens`) (SPEC-TABLES.md §16.2, §3) | `verify-tablegen` pin rows `pin:{f,c}bool:*` on the Rust leg; Rust `TestRustBoolElementConditionHasNoParens` (#4810) |
| unsigned-top-bit-round-trips | an unsigned value with its top bit set (`bits(64)`, `uint64`, ranged or not) is 2^63 and up, never negative: the variable load places it zero-extended, and a save of what the load read writes the same bytes, never `invalid_value` (SPEC-TABLES.md §4, A VALUE OUTSIDE THE READER RANGE) | `verify-tablegen` pin row `pin:fwrec:first:var_wire` (`mask = 2^63`); Goose `TestGooseWireLoadZeroExtendsUnsignedTopBit` (#4810) |

`GAP-26` and `GAP-27` are a FRONT-END vector, as `GAP-12`'s digest is: both rules
bind what a port resolves from schema text (a default's bits, a wire id), so a
port that reads `.schema` text checks its front end against the vector. The
generated legs take both values from the reference compiler, which the
vector's test holds, so no language leg can differ on them.

**Already stated: the section that says it.** These gaps needed no new rule.

| gap | where the text says it |
|---|---|
| Sw10, a present optional table rides even when empty | SPEC-TABLES §22.2 |
| Sw13, an `[E]T` arm payload | refused at check: SPEC-TABLES §2 ("What an arm may not be"); its grammar row is owed with `GAP-1` |
| Hs6, Li5, kind `16`'s header lies inside its own `L` | SPEC-TABLES §3; `framing keyed_header_crosses_L` |
| Li7, a count under the minimum on the wire | SPEC-TABLES §22.2 (`count_over_bound` in every form) |
| Li9, the read depth cap | SPEC-TABLES §16.2 (129 containers read, the 130th refuses); its rows are owed with `FL-12` |
| Li12, the timestamp domain | SPEC-TABLES §16.2, the `timestamp` row (exact instants) |
| Zig13, what bounds a reader's memory | SPEC-TABLES §2.13 and the measure table (a count the body cannot cover is damage before any allocation) |
| Zig14, integer overflow | SPEC.md §4.2 (constants are arbitrary precision); every out-of-range value refuses (SPEC-TABLES §4) |

**Rulings owed.** None: every gap has one reading in the text now.

**Wording only, no row (R17, 2026-11-22).** These sentences changed with
no change of behaviour, so they owe no row: VERSIONING.md promise 1, the
five-year-old file, "Add a field anywhere" and "Remove a field", the
never-clobber rule, "Ship a schema change to a mixed fleet", "Debug an old
file" and "Optional is a widening"; SPEC-TABLES.md §1 "The wire carries
evolution", the close of §4's counters and the opening of §6.6; USAGE.md's enum
width; COMPETITION.md's never-clobber line. Each now promises a newer reader
on older data and no more: an older reader given newer data may refuse it by
name, lawfully.
