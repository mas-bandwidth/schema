# Versioning

```bash
schema version
schema id
schema build-version --facts
schema tables-baseline
```

If anything is going to be wrong with this library, it is going to be that we
did not think something through for versioning. Serialization libraries do not
die of slow code. They die in year three, when a save file written by a build
nobody has anymore meets a build the writer never saw, and something that was
promised on a page turns out not to be true in the bytes. This page is the
promise, stated so that it can be checked, and it is a commitment: every fact
on it is held by a golden or a negative control in the tree, or is listed at
the end as owed, and any change that moves one of these facts moves this page
in the same pull request.

It is also an essay, because a list of rules is not enough to design against.
The rules come from a small number of ideas, and a reader who holds the ideas
can predict the rules — and can tell when a new rule would be wrong.

## The promise

1. **Data written by any build reads in any other build of the same major, in
   either direction, and never crashes.** A table wire from a newer build reads
   in an older one and the other way round. Every difference lands in the read
   report; none is fatal.
2. **Nothing is misdecoded.** A value is read as what it was written as, or it
   is skipped and counted. The edits the wire cannot report — the *silent
   class* — are enumerated, closed, and refused at compile time by the baseline.
3. **Identity is the name.** A field, an enum variant, a union arm and a table
   are identified on the table wire by the hash of their name. Add anywhere,
   remove, reorder. A rename that must keep its data declares the old name with
   `was`, and a `was` moves nothing anywhere.
4. **Two ids and no third.** The *protocol id* versions the packet wire and is
   the only thing two peers compare before they talk. The *build version*
   versions the cooked and blocked forms and addresses every cooked asset. A
   table edit moves the build version and never the protocol id; a type edit
   moves both. A `was` rename moves neither.
5. **Same-build forms match or refuse.** A cook or a block opens under exactly
   the build version that wrote it, by a header match, and otherwise refuses by
   name. They are never read across a version.
6. **The kind set is closed within a major, and every future kind is
   skippable.** A reader of this major skips any kind a later major adds,
   counts it as unknown, and continues.
7. **Ids are 64 bits, and the wire form is versioned.** Every id on the table
   wire is a 64-bit name hash, carried once per file in an id table; the
   framing header carries the form version, so a reader that meets a later
   form refuses by name rather than reading it as damage.
8. **Peers connect on the protocol id and may differ in build version.** Table
   data crossing a connection between builds is the ordinary case, and the read
   report is the witness.
9. **A patch release never moves the protocol id or the build version's form.**
   A minor may move either only together with the wire, announced first and
   loudly. A major is when your world breaks.
10. **This page moves with the facts.** A pull request that changes any fact
    stated here changes this page in the same change. A claim without a golden
    behind it is listed under "owed," never left implied.

## Two wires, two stories

Schema produces two wires from one language, and their versioning stories are
opposites on purpose.

The **packet wire** — what `type` declarations produce — is positional and
compact. Every bit is placed by the schema; nothing on the wire says what it
is. Two sides can read it only if they agree on the schema exactly, and the
protocol id is how they find out. There is no evolution on this wire, no
optional-field machinery, no tags, because a packet is read by the build that
was shipped with the one that wrote it. This is where the bytes-per-packet
budget is won.

The **table wire** — what `table` declarations produce — is self-describing
and tolerant. Every field carries its identity and its kind; a reader skips
what it does not know and defaults what it does not find. It costs bytes for
that, deliberately: this is the wire for data that outlives the build that
wrote it — saves, replays, cooked-asset sources, anything on disk, anything in
a database, anything crossing a connection between builds. It is designed so
that a five-year-old file reads in today's build, and today's file reads in
the five-year-old build, and each side learns exactly what it could not
understand.

**The rule that follows: anything that outlives a build rides tables.** A
replay recorded as packet streams dies with the first type edit; a replay
recorded as tables (commands, or snapshots) reads for years. A save is a
table. A cooked asset's source is a table. A packet is for the wire between
two builds that ship together, and for nothing else.

## The packet wire and the protocol id

