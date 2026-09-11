# Bill: the fixed form reads backward, never forward

Purpose, in Glenn's words: "this is not perfect and does not do everything, but it does work, and extends
from a pattern coded by one person that is a footgun, to something that could work on a larger team."

Glenn, 2026-09-10, in Rowan's window, after walking three cases (an old reader sent a new enum variant, an
old reader sent a longer array, an old reader sent a layout whose definitions moved): "The only correct is
reject." "Newer versions of the fixed table should be able to read OLD versions." "But old versions CANNOT
read new versions, and complain loudly and refuse. Nothing else makes sense." "You can take an enum and widen
it, but you cannot narrow, or incompatible."

This is step 1 of THE PROCESS (the bill). Nothing in the reference moves until it has been read cold. The
algorithm doc (§5), the oracle, the matrix and the properties follow it, then the reference, then the ports.

## 1. The rule

A reader accepts a file only if the file's layout is **older or equal** to the reader's own. A file whose
layout is newer than the reader **refuses by name, loudly, before any record is read**. There is no partial
read, no clamp across versions, no value landed as `None` because the reader had no name for it.

"Older or equal" is decided once per peer at plan time and is a property of the schema pair, never of a
record's values. A pair either reads or it does not.

## 2. What "older or equal" means, per thing that inputs into a table

The table's own law, additions and deprecation only, extends to every definition that inputs into it.

| definition | the writer's may be | refuse when the writer's is |
|---|---|---|
| a field | present on the reader by name, or deprecated on the reader | a name the reader lacks (the writer is newer) |
| an array bound, a string or `bytes` size, a constant behind either | at most the reader's | larger |
| an enum's variants, a union's arms | a subset of the reader's, by name | a name the reader lacks |
| an enum's ordinal width, a union's tag width | at most the reader's | wider |
| a ranged scalar's bounds | inside the reader's | wider on either end |
| an integer or float width | at most the reader's, same signedness ladder | wider, or a different kind |
| a field's kind | the same | different |
| field order | a prefix of the reader's: fields are ADDED AT THE END, deprecated in place, never modified or removed (Glenn) | a field out of order, changed, or missing where the reader has one |
| a fixed-size array `[N]` | N at most the reader's (the reader defaults the rest) | larger |
| an enum-keyed array `[Enum]T` | its enum a prefix of the reader's | the enum has a name the reader lacks |
| `bits(N)` | N at most the reader's | larger |
| `fixed(I,F)` / `ufixed(I,F)` | I at most the reader's, F equal | I larger, or F different (the scale moves, incompatible) |
| an optional | `T` where the reader has `?T` (landed present) | `?T` where the reader has `T` |
| a nested table or type by value | the table's own law, recursively: fields added at the end, deprecated in place (Glenn) | a field modified, removed, or inserted elsewhere |
| `flags` | bits a prefix of the reader's: new flags added, old flags never removed (Glenn) | a moved or missing bit |
| a compressed float | any quantization (it rides as the float) | never |
| the `fixed` keyword | the same | a variable table where the reader has a fixed one, or the reverse: a different form, not a version |

| a range or constraint | absent on the writer where the reader has none, or inside the reader's | ADDED where none was (old values would clamp); a constraint added to a text field |
| a text kind (`string`, `wstring`, `bytes`) | the same | any change between the three (old bytes stop being valid under the new content rules) |
| an array's shape (`[N]`, `[..N]`, `[Enum]`) and its key enum | the same shape and key | a shape change or a swapped key; an element type follows the widening ladder, an element narrowed is refused |
| a reader-side limit a table declares (a record count, a batch size) | at most the reader's | smaller; where the limit is a compiler flag it is outside the law and the doc says so. **RESERVED: no table spelling of one exists yet** — the digest's `'L'` row is empty (ALGORITHM §5.2, pinned by `TestTableFixedDefinitionsDigestLReservedUntilALimitExists`), and `--fixed-record-limit` is a flag |
| a deprecated field | still written, still in its place, read on every plan | leaving the layout (that is a removal); undeprecating (one way, §12.3) |
| a rename | through `was` | without it (the baseline's identity is by name, the fixed wire's by position) |

