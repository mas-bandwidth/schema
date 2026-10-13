# The card conformance set — the file shapes

A language-neutral data set under `tables/cards/conformance`, a Go reference
driver that writes its expected answers, and a `cards` subcommand of the
existing conformance harness that runs any language's driver over it and
compares bytes. The abstract claim is `tla/CardHops.tla` — `ImplementationsAgree`
and `HopOrderIrrelevant`: every leg reads, writes, renders, upgrades and hashes
one card to the same bytes. This check is that claim's concrete side, and the
harness subcommand cites both properties in its comment.

The driver contract is the existing one (test/conformance/README.md, "The
shape"): a driver is a command run from the repository root as

```
<driver> <manifest> list
<driver> <manifest> <surface> <outdir>
```

- `<manifest>` is a DERIVED manifest the harness writes into `build/`: the
  committed one with the input paths folded in and every expected answer
  removed, so a driver cannot pass by reading an answer.
- `list` prints the surfaces this leg implements, one per line.
- A surface run writes ONE FILE PER CASE into `<outdir>`, named by the case,
  and prints nothing that matters. The harness holds the expectations and
  compares byte for byte.
- Exit `0` means the surface ran; exit `2` means the surface is absent; any
  other exit is a failure.
- A single case may be absent too: a driver writes an empty `<case>.absent`
  beside where the answer would go.

## The manifest

`MANIFEST.txt` lists one case per line:

```
<surface> <name> <input> [edit...]
```

`<input>` is a repo-relative path to the case's input file (a wire, JSON or
brief text), except for `view`, whose input is `-` (the view needs no input).
`[edit...]` is present only on `edit` lines: one edit, spelled as
`needs=a,b`, `tier=heavy`, `priority=blocker`, `paths=x,y`, `column=review` or
`holder=h`, applied through the Set* edit of the same name.

## The surfaces

Nine surfaces, one file per case (two for `load` and the successful `upgrade`).
The table names the file the driver writes per case; `X` stands for the case's
name.

| surface | input | the driver writes | answers |
|---|---|---|---|
| `load` | wire | `X.json` and `X.report` | `Load` then `ToJSON`, the JSON text form (docs/SPEC-TABLES.md §16); the report is the read report |
| `save` | JSON | `X.wire` | `FromJSON`, the record's wire bytes |
| `render` | wire | `X.txt` | `Render`, the JOB.md text |
| `parse` | brief text | `X.wire` or `X.refused` | `Parse` |
| `bounds` | JSON | `X.wire` or `X.refused` | `FromJSON` at and over the customer bounds |
| `upgrade` | wire | `X.wire` and `X.report`, or `X.refused` | `Upgrade` |
| `edit` | wire + one edit line | `X.wire` or `X.refused` | one Set* edit |
| `taskhash` | wire | `X.hex` | `TaskHash`, 64 lowercase hex digits |
| `view` | `-` | `view.txt` | the generated view (docs/SPEC-TABLES.md §8) |

The customer bounds the `bounds` surface pins are `MaxBodyBytes` (65536 body
bytes) and `MaxListEntries` (512 entries per collection), declared in
tables/cards/cards.go.

### The read report

`X.report` is one `Name=value` line per counter of the Go `TableReport`
(tables/cards/cards.go, `type Report = TableReport`), in the order the Go type
declares its fields and spelled as the Go fields are:

```
Retained=<n>
RetainLost=<n>
Widened=<n>
Unknown=<n>
KindMismatch=<n>
Clamped=<n>
Duplicate=<n>
Malformed=<true|false>
```

The strings `Reason` and `Verdict`, the two build-version fields and the
layout hash are not counters and never ride. A clean read is seven zeroes and
`Malformed=false`.

### The view

`view.txt` is one line per field of every table in the unit, tables in the
generated view's order (sorted by table name) and each table's fields in
declaration order:

```
<table> <field> <kind> <shape> <bound>
```

`<kind>` is the schema element type (e.g. `string`, `int32`, `PathText`).
`<shape>` is `value` for a plain or counted field, `list` for an unbounded
collection, and `blob` for a pointer field. `<bound>` is the declared string or
array bound, or `-` when the field has none.

### Refusals

A `.refused` file holds ONE line: a reason CLASS from this closed list, never
free text, so every language can match it:

| class | means |
|---|---|
| `body-bound 65536` | the body is past the 65536-byte ceiling |
| `list-bound <field> 512` | the named collection is past the 512-entry ceiling |
| `stranded-key <KEY>` | a header-shaped key appears after prose has begun |
| `retain-lost` | an upgrade would drop a retained field |
| `unknown-present` | an edit over a record carrying an unknown field |
| `malformed` | the wire or JSON is not a well-formed card |

`retain-lost` has no case yet: the public `Upgrade` facade fixes the retention
capacities at `MaxRetainBytes`/`MaxRetainIDs`, so the create target cannot build
a wire that drops a retained field through it. The class is listed so a leg can
spell it the day such a case lands.

## The leg contract

Every leg's `cards-<lang>-conformance` target runs the harness's `cards`
subcommand with `--require $(CARDS_REQUIRE)`. `CARDS_REQUIRE` (defined in
make/go.mk) defaults to all nine surfaces:

```
CARDS_REQUIRE ?= load,save,render,parse,bounds,upgrade,edit,taskhash,view
```

A leg's card targets live in that leg's own make fragment (make/go.mk for go,
make/cpp.mk for cpp, and so on), never in a cards-only fragment and never
behind a root Makefile include line.