The protocol id is the low 64 bits of SHA-256 over the unit's *wire-shape
projection* — a text rendering of every fact that determines packet bytes:
field order and names, kinds, widths, bounds, array bounds, string capacities,
float ranges, fixed-point widths, defaults, branch structure, enum storage
bits, flags bits, union arm order. It excludes what the bytes cannot see:
comments, layout, declaration order across records, enum and union *variant
names* (a variant rides by ordinal on this wire, so `Red` renamed to
`Crimson` moves nothing). A source-text hash would produce spurious
mismatches; hashing the compiler's internal structures could produce a
spurious *match*, which is the dangerous direction. The projection is the
thing in between, and it is versioned: its first line is `ProjectionVersion`,
and bumping it moves every protocol id in existence, which is why it changes
only when the projection must describe something it could not before.

**Same or refuse.** Two peers holding the same protocol id interoperate; two
holding different ids refuse each other instead of misreading each other.
Whether the id travels on the wire — in a connect token, a `const` field, out
of band — is the application's choice. There is no negotiation and no
fallback, and that is the feature: everything downstream of the connection is
simpler for it.

**What the same-or-refuse rule costs, and who pays.** Any type edit moves the
id, and so does an unused helper type added to the unit — every declaration
contributes, because a projection that guessed at reachability would fail in
the dangerous direction. The price is that every id move is a coordinated
redeploy of both ends. The recorded doctrine for this project's own games is
that both sides of every connection redeploy together, always; cross-platform
studios with certification lag ship both store builds dark behind a gate and
flip the server when both have cleared. A studio that cannot force-update its
clients is outside this model, and should know it before choosing the packet
wire for anything long-lived.

**Table declarations never enter the projection.** No table edit can move the
protocol id, so no table edit forces a lockstep redeploy. That independence is
held by test, and it is the reason the two wires can have opposite stories
without a third id to reconcile them.

## The table wire and identity by name

On the table wire a field is `id, kind, payload`; a node — one table's worth
of data — is `type id, length, fields`. The **id** is the hash of the
declared name. That one decision produces the whole evolution story:

- **Add a field anywhere.** An old reader meets an id it does not know, skips
  the payload by its kind, counts one `unknown`, and continues. A new reader
  meets a file without the field and reads the declared default.
- **Remove a field.** The reverse: the old file's field is unknown to the new
  reader, skipped and counted. Nothing else moves.
