# Python hand-editable table text

The hand-editable text form is selected explicitly with
`Table.from_text_form(data)`. It accepts UTF-8 bytes and either returns a
`Document` or raises `Refused`; the exception's `reason` and zero-based
`offset` identify the refusal. The document's `value` is the typed table, and
`document.write()` returns the original bytes, including comments, line
endings and the original final-newline choice.

The optional keyword arguments `max_bytes`, `max_tokens`, `max_nodes`,
`max_allocation` and `max_depth` bound the read. Zero or a negative value uses
the standard default; depth cannot exceed 128. Comments are exposed as
`document.comments`: each entry has a byte `start`, `end`, and a `path` tuple
identifying its attached field or element. The original source remains the
source of the comment text and whitespace.

`document.edit([(path, token), ...])` replaces existing scalar tokens. Paths
use field keys and array indices, for example `nodes[0].title`; tokens are
UTF-8 bytes in the hand-editable grammar. A tuple path such as
`("nodes", 0, "title")` also supports field keys that contain a dot. An edit returns a new document after
validating the complete result; the original document is unchanged on refusal.
Disjoint edits use original byte spans, and a semantic no-op keeps its original
spelling. Structural edits and whole-compound replacements are refused.

The schema controls the interpretation of every list, including empty lists,
and the runtime rejects unknown keys, duplicate keys, values outside declared
bounds, malformed references and resource-limit violations before publishing a
value. The JSON reader supplies the final value walk; hand-text integers first
use exact decimal arithmetic and never pass through a binary float.
