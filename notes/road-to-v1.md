# The road to 1.0 — decision list and state of the spec

*2026-08-05 afternoon. Glenn's ask: "what open questions or work is required before we can
say, OK we have the 1.0 spec and we might try an implementation." This note is the answer:
what was done autonomously today, the decisions that are yours, and what remains after
them. Every recommendation below has a full dossier behind it (evidence with file:line
citations, options with reversal costs); the spec's §9 rows carry the compressed forms.*

## How draft 5 happened

Three independent audits ran over draft 4 — corpus-compiles-on-paper, spec
self-consistency, and wire fidelity against all four extracted runtime contracts — plus
eight decision dossiers and a completeness pass, all findings then **adversarially
verified at their cited lines** (17 of 19 confirmed; 1 refuted and discarded; the wire
audit found zero hard contradictions and one wording defect). Everything decidable by
evidence was repaired in place with a correction note; everything needing design judgment
was drafted as PROPOSED; everything that is genuinely your call is below.

**Repaired without needing you** (each contradicted DECIDED text or its own grammar):
the §3.2 layout-independence sentence (draft-1/2 residue, false under file hashing); three
IR-as-protocol-id-input leftovers (§7.1, §7.2, §8); six three-targets leftovers from
before C# joined; the enum-max off-by-one (`count − 1` → `count` — as written it made the
top variant unencodable while passing); dead `int8(0, 1000)` call syntax in §4.6's own
worked example; the float-attribute "literals only" sentence that contradicted the DECIDED
typed-constants rule; the newline-suppression rule that made every block unparseable as
written; the undefined `FloatExpr`; signed float attribute values (`min = -180.0` was
underivable from the grammar); the `interpolated`/`interpolate` marker drift; the missing
object-artifact family in §6.1/§7.1; §7.2's missing golden-wire-bytes and fp-contract
gates (both promised elsewhere by name); uint64 constants ≥ 2^63 being unwritable under
checked-signed-64 folding; and **the corpus's own standing-check violation** — `const
MaxBlockSize` declared in both Constants.schema and Messages.schema, a real §4.6 duplicate
in one unit (fixed: Constants.schema owns it).

**Added as normative or PROPOSED completions** (an implementer could not have produced one
correct byte from the spec alone): the §4.3 wire model (bit order, word flush, final-byte
fill, align-at-boundary, length prefixes); read termination and buffer exhaustion (§5);
zero-values for enums/nested/arrays (§5); the §3.1 hash procedure's edge cases
(basename-delimited hashing, sort collation, truncation endianness, BOM, CRLF); namespace
and claimed-generated-names rules; object-not-a-field-type and plain-fields-only-in-object
rules; per-target symbol naming; and the small-semantics corner pins (division truncation,
`bits(N)` as switch subject, `case None:`, variant named `None`, message-level MaxBytes).

## THE DECISIONS — twelve lines, each answerable in one

Ordered so the batch confirmations come first and the real design calls get your
attention last.

1. **The confirmations batch — recommended yes to all six, answerable as one line
   ("1: yes to all"):**
   - **q1** strings are byte strings → DECIDED (generated-code consequence now stated).
   - **q3** wide strings + int_relative stay deferred, int_relative bound to the delta pass.
   - **q6** root/packet marker → v2, non-id only; id-scoping closed while §3.1 stands
     (reachability needs the parser in the id path — the exact trade you declined).
   - **q7** enum-count constants spell `Team.max` (count = `Team.max + 1`).
   - **q8** constants are platform-uniform (one id must mean one wire).
   - **q10** sentinel-terminated collections defer into the delta pass (the surveyed
     packets use three different terminator idioms; the construct is inseparable from
     budget-driven packet splitting).
2. **The Ship object shape + view-encoding semantics + q13 (one package).** The corpus
   Ship is verified faithful against the real `Ship.h`, field for field — zero
   misclassifications, zero inventions, one noted omission (client-only
   `predictedExplode`, which needs a side-conditional story someday). The five
   view-encoding rules in §4.8 are transcriptions of your own hand-written code
   (`core_quantize.h` rounding and clamps, the health `ceil` that keeps wounded ships
   alive on the wire, `type Quat [unit]` for the implicit ±1 + renormalize), and q13
   resolves as a fixed type-derived policy table (lerp / shortest-arc nlerp / snap —
   measured: no field in any of the four objects overrides its type's policy).
   **Decide: "Ship approved, view rules per §4.8, q13 table — yes"** (then Missile,
   DynamicProp and Turret get written). One real correction rides it: the corpus thrust's
   `max = 100` conflated the wire range with the float domain (true bound 1.0) — the
   §4.8 rule 4 form fixes it before it propagates three more times.
3. **q9 — enum separator, explicit values, flags.** Recommended **B**: newline-terminated
   variants (like fields), `= value`, and `[flags]` all in v1 — the separator is the one
   piece that cannot be retrofitted without breaking shipped schemas, and the flags form
   has direct in-game demand (three anonymous `uint64` flags fields fly today with
   hand-kept masks). Fallback **D**: separator now, values/flags fast-follow.
   **Decide: "3: B" (or D, or keep bare-whitespace).**
4. **q11 — the no-None enum wire.** Recommended: the field attribute lands in v1; the
   only open bit is the spelling — `[no_none]` states the wire contract; `[index]` is
   your word but is wanted later for the real index feature. **Decide: "4: yes,
   no_none"** (or name it).
5. **Message/object tag order.** I reversed my own sorted-by-name proposal on the
   evidence: inside one ship-together unit the wire cannot tell the orders apart, so the
   decision falls to logs, dashboards and human memory — where sorted renumbers on every
   ordinary addition and declaration order only on deliberate refactor. All three
   discriminant sets in the shipping game are append-ordered. **Decide: "5: declaration
   order" (or keep sorted).**
6. **Derives.** Recommended: `Equal`/`Checksum`/`Print` in v1, **default-on, no syntax**
   — an in-schema toggle would move deployed protocol ids for a wire-irrelevant switch,
   and a compiler flag forks the generated API. `Checksum` = FNV-1a-64 **over the
   canonical encoded bytes** (your existing blob-identity hash), which makes
   cross-language checksum stability a corollary of the conformance matrix instead of a
   second wire contract. Principled fallback: defer all three (reverses for free).
   **Decide: "6: in, default-on" (or "defer").**
7. **q4 — schemafmt timing.** Recommended: **v1 deliverable, built early** — under
   file-byte hashing, reformatting a deployed schema moves its protocol id, so the only
   free canonicalization window closes when v1 ships. Style rules are drafted (§7.4).
   **Decide: "7: v1" (or fast-follow), and bless or delegate the three §7.4 sub-calls.**
8. **q5 — doc comments.** Recommended: **v1** — the comment-carrying scanner is schemafmt
   machinery anyway; two external consumers named; deferral rewrites every golden once.
   **Decide: "8: v1" (or attach-now-emit-later).**
9. **q14 — replication-policy boundary.** Recommendation: state it as a non-goal — wire
   SHAPE in schema, replication POLICY in code; policy is tuned live and is not shape.
   The opposing external data point stays recorded in q14 for the delta pass.
   **Decide: "9: non-goal, yes."**
10. **serialize.cs** (adjacent, blocks the §1 table's honesty): the LICENSE (absent by
    design, your call), CI workflows, and whether it goes public. **Decide when ready —
    nothing in the spec blocks on it.**
11. **The files.go sort TODO** sitting uncommitted in your ~/space tree (the protocol-id
    Readdir-order bug from the survey) — yours to commit whenever.
12. **Name-neutrality consistency** (small): the 60d6376 sweep took the people's names
    out of this repo but "Atlas" still appears in §4.8/§6.1/q13–q16 while
    `notes/external-feedback-learnings.md` deliberately says "an experienced engine
    team." Nothing wrong per your ruling — flagging only so the inconsistency is chosen
    rather than accidental. **Decide: keep as-is, or sweep the engine name too.**

## What "1.0, start implementation" then means

Your twelve answers land as DECIDED stamps (each with your words, per house style); the
corpus updates mechanically (Enums.schema gains a pinned-value and a `[flags]` example
under q9; `CraftCreate.kind` gains `[no_none]` under q11; thrust/health take rule 4's form
under decision 2; Missile/DynamicProp/Turret get written against the approved Ship). Then
the definition of done holds: **every §9 row DECIDED or explicitly deferred-with-owner, no
PROPOSED markers left in normative sections, the corpus compiling under the spec as
written** — and §7.3's path says the rest: the corpus graduates to `testdata/`, and the
scanner is the first code.

Estimated state after your answers: **zero open design questions in v1 scope.** The spec
is implementable today for everything DECIDED; the twelve above are the whole remaining
gap.

## Found in passing — in the game tree, verified at the cited lines by two readers

Three latent defects in `~/space`, reported here because schema's generators are the
structural fix for exactly this class (none of them blocks the spec):

1. **`Libraries/core/core_interpolation.h:247`** — the shortest-arc dot product reads
   `a.x*b.x + a.y*b.y + a.z*a.z + a.w*b.w`: the z term is `a.z*a.z` (a·a), not `a.z*b.z`.
   The negate-on-dot<0 decision is wrong for some rotation pairs — occasional
   long-way-around interpolation, subtle visual spin. One character fix.
2. **`Source/Ship.h:681` and `:740`** — `core_assert( current->ship_type <=
   generated::PropType_MAX )` guards `ship_type` with the *wrong enum's* MAX (PropType 6
   vs ShipType 5; the adjacent writes use the correct `ShipType_MAX`). The assert is too
   loose — it would admit an invalid ship type 6.
3. **`Source/Turret.h:29` vs `:266/:279`** — `turret_index` is written
   `[0, MAX_TURRETS_PER_SHIP - 1]` on the deep path but `[0, MAX_TURRETS_PER_SHIP]` on
   the create path: 8 bits on one wire, 9 on the other, for one field. Each path's
   write/read pair agrees, so nothing desyncs today — it is the hand-restated-bound drift
   class, live. (Also `MAX_TURRETS_PER_SHIP` is `#define`d twice, `Constants.h:58` and
   `:76`, identical values.)