**The invariant that makes widening safe** (Glenn: "widening with default values is what saves us here"):
every widening is defined by the default it fills. A new field takes its default; a grown array's new slots
take the element default; `T` to `?T` lands every old value present; a widened range, width or list changes
nothing an old value held. A change with no default to fill is not a widening, and that is the test for any
case not in this table.

Glenn: "wstring/strings/bytes can be widened only, not narrowed, because a narrowed string/array cannot read
the old." The principle behind every row: a newer reader must be able to hold every value an older writer
could produce, exactly. Where widening keeps that true it is allowed; where it cannot, the change is
incompatible and the baseline (§6) refuses it at commit.

Widening an int or a float, or an enum's ordinal width, is a read: the reader lands it exactly. That is the
one asymmetry the bill keeps, and it is Glenn's: "you can take an enum and widen it, but you cannot narrow."

## 2a. The closure rule

Glenn: "fixed tables must only allow other fixed tables to be included in them, and not recursively include
themselves." A fixed table's closure is a TREE of fixed things: every table or type it reaches by value is
itself declared `fixed` and carries its own locked layout under §6; a pointer, a map, or an unbounded array
makes a table variable and is refused in a fixed closure, so a fixed table cannot reach itself by any path.
The compiler holds both halves today (a plain nested table is a closure break; `check_fixed.go` refuses the
variable kinds); the bill makes them the law and the lock records the closure.

## 3. What the reader does with an older file

- a field the reader added since: the reader's declared default;
- a field the reader deprecated since: READ into its slot by every plan, identity included, and ignored by
  the application; no counter moves for it (§12.3 — one answer for one field on every version);
- a narrower int or float: widened, `COUNT widened` as today;
- everything else: copied, the plan being the identity plan whenever the hashes agree.

## 4. What the reader does with a newer file

Refuse, at plan time, by name. Proposed name: **`layout_newer`**. "Loudly" means the refusal carries THE FILE'S
LAYOUT HASH AND NOTHING ELSE (§12.4): a stranger's layout is never parsed, so the reader has no field, bound
or variant to name — the hash is what the operator looks the writer up by, in the lock's lineage. It rides
the report beside the reason (`layout_hash` where the language has a field for it, the reason's text where it
has text). No counter moves; REFUSE stays total.

`layout_newer` sits beside `newer_form` (a form byte past the reader's) and the seven layout refusals of
§1.1, which are for a layout that is not a layout. `layout_newer` is for a layout that is a layout and is
not the reader's to read.

## 5. What leaves the reference

Everything that existed to read a newer file with an older struct:

- the count clamp across bounds (`count` lands the writer's count; a writer's bound larger than the reader's
  is `layout_newer`, so the clamp can never fire on a legal peer);
- the ranged-scalar clamp across versions (same reason);
- the remap of a variant the reader lacks to `None` (it is `layout_newer`);
- the drop-and-count of a field the reader lacks (it is `layout_newer`; a field the READER deprecated is not
  dropped either — it keeps its slot and every plan lands it, §12.3).

What stays, unchanged: every hostile check. A forged count past the WRITER'S OWN bound, an ordinal past the
writer's own variant count, a tag past the writer's own arm count, a bool byte that is not 0 or 1: those
remain the reader's straight-line validation on every build, and they still land `None` or clamp and
`COUNT clamped`, because they are a lie about the writer's own layout, not a version.

## 6. The monotone law, guaranteed by the lock

**Ruling, Glenn 2026-09-11 02:12Z: "lock."** `schema.lock` is the law's home: it holds the monotone law
(SPEC §2.10 already holds the list rows for fixed tables, with tests in `internal/lockfile`), the lineage
and the floor (§6a, §6b). `tables.baseline` stays the save-game projection of §18 and gains nothing from
this bill. The compiler refuses to generate against a lock the schema contradicts; CI runs the same check.

Glenn: "how do we guarantee ONLY widening is allowed (enums can have entries added at end, no shuffling of
entries for meaning, new entries at end...)". The lock records the last shape and the baseline refuses any
commit that is not a widening of it. Per definition:

| definition | the lock records | the baseline refuses |
|---|---|---|
| enum, union | the variants or arms IN ORDER, each with its name hash | insert not at the end, reorder, rename, remove; deprecate is allowed and keeps its place |
| array bound, string or `bytes` size, a constant behind either | the number | a smaller number |
| ranged scalar | both ends | either end moving inward |
| int, float, ordinal or tag width | the width and signedness | narrower, or the other ladder |
| a field's kind | the kind | a different kind |
| a table's fields | the list | anything but append and deprecate (today's law) |

The compiler refuses to generate against a lock the schema contradicts; CI runs the same check; the refusal
names the definition and the rule. So the law holds on every machine, and no reader ever meets a pair that
is neither older nor newer.

**What appending buys on the wire.** With variants and arms append-only, the writer's ordinal IS the
reader's ordinal: no remap table, no lookup per record; an enum lands as a `copy`, or a `widen` when its
width grew. The plan-time check for an enum is "the writer's list is a prefix of mine", one comparison per
enum, and the same for a union's arms. Faster than the remap by name, and nothing to get wrong.

**What append-only everywhere buys at plan time.** With every list a prefix of the reader's, the plan check
is one shape at every level: walk the writer's layout and the reader's side by side; the writer's must be a
prefix of the reader's entry by entry, widening where a width grew, defaulting the reader's tail. No matching
by name at plan time, no remap tables, one comparison per entry. The compiler that reads old files gets
smaller than the one that exists.

## 6a. Versions: the hash is the version, the lock holds the law

