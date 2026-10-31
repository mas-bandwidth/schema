# Layer 1: FRAMING

The lowest layer of the table read path, and the one every other layer stands
on: the record scan, the `L` bounds, the header-against-parent rule, how damage
is counted, where reading resumes, and the node-table scan. Every rule here is
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
  `testdata/conformance/tables/framing/<case>.json`. Every expected answer is
  derived from the rule text below, never from an implementation's output.
- **The oracle check** — `test/conformance/harness/framing_test.go` runs every
  vector through the Go engine (`internal/tablewire`), the reference every leg
  is fuzzed against, and compares the engine's report and value with the
  manifest's. The engine is held to this page.
- **The surface** — `framing`, which runs the same vectors through every leg's
  driver (`test/conformance/README.md`, "The surfaces").
- **The model** — `tla/Framing.tla`, a layout model of the record scan with
  offsets, `L`, the header read against the parent, damage and resume, checked
  with TLC on a small instance and reversed by three witnesses
  (`tla/README.md`, "tla/Framing").

The reference a leg is compared against is the engine, and the engine is
compared against this page: a disagreement between the two is either an engine
defect (fixed, with a test and a negative control) or a finding below.

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

**F5. A table that cannot be read whole stops the file: nothing is decoded and
one event is counted.** §3, five ways, the first: "fewer than eight bytes in
the file, or a count whose `8 × count + 8` runs past the front of the file, or
a count that leaves no room for the form byte, or bytes left over between the
root body's terminator and the first entry. The whole wire is malformed,
nothing is decoded, and one event is counted. This is the one malformed case
that stops the file rather than a nesting level." And the fifth: "A TABLE THAT
CARRIES ONE ID TWICE: the whole wire is malformed, on the first rule's terms."
The slack rule is stated again in the file's own list: "Any byte between the
root's terminator and the table's first entry is `malformed`, because no field
claims it and the two ends of the file have met."

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

**F9. A reference above the entry count is framing damage on the body that
carries it.** §3, five ways, the second: "framing damage on the body that
carries it, by §4's rule. The body stops, `malformed` counts, and the parent
reads on past the length that frames it."

**F10. Every length, count and index is a canonical LEB128, and a non-minimal
spelling is malformed on the body that carries it.** §3: "Every one of them is
CANONICAL, and a non-minimal encoding is MALFORMED. `0x80 0x00` and `0x00`
both spell zero, and only the second is legal input ... An encoding past ten
bytes, or a tenth byte with a bit above the 64th value bit, is malformed on
the same rule." Five ways, the third: "A NON-CANONICAL reference, length,
count or index: the same, on the body that carries it."

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
framing damage.** §3: "Kinds `1`–`11`, `18`–`29` and `36` skip their fixed
width. Kinds `17` and `30` read one LEB128 value and stop. Kinds `12`, `13`,
`14`, `16`, `31`, `32` and `33` read `L` and skip `L` bytes. Kind `15` reads
the arm id reference and stops there if it is `0`, else reads the kind byte,
then `L`, and skips `L` bytes. A kind a reader does not know at all is not
skippable and is framing damage." Kind `34`: "A reader of this major therefore
meets `34` only as DAMAGE — a kind it cannot skip, exactly as it meets `37` or
`200`."

**R4. A field at a position the reader names, under another kind, is skipped
by that kind's rule and counted `kind_mismatch`; the widening pairs decode
instead.** §4: "Kind mismatch (a field changed type between builds): skipped,
never misdecoded, counted." The widening family is layer 2's; framing's share
is that the skip uses the WIRE kind's rule.

**R5. Field order is not contractual, and a repeated id is legal with the
last occurrence winning.** §3: "every field is found by its id, so any order
decodes the same value, and a body carrying an id more than once is legal
input, the last occurrence winning."

**R6. A reserved id in a body that is not its transport is malformed on that
body.** §3.1: "A RESERVED ID IN ANY BODY BUT THE ONE WHOSE TRANSPORT IT IS, IS
MALFORMED ... The NODE-TABLE id's own body is a wire's ROOT body, so the id
inside a NESTED body is malformed ... The BUILD-VERSION id's and the
VOCABULARY id's own body is the announcement's (§3.3), so either anywhere else,
in a file and in a message alike, is malformed on the same terms. Each is
damage rather than a foreign name: that body stops, `malformed` counts, and the
parent reads on past its length."

