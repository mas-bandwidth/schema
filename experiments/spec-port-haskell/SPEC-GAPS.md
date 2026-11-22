# Spec gaps met by the Haskell spec-only port

This port was written from `docs/SPEC.md`, `docs/SPEC-TABLES.md`,
`docs/USAGE.md`, the driver protocol in `test/conformance/README.md` and the
corpus data, at main `8eed0baf3`, with no other language's code read. Line
numbers are at that commit. Each gap has a `SPEC-GAP n` marker at the code
that answers it.

Scope carried: schema intake, the variable form (form 1) read and write, the
JSON text read and write, for variable roots. Fixed roots (form 3) answer
`.absent`.

| kind | count |
|---|---|
| SILENT | 2 |
| AMBIGUOUS | 6 |
| CONTRADICTION | 3 |
| DEFERS-TO-CODE | 0 |
| VECTOR-DISAGREES | 2 |

---

**1.** `docs/SPEC.md:3158-3175` (§7.1). **Question:** what does a port take
as its schema input? The checked IR is "the load-bearing design decision",
but it is the Go compiler's in-memory form. The spec publishes no serialized
IR and no `schema` command that emits one, and the corpus names `.schema`
files. **Chose:** parse and check `.schema` text in the port. That means a
lexer, a parser, constant folding, one-sided range filling, `was` ids,
guards and defaults. It was the largest single cost of the port.
**Kind:** SILENT.

