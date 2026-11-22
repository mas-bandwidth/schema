# SPEC-GAPS: an OCaml port from the spec alone

This lists every place where the spec on `rowan/fix-ledger-1122` was not enough to write the port without asking
someone. The reading allowed was docs/SPEC*.md (with the addendum SPEC-CONFORMANCE.md), docs/USAGE.md,
docs/FIXED-FORM-ALGORITHM.md, test/conformance/README.md and the normative data. Code that follows a
vector against the spec, or that makes a choice, is marked `(* SPEC-GAP n *)`.

Kinds: AMBIGUOUS, SILENT, CONTRADICTION, DEFERS-TO-CODE, VECTOR-DISAGREES.

| n | spec file:line | the question | chosen, and why | kind |
|---|---|---|---|---|
| 1 | SPEC.md:3236 (the IR preservation invariant), SPEC-TABLES.md:14050 | No serialized IR is handed to a port, and no IR format is specified for a port to read. Does a port parse `.schema` text itself? | The port parses `.schema` with its own lexer and parser (src/parse.ml), working from SPEC.md §4's grammar. The manifest's `unit` lines name schema files and directories, so a port has nothing else to read. | SILENT |
| 2 | SPEC.md:644 (`TableDecl = "table" ident ...`), SPEC-TABLES.md §2.2 | SPEC.md's grammar has no `fixed table` production, but SPEC-TABLES §2.2 and every unit in the corpus use one. | `fixed` before `table` is accepted as a declaration modifier. | CONTRADICTION |
| 3 | FIXED-FORM-ALGORITHM.md:48-80 (the layout kind table) | `timestamp` has no layout kind. Kind 36 is the variable wire's, and kind 36 is not in the layout's closed set (`1..30`, `32`, `35`). What is a timestamp field in a fixed layout? | It is never reached: `timestamp` is a gated key, and no fixed unit in the corpus uses one. The code writes `5` (i64), the digest's leaf kind for timestamp (SPEC-TABLES.md:8236), and is marked. | SILENT |
| 4 | SPEC-TABLES.md:8236 | Digest step 1 gives an enum the leaf kind `7`, while its layout kind is `30`. Is `7` a typo for `30`? | The text was followed (`7`). The hashes then match every `schema.lock` `layout=` this port compared (SimState: 16873267723408638105). | AMBIGUOUS |
| 5 | FIXED-FORM-ALGORITHM.md:42 (``id` is `fnv1a64(wire name)``) | Synthetic layout children have no wire name: an array's element, an optional's payload, an enum-keyed array's key enum. Which id does each carry? | Taken from the lock's `bytes=`: array element `0`; optional payload = the wrapper field's own id; keyed array key = `fnv1a64(<enum type name>)`. With id `0` the hash for every unit holding a `?T` or `[E]T` was `layout_unknown`. | SILENT |
| 6 | test/conformance/README.md:751-755 | On the `wire` surface, how does a driver write a refusal? The text says "Drivers may emit `refused`". `refused\n` (the spelling every other surface uses) scored "expected wire refusal, got bytes". | The driver writes the "historical `<case>.absent`", which the same paragraph says scores as a refusal. A literal `refused` without the newline was not tried. | AMBIGUOUS |
| 7 | SPEC-TABLES.md:102-107, 109-119 | The spec says a form-`1` file at a declared fixed root is refused `previous_form`, and the `report` rows `previous_form_*` owe `refused` for exactly that. But 15 `framing` rows (e.g. `frame_empty_table`, unit tblr1, root `Cfg`, a `fixed table`) hand a form-`1` file to a fixed root and owe `read` plus a form-`3` write-back. | The vector is followed on the `framing` surface only: there a form-`1` file at a fixed root is read with the variable reader and saved as form `3`. Everywhere else the reader refuses `previous_form`, as the spec says. The corpus does not agree with itself on this point. | VECTOR-DISAGREES |
| 8 | SPEC-TABLES.md:17126-17137 | §16.7: "every port refuses the whole read ... on ... a reference to a label not yet defined", inside a skipped value too. json-hostile `reserved-ampersand-in-skipped-value` (`{ "mystery": { "&node": 1 } }`, an unknown key holding a reference to label 1, which is never defined) owes `read`. | The vector is followed: inside a skipped value, a reference to an undefined label is accepted. A reference to a label whose definition is still open is refused. Everything else in the label grammar is checked there. | VECTOR-DISAGREES |
| 9 | FIXED-FORM-ALGORITHM.md:940 (`LADDER(te.kind, me.kind)`), SPEC-TABLES.md §4 | EMIT widens a leaf only when `LADDER(te.kind, me.kind)` holds, but the ladder is defined in prose in SPEC-TABLES §4 and not as a table a lineage read can use. Is an unsigned writer into a WIDER signed reader (`uint32` into `int64`) a rung? Does `fixed(I,F)` widen across a change of `F`? | Lineage reads are applied value by value: the writer's layout is walked beside the reader's declaration and matched by id, with no compiled op list. Lawful: same family; signed into signed; unsigned into unsigned or into a wider signed; never narrower; `float32` into `float64`; a fixed kind into a fixed kind at the same `F`. Anything else refuses `layout_malformed`. `fixedevo_v1_as_v3` and `fixedevo_v2_as_v3` pass. | AMBIGUOUS |
| 10 | testdata/conformance/tables/FORMAT.md:94-96 | `<path>` is "dotted field names with `[i]` element indexes", but rows also address a map entry (`map_f64[0].key`, `map_f64[0].value`) and a union arm by its name (`u_val.u_f32`). In which order are map entries indexed, and does naming an arm select it? | `[i]` indexes the map's entries in the order the read gave them, which is ascending key order on a canonical wire. Naming an arm selects it: if another arm was selected, the named arm starts at its declared defaults. | SILENT |
| 11 | test/conformance/README.md:172 (the `report` row), FIXED-FORM-ALGORITHM.md §2 | What batch capacity does the `report`/`framing` read use for a fixed root? `fixedevo_v3_batch2`, a valid two-record batch, owes `refused`. | The plain read takes a batch of one, so a second record refuses `batch_too_large`. The `wire` surface reads with room for any batch. | SILENT |
| 12 | SPEC-TABLES.md:17175-17185 | "Anywhere else, the prefix is `malformed`". Inside a SKIPPED value the reader cannot tell a map key from a field key. Is `"&foo"` inside an unknown key's value refused? | Only `&node` is held to the label rules inside a skipped value. Other `&` keys there pass, because a map key that begins with `&` is ordinary data (§16.2 map row). | AMBIGUOUS |
| 13 | SPEC-TABLES.md:16506 | "A deprecated variant ... reads as the field's declared default." An array element, a map value, an arm payload or an optional's payload has no declared default of its own. | Such a position reads the element's zero value (None). A field reads its declared default. | SILENT |
| 14 | SPEC-TABLES.md:14050-14057 | "A target carries exactly the keys it declares when it registers, and a new port starts with none." The driver protocol has no way for a port to declare its keys. | The port carries `timestamp`, `unbounded-text`, `text-list`, `array-root`, `one-sided-range`, `optional-variable-closure` and `optional-map`. It does not carry `required`, `forms`, `integrity` or `text-list-total`. A case whose unit uses an uncarried key answers `<case>.absent` (the feature-witnesses unit: 4 instances, 4 json-hostile rows, 1 report row). | SILENT |
| 15 | testdata/conformance/tables/FORMAT.md (json-write-refusal `wide:<field>`) | OCaml strings are bytes, with no UTF-16 storage. | The FORMAT.md rule applies: the leg holds the nearest value that is not text. That is the three-byte encoding of the lone surrogate `D800`, which is ill-formed UTF-8. | (FORMAT.md answers it) |

