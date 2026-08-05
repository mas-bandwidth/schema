# schema × Atlas — what adoption would need

Notes for Glenn from an evaluation of the schema spec (draft 4, 2026-08-05)
against Atlas, Patrick's C++20 game engine. We read the spec seriously as a
candidate for our network replication layer and, longer-term, config/asset
definitions. This writeup is the concrete list: what schema would need to
grow for Atlas to adopt it, ordered by how much each item matters to us.

Context in three sentences, so the requirements make sense. Atlas is
C++-only (macOS/iOS/Windows/Linux; Metal/D3D12/Vulkan), with its own
vocabulary types (math, strings, containers, allocators) and a house rule
that generated or library code never leaks `std` into public surfaces. It
already has a runtime reflection substrate (registered type descriptions
driving text serialization, a canonical binary params codec, a generic
property-inspector editor, and layered asset composition), so schema would
land as the *declaration language and code generator* over an ecosystem that
is otherwise reflection-driven. Our replication layer does delta-snapshot
replication where the wire form itself is the dirty detector: state is
re-encoded and byte-compared against a shadow copy, so canonical encoding
isn't an optimization for us, it's the correctness contract.

## 1. Open extension — the engine-vendor problem (the make-or-break)

This is the one structural gap. Everything else on this list is an addition;
this one runs against current design decisions.

Atlas is an engine, not a game: we ship a core that games extend with types
the core has never seen. The live example is materials — the engine defines
the concept of a material and the fields every material must expose
(display name, alpha mode/cutoff, double-sided, base color); each shading
model (engine- or game-provided) extends that with its own parameters, and
engine systems read the base fields off any material generically while
editors and serializers see the full derived surface. This is exactly where
flatbuffers fails us — no table inheritance, unions closed and owned by the
schema author — and schema v1 currently inherits the same failure. For
adoption we would need:

- **Cross-unit composition.** "One unit, one package, all files compiled
  together" is the blocker: the engine must be able to ship schema units as
  libraries that a game's unit imports and extends. The natural model is
  that the *composed application* is the closure point — the place where
  the full set of types is finally known — and set extraction, discriminant
  generation, and the protocol id all happen there, over engine + game
  declarations together.
- **Base types.** A declared parent whose fields prefix every subtype, so
  generic code can operate through the base declaration. Nested-struct
  composition is not a substitute: it wrecks the flat authoring surface
  (authors and editors should see one field list, not a wrapper), and it
  gives generic code no is-a relationship to work with.
- **Open vs. closed subtype identity, chosen per set.** This is the critical
  distinction. A closed set — one team, one game, everything in the unit —
  is well served by what you have: dense extracted ids, minimal-bit tags.
  An open set needs identity that survives independent extension: a
  type-name/declaration hash, with dense tags (if wanted for the wire)
  derived at the closure point or negotiated per session, never part of the
  declaration. Two current mechanics are actively hostile to open sets and
  would need to be scoped to closed ones: sorted-by-name discriminant
  numbering (a game adding a message named `Aardvark` renumbers every
  engine message's tag) and minimal-bit tag width over `[0, count]`
  (crossing a power of two reframes every message).

If schema had this, it would be materially ahead of every incumbent — this
is the failure they all share, and it's the thing an engine vendor actually
needs. Worth saying explicitly: your horizon already gestures here
("all the object types … defined fully in the schema language"); the open
question is whether the object set is closed at Space Game's build or open
at the layer where an engine's customers live.

## 2. Mapping onto existing vocabulary types

"schema owns the types it serializes" is workable for leaf protocol structs
and wrong for an engine's vocabulary. `position Vec3` must generate a field
of *our* `Vec3` — not a schema-emitted twin — or every boundary crossing
becomes a copy and every generated struct is a stranger to the rest of the
codebase. Same for strings and dynamic arrays: our `String`/`Array<T>` are
allocator-aware; generated C++ that owns `char[N]`/`T[N]` + count works for
wire scratch structs but not for types that live in the engine. Concretely:

- **An extern-type declaration**: a schema type declared as "provided by the
  host, with this wire encoding" — the schema knows its fields for encoding
  purposes, the generated code uses the host's type and includes the host's
  header. (Your per-target storage table is already most of the machinery;
  this is a per-type override of it.)
- **A container/string policy hook** per target, so bounded collections can
  generate against the host's containers instead of fixed arrays where the
  host asks for it.
- Generated-code style must be configurable enough to pass a strict house
  style: no `std` includes in emitted headers, no exceptions (your
  bool-return C++ idiom is already right for us), naming conventions.

This is the difference between "schema can define our packets" and "schema
can define our types," and the second is where the value is.

## 3. Beyond the codec: the metadata and the other generators

For replication, the wire codec is the smaller half of a schema entry. The
other half is per-field and per-type metadata driving *other* generated
artifacts. Your attribute mechanism is the right attachment point and your
horizon names most of this; adoption needs it real rather than horizonal:

- **Per-field:** quantization (your `min`/`max`/`resolution` compressed
  float covers it), interpolation mode (lerp vs. snap vs. angular),
  smoothing eligibility (which fields a client-side prediction correction
  eases across vs. snaps).
- **Per-type:** replication policy — priority weight, despawn policy
  (reliable destroy event vs. TTL), atomic-coherence grouping. These need a
  home even if the replication engine, not the codec, consumes them.
- **Generated interpolate** (your `ShipData_Interpolate` direction) is the
  first non-serialize generator we'd actually use — our hand-written
  interpolate functions are per-field metadata transcribed into code, which
  is exactly the boilerplate class the language should eat.
- **Canonical encoding as a documented guarantee.** Equal post-quantization
  values must produce identical bytes — deterministically, forever — because
  our dirty detection is a byte compare of encoded forms. Your generated
  code surely has this property; we'd need it stated as a contract, since
  for us it's correctness, not hygiene. (Bitpacking is fine by this test —
  bitpacked canonical bytes compare the same.)

