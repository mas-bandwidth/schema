# The tables conformance data, exactly

Every file under `testdata/conformance/tables` is DATA. Nothing here names a
language, and nothing here is executable. A port of the tables layer implements
the driver contract in `test/conformance/README.md` against this directory, and
the harness is the gate.

The forms this data describes are docs/SPEC-TABLES.md's: §3 the table wire, §4 the
read report, §7 the cooked form, §16 the JSON text, §19 the block form. This
page states the FILE shapes only — where a rule about a form belongs, the
section is cited rather than restated.

## The tree

```
MANIFEST.txt          the registry: every case, by kind
reports.txt           the read report of every evolution case  (generated)
json/<instance>.json  the §16 text of every instance           (generated)
json-hostile/<case>/  one tree per rule the text form states
cook/<case>.dump      the canonical node dump of every cook    (pinned)
cook-write/<name>     the cooked file the tool wrote, per order (generated)
block/<name>.dump     the canonical row dump of every block    (pinned)
FORMAT.md             this page
```

Wire bytes are REFERENCED where they already live — `testdata/wire/tables/` —
rather than copied. They are the same files `test/tables/main.cpp` pins, and one
golden with two homes is one golden that can disagree with itself.

## MANIFEST.txt

Line-oriented. Blank lines and lines beginning `#` are comments. Fields are
separated by runs of spaces or tabs; the last field of a `forgery` line runs to
the end of the line.

```
unit         <key> <schema path>...
instance     <name> <unit> <root> <wire file> [no-text]
connection   <key> <unit> <build version> <announcement wire file>
message      <name> <connection> <root> <file-form wire> <message-form wire>
report       <case> <unit> <root> <wire file>
json-hostile <case> <unit> <root> <tree> <verdict>
cook         <case> <unit> <root> <dump file>
cook-write   <instance> <little-endian file> <big-endian file>
block        <name> <unit> <block file> <dump file>
forgery      <name> <kind> <subject> <base> <pointer> <offset> <width> <value> <extent> <verdict> <label>
refusal      <forgery> <reason>
```

- **`unit`** names a compilation unit by the key every other line uses.
- **`instance`** is one table instance in TWO forms that must agree: the wire
  bytes at `<wire file>` and the text at `json/<name>.json`. The same instance,
  both ways.

  **`no-text` says the corpus carries this one on the WIRE alone**, by the text
  form's own rules: a pointer chain nests in the text as deep as it is long
  (docs/SPEC-TABLES.md §16.7), and past the depth cap every walk carries the
  writer refuses it in every implementation; and a byte buffer named from two
  slots has no spelling a string can carry (§16.7), so the writer refuses that
  graph too. A `json/<name>.json` for either would be a text every reader
  refuses. `harness generate` writes none and the harness asks no leg for one.
  The marker is the data saying so on the instance's own line, which is louder
  than a missing file quietly tolerated.
- **`connection`** is one CONNECTION's announced id table
  (docs/SPEC-TABLES.md §3.3): the unit whose whole vocabulary it carries, the
  BUILD VERSION that keys it, and the announcement itself, which is an ordinary
  form-`1` file whose trailer IS the table. The build version is a column
  rather than a fact read out of the file, so a build that moves is visible in
  the review of this file, exactly as a forgery's damaged word is.
- **`message`** is one value in BOTH forms. The FILE form is an ordinary
  `instance` and rides every surface an instance rides; the MESSAGE form rides
  the `wire` and `message` surfaces alone, because its text is the file-form
  vector's byte for byte and a second `json/` file would be one golden with two
  homes. What the pair pins is the RESOLVED FORM: the two bodies are equal once
  every reference is replaced by the id it names and every length recomputed,
  and their reference bytes are expected to differ, because a file's slots are
  its own first-use order and a connection's are the unit's projection order.
- **`report`** is bytes read by a type that did not write them — the evolution
  class. The counters and the verdict live in `reports.txt`, keyed by `<case>`.
- **`json-hostile`** is one tree per rule the text form states (§16.2, §16.3,
  §17.5), and the verdict is `refused` or the §4 report the read produces,
  `<unknown>,<kind_mismatch>,<clamped>,<duplicate>,<malformed>`. `<tree>` is the
  directory `schema pack` reads, so the text is `<tree>/<root>.json` (§17). The
  verdict is HAND-AUTHORED, which is what makes it an expectation rather than a
  restatement of what one engine happens to do.
- **`cook`** names a cooked file's CASE, the root it is opened at, and the dump
  its Open must produce. Case and root are two columns because one root can have
  more than one fixture — `Scene` and `SceneValued` are the same table read two
  ways. The FILE is not committed: `test/cookgen` writes it, deterministically,
  and the harness runs it. What is pinned is the dump. It is also the fixture
  `cook-foreign` makes foreign, on the same terms as `block`'s.
- **`cook-write`** names an INSTANCE the file already carries and the two files
  `schema cook` produced from its wire, little-endian then big. The instance
  line holds the unit, the root and the wire, so none of them is repeated here;
  a line naming no instance is refused when the manifest is read. The two files
  are GENERATED — `make conformance-generate` writes them through the same
  public driver `schema cook` uses — because the TOOL IS THE REFERENCE (§7.6):
  a runtime that writes a cook writes the tool's bytes or it has written a
  second format.
