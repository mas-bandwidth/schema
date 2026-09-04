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
at 3.0.0. The packet wire's promises hold today with two open holes in what
its id can see, #462 and #463, named in its section below.

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
   id. An edit to a `type`, or to an enum, flags or union declaration, which
   both wires share, moves both, with the exception #462 closes: a reorder or
   rename of variants moves neither today. A `was` rename moves neither.
5. **Same-build forms match or refuse.** A cook or a block opens under
   exactly the build version that wrote it, by a header match, and otherwise
   refuses: `Open` returns null, and the tool's `uncook` names the mismatch.
   They are never read across a version.
6. **The kind set is closed within a major, and every future kind is
   skippable.** One kind number is reserved whose payload is a length and
   that many bytes; a kind added in a later major rides inside it, and a
   reader of this major skips it, counts it as unknown, and continues (#434).
7. **Ids are 64 bits, and the wire form is versioned.** Every id on the table
   wire is a 64-bit name hash, carried once per file in an id table; the
   file's first byte is the form version, so a reader that meets a later form
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
10. **This page moves with the facts.** No mechanical gate reads this page
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
structure, enum storage, flags bits, union arm order. It excludes what the
bytes cannot see: comments, layout, declaration order across records. A
source-text hash would produce spurious mismatches; hashing the compiler's
internal structures could produce a spurious *match*, which is the dangerous
direction. The projection is the thing in between, and it is versioned: its
first line is `ProjectionVersion`, and bumping it moves every protocol id in
existence. `schema id` prints the id and `schema projection` prints the text
it is taken over.

Two things the projection must carry and does not yet, both open as defects:

- **Variant order.** An enum value rides as its declaration ordinal, so a
  reorder changes what every stored ordinal means, and the projection today
  records only the variant count: reordering an enum or a flags declaration
  leaves the id unchanged, a spurious match. Variant names enter the
  projection in declaration order (#462); the consequence, that a variant
  rename then moves the id, is free under the redeploy rule below.
- **The codec law.** A compiler change that alters the bytes a schema
  produces without changing its shape, such as a rounding rule, must move
  every id. The projection gains a wire-law version line (#463), and a
  release gate generates the corpus with the previous release and the new one
  and requires byte identity under an equal id.

**Same or refuse.** Two peers holding the same protocol id interoperate; two
holding different ids refuse each other instead of misreading each other.
Whether the id travels on the wire, in a connect token, in a `const` field,
or out of band, is the application's choice. There is no negotiation and no
fallback, and everything downstream of the connection is simpler for it.

**What the same-or-refuse rule costs, and who pays.** Every declaration in the
unit contributes to the id, because a projection that guessed at reachability
would fail in the dangerous direction. So any `type` edit moves the id, an
unused helper type moves it, and so does any edit to an enum, flags or union
declaration, even one that only tables reach: adding an item kind or a quest
to an enum is a protocol id move. The price of every id move is a coordinated
redeploy of both ends. This project's own games redeploy both sides of every
connection together, always; cross-platform studios with certification lag
ship both store builds dark behind a gate and flip the server when both have
cleared. A studio that cannot force-update its clients is outside this model,
and should know it before choosing the packet wire for anything long-lived.

**Table bodies never enter the projection.** No edit inside a `table` body
can move the protocol id. That independence is held by test, and it is the
reason the two wires can have opposite stories without a third id to
reconcile them.

## The table wire and identity by name

On the table wire a field is `id, kind, payload`; a node, one table's worth of
data, is `type id, length, fields`. The **id** is the hash of the declared
name. That one decision produces the whole evolution story:

- **Add a field anywhere.** An old reader meets an id it does not know, skips
  the payload by its kind, counts one `unknown`, and continues. A new reader
  meets a file without the field and reads the declared default.
- **Remove a field.** The reverse: the old file's field is unknown to the new
  reader, skipped and counted. Nothing else moves.
- **Reorder freely.** Position carries nothing.
- **Rename with `was`.** `speed float32 | was = "velocity"` keeps the wire id
  `hash("velocity")` forever; the source name is for people. **`was` names the
  field's first wire name, forever.** A second rename keeps the same `was`; it
  is never re-pointed at an intermediate name.
- **A rename without `was` is the edit to fear, and the compiler cannot see
  it.** The compiler retains nothing between builds; to it a bare rename is a
  removal and an addition, both of which pass. Every value stored under the
  old name reads as the default from then on, counted `unknown`, and nothing
  reports that the two names were one field. A committed baseline is the one
  place the shape of a rename is visible, a removal and an addition in one
  table in one edit, and it will warn on that pair (#444).
- **Collisions are refused at compile time.** Two names in one vocabulary
  whose hashes coincide, or a `was` colliding with a live name, are a compile
  error naming both, and the refusal fires when the *new* name is added,
  before it has shipped, so a collision costs a naming wart and never a
  stored value.

**Every vocabulary, the same rule.** Enum variants and union arms are
identified by name hash exactly as fields are; a table's name is the node's
type id. Ids are 64 bits, `fnv1a64(name)`, in every vocabulary, one rule in
place of the 16-bit fold the repository still carries for fields and variants
until #435 lands. A 64-bit id gives a million-variant vocabulary an expected
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

- **The read report** says what a *reader* can tell happened: four counters,
  `unknown`, `kind_mismatch`, `clamped` and `duplicate`, and a `malformed`
  flag, filled on every load and never fatal on data from another build.
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
| a field added, removed or reordered | `unknown` on the side that lacks it | passes | moves (the record's layout moved) |
| a field renamed under `was` | nothing | passes | **nothing**: keyed by wire id, not source name |
| a field renamed bare | `unknown` on every old file; the new field reads its default | passes, in silence: a removal and an addition (#444 adds a warning on the pair) | moves |
| a field of a `type` that a table reaches, renamed | `unknown` on every old file; `was` is refused there today (#478) | passes, in silence | moves, and so does the protocol id |
| a scalar's default changed | **silent**: the same bytes mean something else | **refuses** | moves (a meaning fact) |
| a string, bytes or flags default changed | silent | refuses once #396 lands; not declarable today | moves |
| a bound raised or lowered, a capacity or array bound grown | `clamped` where a stored value exceeds it | passes; warns on a shrink | moves |
| a range tightened | `clamped` | passes; a warning is #443 | moves |
| a field's kind changed (`int32`→`int64`, `T`→`*T`, `string`→`bytes`, `bits(8)`→`bits(9)`) | `kind_mismatch`; the value reads as the default | **refuses** | moves |
| `T`→`?T`, `?T`→`T` | nothing: one framing. An old file's elided default reads as *absent* under `?T` | passes | moves (the presence flag is storage) |
| an enum variant added or removed | `unknown` where a stored name is gone | passes; warns on a removal | moves, and so does the protocol id |
| an enum variant reordered | nothing | passes | moves; the protocol id moves once #462 lands, **nothing today** |
| an enum variant renamed | the old name reads `None`, counted | warns | moves |
| a `fixed` field's `F` moved | **silent**: the raw integer reads at the new scale | **refuses** | moves |
| a field's referent replaced by one that cannot stand in (an enum respelled as its raw integer) | **silent** | **refuses** | moves |
| a flags variant inserted or removed | silent | refuses | moves, and so does the protocol id |
| a flags variant reordered or renamed in place | **silent** | **refuses** | moves once #435's rule lands; **nothing today**, and the protocol id moves once #462 lands |
| a keyed array made positional | `kind_mismatch` | refuses | moves |
| a keyed array's key enum swapped for another | `unknown`, one per slot; the kind stays | refuses | moves |
| a guard added or removed | nothing | passes | nothing |
| a `json =` key changed | nothing on the wire | passes | nothing |
| a table renamed | nothing when it is held by value (a declaration name is not on the wire); every pointer to it reads null and counts `unknown` when it is a pointer target | warns | moves, until table `was` lands (#396); then a `was` rename moves nothing |
| a retired name re-added with a new meaning | **silent** | **passes today**; the ledger is #441 | moves |
| a language added to the build | nothing | nothing | nothing |

Enum, flags and union declarations are shared by both wires, which is why
some rows move the protocol id as well: see the packet wire section.

## The read report

A load fills the report and returns; nothing on the table wire is fatal on
data from another build. `unknown` is the ordinary sound of evolution and
fires on every cross-version load by design; `duplicate` is the text form's
counter. `kind_mismatch`, `clamped` and `malformed` are damage or a decision,
and a game that alarms on those three and logs the others has the severity
split it needs. A tool that wants to know *which* field was clamped re-walks
the file with the descriptors, the per-field facts every table's generated
header carries; per-event attribution on the generated read path is additive
and safe to add after 3.0.0.

**The never-clobber rule.** Unknown fields are dropped on rewrite, by
decision: everything in schema has a schema, and a tool built from an older
schema that rewrites a newer file drops what it does not know and counts it.
That decision has one consequence every studio with a mixed fleet must write
into its own code before the first staged rollout: **a save cycle or a
rewriting tool never overwrites a file whose read report is not silent.** The
`unknown` counter fires on the exact load that precedes the destructive
rewrite. Write beside the file, or refuse the rewrite. Nothing in the runtime
enforces this: no generated `Save` refuses when the last load's report was
not silent, so the game keeps the report beside the instance and checks it
before it writes.

## The baseline

`tables.baseline`, in the unit's directory, is a committed projection of the
table closure, everything the unit's tables reach by value: every wire fact
of every field, evaluated, keyed by wire id. `schema check` (and so
`generate`) diffs the current schema against it and **refuses any edit that
would make data already written unreadable or quietly change what it means**:
the silent class above, plus kind changes, keyed-array respellings and
referent drops. It warns on shrinks, removals and declaration renames, and
passes in silence on everything the wire reports. `schema tables-baseline`
prints the projection, and with `--update` rewrites the file.

**Commit one.** The baseline is opt-in, no file, no check, and every
"refuses" in the table above holds only for a unit that has one. A unit whose
data leaves the building commits its baseline the day the first such build
ships, because **the first baseline covers only what comes after it**: data
written before that day was written against a shape nobody recorded. A
warning from `check` for a table-bearing unit with no baseline is #445.

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

**Before 3.0.0's evolution claims:** the retired-names ledger (#441), range
facts (#443), `was` misuse and the rename-pair warning (#444), the nudge
(#445).

## The build version

**Any divergence in the bytes a cook would produce is a new build version.**
The id is the low 64 bits of SHA-256 over the unit's *cook projection*, which
digests three groups of facts: the protocol id (the type wire's shape), the
layout of every record in the table closure (sizes, offsets, kinds, wire ids,
array classes and bounds, strides), and the meaning a wire load puts in those
slots (defaults, effective ranges, enum and union vocabularies, and, once
#435 lands, each flags variant's bit position). It is compiler-settled:
tooling cooks before any game binary exists, so the id must be knowable from
the schema alone. `schema build-version --facts` prints it with the facts it
was taken over.

**What moves it:** anything that moves the protocol id; a record added,
removed or renamed bare; any offset, size, alignment, kind or wire-id move;
any change to which declaration a field names; a declared maximum raised or
lowered; a default, range or vocabulary edit; a variant inserted into a keyed
enum (a keyed enum is a layout fact: its `Max` sizes every array keyed by it,
so a content edit re-cooks every asset of the unit); a `ProjectionVersion` or
cook-form bump.

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
the integers by width and sign, the floats, string, table, array, union,
keyed array, pointer index, the 128-bit integers, the fixed-point widths, and
(with #435) a kind of its own for enums, so that an enum and its raw integer
can never be confused on the wire. **A kind is spent only to close a silent
edit**; blocks spend none.

**A reader that does not know a kind cannot skip it.** That is the nature of
a closed set, and it is why a new kind is a wire change. Two rules make that
survivable across a major. First, **one kind number is reserved whose
payload is `L`, then `L` bytes**, opaque; a kind added in a later major is
introduced as that framing carrying its own inner encoding, so a reader of
this major skips it, counts it `unknown`, and continues (#434). Second, **the
file's first byte is the wire form's version**, so a reader can say "newer
form" rather than "malformed" when it meets one (#435). Neither is an
envelope: no identity, no magic and no content hash ride on the table wire,
and a file that needs to say what it is puts a field on its root table,
`format uint32 = 3`, which an older reader still reads, a foreign file
defaults, and that default is the provenance signal.

**The layout under #435.** After the form byte comes the body, as today, with
every id replaced by a reference and every width made variable: a field is
`ref, kind, payload`, and the field list ends with a zero ref. References are
1-based, unsigned LEB128, canonical (a non-minimal encoding is malformed);
lengths, counts, node indexes and node counts are the same varint, 64-bit in
capability and one byte for the small values they nearly always are. An enum
value, a union's arm id, a keyed array's slot keys and a node's type id are
all references. The file ends with the id table: the distinct 64-bit ids the
body used, in first-use order, then the count as a fixed u64, so that a
one-pass writer never patches and a reader, which holds the whole buffer
anyway, reads the count from the end, resolves the table once against its
own descriptors, and dispatches every field through an array index. There is
no mode: no implicit table, no width byte, no magic, no build version in the
header. The measurements that chose this layout are on #435.

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
  of negative raw values in that narrowing, and the protocol id did not move,
  because the projection carries no codec law line. Under #463 it would have
  moved every id, in a minor, announced; this entry is the worked example of
  why that line exists.

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

- **Widen a field.** There is no conversion path and no compatible-pairs
  list: a changed kind is a kind mismatch, and old data reads as the default,
  counted, never truncated, never misread. What is free: growing an array
  bound, a string or bytes capacity, loosening a range. What is not: `int32`
  to `int64`. The pattern: `gold2 ?int64` beside `gold`; old saves read
  `gold2` absent, the load shim copies `gold` across, new saves write it
  present; retire `gold` after the horizon.
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
  redeploy; a type, enum, flags or union edit always does. Write the
  never-clobber rule before the first staged rollout. Roll back freely: the
  old build reads the new files and counts what it cannot use.
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

- **Unknown fields do not survive a rewrite.** By decision, with the
  never-clobber rule as the required consequence, and no runtime enforcement.
- **There is no widening path**, by choice: a whitelist of compatible pairs
  silently misdecodes everything outside the list.
- **A content addition that is an enum variant is a lockstep redeploy**, even
  when only tables reach the enum, because the declaration is shared by both
  wires.
- **The protocol id cannot see an enum or flags reorder today** (#462): until
  it lands, a reorder is a spurious match. Do not reorder; append.
- **A field of a `type` that a table reaches cannot be renamed safely
  today** (#478): `was` is refused there, and a bare rename orphans every
  stored value.
- **`bits(N)` widens freely only within a storage width**: `bits(8)` to
  `bits(9)` is a kind change and loses every stored value on read; `bits(9)`
  to `bits(16)` is free.
- **`string(N)` and `bytes(N)` are different kinds** on the table wire though
  the packet wire treats them as one construct; respelling one as the other
  is a kind change.
- **`T`→`?T` turns every old elided default into an absent field.** The
  value is the default either way; a game that branches on presence sees
  every existing player as "never set."
- **A tightened range clamps on load and the next save writes the clamped
  value.** A narrowing is a data edit; back the files up first.
- **`None` means both "unknown variant" and "never set."** A retired variant
  and an unset field read the same; a game that must tell them apart keeps a
  separate presence field.
- **One `const` edit is as many silent edits as fields use it**, acknowledged
  under one `--reason`. Read the entry.
- **Flags are guarded by the opt-in frame only, today.** Reorder and rename
  in place move no counter and, until #435 lands, no id; a cooked mask from
  before a reorder opens under the new build with the bits meaning different
  things. Commit a baseline; append at the end.
- **The baseline can be regenerated from nothing**: its deletion is a
  reviewable diff, not an invisible act, but a regenerated file starts the
  coverage clock over.
- **An unused type moves the protocol id**, and therefore the build version:
  a cleanup that deletes dead helper types is a redeploy and a re-cook. Batch
  cleanups with the next wire-moving release.
- **Two 64-bit hex ids sit side by side in every unit that declares a
  table**, and nothing at the type level stops a build engineer keying a cook
  cache on the protocol id. Key it on the build version, in the triple.
- **`Open` refuses in silence.** A wrong-version, corrupt or truncated cook
  returns null; only the tool's `uncook` names the mismatch. Log the null.
- **Tiny messages pay for 64-bit identity.** Under #435 a file carries each
  distinct id once at eight bytes, so a three-field message grows from about
  20 bytes to about 45; a stream that wants small same-build messages is a
  `type` stream.
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
- #434: the reserved escape kind.
- #462: variant order in the protocol id projection.
- #463: the wire-law line in the projection and the previous-release
  differential gate.
- #432: the cook triple, and the byte-order sentences in five places.
- #396: table `was`; string, bytes and flags defaults in the baseline and
  the build version.
- #441: the retired-names ledger.
- #442: `was` for variants and arms; #478: `was` for the fields of a `type`
  that a table reaches.
- #443, #444, #445: range facts, `was` misuse and the rename-pair warning,
  the baseline nudge.
- #446: the evolution table's fixtures.
- #439 and #460: the standard's own contradictions on `T`→`*T`, the flags
  row, writer misuse, the declaration-rename row, the count of silent edits,
  and the pages that still say schema is not an evolution system.

When two sentences in the repository disagree, the one with the golden wins,
and the other is a bug in prose. A full read of the standard, front to back,
is re-run before each major.
