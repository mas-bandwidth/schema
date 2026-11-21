# Any JSON value in a variable table: the `json = "value"` design

**Status: withdrawn from the specification on 2026-11-21; kept here as the
design the construct would land under.** This text was `docs/SPEC-TABLES.md`
§16.13. No target ever carried it: `schema check` refuses the table-line value
`"value"` (§16.10's key takes `"array"` alone), the Go cards that would build
it (`sprint-out/schema-go-json-value-*` in `docs/roadmap.sexp`) were deferred
on 2026-10-19 and never started, and no test, corpus row or generated file
reads it. The spec describes what the compiler does, so the section left it.
Those cards, if they are picked up, read this file where their briefs say
"16.13".

One defect is known in the text below and must be fixed before it is built:
the example enum does not pass `schema check`, because `False` and `True` are
reserved words in Python and `String` and `Object` in Dart (SPEC.md §4.6). The
shape rule "an enum with exactly the seven variants above" has to name
variants every target admits.

The section numbers below (§16.2, §16.10, §16.12 and the rest) are
`docs/SPEC-TABLES.md`'s.

**Any JSON text §16.12's layer accepts can be held by ONE declaration and come
back out equal.** That is every `y_` file, every `i_` file whose row says
`accept`, and the `n_` files marked `accept (dialect)`. No file needs a schema
of its own. This section rules four things: the representation, what "equal"
means, the bounds, and where the policy stops.

**THE REPRESENTATION is a TAPE: the value's nodes in pre-order in one
unbounded array, and their bytes in one blob.** It is spelled in schema source
with constructs that already exist (§2.5, §2.9), and nothing on the wire is
new:

```
enum JsonKind { Null, False, True, Number, String, Array, Object }

table JsonNode
{
    kind     JsonKind
    count    uint32 // Array: its elements; Object: its members; 0 for every other kind
    key_len  uint32 // a member of an Object: its key's bytes in text; 0 for every other node
    text_len uint32 // Number: its token's bytes; String: its decoded bytes; 0 for every other kind
}

table JsonValue | json = "value", integrity = "crc32c"
{
    nodes []JsonNode // the value's nodes in pre-order, the root first
    text  *bytes     // every node's key and then its text, in node order, back to back
}
```

A container's node comes before its children, and its `count` says how many
children follow. **Zero nodes is the value `null`**, and it is the table's
declared default, so a field of this table that a text or a wire never
mentions reads as `null`. The writer spells a root `null` as zero nodes, with
`text` null, and nothing else. A reader also accepts a root of exactly one
Null node, and a `text` of length zero in place of a null one, as that same
value. Accepting them costs one comparison each and touches no bound. Neither
is ever written. A member of an Object is ONE node: its key goes in `key_len`,
and the node's own kind is the member's value. The `text` blob holds every
node's key bytes and then its text bytes, node after node, with no separator
and no offsets. The lengths in the nodes say where each piece starts.

**Why a tape, and not a recursive union.** The other spelling would be a
union of seven arms whose Array and Object arms hold `[]*JsonValue` and
`[]*JsonMember`. It needs a pointer per child and a node record per child. A
ported reader would have to carry pointered units (§3.1) to read it. It can
share a node or form a cycle, which a JSON text cannot express, so every read
would have to refuse both. And depth would come back onto a decode that is a
scan (§3.1). The tape has one variable edge of
each kind. It has no pointer to a table. It cannot share a node or form a
cycle, because nothing in it names a node. Its depth is counted by the same
linear scan that checks it (below). Every port that carries `[]T` of a table,
an enum, `uint32` and `*bytes` carries it, with no recursion and no
arithmetic beyond a counter. This is the layout simdjson uses for its tape,
for the same reasons: it is flat, it is in order, and it is checked in one
pass.

**Why `*bytes` and not `*string`.** A JSON string can hold `U+0000`
(`y_string_null_escape.json`, `y_object_escaped_null_in_key.json`). No text
storage holds a zero (§16.3, "A ZERO IN TEXT"). A blob of bytes is not text
storage, so it holds the decoded bytes exactly, and the zero rule never
applies to this construct.

**THE ONE NEW CONSTRUCT is the value `"value"` of §16.10's table-line key
`json`.** The key's closed vocabulary becomes `"array"` and `"value"`. The
key says that the table's TEXT is the raw JSON value its tape holds. It is
not the wrapped object §16.2 would write, `{"nodes":[...],"text":"..."}`.
Like `"array"`, it changes no wire byte. A reader that has never heard of it
reads the file as the ordinary variable table it is. The key is not in the
projection, the build version (§20), the lock or the tables baseline (§18).
The declaration is checked by its SHAPE, and every mismatch is refused by
name, `json_value_shape: <Table>: <the first difference>`:

- exactly the two fields above, in that order, with those names, kinds and
  no attributes;
- an element table with exactly the four fields above, in that order;
- an enum with exactly the seven variants above, in that order;
- `integrity = "crc32c"` on the same line (§2.14).

Type names are free. A table declared `json = "value"` must not be the
target of any pointer: `*T`, `[]*T`, `[..N]*T` and a pointer arm are all
refused, naming the pointer field. The reason is §16.10's: a shared node is
defined under `&node` (§16.7), and a raw value has no object for that key.
By value, the table may appear anywhere a table may appear: as the root, as
a field, as a map value, as a union arm or as a list element. Wherever it
appears, its text is the raw value. Inside a raw value the `&` prefix is
ordinary data, as it is in a map's keys. Go carries the key first. Every
other target refuses a unit that uses it, centrally in the compiler, by name,
in the sentence it gives `"array"` (§16.10): `target "<target>" refuses the construct json = "value" on table <Table>`, until its port lands and the
refusal is removed for that target alone.

**EXACTNESS: what is kept, and what EQUAL means.** The tape keeps every
number's TOKEN, byte for byte as the text spelled it, and nothing else about
it. Object members keep their order. A repeated key is refused by §16.12's
layer before any tape is built (§16.2), here as everywhere. A string and a key keep their DECODED UTF-8 bytes: escapes
are resolved on read and chosen again on write (§16.3's writer).
Whitespace, a leading byte-order mark, comments and trailing commas are not
data, and none is kept (§16.2, §16.12). **No step expands a number's
digits.** The token is copied, never parsed into a value, so `1e999999999`
costs its eleven bytes at every step.

**EQUAL, stated as a rule a stranger can code.** `Equal(a, b)` takes two
texts. It reads each with this construct's own JSON step (below), which is
§16.12's layer plus the construct's refusals, so a key past 255 decoded bytes
is refused. If either text is refused, Equal
refuses too and gives no answer. Otherwise the two values are equal when
all of these hold:

- they are the same kind;
- `null`, `true` and `false` are equal to their own kind;
- **two numbers are equal exactly when their token texts are equal byte for
  byte.** So `1.0`, `1e0` and `10e-1` are three different numbers, none of
  them equal to `1` or to each other, and `-0` is not `0`, and `1E2` is not
  `1e2`;
- two strings are equal exactly when their decoded UTF-8 byte sequences are
  equal, so `"A"` equals `"A"` and `"\/"` equals `"/"`. No Unicode
  normalization is applied: a precomposed `é` is not a decomposed one;
- two arrays are equal when they have the same length and are equal element
  by element, in order;
- two objects are equal when they have the same member count and member `i`
  of each has an equal key (decoded bytes) and an equal value, for every `i`,
  in order. A repeated key is compared where it stands.

**Why token text, not exact decimal value.** The carrier keeps the token, so
the round trip writes the bytes it read. The stricter comparison is free, and
it catches a defect value equality would pass: a step that rewrites `1.0` as
`1` changes what a consumer that tells integers from reals reads. RFC 8259
§6 leaves a number's meaning to the receiver. A carrier that keeps the token
decides nothing on the receiver's behalf. Exact-value equality would be a
second number model in every port. Over a 511-byte token it needs exponent
arithmetic past 64 bits. And it is one easy mistake away from expanding
`1e999999999`'s digits, which the cap exists to forbid. Comparing token text
is one byte comparison, the same in every language. Key order is compared
for the same reason: the carrier keeps the order, so a step that reorders
members is a defect, and Equal names it.

**SECURITY TRUMPS: every bound is held at BOTH steps, and each breach has a
name.** The JSON step is `FromJson` (and its options, §16.2) of a
`json = "value"` table. It reads with §16.12's layer, unchanged, and builds
the tape in one pass. A container's `count` is patched when it closes, from a
stack no deeper than the depth cap. The binary step runs on every read of
the table's binary form: the strict read (§6.5), `Load` and the base64 text
read (§16.8). It runs after §2.14's trailer and §3's decode, and before the
read returns. It is ONE linear scan of `nodes` in index order. It keeps a
stack of open containers and their remaining counts, and a cursor into
`text`. Both steps refuse with the names in the table below, and the JSON
step sets each `Reason` itself where the layer would only mark the read bad
(the depth and token caps, the number grammar): the read returns false with
that name as its one reason. A lone or out-of-order
surrogate escape exists only in a text, so the JSON step names
`string_lone_surrogate` and the binary step, which holds decoded UTF-8, has
only `string_not_utf8` to name. A refusal returns no
partial value. Nothing on this construct is cut or counted, because a cut
key or a dropped member would make the value unequal without saying so. A read either places the whole value, or it refuses.

| bound | JSON step | binary step | name |
|---|---|---|---|
| the integrity trailer (§2.14), which only a file's ROOT carries: it covers this table when the table is the root, and a nested value only through the trailer of the root that holds it, when that root declares `integrity` | not applicable (no trailer in a text) | the CRC-32C is checked before anything is decoded | `integrity_mismatch` |
| the depth cap, 128, counted from the FILE's root as §16.12's layer counts it: the document's root container sits at depth 0, and a container whose depth is past 128 is refused, so a value as the root may nest 129 containers and a value sitting at depth d may nest 129 − d | the layer's own check, the walk passing the depth at which the value sits | the scan's stack, started at the depth the table walk passes in (0 for the root), so the binary step and the writer's cap (§16.3) agree with the text | `json_value_too_deep` |
| a key of at most 255 DECODED bytes | the key is refused, never clamped | a `key_len` past 255 | `json_value_key_too_long` |
| a number token of at most 511 bytes (a resource limit, outside the verdict contract, §6.5) | the carrier's token cap | a Number's `text_len` past 511 | `json_value_number_too_long` |
| a number token is RFC 8259's `number` production | the layer's grammar (a breach is `malformed`) | the Number's bytes, an empty token included, are checked against the same production | `json_value_number_grammar` |
| strings and keys are well-formed UTF-8 | §16.3's refusals, `string_not_utf8` and `string_lone_surrogate` | the bytes of a String or a key are checked for well-formed UTF-8 | `string_not_utf8` |
| a count or a length fits `uint32` | a container with more than 2^32 − 1 children, or a string or key past 2^32 − 1 bytes | not applicable (the field cannot hold more) | `json_value_too_large` |
| exactly one value | the layer's "one whole value" | the root's subtree ends with nodes still to scan | `json_value_trailing` |
| every container's children are present | not applicable (the text closes it) | a `count` runs past the last node | `json_value_truncated` |
| every node's kind is one of the seven | not applicable | a `kind` of `None` or a value no variant names | `json_value_kind` |
| keys sit only on members | not applicable | a `key_len` on a node whose parent is not an Object, including the root | `json_value_key_misplaced` |
| text and counts sit only on the kinds that have them | not applicable | a `text_len` on Null, False, True, Array or Object; a `count` on anything that is not an Array or an Object | `json_value_text_misplaced` |
| the text blob is used exactly | not applicable | the keys and texts the scan consumes do not add up to the blob's length | `json_value_text_length` |

**No new cap is declared on the number of nodes or on the length of a
string, and none is needed.** Each node costs at least one byte of a JSON
text and at least one wire byte, so both are bounded by the input's length.
Every read of a variable table is already linear in that length and draws
its storage from the caller's allocators (§6.5). This follows the rule that
a bound is declared only when every writer and reader enforces it.

**The writer** (`ToJson`, its measure and its options) writes the tape as
the canonical text of §16.1: pretty-printed, one trailing newline, members
in tape order, every number's token verbatim, and
every string and key escaped by §16.3's writer. Measure equals write byte for
byte. A tape the strict read accepted is always writable. A tape a program
built is checked by the same scan on its way out, and a breach returns `-1`.
The text the writer emits is RFC 8259, so it is a text §16.12's layer accepts.
Of §16.2's three options, only **compact** reaches inside a raw value. **Omit
defaults** never drops a member or an element, because each one is data.
It elides a field of this table only when the field holds `null`, the
default. **Null as empty** has no array row to act on here, because a `null`
inside a raw value is data. An OPTIONAL of this table (`?JsonValue`) reads
`null` as ABSENT, because §16.2's optional row is consulted first. So no
field of this table can tell an absent key from a `null`, and that is stated
here, not hidden.

**THE POLICY BOUNDARY.** The JSON step runs §16.12's layer FIRST and builds
nothing from a text the layer refuses. So an `n_` file the policy refuses,
and an `i_` file whose row says `refuse`, never produces a tape and never
reaches the binary step. The `n_` files marked `accept (dialect)` reach it as
the values they spell, without their comments or trailing commas. A key past 255 decoded bytes is refused by this construct
(`json_value_key_too_long`) and by the layer alike, because a cut key is a
different value. No accepted file of the vendored corpus has a key longer than 7 bytes.
The binary
step never assumes that its input came through the JSON step. A wire is
input, and the scan above re-checks every bound a hostile writer could break.

**Held by test** (the pro cards that build it, Go first; `ToJson` and
`FromJson` are proposed, not yet built). Every accepted file of the corpus
goes JSON → tape → binary (with the trailer) → strict read → JSON, and the
result is `Equal` to the file. The reversed witnesses: one flipped byte in a
saved file is refused `integrity_mismatch`. For each row of the table above,
a file one past the bound is refused by its name at the JSON step, and a
hand-built wire one past it is refused by its name at the binary step. Depth
is pinned at 129 nested containers, which passes, and 130, which is refused, for the value as the root and again for a value one level down (128 passes, 129 is refused),
at both steps.