- **`block`** is a block image an Open must accept, and the ROW DUMP its reader
  must produce out of it. It is also the fixture the `block-foreign` surface
  makes foreign: that surface adds no line here, because a foreign image is not
  a different FILE — it is the same file with its magic word reversed, which
  only the reader's own byte order gives meaning to (test/conformance/README.md).
- **`forgery`** is one damaged fixture and the verdict every implementation owes
  it. `<kind>` is `block` or `cook`; `<subject>` names the block or the cook's
  root; `<base>` names the fixture the patch is applied over — a `block` line's
  name, or a `cook` line's case. `<pointer>` is the BUFFER the caller holds —
  `0` an aligned base, `1`..`63` that many bytes past one, `null` no buffer at
  all. `<offset>` and `<width>` locate the word and `<value>` is written
  little-endian; all three are COMMA-SEPARATED lists of equal length, because a
  forgery may damage more than one word, and an `<offset>` of `-1` is a forgery
  that damages none. `<extent>` is the length the CALLER claims (`-1`: the
  file's own), and `<verdict>` is `refuse` or `open`.
- **`refusal`** is the REASON one forgery's refusal names (docs/SPEC-TABLES.md
  §7, §19.2): `refusal <forgery> <reason>`, where `<forgery>` is a `forgery`
  line's name and `<reason>` the enum's own value name, `not_a_cook`,
  `foreign_order`, `wrong_build_version`, `reserved_not_zero`, `bad_alignment`,
  `truncated`, `unaligned_base` or, for a block, `bad_layout`. It takes its kind
  from the forgery it names, and the `cook-reason` and `block-reason` surfaces
  answer it with one word. A row per value, and a row per ORDER the check
  states: a file that fails two clauses answers the first.

A forgery is carried as a PATCH rather than as a whole file because a patch is
what a person can review. The harness materialises the file and hands a driver a
path, so no driver implements a patcher.

**Three of the forgery columns are not file facts and that is why they are
columns.** `<extent>` is the length the caller CLAIMS, which may run past the
bytes the file carries — two rows of the block battery are about exactly that —
or short of them, which is what a truncation is. `<pointer>` is the buffer that
caller holds, and an unaligned base is a pointer fact no file can hold. A driver
allocates EXACTLY the claim, places the base as the pointer column says, copies
what fits and zeroes the rest.

## reports.txt

```
<case>  <unknown>,<kind_mismatch>,<widened>,<clamped>,<duplicate>,<malformed>,<verdict>
```

The six counters of docs/SPEC-TABLES.md §4, `<malformed>` spelled `true` or
`false`, and the VERDICT beside them: `read` or `refused`.

**The verdict is distinct from a clean read, and that is why it exists** (§3). A
reader that meets a FORM BYTE it does not carry refuses the wire by name: it
decodes nothing, moves none of the six counters and reports no damage — so a
refusal and a clean read print the same six zeros and the same false flag, and
only the verdict tells them apart. `form_zero`, `form_three` and `form_ff` are
the three rows that say so, over the three forms no reader knows, and
`message_as_file` is the fourth: form `2` is a form this build DOES carry
(docs/SPEC-TABLES.md §3.3), and a message handed to a reader that expected a
file refuses because its id table is somewhere else.

A driver prints the verdict on its `report` surface beside the counters. A TEXT
read never refuses — only a form byte does — so the `json-hostile` rows carry
`read` on every line.

Generated by the compiler's own engine — a third implementation of the wire,
written from neither backend — with `make conformance-generate`.

## json/&lt;instance&gt;.json

The docs/SPEC-TABLES.md §16 text of the instance, and EXACTLY that — **which
includes the one trailing newline the form ends with** (§16.1). Every writer
emits it, `schema unpack` and every backend's `ToJson` alike, and every reader
accepts a text with or without one; so the byte is part of the form rather than
a file convention, and a golden carries the form and nothing around it, the way
a wire golden carries exactly the wire.

Generated with `make conformance-generate`, and each text is proved COMPLETE
before it lands: it is packed back to wire bytes and must equal the golden it
came from, byte for byte. A text that lost a field cannot pass that.

## json-hostile/&lt;case&gt;/&lt;root&gt;.json

One tree per rule the text form states, in `schema pack`'s own directory shape
(§17). The corpus lives here rather than beside the packer because it was always
DATA: the harness's `json-hostile` surface and `test/tables/hostile_main.cpp`
read the same `json-hostile` rows of MANIFEST.txt, and so does the engine's own
test. One corpus, one set of expectations, three gates asking different things
of it.

## cook/&lt;case&gt;.dump

The canonical node dump of a cooked file (docs/SPEC-TABLES.md §7.5): the walk every
reader makes through its OWN derefs, written as text, so two implementations'
walks are byte-compared rather than merely both succeeding. A record laid out
one byte differently INSIDE a node moves no node offset and no directory entry,
so this is the gate the attribution check cannot be.

