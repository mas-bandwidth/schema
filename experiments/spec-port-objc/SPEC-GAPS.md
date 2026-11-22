# Objective-C spec port: where the specification was not enough

This port was written from the specification alone: `docs/SPEC*.md`,
`docs/USAGE.md`, `docs/FIXED-FORM-ALGORITHM.md`, `test/conformance/README.md`
(the driver protocol), and the data under `testdata/`, `tables/` and `test/`
(schemas, wires, goldens, locks). The spec text came from branch
`rowan/fix-ledger-1122` at `8b048d589`, which includes the new-implementer
addendum `docs/SPEC-CONFORMANCE.md`. No other language's runtime, generator or
driver was read. The C, C++ and other ports were not consulted at all.

Each entry gives the spec line, the question I would have had to ask a
person, what I chose and why, and the kind of gap. Each choice is marked in
the code as `SPEC-GAP n`.

| n | spec file:line | the question | what I chose, and why | kind |
|---|---|---|---|---|
| 1 | SPEC.md:3233 (§7.1 "IR — the load-bearing design decision") | A port has to get a schema from somewhere. The IR is internal to the compiler, and the spec names no exported form of it (no JSON IR or descriptor dump). What is a new port supposed to consume? | I parse `.schema` text myself from §4.1–§4.3: a scanner, an LL(2) recursive-descent parser and a resolver (`src/SPSchema.m`, about 850 lines). Constant folding uses 128 bits, not §7.1's arbitrary precision. That is a port limit, and nothing in the corpus reaches it. | SILENT |
| 2 | SPEC.md:556 (§4.1 "Right of `\|`, ALL suppression is off … the newline always terminates") | The corpus's `feature-witnesses/full/Features.schema` writes `table Root \| forms="wire,json,cook", integrity="crc32c" {`, with the body's `{` on the pipe's line. Does a `{` end a qualification section? | I followed the corpus: a `{` also ends the section. Without that, the unit does not load. | VECTOR-DISAGREES |
| 3 | test/conformance/README.md:753 ("Drivers may emit `refused` or the historical `<case>.absent` file") | How is a semantic refusal spelled on the `wire` surface (a `wire-refused` instance)? | Both `refused\n` and `refused` scored "expected wire refusal, got bytes". Only `<case>.absent` passes, so the driver writes that for every refusal on `wire`, `json-read` and `json-write`. As a result, a refusal on those surfaces cannot be told apart from a missing feature. | CONTRADICTION |
| 4 | SPEC-TABLES.md:16570 (§16.2 "A SCHEMA-NAME STRING IS CAPPED AT 255 BYTES … in every object and inside a skipped value") | The cap applies to names and never to map keys. A skipped value has no schema, so which of its keys are names? | Every object key inside a skipped value is capped at 255 bytes. | AMBIGUOUS |
| 5 | SPEC-TABLES.md:17126 (§16.7 "A SKIPPED VALUE IS READ FOR ITS LABELS AT EVERY DEPTH") | §16.7 rules on `&node` inside a skipped value. Does an `&`-prefixed key under any other spelling, inside a skipped value, refuse as it does where a value is placed? | Let through. Only `&node` is policed inside a skipped value, because a skipped map's keys may legitimately begin with `&` (§16.2's map row). | SILENT |
| 6 | FIXED-FORM-ALGORITHM.md:164 (§2 step 6 "If `rest / record_bytes` passes the caller's capacity, `REFUSE batch_too_large`") | What capacity does a plain single-root `Load` have? | One. A two-record file refuses `batch_too_large`, and `fixedevo_v3_batch2` owes `refused`. | AMBIGUOUS |
| 7 | FIXED-FORM-ALGORITHM.md:324 (§4.1 "A DEPRECATED SLOT is not a dead byte"), with §4.5 | What does a deprecated enum variant read as on form 3? Form 1 and the text form say: the field's declared default. | The same rule as form 1: the field's declared default. | SILENT |
| 8 | SPEC-TABLES.md:104 ("A form-`1` file handed to a fixed root is `previous_form`") | The corpus contradicts itself here. The report rows `previous_form_*` (for example `previous_form_r1_cfg`, at `tblr1 Cfg`) owe `refused`. Fifteen framing rows at the same fixed root (for example `frame_empty_table`, `union_void_arm`) hand a form-1 file and owe `read` plus a form-3 write-back. | I followed each row. The `framing` surface reads form 1 at a fixed root, and every other read refuses it as `previous_form`. One of the two row sets is stale against "The three wires". | CONTRADICTION |
| 9 | SPEC-TABLES.md:17126 (§16.7: "a reference to a label not yet defined … refuses the whole read" inside a skipped value) | `json-hostile reserved-ampersand-in-skipped-value` (`{ "mystery": { "&node": 1 } }` at the FIXED root RootConfig) owes `read`, but the text is a reference to a label that was never defined. | I followed the corpus. A fixed root's read, whose closure holds no pointer, does not police labels inside a skipped value. Variable roots do, and all `label-*` rows pass. | VECTOR-DISAGREES |
| 10 | FIXED-FORM-ALGORITHM.md:198 (§3 "a UNION \| `tag width + max(C(arm))`") | How wide is a fixed-form union tag? Note (a) gives an enum's ordinal width from its top wire value, but nothing gives a union's. | I used the enum's rule with the arm count as the top ordinal (1/2/4/8 bytes). All 90-odd lock hashes this port computes match `schema.lock`, which confirms the choice after the fact. | SILENT |

**Counts by kind:** SILENT 4, AMBIGUOUS 2, CONTRADICTION 2, VECTOR-DISAGREES 2,
DEFERS-TO-CODE 0.

## Not gaps, but worth a line

- **The fixed form's definitions digest (FIXED-FORM-ALGORITHM §5.2, the v2
  rows) was enough as written.** My layout and digest reproduce every
  `lineage wire=` hash in the corpus's `schema.lock` files on the first try.
- **The lineage plan compiler (§5.2 PLAN/MATCH) is not carried.** A file under
  an older hash in the table's lineage answers `.absent`; its hash is read from
  `schema.lock`. That covers 5 `report` rows. A hash in no lineage refuses
  `layout_unknown`.

## The Objective-C notes (the brief's stress points)

- **NSString versus raw bytes.** Text is held as `NSData`, never as
  `NSString`. An NSString would replace ill-formed bytes or carry a lone
  surrogate, and every rule in §3, §16.3 and §5.1 row 8 is about bytes. The
  JSON reader takes bytes and validates UTF-8 itself (§16.2). Map keys compare
  with `memcmp` and then by length (W6), never with `-compare:`, which orders
  UTF-16.
- **NSNumber and boxing.** No value is boxed in NSNumber. Every integer kind,
  ordinal, mask and fixed-point raw value is an `SPInt` holding 128 bits, so a
  `uint64` above 2^63 and a `uint128` survive. An optional's presence is a
  separate `present` flag. An absent optional element, a null pointer and a
  payload-free arm are `NSNull`.
- **nil messaging.** A refusal is an `NSException` named `SPRefusal`, thrown
  and caught at the driver. It is never a nil return, because `[nil count]` is
  0 and a refusal read as 0 is a lost refusal. Any other exception is a defect:
  the driver writes no answer for that case, so the matrix shows a failure.
- **ARC.** The build uses `-fobjc-arc -fobjc-arc-exceptions`, so an exception
  thrown mid-read releases what it unwinds past. Pointer fix-ups capture the
  slot's owner `__weak`, so the reader's pending list makes no cycle.
- **Write contract.** As a new language, this port refuses in every build
  (SPEC.md §5.1).
