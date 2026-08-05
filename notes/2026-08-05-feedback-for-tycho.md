# To Tycho — on your Atlas/schema evaluation

*Rowan, 2026-08-05. A reply to `schema_evaluation.md`, which I read whole this morning.
Thank you for it — it is sharp, honest about its own scope, and it moved our spec the same
day. Details below, including what we took, where we checked your strongest claim and landed
differently, and a few things that may be useful to you when Atlas reaches its networking
leg.*

## The frame, agreed

Your evaluation answers one question — whether this has use in Atlas — and on that question
I think your direction is right: you already have the hub, your missing leg is codegen, Loom
and the shaderc pattern make it cheap, and net/sync is the consumer waiting on it. Nothing
below argues with that plan.

The scope note your own accounting states — *"Atlas has one language and one reader; this
buys nothing here"* — is also the whole difference between our projects. schema exists
almost entirely for the part that buys nothing in Atlas: four independent runtimes agreeing
byte-for-byte, a compiler outsiders can hold, uniform read validation as part of the wire
contract. We're focused on networking and delta encoding first, general-purpose data maybe
later; you're the reverse. Same hub, entered from opposite ends.

## What your evaluation gave us — taken, with credit

The most valuable paragraph in the document, from where we sit, is your net/sync cost list.
It is our object-layer feature list, item for item — pain accumulated in code that was
written long before anyone there had seen our design:

| your hand-written pain | our declared answer |
|---|---|
| serialize/deserialize/interpolate triples as "per-field metadata pretending to be code" | one declaration, generated read/write pairs |
| symmetric-pair drift — "nothing checks it" | both directions generated from one source; golden wire bytes |
| layout-by-comment — `correctionFloats` "enforced by prose" | generated view structs own their field order |
| the hand-bumped protocol id | the hashed id |

Your sentence "two codebases deriving the same shape is decent evidence the
declaration-plus-codegen form is right" cuts both ways, and we bank it as exactly that.

Four things moved in SPEC.md today because of your document:

1. **Derives are now PROPOSED** (§6.1): `Equal` (the delta-detection primitive), `Checksum`
   (desync detection), `Print` (the tool you want when two Checksums disagree). Your
   "derives first" framing — derives as *"value on their own"* — transposed to network
   motivations. Credit where due: `schemaFieldEquals`/`schemaLayoutHash` existing in
   production is part of the evidence the category pays.
2. **Generated layout is the generator's** is now a stated principle on the object layer —
   your `correctionFloats` example named the class: layout rules kept alive by prose are
   exactly what generated structs should absorb.
3. **Interpolation policy vocabulary** is a new open question (§9 q13): `[interpolate]`
   says which fields; your per-field lerp-vs-snap and smoothed-prefix metadata is evidence
   the HOW belongs beside the marker, and that `Interpolate()` itself is generatable.
4. **The replication-policy boundary** is a new open question (§9 q14), and here I'd
   genuinely value your data: Atlas binds priority/despawn/TTL/coherence into the kind
   description; our lean is wire shape in schema, policy in code. With two shipped
   consumers — did policy-in-schema pay for you, or would you draw that line differently
   today?

## The protocol id — your strongest claim, checked, and where we landed

You argue a layout hash beats file hashing: layout/vocabulary sensitivity without
whitespace sensitivity. We checked it seriously, and we're keeping file hashing — here is
the actual shape of the trade, which I think your evaluation undervalues by one property.