There is no version number to increment. A fixed table's layout hash is derived from its content, so any
change to a definition that inputs into it changes the hash by itself, and "older or equal" is decided
structurally at plan time (§2), never by comparing numbers. What `schema.lock` needs is the SHAPE: one entry
per fixed table carrying its layout hash and the layout itself, so that `tables.baseline` can hold §6's
monotone law against the last locked layout at commit time. The hash says which version a file is; the lock
says the versions only ever grew. A human-readable number beside it for release notes is allowed and nothing
on the wire reads it. (Glenn: "So you DEPLOY the backend with the new schema, and it reads old and new. But
nobody guarantees old ever reads new.")

## 6b. The floor, and what it buys: no plan compiler at run time

Glenn: "i would consider, only versions in the past to x would be supported, where current version might be
y." The lock keeps the LINEAGE per fixed table: every layout the table has had, in order, each with its
hash and its layout bytes. A floor is one number per table: the reader accepts lineage entries from x to y
(its own) and refuses older ones by name, `layout_unsupported`.

What that buys: the set of layouts a reader accepts is FINITE AND KNOWN AT BUILD TIME. So:

- the plan for reading each supported version is compiled AT BUILD TIME by the compiler, from the lock,
  and shipped as static data, one plan per version, the identity plan for y;
- the RUNTIME PLAN COMPILER GOES. A file arrives; its hash is looked up in the table's supported set; the
  file's layout bytes are compared to the known layout bytes for that hash; the precompiled plan runs;
- hash not in the set: refuse (`layout_unsupported` if it is in the lineage below the floor, `layout_newer`
  if the reader has never seen it); layout bytes differ from the known ones under that hash: refuse
  (`layout_malformed`, a lie about a known version);
- the seven layout refusals of §1.1 collapse to one byte comparison, because the reader NEVER PARSES A
  STRANGER'S LAYOUT to decide whether it is well formed. No validation walk, no depth bound, no size
  arithmetic on untrusted input. Faster, and much safer, than what exists;
- the branch case (§8a.1) is settled by the lineage: a merge appends both sides' layouts, and both are
  supported.

The floor's syntax is Glenn's to name (§9). Without a floor, x is the table's first layout.

## 7. The deployment rule

Readers first, always. A writer ships only after every reader that will meet its files has, by a margin you
can roll a reader back across (§8a.2). Getting the order wrong is a `layout_newer` refusal at the first
file, not a truncated array in production.

## 7a. What the versioning does, and what a person still does

Glenn: "in practice this is the delta between the live clients and the backend"; "We did stuff like, if
version is > x, read these vars, otherwise set to default, which i think is what you are doing here, so it
SHOULD work"; "it's not 100% 'i don't have to think at all the versioning does this for me'."

The versioning does the bytes: a file is never read wrong, and never read silently wrong. The compiler writes
the `if version > x` chain from the lineage, one plan per supported version, the file's hash picking the
branch. A person still does five things: append and never modify (the baseline catches the slip); choose a
default once, because it defines every old file forever; ship readers before writers, by a margin a reader
can roll back across; set the floor to the real delta between live clients and the backend; start a new
table when the old one has grown past sense, and retire it when nothing live speaks it.

## 8. What changes on the board (#876)

- E1 (unknown field): becomes `layout_newer`; the deprecated-field drop is RETIRED rather than carded — the
  slot is read by every plan and counts nothing (§12.3).
- C1/C2 (count clamp): hostile half stays (forged past the writer's own bound); the cross-version half goes.
- C7 (ranged scalar clamp): hostile half stays; cross-version half goes.
- C9/C10 (tag/ordinal past top): hostile only, as today.
- Card 18 (an unknown variant counted nowhere): superseded; it refuses.
- The seven layout refusals, W10, W6, W16, the byte pins against the reference, FU in the oracle: untouched.
- New items: `layout_newer` asserted by name on every leg for each row of §2; the baseline's monotone law
  with a fixture per row; the deployment rule in the doc's §5.

## 8a. What breaks it, and what the bill does about each

Glenn: "Is there any case you can find that breaks this approach? The widening approach to versioning?
Adding but never removing?" Three.

1. **Two branches both append.** A hotfix appends field A while main appends field B; each commit passes
   its own baseline; after the merge one is reordered, and a reader that matches BY POSITION refuses every
   file the pre-merge hotfix build wrote. Append-only assumes one linear history and git does not give one.
   **Ruling in this bill:** the baseline enforces append-at-end on each branch (§6, the discipline), but the
   READER accepts a writer whose definitions are a SUBSET of its own BY NAME, each compatible under §2, in
   any order (the tolerance). Fields, variants and arms keep the remap by name that exists today, with the
   forward-reading half removed; when the writer is a true prefix the remap is the identity and costs
   nothing. §6's "what appending buys" is therefore the common case, not the rule.
2. **Readers cannot roll back.** Once a newer writer has produced files, the previous reader build refuses
   them by name. That is the contract working, and it makes a reader rollback a data event. Rule: writers
   ship after readers by a margin you can roll back across; a build that must go down migrates its files
   first. Stated in §7.
3. **Growth has no reset.** Deprecated fields ride forever, every record carries them, the record-size and
   leaf caps eventually bind, and a default, once chosen, can never change (it defines what every old file
   means). None of it breaks a read; it is the price. The escape, in Glenn's order: "You would just create a
   new table if it got too large. Then deprecate the old, clean out the cruft, and upgrade the backend first
   to read the new table and the old. Deploy it, then deploy the client." A new table is a fresh lineage
   with its own lock entry; the old one stays readable for as long as its files exist, "and once no clients
   are live talking the old table, remove the old table that is deprecated." The layout hash is the
   per-table version; the new table is the explicit bump (Glenn: "you can see in network next how I would
   do this with per-table versions").

## 9. Open, for the cold read

- The floor's syntax: on the table (`fixed table Foo | floor = 3`?), in the lock, or a compiler flag per project.
- Whether §6b (build-time plans from the lineage, no runtime plan compiler) is the shape, which retires §1.1's
  seven refusals as runtime code (they stay as the lock's own validation of what it records).
- Whether a writer's *deprecated* field (older writer, still writing it) needs any rule: the reader is newer
  and has the name, so the slot is read like any other and the application ignores it (§12.3); it seems to
  need none.

**Closed since, by the rulings below.** Whether a deprecated field counts under `unknown`: it does not, and
it is READ on every plan (§12.3). Whether `layout_newer` carries the offending entry as an index or as text:
neither — it carries the hash and nothing else, because the walk that could have named an entry is gone
(§12.4).

## 10. The variable table: HELD, for a separate decision (Glenn: "I don't know yet. Hold this for later.")

Glenn: "I think the reads backwards rule is going to be required for the variable table too." The same law
holds; the difference is where "newer" is detected. Form 1 has no layout, so today it finds out PER RECORD
(an unknown field id, an unknown variant, a count past the bound), each tolerated and counted (SPEC §4). To
refuse forward as form 3 does, form 1's header carries the writer's closure hash, and the decision is one
comparison at the top of the file, before any record. §4's tolerance was form 1's selling point and it is
the same footgun under a friendlier name. This is its own bill: it changes every leg's form-1 reader, the
wire header, and the packet form's model, and must not ride in on the fixed table's ruling.

## 11. The operator's cold read, and the rulings it forced (2026-09-11 02:35Z)

A cold reader on Fable read the three documents as the person who deploys this over two years. Nine
findings changed contract sentences. Glenn was asleep; under his "run this to completion without me" each
ruling below is a default, recorded on #898, and his to reverse.

1. **A fixed table flows in one direction.** Writers ship after readers, and a reader never rolls back past
   a writer that has shipped: a backend that must go down goes forward instead. (The reader's sentence,
   adopted whole.) There is no migration mechanism, because writing a newer file as an older layout is the
   forward read this bill removed. A table that flows the other way (a backend writing files clients read)
   is its own table with the roles reversed: for it, the clients are the readers and ship first.
2. **Readers first means readers at one hundred percent.** During a rolling reader deploy a new writer's
   file meets old reader instances and refuses; the refusal is safe to retry against another instance and
   carries no state, but the rule is that writers ship only after the last reader instance has.
3. **The lineage is a set, not a line, and the merge is judged against both parents.** Two branches append
   different fields; the merged lock's lineage is the UNION of both parents' lineages; the merged schema
   must widen EACH parent under §2 by NAME (§8a.1), and each parent's list must be a subsequence of the
   merged list (append-only per branch, any interleaving). §5.1's "prefix by (name, position)" is
   corrected to that. `schema lock` at a merge commit takes both parents' locks as `old`.
4. **Retiring an entry retires every entry below it, and the floor stays ONE NUMBER.** `schema lock --retire
   Table@<hash>` marks a lineage entry retired (a permitted non-append edit, recorded with a reason), and the
   floor a build carries is `1 +` the highest retired index — an INDEX CUT into a lineage that is already in
   order, oldest first. A retired entry stays in the lineage forever, so LOAD still finds its hash and still
   answers `layout_unsupported` rather than `layout_newer`: the hash is in the lineage, below the floor. There
   is no retired-hash array to emit beside the supported ones, which is what §6b bought — ONE NUMBER PER
   TABLE, and the supported set an interval decided by one comparison. A layout that never shipped is retired
   the same way. The two names point the operator in opposite directions (ship the reader, or upgrade the
   client) and both stay.
5. **A table is retired, never removed, until nothing live speaks it.** `schema lock --retire Table` marks
   the whole table retired; its lineage stays; the schema may then drop the declaration, and §5.1's
   "fixed removed" refusal applies only to an unretired table. "Remove it" in §8a.3 means remove the
   declaration, keep the lineage.
6. **What earns a lineage entry.** A change of layout hash AT COMMIT, judged by the check that runs there;
   local iterations before a commit collapse to one entry. A warning names a lineage past thirty-two
   entries per table; the remedies are the floor and the new table.
7. **The lock must hold what COMPILE reads.** Today `internal/lockfile` holds one layout per table as a
   text projection, no lineage, no floor, no layout bytes, and a hash that is `TableWireId` over its own
   text rather than the wire's `TableFixedLayoutHash`. Step 2 of #898 gives the lock a lineage section per
   fixed table: one entry per layout with the WIRE hash and the layout bytes, a retired mark and a reason;
   the wire hash keys `R.known`. Defaults, deprecation marks and the closure stay where they are.
8. **A rendering-version bump salvages, never deletes.** The lock's "delete it and write it again" remedy
   would wipe the lineage fleet-wide. Once the lock holds history it adopts the baseline's salvage model
   (SPEC §18.4): the new renderer reads the old lineage and rewrites it, entry for entry, bytes unchanged.
9. **Files older than the first lock do not exist by rule.** A fixed table's layout is locked before its
   first file ships; the first lineage entry is the first layout. A file whose hash matches nothing is
   `layout_newer`, and the doc says why that is the right name even for a save game: the writer is one the
   reader has never locked.

The cost the reader measured: about `4 + 17E` bytes of layout plus roughly 25 bytes per plan op per
supported version per table per leg; at five hundred entries and thirty versions, a quarter of a megabyte
of layout bytes per table per leg in generated source. Java's 64 KB static-initializer limit and JS/Dart
bundle size bite first; the legs carry layout bytes as a resource or a string constant where the language
needs it. Bounded by §11.6 and the floor.

## 12. The implementer's cold read and the lock survey: the rulings (2026-09-11 02:45Z)

A second Fable reader read as the implementer who is blamed for a wrong read; a third child surveyed
`internal/lockfile` row by row. Defaults, recorded on #898, Glenn's to reverse.

1. **The lock's rule becomes MONOTONE, not equal.** Today the lock refuses every narrowing AND every
   widening with one sentence ("keeps its width"), `schema lock` refuses the relock, and `bits(N)` is
   recorded nowhere. Step 1 of #898 rewrites the comparison to §2's direction per row and records the facts
   it lacks: each bound and capacity as a NUMBER, `bits=N`, `fixed`'s I and F, an int's width and
   signedness ladder, a reader-side limit, and judges every constant by its EVALUATED use (a constant
   behind `min =` widens by going DOWN). SPEC §2.10's "nothing is widened" is superseded by this bill.
2. **"Kind" means the family.** WIDENS checks the ladder BEFORE kind equality: an int into a wider int of
   the same signedness, `f32` into `f64`, an enum's or tag's ordinal width into a wider one, `bits(N)`
   into a wider storage, are widenings although the wire kind byte changes. Every other kind change refuses.
3. **Deprecation is one way, and a deprecated field is READ on every plan.** §2.10's reason stands
   ("what would come back is not data"): undeprecating is refused; §2's row is corrected. And a deprecated
   field keeps its slot and is landed by every plan, identity included; nothing is dropped and `unknown`
   does not move for it. The application ignores it. One answer for one field on every version.
4. **`layout_newer` carries the hash and nothing else.** The refusal walk over a stranger's layout is
   REMOVED from LOAD: no parse, no names, no depth bound, no size arithmetic on untrusted bytes, anywhere.
   The seven §1.1 checks are the lock's validation of what it records and the oracle's validation of the
   corpus; at run time a known hash is a byte comparison and an unknown hash is a refusal. The design's
   OLD-REFUSES-NEW column asserts the hash, not a name.
5. **The plan carries the writer's bounds, and the hostile pass is per plan.** For lineage entry x, the
   plan's `count`, `ordinal`, tag and range checks use x's bound, x's variant count, x's arm count and x's
   range (the writer's own), never the reader's; a forged value past the WRITER's bound clamps or lands
   `None` and counts, exactly as §4.5 and §4.6 say, with the bound taken from the plan, not from the
   reader's type. One pass over storage per plan, laid down at build time.
6. **A grown `[N]` fills the ELEMENT DEFAULT, never zeros.** The prefill recurses into the reader's fresh
   image (a nested type's own defaults, an element's own), so a reader-added field of a nested type and a
   grown fixed-size array both land what a fresh value holds. `[..N]`'s slack past the count is zeros.
7. **A grown ordinal width is a `widen`, and the guard compares at the tag's width.** An enum crossing
   255 variants, a union crossing 255 arms: the plan widens, and every leg's guard/arg lane is full width
   (card 14 on #876 becomes a rule: no byte lane anywhere).
8. **`T` to `?T` is a `present` op**, an unguarded constant 1 into the present byte, then the value; named
   as its own op so no leg invents it.
9. **LOAD keeps every framing and file check.** Form byte, header length, `20 + L`, the per-record hash
   (`no_layout`), `batch_too_large`, the ragged tail as `malformed`; the vacuous record-bytes clause is
   dropped; the retired hashes are emitted for `layout_unsupported`.
10. **PLAN can fail at build, so BASELINE has a cap row.** A widening whose plan for ANY supported older
    entry exceeds `plan_too_large` or the leaf cap is refused at commit, naming the entry; the remedy is
    the floor or a new table.
11. **The default row is in §6's table.** A changed specified default is refused (SPEC §18 was built for
    it); it was in prose only.
12. **Smaller.** A bool or present byte not 0 or 1 is normalised and counts nothing (ALG §4.5 stands; the
    hostile list is corrected). `unknown` counts for a deprecated writer field that the reader lacks
    entirely: none, by ruling 3, so the counter is removed from PLAN. `bits(N)` refuses with text. The
    headroom sentence at ALG §1.1 (`>= key.children`) goes with C11. `ufixed` is in the spec's table. A
    compressed float's quantization never refuses. §6's law is judged on evaluated uses, stated. The bill's
    line saying the baseline "can hold" the law is deleted; the lock holds it (§6).

The implementer's verdict was "not ready to build from"; with §11 and §12 applied it is the bill a stranger
can implement, and the tests in `FIXED-FORM-VERSIONING-TESTS.md` are corrected to match (the refusal
carries the hash; `[N]` lands defaults; deprecated fields read).

## 13. The hash covers the definitions, not only the layout (2026-09-11 03:20Z)

The numbers fixtures (#907) found that a ranged scalar's bounds are not in the layout: `int32 | 0..100` and
`| 0..200` hash identically, so an old reader cannot refuse the widening and a new writer's 150 is silently
clamped; the same holds for a `flags` bit count (#906), `bits(N)`, `fixed(I,F)`'s split, and any reader-side
limit. §6a's sentence "any change to a definition changes the hash by itself" was false for those rows.

**Ruling (default).** The layout hash a file carries is the hash of the LAYOUT BYTES together with a
DEFINITIONS DIGEST: every fact of §2 that is not wire shape (each range, each flags bit count, each `bits(N)`,
each `fixed` I and F, each reader-side limit, in closure order). The digest is computed by the compiler from
the schema, recorded in the lock's lineage entry beside the layout bytes, and never rides the wire: the
hash binds it. A stranger with a known hash and different layout bytes is caught by the byte comparison; a
stranger with a known hash and the same layout bytes reads under the plan the reader compiled for that
hash, whose bounds are the ones the lock recorded, so no forged range can widen a plan. The layout format
does not change; `TableFixedLayoutHash` gains the digest as a second input; every lineage entry records
both.

Consequences for the green phase: `range_widen`, `flags_append`, `bits_grow` and `fixed_I_grow` refuse
`layout_newer` by hash once the digest is in; the floor API the fixtures assumed (`T##FixedLineage[]`
oldest first, `T##FixedFloor`, a test-only setter under a define) is accepted as the shape; the seven §1.1
malformations against a known hash return one name, `layout_malformed` (§12.4).