## OCaml stress points

- **63-bit native int.** No wire integer passes through `int`. Every integer kind, `bits(N)`, the fixed raw
  values, the 128-bit kinds and timestamps are held as arbitrary-precision integers (src/big.ml, sign plus
  base-2^24 limbs). Ranges are tested on the exact value. u64 and i64 cross the 2^63 line with no special case.
- **float is binary64 only.** A float32 value is held as a binary64 float that is exactly a float32. A text
  token converts to float32 by ONE correctly rounded conversion over exact integers, ties to even, never
  through a double (src/num.ml). Storage rounds with `Int32.bits_of_float`. A finite value that rounds past
  float32's range is refused.
- **strings are bytes.** UTF-8 validity, surrogates and U+0000 are checked by hand at every site the spec
  names.
- **exhaustive variants.** Unions and enums are held as ordinals and matched exhaustively. A deprecated
  variant keeps its ordinal: the writer refuses it, and the text reader lands the declared default.

## What is not here

The surfaces this port does not carry yet are answered with exit code 2 (ABSENT): `message`, `retain`,
`retain-save`, `cook`, `cook-write`, `cook-foreign`, `block`, `block-foreign`, `block-dump`, `forgery`,
`cook-forgery` and `write-refusal-message`.
Under SPEC-CONFORMANCE.md §2 all of these are owed, so the port does not conform yet.
