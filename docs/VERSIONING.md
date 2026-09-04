# Versioning

If anything is going to be wrong with this library, it is going to be that we
did not think something through for versioning. Serialization libraries do not
die of slow code. They die in year three, when a save file written by a build
nobody has anymore meets a build the writer never saw, and something that was
promised on a page turns out not to be true in the bytes. This page is the
promise, stated so that it can be checked. Every fact on it is one of three
things: held by a test in the repository (a pinned *golden* the tests compare
against, or a *negative control* that breaks the guarded thing and confirms
the gate goes red), cited to the issue that will hold it, or wrong. There is
no fourth category. A pull request that changes any fact here changes this
page in the same pull request.

## The promise

The table wire is pre-release until 3.0.0. Before that release its files may
change in a minor version, announced first, and the promises below are made
at 3.0.0. The packet wire's promises hold today, with one gate still owed on
what proves them across releases, #463, named in its section below.

1. **From 3.0.0, data written by any build reads in any other build of the
   same major, in either direction, and never crashes.** A table wire from a
   newer build reads in an older one and the other way round. Every
   difference lands in the read report; none is fatal.
2. **Nothing is misdecoded.** A value is read as what it was written as, or
   it is skipped and counted. The edits the wire cannot report, the *silent
   class* (the bold cells in the table below), are refused at compile time by
   a committed baseline, so commit one before the first build whose data
   leaves the building; the one the repository cannot refuse today, a retired
   name reused, is #441.