- **Reorder freely.** Position carries nothing.
- **Rename with `was`.** `speed float32 | was = "velocity"` keeps the wire id
  `hash("velocity")` forever; the source name is for people. A bare rename is
  refused, because it would silently orphan every byte ever written under the
  old name, and the compiler is the only party that can see that. **`was`
  names the field's first wire name, forever.** A second rename keeps the same
  `was`; it is never re-pointed at an intermediate name. (The misuse — a new
  `was` naming a field that itself carried one — passes silently today and is
  owed a warning: #444.)
- **Collisions are refused at compile time.** Two names in one vocabulary
  whose hashes coincide, or a `was` colliding with a live name, are a compile
  error naming both — and the refusal fires when the *new* name is added,
  before it has ever shipped, so a collision costs a naming wart and never a
  stored value.

**Every vocabulary, the same rule.** Enum variants and union arms are
identified by name hash exactly as fields are; a table's name is the node's
type id. Ids are 64 bits — `fnv1a64(name)` — in every vocabulary, a single rule where the language once had a 16-bit
fold for fields and variants and a 64-bit hash for tables. A 64-bit id gives a
million-variant vocabulary an expected 0.00000003 collisions over its life.
The wire does not pay eight bytes per field for it: a file carries each
distinct id once, in a table at its root, and every reference is a one-byte
index into that table (#435 carries the layout and its measurements). (The
uniform-64 wire lands in #435; until then the tree carries the 16-bit fold,
and this section describes the committed rule.)

**`was` in every vocabulary.** Today `was` is a field attribute. Table `was`
is ruled and landing (#396): a renamed pointer-target table keeps its node
type id, so a rename no longer nulls every pointer in every stored file.
Variant and arm `was` is owed (#442). Until they land, renaming a variant or an
arm is a new name and stored values read `None` — say what you mean.

**Flags are the one positional vocabulary.** A mask rides raw, so a variant's
identity is its bit. The rule for flags is *append at the end*; insert,
reorder and rename-in-place are the silent class and the baseline refuses
them. Removing a variant frees no bit — retire the name, keep the position.

**Defaults are part of the wire contract.** A field equal to its declared
default is elided from the wire, and an absent field reads as the default. So
a changed default changes what every stored file *means* without touching a
byte, and `was` does not cover it: `was` preserves an identity, not a value.
Change a default the way you would change data — rewrite the files you hold
— or add a new field and leave the old one alone. Tuning values that move
weekly belong in configuration the studio holds, where rewriting is a
pipeline step; player saves hold state, which has no weekly default.

## The three frames

Three mechanisms judge an edit, and each sees what the others cannot:

- **The read report** says what a *reader* can tell happened: five counters —
  `unknown`, `kind_mismatch`, `clamped`, `duplicate`, `malformed` — filled on
  every load, never fatal on data from another build.
- **The baseline** says what the *compiler* refuses to let you do to data
  already written: the edits the wire cannot report, caught before they ship.
- **The build version** says whether a cooked or blocked file this build
  wrote is still this build's.

One table reconciles them. It is the single statement of what an edit does;
§4, §18.2 and §20.4 of SPEC-TABLES.md derive from it, and it is owed a golden
— one fixture per row, each edit run through all three frames with the
verdicts pinned (#446) — so that it can go red.

| the edit | the read report | the baseline | the build version |
|---|---|---|---|
| a field added, removed or reordered | `unknown` on the side that lacks it | passes | moves (the record's layout moved) |
| a field renamed under `was` | nothing | passes | **nothing** — keyed by wire id, not source name |
| a field renamed bare | refused at compile time | — | — |
| a scalar's default changed | **silent** — the same bytes mean something else | **refuses** | moves (a meaning fact) |
| a string, bytes or flags default changed | silent | refuses (#396 extends the rule) | moves |
| a bound raised or lowered, a capacity or array bound grown | `clamped` where a stored value exceeds it | passes; warns on a shrink | moves |
| a range tightened | `clamped` | passes today; **warn owed** (#443) | moves |
| a field's kind changed (`int32`→`int64`, `T`→`*T`, `string`→`bytes`, `bits(8)`→`bits(9)`) | `kind_mismatch`; the value reads as the default | **refuses** | moves |
| `T`→`?T`, `?T`→`T` | nothing — one framing | passes | nothing |
| an enum variant added, removed or reordered | `unknown` where a stored name is gone | passes; warns on a removal | moves |
| an enum variant renamed | the old name reads `None`, counted | warns | moves |
| a `fixed` field's `F` moved | **silent** — the raw integer reads at the new scale | **refuses** | moves |
| a field's referent replaced by one that cannot stand in (an enum respelled as its raw integer) | **silent** | **refuses** | moves |
| a flags variant inserted or removed | silent | refuses | moves (through the protocol id) |
| a flags variant reordered or renamed in place | **silent** | **refuses** | moves once #435's rule lands; **nothing today** |
| a keyed array made positional, or its key enum swapped | `kind_mismatch` | refuses | moves |
| a guard added or removed | nothing | passes | nothing |
| a `json =` key changed | nothing on the wire | passes | nothing |
| a table renamed | every pointer to it reads null, `unknown` counted | warns | moves — until table `was` lands (#396), then a `was` rename moves nothing |
| a retired name re-added with a new meaning | **silent** | **passes today** — the ledger is owed (#441) | moves |
| a language added to the build | nothing | nothing | nothing |

Two rows are in bold twice because they are the ones that matter most. The
flags row states the committed rule and the current tree in one cell, because
this page does not describe a wire that does not exist yet without saying so.

## The read report

A load fills the report and returns; nothing on the table wire is fatal on
data from another build. `unknown` and `duplicate` are the ordinary sound of
evolution and fire on every cross-version load by design. `kind_mismatch`,
`clamped` and `malformed` are damage or a decision, and a game that alarms on
those three and logs the other two has the severity split it needs. A tool
that wants to know *which* field was clamped re-walks the file with the
descriptors; per-event attribution on the generated read path is a named
follow-on, additive and safe to add after 3.0.0.

**The never-clobber rule.** Unknown fields are dropped on rewrite, by ruling:
everything in schema has a schema, and a tool built from an older schema
that rewrites a newer file drops what it does not know and counts it. That
ruling has one consequence every studio with a mixed fleet must write into
its own code before the first staged rollout: **a save cycle or a rewriting
tool never overwrites a file whose read report is not silent.** The `unknown`
counter fires on the exact load that precedes the destructive rewrite. Write
beside the file, or refuse the rewrite, and the rollback-and-forward case,
the cross-progression case and the staged-fleet case all survive on one
sentence of policy. Nothing carries unknown bytes *through* a rewrite; the
old file kept whole is the preservation.

## The baseline

`tables.baseline`, in the unit's directory, is a committed projection of the
table closure: every wire fact of every field, evaluated, keyed by wire id.
`schema tables-baseline` checks the current schema against it and **refuses
any edit that would make data already written unreadable or quietly change
what it means** — the silent class above, plus kind changes, keyed-array
respellings and referent drops. It warns on shrinks, removals and declaration
renames, and passes in silence on everything the wire reports.

**Commit one.** The baseline is opt-in — no file, no check — and every
"refuses" in the table above holds only for a unit that has one. A unit whose
data leaves the building (saves, anything on a player's disk) commits its
baseline the day the first such build ships, because **the first baseline
covers only what comes after it**: data written before that day was written
against a shape nobody recorded. A nudge from `check` for a table-bearing unit
with no baseline is owed (#445).

**Moving it is explicit and reasoned.** A refused edit that is nonetheless
intended is accepted with `--update --reason "..."`, which rewrites the file
and appends a dated entry to its history section naming every edit, old value
to new. `--update` without a reason is refused. The history is the one record
the wire lacks — it is what a person consults when an old save reads back
wrong — and it survives every later `--update` verbatim.

**Merging two branches of one schema.** Merge the `.schema` text; keep either
parent's baseline; run the check — the refusals name every semantic
divergence the textual merge introduced; then `--update --reason "merge"`,
and read the entry, which lists each edit. The history sections are unioned
by hand. Two forks that both shipped and both added a field whose ids collide
force one hand migration; the odds are one in four billion per cross pair.

**Its own version.** The baseline's rendering carries a version on its first
line. Any new judged token bumps it, which makes every committed baseline
stale at once, deliberately and visibly; `--update` repairs a file it cannot
read, salvaging the history. The window during which that repair runs is the
one window the check is off, marked in the history as "could not be diffed";
review the schema diff of that commit by hand.

**Owed on the baseline before 3.0.0's evolution claims:** the retired-names
ledger (#441) — the one silent edit the enumeration misses because it needs
history the tool does not keep; range facts (#443); `was` misuse (#444); the
nudge (#445).

## The build version

**Any divergence in the bytes a cook would produce is a new build version.**
That sentence is the whole definition. The id is the low 64 bits of SHA-256
over the unit's *cook projection*, which digests three groups of facts: the
protocol id (the type wire's shape), the layout of every record in the table
closure (sizes, offsets, kinds, wire ids, array classes and bounds, pitches),
and the meaning a wire load puts in those slots (defaults, effective ranges,
enum and union vocabularies, and — once #435 lands — each flags variant's bit
position). It is compiler-settled: tooling cooks before any game binary
exists, so the id must be knowable from the schema alone.

**What moves it:** anything that moves the protocol id; a record added,
removed or renamed bare; any offset, size, alignment, kind or wire-id move;
any change to which declaration a field names; a declared maximum raised or
lowered; a default, range or vocabulary edit; a variant inserted into a keyed
enum (a keyed enum is a layout fact — its `Max` sizes every array keyed by
it, so a content edit re-cooks every asset of the unit); a `ProjectionVersion`
or cook-form bump.

**What does not:** comments, layout, declaration order across records; a
`was` rename — this is §7's stated obligation, a rename must not invalidate
every cooked file in existence, and the layout is keyed by wire id to make it
true; a guard; a `json` key; anything only the baseline judges; adding a
target language. **Byte order is not an input.** Build version is
target-neutral, one id for every target of a game (#432).

**Its two jobs.** It *addresses* a cooked artifact — the store is indexed by
it — and it *refuses* one — `Open` checks it out of the header. There is no
second version id anywhere in the cooked or blocked forms; a form's identity
is its magic, not a second digest.

**The release policy, stated here because nothing stated it.** The build
version's *form* — its projection rendering and the cook form it names — can
move only by a deliberate version bump in the compiler, and such a bump
invalidates every cooked asset in every build cache at once. So: **no patch
release moves the build version's form**; a minor release may bump it only
together with a wire change, announced first and loudly in the release notes,
exactly as the protocol id's rule reads; a major may. A schema edit moving the
id is the user's own act and is priced by the cook cost model below. (The
tree today has a form version constant and no rule about which release may
bump it; this paragraph is the rule, and the release gate holds it from
3.0.0.)

**Peers connect on equal protocol ids and may differ in build version.**
Cooked assets are local on both sides — each peer loads its own cooks, out
of its own store, and neither ever sees the other's — so a build-version
difference is not a connection question. This is the fact behind promise 8.

## The same-build forms: cook and block

A **cook** is a table's data laid out as the target build's in-memory records,
in the target's byte order, so that loading is a header match and a pointer.
The fix-up for byte order happens where the target is known — offline, once,
on the writing side — and never on the reading side, which is what makes
`Open` a match and a point rather than a pass over the region. **A cooked
artifact is content-addressed by a triple: the hash of its source wire file,
the unit's build version, and the target byte order** (#432). Tooling
produces a cook under that triple; the store is indexed by it; the game does
a lookup. A hit cannot be refused by `Open` save on a corrupt or truncated
file, where it returns null and the caller falls back to a wire load — the
path that carries every version. **Meter that fallback rate**; a store that
evicted last week's build version turns a rollback into a fleet-wide slow
load with no error anywhere, and the metric is the only witness.

**The cook cost model, in one sentence: a new build version is a new cook,
and that is the model rather than a cost.** Nothing finer than the unit is
keyed, because a finer key buys a smaller re-cook and pays for it with a
second id. A weekly-tuned live game shipping full cooks to players pays real
bandwidth for that; the answers are binary diffing at the patcher, which is
its layer, and tunables in studio-held configuration rather than in cooked
assets.

**The dev loop runs on the wire.** Cook and block are ship-load accelerators;
if load time does not demand one, the game uses the generic table, which
hot-reloads across any edit and reports what changed. No "ignore build
version in development" flag exists or should, because the path such a flag
would open already exists and is tolerant.

A **block** is a fixed table's rows at a compiler-computed pitch, for data one
language writes and another reads at frame rate. It is same-build in the
strictest sense: a prologue of magic, build version and byte order, and
`BlockOpen` either matches all three or refuses. Its evolution story is that
it has none — any layout edit moves the build version, any ABI drift is a
build error on both generated sides before it is anything else.

## The kind space and the wire form

Every table-wire payload begins with a kind byte from a closed set — bool,
the integers by width and sign, the floats, string, table, array, union,
keyed array, pointer index, the 128-bit integers, the fixed-point widths, and
(landing with #435) a kind of its own for enums, so that an enum and its raw
integer can never be confused on the wire. **A kind is spent only to close a
silent edit**; blocks and maps spend none.

**A reader that does not know a kind cannot skip it** — that is the nature of
a closed set, and it is why a new kind is a wire change. Two rules make that
survivable across a major. First, **every kind with the high bit set is
length-framed by rule** — `L`, then `L` bytes — so a reader of this major
skips any kind a later major adds, counts it `unknown`, and continues (#434).
Second, the file's **framing header carries the wire form's own version**,
so a reader can say "newer form" rather than
"malformed" when it meets one (#435). Neither is an envelope: no identity, no
magic, no content hash rides on the table wire, and a file that needs to say
what it is puts a field on its root table — `format uint32 = 3` — which an
older reader still reads, a foreign file defaults, and that default is the
provenance signal.

**The framing is variable-length and the ids are not.** Lengths, counts and
node indexes are 64-bit in capability and one or two bytes for the small
values they nearly always are. Ids are 64-bit hashes and incompressible, so a
file carries each distinct id once, in a table at the root, and every field
references it by a short index — one byte where today's field id is two,
resolved once at open into a dispatch array. The design and its measurements
are on #435.

## The text form

The text form is JSON keyed by *names*. It carries no version, no schema
reference and no envelope, and reads with the wire's own tolerance: absent
keys keep their defaults, unknown keys are skipped and counted, values outside
a range clamp and count, so a text and a wire loaded from the same data land
the same instance. One consequence to plan for: **the text's identity is the
name, and `was` does not carry it.** A field renamed under `was` is safe on
the wire and breaks every hand-edited configuration file, modder file and
editor that spelled the old key — unless the rename is paired with
`| json = "old"`, which keeps the text key while the wire keeps its id. Pair
them. Shared nodes carry a `&node` label, a reserved prefix, and a fixed reader
refuses a text that places one; everything else it does not place, it does
not police.

## The compiler's own version

The compiler follows [semantic versioning](https://semver.org/), and the thing
being versioned needs saying precisely, because for a code generator two
different things can change: the compiler the user runs, and the wires their
schemas produce. Each has its own version, and they are not the same number.

**Major** — the user's world breaks: existing `.schema` files stop compiling
or change meaning, the generated API breaks, or the wire's closed set gains a
kind. Expect a migration note. Nothing less than this earns a major.

**Minor** — additive features: new language features, new attributes, new
backends, better diagnostics, generated code that is faster or cleaner. New
syntax you have not used cannot affect you. A minor release may also carry a
wire change, *with* its protocol id bump, or a build-version form bump: the
bytes and the id move together, so deployed peers refuse newly built ones
rather than misread them — and the release notes state the bump first and
loudly, before anything else in the entry.

**Patch** — bug fixes and documentation, and one promise, kept verbatim:
**"no PATCH release will break protocol id"** — nor the build version's form.
Take any schema, rebuild it with a newer patch release of the same minor, and
its protocol id and its build version are the same ids. Patch releases are
always safe to take.

This is what the pinned goldens in CI exist to enforce. The conformance
corpus's golden wire bytes pin every construct's encoding; `schema id` and
`schema build-version` over the corpus are pinned as exact values; a compiler
change that moves any of them is a stop-the-line event, never a quiet re-pin.
Moving a golden is a deliberate act argued for in review, and the release that
carries it wears the number these rules assign.

**History: the v2 line.** An early v2.0.0 and v2.1.0 were re-versioned into
the 1.x line as v1.6.0 and their tags retired; the 2.x line that exists today
starts at the v2.0.0 that follows v1.16.0 and carries the module path
`github.com/mas-bandwidth/schema/v2`. The 3.0.0 gate is complete feature
parity across all nine languages, each at its language's performance floor
(#366); the wire is settled at 3.0.0.

**Recorded wire-affecting amendments.** The rules above are policy; this
records the instances, so the history of "a release moved bits" lives where
the compatibility rules do.

- **2026-08-15 — fixed-point rounding unified: half away from zero.** The
  generated fixed-point narrowing changed from the bare arithmetic shift (ties
  toward +infinity) to the one fixed-point rounding rule, ties away from zero.
  This moved the bytes generated code produced only on exact ties of negative
  raw values in that narrowing, and the protocol id did not move — a rounding
  rule is not wire shape, so the id cannot see this class of change. It rode
  the next release loudly, with this note.
- **2026-09-04 — the table wire rewritten before 3.0.0**: uniform 64-bit ids
  with a framing header, variable-length framing, a dedicated enum kind, the
  length-framed rule for future kinds, flags bit positions in the build
  version, and the cook triple (#432, #434, #435). Every table golden re-pins
  under it, deliberately; the protocol id does not move, because no table fact
  enters it, and that is checked.

The standing risk calculus for every wire-affecting amendment: both sides of
every connection redeploy together, so an amendment that moves bytes is
priced by that doctrine, not by the fiction that deployed halves must
interoperate across the change.

**What is not covered.** The protocol id is not a version number; it is a
hash of your schema's wire shape and changes when your schema's wire changes,
independently of the compiler's version. Generated files do not record the
compiler version, deliberately: stamping it would put a diff in every
generated file in every downstream repository on every release, saying nothing
about whether the wire changed; generated code carries the protocol id and the
build version instead, which are the things that govern compatibility.

**The Go API under `compiler/` and `ir/` is covered; `internal/` is not.**
`github.com/mas-bandwidth/schema/v2/compiler` loads and checks units and
generates through registered generators; `github.com/mas-bandwidth/schema/v2/ir`
is the checked unit those generators read. Their exported surface follows the
rules above: breaking it is a major, adding to it is a minor. Everything
under `internal/` — the scanner, parser, AST, checker and the nine per-language
emitters — carries no promise and may change in any release. The tables
baseline adds `compiler.TablesBaselineText`, `compiler.UpdateTablesBaseline`,
the `TablesBaseline` and `OnWarn` policy fields on `compiler.Compiler`, and
in `ir` the table-wire kind vocabulary — the `TableKind*` constants and
`TableScalarKind` / `TableFieldKind` / `TableElemKind` — which are wire law
and therefore frozen within a major.

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

They share a wire standard (`STANDARD.md`, kept identical across all six)
and are checked against each other. CI pins each to its latest released tag,
never to `main`, in exactly one place per sibling; a given compiler release
states the minimum runtime version it needs, and newer runtimes keep working
with older generated code. Dart, Java and Elixir generate self-contained
output and need only their pinned SDKs.

**Release process.** Releases are tagged `vMAJOR.MINOR.PATCH` on `main`. CI
must be green on the tagged commit — the cross-language corpus, the seeded
fuzz corpus, and the certify chain. The build stamps `git describe` into the
binary, so `schema version` on a release build reports the exact tag and on a
development build reports the tag plus commits-since plus hash; quote that
line in bug reports. A refusal message cites the spec that binary was built
against, so a stale binary will contradict the current page — check the
version first.

## Patterns

The features above answer most of what a live game does to a schema over
years, and several of the answers are patterns rather than syntax. Each is
one paragraph here and gets its worked example in the expert guide.

- **Widen a field.** There is no conversion path and no compatible-pairs
  list: a changed kind is a kind mismatch, and old data reads as the default,
  counted — never truncated, never misread. What is free: growing an array
  bound, a string or bytes capacity, loosening a range, and widening an
  enum's storage (an enum rides by name hash whatever its declared width).
  What is not: `int32` to `int64`. The pattern: `gold2 ?int64` beside `gold`;
  old saves read `gold2` absent, the load shim copies `gold` across, new
  saves write it present; retire `gold` after the horizon.
- **Split, merge or move a field.** Identity is (table, name); nothing
  carries a value across that boundary. Keep the old fields declared through
  the migration horizon, shim on load, retire after. Five years of shims is
  the migration system; keep them in one file.
- **Change what a field means.** A meaning change is a new identity: rename
  *without* `was`, or add a field. The deliberate orphaning is correct — old
  values are wrong at the new meaning, and reading them raw is the bug every
  other format commits.
- **Retire a field.** Removal is free and reported; there is no deprecation
  marker because removal is what deprecation exists to fake elsewhere. Do not
  re-add the name with a new meaning; until the ledger (#441) refuses it, the
  baseline's history is the only record that the name is haunted.
- **Rename anything.** `was` on the wire; `json =` for the text; never
  re-point a `was`.
- **Ship a schema change to a mixed fleet.** Tables never force a redeploy;
  types always do. Write the never-clobber rule before the first staged
  rollout. Roll back freely — the old build reads the new files and counts
  what it cannot use.
- **Keep a replay.** Record it as tables.
- **Debug an old file.** `uncook` and `unpack` render any version to text;
  the read report says what was not understood; the baseline history says
  what changed and why, by date. Put a `format` field on your root table so a
  restored backup can say which schema wrote it.

## Sharp edges, stated

A versioning page that hides its edges is how the failure this page exists
to prevent happens. These are the places the design chose a cost, or has one
still open.

- **Unknown fields do not survive a rewrite.** Ruled, with the never-clobber
  rule as the required consequence.
- **There is no widening path**, by choice: the alternative — a whitelist of
  compatible pairs — silently misdecodes everything outside the list.
- **`bits(N)` widens freely only within a storage width**: `bits(8)` to
  `bits(9)` is a kind change and loses every stored value on read; `bits(9)`
  to `bits(16)` is free.
- **`string(N)` and `bytes(N)` are different kinds** on the table wire though
  the packet wire treats them as one construct; respelling one as the other
  is a kind change.
- **Flags are guarded by the opt-in frame only, today.** Reorder and rename in
  place move no counter and, until #435 lands, no id; a cooked mask from
  before a reorder opens under the new build with the bits meaning different
  things. Commit a baseline; append at the end.
- **The baseline can be regenerated from nothing** — its deletion is a
  reviewable diff, not an invisible act, but a regenerated file starts the
  coverage clock over.
- **An unused type moves the protocol id**, and therefore the build version:
  a cleanup that deletes dead helper types is a redeploy and a re-cook. Batch
  cleanups with the next wire-moving release.
- **Two 64-bit hex ids sit side by side in every generated unit.** Their
  values are engineered never to coincide, even for an empty unit, but nothing
  at the type level stops a build engineer keying a cook cache on the
  protocol id. Key it on the build version, in the triple.
- **The enum vocabulary has three shapes**: a name hash on the wire, an
  ordinal in the cook and the block, storage at a derived width in memory.
  Reflection walkers read the descriptor, never the kind's width.
- **The C port writes the nested pointered form today**, and its files are
  not readable by the flat readers; the 3.0.0 parity gate blocks the release
  on it, and no shipped save exists for the hazard to replay against.

## Attacks considered

On 2026-09-04 a red team was set against this design with the brief to argue
every change a game team makes to a schema over five years, and every fleet,
rollback, backup and ecosystem situation that breaks versioning; a blue team
answered each attack with the feature and the worked example, and was allowed
to be wrong. Thirty-five attacks: seven answered by a feature, nine by
recorded doctrine, seventeen partially with the residue named, two
unanswered — both the same root cause, a retired name reused, which is #441.
Blue corrected red's headline arithmetic (a 300-variant vocabulary had a
one-in-two chance of one 16-bit collision over its life, not two-in-three,
and the forced rename always lands on the unshipped name); the 64-bit ruling
made the arithmetic moot. The competitors' versioning machinery — Protocol
Buffers, FlatBuffers, Avro, Cap'n Proto — was mapped mechanism by mechanism
onto this design: every failure their machinery defends against is covered
here, usually by a mechanism where they have advice, except the retired-name
ledger and renames beyond fields, both listed below. The deepest scar in
Protobuf's history, "required is forever," is pre-absorbed: every field
optional with a declared default is the ground rule, not a retrofit. The
full record is on #439 and the issues it cites.

## Owed before 3.0.0

Each of these is a claim this page makes in the present tense with the tree
not yet behind it. The release gate holds the list at zero.

- #435 — the uniform 64-bit wire: ids, lengths, counts, indexes; the framing
  header with the form version; the id table; the enum kind; flags
  bit positions in the build version; the reserved-id refusals.
- #434 — the length-framed rule for future kinds.
- #432 — the cook triple, and the byte-order sentences in five places.
- #396 — table `was`; string, bytes and flags defaults in the baseline and
  the build version.
- #441 — the retired-names ledger.
- #442 — `was` for variants and arms.
- #443, #444, #445 — range facts, `was` misuse, the baseline nudge.
- #446 — the evolution table's golden.
- #439 — the standard's own contradictions on `T`→`*T`, the flags row,
  writer misuse, and the rest of that read.

## How this page is kept

Every fact stated here is one of three things: held by a golden or a negative
control in the tree, cited to the issue that will hold it, or wrong. There is
no fourth category, and finding the third is the reason to read the page
slowly. A pull request that changes any fact on this page changes this page
in the same pull request — the same rule the techniques register lives by —
and the whole-standard read is re-run before each major. When two sentences
in the tree disagree, the one with the golden wins, and the other is a bug
in prose.
