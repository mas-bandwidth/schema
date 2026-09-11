# Bill: the fixed form reads backward, never forward

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
| field order | any | never (by name) |

Widening an int or a float, or an enum's ordinal width, is a read: the reader lands it exactly. That is the
one asymmetry the bill keeps, and it is Glenn's: "you can take an enum and widen it, but you cannot narrow."

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

## 6a. Versions: the hash is the version, the lock holds the law

There is no version number to increment. A fixed table's layout hash is derived from its content, so any
change to a definition that inputs into it changes the hash by itself, and "older or equal" is decided
structurally at plan time (§2), never by comparing numbers. What `schema.lock` needs is the SHAPE: one entry
per fixed table carrying its layout hash and the layout itself, so that `tables.baseline` can hold §6's
monotone law against the last locked layout at commit time. The hash says which version a file is; the lock
says the versions only ever grew. A human-readable number beside it for release notes is allowed and nothing
on the wire reads it. (Glenn: "So you DEPLOY the backend with the new schema, and it reads old and new. But
nobody guarantees old ever reads new.")

## 7. The deployment rule

Readers first, always. A writer ships only after every reader that will meet its files has. Getting the
order wrong is a `layout_newer` refusal at the first file, not a truncated array in production.

## 8. What changes on the board (#876)

- E1 (unknown field): becomes `layout_newer`; the deprecated-field drop is its own item.
- C1/C2 (count clamp): hostile half stays (forged past the writer's own bound); the cross-version half goes.
- C7 (ranged scalar clamp): hostile half stays; cross-version half goes.
- C9/C10 (tag/ordinal past top): hostile only, as today.
- Card 18 (an unknown variant counted nowhere): superseded; it refuses.
- The seven layout refusals, W10, W6, W16, the byte pins against the reference, FU in the oracle: untouched.
- New items: `layout_newer` asserted by name on every leg for each row of §2; the baseline's monotone law
  with a fixture per row; the deployment rule in the doc's §5.

## 9. Open, for the cold read

- Whether a deprecated field on the reader should count under `unknown` at all, or be silent.
- Whether `layout_newer` should carry the offending entry as an index into the writer's layout (portable,
  one integer) or as text (readable, per language).
- Whether a writer's *deprecated* field (older writer, still writing it) needs any rule: the reader is newer
  and has the name, so it reads or drops by the reader's own deprecation; it seems to need none.