**2.** `docs/SPEC.md:645-650` (`TableDecl = "table" ident ( Block |
AttrSection NL Block )`) and `docs/SPEC.md:701-703` (an `AttrSection` "runs
to END OF LINE ... nothing follows a qualification"). **Question:** may a
qualified declaration open its `{` on the qualification's own line? The
grammar says no. The corpus unit
`testdata/conformance/feature-witnesses/full/Features.schema:6` and `:18`
(`table Root | forms="wire,json,cook", integrity="crc32c" {`) says yes.
**Chose:** followed the vector. A qualification section also ends at any
token that is not `,`. **Kind:** VECTOR-DISAGREES.

**3.** `docs/SPEC.md:645` (TableDecl), `:662` (Type), `:676-699` (Scalar),
`:701` (Bound). **Question:** the grammar has no production for `[]T`, a bare
`string`, `*bytes` / `*string`, an element-level `?` (`[]?string(M)`) or
`fixed table`. `docs/SPEC-TABLES.md` uses all of them: §2.2 `:991`, §2.5
`:1675`, §2.9 `:2783` and §2.13 `:4004`. **Chose:** extended the parser to
the spellings the SPEC-TABLES examples and the corpus schemas use.
**Kind:** CONTRADICTION.

**4.** `docs/SPEC.md:734` and `:810` (FloatExpr is "float64 arithmetic").
**Question:** what does a `float32` field's default `= 0.1` hold? There are
two readings. Round the literal to float64 and then narrow it, which rounds
twice. Or round the exact decimal once, to the field's width.
`docs/SPEC-TABLES.md` §16.2 forbids the double rounding for text reads, but
the language section says only "float64". **Chose:** a lone literal stays
exact and rounds once at the field's width. Arithmetic between operands is
float64. No vector reached the difference. **Kind:** AMBIGUOUS.

**5.** `docs/SPEC.md:645-650` (a table's qualification "carries TAGS, `was`
and `forms`"). **Question:** which valued keys may a TABLE line hold?
`docs/SPEC-TABLES.md:4332-4356` (§2.14, `integrity = "crc32c"`) and
`:17196-17236` (§16.10, `json = "array"`) put two more keys there, and
SPEC.md's vocabulary lists neither. **Chose:** accepted both, as
SPEC-TABLES.md does. **Kind:** CONTRADICTION.

**6.** `docs/SPEC-TABLES.md:5964-5968` (§3.2: a kind-16 body's count is
"read against the ENCLOSING body and never against the field's own `L` (§3,
THE HEADER AGAINST THE PARENT)"), against `:4954-4962` and `:3000-3003`
("lies inside the field's own `L`, for kind `14`, for kind `16` and for a
map alike ... a header that crosses `L` ... is damage"). §3 has no passage
titled "THE HEADER AGAINST THE PARENT". **Question:** is a kind-16 count that
crosses `L` damage, or is it completed from the parent? **Chose:** damage,
inside `L`, the same rule as kind 14. No vector disagreed. **Kind:**
CONTRADICTION.

**7.** `docs/SPEC-TABLES.md:5692-5693` and `:8913` ("a node record no
pointer reaches" is `wire_damaged`). **Question:** does "reaches" mean that
any kind-17 index on the wire names the record, including one inside an
unknown field or inside a record of a type this reader does not declare? Or
that the record is reachable from the root through pointers this reader
decodes? The two readings give different verdicts on evolved data.
**Chose:** any kind-17 index anywhere in the framed bytes. **Kind:**
AMBIGUOUS.

**8.** `docs/SPEC-TABLES.md:6729` (message form: "a union whose arm is
structural ... a scalar arm adds no level, as on the file form") and
`:8806-8816` (the file form's depth bound, which never mentions unions).
**Question:** on the file form, does a union add a nesting level of its own,
on top of its arm's body? The file-form rule is given only by reference from
the message section. **Chose:** a structural arm's body is one level, and
the union adds none. **Kind:** AMBIGUOUS.

**9.** `docs/SPEC-TABLES.md:16209-16218`. **Question:** "a SCHEMA-NAME
STRING IS CAPPED AT 255 BYTES ... in every object and inside a skipped
value", but "the cap never reaches a map key". A reader skipping an unknown
key's value cannot tell a map's keys from a table's. Which object keys
inside a skipped value are capped? **Chose:** every object key inside a
skipped value. **Kind:** AMBIGUOUS.

**10.** `docs/SPEC-TABLES.md:16740` and `:16779` (§16.7: "the label grammar and the
definition rules apply EVERYWHERE, inside an unknown key's skipped value
too"; "Anywhere else, the prefix is `malformed`"), against the §16.2 map row
(`:16158`: "the `&` prefix is ordinary data here"). **Question:**
inside a skipped value, is a key that starts with `&` but is not `&node`
malformed, or is it possible map data? **Chose:** only `&node` takes the
label rules inside a skipped value, and any other `&` key there is left
alone. **Kind:** AMBIGUOUS.

**11.** `docs/SPEC-TABLES.md:16150` (the JSON row for `[..N]T`: "count =
length; more than N refuse"). **Question:** what does a JSON read do with a
counted `[A..B]T` array that holds fewer than `A` elements? The table states
only the upper side. **Chose:** refuse (`count_over_bound`), which matches
this port's wire read. **Kind:** SILENT.

**12.** `test/conformance/README.md:742-747` ("Drivers may emit `refused` or
the historical `<case>.absent` file") and `:170` (the `wire` surface writes
"the bytes"). **Question:** how does the `wire` surface spell a refusal? The
harness scored both `refused\n` and `refused` as value bytes: "expected wire
refusal, got bytes" on `blob_shared` and `blob_shared_string`. Only
`<case>.absent` scored PASS. **Chose:** followed the harness and wrote the
marker. The other surfaces keep `refused\n`, which the harness accepts.
**Kind:** VECTOR-DISAGREES.

**13.** `test/conformance/README.md:101-104` (`<case>.absent` is written
"beside where the answer would go"). **Question:** what is the marker's name
on `json-write`, whose answer file is `<name>.json`? The harness reported
`<name>.absent` as "the driver wrote nothing" and accepts
`<name>.json.absent`. **Chose:** `<name>.json.absent`. **Kind:** AMBIGUOUS.

---

Not gaps, but worth noting for the next port:

- The text-list `| total = N` rule (`docs/SPEC-TABLES.md:4115-4160`) is not
  repeated in §16.2's mapping table. This port missed it until the
  `json-hostile text-total-past` row refused. The rule is stated, and its
  JSON half is at `:4147`.
- Every float-spelling vector (`text_float*`) sits on a fixed root, so this
  scope does not reach W12's loop through the corpus. The unit tests pin
  §16.14's own examples instead.
