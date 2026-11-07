# cards testdata — the variable form's declaration pair

These fixtures are the two declarations the Dart variable-form round trip
(docs/SPEC-TABLES.md §3, §3.1, §5) compares: `base.schema` is the OLDER
declaration and `new.schema` the NEWER one. The pair differs only by a
`was = "..."` rename on the optional body field, so a reader built from
`base.schema` must find the newer wire's renamed id through the alias
(docs/SPEC-TABLES.md §5) and default every field the newer wire does not carry.

`new.schema` carries exactly the card constructs the Dart backend is asked to
emit on form byte 1: a variable table root, tables as unbounded list elements
(`[]PathText`, §2.9), bool/int32 (with min/max)/enum fields, `string(N)`, and
`*string` — the string kind of §2.5. The two files differ by the `body` to
`done_when` rename and its `was = "body"` alias; neither names a field after a
Dart reserved word, because the backend spells members by `dartName` and would
refuse such a declaration by name (docs/SPEC-TABLES.md §11).

The tests that read these fixtures live in `cardsform_test.go`. They are the
red-first anchor for the wire and text work the card still owes; nothing here
is a golden byte pin, because the emitter writes no form-1 bytes yet.

## The form-1 wire the emitter must write

The card's byte oracle is `tables/cards/fuzz/card-populated.wire` (641 bytes,
pinned against the Go leg), and its structure is the whole of §3, read down to
the card's closure. The three parts, in order:

- **The FORM BYTE, `1`, and nothing else in the header** (§3): the variable
  form carries no hash and maps nothing, so its body begins at byte 1.
- **The ROOT BODY** — a sequence of fields, each `reference (LEB128), kind (u8), payload`, ended by a ZERO REFERENCE (§3). A reference names a slot of
  the id table, counted from 1; `0` is the body terminator.
- **THE ID TABLE is the trailer**, found from the END (§3): the final eight
  bytes are the ENTRY COUNT (fixed little-endian u64), the `8 × count` bytes
  before them are the entries (each `fnv1a64(name)`, §5) in FIRST-USE ORDER
  across the whole wire, distinct.

The card's constructs map onto the closed kind set (§3) exactly as the other
backends write them:

| construct | kind | payload |
|---|---|---|
| `bool` | `1` | 1 byte |
| `int32` | `4` | 4 bytes, two's complement, little-endian |
| `int64` | `5` | 8 bytes |
| `enum` | `30` | the VARIANT ID reference (`0` = `None`) |
| `string(N)` | `12` | `L`, then `L` UTF-8 bytes, no terminator |
| `*string` | `17` | the node index of §3.1 — a `*string` rides as a POINTER into a flat node table, its record a string blob |
| `[]T` of tables | `14` | `L`, then the array body: `element kind (u8)`, `N`, then the elements; element kind `13` (table) is preceded by its own `L` each |

The one non-obvious part is `*string` (kind `17`): it is NOT an inline string.
A pointered save writes every reachable node once into a NODE TABLE (§3.1), and
the field carries the node INDEX. The Card's `done`, `source` and `body`
(`*string`) fields each reference a string-blob node record, so the emitter owes
the node table beside the id table, and a reader resolves a kind-17 field
through it. The report the tolerant read fills is §4's six counters plus the
refusal verdict (§3): unknown ids skipped and counted, absent fields read their
defaults, kind mismatch and clamp counted, identity by the name hash and `was`
(§5).
