# Spec gaps met by the Zig spec-only port

The port was written from `docs/SPEC*.md`, `docs/USAGE.md`,
`docs/FIXED-FORM-ALGORITHM.md`, the driver protocol in
`test/conformance/README.md`, `testdata/conformance/tables/FORMAT.md` and the
normative data files, on branch `rowan/fix-ledger-1122` (PR #4706). No other
language's runtime, generator or driver, nothing under `internal/`, and no
`docs/PORT*.md` was opened. Each entry is a question this port would have had
to ask a person. Code that follows a vector against the text says
`SPEC-GAP n` at the place.

Kinds: AMBIGUOUS (two readings fit the text), SILENT (the text says
nothing), CONTRADICTION (two normative places disagree), DEFERS-TO-CODE (the
text names code as the authority), VECTOR-DISAGREES (a corpus row answers
against the text).

## 1. There is no machine-readable schema for a port to start from
- **Where:** SPEC-TABLES.md §11 (14045, WHAT A NEW PORT OWES) and
  SPEC-CONFORMANCE.md §3; the derived manifest's `unit` lines name `.schema`
  files and directories.
- **Question:** what does a new port consume: a serialized IR from the
  compiler, or the schema text? Nothing names an IR, and the driver protocol
  hands a schema path at run time.
- **Chosen:** the port parses the schema language itself (SPEC.md §4) and
  interprets the declarations at run time. That is also why it is not
  comptime-generated per schema: Zig's comptime needs the schema at build
  time, and the protocol supplies it at run time. A generator port would need
  a build step per manifest, which the protocol does not describe.
- **Kind:** SILENT.

## 2. The declaration grammar and the corpus disagree
- **Where:** SPEC.md 596-600 and 644-650 (`AttrSection NL Block`: "a
  qualified declaration's body opens on the NEXT line"), SPEC.md 951.
- **Question:** the corpus writes `fixed table Disabled | forms="wire" { value
  int32 }` (testdata/conformance/feature-witnesses/forms/Disabled.schema:2)
  and `table ArrayRoot | json="array" {` on one line. `fixed table` is not in
  the `TableDecl` production, and neither are `[]T`, `[Min..N]T`, `*bytes`,
  `*string` or unbounded `string`, which §2.x of SPEC-TABLES.md define and the
  corpus uses.
- **Chosen:** accept `{` ending a qualification section on the same line,
  accept `fixed table`, and take the missing productions from SPEC-TABLES.md
  prose.
- **Kind:** CONTRADICTION.

## 3. The harness reports success for a driver that answers nothing
- **Where:** SPEC-CONFORMANCE.md §2 ("An absence on a required surface is a
  failure, not a gap"), §5 ("exits nonzero on any failure").
- **Question:** a stub driver that lists one surface and exits 2 for it gets
  every cell `absent` and the footer "every registered surface passes", exit
  0. Which is right?
- **Chosen:** the addendum. Every absence in this port's matrix is reported
  as work not done, never as a pass.
- **Kind:** CONTRADICTION.

## 4. Does a run-time port owe the compiler's refusals?
- **Where:** SPEC-TABLES.md §11 (refused by name) and SPEC.md §4.
- **Question:** a port that reads `.schema` text meets declarations the
  compiler refuses (a `required` in a fixed table, a `&` json key, a by-value
  nesting past 128). Is the port a compiler and owes those refusals, or may it
  assume a unit the reference compiler accepted?
- **Chosen:** assume an accepted unit; refuse only what the parser cannot
  resolve.
- **Kind:** SILENT.

## 5. How deep is a union whose arm is a table?
- **Where:** SPEC-TABLES.md 9102-9110 (ONE NESTING BOUND).
- **Question:** "a union whose arm holds one of those" is one level. Is the
  arm's table body then one more (union + body = 2), or is the union's level
  the body's level (= 1)?
- **Chosen:** 2 (the union is a level and the body inside it another). No row
  in the corpus distinguished the readings for this port.
- **Kind:** AMBIGUOUS.

## 6. Form 1 at a declared fixed root: refused or read?
- **Where:** SPEC-TABLES.md 102-119 ("NEITHER CLASS BORROWS THE OTHER'S
  FORM"; "the conformance corpus's historical form-1 fixed-root files are now
  `previous_form` refusal rows that owe `refused`").
- **Question:** the `report` rows `previous_form_*` refuse form 1 at a fixed
  root, as the text says. The `framing` battery (MANIFEST.txt 1169-) loads
  form-1 files at `tblr1 Cfg`, which is `fixed table Cfg` (test/tables/R1.schema),
  and owes values and a 487-byte write-back for 15 rows
  (`frame_empty_table`, `frame_entries_4096`, `nested_L_exact`, ...).
- **Chosen:** the text and the report rows: form 1 at a fixed root refuses.
  The 15 framing rows fail in this port's matrix and are the whole of its
  `framing` failure.
- **Kind:** CONTRADICTION (and VECTOR-DISAGREES for the framing rows).

## 7. An undefined `&node` reference inside a skipped value
- **Where:** SPEC-TABLES.md 17126-17131 ("every port refuses the whole read
  ... on ... a reference to a label not yet defined", inside a skipped value
  too).
- **Question:** the json-hostile row `reserved-ampersand-in-skipped-value`,
  `{ "mystery": { "&node": 1 } }` with `mystery` unknown, owes `read`.
- **Chosen:** the vector: an undefined reference inside a skipped value is
  tolerated (`src/json.zig`, SPEC-GAP 7). Other label rules stay enforced
  there.
- **Kind:** VECTOR-DISAGREES.

## 8. How a driver says "refused" on the `wire` surface
- **Where:** test/conformance/README.md 751-756 ("Drivers may emit `refused`
  or the historical `<case>.absent` file").
- **Question:** the harness scores a `wire-refused` instance answered with
  the bytes `refused\n` (or `refused`) as "expected wire refusal, got bytes";
  only `<case>.absent` passes. The `report`, `framing` and `json-hostile`
  surfaces take `refused\n`.
- **Chosen:** `<case>.absent` for every wire-surface refusal
  (`src/main.zig`, SPEC-GAP 8). The cost: on that surface a wrong refusal
  now reads as an absence rather than a failure, so the stderr log names
  every refusal with its reason.
- **Kind:** CONTRADICTION.

## 9. The 255-byte name cap inside a skipped value
- **Where:** SPEC-TABLES.md 16570-16578.
- **Question:** the cap reaches "an object key that names a field ... inside
  a skipped value" and "never reaches a map key". Inside a skipped value the
  reader has no declaration, so it cannot tell a table's object from a map's.
  Which keys does the cap reach there?
- **Chosen:** every key inside a skipped value (strict).
- **Kind:** AMBIGUOUS.

## 10. `wide:<field>` and `units` name a construct the spec retired
- **Where:** FORMAT.md 84 and 101 against SPEC-TABLES.md 4860 ("`33` IS
  RETIRED: wide text is not part of schema").
- **Chosen:** no wide text; a row naming one would be answered absent (none
  in the derived manifest uses it today).
- **Kind:** CONTRADICTION.

## 11. A `count` row on an empty array
- **Where:** FORMAT.md 97 ("a new element is a copy of element 0").
- **Question:** what is a new element when the loaded array has none?
- **Chosen:** the element's default.
- **Kind:** SILENT.

## 12. A write-refusal path through a union arm
- **Where:** FORMAT.md 94-96 ("`<path>` is dotted field names with `[i]`
  element indexes").
- **Question:** rows such as `weapons[0].effect.buff.multiplier` would pass
  through a union; the grammar names no arm step.
- **Chosen:** a segment naming an arm selects it (at its defaults when it is
  not the current arm), then continues into the payload.
- **Kind:** SILENT.

## 13. What bounds a reader's memory
- **Where:** SPEC-TABLES.md §2.2 (int32 extent cap, 2^31), §3 (4096 ids),
  9102 (depth 128); the brief's stress point "no hidden allocation".
- **Question:** the caps alone do not bound memory: 2^31 elements of a table
  array is far past any input. What bounds it is that every element costs at
  least one byte of the body that frames it, and a count the body cannot
  cover is damage. The text states the per-kind framing, not the bound.
  Separately, `[N]T` pads to `N` from the declaration, so a schema can ask a
  reader for an allocation no input paid for.
- **Chosen:** refuse a count larger than the bytes left in its body before
  allocating; allocate exactly `N` for `[N]T`. Memory is then at most
  (input bytes + declared fixed extents) times the value size.
- **Kind:** SILENT.

## 14. Integer overflow behaviour
- **Where:** SPEC.md §4.2 (constants: arbitrary precision, fit-checked at
  use), SPEC-TABLES.md §4 (out_of_range), §16.2 (Numbers).
- **Question:** Zig makes every overflow choice explicit (wrapping,
  checked, saturating). The spec settles all of them by refusal: no value is
  wrapped or clamped anywhere. Two places it does not say: the arithmetic of
  a constant expression that overflows the compiler's own precision, and
  what an unsigned `bits(64)` read of a kind-9 payload does with the top bit.
- **Chosen:** checked arithmetic everywhere, a refusal on overflow;
  two's-complement truncation only where bytes are packed little-endian.
- **Kind:** SILENT.
