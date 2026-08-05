# Road to 1.0 — the decisions, as they landed

*2026-08-05. The audit ran in the afternoon (three audits, eight dossiers, adversarial
verification — see SPEC.md draft-5 changelog); Glenn answered the same evening, item by
item, in the main channel. This file is now the record of those answers; the spec carries
each one at its section with his words. The original decision-list form of this file is in
git.*

## Decided by Glenn, 2026-08-05 (his words at each spec site)

- **Strings are byte strings** — §4.7. *"fine"*
- **Wide strings + int_relative deferred to the delta pass** — §4.10. *"OK"* / *"yes,
  int_relative is used when doing delta encoding, which is later"*
- **Root/packet marker: DISCARDED** — never his ask. *"don't know what root packet marker
  is. did not ask for it."*
- **`E.Max` enum references** (his capitalization) — §4.2. *"yes, but prefer Team.Max"*
- **Constants are platform-uniform** — §4.2. *"yes, because server and clients will be on
  different platforms, but must always be able to communicate."*
- **Sentinel-terminated collections deferred** — §9 q10. *"sure, that's out of scope"*
- **Hash procedure (basename-delimited)** — §3.1. *"yes."*
- **Canonical enums only** — 0 = None, dense [1,max], explicit/sparse values declined
  permanently — §4.2. *"canonical enums are in the form 0 = none, [1,max], dense
  non-sparse values."*
- **The enum family is type-level**: `enum` (with-None) / `enum_index` (value − 1 on the
  wire, [0, Max−1]) — §4.2. *"suspect it's better done as a type"* → *"#3 is confirmed."*
- **`enum_flags`**: uint64 storage, one field per bit, up to 64 — §4.2. *"yes, we can and
  should support flags too."*
- ~~`quat` is built-in~~ — **superseded the same evening by the type-tags decision (see
  the addendum below)**; what survives of this row is the unit-length fact itself:
  *"quaternions are used to represent rotation, therefore always unit length."* — now the
  reason a rotation field's quantize bound is written `max = 1`.
- **Tag order is SORTED BY NAME** — §4.8 (overruling my declaration-order proposal).
  *"alphabetical order is better, because it's independent of formatting and cut & paste
  stuff."*
- **schemafmt in v1** — §7.4. *"there is a canonical syntax form from which we generate
  the hash"*
- **Doc comments deferred** — §4.1. *"don't care. leave for later."*
- **The v1 scope** — §1. *"goal for v1 is schema fully defines generated code for the
  constants, enums, types, messages, object definitions. the delta serialization is out
  of scope of v1."*
- **DISCARDED as a family** — replication-policy knobs, interop/adoption asks, the
  Equal/Checksum/Print helpers: *"those concepts are from [the external engine]. Discard.
  we don't use them. our networking techniques are stronger than [theirs]"* — *"built
  around deltas and snapshots, not around priority accumulators […]"* No external-engine
  name mentions remain in this repo's tracked files (grep-verified; bracketed elisions in
  his quotes are the sweep applied to the quotes themselves; the full sentences are in
  SPEC.md §9 q14).

## Landed outside the spec the same day

- serialize.cs: **AGPL-3.0** (`baa2f9b`), **PUBLIC**, CI workflows landing.
- Space game: the interpolation dot-product bug, the two wrong-enum asserts, the
  duplicate `#define`, and the turret_index width divergence — **all fixed and pushed**
  (`05f134c2`, `114ea1bc`); turret wire change free because turret is unused (*"turret is
  currently unused... fix now."*).

## Remaining before the corpus review

1. The corner-rule pins, presented one by one for his check (his ask).
2. `files.go` — his call once the explanation lands (his own tooling file; Readdir order
   feeds the flatbuffers protocol id; fix is one sort line, redeploy-together).
3. **Then: regenerate the full example corpus** — all aspect files under the final
   language (`quat`, `enum_index`, `enum_flags`, `E.Max`, corrected thrust/health forms)
   plus `object Missile` / `DynamicProp` / `Turret` from the measured inventories — and
   he reviews the `.schema` files. *"Before we implement, let's land all design
   questions, generate final language example source code in *.schema in repo and i'll
   review."*

## Addendum — the type-tags decision (2026-08-05, evening, after the corner pins)

The last design question opened and closed the same hour: does the language pre-define
composite types? **No — DECIDED.** Three candidate designs died on the way (a built-in
`quat` keyword; a `type Quat [rotation]` behavior attribute; a compiler-known class
vocabulary), each on Glenn's direction that the language stay general and pre-define no
types. What landed (SPEC §4.2, Type tags, his words throughout): **types are entirely
user-defined and user-tagged (`type Vec3 [vec3]`); tags are inert in v1; the delta pass
CLAIMS tags and assigns meaning and actions** — "tag -> specifying behavior." His
arguments, all banked verbatim: types are per-game, not general; a first-class set
sufficient for any game is impossible; and the fixed-point migration would make any
hard-coded float types immediately obsolete, while under user types it is a
re-declaration in his schemas, not a language change. v1 consequence: no interpolation
generation at all — v1 builds the structs and Quantize/Unquantize; Interpolate() stays
hand-written until claiming. Corpus updated to match (tagged Vec3/Quat in Types.schema;
rotation fields carry the explicit `max = 1`).