## 4. The schema as a machine-readable artifact, not only generated code

Atlas drives editors and tools from runtime type descriptions — a property
inspector renders and edits types it has never seen, with no generated code
in the loop. "All knowledge lives in the generated code on both ends" is
the right trust model for the wire, but adoption would need the compiler to
also emit the *resolved schema itself* in a stable machine-readable form
(you already treat the IR's canonical encoding as a frozen public contract —
exporting it is most of the work), and ideally optional emission of runtime
reflection tables for the C++ target. Without this, schema-defined types
become second-class citizens in schema-driven tooling — invisible to the
editor that every other engine type participates in.

## 5. The protocol id should hash meaning, not bytes

File hashing moves the id on a comment or whitespace edit. You've accepted
that as fail-safe, and under ship-together it is — but it also trains people
to expect id churn, and it composes badly with §1 (an engine shipping schema
units can't have a doc-comment fix look like a protocol break to every
downstream game). Hash the resolved declarations: names, field order, types,
ranges, enum vocabularies — the things that change what bytes mean. Your
draft-1 concern (name-stripped hashes allowing crosswise reads) is answered
by keeping names *in* the semantic hash; it was canonicalizing away too much
that was the bug, not semantic hashing itself. With composition, the id
belongs to the closure (the composed app), computed over everything
reachable — which also resolves your unused-helper-moves-the-id wart for
free.

## 6. Smaller items, quickly

- **Explicit enum values and flag enums** (your open question 7): needed —
  our real enums pin values, and bitmask enums are pervasive. The
  whitespace-separated variant form is the only thing in the way.
- **Doc comments carried into generated code and into the exported schema**
  (your open question 5): yes from us — they're the inspector tooltips and
  the header docs.
- **u8/u16 storage, f64, byte strings**: all as decided — no notes.
- **Wide strings, relative ints**: agreed deferred; no need in sight here.

## What we don't need (so you know what's *not* driving this)

- **The other three languages.** C++ is the only target that matters to us.
  Everything multi-language — the conformance matrix, target-name
  reservations biting identifier choices, per-language dispatch idioms — is
  cost with no benefit here. None of it needs removing; it just can't tax
  the single-target path.
- **Wire versioning.** Ship-together with an id gate is our model too;
  where we eventually need mixed-version tolerance, that's per-boundary
  policy in our own layer, not wire machinery.
- **Bitpacking as a selling point.** Welcome, not required — our records
  are small and byte-aligned today and the canonical-bytes contract is what
  we actually depend on.

## The one-paragraph summary

The codec generator, the attribute surface, the quantization vocabulary,
and the id discipline are all things we'd happily use. What decides
adoption is §1: whether schema's world can be open — engine units imported
and extended by games, base types, and per-set open identity (type hash)
alongside closed (dense id) — and §2: whether generated code can speak an
existing engine's types instead of owning its own. Those two are where
every incumbent schema system has failed us, flatbuffers loudest; a schema
language that got them right would be adoptable here and, we suspect,
anywhere.