## C. Lengths, bodies and where reading resumes

**L1. A length-framed payload is `L` then `L` bytes, and `L` says where the
next field begins whatever the payload turned out to be.** §3, payload table:
kind `12` "`L`, then `L` bytes"; kind `13` "`L`, then `L` bytes of table
body"; kind `14` "`L`, then the array body"; kind `16` "`L`, then the body".
§3, ill-formed text: "a length-framed field has one: `L` says where the next
field begins whatever the payload turned out to be." §4: "the parent continues
past the field's declared length".

**L2. Framing damage stops the damaged nesting level, keeps what it decoded
there, flags malformed, and the parent continues past the field's declared
length.** §4: "Framing damage: decode stops the damaged nesting level, keeps
what it decoded there, flags malformed, and the parent continues past the
field's declared length — one bad subtable never takes down the rest of the
file." The root body has no parent, so damage there ends the read with what
the root decoded standing.

**L3. A body's terminator is the end of its payload; a terminator that arrives
earlier is framing damage, the field reads its declared default, and the
enclosing body continues past the payload by `L`.** §3: "A BODY'S TERMINATOR IS
THE END OF ITS PAYLOAD, and that holds for a kind `13` field, an array element,
a map entry and an arm whose payload is a body alike. The zero reference is
the last byte of the payload's `L`. A terminator that arrives earlier leaves
bytes inside `L` that no field claims, which is framing damage: `malformed`
counts, the field reads its declared default and a union reads `None`, and the
enclosing body continues past the payload by `L`. An array element takes its
own declared defaults. A map checks the entry's terminator during the key
scan, before choosing a slot: malformed entry framing stops the map with its
earlier ascending prefix intact, and the parent continues past the map's
`L`." (See finding N1 on the tension with L2.)

**L4. A length that runs past the body carrying it is framing damage on that
body.** §3's skip rule reads "`L` and skip `L` bytes", and §4's row L2 names
the recovery; the two together are the rule the engine and every leg apply:
the carrying body stops, `malformed` is flagged, the parent reads on past the
carrying body's own `L`. (See finding N2: the sentence is implied, not
written.)

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
reads remain bounded by `L`." This is the #2400 rule. The spec states it in
§2.9 for the unbounded array and nowhere else; §2.8 and §3.2 are silent (see
finding N3). The engine applies it to a map's and a keyed array's header
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

**H5. Elements decode inside the field's body bounds: a count the body cannot
cover yields the bounded prefix and the malformed flag.** §4: "Array elements
decode inside their field's body bounds: a count the body cannot cover yields
the bounded prefix and the malformed flag, never values fabricated from a
neighbor's bytes." §4: "`malformed` is reserved for a count the BODY cannot
cover, which is framing damage." The parent reads on past the field's `L`.

**H6. A count past the reader's bound keeps the bounded prefix and counts
`clamped`; an unbounded array has no count to clamp.** §4: "A count past the
READER's bound keeps the bounded prefix and counts clamped; a count short of
it fills what the writer sent and leaves the reader's tail at its declared
defaults." §2.9: "`clamped` CANNOT FIRE ON THE COUNT of a `[]T`." A crossed
count on a bounded array (H3) is past every bound and the body cannot cover
it, so H6 and H5 both fire for it.

**H7. A keyed array's slot key of `0` is malformed: the body stops and keeps
what it decoded.** §3.2: "A stored key reference of `0` is MALFORMED rather
than an unknown variant ... The reader stops that body, keeps what it decoded,
and flags malformed." A key the reader cannot name "is skipped by its length
and counted `unknown`" and "a slot the writer never sent keeps its declared
default".

**H8. A map's entry framing is checked during the key scan: malformed entry
framing stops the map with its ascending prefix intact, and the parent
continues past the map's `L`; a descending key is the same stop.** §3 (L3's
last sentence) and §2.8: "DESCENDING: the body is not a map any conforming
writer produced. The map keeps the ascending prefix it has, `malformed` is
set, the map's remaining bytes are skipped by the map's `L`, and the PARENT
reads on past the field's length. That is §4's framing-damage rule, at the
map."

