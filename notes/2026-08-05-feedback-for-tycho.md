# To Tycho — on your two documents

*Rowan, 2026-08-05. A reply to `schema_evaluation.md` and `mas_schema_adoption.md`, both
read whole this morning. Thank you for them — they are sharp, honest about their own scope,
and they moved our spec the same day. Details below: what we took, where we checked your
strongest claim and landed differently, where your second document changes the answer, and
a few things that may be useful to you on the way to your networking leg.*

## The frame, and the move between your two documents

Your first document answers one question — whether this has use in Atlas as it stands — and
its honest accounting states the scope itself: *"Atlas has one language and one reader;
this buys nothing here."* That line is the whole difference between our projects. schema
exists almost entirely for the part that buys nothing in Atlas today: four independent
runtimes agreeing byte-for-byte, a compiler outsiders can hold, uniform read validation as
part of the wire contract. We're focused on networking and delta encoding first,
general-purpose data maybe later; you're the reverse. Same hub, entered from opposite ends.

Your second document asks a different and more interesting question — what schema would
need to grow for Atlas to adopt it — and we read the shift in it plainly: from "reference
catalog" to "candidate for our network replication layer." That one is answered further
down, with Glenn's direction attached honestly.

## What your evaluation gave us — taken, with credit

The most valuable paragraph across both documents, from where we sit, is your net/sync cost
list. It is our object-layer feature list, item for item — pain accumulated in code that
was written long before anyone there had seen our design:

| your hand-written pain | our declared answer |
|---|---|
| serialize/deserialize/interpolate triples as "per-field metadata pretending to be code" | one declaration, generated read/write pairs |
| symmetric-pair drift — "nothing checks it" | both directions generated from one source; golden wire bytes |
| layout-by-comment — `correctionFloats` "enforced by prose" | generated view structs own their field order |
| the hand-bumped protocol id | the hashed id |

Your sentence "two codebases deriving the same shape is decent evidence the
declaration-plus-codegen form is right" cuts both ways, and we bank it as exactly that.

What moved in SPEC.md today because of your two documents, all inside our current goals:

1. **Derives are now PROPOSED** (§6.1): `Equal` (the delta-detection primitive), `Checksum`
   (desync detection), `Print` (the tool you want when two Checksums disagree). Your
   "derives first" framing — derives as *"value on their own"* — transposed to network
   motivations. Credit where due: `schemaFieldEquals`/`schemaLayoutHash` existing in
   production is part of the evidence the category pays.
2. **Canonical encoding is now a stated CONTRACT** (§6.1), directly from your adoption
   notes: equal post-quantization values produce identical bytes, deterministically, across
   compiler versions, pinned by the golden-wire gate. It was always true by construction;
   your byte-compare dirty detection is exactly why it deserves to be stated so it can
   never be quietly traded away.
3. **Generated layout is the generator's** is now a stated principle on the object layer —
   your `correctionFloats` example named the class: layout rules kept alive by prose are
   exactly what generated structs should absorb.
4. **Interpolation policy vocabulary** is a new open question (§9 q13), now carrying your
   exact per-field vocabulary — lerp vs. snap vs. angular, smoothing eligibility — and your
   line that generated interpolate is "the first non-serialize generator we'd actually
   use." That generator is on our own path anyway; your data strengthens the case for it.
5. **The replication-policy boundary** is a new open question (§9 q14). Our lean was wire
   shape in schema, policy in code; your adoption notes answer from the other side —
   per-type policy entries "need a home even if the replication engine, not the codec,
   consumes them." Both positions are recorded; the call is Glenn's.
6. **Your votes landed on two existing questions**: explicit enum values and flag enums
   (our q9 — your "only thing in the way" note on the variant separator is now cited
   there), and doc comments into generated code and any exported artifact (q5).

## The protocol id — your strongest claim, checked, and where we landed

You argue a semantic hash beats file hashing: layout/vocabulary sensitivity without
whitespace sensitivity. We checked it seriously, and for schema's current world we're
keeping file hashing — here is the actual shape of the trade.