```
node <index> <TypeName> @<byte offset>
  <field path> = <value>
```

- Nodes are numbered in visit order from the root at offset 0, one `node` line
  each, and a node is visited ONCE: sharing and a back-reference are the same
  fact (§6.3).
- **THE WALK IS DEPTH-FIRST AND EMITS AS IT GOES, so a node's own lines are NOT
  contiguous.** A pointer field emits its `-> @<offset>` line and then descends
  immediately, so the whole subtree's `node` blocks land before the parent's
  REMAINING leaf lines. In `Scene.dump` the root's own `tree`, `settings`,
  `alias`, `ground`, `layers` and `meta` lines sit at line 493, after node 125 —
  they are the root's, not that node's. A reader that emitted each node's leaves
  as one block and then descended produces a reordered dump on every fixture
  with a pointer that is not the last field, which is three of the six.
- Leaf lines are indented two spaces. A field path is dotted through by-value
  nesting and indexed `[n]` through array slots.
- Values: an integer in decimal, signed by its declared kind; a bool as `true`
  or `false`; a pointer as `-> @<offset>`, or `null` for a delta of zero; a
  string or `bytes` as its USED bytes in double quotes with every byte outside
  printable ASCII (and `"` and `\`) written `\xNN`, followed by ` len=<n>`.
- A field with a count companion emits `<path>#count = <n>` AFTER its slots; a
  `?T` emits `<path>#present = true|false` AFTER its value, and **an absent
  `?T` emits its value all the same** — the storage is there whether the
  presence bool is set or not, and a dump of a region is a dump of its bytes.
  Both companions follow the field they belong to and neither replaces it.
- A float has no canonical cross-language spelling here and the corpus this
  covers has none. A dump that meets one REFUSES rather than inventing a
  spelling in passing.

Pinned from the reference leg with `make conformance-pin`. C++ writes the pins
and every other leg byte-compares them, exactly as the wire goldens work.

**A value-initialised chain locks structure and almost no VALUES**, because
there are almost none in it: `SceneValued` is the same chain with every
non-pointer leaf filled (`test/cookgen --values`), so its dump locks what a
reader READS out of a node as well as where the node is.

## cook-write/&lt;name&gt;.cook and cook-write/&lt;name&gt;-be.cook

The cooked file `schema cook` produces from an instance's wire, one per byte
order (docs/SPEC-TABLES.md §7.6). They are not dumps and not text: they are the
FILE, and a runtime's writer is byte-compared against them. Nothing is
hand-authored — `make conformance-generate` writes both from the wire golden the
instance line already names — and a byte that MOVES under an unchanged schema is
stop-the-line, exactly as a wire golden is.

The big-endian half needs no big-endian host on either side: the byte order is a
PARAMETER of the write, settled at cook time for the target build, so the tool
writes one on any machine and so must a runtime.

## block/&lt;name&gt;.dump

The canonical ROW DUMP of a block image (docs/SPEC-TABLES.md §19.2). The `block`
surface says only that an image OPENS, which a reader passes by checking the
prologue and stopping; this is the value-for-value read, so two implementations'
reads of the same bytes are byte-compared. It is produced from §8's DESCRIPTORS
and nothing else — no generated row struct — because that is the claim §19.2
makes for them.

```
projection <TableName> @0
  <field path> = <value>
array <field> <RowTypeName> @<byte offset> count=<n> stride=<n>
row <index> @<byte offset>
  <field path> = <value>
```

- The `projection` line comes first and carries the table's own inline fields,
  in descriptor order. The generated prologue — magic, build version, byte
  order — is not in the descriptors and is not dumped; it is the block forgery
  battery's subject instead.
- Then one `array` section per out-of-line array, in declaration order, with the
  offset, count and stride read OUT OF THE INSTANCE. Rows are numbered from 0
  within their array and their offsets are block-relative.
- Leaf lines are indented two spaces, and every convention is the cook node
  dump's: dotted paths through by-value nesting, `[n]` through inline array
  slots, `<path>#present` for a `?T`, quoted USED bytes with ` len=<n>` for a
  string or a `bytes`.
- **A FLOAT is its IEEE-754 BIT PATTERN**, `0x` and eight or sixteen hex digits.
  A block row is a byte-identical projection, so its bits are the fact; a
  decimal spelling would be a rounding rule two languages have to agree on for
  no gain. The cook's dump refuses a float rather than inventing a spelling, and
  this is the same refusal to invent, answered by not spelling it as a number.

Pinned from the reference leg with `make conformance-pin`, like the cook's.

## What moves a golden

The same rule the wire goldens have: a byte that moves under an UNCHANGED schema
is stop-the-line, never a quiet repin. `make conformance-generate` and `make
conformance-pin` rewrite the two halves deliberately, and the diff is the review.

Both forgery batteries carry a BUILD VERSION in their `<value>` column, so a
change to `tables/block` or to `tables/pointers` moves one. It moves LOUDLY: a
stale row stops naming a build the reader disagrees with, the forgery opens, and
the harness goes red. The cook battery's rows carry this build's own part
lengths and root sizeof beside it, and move for the same reason.