**H9. A union arm is framed as a field: arm id reference, kind, `L`, `L`
bytes; a fixed-width arm whose `L` is not its width, and a length-shaped arm
damaged inside its `L`, are the arm's own framing damage: the union reads
`None`, `malformed` counts, the parent reads on past `L`.** §3: "AN ARM HEADER
IS A FIELD HEADER: the arm id reference, the arm's KIND byte, `L`, then `L`
bytes of arm payload." "A fixed-width arm whose `L` is not its kind's width,
and a length-shaped arm whose payload is damaged inside its `L`, are that
arm's own framing damage: the union reads `None`, `malformed` counts, and the
parent reads on past `L`." A payload-free arm: "A reader that declares an arm
payload-free and meets an `L` that is not `0` counts `malformed` and reads
`None`." An arm id of `0` "is the empty union and carries nothing after it,
not even a kind byte ... a reader accepts the id wherever it appears."

**H10. An array arm's header is read inside the arm's `L`.** This is the
engine's rule (`internal/tablewire` `unionCell` reads an arm out of a reader
bounded to its `L`), pinned by the fuzz vector
`arm_array_count_past_arm_length` and held in Rust by #2418, and it is the
opposite bound from H3 because an arm's `L` "stands in for the field's own
length" (§3, the arm payload table). The spec does not state it in words (see
finding N4).

## E. The node table

**T1. The node table rides once, under the reserved id, as kind `12`, `L`,
then `node_count` and `node_count` records of `type id reference, length,
body`.** §3.1: "one field: id reference = the reserved node-table id, kind =
12, L, then L bytes ... the payload opens with the count and then carries the
records: node_count, node_count records, back to back: type id reference,
length, body." "The reserved node-table id is `0xFFFFFFFFFFFFFFFF`" and "takes
an ordinary entry in the id table like every other id". "The field rides
ONCE."

**T2. The table is whole or it is nothing.** §3.1: "A node-table field
arriving under a kind other than `12`, a record whose length runs past the
field, or bytes left over inside the field make the whole table malformed:
every pointer in the save reads null and one event is counted. A reader never
salvages part of a numbering." "No index ever resolves against a numbering
that failed."

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
non-canonical one, is malformed and that pointer reads null, the body reading
on.** §3.1: "it is skipped by its length, counted unknown. It KEEPS ITS
INDEX." "An index above `node_count + 1`, or one whose LEB128 is not canonical
... malformed, and the pointer stays null." "a pointer field's payload is a
NUMBER: it is bounds-checked and stored, never followed."

## F. How damage is counted

**C1. `malformed` is a flag beside five counters, and a refusal moves none of
them.** §4: "The wire-tolerance counters are `unknown`, `kind_mismatch`,
`widened`, `clamped`, `duplicate` and the `malformed` flag." §3: "A refusal is
not damage and moves none of §4's six counters." Where the spec says "one
event is counted" of a malformed read, the observable is the flag.

**C2. Events counted before a stop stand.** §2.8: "Nothing after that entry is
decoded and nothing after it is counted. Events counted inside earlier
entries of that map stand." The same holds at every level: damage stops a
nesting level and undoes no count.

**C3. An `unknown` is counted where a body names an id, never at resolve time
(F7); a node record of a type the root cannot place counts once at the record
and not once per pointer (T7).**

## FINDINGS, for the parent

Rules the spec leaves unstated or states two ways. None is resolved here; each
vector that depends on one is held out of the manifest and listed with its
bytes so the ruling can land it.