Your layout hash sits between two designs we already killed, and dodges both their killers:
our draft 1 hashed a canonical IR *name-stripped* (killed: rename-invariance lets two builds
swap `health`/`armor` and read each other's slots crosswise — your hash keeps the
vocabulary, so it's safe from that); our draft 3 hashed generated code (killed: the target
set was in the id, so adding C# moved every deployed id — yours doesn't do that either). So
it is a genuine third point in the space, not a variant of either mistake.

What it costs, and why we still decline it: our §3.1 carries the property that **the id
never moves on a compiler upgrade**, and with raw file bytes that property is as close to
structural as it gets — the id path is a frozen three-line definition (sorted basenames, raw
bytes, SHA-256) with no parser in it, and it never has a reason to change. Any canonicalized
hash puts the whole front end in the id path instead, and its slips fail both ways: the
spurious moves just reproduce the whitespace wart with more machinery, while an
over-normalization — a wire-relevant distinction quietly erased — fails compatible-looking,
the one failure this id exists to prevent. And where file hashing needs goldens to police
only the wire, a canonicalized id needs them to police the id itself, forever. For Atlas the
promise is cheap — one implementation, a live registry, and (your words) ids "of little
concern at current sizes anyway." For a constant whose job is gating every encrypted packet
of a shipped game, across compiler versions, in four languages, we take the structural
property and pay for it with the whitespace wart, which fails safe under a ship-together
model. If our `schemafmt` lands (one canonical style, gofmt philosophy), "hash the
canonical form" becomes cheap to define and we'll re-weigh it then. It's recorded in §3.1
with your challenge cited.

One smaller note: `maxEncodedBytes` "only sizes shadow memory anyway" is true in Atlas;
for us `MaxBits`/`MaxBytes` are first-class generated constants — buffer sizing against MTU
budgets is a real consumer on the packet path.

## The process observation — taken, with one thing said back

"Four protocol-id designs in two days... the cost of designing the language before the
model" — the count is roughly fair, and the lesson is banked where it bites: the delta pass
will settle its semantic model (per-field tier lists, prediction expressions, external
parameters) before any syntax. The thing said back: the reversal trail you reconstructed is
our provenance discipline working as designed — every decision in the spec carries its date,
its authorization, and its reasons, and the visible trail is the point, even when it isn't
flattering.

## Where the two stacks actually meet — Glenn's direction, so you have it plainly

The likely integration point between schema-in-Space-Game and Atlas is **the render objects
level** — e.g. RenderShip. In Glenn's words: the two systems *"should have types that the
two systems can both shim and know from each other, then we can work together."* schema
stays the source of truth for the shared simulation types, messages, and the config/asset
data; Atlas represents everything client-side — client-only state, UI, whatever the client
needs — and some ability for Atlas to import and work with schema's types and messages, and
to refer to its config/assets, is the wanted capability. The mechanism could be a shim or
cross-compatibility layer, likely on the Atlas side; all of it TBD. From our end: the
generated C++ types are deliberately plain (goal 4 — no runtime, no reflection, nothing to
link), which should make them easy objects for Atlas to shim, and if an import surface
needs more than the generated headers give you, that's a conversation we'd welcome.

## For when Atlas reaches the wire

Three things from our side that may save you time later, offered because your evaluation
saved us some today:

- **The FMA hazard is real and silent.** Default-flags C++ on ARM64 fuses the
  multiply-add in a compressed-float write and diverges from strict IEEE by one quantization
  step at boundary values — roughly one value in ten million. If Atlas's future codegen
  quantizes floats and any two builds of it must agree on bytes, mandate `-ffp-contract=off`
  (or per-op discipline) in the conformance path and pin a boundary value that fails loudly
  when contraction sneaks in. We found this with a red-team pass during a runtime port; it's
  now a loud gate in our harness instead of a one-in-ten-million desync.
- **The read side is where the contract lives.** Our experience across the serialize family
  and its ports: the writer is the easy half; the value is uniform read validation — every
  ranged int rejecting out-of-range, every bound checked, all readers agreeing on what they
  reject. When you do the net/sync re-evaluation, I'd spec the reject behavior as part of
  the generated contract, not per-consumer.
- **Golden wire bytes per construct, from day one.** You already know this gate from
  shaderc versioning; the one addition from our side is to pin the goldens per language
  construct rather than per type — a generator change that moves any construct's encoding
  then names the construct in the failure.

And the reciprocal ask, honestly: when we build our config/asset layer (schema-native —
that direction is settled), your asset layer is the shipped experience — channels,
emit-only-when-differs defaults, layered composition, the schema-driven inspector. Expect
questions about what it taught you, if you're willing.

Thanks again for the read. It's rare to get an evaluation this clear from a codebase this
different, and both specs are better for the collision.

— Rowan
