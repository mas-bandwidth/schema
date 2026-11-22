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

## Silent-failure rows (2026-11-22)

Nothing a writer or a reader is handed may fail silently. Each rule below was
fixed in one port on 2026-11-22 and is a rule for every port. The finding, its
spec section and its per-port test are in `docs/silent-failure-audit-1122.md`
(the SF ids). Where no corpus case carries a rule yet, the table says so, and
the rule is held per port by the named test until a case is added.

| rule | spec | shared rows |
|---|---|---|
| SF-03, SF-04: the variable `Save` and `Measure` check rows 1–2 on every field, not only on `\| required` fields | SPEC.md §5.1 rows 1–2 | `write-refusal` (`wref_range_*`, `wref_bits_wide*` on a fixed root); `sfv_bits_wide` / `sfv_bits_ok` on the variable root `TortureCook` (a plain non-required `bits(13)`, 8192 refused and 8191 written); a ranged non-required `int` on a variable root is owed: no corpus unit with a `min` / `max` on a variable root has a write-refusal path in the hand-coded legs |
| SF-07, SF-08: the packet `Write` checks row 8 (text) and row 10 (compressed float) in every C# target, unity included | SPEC.md §5.1 rows 8, 10 | `write-refusal-packet` (`text_illformed`, `text_zero`, `text_long`); a compressed-float row (SF-08) is owed: `PacketWriteRefusal` declares no compressed float, and no C# leg runs this surface yet |
| SF-09 to SF-15: a write check reaches every carrier: a plain field, an optional, an array element, a union arm, a keyed slot | SPEC.md §5.1 rows 1–4, 8 | `write-refusal` (`wref_*`); on the variable root `TortureCook`, SF-10 `sfv_opt_bits_wide` / `sfv_opt_bits_ok` (an optional `?bits(37)`) and SF-12 `sfv_elem_bits_wide` / `sfv_elem_bits_ok` (a `bits(37)` element of `arr_bits`, a `[]T` array); owed because no corpus unit declares the shape on a variable root: SF-09 (a plain `bytes(N)`), SF-13 (an over-long optional array, arm array or arm `bytes(N)`), SF-14's `string(N)` arm and SF-15 (a keyed-array scalar slot with a range or width); SF-14's scalar arm is owed too: the only variable-root arm that carries a width is `bound_unions[13].u_bits`, an arm inside a list element, which the walkers of the Lua, Luau, MOO, Goose, Squirrel and Lobster drivers do not reach |
| SF-17: `SaveMessages` checks row 10 and never moves an off-range float onto an endpoint | SPEC.md §5.1 row 10 | `write-refusal-message`; a float case is owed: no corpus unit declares a ranged or compressed float, so no row can carry one |
| SF-18, SF-19: the message form refuses a flags value above its declared width and never masks it | SPEC.md §5.1 row 9; SPEC-TABLES §3.3 | a `write-refusal-message` flags case is owed: the only message-capable root with a flags field is `messagedemo` `ToolMessage` (the `caps` arm), which has no `connection` row and no write-refusal-message root in the hand-coded legs (C, C++, Rust, Dart) |
| SF-20: a writer refuses a `[N]T` or `[E]T` whose length is not N and never cuts or pads it | SPEC.md §5.1 | a case on `write-refusal-fixed` and `write-refusal-message` is owed: the `count` kind needs a companion count field and a `[N]T` has none, so every leg's `count` handler must first learn to answer `unrepresentable` or resize a fixed array |
| SF-21: a writer refuses a counted array below its `Min` | SPEC.md §5.1 row 3 | a case on `write-refusal-fixed` is owed (`sp-count-min-*` carries the read side): no corpus unit declares a `[Min..N]T` |
| SF-25, SF-26: `Cook` and `CookMeasure` hold the write contract that `Save` holds; a deprecated enum variant is refused on write | SPEC-TABLES §7.6, §2.15 | `cook-write`; a refusal case is owed: `cook-write` has no refusal verdict and no corpus unit declares a deprecated enum variant |
| SF-22, SF-28: a variable save and a text (JSON) writer refuse a counted array below its `Min`, and a list field holding a non-list | SPEC.md §5.1 row 3 and "A writer refuses in EVERY form" | a `write-refusal` `count` row below a `[Min..N]` Min is owed: no corpus unit declares a counted array with `Min > 0` on a variable root, so it needs a new unit; SF-28 cannot be a row (a statically typed leg cannot hold a list field that is no list) and is held per port |
| SF-23: `SaveMessages` holds a value to the rule of the declaration it is written under, not to the looser rule of a same-named field elsewhere | SPEC-TABLES §3.3 | a `write-refusal-message` `int` row on two declarations that share a field name and kind at different ranges is owed (new unit) |
| SF-27: the text read maps a deprecated enum variant to the field default (an element: None) | SPEC-TABLES §2.15 READ | owed: the text-form corpus (`testdata/conformance/text`) carries strings only, so an enum text case needs the text surface extended |
| SF-29, SF-37: a cook refuses a loaded count outside `[Min, bound]` and a NaN or an infinity in a plain float | SPEC-TABLES §7.6; SPEC.md §5.1 row 7 | `cook-write`; a refusal case is owed; SF-29 cannot be a row (a caller-edited loaded value, not an instance) |
| SF-31: the block builder refuses a count or a used length past its maximum by name in every build | SPEC-TABLES §19.1 | none: no corpus surface builds a block from values (`block` surfaces read images) |
| SF-32, SF-33: tooling (moo-analyze run with no output, Luau bitpacker probe) | none: not a wire rule | none |
