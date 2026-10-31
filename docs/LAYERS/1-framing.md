# Layer 1: FRAMING

The lowest layer of the table read path, and the one every other layer stands
on: the record scan, the `L` bounds, the header-against-parent rule, what is
damage and what is evolution, and the node-table scan. Every rule here is
quoted from `docs/SPEC-TABLES.md` with its section, in present tense, and
numbered so a vector, a test and a model can cite one rule by number. Nothing
here is invented: a rule the spec does not state, or states two ways, is on the
FINDINGS list at the end and stays there until it is ruled.

What holds the layer:

- **Vectors** — the `framing` rows of `testdata/conformance/tables/MANIFEST.txt`,
  one per rule and bound, hand-built as `testdata/wire/tables/framing/*.bin`
  from the annotated byte scripts in
  `test/conformance/harness/framing_vectors.go`, with the read report each
  owes on its manifest line and the value each owes in
  `testdata/conformance/tables/framing/<case>.json`. A row over DAMAGED bytes
  owes the refusal alone: its verdict is spelled `malformed` and its value is
  `-`, because a damaged load is discarded (D1) and nothing else it produced
  is defined. Every expected answer is derived from the rule text below, never
  from an implementation's output.
- **The oracle check** — `test/conformance/harness/framing_test.go` runs every
  vector through the Go engine (`internal/tablewire`), the reference every leg
  is fuzzed against, and compares the engine's report and value with the
  manifest's (on a damaged row, its refusal alone). The engine is held to this page.
- **The surface** — `framing`, which runs the same vectors through every leg's
  driver (`test/conformance/README.md`, "The surfaces").
- **The model** — `tla/Framing.tla`, a layout model of the record scan with
  offsets, `L` and the header read against the parent, in which damage ends
  the load (invariant `DamageEndsLoad`), checked with TLC on a small instance
  and reversed by four witnesses, `ResumeAfterDamage` among them
  (`tla/README.md`, "tla/Framing").

The reference a leg is compared against is the engine, and the engine is
compared against this page: a disagreement between the two is either an engine
defect (fixed, with a test and a negative control) or a finding below.

## 0. Damage discards the whole load

**D1. Damage anywhere refuses the whole load: the report says `malformed`, and
nothing else the read produced is defined.** §4, DAMAGE DISCARDS THE WHOLE
LOAD: "A read that meets damage anywhere in its input refuses the whole load:
it returns nothing to use, its report says `malformed`, and nothing else the
read produced is defined — not a value it decoded, a default it filled, a
counter it moved before or after the damage, nor anything a retaining read
kept or would save." Glenn, 2026-10-31: "failing to read something means the
whole read is undefined, in my book." and "there's no point hand wringing
trying to make partial reads well defined. if it fails to read, it's broken
and everything in it today is suspect. discard." Every rule below that names
a condition `malformed` names a case of D1, and none of them says what a load
returns after it: no partial value, no resume point, no default, no counter
other than the flag. A reader may stop at the first damage.

**D2. Evolution is not damage.** §4: "EVOLUTION IS NOT DAMAGE and every other
row of this section stands: an unknown field, variant, arm or slot skipped and
counted, a kind mismatch, a widening and a clamp; the load succeeds and the
report counts them." The rules below that skip, count and read on (R2, R4, H4,
H6, L1, T6, T7's unknown record) are evolution and keep their full answer,
value included.

**D3. Input no writer produces is damage (D1).** §4, INPUT NO WRITER PRODUCES
IS DAMAGE. Glenn, #391, 2026-09-03: "Table reads are not trusted, they will
come over the network. Table reads are exactly equivalent to type reads in
security posture." The tolerance of D2 is tolerance of versions, never of
shapes no version writes: one id twice in one body (R5), one key twice (H8,
H11), a header outside its body (H2, H3) and bytes after a nested union inside
its arm (L5) are each damage.

## A. The file frame