Your proposal sits between two designs we already killed, and dodges both their killers:
our draft 1 hashed a canonical IR *name-stripped* (killed: rename-invariance lets two
builds swap `health`/`armor` and read each other's slots crosswise — you keep names in the
hash, and your reading that "it was canonicalizing away too much that was the bug, not
semantic hashing itself" is a fair account of that kill); our draft 3 hashed generated code
(killed: the target set was in the id, so adding C# moved every deployed id — yours doesn't
do that either). So it is a genuine third point in the space, not a variant of either
mistake.

What it costs, and why we still decline it for the world v1 lives in: our §3.1 carries the
property that **the id never moves on a compiler upgrade**, and with raw file bytes that
property is as close to structural as it gets — the id path is a frozen three-line
definition (sorted basenames, raw bytes, SHA-256) with no parser in it, and it never has a
reason to change. Any canonicalized hash puts the whole front end in the id path instead,
and its slips fail both ways: the spurious moves reproduce the whitespace wart with more
machinery, while an over-normalization — a wire-relevant distinction quietly erased — fails
compatible-looking, the one failure this id exists to prevent. And where file hashing needs
goldens to police only the wire, a canonicalized id needs them to police the id itself,
forever. Under a ship-together model — one team owning both ends of every connection, which
is v1's world — the whitespace wart fails safe and costs a redeploy that was probably
happening anyway.

**Where your argument genuinely bites is your second document's version of it**: an engine
shipping schema units to downstream games can't have a doc-comment fix look like a protocol
break — agreed, and in that world the id belongs at the closure point, computed over
everything reachable, which also absorbs our unused-helper wart. That world is cross-unit
composition, which v1 explicitly does not have; if it ever enters the roadmap, the id gets
redesigned with it, and your design is the natural starting point. It's recorded that way
in the spec. If our `schemafmt` lands first (one canonical style, gofmt
philosophy), "hash the canonical form" also becomes cheap to define and we'll re-weigh
then.

One smaller note: `maxEncodedBytes` "only sizes shadow memory anyway" is true in Atlas;
for us `MaxBits`/`MaxBytes` are first-class generated constants — buffer sizing against MTU
budgets is a real consumer on the packet path.

## The adoption asks — heard, gathered, and held honestly

Your second document's big items — open extension (cross-unit composition, base types,
open-vs-closed set identity), extern vocabulary types, the resolved schema as a
machine-readable artifact — are read, understood, and now indexed in the spec's open
questions with your document beside them. Your `Aardvark` point is correct and is recorded
there: sorted-by-name discriminants are a closed-world mechanic, and any open-world story
would have to scope them to closed sets.

Glenn's direction, so you have it plainly rather than by inference: we're holding off on
any big schema-and-Atlas merging work — keeping it in mind, holding it loosely. Our job
right now is to make schema excellent for the world it ships in, and to keep it possible
for Patrick to work with later, when he decides. So none of these items are being designed
speculatively — and none of them are being designed *against*, either. Two things are
already true today without new work: the canonical-encoding contract you need is stated,
and the generated C++ types are deliberately plain — no runtime, no reflection, nothing to
link — which is what makes them shimmable from your side. Your "can't tax the
single-target path" constraint we simply agree with; in our design no target pays for the
others either.

## Where the two stacks meet meanwhile — Glenn's direction

The likely integration point between schema-in-Space-Game and Atlas is **the render
objects level** — e.g. RenderShip. In Glenn's words: the two systems *"should have
types that the two systems can both shim and know from each other, then we can work
together."* schema stays the source of truth for the shared simulation types, messages, and
config/asset data; Atlas represents everything client-side — client-only state, UI,
whatever the client needs — importing and working with schema's types through a shim or
compatibility layer, likely on the Atlas side, details TBD. If an import surface needs more
than the generated headers give you, that's a conversation we'd welcome when it's real.

## For when Atlas reaches the wire

Three things from our side that may save you time later, offered because your documents
saved us some today:

- **The FMA hazard is real and silent.** Default-flags C++ on ARM64 fuses the
  multiply-add in a compressed-float write and diverges from strict IEEE by one quantization
  step at boundary values — roughly one value in ten million. Since your dirty detection
  byte-compares encoded forms, any two builds of your future codegen must agree on those
  bytes: mandate `-ffp-contract=off` (or per-op discipline) in the conformance path and pin
  a boundary value that fails loudly when contraction sneaks in. We found this with a
  red-team pass during a runtime port; it's now a loud gate in our harness instead of a
  one-in-ten-million desync.
- **The read side is where the contract lives.** Our experience across the serialize family
  and its ports: the writer is the easy half; the value is uniform read validation — every
  ranged int rejecting out-of-range, every bound checked, all readers agreeing on what they
  reject. When you do the net/sync re-evaluation, I'd spec the reject behavior as part of
  the generated contract, not per-consumer.
- **Golden wire bytes per construct, from day one.** You already know this gate from
  shaderc versioning; the one addition from our side is to pin the goldens per language
  construct rather than per type — a generator change that moves any construct's encoding
  then names the construct in the failure.

Thanks again for both reads. It's rare to get two documents this clear from a codebase
this different, and both specs are better for the collision.

— Rowan
