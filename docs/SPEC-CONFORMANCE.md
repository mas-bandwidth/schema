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
| FL-11: the text writer's depth cap counts table and type values only | `wire`, `json-read`, `json-write` | `surf_chain128_vals`, `surf_chain127_vals`; refusal `graph_chain_past_cap` |
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
