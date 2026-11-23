# Spec gaps met by the Common Lisp port

This port was written from `docs/SPEC.md`, `docs/SPEC-TABLES.md`, `docs/USAGE.md`,
`docs/FIXED-FORM-ALGORITHM.md`, the driver protocol in `test/conformance/README.md`,
`testdata/conformance/tables/FORMAT.md` and the normative data files, and from nothing
else. Each entry below is a place where those sources did not settle an answer on their
own. Each one gives the spec line, the question, what this port chose and why, and a
kind:

- AMBIGUOUS: more than one reading fits the text.
- SILENT: the text does not say.
- CONTRADICTION: two places in the sources disagree.
- DEFERS-TO-CODE: the text points at an implementation instead of stating the rule.
- VECTOR-DISAGREES: the corpus answers differently from the plainest reading of the text.

The code marks each one with `;; SPEC-GAP n`.

| n | kind | where (file:line) | question | chosen, and why |
|---|---|---|---|---|
| 1 | SILENT | SPEC-TABLES.md:9477 (§5) | `fnv1a64( name )`: which bytes of the name are hashed? The encoding is never stated. | UTF-8 bytes. Every name in the corpus is ASCII, so nothing tests this choice. |
| 2 | DEFERS-TO-CODE | SPEC.md:3158 (§7.1) | Backends "consume only the IR", but the IR is a Go package and nothing serialized is specified. How does a port in another language get the resolved schema? | This port parses `.schema` text itself, from the §4.1–§4.2 grammar, and does its own resolution: constants, `E.Max`, guards, defaults, one-sided ranges. This cost the most time of any gap. |
| 3 | CONTRADICTION | SPEC.md:530, SPEC.md:582 vs testdata/conformance/feature-witnesses/full/Features.schema:6 | §4.1 says an attribute pipe "claims the rest of the line", and SPEC.md:582 refuses a brace after a qualified variant for that reason. Yet the corpus writes `table Root \| forms=..., integrity="crc32c" {` with the brace on the same line. | An attribute section ends at a newline or at `{`, so the corpus parses. |
| 4 | AMBIGUOUS | SPEC-TABLES.md:8776 (§4 nesting bound) | "A union whose arm holds" a structural payload is a level. Is the arm's own table body then a second level? | The union counts one level and the arm body counts one more. No corpus row sits on the boundary, so nothing tests this. |
| 5 | CONTRADICTION | SPEC-TABLES.md:4948 vs SPEC-TABLES.md:5955 | §3 "THE HEADER INSIDE THE BODY" puts kind 16's header (element kind and count) inside the field's `L`. §3.2 says its count is read "against the ENCLOSING body and never against the field's own `L`". | Read inside `L`, as §3 states for all three body kinds. All 70 form-1 wire goldens pass with this reading. |
| 6 | SILENT | SPEC-TABLES.md:6900 | A compressed float "lands inside its declared range" on read, but the range is decimal (`min = 0.1`) and the stored value is binary. Compared exactly, or after rounding the bounds to the field's width? | The bounds are rounded to the field's width (ties to even), then compared exactly with the stored value. This applies on the wire read, the JSON read and the JSON write. |
| 7 | SILENT | SPEC-TABLES.md §4 refusal list; nearest text SPEC.md:2609 | A `[A..B]T` read with fewer than A elements: is it refused, and by what name? SPEC.md:2609 says "validated to [Min, N]" for the packet form, but the table refusals list only `count_over_bound`. | Refuse (named `count_under_bound` here). This applies on both the wire and JSON reads. No corpus row tests it. |
| 8 | AMBIGUOUS | SPEC-TABLES.md:16155 | The 255-byte schema-name cap applies "in every object and inside a skipped value". Inside a skipped value, a reader cannot tell a field key from a map key, and map keys are exempt from the cap. | Every object key inside a skipped value is held to the cap. |
| 9 | AMBIGUOUS | SPEC-TABLES.md:16150-16152 vs 17527-17533 (W2) | The read cap: "the root at depth 0: 128 container levels are read and 129 refuse". Counting from 0, that refuses depth 128. But W2's pointer chain of 128 nodes under the root puts the last node at depth 128, and both "writes and reads". | A container at depth 128 is read and one at 129 refuses. `graph_chain_text` passes and `graph_chain_past_cap` refuses with this reading. |
| 10 | VECTOR-DISAGREES | SPEC-TABLES.md:16682-16688 vs MANIFEST.txt:688 | §16.7: "The label grammar and the definition rules apply EVERYWHERE, inside an unknown key's skipped value too: ... a reference before its definition ... refuse[s] wherever [it] sit[s]." Row `reserved-ampersand-in-skipped-value` (`{ "mystery": { "&node": 1 } }`, root `RootConfig`) expects `read`. Row `label-undefined` (unit `graphdemo`) expects `refused`. | Followed the vector. The label rules apply in a skipped value only when the unit declares a pointer anywhere. The only distinction in the text that separates the two rows is §16.7:16808, where a pointer-free unit's skip adapter is a stub. |
| 11 | SILENT | SPEC-TABLES.md:16104 | A deprecated enum variant "reads as the field's declared default". An array element, map value or union arm has no field default. What does it read as there? | The type's default, which is `None`. No corpus row tests it. |
| 12 | DEFERS-TO-CODE | SPEC-TABLES.md:16103 | A timestamp string "naming an instant outside `Time.UnixNano`'s domain" is refused. The domain is given only as a Go API. | A signed 64-bit count of nanoseconds since 1970-01-01T00:00:00Z. Anything outside it is refused as `timestamp_not_rfc3339`. |

## The five that cost the most time

These are my estimates, not measurements.

1. Gap 2. With no consumable IR, the port needed a full `.schema` lexer, parser and resolver (`src/schema.lisp`, about 600 lines) before anything else could run.
2. Gap 5. Kind 16's header sits in two places in the text, and the right one only showed up when the keyed-array goldens were framed.
3. Gap 10. The only failing hostile row, and the text says the opposite of the vector.
4. Gap 3. The first corpus schema with a same-line brace stopped the parser, and the spec says that layout is refused.
5. Gap 9. The read and write depth caps share a number but count different things, and "128 levels are read" with the root at 0 is off by one from the pinned chain.
