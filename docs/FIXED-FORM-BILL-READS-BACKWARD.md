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
| a reader-side limit a table declares (a record count, a batch size) | at most the reader's | smaller; where the limit is a compiler flag it is outside the law and the doc says so |
| a deprecated field | still written, still in its place | leaving the layout (that is a removal); undeprecating is allowed |
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
- a field the reader deprecated since: dropped, counted once under `unknown` per plan (the reader chose not
  to need it; the count is information, not a fault);
- a narrower int or float: widened, `COUNT widened` as today;
- everything else: copied, the plan being the identity plan whenever the hashes agree.

## 4. What the reader does with a newer file

Refuse, at plan time, by name. Proposed name: **`layout_newer`**. "Loudly" means the refusal carries what
the reader could not hold: the first offending entry, as the reader can name it (a field name, a bound pair,
a variant hash), through the report's reason where the language has one and through the reason's text where
it has text. No counter moves; REFUSE stays total.

`layout_newer` sits beside `newer_form` (a form byte past the reader's) and the seven layout refusals of
§1.1, which are for a layout that is not a layout. `layout_newer` is for a layout that is a layout and is
not the reader's to read.

## 5. What leaves the reference

Everything that existed to read a newer file with an older struct:

- the count clamp across bounds (`count` lands the writer's count; a writer's bound larger than the reader's
  is `layout_newer`, so the clamp can never fire on a legal peer);
- the ranged-scalar clamp across versions (same reason);
- the remap of a variant the reader lacks to `None` (it is `layout_newer`);
- the drop-and-count of a field the reader lacks (it is `layout_newer`; deprecated fields keep the drop).

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

- E1 (unknown field): becomes `layout_newer`; the deprecated-field drop is its own item.
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

- Whether a deprecated field on the reader should count under `unknown` at all, or be silent.
- Whether `layout_newer` should carry the offending entry as an index into the writer's layout (portable,
  one integer) or as text (readable, per language).
- Whether a writer's *deprecated* field (older writer, still writing it) needs any rule: the reader is newer
  and has the name, so it reads or drops by the reader's own deprecation; it seems to need none.

## 10. The variable table: HELD, for a separate decision (Glenn: "I don't know yet. Hold this for later.")

Glenn: "I think the reads backwards rule is going to be required for the variable table too." The same law
holds; the difference is where "newer" is detected. Form 1 has no layout, so today it finds out PER RECORD
(an unknown field id, an unknown variant, a count past the bound), each tolerated and counted (SPEC §4). To
refuse forward as form 3 does, form 1's header carries the writer's closure hash, and the decision is one
comparison at the top of the file, before any record. §4's tolerance was form 1's selling point and it is
the same footgun under a friendlier name. This is its own bill: it changes every leg's form-1 reader, the
wire header, and the packet form's model, and must not ride in on the fixed table's ruling.