**F1. The form byte is read first, and a form the reader does not carry is a
refusal, never damage.** §3, THE FIRST BYTE: "BYTE `0` AND ANY UNASSIGNED VALUE
ARE REFUSED BY NAME ... A refusal is not damage and moves none of §4's six
counters, because nothing was decoded and there is nothing to count. THE FORM
BYTE IS READ FIRST, before any body and before any trailer, so a file that is
both an unknown form and damaged is a refusal." §3.3: form `2` stored on its
own "refuses BY NAME rather than merely by form byte".

**F2. A file is three parts: the form byte `1`, the root body, the id table.**
§3: "A saved table is THREE PARTS in this order: the FORM BYTE, the ROOT BODY,
and the ID TABLE." "The FORM BYTE is `1` for a file, and it is the whole
header." "Nothing else rides in front."

**F3. The root body ends at its own zero reference.** §3: "The ROOT BODY follows
the form byte and ENDS AT ITS OWN ZERO REFERENCE, wherever that falls."

**F4. The id table is found from the end: eight bytes of entry count, then
`8 × count` bytes of entries before them.** §3: "The final eight bytes are the
ENTRY COUNT, a fixed little-endian u64. The `8 × count` bytes before them are
the ENTRIES, each a fixed little-endian u64, and the body ends where the first
entry begins." It is "the one FIXED-WIDTH number on the wire".

**F5. A table that cannot be read whole is malformed, and nothing is
decoded.** §3, five ways, the first: "fewer than eight bytes in the file (a
buffer of no bytes included: it has no form byte to read, and it is
malformed, never refused), or a count whose `8 × count + 8` runs past the
front of the file, or a count that leaves no room for the form byte, or bytes
left over between the root body's terminator and the first entry. The whole
wire is malformed and nothing is decoded." And the fifth: "A TABLE THAT
CARRIES ONE ID TWICE: the whole wire is malformed, on the first rule's terms."
The slack rule is stated again in the file's own list: "Any byte between the
root's terminator and the table's first entry is `malformed`, because no field
claims it and the two ends of the file have met." D1.

**F6. The entry count has a maximum of 4096, and a count above it is malformed
on F5's terms.** §3, THE DISTINCTNESS CHECK: "a reader refuses as MALFORMED, on
the first rule's terms, any table whose entry count exceeds 4096 ... whatever
the buffer would hold. At or below it the reader proves the entries distinct."

**F7. An entry nobody references costs nothing and counts nothing.** §3: "An
entry this reader cannot name is the ordinary `unknown` of §4, counted when a
field, a variant, an arm or a key names it, and never at resolve time.
Resolving a table with twenty unnameable entries counts nothing at all if no
body references them."

**F8. A reference names a slot of the id table, counted from `1`; reference
`0` names no id and is the terminator, the enum's `None` and the empty arm.**
§3: "A REFERENCE is an unsigned LEB128 integer naming a slot of the id table,
counted from `1`. Reference `k` is the table's `k`th entry. Reference `0`
names NO ID." The last slot resolves: the held-by-test list, "the entry count
itself, which is the last legal slot and must RESOLVE".

**F9. A reference above the entry count is damage (D1).** §3, five ways, the
second: "A reference ABOVE the entry count: damage, wherever it sits."

**F10. Every length, count and index is a canonical LEB128, and a non-minimal
spelling is damage (D1).** §3: "Every one of them is CANONICAL, and a
non-minimal encoding is MALFORMED. `0x80 0x00` and `0x00` both spell zero,
and only the second is legal input ... An encoding past ten bytes, or a tenth
byte with a bit above the 64th value bit, is malformed on the same rule."
Five ways, the third: "A NON-CANONICAL reference, length, count or index: the
same."

**F11. The integrity trailer, when the root declares `integrity`, is checked
after the form byte and before the table is located, and the file is then read
without it.** §3: "A reader checks it after the form byte and before it
locates the id table, and then reads the file without it, so every rule below
speaks of the file with the trailer removed." No unit of the conformance
corpus declares `integrity`, so this rule carries no vector here; it is §2.14's
to hold.

## B. The record scan

