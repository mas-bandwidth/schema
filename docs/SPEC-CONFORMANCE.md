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
  beside it). The [accepted variable-table write bounds](#accepted-variable-table-write-bounds)
  run through the same harness with a focused manifest. The wider fixtures it names are under `testdata/conformance/`
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
- **The fixed byte-buffer lineage pins** (`test/verify-tablegen/pinned.go`):
  `pin:bytes_lineage:empty` and `pin:bytes_lineage:short`. Both load a
  supported `bytes(2)` writer's fixed file with a `bytes(6)` reader and
  compare the reader's fixed save byte for byte with the same value written
  directly under the reader's layout. The older values are zero bytes and
  the two bytes `00 ff`. Both must read whole and unchanged
  ([FIXED-FORM-ALGORITHM §4.1](FIXED-FORM-ALGORITHM.md#41-the-plan)).
  Run on a target with `make verify-tablegen-<lang>`. To select this pair
  alone, pass `--only pin:bytes_lineage:` to `build/verify-tablegen-bin`.
- **The compressed-float endpoint pins**: [`ratio-endpoints.txt`](../test/packet-cfloat-endpoints/ratio-endpoints.txt)
  gives the packet bytes for both [accepted endpoints](#compressed-float-packet-endpoints).
- **The packet end pins**: [packet-end](../testdata/wire/packet-end/README.md)
  includes the [logical bit count case](#packet-end-and-the-logical-bit-count).
- **Variable-form optional evolution** ([SPEC-TABLES §2.3](SPEC-TABLES.md)):
  the shared table cases `chain_value` and `chain_optional` pin identical
  nondefault encodings. `chain_value_empty` elides the default plain field,
  while `chain_optional_empty` encodes a present optional with default
  contents. When the plain field widens to optional, the newer reader
  observes the elided field as absent. The cross-version report cases are
  `p1_empty_as_p3` and `p3_empty_as_p1`.
- **The cards corpus**: `tables/cards/conformance/` (`MANIFEST.txt`,
  `FORMAT.md`, `cases/`, `expected/`).
- **The harness and driver contract**: `test/conformance/` (the harness in
  `test/conformance/harness`, the contract in `test/conformance/README.md`).

A port is **conforming only if it passes every case on every surface it
carries.** A case it cannot answer is an absence (sections 2 and 3), never a pass; a
wrong answer is a failure whatever the reason.

### Packet end and the logical bit count

The public reader accepts [`packet-end/exact.bin`](../testdata/wire/packet-end/exact.bin)
(the byte `03`) as `packetend.End` with `value = 3`. The declaration uses
three bits and [`exact.bits`](../testdata/wire/packet-end/exact.bits) records
that count. The reader checks the final five bits as padding. An API that
reports bits consumed reports **3** after success. The C++ reference
driver asserts the value and count in [`make packet-end-cpp`](../test/packet-end/cpp/main.cpp).

### Python packet defaults and hosted packet items

[SPEC §4.2](SPEC.md#42-grammar) requires declared string, bytes and flags
defaults to initialize packet storage. These fields still ride on the wire.
[SPEC §4.8](SPEC.md#48-unions--union-first-class-one-of-fields) applies the
construction rule when a packet union arm is selected. The shared
[packet-defaults pins](../testdata/wire/packet-defaults) `sample-defaults`,
`sample-empty`, and `choice-sample` hold the bytes and logical bit count.
`make packet-defaults-python` runs these pins. The public Python compiler
entry is covered by `TestPythonDriverCarriesPacketValueDefault`.

[SPEC §4.3](SPEC.md#43-field-types-and-their-wire-encodings) also applies
to `const`, `reserved`, and `align` in packet types in units that declare
tables. These items occupy packet bits and contribute no table field. The
[hosted packet probe](../internal/codegen/pythontable/hosted_items_test.go)
checks this distinction in `TestPythonTableHostedPacketItemsGenerate` and
`TestPythonTableHostedPacketItems`.

### Declared defaults in table fields

[SPEC §4.2](SPEC.md#42-grammar) places string, bytes and flags defaults
on fields. A field in an embedded type keeps those defaults when the type
is used in an array, keyed slot, map value, optional, pointer or union arm.
SPEC-TABLES §§4 and 22.2 define variable-form elision and reconstruction.
Section 3.4 defines fixed-record prefill, and §16.2 applies declared defaults
to absent JSON keys.

The [tables manifest](../testdata/conformance/tables/MANIFEST.txt) registers
`vd_var_defaults` and `vd_fixed_defaults` for construction values, and
`vd_var_values` and `vd_fixed_values` for values distinct from their defaults,
including empty strings, empty bytes, and zero masks. The accepted JSON cases
`vd-var-omitted` and `vd-fixed-omitted` exercise omitted fields.
`TestValueDefaultsEveryShape` checks that the registered compiler targets
generate both roots from [the shared declaration](../test/tables/ValueDefaults.schema).

## 2. The surfaces

A surface is one question put to every case of the corpus that applies to it.
The tables surfaces are `wire`, `message`, `report`, `framing`, `retain`,
`retain-save`, `json-read`, `json-write`, `json-hostile`, `cook`,
`cook-write`, `cook-foreign`, `block`, `block-foreign`, `block-dump`,
`forgery`, `cook-forgery`, `write-refusal`, `write-refusal-fixed`,
`write-refusal-message`, `write-refusal-json`, `write-refusal-cook`,
`write-refusal-block`, `announce`, `read-into` and `json-write-into`. The cards surfaces are
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
- **MF-ROSTER: every message-fuzz root is answered or ledgered.** A root of
  the message-fuzz roster that a leg answers with nothing (no known message
  in any case) is dead, and `harness message-fuzz` is red on a dead root the
  leg's `test/conformance/message-absent/<leg>.tsv` does not name, and on a
  line there naming a root the leg now answers. Lines are `<unit>.<Root>`, a
  tab, and the reason; the set is exact.
- **Every conformance row file runs.** A leg's row list is its directory
  `test/conformance/<leg>/rows/`, never a list copied by hand: `make
  conformance-rows-<leg>` (tools/conformancerows) builds and runs every row
  file, and Go's rows are named off the `TestRow*` functions of its rows
  directory. A row the leg cannot answer yet is named, with its cause, in
  `test/conformance/<leg>/rows-owed.tsv`; an owed row still runs, a failing
  row the ledger does not name is red, and a ledgered row that passes is red
  until its line is removed. A ledger line's cause is the failure itself,
  read off the row's log (the compiler's error, the exception and its
  message, the failed check): a banner around it ("error: compilation
  failed", "Unhandled exception:", "N passed, M failed") or a line of the
  row's own source is refused. A scripted row (Dart, Java, JavaScript,
  Elixir) with no way to fail (no non-zero exit, throw, raise or assert)
  checked nothing and is red when it exits 0, the twin of a Rust row whose
  test binary ran no test.
- **Every §5.6 fixed-form retirement is ledgered.** The fixed-form gates of the
  native legs print a `SKIP`/`RETIRED` line for each test retired by
  FIXED-FORM-ALGORITHM.md §5.6; `tools/retiredskips` holds the printed set to
  the leg's lines of `test/conformance/fixed-retired.json` exactly, so a new
  unledgered skip, or a ledgered retirement that no longer prints, is red.

**The rules the 2026-11-22 surfaces carry**, each with its lawful twin beside
the refusal (`MANIFEST.txt`, "THE 2026-11-22 SURFACES"):

| rule | surface | rows |
|---|---|---|
| SF-34, SF-35, FL-22: a `[N]T` holds exactly N values and a `[E]T` one per variant, in every writer; a map holding one key twice is refused | `write-refusal`, `-fixed`, `-message`, `-json`, `-cook`, `-block` (`elems`, `dupkey`) | `sref_fixed_*`, `sref_keyed_*`, `sfref_*`, `smref_fixed_*`, `smref_keyed_*`, `sjref_*`, `cref_fixed_*`, `bref_elems_*`, `sref_map_dupkey*`, `sjref_map_dupkey*` |
| FL-06: a flags bit no variant names is kept by the file writers, refused by the text writer, refused by the message writer above the width | `write-refusal*` (`mask`) | `*_mask_unnamed`, `*_mask_named` |
| FL-04: a bare `intN`/`uintN` value outside its width | `write-refusal` (`int`) | `sref_int8_*`, `sref_uint8_*`, `sref_uint32_*` |
| SF-22, SF-28: a counted array below its Min, and a list field holding no list | `write-refusal`, `-json` (`count`) | `sref_count_*`, `sjref_count_*` |
| SF-23: `SaveMessages` holds a value to its own declaration's range | `write-refusal-message` | `smref_narrow_hp_*`, `smref_wide_hp_*` |
| SF-05, SF-06: a fixed table past the size ceiling is checked by `SaveMessages` | `write-refusal-message` | `smref_big_*` |
| FL-28, SF-29, SF-37: the cook refuses a cycle, a count outside `[Min, bound]`, a NaN or an infinity | `write-refusal-cook` | `cref_*` |
| SF-31: the block builder refuses a count or a used length past its maximum | `write-refusal-block` | `bref_*` |
| FL-38, FL-36, FL-18: 4096 entries announced, 4097 refused; strict mode; a send with no announcement | `announce` | `ann_*` |
| FL-02, FL-03, FL-17, FL-29: a read zeroes its report, a refusal leaves the caller's values at their defaults, an omitted fixed capacity is the file's own, a read never throws | `read-into` | `ri_*` |
| FL-14, FL-15: float text ignores the process locale; a caller-buffer writer writes nothing past its text | `json-write-into` | `jwi_*` |
| FL-11: the text depth cap (read counts every container; write counts table/type values) | `wire`, `json-read`, `json-write` | `surf_chain127_vals` (`wire`/`json-read`/`json-write`), `surf_chain128_vals` (`wire`, `no-text`); refusal `graph_chain_past_cap` |
| FL-16: a null `*T \| required` is written as index 0 and reads back null | `wire`, `json-read`, `json-write` | `surf_holder_null_required`, `surf_holder_present` |

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

**Nothing a driver leaves out is a pass.** Each of the following is a
FAILURE that the harness names by leg, surface and row. None of them reads as
an absence:

| the driver | the verdict |
|---|---|
| `list` exits 0 and names no surface | the leg is refused |
| `list` names a surface the suite does not have | the leg is refused, by name |
| answers no row on any surface it is asked (every surface absent, every case `.absent`) | the leg fails |
| leaves a row unanswered: no answer file and no `<case>.absent` | the row fails |
| writes an answer for a row the derived manifest did not send | the stray file fails |
| writes an answer line or bytes that are not the row's | the row fails |
| dies on a signal, or exits other than 0 or 2 | the surface fails, with its unanswered rows listed |
| exits 2 after writing answers, or with a runtime panic on stderr | the surface fails: a crash, not an absence |

A declared absence (exit 2 on a surface, or `<case>.absent`) is the only
answer that is neither a pass nor a failure. The cell counts it against every
row sent (`pass 16/20 +4a`), and a run that has any absence prints what was not
answered instead of the success line. `test/conformance/README.md` ("Nothing a
driver leaves out reads as a pass") and `harness/footgun_test.go` hold the
same table.

Three surfaces run more than one call per case. `read-into` hands ONE report
and one set of values to three reads in a row and answers one line per read
(`read`, `refused`, or `threw` when the read raised instead of refusing)
with the values' text after the refused read. `announce` answers the
announcement's bytes, a SaveMessages batch's bytes, or a verdict line.
`json-write-into` runs under the comma-decimal locale the harness names in
`SCHEMA_COMMA_LOCALE` (and `LOCPATH`), which the driver must make the
process's own locale before it writes. On the `write-refusal*` and
`read-into` (`fixed-nocap`) surfaces a driver may answer `unrepresentable`
where its own types or API cannot hold the case; that answer is credited only
where the README says so.

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

- **A refusal counts only beside an accepted twin.** Every case that requires
  a refusal has a lawful twin in the corpus: a case of the same group (the
  same surface, unit and root) that requires acceptance. A port's refusal is
  credited only when the same port also answers at least one of its twins
  correctly; otherwise the case is REFUSAL-ONLY. A port that refuses a whole
  form (reads a root through the wrong reader, or has no reader for it)
  refuses the lawful value too, and has not reached the rule the refusal case
  tests. `test/conformance/README.md` states the groups and the harness
  enforces both halves: it refuses a corpus with a refusal case that has no
  twin, and scores refusal-only cases apart.

**The score.** For each port the suite prints `credited C of T; absent A;
refusal-only R; failed F` over every case of every surface it was asked.
CREDITED is a correct answer (a refusal only as above). ABSENT (a surface the
port does not list, exit code 2, or `<case>.absent`) and REFUSAL-ONLY are
reported separately and are **never counted as passes**: a port conforms only
when every case is credited except the absences section 2 lets it declare, and
it has no refusal-only case and no failure.

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

### Regression cases for implementation fixes

Every fix to observable Schema behavior adds or extends a case in the shared
corpus listed in §1. Give the case a stable identifier and cite the rule it
tests. State the input and the expected result precisely enough for a new
implementation to produce its answer from the specification alone. For wire
output, include the bytes and significant bit count when the format requires
both. For a refusal, identify the operation and the outcome the specification
requires for that target. If the rule admits more than one answer, say which
answers a port may return and how the case is judged.

When two readings of a rule would produce different observable results,
clarify the normative clause and choose a case that distinguishes them. The
case must fail on the affected implementation before the fix and pass after
it. If the shared suite cannot express the difference yet, record the missing
case and keep the regression open until a port can run it through the suite.

When a public API accepts an optional numeric limit, its contract must
identify the unit, the source of the default, and the result of explicitly
supplying zero. The regression cases must distinguish omission (where
supported), zero, exact fit and one over capacity using otherwise valid
inputs. Where several limits apply, vary each independently. Include an
empty-input case when the format admits one.

## 7. Conformance evidence for rules decided 2026-11-20..22

The source registrations for these rulings are linked below. Each evidence
entry identifies a shared corpus row or a test present in the tree. The
conformance matrix records runtime results separately by language and surface.

- **entry** names the rule. `FL-nn` is a row of the fix ledger
  (`docs/fix-ledger-1122.md`, carried by #4757); `GAP-n` is a question a
  spec-only port had to guess at (`experiments/spec-port-<lang>/SPEC-GAPS.md`
  of Swift, OCaml, Objective-C, Zig, Haskell and Common Lisp: `Sw`, `OC`,
  `ObjC`, `Zig`, `Hs` and `Li` with the gap number); `SF-nn` is a row of the
  silent-failure audit (`docs/silent-failure-audit-1122.md`, carried by
  #4740); `Rnn` is a ruling; `INDEX` and `retain-id-bound-is-the-wires` are
  single rules.
**Linked rows.** The references below record source coverage. Runtime
verification of the #4808 write-refusal surfaces is still pending for
individual language legs. The shared cook cases cover counts, repeated nodes
and non-finite floats. Deprecated enum rejection in the cook remains a
per-port check where no shared case exercises it.

| entry | rule (spec) | held by |
|---|---|---|
| INDEX | every pinned wire-fuzz vector is run: `harness wire-fuzz` refuses a vector on disk and not in the index, or in the index and not on disk, naming the file (§2; SPEC-TABLES.md §4.2) | `TestWireVectorIndexAndDirectoryAgree` (#4765, merged) |
| retain-id-bound-is-the-wires | a retaining save numbers the generated table's ids and the retained ids into one trailer bounded by `id_table_max` = 4096, never by the generated table's own capacity; a file of 4096 retained ids saves back byte for byte (SPEC-TABLES.md §6.6, THE MERGED TRAILER IS BOUNDED BY THE WIRE) | `wire-fuzz --retain` on the pinned seed `id_table_4096` (graphdemo.Scene); C++ `TestCppRetainIdTableBoundIsTheWires` (#4767, merged) |
| counted-born-elides | a plain counted `[Min..N]T` is born with `Min` elements each at its default, and a save of that born state elides the field, whether the value was born fresh or read back from text or wire at it; the shared rows of FL-25 add the refusals below `Min` (SPEC-TABLES §2.3, §22.2) | the verify-tablegen pinned matrix (`test/verify-tablegen/pinned.go`): `pin:c<elem>:fresh:*` and `pin:c<elem>:min_default:*` on every leg; Elixir also `TestCountedBornStateElides` |
| FL-01 | compressed -0 stored as +0 (SPEC-TABLES §3.3; rulings `fixed-compressed-negzero`) | `testdata/conformance/feature-witnesses/compressed-negzero-go` |
| FL-08 | announcement over capacity refused before any entry (SPEC-TABLES §3.3 (the announcement)) | hostile corpus `announce-entries-too-large` |
| FL-10 | the JSON read holds bytes to the UTF-8 rules (SPEC-TABLES §16.2, §16.3) | `str-illformed-at-bound`, `str-raw-non-ascii`, `str-lone-lead-surrogate` |
| FL-21 | every field is checked on write (SPEC.md §5.1) | the `write-refusal` surface |
| FL-30 | nothing is silently omitted (SPEC-TABLES §11) | the absence rule (§2) |
| FL-40 | assert legs check writes in debug builds (SPEC.md §5.1) | the `write-refusal` surfaces |
| GAP-6 | The checker's refusals are `schema check`'s; a runtime port takes its input as checked (SPEC.md §4.2); gaps Sw2, Zig4 | every corpus unit checks clean (`make check`) |
| GAP-14 | The `framing` surface reads its fixed root through the declaration's VARIABLE twin; every other read at a fixed root refuses form `1` (SPEC-TABLES §3.4; README); gaps Sw9, OC7, ObjC8, Zig6 | the `framing` battery at `tblr1 Cfg`; `report previous_form_*` |
| framing-twin-every-fixed-root | the `framing` surface reads EVERY form-`1` vector at ANY declared fixed root through that root's VARIABLE twin, not `tblr1 Cfg` alone: an empty bounded list and a counted array at its `Min` read there, and are `previous_form` at the fixed root itself (SPEC-TABLES §3.4, "The framing battery is no exception"; §2.3; nightly run 45130315968) | `framing twin_listdemo_bounded_empty`, `framing sp_count_min_at`, `framing twin_count_min_at_form1`; Lua `TestLuaFramingReadsEveryFixedRootThroughItsTwin` |
| GAP-15 | The LADDER is a normative table; never across signedness (FIXED-FORM-ALGORITHM §5); gaps OC9 | `report fixedevo_v1_as_v3`, `fixedevo_v2_as_v3` |
| GAP-16 | A fixed root's binary read has batch capacity one; older layouts come from the unit's `schema.lock` (README); gaps OC11, ObjC6, Zig16 | `report fixedevo_v3_batch2`; `fixedevo_v1_as_v3`, `fixedevo_v2_as_v3` |
| GAP-21 | A refused read returns no value when it was handed none, and resets every instance it was handed (SPEC-TABLES §6.5, R4); gaps Sw R4 | FL-03's per-leg rows (docs/fix-ledger-1122.md) |
| GAP-23 | Compressed-float range ends compare at the field's width (SPEC-TABLES §3.3); gaps Li6 | `feature-witnesses/compressed-f32-ends` |
| GAP-24 | A float32 field is written as a float32 (SPEC-TABLES §16.2); gaps Sw11 | the `text_float*` rows |
| GAP-25 | A node is reached along every kind-17 edge from the root (SPEC-TABLES §4); gaps Hs7 | `framing varevo_cycle_*` |
| message-kind-mismatch-refuses-the-batch | a message-form batch whose announced kind at a field or union arm is not the one this reader declares (and is no widening) refuses the whole batch `kind_mismatch`, count `0`, every body at its declared defaults; the entry is never skipped and counted into a delivered batch (SPEC-TABLES.md §3.3, A FIELD WHOSE ANNOUNCED KIND IS NOT THE ONE THIS READER DECLARES) | `message-fuzz` seed 1, `cross-tbla1-to-tbla2.Root` (test/tables/A1.schema written, A2.schema read); JS `TestJsMessageLoadRefusesKindMismatchBatch` (#4809) |
| json-int128-every-root | a 128-bit integer field is carried by the text form in every root that has one, fixed or variable, at a field, an array element and a table element alike: written as a string of decimal digits, read from that string only (a bare number refuses `kind_mismatch`), exact over all 128 bits, and a value past the kind or off its declared range refused (SPEC-TABLES.md §16.2, the `int128`, `uint128` row) | `verify-tablegen` pin rows `pin:{f,c}{i128,u128,wrec}:*:fix_json` (op 7); Goose `TestGooseJsonWideIntegersAreStrings` rows `typed128-*` (#4810) |
| fixed-keyed-lineage-default | when appending a key enum variant grows a fixed enum-keyed array, a newer reader retains the older slots and supplies the declared element default in the new slot. The resulting value saves under the current layout (FIXED-FORM-ALGORITHM.md §5.9) | `keyed_array_enum_append` versioning fixture, Lua `TestLuaFixedFormReadsItsLineage/keyed_array_enum_append`, and `verify-tablegen` seed 1, `tg01:v1_to_v2_fixed_lineage` |
| generated-code-lint-clean | generated code compiles clean under the warnings-as-errors build of its leg: Rust emits boolean expressions and defaults at their native type, keeps compiler-settled message byte images out of the stack, spells a minimum count of one as an emptiness test, and does not cast a flags mask that is already `u64` (SPEC-TABLES.md §§3, 3.3, 16.2) | `verify-tablegen` pin rows `pin:{f,c}bool:*` on the Rust leg; Rust `TestRustBoolElementConditionHasNoParens`, `TestRustMessageGeneratedLintShapes` and `TestRustArrayMinOfOneIsAnEmptinessTest`; `tables-rust-clippy` over every generated unit (#4810) |
| lua-lineage-source-lint | generated Lua fixed lineage descriptors keep writer range tables within the Lua source line limit | `make tables-lua-luacheck` checks the generated table corpus, including `tables/examples` and `tables/scalars` |
| unsigned-top-bit-round-trips | an unsigned value with its top bit set (`bits(64)`, `uint64`, ranged or not) is 2^63 and up, never negative: the variable load places it zero-extended, and a save of what the load read writes the same bytes, never `invalid_value` (SPEC-TABLES.md §4, A VALUE OUTSIDE THE READER RANGE) | `verify-tablegen` pin row `pin:fwrec:first:var_wire` (`mask = 2^63`); Goose `TestGooseWireLoadZeroExtendsUnsignedTopBit` (#4810) |
| unsigned-64-top-bit-is-in-width | a `bits(64)` or `uint64` value at 2^63 and up is inside its width: a reader carrying it in a wider slot extends it with zeros, and the save writes it back byte for byte, never refusing it as wider than its bits (Goose sign-extended it and refused `invalid_value`) (SPEC.md §5.1, ROWS 2 AND 11 READ AN UNSIGNED VALUE UNSIGNED) | verify-tablegen pinned case `pin:fwrec:first` (`verify-<lang>-tablegen` for every leg) (#4762) |
| skipped-ampersand-in-a-pointer-free-unit | the label grammar holds inside an unknown key's skipped value in a unit with no pointer too: an `&`-prefixed key that is not its object's first `&node`, or an `&node` whose label is not a positive decimal, refuses the read (Goose's pointer-free skip read past both) (SPEC-TABLES.md §16.7, THE LABEL GRAMMAR AND THE DEFINITION RULES APPLY EVERYWHERE) | json-hostile `skipped-ampersand-stray-key` and `skipped-ampersand-node-not-first` (tabledemo RootConfig), every leg (#4800) |
| FL-07 | comment bytes are well-formed UTF-8 with no NUL (SPEC-TABLES §16.2 (COMMENTS)) | `rr-comment-illformed-utf8`, `rr-comment-lone-surrogate`, `rr-comment-nul`, `rr-comment-wellformed-utf8` (#4706) |
| FL-13 | the sign of zero survives a long token (SPEC-TABLES §16.2) | `rr-num-negative-zero-long-token` (#4706) |
| FL-20 | labels are exact 64-bit values (SPEC-TABLES §16.7) | `rr-label-past-2-53-distinct`, `rr-label-past-2-53-undefined`, `label-overflow` (#4706) |
| FL-23 | float32 past range is refused by every writer (SPEC-TABLES §16.3) | `rr_wref_f32_past_range`, `rr_wref_f32_max`, `rr_fref_f32_past_range`, `rr_fref_f32_max`, `rr_jref_f32_past_range`, `rr_jref_f32_max` (#4706) |
| FL-24 | text form refuses an ill-formed comment (SPEC-TABLES §16.9.1, §16.9.4) | `test/conformance/textform/refusals/comment-illformed` (#4706) |
| FL-26 | a repeated binary map key is damage (SPEC-TABLES §3 (a map's entries ascend with no key twice), §2.8) | `rr_map_key_repeated`, `map_descending` (#4706) |
| FL-39 | a fixed-form file holds at least one record (SPEC-TABLES §3.4 (load step 9)) | `rr_fixed_zero_records` (#4706) |
| GAP-1 | A port reads `.schema` text; the grammar is complete and normative, the IR is no interchange (SPEC.md §4.2); gaps Sw1, OC1, ObjC1, Zig1, Hs1, Li2 | `testdata/conformance/grammar` (`compiler/grammar_corpus_test.go`) (#4706) |
| GAP-2 | A declaration's qualification section also ends at its body's `{`; a variant's does not (SPEC.md §4.1); gaps Sw3, ObjC2, Zig2, Hs2, Li3 | grammar `accept/QualifiedBrace`, `refuse/QualifiedVariantBrace` (#4706) |
| GAP-3 | `fixed table`, `[]T`, `[N]?T`, bare `string`, `*bytes`, `*string` are productions (SPEC.md §4.2); gaps OC2, Zig2, Hs3 | grammar `accept/TypeSpellings` (#4706) |
| GAP-4 | An `if` with no `else` ends at its `}`; `else` may open the next line (SPEC.md §4.2, If); gaps Sw14 | grammar `accept/IfBlocks` (#4706) |
| GAP-5 | A table's qualification takes `integrity` and `json` too (SPEC.md §4.2); gaps Hs5 | grammar `accept/QualifiedBrace` (#4706) |
| GAP-7 | An ordinary read refusal is `refused\n`, including a wire-refused row and a message row with no message wire. An empty `.absent` declares absence (ruling R13; test/conformance/README.md); gaps Sw4, OC6, ObjC3, Zig8, Hs12 | `TestWireRefusedSpelledRefusedScoresPass`; `blob_shared`, `blob_shared_string` (#4706) |
| GAP-8 | The json-write absence marker is `<case>.json.absent` (README); gaps Sw12, Hs13 | `TestJsonWriteAbsentMarkerIsTheAnswerNamePlusAbsent` (#4706) |
| GAP-9 | `make conformance-port` fails on any absence (§5); a port declares its gated keys by answering absent (README); gaps Zig3, OC14 | `TestCompleteRunRefusesEveryAbsence` (#4706) |
| GAP-10 | First use counts every reference, skipped bytes included (SPEC-TABLES §3); gaps Sw5 | `framing skipped_ids_first_use`, `skipped_ids_out_of_order` (#4706) |
| GAP-11 | A `tag` edit: `0` None, `n` the `n`-th arm; path steps through maps, keyed arrays and arms; `count` on an empty array takes the element default; `wide:`/`units` retired (FORMAT.md); gaps Sw6, Sw7, OC10, OC15, Zig10, Zig11, Zig12, Zig17 | `wref_`/`fref_`/`mref_`/`jref_union_tag_none`; the `write-refusal` rows' map, keyed and arm paths (#4706) |
| GAP-12 | The definitions digest, pinned: leaf kinds (enum `7`), roles, row order, one-sided fill, `was` ids (FIXED-FORM-ALGORITHM §5.2; SPEC-TABLES §21); gaps Sw8, OC4, Zig15 | `testdata/conformance/fixed-digest/vector.txt` (`TestFixedDefinitionsDigestVector`) (#4706) |
| GAP-13 | Synthetic layout ids; a timestamp lays as kind `5`; the union tag width (FIXED-FORM-ALGORITHM §1); gaps OC3, OC5, ObjC10 | the digest vector's layout; every `schema.lock` `layout=` (#4706) |
| GAP-17 | A skipped value: label rules at every variable root, label grammar only at a fixed root; any other `&` key refuses at every root (SPEC-TABLES §16.7, R7); gaps OC8, OC12, ObjC5, ObjC9, Zig7, Hs10, Li10 | `skipped-label-fraction`, `skipped-label-misspelled`, `skipped-label-not-first` (#4501); `skipped-ampersand-key-variable`, `skipped-label-undefined-pointer-free` (#4757) |
| FL-37 | R7 labels in a skipped value (SPEC-TABLES §16.7) | `skipped-label-undefined-pointer-free` (#4757) |
| GAP-18 | Every key in a skipped value is capped at 255 bytes (SPEC-TABLES §16.2); gaps ObjC4, Zig9, Hs9, Li8 | `skipped-key-255-bytes`, `skipped-key-256-bytes` (#4706) |
| GAP-20 | A union and its structural payload are two levels (SPEC-TABLES §4); gaps Zig5, Hs8, Li4 | `rr-depth-placed-union-map-128`, `-129` (#4706) |
| SF-03, SF-04 | the variable `Save` and `Measure` check rows 1–2 on every field, not only on `\| required` fields (SPEC.md §5.1 rows 1–2) | `write-refusal` (`wref_range_*`, `wref_bits_wide*` on a fixed root); `sfv_bits_wide` / `sfv_bits_ok` on the variable root `TortureCook` (a plain non-required `bits(13)`, 8192 refused and 8191 written); a ranged non-required `int` on a variable root is owed: no corpus unit with a `min` / `max` on a variable root has a write-refusal path in the hand-coded legs (#4740) |
| SF-05, SF-06 | C and C++ debug builds assert the message save and measure write contract even when the fixed table exceeds the fixed-file record ceiling (SPEC.md §5.1 rows 1, 2, 10 and SPEC-TABLES §3.4) | `TestCMessageCeilingWriteAssert`, `TestCppMessageCeilingWriteAssert` and their `Compiled` twins. The compiled checks save and measure the same table with lawful values. A shared `write-refusal-message` case is owed until the harness has an initialized-value seed mode. The current surface seeds each value by loading a file, and this table has no file form (#4744). |
| SF-07, SF-08 | the packet `Write` checks row 8 (text) and row 10 (compressed float) in every C# target, unity included (SPEC.md §5.1 rows 8, 10) | `write-refusal-packet` (`text_illformed`, `text_zero`, `text_long`); a compressed-float row (SF-08) is owed: `PacketWriteRefusal` declares no compressed float, and no C# leg runs this surface yet (#4740); Go runs the [accepted compressed-float endpoints](#compressed-float-packet-endpoints) |
| SF-27 | the text read maps a deprecated enum variant to the field default (an element: None) (SPEC-TABLES §2.15 READ) | owed: the text-form corpus (`testdata/conformance/text`) carries strings only, so an enum text case needs the text surface extended (#4740) |
| SF-32, SF-33 | tooling (moo-analyze run with no output, Luau bitpacker probe) (none: not a wire rule) | none (#4740) |
| SF-09 to SF-15 | a write check reaches every carrier: a plain field, an optional, an array element, a union arm, a keyed slot (SPEC.md §5.1 rows 1–4, 8) | `write-refusal` (`wref_*`, `wref_var_*`, `wref_shapes_*`); on `TortureCook`, `sfv_opt_bits_wide` / `sfv_opt_bits_ok`, `sfv_elem_bits_wide` / `sfv_elem_bits_ok`, and `TestRustWriteRefusalReachesEveryCarrier` (#4501, #4740) |
| SF-13–SF-15 accepted bounds | Variable table writers accept each carrier at its declared boundary (SPEC.md §5.1 rows 1–4 and 8) | [Accepted variable-table write bounds](#accepted-variable-table-write-bounds) |
| R14 | R14: a field whose type changed refuses the WHOLE load `kind_mismatch`; only a widening reads (SPEC-TABLES.md opening, §2.3, §2.11, kind `36`, §4 THE ONE WIDENING RULE and its edits table) | refuses `message-prior prior_restring_kind` (int64 read as string), twin `message-prior prior_kept64_inside` (int64 read as int64); refuses `message-prior prior_stamp_kind` (int64 read as timestamp), twin `message-prior prior_stamped_inside`; refuses `report r14_at_int64_as_timestamp` (int64 read as timestamp, file form), twin `report r14_features_twin`; refuses `report r14_optional_pointer_as_optional` (from `*T`), twin `report r16_optional_plain_as_optional`; refuses `report r14_child_value_as_pointer` (to `*T`), twin `report r14_features_twin` (#4764) |
| R15 | R15: a ranged announcement is checked per value: the base at the announcement, each value on the body (SPEC-TABLES.md §3.3, A RANGED ANNOUNCEMENT IS CHECKED PER VALUE) | refuses `message-prior prior_band_past` (245 against the reader's 200..240), twin `message-prior prior_band_inside` (the writer announces `uint8 \| min = 200, max = 250`, and 230 reads) (#4764) |
| R16 | R16: optional is a widening and runs one way: `T` to `?T` passes, `?T` to `T` is refused by the baseline naming the field (SPEC-TABLES.md §2.3, §4, §18.2, §21.1; VERSIONING.md) | refuses `internal/baseline` `TestBaselineOptionalIsOneWay`, `TestRefusals/an_optional_field_made_plain`, `TestEvolutionTableFrames`; the lock, `TestLockOptionalAddRefuses`, twin `report r16_optional_plain_as_optional` (`Part` read as `?Part`, present) (#4764) |
| R17 (widening) | R17: a new reader reads old data, never the other way: the baseline passes every widening of §4 (an integer kind to a wider one of the same signedness, `f32` to `f64`, `T` to `?T`; at a field, an arm, an element and a map key) and refuses a narrowing, a retype and `?T` to `T`; an old reader given a widened file refuses it `kind_mismatch` by name, lawfully (SPEC-TABLES.md §4 THE ONE WIDENING RULE and its edits table, §18.2; VERSIONING.md) | refuses `internal/baseline` `TestBaselinePassesEveryWidening` (every narrowing and retype refuses, naming the field; with the old refusal planted, every widening refuses), `TestRefusals/a_field's_kind_changed`, twin `internal/baseline` `TestBaselinePassesEveryWidening` (every widening passes in silence), `TestEvolutionTableFrames/a_scalar_kind_widened` (the read counts `widened`, the baseline passes) (#4769) |
| R17 (kind 31) | R17: the newer reader reads old data, never the other way, so kind `31` (the escape) is HELD BY NAME AND NEVER WRITTEN, and a reader that meets it ANYWHERE (a field it cannot name, one it names, an arm, an element kind, a message announcement or body, under retain-unknown) refuses the WHOLE load, as for `33` and `34` (SPEC-TABLES.md §3 kind table, `31` IS HELD BY NAME, WHERE THE READER MEETS KIND `32`, §3.3 entry shapes; VERSIONING.md promise 6) | refuses `framing varevo_kind31_escape` (unnamed field, `refused`); `report escape_unnamed_field`, `escape_named_field`, `escape_arm`, `escape_element`, `varevo_kind31_report`, twin `framing fixed_array_rides_absent` (same unit and root, reads); refuses `retain varevo_kind31_retain`, `retain varevo_2hop_step1_v1`, `retain varevo_2hop_step2_v2` (owe `refused`), twin `retain varevo_retain_v3_via_v1`; refuses an announcement entry under kind `31`: `test/conformance/harness` `TestTheRetiredAndReservedKindsRefuseTheAnnouncement`; the engine, `internal/tablewire` `TestKindHeldEscapeRefuses`, twin `report varevo_v3_compat_as_v1`; Java `TestJavaMessageKnownKindReserves31` executes the generated closed-kind predicate with lawful controls 0 (variant name), 30 (enum) and 32 (void), and its ordinary enum-bearing announcement gate preserves kind 0; `instance message_open` (`messagedemo ToolMessage`, the escape rows' unit, reads) (#4773) |
| R17 (flags) | R17: a mask bit at or above the reader's W refuses the load, file and message (SPEC-TABLES.md §4, §3.3; SPEC.md §4.2) | refuses `report r17_flags_past_width` (VarEvo2's Read and Exec read by tblvarevo1, variable form), twin `report r17_flags_inside_width` (Read and Write from the same writer); refuses `report r17_fixed_flags_past_width` (bit 17 of a 17-variant mask, fixed form), twin `report r17_fixed_flags_inside_width` (the unforged file); refuses `message-prior prior_flags_past` (the writer's Exec, message form), twin `message-prior prior_flags_inside` (#4773) |
| FL-38 | R8 id cap (SPEC-TABLES §3 (the id table); §3.3 (the sender)) | `frame_entries_4096`, `frame_entries_4097`; `tablewire.IdCapSchema`, `tablewire.AnnounceSchema`, `TestGoAnnouncementRefusesMoreThan4096Entries`, `TestCppAnnouncementRefusesMoreThan4096Entries` (#4685). Shared announcement rows `ann_4096_write` (writes) and `ann_4097_write` (sender refuses). Luau runs both boundaries and strictly typechecks each generated unit with `SCHEMA_SLOW=1 go test -count=1 -v ./internal/codegen/luautable -run '^TestLuauAnnouncementRefusesMoreThan4096Entries$'` |
| FL-19 | deprecated enum element on the fixed form and compiled plan (SPEC-TABLES §2.15) | `sp-enum-elem-deprecated-array` (#4753) |
| FL-25 | counted arrays born at their minimum (SPEC-TABLES §22.2, §2.13) | `sp-count-min-at`, `sp-count-min-below`, `sp-count-min-empty`, `sp-count-min-max`, `sp-count-min-over` (#4753) |
| GAP-19 | A deprecated variant where no default is declared reads `None`, in every form (SPEC-TABLES §16.2; FIXED-FORM-ALGORITHM §4); gaps OC13, ObjC7, Li11 | `sp-enum-elem-deprecated-array`, `sp_enum_fixed_deprecated`, `sp_enum_evolved_deprecated` (#4689) |
| GAP-22 | A JSON array under its counted minimum refuses (SPEC-TABLES §16.2, R10); gaps Hs11 | `sp-count-min-at`, `sp-count-min-below`, `sp-count-min-empty`, `sp-count-min-max`, `sp-count-min-over` (#4689, #4753) |
| SF-21 | a writer refuses a counted array below its `Min` (SPEC.md §5.1 row 3) | `write-refusal-fixed` (`fref_count_min`, `fref_count_min_ok`) (#4753) |
| GAP-26 | A float literal is a float64 first, then the field's width (SPEC.md §4.2); gaps Hs4 | `testdata/conformance/frontend/vector.txt` (`TestFrontEndVector`) (#4757) |
| GAP-27 | Names are ASCII; a `was` alias hashes its UTF-8 bytes (SPEC.md §4.2); gaps Li1 | `testdata/conformance/frontend/vector.txt` (#4757) |
| FL-05 | enum `null` and `"None"` are one value (SPEC-TABLES §16.2 (enum row)) | `sp-enum-none-name` (#4688) |
| FL-09 | map key length uses its own `string(N)` bound, not `kTableJsonMaxKey` (SPEC-TABLES §16.2) | `TestTextMapKeyAboveNameCapReadsUnderItsOwnBound`, `sp-map-key-256-wide`, `sp-map-key-300-wide`, `sp-map-key-301-wide` (#4688) |
| FL-27 | non-canonical base64 refuses (SPEC-TABLES §16.8, §16.2) | `base64-padding-*`, `base64-unpadded`, `sp-b64-escaped-noncanonical` (#4688) |
| FL-12 | the read depth cap counts every container (SPEC-TABLES §16.2) | `rr-depth-*`, `sp-skip-depth-129`, `sp-skip-depth-130` (#4706, #4688); a flags array or text list nested at the depth limit still needs a shared row |
| FL-31 | names some target cannot build are refused at check (SPEC.md §4.6) | `internal/check/buildable_names_test.go` (#4691), [JavaScript read-into names](#javascript-read-into-names) |
| FL-33, FL-34, FL-35 | R2, R5, R9 (SPEC-TABLES §3.4 (the presence entry)) | `internal/ci` `TestRulingsR2R5R9PortRows`, `TestFixedLineageOptionalIntoBareRefusesKindMismatch` (#4680) |
| SF-17 | `SaveMessages` checks row 10 and never moves an off-range float onto an endpoint (SPEC.md §5.1 row 10) | `smref_big_ratio_high`, `smref_big_ratio_max` (#4808 source registrations) |
| SF-18, SF-19 | the message form refuses a flags value above its declared width and never masks it (SPEC.md §5.1 row 9; SPEC-TABLES §3.3) | `smref_big_mask_max`, `smref_big_mask_wide`, `smref_mask_named`, `smref_mask_unnamed` (#4808 source registrations) |
| SF-20 | a writer refuses a `[N]T` or `[E]T` whose length is not N and never cuts or pads it (SPEC.md §5.1) | `sref_fixed_long`, `sref_fixed_ok`, `sref_fixed_short`, `sref_keyed_long`, `sref_keyed_ok`, `sref_keyed_short`, `sfref_fixed_long`, `sfref_fixed_ok`, `sfref_fixed_short`, `sfref_keyed_long`, `sfref_keyed_ok`, `sfref_keyed_short`, `sjref_fixed_long`, `sjref_fixed_ok`, `sjref_fixed_short`, `sjref_keyed_long`, `sjref_keyed_ok`, `sjref_keyed_short`, `smref_fixed_long`, `smref_fixed_ok`, `smref_fixed_short`, `smref_keyed_long`, `smref_keyed_ok`, `smref_keyed_short`, `cref_fixed_long`, `cref_fixed_ok` (#4808 source registrations) |
| SF-25, SF-26 | `Cook` and `CookMeasure` hold the write contract that `Save` holds; a deprecated enum variant is refused on write (SPEC-TABLES §7.6, §2.15) | `cref_count_at_max`, `cref_count_at_min`, `cref_count_below_min`, `cref_count_over`, `cref_cycle`, `cref_shared_ok`, `cref_finite_ok`, `cref_infinite`, `cref_non_finite` (#4808 source registrations) |
| SF-22, SF-28 | a variable save and a text (JSON) writer refuse a counted array below its `Min`, and a list field holding a non-list (SPEC.md §5.1 row 3 and "A writer refuses in EVERY form") | `sref_count_at_min`, `sref_count_below_min`, `sref_count_not_list`, `sjref_count_at_min`, `sjref_count_below_min`, `sjref_count_not_list` (#4808 source registrations) |
| SF-23 | `SaveMessages` holds a value to the rule of the declaration it is written under, not to the looser rule of a same-named field elsewhere (SPEC-TABLES §3.3) | `smref_narrow_hp_5`, `smref_narrow_hp_6`, `smref_wide_hp_10`, `smref_wide_hp_11` (#4808 source registrations) |
| SF-29, SF-37 | a cook refuses a loaded count outside `[Min, bound]` and a NaN or an infinity in a plain float (SPEC-TABLES §7.6; SPEC.md §5.1 row 7) | `cref_count_at_min`, `cref_count_below_min`, `cref_count_over`, `cref_finite_ok`, `cref_infinite`, `cref_non_finite` (#4808 source registrations) |
| SF-31 | the block builder refuses a count or a used length past its maximum by name in every build (SPEC-TABLES §19.1) | `bref_elems_long`, `bref_elems_ok`, `bref_length`, `bref_length_ok`, `bref_range`, `bref_range_ok` (#4808 source registrations) |
| Elixir table form selection | Each explicit subset of JSON, cooked and block forms includes wire. The selected APIs build and run; the wire bytes are identical across the subsets. A default declaration also binds its fixed message root and measures and writes a batch. | [`TestFormsAllSubsetsElixir`](../internal/codegen/elixirtable/forms_construct_test.go) compiles eight explicit declarations and one default declaration, exercises the available forms, and requires warning-free compilation. |
| Elixir string-map writers | A native map and a list containing the same distinct string-key pairs must produce identical bytes. The two empty representations must also agree. Adding a second occurrence of an existing key to the list, or passing an integer key in either representation, must refuse the write. | `internal/codegen/elixirtable` `TestElixirStringMapWriteKeys` checks the generated public writer with keys `z`, `a`, and `m`, then loads the saved map. |
| Same field name in different declarations | Declare `First.count` with default 3 and range 2–4, and `Second.count` with default 9 and range 8–10. The two `count` fields share a wire ID. For each table, saving an empty caller record must produce the same bytes as saving its own explicit default. | `internal/codegen/elixirtable` `TestSparseDefaultsKeepDeclarationIdentity` checks both declarations through the public writer. |
| Elixir fixed enum-keyed writes | For slots `HTTPReady = 7` and `Fallback = 9`, the declared string names and their snake-case atom keys must produce identical bytes. If both spellings occur for one slot, its string key supplies the value. Omitting a slot must refuse the write. | `internal/codegen/elixirtable` `TestElixirFixedKeyedSaveKeyForms` checks each spelling, conflicting spellings, a missing slot, and canonical readback. |
| Elixir retained writes | With an empty retention store, a `Card` whose string `id` is `"a"` must save, and its measure must equal the byte length. Set only `id` to a list. The retained measure returns `-1`; the retained save returns `{:error, :invalid_value, report, retain}` with the supplied report and store. | `internal/codegen/elixirtable` `TestRetainSaveRefusesInvalidValues` exercises both public methods. |
| Elixir retained fixed-array arm evolution | When an older union arm carries a fixed array shorter than the current declaration, the retaining read supplies the current element default for every appended slot, exactly as the plain read does (SPEC-TABLES.md §3 fixed arrays; §4 append-only evolution). The resulting public values are equal, their plain and retaining saves are equal, and the retaining measure equals its save length. | `messagedemo.ToolEnvelope.body.spans`: `test/tables/widenbound/WidenBoundPeer.schema` declares `[1]int32`, while `tables/messages/Messages.schema` declares `[2]int32`; `internal/codegen/elixirtable` `TestRetainReadPadsEvolvedFixedArrayArm` writes the ordinary older value `{spans, [5]}` and reads `[5, 0]`. |
| Elixir variable enum-keyed text completion and typed write extent | A text `[E]T` value may omit a named slot and the reader completes it with that slot's element default (SPEC-TABLES.md §16.2, §16.9.4). The resulting full typed value saves. A directly supplied typed keyed map must already contain exactly one value per variant; a short value refuses under SF-20 rather than being silently padded. | `internal/codegen/elixirtable` `TestElixirFiniteFloat` reads `{"keyed":{"A":5.25}}` for `Key { A, B }`, observes `B = 0.0`, and saves the completed value; the paired direct `%{"A" => 5.25}` save refuses. Shared `sref_keyed_short` independently requires the same typed-writer refusal. |
| C and C++ block integer bounds | The generated block API must build for full-range signed and unsigned 64-bit fields and for fields that narrow either end by one. Values at each declared endpoint are legal. A narrower upper or lower bound still applies. | `internal/codegen/ctable` `TestCBlockRangeChecksElideStorageLimits` and `internal/codegen/cpptable` `TestCPPBlockRangeChecksElideStorageLimits` check the generated bound expressions for the six declarations. Run `make tables-clamp-limits` for the native limit checks. |
| JavaScript step-5 miss is `layout_unknown` | A fixed-form file whose header hash is in no lineage entry refuses at FIXED-FORM-ALGORITHM §5.3 step 5 as `layout_unknown`, never `layout_newer`: `n = -1`, `layoutHash` is the file's hash and nothing else, every counter is zero, and every value handed in holds its declared default (ruling R4). Fixed 2026-11-24; the JavaScript leg answered `layout_newer` before. | `test/conformance/js/rows/R15.mjs` (the build's own `Cell` file reads as its positive control, then the same file under hash `0xdeadbeefcafe0001`); `internal/codegen/jstable` `TestJSFixedVersioningOldRefusesNew`, `TestJSFixedVersioningHash`, `TestJSFixedVersioningReferenceForwardRefusal`, `TestFixedRoadmapT01Framing` (js/R8, js/R13) and `TestFixedRoadmapJsT03PlansVersions` (js/R15). |
| JavaScript `TableFixedKnownLayout` carries the contract's members only | One known-layout entry has exactly FIXED-FORM-ALGORITHM §5.9 #19's members in #19's order, the hash as two `uint32` lanes (#34): `hashLo`, `hashHi`, `layout`, `layoutBytes`, `recordBytes`, and nothing else. The lock bytes' `DataView` the layout compare reads through rides beside the table in `<Name>FixedKnownViews`. Fixed 2026-11-24; the entry carried a sixth own key, `layoutView`, before. | `test/conformance/js/rows/R23.mjs` (`Object.keys` of an entry, and every `<Name>FixedKnown` entry of the v1 unit). |

`GAP-26` and `GAP-27` are a FRONT-END vector, as `GAP-12`'s digest is: both rules
bind what a port resolves from schema text (a default's bits, a wire id), so a
port that reads `.schema` text checks its front end against the vector. The
generated legs take both values from the reference compiler, which the
vector's test holds, so no language leg can differ on them.

**Already stated: the section that says it.** These gaps needed no new rule.

| gap | where the text says it |
|---|---|
| Sw10, a present optional table rides even when empty | SPEC-TABLES §22.2 |
| Sw13, an `[E]T` arm payload | SPEC-TABLES §2.6 refuses the union arm and §2.4 permits a table field. `TestGrammarCorpus` checks [`refuse/ArmKeyed.schema`](../testdata/conformance/grammar/refuse/ArmKeyed.schema) and [`accept/FieldKeyed.schema`](../testdata/conformance/grammar/accept/FieldKeyed.schema). |
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

### Announcement vocabulary with a zero-byte budget

R9 in [SPEC-TABLES §3.3](SPEC-TABLES.md#33-the-message-form-a-batch-of-bitpacked-bodies-under-one-announced-vocabulary)
limits the vocabulary entries' bytes. Use a fresh connection for each
case. Read the ordinary nonempty announcement produced by `tables/backend`
and the empty vocabulary produced by the variable-only `tables/blobs` unit.

| Reader configuration | Empty vocabulary | Nonempty vocabulary |
|---|---|---|
| Default byte limit | Accept, count 0 | Accept, count greater than 0 |
| Explicit byte limit sufficient for the announcement | Accept, count 0 | Accept, count greater than 0 |
| Explicit byte limit 0 | Accept, count 0 | Refuse with `vocabulary_too_large` and leave `announced` false |

The empty announcement still carries its framing bytes. The zero limit
applies to the vocabulary payload. Run the generated Java example with

```sh
SCHEMA_SLOW=1 go test -count=1 -v ./internal/codegen/javatable -run '^TestMessageFormAnnounceJavaR9MaxBytes$'
```

### Ordinary message-driver round trips

The Java, Rust, and Lua conformance drivers exercise the message form through
the generated public API. For each ordinary round-trip `message` row
they read the row's `connection` announcement into one fresh receiving
vocabulary, load the batch against that same vocabulary, and save the loaded
values through the root's message writer. The harness compares every saved
byte with the committed message golden.

Use the following fixtures to check the public results.

| Contract | Shared fixture and required answer |
|---|---|
| Exact golden comparison | `store_full` loads and re-saves byte-for-byte as `testdata/wire/tables/store_full_message.bin`. |
| Declared enum value survives the public read/write path | `store_full` carries `Currency.EUR`; the reader must preserve that value and the writer must reproduce its encoded ordinal. |
| A lawful multi-body batch is actually delivered | `batch_delivered` loads both `StorePurchase` bodies and answers `count 2`; `batch_range_in_second`, `batch_cut_in_second`, and `batch_byte_after_pad` each answer `count 0`. |
| Omitted enum-keyed slots keep their declared defaults | `batch8-tabledemo.KeyedConfig` loads through the ordinary public reader and its returned values save successfully; each keyed slot absent from the wire has its declared default. `internal/codegen/luatable` `TestMessageEnumKeyedArrayKeepsOmittedDefaults` runs the same rule through the generated Lua API. |
| A nested fixed-point field distinguishes explicit zero from its declared default | In `scalars.SimState`, the default of `spawn.x` is 0.5 at 16 fractional bits (raw 32768). The `scalars_uint128_max` fixture supplies zero and must load raw 0. A present `spawn` with `x` omitted must load raw 32768. Both batches must save back byte for byte. `internal/codegen/rusttable` `TestRustMessageNestedFixedDefault` runs both cases. |

The batch-count rows deliberately judge only the public loader's returned
count. After a refusal, caller storage and the implementation's reason are not
part of the shared contract (SPEC-TABLES.md §§3.3 and 6.5). Run the focused
driver checks after `make conformance-generate` and the corresponding driver
build:

```sh
./build/conformance-harness run --only java --surfaces message
./build/conformance-harness run --only rust --surfaces message
./build/conformance-harness run --only lua --surfaces message
```

### JavaScript read-into names

The shape checker refuses `ConfigLoadInto` because the JavaScript table
emitter binds that name for `Config` ([SPEC §4.6](SPEC.md#46-shape-checks),
[SPEC-TABLES §11](SPEC-TABLES.md#11-refused-by-name)).

```schema
package probe
table Config { count int32 }
type ConfigLoadInto { count int32 }
```

Replace the final line with `type ConfigApplicationState { count int32 }`
for an accepted schema. `TestJSReadIntoDeclarationNames` checks both outcomes.
`TestJsTableModuleScopeNamesAreClaimed` and
`TestJavaScriptVariableRuntimeNamesAreClaimed` inventory the emitted bindings.
The Python, Lua, Luau, Squirrel, and Lobster runtime-name tests inventory
their module or namespace entries separately from the schema-level claims.

### Compressed float packet endpoints

[`RatioEndpoint`](../test/packet-cfloat-endpoints/PacketRatio.schema) declares a closed
`[0, 1]` range at resolution `0.01` ([packet wire §2.5](PACKET-WIRE-ALGORITHM.md#25-compressed-float-write)).
The [endpoint cases](../test/packet-cfloat-endpoints/ratio-endpoints.txt) pin the seven-bit
step indices 0 and 100 and their zero-padded packet bytes `00` and `64`.
`make packet-cfloat-endpoints-go` writes each endpoint through the generated Go
packet writer, checks the exact byte, and reads back the original value.


### Accepted variable-table write bounds

[The focused manifest](../testdata/conformance/table-write-bounds/ACCEPTED-WRITE-BOUNDS.txt)
loads the lawful `TortureVarWref` seed, changes one field or arm per case, and
requires its variable-form measure and save to complete.

| Carrier | Accepted cases |
|---|---|
| Present optional counted array of maximum length 3 | `sfw_opt_arr_ok` |
| Direct `bits(13)` union arm at 8191 | `sfw_arm_bits_ok` |
| Direct ranged integer union arm at 0 and 10 | `sfw_arm_range_min`, `sfw_arm_range_max` |
| Direct string union arm of maximum length 8 | `sfw_arm_str_ok` |
| Enum-keyed `Beta` slot at 0 and 50 | `sfw_slot_min`, `sfw_slot_max` |

Run these cases with `make table-write-bounds-go`.


### Packet union arrays in table closures

[The packet union fixture](../test/tables/packetunions/PacketUnions.schema) covers
arrays of a union whose arms are packet types ([SPEC §4.8](SPEC.md) and
[SPEC-TABLES §§2.6, 3, 3.3, 3.4 and 16](SPEC-TABLES.md)). The shared manifest
registers these canonical cases across the variable, fixed, message and text forms.

| Cases | Form and values |
|---|---|
| `packetunions_doc_full`, `packetunions_doc_empty` | Form 1 document with a full or empty counted array, together with a fixed array. |
| `packetunions_msg_full`, `packetunions_msg_one` | Form 3 records with full or single-element counted arrays. Each also has a form 2 pin over `packetunions_conn`. |

The full cases carry a live `None` between selected packet arms. The counted
array's length includes that element.

### Declared deprecated enum writers

Compile the [SF-26 schema](../testdata/conformance/feature-witnesses/declared-deprecated-enum/Probe.schema).
Its `Phase` enum keeps the deprecated variant `Old` at ordinal 2 and the
live variant `Next` at ordinal 3. Assign each variant directly to the
root's `phase` field through the caller API. A reader may map a deprecated
variant to its default, so constructing the caller value directly is part
of this test. Run with write assertions enabled on targets that use them.

| Operation | `Phase.Next` | `Phase.Old` |
|---|---|---|
| Save one `VarRoot` in Form 1 | `sf26_form1_next` emits the pinned 29 bytes | `sf26_form1_old` refuses |
| Save one `FixedRoot` in Form 3 | `sf26_form3_next` emits the pinned 118 bytes, with ordinal 3 in its enum slot | `sf26_form3_old` refuses |
| Call fixed-root `CookMeasure` | `sf26_cook_measure_next` returns 88 bytes | `sf26_cook_measure_old` also returns 88 bytes |
| Cook one `FixedRoot` in little-endian order | `sf26_cook_next_le` emits the pinned 88 bytes | `sf26_cook_old_le` refuses before writing |
| Cook one `FixedRoot` in big-endian order | `sf26_cook_next_be` emits the pinned 88 bytes | `sf26_cook_old_be` refuses before writing |

[The shared expected answers](../testdata/conformance/feature-witnesses/declared-deprecated-enum/cases.txt)
give each case ID and its complete wire bytes in hex, size in decimal bytes,
or refusal verdict. Compare every output byte for `Next`. For a caller-buffer
Cook API, fill an 88-byte destination with `0x5a` before each `Old` call
and check that every byte retains that value after refusal. A reasoned
fixed-root measure reports success with size 88 for either variant. The
fixed layout determines the size. The writer checks whether the supplied
value may be emitted. These are the enum and fixed Cook rules in
[SPEC-TABLES §2.15](SPEC-TABLES.md#215-enum-variants--deprecated-in-place)
and [§7.6](SPEC-TABLES.md#76-the-write-side-cook-and-cookmeasure).

Run the six fixed-root Cook and CookMeasure cases against generated Go with

```sh
SCHEMA_SLOW=1 go test -count=1 -v ./internal/codegen/gotable -run '^TestDeclaredDeprecatedEnumCook$'
```

### Present optional array grown from two elements to three

Load the [variable-form seed](../testdata/conformance/table-write-bounds/torture_var_wref.bin)
as `TortureVarWref` using the [declaration](../testdata/conformance/table-write-bounds/TableWriteBounds.schema).
The optional `opt_arr` is present and holds `[1, 2]`. For the conformance
driver's `count` edit, grow it to its declared maximum of three elements.
Preserve the prefix and copy element zero into the new slot. Save the root.
The output must match the complete [252-byte expected file](../testdata/conformance/table-write-bounds/expected/torture_var_wref_resized.bin).
Loading it must return a present `[1, 2, 1]`, with every other field unchanged.
A successful save of `[1, 2, 0]` fails this test. The fill rule defines the
test driver's edit. A port may use any caller API that constructs that value.

Run `make table-optional-array-resize-go` to exercise the generated Go writer
and reader. The expected file is also the comparison input for a new port.

### Explicit zero and omitted limits

Use a fresh receiving vocabulary for each [§3.3 announcement case](SPEC-TABLES.md#33-the-message-form-a-batch-of-bitpacked-bodies-under-one-announced-vocabulary).
Use an ordinary empty vocabulary and an ordinary nonempty vocabulary
that fits the defaults. Let `E` and `L` be the nonempty vocabulary's entry
count and payload length in bytes. Keep the other limit sufficient while
varying the limit under test. Use the same announcement bytes throughout
each column.

| Receiver setting | Empty vocabulary | Nonempty vocabulary |
|---|---|---|
| Construction defaults, sufficient storage | Accept, count 0 | Accept, count `E` |
| Explicit entry limit 0 | Accept, count 0 | Refuse |
| Explicit entry limit `E` | Accept, count 0 | Accept, count `E` |
| Explicit entry limit `E - 1` | Accept, count 0 | Refuse |
| Explicit byte limit 0 | Accept, count 0 | Refuse |
| Explicit byte limit `L` | Accept, count 0 | Accept, count `E` |
| Explicit byte limit `L - 1` | Accept, count 0 | Refuse |
| Both limits explicitly 0 | Accept, count 0 | Refuse |

A refused first announcement leaves `announced` false. Its outer framing
still occupies bytes for the empty vocabulary. Measure `L` inside that
framing. On APIs with separately allocated storage, keep enough storage
for the nonempty vocabulary even in the explicit-zero tests.

The generated Java witness derives `E` and `L` from a compiler-produced
nonempty vocabulary and exercises the same empty and nonempty announcements
under independent entry and byte limits. Run it with a JDK available:

```sh
SCHEMA_SLOW=1 go test -count=1 -v ./internal/codegen/javatable -run '^TestAnnouncementBudgetsJava$'
```

For [§3.4 fixed-file reads](SPEC-TABLES.md#34-the-fixed-form-a-record-of-values-under-a-layout),
use the same [one-record file](../testdata/wire/fixed-empty-batch/one-record.bin)
in every row. It stores `a = 7` and `name = "hello"` in the
[Batch declaration](../test/fixed-empty-batch/Batch.schema). This file
format requires at least one record.

| Fixed reader capacity | Result on one record |
|---|---|
| Omitted, on an API that permits omission | Accept, return one record |
| Explicit 0 | Refuse for insufficient capacity |
| Explicit 1 | Accept, return one record |

An implementation whose fixed reader requires a caller-provided destination
runs the explicit-capacity cases with room for zero and one record respectively.

The [fixed-capacity cases](../testdata/conformance/feature-witnesses/fixed-load-capacity/cases.txt)
also exercise the typed empty-save call, which must refuse before emitting a
file. Python exercises omission and both explicit capacities. Go uses the
length of the required destination slice and also checks that the empty
save leaves the supplied buffer intact.

Run this command from the repository root after preparing the Go runtime
with `make deps` and the pinned interpreters with `make dist-python`.
An existing `../serialize.go` checkout or an absolute `SERIALIZE_GO` path
also supplies the Go runtime. Set `PYTHON` and `PYTHON_FLOOR` to existing
pinned executables to use them in place of the local Python builds.

```sh
SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -v \
  ./internal/codegen/gotable ./internal/codegen/pythontable \
  -run 'Test(Go|Python)FixedLoadCapacityWitness'
```