3. **Identity is the name.** A field, an enum variant, a union arm and a
   table are identified on the table wire by the hash of their name. Add
   anywhere, remove, reorder. A rename that must keep its data declares the
   old name with `was`, and a `was` moves nothing anywhere. `was` is an
   attribute of a table's own fields today; tables (#396), variants and arms
   (#442), and the fields of a `type` that a table reaches (#478) get it
   before 3.0.0.
4. **Two ids and no third.** The *protocol id* versions the packet wire and
   is the only thing two peers compare before they talk. The *build version*
   versions the cooked and blocked forms and addresses every cooked asset. An
   edit inside a `table` body moves the build version and never the protocol
   id. An edit to a `type`, to an enum or union declaration a `type` REACHES,
   or to any `flags` declaration, moves both, a variant or arm reordered or
   renamed included, because that order is the wire. An enum or a union only
   tables reach moves the build version alone, which is what makes a content
   addition a table edit (SPEC.md §3.1). A `was` rename moves neither.
5. **Same-build forms match or refuse.** A cook or a block opens under
   exactly the build version that wrote it, by a header match, and otherwise
   refuses: `Open` returns null, and the tool's `uncook` names the mismatch.
   They are never read across a version.
6. **The kind set is closed within a major, and every future kind is
   skippable.** Kind `31` is the escape: its payload is a length and that
   many bytes, a kind added in a later major rides inside it, and a reader of
   this major skips it, counts it as unknown, and continues (#434).
7. **Ids are 64 bits, and the wire form is versioned.** Every id on the table
   wire is a 64-bit name hash, carried in an id table and named by a
   reference: once per FILE under the file form, and once per CONNECTION under
   the message form, which announces the unit's whole vocabulary and then
   carries none of it (#523). The first byte is the form version, so a reader
   that meets a later form
   refuses by name rather than reading it as damage (#435). A 2.x table file
   is not a 3.0.0 table file: a studio already shipping on 2.x keeps its 2.x
   binary, renders each file to text with it, and packs the text with 3.0.0.
   The text form is keyed by names and carries no version, which is what makes
   it the bridge.
8. **Peers connect on the protocol id and may differ in build version.**
   Cooked assets are local on both sides, so a build-version difference is
   not a connection question, and table data crossing a connection between
   builds is the ordinary case.
9. **A patch release never moves the protocol id or the build version's form,
   and never changes a byte under an unchanged id.** A minor may move either
   only together with the wire, announced first. A major is when your world
   breaks. The cross-release gate that proves this is #463.
10. **A build can keep what it cannot name, when the caller asks.** Unknown
    fields are dropped on rewrite by default, and the never-clobber rule below
    is the consequence. A caller that opts in at `Load`, with a bounded side
    buffer it declares and owns, keeps every unknown FIELD's bytes, and `Save`
    writes them back into the body they came from, so a player who rolls back
    to an older build, saves, and rolls forward again keeps the newer build's
    fields. **The promise is exactly as wide as the sharp edge below says.**
    It is a REGION round trip and not a builder one. It covers unknown FIELDS
    and not unknown enum variants, union arms, keyed-array slots, node
    records, node indices or the node table itself. And it covers the
    `unknown` class alone, so a load that reported `kind_mismatch`, `clamped`
    or `malformed` still loses what those counters name. It is opt-in, it
    allocates nothing, and every gap counts `retain_lost`. Specified in
    SPEC-TABLES.md §6.6, owed as #525.
11. **This page moves with the facts.** No mechanical gate reads this page
    today; #446 adds one, the evolution table's fixtures, and the rest is
    review.

## Two wires, two stories

Schema produces two wires from one language, and their versioning stories are
opposites on purpose.

The **packet wire**, what `type` declarations produce, is positional and
compact. Every bit is placed by the schema; nothing on the wire says what it
is. Two sides can read it only if they agree on the schema exactly, and the
protocol id is how they find out. There is no evolution on this wire, no
optional-field machinery, no tags, because a packet is read by the build that
was shipped with the one that wrote it. This is where the bytes-per-packet
budget is won.

The **table wire**, what `table` declarations produce, is self-describing and
tolerant. Every field carries its identity and its kind; a reader skips what
it does not know and defaults what it does not find. It costs bytes for that,
deliberately: this is the wire for data that outlives the build that wrote it,
whether on disk or crossing a connection between builds. A five-year-old file
reads in today's build, today's file reads in the five-year-old build, and
each side learns exactly what it could not understand.

**Anything that outlives a build rides tables.** A replay recorded as packet
streams dies with the first type edit; a replay recorded as tables reads for
years. A save is a table. A cooked asset's source is a table. A packet is for
the wire between two builds that ship together, and for nothing else.

## The packet wire and the protocol id

The protocol id is the low 64 bits of SHA-256 over the unit's *wire-shape
projection*, where a unit is a directory of `.schema` files compiled together.
The projection is a text rendering of every fact that determines packet bytes:
field order and names, kinds, widths, bounds, capacities, defaults, branch
structure, enum storage, flags bits, enum and flags variant names in
declaration order, union arm order and arm names. It excludes what the bytes
cannot see: comments, `///` doc comments among them; layout; declaration order
across records; `const` declarations, whose values are already resolved into
the bounds it carries; tags and native-type attributes, wherever they are
written; and every declaration no `type` reaches, `flags` excepted. A source-text hash would
produce spurious mismatches, and hashing the compiler's internal structures
could produce a spurious *match*, which is the dangerous direction. The
projection is the thing in between. `schema id` prints the id and
`schema projection` prints the text it is taken over.

Two version lines ride the top of that text, and bumping either moves every
protocol id in existence:

- **`ProjectionVersion`**, the rendering's own version, for the day the
  projection must describe something it previously did not.
- **`WireLaw`**, the codec law's version, for what the rendering cannot see.
  Any compiler change that can alter, for the same schema and the same
  values, the encoded bytes, the inputs accepted, the reads rejected, the
  defaults materialized, or a numeric conversion bumps it. The invariant:
  **no generated byte and no read decision may change for the same schema and
  input without the protocol id changing.** Generated files carry the id
  alone, so a bump costs no churn in a consumer's tree. What is still owed is
  the gate that proves the invariant across releases — the corpus generated
  by the previous release and the new one, byte-compared under an equal id
  (#463).

**Variant order is the wire, and the names carry it.** An enum value rides as
its declaration ordinal, a flags variant as its bit position, a union arm as
its tag, so a reorder changes what every stored ordinal, every set bit and
every tag means while the shape stays put. Each declaration's variant or arm
names therefore enter the projection in declaration order, which is the only
way a reorder can be seen. A union's payload types carry its arm order only
while the arms differ in type: two arms of the SAME payload type reorder with
every projected type unmoved, and the names are the whole of the difference.
The consequence is that a *rename* moves the id too — order is spelled in
names and nothing else — and under the redeploy rule below that is free.

**Same or refuse.** Two peers holding the same protocol id interoperate; two
holding different ids refuse each other instead of misreading each other.
Whether the id travels on the wire, in a connect token, in a `const` field,
or out of band, is the application's choice. There is no negotiation and no
fallback, and everything downstream of the connection is simpler for it.

**What the same-or-refuse rule costs, and who pays.** The projection is the
CLOSURE over the unit's `type` declarations, plus every `flags` declaration
(SPEC.md §3.1). So any `type` edit moves the id, an unused helper type moves
it, since every `type` is a root because nothing in the language says which
types go on a wire, and so does any edit to an `enum` or `union` a `type`
reaches, or to any `flags` declaration at all. **What does NOT move it is a
declaration only tables reach.** Adding an item kind, a quest or an
achievement to a content enum no packet carries is a table edit, and it costs
no redeploy. That is the one concession, and it is aimed at the game that
takes the README at its word and keeps packets, saves, config and assets in
one unit. The price of every id move that remains is a coordinated redeploy of
both ends. This project's own games redeploy both sides of every connection
together, always. Cross-platform studios with certification lag ship both
store builds dark behind a gate and flip the server when both have cleared. A
studio that cannot force-update its clients is outside this model, and should
know it before choosing the packet wire for anything long-lived.

**`flags` is held in the projection deliberately, and it is the exception to
the closure.** A mask is the table wire's one positional vocabulary, so an
insert, a reorder or a rename in place is the silent class's second member and
no read report can see it. The protocol id is the only frame that refuses two
peers holding different bit assignments while they exchange table data over
one connection. Content never rides in a flags declaration, because
sixty-four bits is the ceiling and the law is append at the end, so holding it
in costs nothing the closure was meant to buy.

**Table bodies never enter the projection.** No edit inside a `table` body
can move the protocol id. That independence is held by test, and it is the
reason the two wires can have opposite stories without a third id to
reconcile them.

## The table wire and identity by name

On the table wire a field is `id reference, kind, payload`; a node, one
table's worth of data, is a record of `type id reference, length, body` inside
the node table. A REFERENCE names a slot of the id table the wire carries once
(the layout section below), and the **id** in that slot is the hash of the
declared name. That one decision produces the whole evolution story:

- **Add a field anywhere.** An old reader meets an id it does not know, skips
  the payload by its kind, counts one `unknown`, and continues. A new reader
  meets a file without the field and reads the declared default.
- **Remove a field.** The reverse: the old file's field is unknown to the new
  reader, skipped and counted. Nothing else moves.
- **Reorder freely.** Position carries nothing.
- **Rename with `was`.** `speed float32 | was = "velocity"` keeps the wire id
  `hash("velocity")` forever; the source name is for people. **`was` names the
  field's first wire name, forever.** A second rename keeps the same `was`; it
  is never re-pointed at an intermediate name, and a committed baseline
  refuses one that is — the intermediate spelling hashes an id no byte was
  ever written under. `was` keeps the wire id and not the TEXT key, which is
  the field's own name, so the edit that adds a `was` also draws a one-line
  hint to pair `json = "velocity"` when the field has no key of its own.
- **A rename without `was` is the edit to fear, and the compiler cannot see
  it.** The compiler retains nothing between builds; to it a bare rename is a
  removal and an addition, both of which pass. Every value stored under the
  old name reads as the default from then on, counted `unknown`, and nothing
  reports that the two names were one field. A committed baseline is the one
  place the shape of a rename is visible — a removal and an addition in one
  table in one edit — and it warns on that pair, naming both and the `was`
  and `json =` that declare it. Two independent edits in one commit are
  legitimate, so it is a warning and never a refusal.
- **Collisions are refused at compile time.** Two names in one vocabulary
  whose hashes coincide, or a `was` colliding with a live name, are a compile
  error naming both, and the refusal fires when the *new* name is added,
  before it has shipped, so a collision costs a naming wart and never a
  stored value.

**Every vocabulary, the same rule.** Enum variants and union arms are
identified by name hash exactly as fields are; a table's name is the node's
type id. Ids are 64 bits, `fnv1a64(name)`, in every vocabulary, one rule with
no fold and no rebound. A 64-bit id gives a million-variant vocabulary an expected
0.00000003 collisions over its life. The wire does not pay eight bytes per
field for it; the layout is in the wire form section below.

**Flags are the one positional vocabulary.** A mask rides raw, so a variant's
identity is its bit. The rule for flags is *append at the end*; insert,
reorder and rename-in-place are the silent class and the baseline refuses
them. Removing a variant frees no bit: retire the name, keep the position.

**Defaults are part of the wire contract.** A field equal to its declared
default is elided from the wire, and an absent field reads as the default. So
a changed default changes what every stored file *means* without touching a
byte, and `was` does not cover it: `was` preserves an identity, not a value.
Change a default the way you would change data, by rewriting the files you
hold, or add a new field and leave the old one alone. Tuning values that move
weekly belong in configuration the studio holds, where rewriting is a
pipeline step; player saves hold state, which has no weekly default.

## The three frames

Three mechanisms judge an edit, and each sees what the others cannot:

- **The read report** says what a *reader* can tell happened: six counters,
  `unknown`, `kind_mismatch`, `widened`, `clamped`, `duplicate` and the
  `malformed` flag, filled on every load and never fatal on data from another
  build. A
  caller that opts into retain-unknown reads two more on the same struct
  (SPEC-TABLES.md §6.6), and they report on retention rather than on the read.
- **The baseline** says what the *compiler* refuses to let you do to data
  already written: the edits the wire cannot report, caught before they ship.
- **The build version** says whether a cooked or blocked file this build
  wrote is still this build's.

One table reconciles them. It is the single statement of what an edit does;
SPEC-TABLES.md §4, §18.2 and §20.4 will derive from it once it has a fixture
per row, each edit run through all three frames with the verdicts pinned
(#446), so that it can go red. Every cell was run against the tool; the rows
that name an issue describe the committed rule and say what the repository
does today.

| the edit | the read report | the baseline | the build version |
|---|---|---|---|
| a field added, removed or reordered | `unknown` on the side that lacks it | passes; a removal AND an addition in one table in one edit is the bare-rename row below | moves (the record's layout moved) |
| a field renamed under `was` | nothing | passes; the edit that adds the `was` hints the `json =` pairing | **nothing**: keyed by wire id, not source name |
| a field renamed a second time, the new `was` naming the INTERMEDIATE spelling instead of the first | `unknown` on every old file; the new id was never written to | **refuses** | moves |
| a field renamed bare | `unknown` on every old file; the new field reads its default | warns: a removal and an addition in one table in one edit is the shape of a rename | moves |
| a field of a `type` that a table reaches, renamed | `unknown` on every old file; `was` is refused there today (#478) | passes, in silence | moves, and so does the protocol id |
| a scalar's default changed | **silent**: the same bytes mean something else | **refuses** | moves (a meaning fact) |
| a string, bytes or flags default changed | silent | refuses once #396 lands; not declarable today | moves |
| a bound raised or lowered, a capacity or array bound grown | `clamped` where a stored value exceeds it | passes; warns on a shrink | moves |
| a range tightened | `clamped` | warns | moves |
| a field's kind changed (`T`→`*T`, `string`→`bytes`, `int64`→`int32`) | `kind_mismatch`; the value reads as the default | **refuses** | moves |
| a field's kind WIDENED (`int32`→`int64`, `uint8`→`uint32`, `bits(8)`→`bits(9)`, `float32`→`float64`) | `widened`, and the value decodes exactly (SPEC-TABLES.md §4) | **refuses**: the tolerance runs one way, and the OLD build reading the new file still kind-mismatches | moves |
| `T`→`?T`, `?T`→`T` | nothing: one framing. An old file's elided default reads as *absent* under `?T` | passes | moves (the presence flag is storage) |
| an enum variant added or removed | `unknown` where a stored name is gone | passes; warns on a removal | moves, and the protocol id with it where a `type` reaches the enum |
| an enum variant reordered | nothing | passes | moves, and the protocol id with it where a `type` reaches the enum |
| an enum variant renamed | the old name reads `None`, counted | warns | moves, and the protocol id with it where a `type` reaches the enum: order is spelled in names |
| a `fixed` field's `F` moved | **silent**: the raw integer reads at the new scale | **refuses** | moves |
| a field's referent replaced by one that cannot stand in (a nested table swapped for a same-shaped twin) | **silent** | **refuses** | moves |
| a field or a union arm changed between an enum and its raw integer | `kind_mismatch`: an enum has a kind of its own | **refuses** | moves |
| a union arm's declared type changed | `kind_mismatch` where the kind moved, `malformed` where the length no longer frames it | **refuses** | moves |
| a flags variant inserted or removed | silent | refuses | moves, and so does the protocol id |
| a flags variant reordered or renamed in place | **silent** | **refuses** | moves: the cook projection digests each variant's bit position, and the protocol id moves too |
| a union arm reordered or renamed | `unknown` for an arm this reader lacks; a reorder is silent and safe | warns on a vanished name | moves, and the protocol id with it where a `type` reaches the union: the arm names are what a same-typed reorder moves |
| a keyed array made positional | `kind_mismatch` | refuses; and in a TABLE body the positional spelling is refused by name (SPEC-TABLES.md §2.4, §11), which the checker does not do yet (#540) | moves |
| a keyed array's key enum swapped for another | `unknown`, one per slot; the kind stays | refuses | moves |
| a map's KEY kind changed, or its KEY bound tightened (SPEC-TABLES.md §2.8) | a changed kind is one `kind_mismatch` for the map, which reads empty. A tightened bound drops the entries that no longer fit and counts `clamped`, one per entry | **refuses** a changed kind, warns on a tightened bound | moves |
| an array changed between `[]T` and `[..N]T` (SPEC-TABLES.md §2.9) | nothing where the count fits the new bound, `clamped` past it: the two are the same bytes | warns on the direction that ADDS a bound, as any capacity shrunk; passes on the one that removes it | moves: the storage is a reference and a count on one side and the maximum inline on the other |
| an unbounded array's ELEMENT retyped, or moved to or from `[]*T` (SPEC-TABLES.md §2.9) | `kind_mismatch`, the array reading empty | **refuses**, as any element kind changed | moves |
| a guard added or removed | nothing | passes | nothing |
| a `json =` key changed | nothing on the wire | passes | nothing |
| a `///` doc comment or a tag added, changed or removed (SPEC.md §4.1, §4.2) | nothing: neither is a fact a codec reads | passes; neither enters a baseline row | **nothing**, and the protocol id does not move either, so annotating a shipped schema is a free edit |
| a table renamed | nothing when it is held by value (a declaration name is not on the wire); every pointer to it reads null and counts `unknown` when it is a pointer target | warns | moves, until table `was` lands (#396); then a `was` rename moves nothing |
| a retired name re-added with a new meaning | **silent** | **passes today**; the ledger is #441 | moves |
| a language added to the build | nothing | nothing | nothing |

Enum, flags and union declarations are shared by both wires, which is why some
rows move the protocol id as well. An enum or a union does so only where a
`type` reaches it, and a `flags` declaration always does: see the packet wire
section.

## The read report

A load fills the report and returns; nothing on the table wire is fatal on
data from another build. `unknown` is the ordinary sound of evolution and
fires on every cross-version load by design. `duplicate` counts a REPEATED
KEY, and it has one source on each side: a repeated JSON key in the text
form, and a MAP's repeated key on the wire, which is the one wire event that
raises it (SPEC-TABLES.md §2.8, §4). `widened` is the one counter that names
NO LOSS: an integer kind read into a wider one of the same signedness, or an
`f32` into an `f64`, decodes exactly, and the count says the bytes were not
the shape this reader declares rather than that anything was dropped. It fires
on the NEW build reading the OLD file, and never the other way, which is the
whole shape of the pattern below. `kind_mismatch`, `clamped` and `malformed` are damage or a decision,
and a game that alarms on those three and logs the others has the severity
split it needs. A tool that wants to know *which* field was clamped re-walks
the file with the descriptors, the per-field facts every table's generated
header carries; per-event attribution on the generated read path is additive
and safe to add after 3.0.0.

**Two more counters ride on the same struct and stay zero unless a caller asks
for them**: `retained` and `retain_lost`, the retain-unknown pair
(SPEC-TABLES.md §6.6). They report on RETENTION and not on the read. A
retained field still counts `unknown`, because `unknown` says what a reader
could not name and that stays true, so no existing counter changes meaning and
no existing caller sees a number move.

**The never-clobber rule, and the opt-in that lifts it.** Unknown fields are
dropped on rewrite BY DEFAULT: everything in schema has a schema, and a tool
built from an older schema that rewrites a newer file drops what it does not
know and counts it. That default has one consequence every studio with a mixed
fleet must write into its own code before the first staged rollout: **a save
cycle or a rewriting tool never overwrites a file whose read report is not
silent.** The `unknown` counter fires on the exact load that precedes the
destructive rewrite. Write beside the file, or refuse the rewrite. Nothing in
the runtime enforces this: no generated `Save` refuses when the last load's
report was not silent, so the game keeps the report beside the instance and
checks it before it writes.

**RETAIN-UNKNOWN is the opt-in that answers the case the rule exists for, and
it strikes ONE counter out of the never-clobber condition** (SPEC-TABLES.md
§6.6, owed as #525).
A caller that hands `LoadRetain` a bounded side buffer it declares and owns
keeps every unknown FIELD's bytes, and `SaveRetain` writes them back into the
body they came from. Retention covers the `unknown` class and nothing else, so
the rule reads: **a save cycle or a rewriting tool never overwrites a file
whose read report is not silent, UNLESS retention was on and `retain_lost`,
`kind_mismatch`, `clamped` and `malformed` are all zero after the save.** That
is the original condition with `unknown` struck out and nothing else moved,
and it is precisely what retention buys. The other three still name real loss:
a `kind_mismatch` field was skipped and read its declared default, a `clamped`
value was changed on the way in, and a `malformed` load kept a partial decode.
**`widened` is not in the condition and never was**, on either side of the
opt-in: a widened field decoded exactly, so a rewrite loses nothing, and the
counter is there to say the file's bytes will change shape rather than that
its values did (SPEC-TABLES.md §4).
The condition is read after the SAVE and not only after the load, because a
retained record can also fail to be placed. The default is unchanged, every
existing caller keeps the behavior it has, and a caller that treats
`retain_lost` as fatal and refuses its own rewrite is back at the rule above
with no code path of its own.

## The baseline

`tables.baseline`, in the unit's directory, is a committed projection of the
table closure, everything the unit's tables reach by value: every wire fact
of every field, evaluated, keyed by wire id. `schema check` (and so
`generate`) diffs the current schema against it and **refuses any edit that
would make data already written unreadable or quietly change what it means**:
the silent class above, plus kind changes, keyed-array respellings and
referent drops, and a second `was` aimed at a field's intermediate spelling.
It warns on shrinks, tightened ranges, removals, declaration renames and the
removal-and-addition pair a bare rename leaves, hints the `json =` pairing at
the edit that adds a `was`, and passes in silence on everything the wire
reports. `schema tables-baseline` prints the projection, and with `--update`
rewrites the file.

**Commit one.** The baseline is opt-in, no file, no check, and every
"refuses" in the table above holds only for a unit that has one. A unit whose
data leaves the building commits its baseline the day the first such build
ships, because **the first baseline covers only what comes after it**: data
written before that day was written against a shape nobody recorded. `schema
check` says so: a unit that declares a table and holds no baseline draws one
line on stderr naming the command that commits one, with the exit code
untouched, and committing one silences it.

**Moving it is explicit and reasoned.** A refused edit that is nonetheless
intended is accepted with `--update --reason "..."`, which rewrites the file
and appends a dated entry to its history section naming every edit, old value
to new. `--update` without a reason is refused. The history survives every
later `--update` verbatim. Today it records only the edits that were refused
or warned: a plain removal writes "no compatibility-affecting edits," so the
history is not yet a record of retired names; that is #441.

**Merging two branches of one schema.** Merge the `.schema` text; keep either
parent's baseline; run the check, whose refusals name every semantic
divergence the textual merge introduced; then `--update --reason "merge"` and
read the entry. The history sections are unioned by hand.

**Its own version.** The baseline's rendering carries a version on its first
line. Any new judged token bumps it, which makes every committed baseline
stale at once, deliberately and visibly; `--update` repairs a file it cannot
read, salvaging the history. The window during which that repair runs is the
one window the check is off, marked in the history as "could not be diffed";
review the schema diff of that commit by hand.

**Before 3.0.0's evolution claims:** the retired-names ledger (#441).

## The build version

**Any divergence in the bytes a cook would produce is a new build version.**
The id is the low 64 bits of SHA-256 over the unit's *cook projection*, which
digests three groups of facts: the protocol id (the type wire's shape), the
layout of every record in the table closure (sizes, offsets, kinds, wire ids,
array classes and bounds, strides), and the meaning a wire load puts in those
slots (defaults, effective ranges, enum and union vocabularies, and each
flags variant's bit position). It is compiler-settled:
tooling cooks before any game binary exists, so the id must be knowable from
the schema alone. `schema build-version --facts` prints it with the facts it
was taken over.

**What moves it:** anything that moves the protocol id; a record added,
removed or renamed bare; any offset, size, alignment, kind or wire-id move;
any change to which declaration a field names; a declared maximum raised or
lowered; a default, range or vocabulary edit; a variant inserted into a keyed
enum (a keyed enum is a layout fact: its `Max` sizes every array keyed by it,
so a content edit re-cooks every asset of the unit); a `ProjectionVersion`,
`WireLaw` or cook-form bump.

**What does not:** comments, layout, declaration order across records; a
`was` rename, because a rename must not invalidate every cooked file in
existence and the layout is keyed by wire id to make that true; a guard; a
`json` key; anything only the baseline judges; adding a target language.
**Byte order is not an input in effect:** the projection carries a byte-order
line that is `little` for every target, so one id serves every target of a
game; #432 makes the cache key say so.

**Its two jobs.** It *addresses* a cooked artifact, the studio's cook cache
is indexed by it, and it *refuses* one: `Open` checks it out of the header.
There is no second version id anywhere in the cooked or blocked forms; a
form's identity is its magic.

**The release policy.** The build version's *form*, its projection rendering
and the cook form it names, can move only by a deliberate version bump in
the compiler, and such a bump invalidates every cooked asset in every cache
at once. So: no patch release moves the build version's form; a minor release
may bump it only together with a wire change, announced first in the release
notes, exactly as the protocol id's rule reads; a major may. A schema edit
moving the id is the user's own act and is priced by the cook cost model
below. The repository today has a form version constant and no gate on which
release may bump it; the 3.0.0 release adds the gate.

## The same-build forms: cook and block

A **cook** is a table's data laid out as the target build's in-memory
records, in the target's byte order, so that loading is a header match and a
pointer. The fix-up for byte order happens where the target is known,
offline, once, on the writing side, and never on the reading side, which is
what makes `Open` a match and a point rather than a pass over the region. **A
cooked artifact is content-addressed by a triple: the hash of its source wire
file, the unit's build version, and the target byte order** (#432). Tooling
produces a cook under that triple; the cache is indexed by it; the game does a
lookup. `Open` on a hit refuses only a corrupt, truncated or wrong-version
file, by returning null, and the caller falls back to a wire load, the path
that carries every version. **Meter that fallback rate.** A cache that evicted
last week's build version turns a rollback into a fleet-wide slow load with
no error anywhere, nothing in the runtime meters it, and the metric is the
only witness.

**The cook cost model: a new build version is a new cook.** Nothing finer
than the unit is keyed, because a finer key buys a smaller re-cook and pays
for it with a second id. A weekly-tuned live game shipping full cooks to
players pays real bandwidth for that; the answers are binary diffing at the
patcher, which is its layer, and tunables in studio-held configuration rather
than in cooked assets.

**The dev loop runs on the wire.** Cook and block are ship-load accelerators;
if load time does not demand one, the game loads the table from the wire,
which hot-reloads across any edit and reports what changed. No "ignore build
version in development" flag exists, because the path such a flag would open
already exists and is tolerant.

A **block** is a fixed table's rows at a compiler-computed stride, for data
one language writes and another reads at frame rate. It is same-build in the
strictest sense: a prologue of magic, build version, byte order and the row
facts, and `BlockOpen` either matches every one or refuses. Its evolution
story is that it has none: any layout edit moves the build version, any ABI
drift is a build error on both generated sides before it is anything else.

## The kind space and the wire form

Every field on the table wire carries a kind byte from a closed set: bool,
the integers by width and sign, the floats, string, wstring, table, array,
union, keyed array, pointer index, the 128-bit integers, the fixed-point
widths, a
kind of its own for enums so that an enum and its raw integer can never be
confused on the wire, the escape kind, and the payload-free kind a union arm
takes. **A kind is spent only to close a silent
edit**; blocks spend none. One number past the set is RESERVED BY NAME and
never written, `34` for `float16` (SPEC.md §4.10), so the construct a later
major adds has its number written down before it is built.

**A reader that does not know a kind cannot skip it.** That is the nature of
a closed set, and it is why a new kind is a wire change. Two rules make that
survivable across a major. First, **kind `31` is reserved, its payload `L`
then `L` bytes**, opaque; a kind added in a later major is introduced as that
framing carrying its own inner encoding, so a reader of this major skips it,
counts it `unknown`, and continues (#434). Second, **the file's first byte is
the wire form's version**, so a reader can say "newer form" rather than
"malformed" when it meets one (#435). Neither is an
envelope: no identity, no magic and no content hash ride on the table wire,
and a file that needs to say what it is puts a field on its root table,
`format uint32 = 3`, which an older reader still reads, a foreign file
defaults, and that default is the provenance signal.

**The layout.** After the form byte comes the body, every id a reference and
every width variable: a field is `ref, kind, payload`, and the field list ends
with a zero ref. References are 1-based, unsigned LEB128, canonical (a
non-minimal encoding is malformed); lengths, counts, node indexes and node
counts are the same varint, 64-bit in capability and one byte for the small
values they nearly always are. An enum value, a union's arm id, a keyed
array's slot keys and a node's type id are all references. A union arm's
header is a field header, so an arm carries its own kind byte and a retyped
arm is reported like a retyped field. The file ends with the id table: the
distinct 64-bit ids the body used, in first-use order, then the count as a
fixed u64, so that a one-pass writer never patches and a reader, which holds
the whole buffer anyway, reads the count from the end, resolves the table
once against its own descriptors, and dispatches every field through an array
index. There is no mode: no implicit table, no width byte, no magic, no build
version in the header. The measurements that chose this layout are on #435,
and SPEC-TABLES.md §3 is the encoding.

## The message form, and an id table announced once a connection

A table file carries its own id table, and that is the right trade for the
shape the wire was designed against: a config bin or a save naming forty
distinct ids across ten thousand fields pays eight bytes an id once and spends
one byte a field header for the rest of the file. A four-field message between
a game and a backend is the opposite shape, and it pays the trade in full with
nothing to amortize it against. Measured on #523, three ordinary backend
messages ran 106, 273 and 104 bytes against proto3's 49, 189 and 40, and the
id table alone was 48 of the first and 56 of the third.

**The message form is form byte `2`, and it moves the id table off the message
and onto the connection.** A form-`2` wire is the form byte and the root body,
with no trailer. The three messages become 58, 225 and 48, which turns a loss
of 2.2x, 1.4x and 2.6x against proto3 into one of about 1.2x, and a message
whose fields sit at their declared defaults goes from 43 bytes to 27 against
proto3's 40, which is an outright win. SPEC-TABLES.md §3.3 is the encoding.

**The table is the UNIT's whole vocabulary, announced once and never again.** A
peer sends an ID TABLE MESSAGE before its first form-`2` message: an ordinary
form-`1` file whose one required field is the build version under a reserved id,
and whose trailer is every id the peer's unit closure can put on this wire, in a
compiler-settled order. Each direction has its own. There is no
re-announcement and no state machine: a second announcement on a connection is
refused by name, and a refused announcement sets no table at all, so every
message on that connection is then refused for want of one. A build change is a
new binary, a new process and a new connection.

**Three properties follow from announcing the unit rather than the message.**
The table is a pure function of the build version, so two peers at one build
derive one table and the key is literally a key. The whole announcement is
therefore a compile-time constant of the unit, and the writer's slot numbers are
compile-time constants baked into the generated field headers, so there is no
runtime lookup on the send path. And the receiver resolves once, at the
announcement, and dispatches every message after it through one array index.
The price is that a unit pays for its whole vocabulary rather than the part a
connection uses, and for a tail that carries the node-table id, the blob type
ids and every table's name id whether the unit has a pointer or not, so that a
slot number never drifts under an edit that has nothing to do with it: a unit of
500 ids announces about 4 KB once.

**The build version KEYS the table and does not gate the connection.** Promise
8 stands exactly as written: peers connect on the protocol id and may differ in
build version, and a receiver never refuses a message because the announced
build version is not its own. What the key buys is that a build's table is
derivable from that build alone, that a refusal can name the build version it
could not resolve, and that a vocabulary is traceable to one compilation of one
unit in a log.

**A peer with no table for the connection refuses the message by name.** It is
the same refusal the form byte already carries: nothing is decoded, no counter
moves, and `malformed` does not fire. The recovery is the sender's, and both
shapes of it are already in the design. The sender opens a new connection and
announces first, through whatever its own application declares for the purpose.
Or the sender writes the file form, which carries its own table and needs no
connection.

**A connection here is a transport connection.** One TCP or WebSocket
connection, or one reliable ordered stream or channel of QUIC or a
reliable-UDP transport, counted per channel. A restart is a new connection with
empty tables, and a receiver caches nothing across connections. A stateless
request-response transport is out of scope for this form, because an
announcement would ride every request and cost more than the trailer it
replaced. The file form rides there.

**Nothing else moves.** No kind is spent, no payload changes, no skip rule
changes. The protocol id does not move, the build version does not move, the
baseline does not move, the text form does not move, the cook and the block do
not move, and no row of the evolution table above moves, because a reader sees
every edit exactly as it saw it before. The read report keeps its counters and
their meanings. The packet wire is untouched and out of scope, for the reason
this form exists at all: the packet wire is same-or-refuse on the protocol id,
so both peers must ship together, and a deployed game client and a backend do
not.

**Retention crosses the forms in one direction, and refuses in the other.** A
message body loads with retention exactly as a file does, since it is framed
the same way under the same skip rules. A `SaveRetain` writing form `2` refuses
by name and returns `-1`, because a form-`2` writer names ids through slots of a
compiler-settled vocabulary and a retained id is by definition one that
vocabulary does not contain. It is a misuse refusal on §6.6's own precedent and
never a silent drop. A caller that must carry unknowns across a rewrite writes
the file form. A relay that must forward them forwards the sending peer's
announcement and its message bytes verbatim, which loses nothing and costs
nothing.

Two sharp edges come with it, and both are the same fact seen twice.

- **A message is not readable on its own.** A capture without the connection's
  announcement cannot be decoded, and a form-`2` wire stored as a file is
  refused by name rather than read. proto3 makes the same trade, since a
  `.proto` is required out of band, and the build version is what makes this
  one nameable: a receiver says which build's table it lacks. `schema pack`
  and `schema unpack` are file-form tools and stay that way.
- **The form needs an ordered, reliable channel, and the announcement has to
  arrive first.** On an unordered or lossy transport, or a stateless one, the
  answer is the file form, which is self-contained, or the packet wire, which
  is positional and carries no identity at all.

## The text form

The text form is JSON keyed by *names*. It carries no version, no schema
reference and no envelope, and reads with the wire's own tolerance: absent
keys keep their defaults, unknown keys are skipped and counted, values outside
a range clamp and count, so a text and a wire loaded from the same data land
the same instance. One consequence to plan for: **the text's identity is the
name, and `was` does not carry it.** A field renamed under `was` is safe on
the wire and breaks every hand-edited configuration file and editor that
spelled the old key, unless the rename is paired with `| json = "old"`, which
keeps the text key while the wire keeps its id.

## The compiler's own version

The compiler follows [semantic versioning](https://semver.org/), and the thing
being versioned needs saying precisely, because for a code generator two
different things can change: the compiler the user runs, and the wires their
schemas produce. Each has its own version, and they are not the same number.

**Major**: the user's world breaks. Existing `.schema` files stop compiling
or change meaning, the generated API breaks, or the wire's closed set gains a
kind. Expect a migration note. Nothing less than this earns a major.

**Minor**: additive features. New language features, new attributes, new
backends, better diagnostics, generated code that is faster or cleaner. New
syntax you have not used cannot affect you. A minor release may also carry a
wire change, *with* its protocol id bump, or a build-version form bump: the
bytes and the id move together, so deployed peers refuse newly built ones
rather than misread them, and the release notes state the bump first. Before
3.0.0 the table wire's goldens may move in a minor under this rule; that is
what pre-release means here.

**Patch**: bug fixes and documentation, and one promise, kept verbatim: **"no
PATCH release will break protocol id"**, nor the build version's form, nor a
byte under an unchanged id. Take any schema, rebuild it with a newer patch
release of the same minor, and its protocol id and its build version are the
same ids and its bytes are the same bytes. Patch releases are always safe to
take.

The pinned goldens in CI enforce this within a release: the conformance
corpus's golden wire bytes pin every construct's encoding, and `schema id`
and `schema build-version` over the corpus are pinned as exact values, so a
compiler change that moves any of them stops the build until it is argued
for and the release that carries it takes the number these rules assign. The
gate that proves it across releases, the corpus generated by the previous
release and the new one and compared, is #463.

**Recorded wire-affecting amendments.** The rules above are policy; this
records the instances, so the history of "a release moved bits" lives where
the compatibility rules do.

- **2026-08-15, fixed-point rounding unified: half away from zero.** The
  generated fixed-point narrowing changed from the bare arithmetic shift
  (ties toward +infinity) to the one fixed-point rounding rule, ties away
  from zero. This moved the bytes generated code produced only on exact ties
  of negative raw values in that narrowing, and the protocol id did not move:
  the projection carried no codec law line at the time. It is the worked
  example of why that line exists — under the rule now in force it bumps
  `WireLaw` and moves every id, in a minor, announced first.

Both sides of every connection redeploy together, so an amendment that moves
bytes is priced by that rule and not by the fiction that deployed halves must
interoperate across the change.

**What is not covered.** The protocol id is not a version number; it is a
hash of your schema's wire shape and changes when your schema's wire changes,
independently of the compiler's version. Generated files do not record the
compiler version, deliberately: stamping it would put a diff in every
generated file in every downstream repository on every release, saying
nothing about whether the wire changed; generated code carries the protocol
id and the build version instead, which are the things that govern
compatibility.

**The Go API under `compiler/` and `ir/` is covered; `internal/` is not.**
`github.com/mas-bandwidth/schema/v2/compiler` loads and checks units and
generates through registered generators; `github.com/mas-bandwidth/schema/v2/ir`
is the checked unit those generators read. Breaking their exported surface is
a major, adding to it is a minor. Everything under `internal/`, the scanner,
parser, checker and the per-language emitters, carries no promise and may
change in any release. In `ir`, the table-wire kind vocabulary (the
`TableKind*` constants and `TableScalarKind` / `TableFieldKind` /
`TableElemKind`) is wire law and therefore frozen within a major.

**The SPEC is versioned with the compiler.** [SPEC.md](SPEC.md) and
[SPEC-TABLES.md](SPEC-TABLES.md) are normative; where the compiler and a spec
disagree, one of them is a bug, and the goldens say which.

**The serialize runtimes version separately.** Generated code targets a small
runtime per language for the packet wire, and those are their own projects
with their own version numbers:

| runtime | |
|---|---|
| [serialize](https://github.com/mas-bandwidth/serialize) | C++ |
| [serialize.c](https://github.com/mas-bandwidth/serialize.c) | C |
| [serialize.cs](https://github.com/mas-bandwidth/serialize.cs) | C# |
| [serialize.go](https://github.com/mas-bandwidth/serialize.go) | Go |
| [serialize.js](https://github.com/mas-bandwidth/serialize.js) | JavaScript |
| [serialize.rs](https://github.com/mas-bandwidth/serialize.rs) | Rust |

They share one wire standard and are checked against each other. CI pins
each to a released tag, never to `main`, and newer runtimes keep working with
older generated code. Dart, Java and Elixir generate self-contained output
and need only their pinned SDKs.

**Release process.** Releases are tagged `vMAJOR.MINOR.PATCH` on `main`. CI
must be green on the tagged commit, including the full certification run
across every language. The build stamps `git describe` into the binary, so
`schema version` on a release build reports the exact tag and on a
development build the tag plus commits-since plus hash; quote that line in
bug reports, because a refusal message cites the spec that binary was built
against and a stale binary will contradict the current page.

## Patterns

The features above answer most of what a live game does to a schema over
years, and several of the answers are patterns rather than syntax.

- **Widen a field.** A NEW build reads an OLD file's narrower integer of the
  same signedness, and its `float32` as a `float64`, exactly, counting
  `widened` (SPEC-TABLES.md §4). What is free besides: growing an array bound,
  a string or bytes capacity, loosening a range. **What the widening does not
  do is run backwards**, and that is the whole of the pattern: the OLD build
  meets `int64` where it declares `int32`, which is a narrowing, and it reads
  the default and counts `kind_mismatch`. So `int32` to `int64` is a one-way
  edit, safe the moment every reader is on the new build and lossy for every
  reader that is not. Flip the whole fleet first and the edit is one line.
  Where a reader can still be old, the pattern is unchanged: `gold2 ?int64`
  beside `gold`, old saves read `gold2` absent, the load shim copies `gold`
  across, new saves write it present, retire `gold` after the horizon. Nothing
  widens across the signed and unsigned ladders, out of the fixed-point kinds,
  or from a `float64` back to a `float32`.
- **Split, merge or move a field.** Identity is (table, name); nothing
  carries a value across that boundary. Keep the old fields declared through
  the migration horizon, shim on load, retire after. Five years of shims is
  the migration system; keep them in one file.
- **Change what a field means.** A meaning change is a new identity: rename
  *without* `was`, or add a field. The deliberate orphaning is correct, and
  it is the only judge of meaning there is: a field whose declaration is
  unchanged while its unit changes from meters to centimeters is invisible to
  every frame.
- **Retire a field.** Removal is free and reported; there is no deprecation
  marker because removal is what deprecation exists to fake elsewhere. Do not
  re-add the name with a new meaning; until the ledger (#441) refuses it,
  nothing records that the name is haunted, so keep your own list.
- **Ship a schema change to a mixed fleet.** A table-body edit never forces a
  redeploy, and neither does an edit to an enum or a union no `type` reaches,
  which is where content lives. A type or flags edit always does, and so does
  an enum or union edit a `type` reaches. Write the never-clobber rule before
  the first staged rollout, or turn retain-unknown on and check `retain_lost`.
  Roll back freely: the old build reads the new files and counts what it
  cannot use, and under retention it writes the unknown fields back out
  unharmed.
- **Debug an old file.** `unpack --tolerate` renders a wire file of any
  version to text and reports what it did not understand; without
  `--tolerate` it refuses a file whose report is not silent, and a file
  written before a flags variant was removed is refused either way, because
  a bit with no name has no text spelling. `uncook` renders a cook written by
  this build's version and refuses any other. The baseline history says what
  changed and why, by date. The `format` field on the root table says which
  schema wrote a restored backup.

## Sharp edges

A versioning page that hides its edges is how the failure this page exists to
prevent happens. These are the places the design chose a cost, or has one
still open.

- **Unknown fields do not survive a rewrite unless the caller asks, and even
  then not all of them.** The default is a drop, with the never-clobber rule
  as the required consequence and no runtime enforcement. Retain-unknown
  (SPEC-TABLES.md §6.6, #525) is opt-in, is bounded by a buffer the caller
  sizes, and is a REGION round trip only: `LoadRetain` loads into a region and
  `SaveRetain` saves from that region, and the BUILDER path carries no
  retention, because a builder has no node directory to anchor a record on and
  re-derives its numbering from the reader's own declaration order. Seven
  things are still dropped and each counts `retain_lost`. A field of kind
  `17`, an array whose element kind is `17`, and the reserved node-table field
  itself, all three because a node index means nothing in a numbering this
  reader re-derives. An unknown enum variant, an unknown union arm and an
  unknown keyed-array slot, none of which is a field the reader can append.
  And a node record whose type id is unnameable, which is a whole node. A
  record whose inner structure the resolving walk finds damaged is dropped
  too, and that never turns the plain read `malformed`.
- **Retention covers `unknown` and no other counter.** A load that counted
  `kind_mismatch`, `clamped` or `malformed` still loses what those name on a
  rewrite, so the never-clobber condition keeps all three beside
  `retain_lost`.
- **The widening path runs FORWARD only.** A new build reads an old file's
  narrower integer of the same signedness, and its `float32` as a `float64`,
  exactly, counting `widened`. An OLD build reading a NEW file gets nothing:
  the narrowing is a `kind_mismatch` and the field reads its default. A rolling
  deploy that widens a field therefore loses that field on every peer still
  running the old build, and the baseline refuses the edit until a `--reason`
  says the fleet is flipped. The pairs are the two integer ladders and the one
  float rung and no others (SPEC-TABLES.md §4), which is what keeps the rest of
  the kind space refusing loudly instead of guessing.
- **A variant or union arm reordered or renamed is a lockstep redeploy**
  wherever a `type` reaches the declaration, a spelling fix included: those
  names ride in the projection in declaration order, because a reorder changes
  what every ordinal means and nothing else can see it, so a rename moves the
  protocol id with it.
- **The protocol id no longer guards a table-only vocabulary, and that guard
  was real while it lasted.** Scoping the projection by reachability (SPEC.md
  §3.1) ends an INCIDENTAL protection: two peers whose enum or union
  declarations disagreed used to refuse each other before they exchanged a
  byte, table data included, and they now connect. Nothing is misdecoded, and
  the reason is the table wire rather than this id. A variant and an arm ride
  as name hashes, so a reorder is invisible and safe and a rename is
  `unknown`, counted. **Two things had to be true for that to hold, and both
  were made true rather than found true.** `flags`, the one vocabulary where a
  reorder IS silent, is held in the projection. And `[E.Max]T` is refused in a
  table body (SPEC-TABLES.md §2.4, §11), so the other positional vocabulary a
  table could have had is gone rather than excepted. The residue is that a
  table-only enum or union is guarded by the tables baseline and the build
  version and no longer by the connect gate.
- **A field of a `type` that a table reaches cannot be renamed safely
  today** (#478): `was` is refused there, and a bare rename orphans every
  stored value.
- **`bits(N)` grows freely, and across a storage width it now costs a
  counter rather than the values**: `bits(9)` to `bits(16)` is one kind and is
  silent, and `bits(8)` to `bits(9)` moves kind `6` to kind `7`, which the
  widening rule decodes exactly and counts `widened`. Old builds reading the
  new file still lose it, on the row above.
- **`string(N)` and `bytes(N)` are different kinds** on the table wire though
  the packet wire treats them as one construct; respelling one as the other
  is a kind change.
- **`T`→`?T` turns every old elided default into an absent field.** The
  value is the default either way; a game that branches on presence sees
  every existing player as "never set."
- **A tightened range clamps on load and the next save writes the clamped
  value.** A narrowing is a data edit; back the files up first. A committed
  baseline warns on it, from either end and on a range declared where the
  field had none, but the warning is a report and not a rescue.
- **`None` means both "unknown variant" and "never set."** A retired variant
  and an unset field read the same; a game that must tell them apart keeps a
  separate presence field.
- **One `const` edit is as many silent edits as fields use it**, acknowledged
  under one `--reason`. Read the entry.
- **Flags are guarded by the opt-in frame and by a re-cook.** Reorder and
  rename in place move no counter, and they move the build version, so a
  cooked mask from before a reorder does not open under the new build. The
  wire stays silent. Commit a baseline; append at the end.
- **The baseline can be regenerated from nothing**: its deletion is a
  reviewable diff, not an invisible act, but a regenerated file starts the
  coverage clock over.
- **An unused type moves the protocol id**, and therefore the build version:
  a cleanup that deletes dead helper types is a redeploy and a re-cook. Batch
  cleanups with the next wire-moving release.
- **Two 64-bit hex ids sit side by side in every unit that declares a
  table**, and nothing at the type level stops a build engineer keying a cook
  cache on the protocol id. Key it on the build version, in the triple.
- **`Open` NAMES its refusal, and a caller that ignores the name is back
  where it was.** A wrong-version, foreign-order, truncated or corrupt cook
  returns null and fills a `TableRefuseReason` beside it (SPEC-TABLES.md §7),
  and `BlockOpen` answers the same enum, as does `LoadMeasure`'s `-1`
  (SPEC-TABLES.md §6.5) — which is why the enum is named for the REFUSAL and
  not for `Open`. The parameter is optional in every
  target so that no existing call site had to move, which means the silence is
  now the CALLER's choice rather than the design's: a fallback that logs the
  reason tells a build engineer whether to re-cook, to fix a cross-endian
  pipeline, to re-download, or to fix its own unaligned pointer, and one that
  passes nothing learns none of it. `unaligned_base` is the value worth
  checking first, because it is the one the caller caused.
- **Tiny messages pay for 64-bit identity, in the FILE form.** A file carries
  each distinct id once at eight bytes, so a three-field message is about 45
  bytes and an empty table is ten. A stream whose peers ship together is a
  `type` stream; one whose peers do not is the message form above, which sheds
  the trailer and takes that empty table to two bytes.
- **Deep pointered saves have no text form** past the text reader's depth
  cap of 128 levels; the "debug an old file" pattern hits that wall on the
  largest saves.
- **The enum vocabulary has three shapes**: a name hash on the wire, an
  ordinal in the cook and the block, storage at a derived width in memory.
  Reflection walkers read the descriptor, never the kind's width.
- **The C port writes the nested pointered form today**, and its files are
  not readable by the flat readers; the 3.0.0 parity gate blocks the release
  on it, and no shipped save exists for the hazard to replay against.

## Attacks considered

A red team argued every change a game team makes to a schema over five years
and every fleet, rollback, backup and ecosystem situation that breaks
versioning; a blue team answered each attack with the feature and a worked
example, and was allowed to be wrong. The competitors' versioning machinery,
Protocol Buffers, FlatBuffers, Avro and Cap'n Proto, was mapped mechanism by
mechanism onto this design. The record, all three documents verbatim with the
tally, is #464; the gaps it found are the issues in the list below.

## Owed before 3.0.0

Each of these is a claim this page makes in the present tense with the
repository not yet behind it. The 3.0.0 release holds the list at zero.

- #435: the uniform 64-bit wire, the form byte, the id table, the enum kind,
  flags bit positions in the build version, the reserved-id refusals.
- #523: the message form, form byte `2`, the id table message and its reserved
  build-version id, the announced unit vocabulary and its compiler-settled
  order, the connection table's bound, and the names §11 owes it.
- #434: the reserved escape kind.
- #463: the previous-release differential gate — the corpus generated by the
  previous release and the new one, byte-compared under an equal id.
- #432: the cook triple, and the byte-order sentences in five places.
- #396: table `was`; string, bytes and flags defaults in the baseline and
  the build version.
- #441: the retired-names ledger.
- #442: `was` for variants and arms; #478: `was` for the fields of a `type`
  that a table reaches.
- #446: the evolution table's fixtures.
- #540: the `[E.Max]T` refusal in a table body, which SPEC-TABLES.md §2.4 and
  §11 state and the checker does not make, so the positional spelling still
  compiles there and reopens the class §4.1 closed.
- #524: the reachability-scoped wire-shape projection, and the negative
  control on the walk that a missed edge must turn red.
- #525: retain-unknown, the two report counters, and the conformance rows.
- #522: the `wstring` kind `33` on the id-table wire, `*wstring`, the cooked
  storage, the text row and the table-form goldens (SPEC.md §4.12).
- #523: the `widened` counter and its two integer ladders, the
  `TableRefuseReason` enum on `Open`, `BlockOpen` and `LoadMeasure`, and `//`
  and `/* */` comments accepted by the text form's reader.
- #523: the unbounded array, `[]T` and `[]*T`, its refusals and the
  `list_migrates` golden that pins "the same bytes" as `[..N]T`
  (SPEC-TABLES.md §2.9).
- #523: the `///` doc comment, the field-, variant- and arm-level tag, and the
  `doc` and `tags` descriptor columns (SPEC.md §4.1, §4.2, SPEC-TABLES.md
  §8.1). A tag is carried on a `type` declaration alone today.
- #439 and #460: the standard's own contradictions on `T`→`*T`, the flags
  row, writer misuse, the declaration-rename row, the count of silent edits,
  and the pages that still say schema is not an evolution system.

When two sentences in the repository disagree, the one with the golden wins,
and the other is a bug in prose. A full read of the standard, front to back,
is re-run before each major.