**R1. A body is fields, each `id reference, kind (u8), payload`, ended by a
zero reference.** §3: "A body is a sequence of FIELDS, each `id reference,
kind (u8), payload`, terminated by a ZERO REFERENCE. The terminator is one
byte, the value `0`."

**R2. A field whose id the reader cannot name is skipped by its kind and
counted `unknown`, once.** §4: "Unknown field (newer writer): skipped by its
length, counted." §3: "Skipping a field you cannot name needs the kind byte and
nothing else."

**R3. Four skip rules cover the closed kind set, and a kind outside it is
damage (D1).** §3: "Kinds `1`–`11`, `18`–`29` and `36` skip their fixed
width. Kinds `17` and `30` read one LEB128 value and stop. Kinds `12`, `13`,
`14`, `16`, `31`, `32` and `33` read `L` and skip `L` bytes. Kind `15` reads
the arm id reference and stops there if it is `0`, else reads the kind byte,
then `L`, and skips `L` bytes. A kind a reader does not know at all is not
skippable and is framing damage ... At a position the reader names and at one
it cannot alike, the failed skip is damage and the whole load is discarded
(§4), so no counter the read moved on the way is defined." Kind `34`: "A
reader of this major therefore meets `34` only as DAMAGE — a kind it cannot
skip, exactly as it meets `37` or `200`."

**R4. A field at a position the reader names, under another kind, is skipped
by that kind's rule and counted `kind_mismatch`; the widening pairs decode
instead.** §4: "Kind mismatch (a field changed type between builds): skipped,
never misdecoded, counted." The widening family is layer 2's; framing's share
is that the skip uses the WIRE kind's rule.

**R5. Field order is not contractual, and a repeated id is legal with the
last occurrence winning.** §3: "every field is found by its id, so any order
decodes the same value, and a body carrying an id more than once is legal
input, the last occurrence winning."

**R6. A reserved id in a body that is not its transport is damage (D1).**
§3.1: "A RESERVED ID IN ANY BODY BUT THE ONE WHOSE TRANSPORT IT IS, IS
MALFORMED ... The NODE-TABLE id's own body is a wire's ROOT body, so the id
inside a NESTED body is malformed ... The BUILD-VERSION id's and the
VOCABULARY id's own body is the announcement's (§3.3), so either anywhere else,
in a file and in a message alike, is malformed on the same terms. Each is
damage rather than a foreign name: `malformed` counts and the whole load is
discarded (§4), in a file and in a message batch alike."

## C. Lengths and bodies

**L1. A length-framed payload is `L` then `L` bytes, and `L` says where the
next field begins.** §3, payload table: kind `12` "`L`, then `L` bytes"; kind
`13` "`L`, then `L` bytes of table body"; kind `14` "`L`, then the array
body"; kind `16` "`L`, then the body". A field skipped as evolution (R2, R4,
H4) is stepped over by its `L`, and the parent reads on at the byte after it.

**L2. Damage inside a nested body is damage to the whole load (D1).** §4,
DAMAGE DISCARDS THE WHOLE LOAD. No nesting level stops alone and no parent
reads on past a damaged body.

**L3. A body's terminator is the end of its payload; a terminator that arrives
earlier, or not by `L`, is damage (D1).** §3: "A BODY'S TERMINATOR IS THE END
OF ITS PAYLOAD, and that holds for a kind `13` field, an array element, a map
entry and an arm whose payload is a body alike. The zero reference is the last
byte of the payload's `L`. A terminator that arrives earlier leaves bytes
inside `L` that no field claims, and a terminator that does not arrive by `L`
leaves the body unended; both are damage, `malformed` counts and the whole
load is discarded (§4)."

**L4. A length that runs past the body carrying it is damage (D1).** §3: "A
LENGTH THAT RUNS PAST THE BODY CARRYING IT IS DAMAGE, at any depth, the root
included: `malformed` counts and the whole load is discarded (§4)."

**L5. Bytes left inside a union arm's `L` after a whole inner union are not
damage, and the parent steps over them by the arm's `L`.** §3: "Bytes left
inside a union arm's `L` after a whole inner union are not damage: the append
rule lets a newer writer add at the end of an envelope, so the reader decodes
the inner union, steps over the rest of the arm by its `L` and counts
nothing, while a byte after the root's own terminator, outside every envelope,
stays malformed."

## D. Arrays, maps and keyed arrays: the header against the parent

**H1. An array body is `element kind (u8)`, `N`, then the elements, under the
field's `L`.** §3, payload table, kind `14`: "`L`, then the array body:
`element kind (u8)`, `N`, then the elements." Kind `16`: "`L`, then the body:
`element kind (u8)`, `N` = the number of SLOTS PRESENT, then N triples of `key
reference`, `L`, `L` bytes of element." A map "rides as kind `14` with element
kind `13`, an array of one generated `{ key, value }` table" (§3).

**H2. A body too short to carry its header, fewer than two bytes, is inert:
nothing decoded, nothing counted, the field keeps the value it has.** §4: "An
ARRAY body too short to carry its own header, which is the element kind byte
and the count and so fewer than two bytes, is INERT: no element is decoded,
no counter fires, and the field keeps the value it has ... The framing is not
damaged — the length agrees with the enclosing body and the walk continues
past it — so this is not `malformed`." §2.9 restates it for `[]T`: "A body TOO
SHORT TO CARRY ITS OWN HEADER is INERT, §4's rule unchanged."

**H3. The header is read against the enclosing buffer, not against the body's
own `L`; the elements stay bounded by `L`.** §2.9: "The decoding cursor reads
the count against the enclosing value buffer, so a continuation byte beyond
`L` can complete that header. This does not extend the element body: element
reads remain bounded by `L`." This is the #2400 rule, and §3, THE HEADER
AGAINST THE PARENT, states it for kind `14`, kind `16` and a map alike (N3).
It decides whether a header is evolution (H4) or damage (H5, D1). The engine
applies it to a map's and a keyed array's header
(`internal/tablewire` `mapField`, `keyed`), and PR #2400 and #2418 hold Dart
and Rust to it.

**H4. An element kind the reader does not declare is a kind mismatch for the
field: counted once, the field skipped whole by its `L`, the field at its
declared default.** §3: "For kinds `14` and `16` a reader compares the element
kind it declares against the one in the body and, when they differ, skips the
field and counts a kind mismatch (§4) ... the array reads empty, one
`kind_mismatch` counts, and the field is skipped whole by its `L`." §2.8: "A
map header whose ELEMENT kind is not `13` is the ordinary array kind mismatch
of §4." The header is judged before any element is read, so a count that
crossed the `L` (H3) under a foreign element kind is this event and nothing
else.

**H5. Elements decode inside the field's body bounds, and a count the body
cannot cover is damage (D1).** §4: "Array elements decode inside their field's
body bounds, so no value is ever read from a neighbor's bytes." "`malformed`
is reserved for a count the BODY cannot cover, which is damage and a
different thing."

**H6. A count past the reader's bound keeps the bounded prefix and counts
`clamped`; an unbounded array has no count to clamp.** §4: "A count past the
READER's bound keeps the bounded prefix and counts clamped; a count short of
it fills what the writer sent and leaves the reader's tail at its declared
defaults." §2.9: "`clamped` CANNOT FIRE ON THE COUNT of a `[]T`."

**H7. A keyed array's slot key of `0` is damage (D1).** §3.2: "A stored key
reference of `0` is MALFORMED rather than an unknown variant ... It is damage:
`malformed` counts and the whole load is discarded (§4)." A key the reader
cannot name "is skipped by its length and counted `unknown`" and "a slot the
writer never sent keeps its declared default", which are evolution (D2).

**H8. Malformed entry framing in a map, and a descending key, are damage
(D1).** §3 (L3) and §2.8: "DESCENDING: the body is not a map any conforming
writer produced. It is damage: `malformed` is set and the whole load is
discarded (§4, DAMAGE DISCARDS THE WHOLE LOAD)." A repeated key is evolution:
`duplicate`, last wins whole (D2).

**H9. A union arm is framed as a field: arm id reference, kind, `L`, `L`
bytes; a fixed-width arm whose `L` is not its width, and a length-shaped arm
damaged inside its `L`, are damage (D1).** §3: "AN ARM HEADER IS A FIELD
HEADER: the arm id reference, the arm's KIND byte, `L`, then `L` bytes of arm
payload." "A fixed-width arm whose `L` is not its kind's width, and a
length-shaped arm whose payload is damaged inside its `L`, are damage:
`malformed` counts and the whole load is discarded (§4)." A payload-free arm:
"A reader that declares an arm payload-free and meets an `L` that is not `0`
counts `malformed` and discards the whole load (§4)." A retyped arm is
evolution: "the arm skips by `L`, the union reads `None`, `kind_mismatch`
counts (§4), and the parent reads on" (D2). An arm id of `0` "is the empty
union and carries nothing after it, not even a kind byte ... a reader accepts
the id wherever it appears."

**H10. An array arm's header is read inside the arm's `L`, and a count that
crosses it is damage (D1).** §3: "AN ARRAY ARM IS THE ONE PLACE THE BOUND IS
NOT THE ENCLOSING BODY: an arm's `L` stands in for the field's own length (the
arm payload table, below), and the buffer an arm payload is read from is the
arm's, so an array arm's header is read inside the arm's `L` and a count that
crosses it is damage, which discards the whole load
(`arm_array_count_past_arm_length`)."

**H11. A keyed body carrying one slot key twice is damage (D3).** §3.2: "A key
rides ONCE per body: a keyed body carrying one key reference twice, whether
or not this reader can name the variant, is input no writer produces, and it
is damage" (`keyed_repeat_slot`, `keyed_repeat_unknown_slot`).

## E. The node table

**T1. The node table rides once, under the reserved id, as kind `12`, `L`,
then `node_count` and `node_count` records of `type id reference, length,
body`.** §3.1: "one field: id reference = the reserved node-table id, kind =
12, L, then L bytes ... the payload opens with the count and then carries the
records: node_count, node_count records, back to back: type id reference,
length, body." "The reserved node-table id is `0xFFFFFFFFFFFFFFFF`" and "takes
an ordinary entry in the id table like every other id". "The field rides
ONCE."

**T2. The table is whole or it is nothing, and a table that cannot be read
whole is damage (D1).** §3.1: "A node-table field arriving under a kind other
than `12`, a record whose length runs past the field, or bytes left over
inside the field make the whole table malformed, which is damage and
discards the whole load (§4). A reader never salvages part of a numbering."
"No index ever resolves against a numbering that failed."

**T3. The record scan is authoritative, and a `node_count` that disagrees
with it is malformed.** §3.1: "a reader scans records until the fields are
consumed and takes what it finds, and a `node_count` that disagrees with the
scan is malformed. Nothing — no directory, no region, no allocation — is sized
from `node_count` before the scan has confirmed it."

**T4. A type id reference of `0` is malformed.** §3.1: "A type id reference of
`0` is malformed, because `0` names no id and a record must say what it is."
§3, five ways, the fourth, names the two places.

**T5. Only the root body carries the node table; it is found by id, not by
position.** §3.1: "Only the ROOT body carries the node table. No nested body
ever does." "Field order is not part of the contract (§3), so a reader finds
it by id." The nested case is R6.

**T6. A reader that cannot name the reserved id counts one `unknown` for the
field; a reader that can counts nothing.** §3.1: "The `unknown` count for it
is ONE, whatever the save's size ... A reader that CAN name it counts
nothing."

**T7. A record whose type id this root cannot place is skipped by its length,
counted `unknown`, and keeps its index; an index above `node_count + 1`, or a
non-canonical one, is damage (D1).** §3.1: "it is skipped by its length,
counted unknown. It KEEPS ITS INDEX." "An index above `node_count + 1`, or one
whose LEB128 is not canonical ... malformed, which discards the whole load
(§4)." "a pointer field's payload is a NUMBER: it is bounds-checked and
stored, never followed."

## F. The report

**C1. `malformed` is a flag beside five counters, and a refusal moves none of
them.** §4: "The wire-tolerance counters are `unknown`, `kind_mismatch`,
`widened`, `clamped`, `duplicate` and the `malformed` flag." §3: "A refusal is
not damage and moves none of §4's six counters." On a damaged read the flag is
the whole observable answer (D1).

**C2. Counters on a damaged read are not defined.** D1: "nothing else the
read produced is defined — ... a counter it moved before or after the
damage". Counters stand on a read that meets no damage, including the ones
an evolution event moved before a later evolution event (§2.8: "Events
counted inside earlier entries of that map stand", said of a key-kind
mismatch, D2).

**C3. An `unknown` is counted where a body names an id, never at resolve time
(F7); a node record of a type the root cannot place counts once at the record
and not once per pointer (T7).**

## FINDINGS, and how each was settled

Each finding is settled by a ruling or by the spec's own rule for a
contradiction (docs/VERSIONING.md: the sentence with the golden wins, and the
other is a bug in prose), and the settled answer is written into SPEC-TABLES
in present tense and pinned as a `framing` row. A row over damaged bytes owes
the refusal alone (D1).

- **N1. What a damaged nested body reads as. RULED (Glenn, 2026-10-31):**
  "failing to read something means the whole read is undefined, in my book."
  and "there's no point hand wringing trying to make partial reads well
  defined. if it fails to read, it's broken and everything in it today is
  suspect. discard." The question was whether a damaged nested body keeps
  what it decoded or reads its declared defaults; under D1 it is neither,
  because the whole load is discarded. §4's framing-damage row and §3's
  early-terminator defaults are replaced by D1. The two vectors held for it,
  `nested_terminator_missing` (buff `L = 6`, the multiplier field whole and no
  terminator) and `nested_damage_midway` (buff `L = 9`, the multiplier field
  whole then a field under kind `37`), are refusal rows.
- **N2. An `L` past its carrying body.** Settled: §3 states "A LENGTH THAT
  RUNS PAST THE BODY CARRYING IT IS DAMAGE", at any depth, the root included
  (L4).
- **N3. The header-against-parent rule for kinds `14` and `16` and maps.**
  Settled: §3, THE HEADER AGAINST THE PARENT, states it for kind `14`, kind
  `16` and a map alike, and §2.8 and §3.2 cite it (H3). Java, Luau and
  Squirrel are owed the rule where it decides between a kind mismatch and
  damage.
- **N4. An array ARM's header is bounded by the arm's `L`.** Settled: §3
  states it as the one place the bound is not the enclosing body, and a count
  that crosses it is damage (H10).
- **N5. A count header that cannot complete even in the enclosing buffer.**
  Settled: §2.9 and §3 call it damage, not inertness (D1); inertness is for a
  body of fewer than two bytes alone (H2).
- **N6. An empty buffer.** Settled: §3 says a buffer of no bytes has no form
  byte to read and is malformed, never refused (F5); `frame_empty_buffer` is a
  refusal row.
- **N7. The node table riding twice.** Settled (Glenn, 2026-10-31, the strike
  list): one id twice in the root body, which is damage (R5, D3);
  `node_table_twice` is a refusal row.
- **N8. The 4096 entry bound.** An engine fix, not a finding: F6 is
  unambiguous and the engine applies it (`ir.TableWireIdTableBound`,
  `internal/tablewire` `trailerBounded`, held by
  `TestIdTableAtTheBoundResolvesAndOneOverIsMalformed` with its reversed bound
  as the negative control). The legs' own readers are owed the same check.
- **N9. An unskippable kind at a position the reader NAMES.** Settled by D1:
  the failed skip is damage at a named position and an unnamed one alike, and
  no counter the read moved on the way, `kind_mismatch` included, is defined.
  `frame_kind_unknown_37`, `frame_kind_reserved_34`, `frame_kind_zero` and
  their named twins (`*_named`) are refusal rows.
