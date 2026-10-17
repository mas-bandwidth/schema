# Hand-editable text-form vectors

These pin SPEC-TABLES.md §16.9. They are standard data, not an implemented
codec test suite; the Go implementation card must consume them. No runtime
or emitter is changed here. Compare `.input.sexp` and `.rewrite.sexp` as bytes,
including line endings and EOF. Do not normalize them in checkout or tests.

`contract.json` describes the root and field kinds using Schema declaration
notation. Its enum/flags/union notation describes named members, not a
compiler input file. All ordinary missing fields have the stated defaults;
`explicit_fields` in the value oracle plus those defaults is the full typed
value. Wrapper objects in the oracle denote exact wide numbers, raw fixed
storage, byte strings, float bits or an instant, not extra table fields.
`State` has two non-None variants, `unknown` and `done`; `None` is implicit.
`Features` has Fast and Safe bits. `Node` and `TextItem` have the fields stated in the contract.
Run Document vectors for both fixed and variable roots. `variable-*` uses
its separate Graph contract, only for variable tables. Graph labels in the
oracle express identity: both items MUST point to the same node.

| Vector | Rule pinned |
| --- | --- |
| `roadmap` | Leading semicolon block, blank comment, multiline key/value, nested keyword records, compact lists, blank lines, inline and footer comments, escaped newline and semicolon in string. |
| `layout` | Mixed LF/CRLF, tab, trailing spaces, no final newline, numeric exponent and escaped string spelling retained. |
| `kinds` | Exact 128-bit limits, fixed raw unit, negative-zero float, Unicode width, bytes, timestamp offset, flags, keyed array, payload-free union, present empty optional and empty sequence. |
| `unbounded` | Unbounded text-list elements and map keys (§2.13.1) read and rewrite with no length bound; the `[..N]` count bound still holds. |
| `variable` | Sharing label need not be sequential on read; map source order is preserved, null optional map value and present empty blob stay distinct. |

Each positive has an input, complete typed-value oracle and byte-identical
rewrite. `roadmap.edit.json` and `layout.edit.json` request one scalar change;
the `.edited.sexp` files show the only allowed byte change. A semantic no-op
edit of layout.count to 2 MUST retain `2e0`, not replace it with `2`.

Attachment oracle for roadmap: the three comments before the root belong to
the root; the key/value comment belongs to title; “Comment above” belongs to
nodes; “first element” belongs to nodes[0]; “Second element” belongs to
nodes[1]; “trailing field” belongs to count; “container footer” and “document
footer” belong to the root. Blank lines are separate trivia. In layout the
heading and EOF comment belong to the root, and “field” belongs to title.
The variable heading belongs to Graph.

Every `refusals/*.sexp` has its exact stable name in the same-stem `.reason`.
Use Document except `duplicate-map` and `forward-reference`, which use Graph.
Inputs deliberately omit a final newline. All use contract limits. The names
of the files identify the specific rule: unknown/duplicate keys (including
quoted aliases), numeric/string/UTF-16/byte/array bounds, short fixed array,
unterminated list/string/pair, wrong kind, inexact fixed fraction, unknown
variant, duplicate flags, multiple union arms, noncanonical base64 pad bits,
unpaired surrogate, timestamp, extra root, executable-looking symbol,
unknown enum slot, illegal root label, duplicate map key and forward label.
No refusal yields a partially populated table or rewritable document.

Survey evidence: repository docs/roadmap.sexp lines 1–21 has semicolon headers,
keyword records, blank/comment lines and multiline values. nova-sprint's
internal/workfile/decode.go and encode.go additionally have tagged/positional
records and deterministic sorted output; those require explicit table migration,
not an implicit permissive reader. cmd/nova-sprint/landsexp.go merges whole lines
and entries, motivating unchanged-byte and local-edit requirements. This corpus
captures their layout shapes, not a claim to parse their undeclared legacy data.

The directory's `.gitattributes` disables newline conversion and treats CRLF
as intentional for whitespace checking; byte preservation is part of the oracle.