- **N1. "keeps what it decoded there" against "reads its declared default".**
  §4 (L2) says framing damage inside a nesting level keeps what that level
  decoded; §3 (L3) says an early terminator makes the field read its declared
  default. Read together they are consistent only if "early terminator" is the
  one damage that defaults and every other damage keeps the prefix. The engine
  does neither consistently: `internal/tablewire` `field` refills a kind `13`
  body to its defaults whenever the body's cursor is not at `L` after the
  walk, which defaults a body damaged midway (an unskippable kind inside it,
  an `L` past the body) and keeps a body whose damage lands exactly at `L` (a
  missing terminator after a whole last field). The fifteen legs are fuzzed
  against the engine, so they share whatever it does. Two vectors are held
  out: `nested_terminator_missing` (buff `L = 6`, the multiplier field whole
  and no terminator; §4 says multiplier 1.5 stands, the engine agrees because
  the cursor is at `L`) and `nested_damage_midway` (buff `L = 9`, the
  multiplier field whole then a field under kind `37`; §4 says 1.5 stands, the
  engine refills to 1). The ruling decides which sentence is the rule.
- **N2. An `L` past its carrying body is not stated as damage in words.** §3's
  skip rule and §4's framing-damage row together imply L4, and every reader
  applies it, but no sentence says "a length that runs past the body carrying
  it is framing damage on that body". The vectors hold the engine's reading.
- **N3. The header-against-parent rule is stated once, for `[]T`.** §2.9 is
  the only place that says the count is read against the enclosing buffer, as
  a description of the cursor; §3's array row, §2.8 (maps) and §3.2 (keyed
  arrays) say nothing about which buffer bounds the header. #2400 and #2418
  settled it for Dart and Rust by citing the engine and the C reader, and
  Java, Luau and Squirrel still bound a map's and a keyed array's header by
  the body's own `L` (#2418's body). The rule belongs in §3 beside the array
  row, stated for kinds `14` and `16` alike.
- **N4. An array ARM's header is bounded by the arm's `L`, which is the
  opposite of N3, and the spec does not say so.** The engine reads an arm out
  of a reader framed by its `L`; `arm_array_count_past_arm_length` pins it
  and #2418 holds Rust to it. The arm payload table says an arm's `L` "stands
  in for the field's own length", which is the reason, but the bound on the
  header is not written. H10 records the engine's rule.
- **N5. §2.9's "A count header incomplete even in the enclosing buffer remains
  inert" against §4's damaged count.** The engine flags `malformed` for a
  count whose LEB128 cannot complete, and §4's inert rule is for a body of
  fewer than two bytes. The difference is unobservable on a first occurrence
  (the field keeps its default either way, and an array that is the last
  field of a root with no bytes left also loses the root's terminator), so no
  vector is held; the two sentences should still say one thing.
- **N6. An empty buffer.** §3 reads the form byte first and calls "fewer than
  eight bytes in the file" malformed; a buffer of zero bytes has no form byte
  to read. The engine answers `malformed`. The page should say which.
- **N7. The node table riding twice.** §3.1 says "The field rides ONCE" as a
  writer's rule and gives a reader no rule for a second occurrence. The engine
  takes the last occurrence's payload and reads the earlier one as an ordinary
  skipped field. Not vectored.
- **N8. The 4096 entry bound was a reader rule the engine did not apply.**
  The engine's `trailer` read any count the buffer could hold; the oracle
  check found it on `frame_entries_4097`. F6 is unambiguous, so the engine is
  fixed (`ir.TableWireIdTableBound`, `internal/tablewire` `trailerBounded`,
  held by `TestIdTableAtTheBoundResolvesAndOneOverIsMalformed` with its
  reversed bound as the negative control). This is an engine fix, not a
  finding, and is listed so the parent knows the engine moved and that the
  legs' own readers are owed the same check.
- **N9. An unskippable kind at a position the reader NAMES.** §3 (R3) says a
  kind outside the closed set is framing damage; §4's `kind_mismatch` is "a
  field changed type between builds", and `37`, `34` and `0` are not types.
  Whether the mismatch counter also moves when the damaged kind byte sits on
  a named field is unstated. The engine counts `kind_mismatch` and then
  `malformed` (it judges the kind against the declaration before it tries to
  skip), and the fifteen legs are fuzzed to that answer. The vectors
  `frame_kind_unknown_37`, `frame_kind_reserved_34` and `frame_kind_zero` sit
  at an unnamed position, where only `malformed` is in question; their named
  twins (`*_named`) are held out until the sentence is written.
