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
surface the driver lists, prints the matrix and exits nonzero on any failure.
The second form also runs the cards corpus. `NAME` must not be `cpp`, which is
the name of the repository's reference leg.

## 6. Versioning

The suite is versioned with the specification. A change to the specification
that changes what a conforming implementation does is incomplete without a
matching row in the corpus (a case, a golden, a refusal row); a case added
to the corpus without a matching statement in the specification is likewise
incomplete. A port states the spec version it passes the suite of.

## Ruling rows: R14, R15, R16 and R17 (2026-11-22)

Each ruling lands three ways: the sentence in the specification, a test that
goes red when the old behaviour is planted back, and the shared rows below,
which every port must answer. A refusal row is credited only beside its lawful
twin, the accepted row of the same surface, unit and root.

| ruling (where) | refuses | lawful twin |
|---|---|---|
| R14: a field whose type changed refuses the WHOLE load `kind_mismatch`; only a widening reads (SPEC-TABLES.md opening, §2.3, §2.11, kind `36`, §4 THE ONE WIDENING RULE and its edits table) | `message-prior prior_restring_kind` (int64 read as string) | `message-prior prior_kept64_inside` (int64 read as int64) |
| | `message-prior prior_stamp_kind` (int64 read as timestamp) | `message-prior prior_stamped_inside` |
| | `report r14_at_int64_as_timestamp` (int64 read as timestamp, file form) | `report r14_features_twin` |
| | `report r14_optional_pointer_as_optional` (from `*T`) | `report r16_optional_plain_as_optional` |
| | `report r14_child_value_as_pointer` (to `*T`) | `report r14_features_twin` |
| R15: a ranged announcement is checked per value: the base at the announcement, each value on the body (SPEC-TABLES.md §3.3, A RANGED ANNOUNCEMENT IS CHECKED PER VALUE) | `message-prior prior_band_past` (245 against the reader's 200..240) | `message-prior prior_band_inside` (the writer announces `uint8 \| min = 200, max = 250`, and 230 reads) |
| R16: optional is a widening and runs one way: `T` to `?T` passes, `?T` to `T` is refused by the baseline naming the field (SPEC-TABLES.md §2.3, §4, §18.2, §21.1; VERSIONING.md) | `internal/baseline` `TestBaselineOptionalIsOneWay`, `TestRefusals/an_optional_field_made_plain`, `TestEvolutionTableFrames`; the lock, `TestLockOptionalAddRefuses` | `report r16_optional_plain_as_optional` (`Part` read as `?Part`, present) |
| R17: a new reader reads old data, never the other way: the baseline passes every widening of §4 (an integer kind to a wider one of the same signedness, `f32` to `f64`, `T` to `?T`; at a field, an arm, an element and a map key) and refuses a narrowing, a retype and `?T` to `T`; an old reader given a widened file refuses it `kind_mismatch` by name, lawfully (SPEC-TABLES.md §4 THE ONE WIDENING RULE and its edits table, §18.2; VERSIONING.md) | `internal/baseline` `TestBaselinePassesEveryWidening` (every narrowing and retype refuses, naming the field; with the old refusal planted, every widening refuses), `TestRefusals/a_field's_kind_changed` | `internal/baseline` `TestBaselinePassesEveryWidening` (every widening passes in silence), `TestEvolutionTableFrames/a_scalar_kind_widened` (the read counts `widened`, the baseline passes) |
| R17: the newer reader reads old data, never the other way, so kind `31` (the escape) is HELD BY NAME AND NEVER WRITTEN, and a reader that meets it ANYWHERE (a field it cannot name, one it names, an arm, an element kind, a message announcement or body, under retain-unknown) refuses the WHOLE load, as for `33` and `34` (SPEC-TABLES.md §3 kind table, `31` IS HELD BY NAME, WHERE THE READER MEETS KIND `32`, §3.3 entry shapes; VERSIONING.md promise 6) | `framing varevo_kind31_escape` (unnamed field, `refused`); `report escape_unnamed_field`, `escape_named_field`, `escape_arm`, `escape_element`, `varevo_kind31_report` | `framing fixed_array_rides_absent` (same unit and root, reads) |
| | `retain varevo_kind31_retain`, `retain varevo_2hop_step1_v1`, `retain varevo_2hop_step2_v2` (owe `refused`) | `retain varevo_retain_v3_via_v1` |
| | an announcement entry under kind `31`: `test/conformance/harness` `TestTheRetiredAndReservedKindsRefuseTheAnnouncement`; the engine, `internal/tablewire` `TestKindHeldEscapeRefuses` | `report varevo_v3_compat_as_v1`; `instance message_open` (`messagedemo ToolMessage`, the escape rows' unit, reads) |

The file-form writers are under `test/tables/r14/`: each is the
`featurewitnesses` schema with one declaration edited, and its wire is
`schema pack --root Root` of the `Root.json` beside it. The message-form
writer is `test/tables/prior/PriorWide.schema`.

## Ruling row: R17 (2026-11-22)

"The newer reader can read old data, but never the other way." A `flags`
mask with a bit at or above the reader's own W refuses the whole load
`out_of_range` on every table form, and every table writer refuses to write one
(SPEC-TABLES.md §4, A TABLE LOAD HOLDS A MASK TO ITS WIDTH; §3.3; SPEC.md §4.2).

| ruling (where) | refuses | lawful twin |
|---|---|---|
| R17: a mask bit at or above the reader's W refuses the load, file and message (SPEC-TABLES.md §4, §3.3; SPEC.md §4.2) | `report r17_flags_past_width` (VarEvo2's Read and Exec read by tblvarevo1, variable form) | `report r17_flags_inside_width` (Read and Write from the same writer) |
| | `report r17_fixed_flags_past_width` (bit 17 of a 17-variant mask, fixed form) | `report r17_fixed_flags_inside_width` (the unforged file) |
| | `message-prior prior_flags_past` (the writer's Exec, message form) | `message-prior prior_flags_inside` |

The variable-form writer is `test/tables/varevo/VarEvo2.schema` and each wire is
`schema pack --root EvoDoc` of `test/tables/r17/<Case>/EvoDoc.json` under it.
The engine's red-when-planted test is
`internal/tablewire` `TestFlagBitsPastWidthRefuse`, which also holds every
writer's refusal.
