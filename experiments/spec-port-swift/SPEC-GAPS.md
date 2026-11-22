# Swift spec port — gaps

These are the questions this port would have had to ask a human. It read only the
spec (docs/SPEC*.md, docs/USAGE.md, docs/FIXED-FORM-ALGORITHM.md, and the driver
section of test/conformance/README.md) and the normative data under test/ and
testdata/. The spec came from branch `rowan/fix-ledger-1122`.

Line numbers refer to that branch.

Each entry gives: n, spec file:line, the question, what I chose and why, and kind.

1. **docs/SPEC.md:140 / docs/SPEC-CONFORMANCE.md**
   - Question: what does a port consume? The spec does not define a serialized IR or descriptor format that a new runtime could read, and the driver manifest names `.schema` source files.
   - Chose: the port parses `.schema` text itself (Schema.swift), which means building a second front end.
   - Kind: SILENT / DEFERS-TO-CODE.
2. **docs/SPEC.md §4 (compile refusals)**
   - Question: must a runtime port re-run the checker's compile-time refusals?
   - Chose: no. The port assumes every unit it reads has already been checked, and only applies the runtime rules.
   - Kind: SILENT.
3. **docs/SPEC.md:532, 584, 917, 939, 952**
   - Question: the spec says a `|` qualification "claims the rest of the line", but test/tables/Features.schema puts `{` on the same line after a qualification.
   - Chose: a qualification section also ends at `{` or `}`. This follows the corpus.
   - Kind: CONTRADICTION. Code: `// SPEC-GAP 3` in Schema.swift attrs().
4. **test/conformance/README.md:97, 753**
   - Question: how does the `wire` surface spell a refused instance? The README says drivers "may emit `refused` or the historical `<case>.absent`".
   - What the harness did: a `refused` answer was judged as bytes, and `.absent` is counted ABSENT rather than PASS. blob_shared and blob_shared_string therefore cannot score.
   - Chose: `.absent`.
   - Kind: AMBIGUOUS / VECTOR-DISAGREES. Code: main.swift wire case.
5. **docs/SPEC-TABLES.md:5211, 5229**
   - Question: does the first-use id order count ids that appear only inside payloads the reader steps over (unknown fields, skipped arms)?
   - Chose: yes. The writer's walk names every id it wrote, so the reader checks order across the full scan, skipped bytes included.
   - Kind: AMBIGUOUS.
6. **testdata/conformance/tables/FORMAT.md:99**
   - Question: for a `tag` edit, is the "raw tag" 1-based with 0 meaning None, and what does a tag past the last arm hold?
   - Chose: 1-based with 0 = None. A tag past the last arm holds no payload, and the writer refuses it.
   - Kind: AMBIGUOUS.
7. **testdata/conformance/tables/FORMAT.md:82-86**
   - Question: how should a program store `wide:` damage (the units 0061 D800 0062)? A language with Unicode-first strings, like Swift `String`, cannot hold a lone surrogate at all.
   - Chose: storage is bytes, and the lone surrogate is kept as its 3-byte generalized-UTF-8 spelling (ED A0 80), which the writer then refuses.
   - Kind: SILENT. Swift stress point: text is held as `[UInt8]` throughout, never as `String`.
8. **docs/SPEC-TABLES.md §3.4 (THE LAYOUT) and docs/FIXED-FORM-ALGORITHM.md**
   - Question: can a port build the definitions digest without the compiler's IR? The digest is "emitted by the compiler; no runtime computes it", but a port that compiles from source has to compute it, so the hash depends on a compiler walk that a runtime port must reproduce.
   - Chose: did not implement form 3, so every fixed-root case answers ABSENT.
   - Kind: DEFERS-TO-CODE.
9. **docs/SPEC-TABLES.md:111-129 (fixed root reads form 1)**
   - Question: the corpus holds "historical form-1 fixed-root" wires. Should a reader without form 3 decode them as form 1, or refuse them?
   - Chose: decode form 1 at a fixed root for reading, and answer ABSENT for any save at a fixed root.
   - Kind: AMBIGUOUS.
10. **docs/SPEC-TABLES.md §22.2 (elision) with optional `?T` of a table**
    - Question: is a present optional table whose body is empty elided?
    - Chose: no. Presence is the field riding, so a present optional always rides, even with an empty body.
    - Kind: SILENT.
11. **docs/SPEC-TABLES.md §16.3 (float32 text)**
    - Question: when a float32 field holds a value set by a program as float64, does the JSON writer round to float32 first?
    - Chose: yes, it writes `Float(x)` with the float32 precision loop.
    - Kind: SILENT.
12. **test/conformance/README.md (json-write absent marker)**
    - Question: is the absence marker for a json-write case `<case>.json.absent` or `<case>.absent`?
    - Chose: `<case>.json.absent`. The harness accepted it as ABSENT.
    - Kind: AMBIGUOUS.
13. **docs/SPEC-TABLES.md §3 (arm payload kinds)**
    - Question: the list of kinds an arm payload may carry does not say whether kind 16 (keyed) is allowed in an arm.
    - Chose: allow it wherever the arm's declared type is an enum-keyed array.
    - Kind: SILENT.
14. **docs/SPEC.md §2 (guard blocks)**
    - Question: the grammar does not say whether a field line may follow the `}` of an `if` block with no `else`. test/tables/G1.schema does this.
    - Chose: yes. A missing `else` returns to field lines. I found this bug in my own parser through 4 wire instances.
    - Kind: SILENT.

Count by kind (an entry with two kinds counts once, under its first):

| kind | count |
|---|---|
| SILENT | 7 |
| AMBIGUOUS | 5 |
| CONTRADICTION | 1 |
| DEFERS-TO-CODE | 1 |

Gap 4 also has a VECTOR-DISAGREES side, and gaps 1 and 8 both lean on the compiler as the definition.

## Rulings: "if in doubt, go strict"

- A JSON text refuses a repeated key at every depth.
- A wire refuses a field that appears twice in one body. The corpus names a "repeat last wins" case, and it scores as a refusal.
- Text is checked as bytes. A zero byte in text refuses (`text_holds_zero`) unless the spec admits it.
- Every integer path uses arithmetic that reports overflow (Big, `addingReportingOverflow`), so no input can trap.
- R4: every read builds a fresh `Rec` and only returns it once the whole read succeeds, so a refused read leaves the caller's value as it was.
