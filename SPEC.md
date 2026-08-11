# schema — specification (draft 5)

> **Status: IMPLEMENTATION STARTED 2026-08-05, on Glenn's word, mid-corpus-review.** The
> first slice is in the tree: scanner, parser, resolver/checker and the C++ storage
> emission (structs, constants, enums, flags, the object view families) — `make` builds the
> compiler, generates `generated/cpp/*.h` from `examples/`, and compiles+links+runs a test that
> prints OK. Write/Read generation, Quantize/Unquantize, and the other three targets are
> next; nothing is wire-tested yet. Sections are marked **DECIDED** (Glenn) or **PROPOSED**
> (Rowan's recommendation, awaiting review). **§4 — the language — remains the chapter to
> review hardest.**
>
> *Draft 2, 2026-08-04: draft 1 taken apart by two independent cold readers — one attacking
> every wire claim against the extracted runtime contracts, one attacking the language design
> and the Go pipeline. The material changes: back-references got a dominance rule, `string`
> became a byte string so all three generated readers agree, untaken branch fields are
> zeroed, the IR preservation invariant replaced draft 1's lossy "ranges reduced to bit
> widths", and the grammar chapter now exists.*
>
> *Draft 3, same evening, on Glenn's word: the protocol id hashes the **generated code**
> (superseding both draft 1's name-stripped IR hash and draft 2's named-IR hash), and
> generated measure functions are dropped.*
>
> *Draft 4, 2026-08-05, on Glenn's word: the protocol id reversed once more to **hash the
> schema files themselves** (§3.1 keeps all three designs with why each fell); C# joined as
> the fourth target and serialize.cs was ported the same night; and the register went
> **Go-inspired** — `type` not `struct`, `float32`/`float64`, snake_case fields, no
> semicolons, `schemafmt` (§4.1).*
>
> *Draft 5, 2026-08-05 afternoon — the road-to-1.0 audit: three independent audits
> (corpus-on-paper, self-consistency, wire fidelity vs all four runtime contracts), eight
> decision dossiers, and an adversarial verification pass over all of it (17 of 19 findings
> confirmed at their cited lines). What changed: stale draft-1/2 residue repaired wherever it
> contradicted DECIDED text (the §3.2 layout-independence sentence, the IR-as-id-input
> sentences, the three-targets leftovers, the enum-max off-by-one, dead call-syntax ranges,
> the float-attribute literals-only clash); the grammar's newline-suppression and
> float-expression holes closed; a normative wire model added (§4.3.0); read termination
> defined (§5); §6.1/§7.1 completed with the DECIDED object artifacts; §7.2 gains the
> golden-wire-bytes and fp-contract gates its own promises relied on; and every §9 open
> question now carries a worked recommendation with its evidence, his calls landed the same evening, item by item —
> the decision list is `notes/road-to-v1.md`.*

## 1. What schema is

**schema** is a small language for describing bitpacked network data, and a compiler — written
in Go — that translates `*.schema` files into generated C++, C#, Go and Rust source code
targeting the serialize family of libraries:

| target | runtime library |
|---|---|
| C++ | [serialize](https://github.com/mas-bandwidth/serialize) |
| C# | [serialize.cs](https://github.com/mas-bandwidth/serialize.cs) — ported 2026-08-05 (PUBLIC since 2026-08-05, AGPL-3.0; 32/32 tests, wire-verified vs C++ both directions; CI landing) |
| Go | [serialize.go](https://github.com/mas-bandwidth/serialize.go) |
| Rust | [serialize.rs](https://github.com/mas-bandwidth/serialize.rs) |

All four runtimes are bit-for-bit wire compatible — the original three pin it in CI with
golden bytes; serialize.cs pins the same 72 golden bytes and byte-identical clang++ interop,
with CI to follow. schema inherits that foundation: **a type serialized by generated code
in any target language decodes identically in the others.**

**C# — DECIDED (Glenn, 2026-08-04), prerequisite MET overnight.** serialize.cs was ported the
same night by the red/blue method — golden bytes verbatim from the family's shared 72-byte
pin, byte-identical head-to-head against real `serialize.h` under clang++, cross-reads both
directions, Go-style sticky errors. Private pending Glenn's license call and CI. One wire
finding worth knowing came out of the port: **default-flags C++ on ARM64 FMA-contracts the
compressed-float write** and diverges from strict IEEE by one quantization step at boundary
values — strict IEEE is declared the normative wire, and the compat harness mandates
`-ffp-contract=off` with an FMA-boundary value in the sequence so a contracted build fails
the gate loudly. The schema compiler's conformance suite (§7.2) inherits that rule. The compiler architecture does not
change: a new target is a new dumb printer over the same IR — and since the protocol id
hashes the schema files (§3.1), adding a target moves no deployed id, ever.

The idea is extracted from
[serialize.modern](https://github.com/mas-bandwidth/serialize.modern)'s compile-time schema
language, which proved two things: a schema that sees the packet as a type can generate
radically faster serialization than a runtime stream (schema writes ~520 M packets/s against
classic's 44.3 M stream writes — "~12x classic's stream writes", serialize.modern README,
reproduced by its shipped bench.cpp), and it can hold wire compatibility to the byte while
doing so. What the template-metaprogramming host cannot escape is that the metaprogram re-runs
inside every consumer's C++ compiler, requires C++23, and can only ever emit C++. schema moves
the same computation into an external compiler: **run once, at code-generation time, emitting
for every platform, with zero compile-time tax on the consumer.**

### Goals

1. **One source of truth for a protocol.** Types and their wire encoding defined once, in
   `*.schema` files; read/write code generated for C++, C#, Go and Rust.
2. **Minimal generated code.** Separate, straight-line read and write functions per type — the
   code a careful expert would hand-write against each serialize library — with no runtime
   schema interpretation, no reflection, and no `IsWriting` branching.
3. **Byte-identical wire output across all four targets**, enforced by a conformance matrix
   in CI, with classic serialize as the oracle. The four generated readers also **agree on
   what they reject** — acceptance is part of the wire contract here.
4. **Compile-time cost near zero for consumers.** Generated code is plain source; the C++
   target requires only what classic serialize requires.
5. **Errors a person can act on.** Every diagnostic carries file:line:column and names the
   fix, holding the bar serialize.modern set with its named `static_assert`s — and exceeding
   it, because a real compiler can point at the exact token.

**The v1 scope, DECIDED (Glenn, 2026-08-05, verbatim):** *"goal for v1 is schema fully
defines generated code for the constants, enums, types, messages, object definitions. the
delta serialization is out of scope of v1."* — *"we will hit that once we lay the
foundation of types/objects."* And the object layer's v1 deliverable, his words: *"just
build the structs from the definition in schema lang to start. (shallow, deep,
interpolated, quantize)."*

### Non-goals (v1)

- **No wire-format versioning.** See §3: versioning is by protocol id, deliberately.
- **No unbounded collections.** Everything on the wire has a declared bound, as everywhere in
  the serialize family.
- **No annotation of existing hand-written types.** schema owns the types it serializes and
  generates them (§6). Mapping onto pre-existing hand-written types may come later.
- **No imports across compilation units.** One unit, one package, all files compiled together
  (§3.2). Cross-unit composition is a later problem.
- **The config/asset table layer is not in v1** — but it is a **committed direction, and it
  is schema-native, not flatbuffers-style**: flatbuffers is being replaced outright (Glenn,
  2026-08-05). See "The horizon" below.
- **No self-describing wire data.** The wire stays an unattributed bit stream; all knowledge
  lives in the generated code on both ends.
- **Deferred constructs:** wide strings (`serialize_wstring`) and relative integers
  (`serialize_int_relative`) are not in v1 — see §4.10.

### The horizon — recorded so v1 does not foreclose it

Glenn, 2026-08-04, on the eventual scope for Space Game: *"all the object types and event
types and all the flatbuffer stuff, eventually i hope to have all that in the schema
language"* — *"and all the delta stuff, and object type definitions, and the baseline, all
generated from schema level definitions in this custom language. NOT right now, but
eventually :)"*

So the long arc is: schema as the single data-definition language for the game — bitpacked
realtime messages (v1, this spec), object and event type definitions, flatbuffers-style
versioned config/asset data, and **delta encoding against a baseline** all generated from one
source. Two v1 implications, noted and then set aside:

- Delta-against-baseline changes the generated function *signature* (it takes the current
  object and a baseline), not the wire model or the compiler architecture — it fits the
  pipeline when its design pass comes. Glenn's concrete shape for it (2026-08-04): *"define
  ship properties and types and attributes in the schema language, and then combined with the
  existing C++ code in space game, it would spit out the full delta code for that object
  hardcoded."* The 2026-08-04 survey of `core_delta.h` shows the hand-written delta code
  already follows one uniform grammar — **per-field encoding tiers tried cheapest-first, one
  bit selecting the tier** (small-window delta vs absolute; or error-vs-*prediction*, where
  the prediction is arithmetic over other fields plus an external parameter like
  `deltaFrames`). So the delta pass needs three language surfaces: per-field tier lists,
  prediction expressions over sibling fields, and declared external parameters — and the
  generated output is the full hardcoded WriteDelta/ReadDelta per object type.
  And the scope is wider than the serialize functions — Glenn: *"It would WRITE the delta
  functions, and the different struct definitions for the ship, and everywhere that ship
  object type is hooked in to the code etc etc"* / *"it's a huge amount of hard coding in
  space game that I've done so far, the intent was always, this will move out to code
  generation."* **schema eventually owns the object TYPE, not just its wire format**: the
  deep/shallow struct definitions, the capacity constants, the per-type dispatch cases, the
  integration points into the game's managers — generated from one declaration, woven into
  the existing C++ codebase. And the generator family is open-ended — Glenn: *"it could also
  generate functions to interpolate the ship structs, generate render data for them, lots of
  codegen could be done."* The architectural consequence lands on v1's IR design: the IR is
  not merely wire semantics, it is a **typed object model**, and backends generalize from
  language targets to *generator kinds* (serialize × four languages today; delta,
  interpolation, render data, struct-to-struct mapping × C++ tomorrow — his example: *"take
  the interpolated ship and then copy it to the ship render struct and so on"*) — per-field
  metadata beyond the wire (how a field interpolates, its delta tiers, which mapped struct a
  field lands in) attaches through the per-field attribute mechanism of §4.2 — the
  attachment point exists in v1's grammar already.
- Nothing in v1's grammar may squat on syntax the horizon will want (`packet`, `table`,
  `delta`, `baseline`, `index` are informally reserved for future use — `index` for the
  everything-is-an-index feature, §9 q11).

**THE FLATBUFFERS REPLACEMENT — DECIDED IN DIRECTION, 2026-08-05.** Glenn: *"I don't want to
use flatbuffers. What I want to do is to bring across definitions for config and assets into
Config.schema and Assets.schema respectively, and have all the boilerplate code to generate
and load them created by the schema compiler. The golang tool, the code in C++
ConfigManager / AssetsManager and so on."* — *"Everything should be in schema."*

The scope rule that governs the pass: **not a flatbuffers equivalent** — *"It should be able
to effectively act as flatbuffers (at least, the subset of flatbuffers that we're using here
in space game)... flatbuffers was just a means to an end there."* And the governing
principle, which is really the whole language's thesis: ***"as always the goal is the
minimal representation of the true thing in the schema language, with the boilerplate and
all that code just tracked and generated"* — versus 1:1 porting of the flatbuffers stuff.**

What gets absorbed (surveyed 2026-08-05): `game/config/` (Explosions, Global, Lasers,
Missiles, Props, Ships, Teams — JSON + .flat pairs) and `game/assets/` (Levels, Missiles,
Props, Ships, Turrets); the Go pipeline (`cmd/update_schemas` 132 lines,
`cmd/update_config` 928 lines) that compiles and collates JSON definitions into
`Config.bin`/`Assets.bin`; and the C++ `ConfigManager`/`AssetsManager` loading code — all
becoming schema compiler outputs driven by `Config.schema` and `Assets.schema`.

**And the enum convergence, his catch:** some enums are hand-declared today only as
flatbuffers residue — *"some enums should actually be generated parts of config/assets —
manually specifying them here is just an artifact of working with flatbuffers."* `ShipType`
is really a derived set (each ship config/asset defines a type), so the table pass generates
it from the definitions — **the set-extraction move a third time**: messages → `MessageType`,
objects → `ObjectType`, config/asset definitions → their type enums. Hand-declared enums
remain for genuinely hand-owned sets (`Team`).

**And the shape of the table layer itself — ASPIRATIONAL, his preference stated with its
fallback (Glenn, 2026-08-05):** *"If we do this correctly, Config.bin and Assets.bin are
just expressions of this pattern, not hard coded things. This is aspirational, if not
possible, then we can fall back on config and assets being first class concepts in the
language, but I think it will be more flexible if they weren't."* The evidence for the
general pattern is in his own `Constants.h`: **four** data blobs already exist —
`MAX_CONFIG_DATA_BYTES`, `MAX_OPTIONS_DATA_BYTES`, `MAX_ASSETS_DATA_BYTES`,
`MAX_USER_SETTINGS_DATA_BYTES` — so first-class `config`/`assets` keywords would undercount
the game's own usage on day one. The general mechanism to design: **a declared collection of
typed instances, living as data files, collated by the compiler into a versioned, hashed
binary with generated loaders, accessors and derived enums** — of which config, assets,
options and user settings are four expressions. His closing formulation of the shape, the
collection triple: *"the pattern for Config.bin and Assets.bin is a source directory of
json files working against some schema pattern, eg. set of config types, set of asset
types, compiling and linking into *.bin"* — **one source directory, against one declared
set of types, into one binary.**

**The frame that completes it (Glenn, 2026-08-05): the schema compiler is a
compiler-linker for data.** *"the json files are sort of 'compiled' and 'linked' into the
*.bin file"* — *"and the contents are verified... as they are processed."* The JSON
instances are **source files with human authors**: designers editing config; artists
authoring assets out of tools inside Maya or Blender (collider sets and the like).
Verification becomes a compile step — which closes the survey's trust-boundary hole at
exactly the place his own `update_config.go:794` comment asked for. *"you can see all this
is implemented in space game right now, just with flatbuffers and the golang tools doing
the compile+link step with a lot of boilerplate gross code."*

**Collections are the same mechanism differentiated by declared PROPERTIES, not different
kinds** — *"point is they are really just the same thing"* — and the two properties already
visible:

- **Reload semantics.** Assets: *"loaded once and can't be reloaded unless you reload the
  whole level."* Config: *"expected to be tweakable and can be tweaked (atomically, whole
  config.bin at once) while the game is playing, and it can handle it. this is very
  powerful!!!"* — a per-collection property driving which generated loader the collection
  gets (hot-swap path vs load-once).
- **Cross-collection references, one direction.** *"config can back refer to assets...
  assets don't know about config, but config can refer to and expect things in the asset"*
  — possibly a new language concept (a declared, directional expectation between
  collections — a DAG, verified at data-compile time), TBD.

From the 2026-08-04 `.fbs` survey, the structural shapes the table layer still owes beyond
v1: enums with explicit values and bit-flag enums, unions at the type level, **field
defaults**, and vectors of tables — each now scoped to *the subset space game actually
uses*, per the rule above.

**The nearest edge of the horizon is the event set** — Glenn: *"the set of events could also
be defined fully in the schema language, along with how to serialize each event type."* Its
wire half is already expressible in v1: an enum-dispatched union with per-case fields
(`examples/Messages.schema` is exactly that shape, and the current `events.fbs` union maps
onto it directly). Only its table-layer half waits.

**The whole horizon in his one sentence:** *"so much boilerplate would just go away, replaced
with a definition of what object types there are, and what properties and attributes
per-property."*

**ORDERING AND MIGRATION STRATEGY — DECIDED (Glenn, 2026-08-11, live, during the space
integration):**

- **The table layer comes BEFORE the delta pass.** His words: *"we also want to replace the
  flatbuffers stuff and the config/asset stuff and express that in schema language. We'll
  need a new language feature to express this, it won't fit into bitpacked types, and it
  needs proper versioning like flatbuffers. Maybe after you do messages, this is the next
  thing to do?"* then *"This would actually be better before the delta stuff, and will be
  less complicated."* Sequence: messages (landed 2026-08-11, byte-identical to the
  hand-written wire) → table layer → delta.
- **Constants migrate through a temporary duplicate set.** *"we need to work at getting
  constants into schema language, and we can only do that by having a duplicate set of
  constants in schema and flatbuffers temporarily (eventually, it should all be in
  schema)"* — because the .fbs constant-wrapper enums are consumed by other .fbs files
  until flatbuffers is fully removed. Enacted in space the same day: `Constants.schema` is
  the one home, the C++ game reads generated `space::` values through name-preserving
  aliases, and a static_assert guard block makes schema↔flatbuffers drift a compile error.
- **Generated files are checked into a consumer repo exactly when consumers exist that
  cannot run the generator** (Unity, a Go module on fresh clone) — his principle verbatim:
  *"If it were just C++ only, I would not."* C++-only consumers generate at build (the
  space CMake `schema_generate` target is the worked example).

## 2. The name and the files — DECIDED

The language is called **schema**. Schema files use the `.schema` extension, named in
UpperCamelCase after their contents.

**The file layout convention is aspect-oriented** (Glenn, 2026-08-05: *"I like aspect
oriented programming, eg. all constants here, all messages there, all objects there and so
on"* — with his hedge kept: *"This is not a hard requirement, just a personal preference"*):
`Constants.schema`, `Enums.schema`, `Types.schema`, `Messages.schema`, `Objects.schema`,
and — when their passes land — `Config.schema` and `Assets.schema`. A convention the corpus
and docs follow and `schemafmt` respects, never compiler-enforced; order-free cross-file
resolution (§4.2) is what makes it free.

## 3. Versioning: the protocol id — DECIDED, mechanism DECIDED

**Only two sides holding the same protocol id can talk. There is no versioning overhead on the
wire, no optional-field tags, no evolution machinery.** This is the serialize-family model:
for realtime network data you ensure client and server are at the same protocol version before
they exchange a byte, which radically simplifies everything downstream. (Glenn, 2026-08-04.)

**The protocol id is a hash of the schema itself** — DECIDED. The compiler computes it; nobody
maintains it by hand; two sides with different wire formats cannot accidentally claim
compatibility.

### 3.1 The hash — DECIDED: hash the schema files

**The id is the low 64 bits of SHA-256 over the unit's `*.schema` files themselves** (Glenn,
2026-08-04: *"just hash the schema files themselves, instead of generated. Seems cleaner,
because the schemas generates the files, it would be faster, and now the protocol id does not
change when we add a new language"*). The context that sets the bar (Glenn, same
conversation): *"we would manually bump version id in games when we changed the serialize
functions, and that's super unsafe"* — the id exists so that forgetting to bump is
structurally impossible.

**This is the production-proven pattern**: Space Game already ships `schema_hash.h` — *"hash
of all game/Schemas/*.fbs files. generated. do not edit!"* — folded with `PROTOCOL_VERSION`
into the protocol id that gates every encrypted packet. schema does for the bitpacked layer
exactly what that mechanism already does for the flatbuffers layer.

~~Definition, precise so two compiler builds agree: the unit's `*.schema` files ordered by
sorted basename, each file's raw bytes hashed in sequence; low 64 bits of the SHA-256.~~
*(Superseded 2026-08-05 by the pinned procedure below — which is NOT only edge-case
pinning: it changes the hash input. Bare concatenation lets two textually different units
hash identically — "AB"+"C" equals "A"+"BC" — so the procedure below delimits each file
with its basename, which also means every file RENAME moves the id, not only
order-changing ones. A semantic change to the PROPOSED mechanism, surfaced on the
road-to-v1 decision list rather than slipped through as tidying — a first version of this
edit did exactly that and a cold reader blocked it.)*

Properties, stated honestly in both directions:

- **Any schema file edit moves the id** — including comments and whitespace. Accepted: under
  the ship-together model a spurious id change costs a redeploy that was probably happening
  anyway, and it fails safe (sides refuse to talk when they actually could).
- **The id does not move when a target language is added or the compiler upgrades.** The
  corollary is a real obligation on the compiler, named rather than hidden: **the same schema
  must produce the same wire across compiler versions** — a wire-affecting emitter change
  under an unchanged schema would be a silent false-negative, so the conformance corpus's
  golden wire bytes pin every construct's encoding permanently, and a compiler change that
  breaks a wire golden is a stop-the-line event, never a quiet fix.

*(Two designs preceded this one tonight. Draft 1 hashed a canonical IR and prized
rename-invariance; a cold reader killed that — name-stripped hashes let two builds swap
`health`/`armor` and silently read each other's slots crosswise. Draft 3 hashed the generated
code of every target; deciding C# an hour later exposed the cost — the target set was part of
the id, so adding a language moved every deployed id. File hashing keeps names in the id,
needs no frozen IR encoding, and decouples the id from both the emitters and the target set.)*

*(Challenged externally 2026-08-05: the evaluation argued for a canonical layout hash —
whitespace-insensitive, names kept. Declined, with the trade named: any canonicalized hash
converts the second property above — the id not moving on a compiler upgrade — from a
structural fact into a maintained promise, because every parser/canonicalization change then
risks moving deployed ids. File bytes keep that property structural, and the whitespace cost
fails safe. Revisit only if `schemafmt` (§9 q4) lands, which would make "hash the canonical
form" cheap to define. The full argument is the paragraph above plus
`notes/external-feedback-learnings.md`; the exchange record lives outside this repo.)*

**The hash procedure, pinned so two compiler builds agree — DECIDED (Glenn, 2026-08-05: "yes."; drafted Rowan, 2026-08-05 —
the file-hash decision is Glenn's, this procedure is the mechanism half, and it CHANGES
the input relative to the superseded sentence above — decision list, road-to-v1):** the
unit's `*.schema` files (non-recursive; only the `.schema` extension; a duplicate basename
in an explicit file list is a compile error) ordered by **bytewise ascending basename**;
for each file, hash its basename bytes, then a single `0x00`, then its raw content bytes
(the delimiter prevents two textually different units from concatenating identically, and
puts renames in the hash by construction); the id is the **final 8 bytes of the SHA-256
digest interpreted big-endian** as a uint64. A UTF-8 BOM is rejected at parse (it would
silently move the id). File bytes are the contract — a CRLF checkout moves the id;
repositories should pin LF for `*.schema`.

### 3.2 The unit

A compilation unit is a set of `*.schema` files compiled together (one directory by default).
**Exactly one `package` per unit** — a mismatch is an error; `package` appears once per file,
as its first declaration. Names resolve across all files in the unit — a declaration may sit
in any file, in any order, with no *semantic* effect. **The id is §3.1's file hash: it depends
on the unit's bytes — file layout, declaration order, comments and whitespace included.**
*(This paragraph said "the id depends only on the set of declarations, never on their file
layout" until 2026-08-05 — a true sentence about drafts 1–2's IR hash that survived two hash
reversals; under DECIDED §3.1 it was false, and it is corrected rather than left to disagree.)*
One id per unit, exposed as a constant (`ProtocolId` / `PROTOCOL_ID`) and printed by `schema id`.

**Known consequence, documented rather than hidden:** every byte of the unit contributes to
the id — an unused helper type moves it, exactly as a comment does (§3.1), accepted under the
same rationale: fails safe, and under the ship-together model the redeploy was happening
anyway. Scoping the id to reachable declarations is **not** an available fix while §3.1
stands: reachability is a front-end computation, so excluding unreachable declarations means
hashing something other than raw file bytes — the parser enters the id path, the exact trade
§3.1 declined (§9, open question 6).

`reserved(bits)` fields remain useful *within* a protocol id — not to dodge redeploys (any
claim of reserved bits moves the id like any other change), but to keep packet sizes and
layout budgets stable while a protocol grows into space already paid for.

Whether the id travels on the wire (a connect token, a `const` field, out of band) is the
application's choice — netcode-style stacks already carry one.

## 4. The language

### 4.1 Lexical structure

- UTF-8 source; no BOM — DECIDED (corner-pins list, Glenn 2026-08-05): a BOM is
  rejected at parse, because it would silently move the protocol id (§3.1). `//` line
  comments and `/* */` block comments (non-nesting).
- **Doc comments — DEFERRED, not v1 (Glenn, 2026-08-05: "don't care. leave for later."). The design below is kept for the day it lands:** a `//` comment
  block whose last line immediately precedes a declaration, field, or enum variant — no
  blank line between — is that item's doc comment, carried through the IR and emitted in
  each target's idiom (`///` Rust, `/// <summary>` C#, preceding-line `//` Go, `//` C++),
  so the schema's documentation is the generated headers' documentation. All other comments
  are preserved by `schemafmt` and invisible to codegen. Comments already participate in
  the protocol id (§3.1 hashes file bytes), so doc comments add no new id sensitivity —
  and the comment-carrying scanner they need is machinery `schemafmt` requires anyway.
- **Identifiers:** `[A-Za-z_][A-Za-z0-9_]*`. Conventions, matching the flatbuffers layer the
  language will eventually absorb: type and enum names `UpperCamelCase`, enum variants
  `UpperCamelCase`, **fields `lower_snake_case`**, constants `UpperCamelCase`. The compiler
  does not enforce case, but the corpus and all documentation follow it.
- **The register is Go-inspired — "C-like but not too C"** — DECIDED (Glenn, 2026-08-05):
  types are declared with `type`, not `struct`; declarations put the **name first, then the
  type**, Go's order (*"we can do golang order of setup vars and types"*); scalar names are
  Go's — **`float32` and `float64`, never `float`/`double`** (*"we can use type names from
  golang instead of C++"*); `if` and `switch` take no parentheses; the canonical formatter
  is **`schemafmt`**, gofmt's philosophy included — one style, no options; array bounds are
  a **prefix**, Go's order (`[<= MaxObjects]ObjectState`); fields read flatbuffers-plain. His reasons: *"we don't want to ape C/C++ but write something a bit
  cleaner. Nobody working in C# or Rust will feel like they want to be coding in C++ to
  specify types."* The principle outlives the individual decisions: when a syntax choice
  arises, Go is the model to consult first, the neutral form beats the C-family reflex, and
  between two workable forms the shorter wins — *"I always like, shorter, simpler"* (his
  tiebreak, choosing `[local]` over `[unsynchronized]`). Authorship is not a criterion:
  *"it doesn't matter where the good ideas come from. let the best idea win."*
- **Integer literals:** decimal, hex (`0x`), binary (`0b`). **Float literals** (decimal, with
  optional fraction and exponent) appear in float constants and as float attribute values
  (`min`/`max`/`resolution` on `float32`).
- **Punctuation and operators:** `{ } ( ) [ ] , : = ! . .. <= + - * / %` (maximal munch:
  `..` wins over `.`).
- **Reserved words:** `package const enum type message object if
  else switch case align reserved`
  and the wire-type keywords `bits bool float32 float64 string bytes fixed` plus the
  integer family `int8 int16 int32 int64 uint8 uint16 uint32 uint64 int128 uint128`
  (and `int uint`, reserved so `int` gets a "did you mean int32?" diagnostic instead of
  a parse error). *(`int128`, `uint128` and `fixed` were reserved-for-later until
  2026-08-06, when the serialize runtime's phase-1 surface landed and they went live —
  §4.3, §4.10.)*
  Reserved words cannot be used as names. Attribute keys (`min`, `max`, `resolution`, ...)
  are contextual — they live only inside `[ ]` and are not reserved.
- **Newlines terminate declarations and fields — there are no semicolons, like Go.** The
  newline is a terminator token,
  suppressed: immediately after `{`, `(`, `[`, `,`, `:`, `=`, `else`, and an infix operator;
  and immediately before `)`, `]`, `}`. Blank lines are insignificant. `{` sits on the same
  line as its construct; `} else {` is written on one line — and a newline between `}` and
  `else` is tolerated by the parser (the formatter rewrites it to one line).
  **Two consequences, stated so the grammar is implementable as written** *(repaired
  2026-08-05 — the suppression rule as first stated made every NL-terminated production
  unable to complete before `}`)*: **a closing `}` also terminates the item before it** — in
  every production below, a trailing `NL` is satisfied by an actual newline OR by the
  immediately following `}`; and **end-of-file synthesizes a terminator**, so a file without
  a trailing newline still parses. Within an enum or flags body, newlines around commas are
  ordinary whitespace (suppression after `,`) — variants are not items and need no terminator.

### 4.2 Grammar

EBNF (`NL` = the newline terminator; `{X}` repetition; `[X]` option):

```
File        = { Declaration } .
Declaration = Package | Const | Enum | Flags | TypeDecl | Message | Object | Contexts .
Object      = "object" ident Block NL .
Flags       = "flags" ident [ Attributes ] VariantList NL .        // "flags" contextual, §4.2
Contexts    = "contexts" VariantList NL .                          // "contexts" contextual, §4.2;
                                                                   // one list per unit
Package     = "package" ident NL .
Const       = "const" ident [ ConstType ] "=" ConstExpr NL .
ConstType   = IntType | "float32" | "float64" .
ConstExpr   = IntExpr | FloatExpr .
Enum        = "enum" ident [ Attributes ] VariantList NL .
VariantList = "{" [ ident { "," ident } [ "," ] ] "}" .            // commas; trailing comma OK
TypeDecl    = ( "type" | "table" ) ident [ Attributes ] Block NL .  // attribute = the type TAG, §4.2;
                                                            // "table" = a table-wire/reflection
                                                            // ROOT (§4.11)
Message     = "message" ident Block NL .

Block       = "{" { Item } "}" .
Item        = Field | ConstField | Reserved | Align | If .
Field       = ident Type [ Attributes ] [ "=" Default ] NL .
Default     = ConstExpr | ident .                    // specified default (see below):
                                                     // ident = true | false | an enum variant
ConstField  = "const" "(" IntExpr "," IntExpr ")" NL .          // (value, bits)
Reserved    = "reserved" "(" IntExpr ")" NL .
Align       = "align" NL .

Type        = [ "[" Bound "]" ] Scalar .                         // array bound is a PREFIX, Go's order
Scalar      = IntType
            | "int128" | "uint128"                               // 128-bit integers (§4.3);
                                                                 // field types only, not ConstType
            | "bits" "(" IntExpr ")"
            | "bool" | "float32" | "float64"
            | "string" "(" IntExpr ")"
            | "bytes" "(" IntExpr ")"
            | "fixed" "(" IntExpr "," IntExpr ")"                // fixed(I, F) — signed Q I.F (§4.3);
                                                                 // the Q format is the type's SHAPE,
                                                                 // so it is positional like bits(N)
            | ident .                                            // a declared type or enum
IntType     = "int8" | "int16" | "int32" | "int64"
            | "uint8" | "uint16" | "uint32" | "uint64" .
Bound       = IntExpr | "<=" IntExpr | IntExpr ".." IntExpr .

Attributes  = "[" Attr { "," Attr } "]" .                        // trailing, optional, per field
Attr        = ident "=" ( ConstExpr | ident )                    // valued:    min = 0, round = up
            | ident .                                            // valueless: interpolate, local
                                                                 // (ident values are keyword-like:
                                                                 // each valued key declares whether
                                                                 // it takes an expression or a word)

If          = "if" [ "!" ] ident Block [ "else" Block ] NL .



                                                                 // (an ident that is a const
                                                                 // name resolves as IntExpr;
                                                                 // enum-subject labels resolve
                                                                 // as variants first)

IntExpr     = integer expression over literals, const names, and enum max
              references ( ident "." "Max" ):
              "+" "-" "*" "/" "%", unary "-", parentheses.
FloatExpr   = float expression over float literals, int literals and const names:
              "+" "-" "*" "/", unary "-", parentheses — float64 arithmetic, no "%".
```

- **Integer expressions** evaluate at compile time in arbitrary precision, Go's
  untyped-constant style, with a fit-check at each use site — so `0xFFFFFFFFFFFFFFFF` is
  writable for a full-range `uint64` bound while overflow at a use site stays a compile
  error *(was "checked signed 64-bit", which made every uint64 value ≥ 2^63 unwritable —
  repaired 2026-08-05)*. Division by zero, and a negative result where a width, bound or
  count is required, are compile errors. Integer division truncates toward zero, Go's rule.
  **Float attribute values (`min`/`max`/`resolution` on `float32`) are float expressions** —
  literals, const names, and arithmetic, per `FloatExpr`; integer constants convert in float
  positions *(this bullet said "float literals only" while the typed-constants bullet below
  said constants convert there — the DECIDED typed-constants rule wins; repaired 2026-08-05)*.
- **Zero initialization is the rule, in every generated language — DECIDED (Glenn,
  2026-08-05: *"can we use golang's rule of zero initialization for all types in all
  generated langs"*), with an optional SPECIFIED DEFAULT overriding it (his same message:
  *"unless we allow some specified default, eg. ... = true"*).** Every generated type
  fully initializes to zero values on construction — 0, 0.0, false, `None` for an enum, 0
  for a flags mask, zeroed buffers and counts, recursively for nested types — in all four
  targets (C++ emits default member initializers; Go/C#/Rust get it from their languages'
  own rules, stated so the C++ target cannot silently be the odd one out). A field may
  override its zero with `= value` after the attributes: `invulnerable bool [local] = true`.
  v1 defaults cover bool (`true`/`false`), integer and float fields (constant
  expressions, fit-checked like any use site) and enum fields (a variant name); arrays,
  strings, bytes and composite fields zero-initialize with no override. **The default is
  STORAGE initialization — what a freshly constructed object holds. It does not touch the
  wire, and §5's read rule is deliberately unchanged: fields in untaken branches read as
  ZERO values, not as defaults** — the wire contract stays a pure function of the schema's
  encodings, and whether untaken branches should instead restore defaults is a question
  for Glenn if a real case ever wants it.
- **Constants compose** — DECIDED (Glenn, 2026-08-05: *"constants can refer to other
  constants, so we can build up things, const C = A * 2 + B"*). A `const` may reference any
  other `const` in the unit, order-free across files like type references; reference cycles
  are a compile error naming the cycle. The corpus exercises it:
  `const MaxPositionUnits = MaxWorldMeters * PositionUnits`.
- **Constants are typed, and not just integers** — DECIDED (Glenn, 2026-08-05: *"They won't
  just be integer"*), on Go's untyped-constant model: a bare `const` takes its **kind** from
  its expression — integer (arbitrary-precision arithmetic, fit-checked at each use —
  the expression rule above) or float (float64 arithmetic;
  `+ - * /` and parentheses, no `%`) — and converts wherever that kind fits: integer
  constants in any range, bound, width, case label or float position; float constants in
  float attribute values (`resolution`, quantize scales) and float contexts. An **explicit
  type makes a typed constant** — `const MaxBounds uint64 = 12000` — which pins the exported
  type in every target. The worked migration case is `definitions.fbs`: its thirteen
  constants-hacked-as-single-value-enums (`MaxPlayers`, `MaxObjects`, `MaxShips`, ...,
  `MaxBounds : uint64`) become thirteen `const` lines in `Constants.schema`, generated into
  C++ exactly as game code consumes them today — *"the same set of constants (non-enum
  hacked this time)."* **The second migration source is `game/Source/Constants.h`** (194
  lines, surveyed 2026-08-05), sorting into four piles: **straight migrations** (the numeric
  limits and tuning — including six-plus float/double constants, the existence proof for
  float `const`); **absorbed by generated sets** (`OBJECT_TYPE_*`/`NUM_OBJECT_TYPES` die
  into the generated `ObjectType`; packet-type ids into the message/packet tag story;
  `DELTA_ACTION_*` into the delta pass); **already-covered bridges** (`MAX_PLAYERS` etc.
  alias the fake-enum constants above); and **two new language needs, filed as open
  questions** — const expressions over enum counts (`NUM_TEAMS = Team_MAX + 1`, `MAX_PROPS`
  as a sum) and platform-conditional constants (`FRAME_SAFETY` differs under `__linux__`).
- **Enum max references — DECIDED (Glenn, 2026-08-05: *"yes, but prefer Team.Max"*):**
  **`E.Max`** in any integer expression names enum `E`'s max — the derived count, or the
  widened `[max = K]` — the same number the enum's wire range and storage already derive.
  The count of wire values is `E.Max + 1` by ordinary constant arithmetic, and sizing
  enum-indexed tables with it stays correct under headroom, where non-variant values are
  wire-legal (§6.1). The worked migration is `Constants.h`'s `NUM_TEAMS = ( Team_MAX + 1 )`
  becoming `const NumTeams = Team.Max + 1`, exported to all four targets like any const; it
  works on generated sets too (`MessageType.Max`). `Max` after `.` is contextual, like
  attribute keys, and capitalized to match the UpperCamelCase constants convention — his
  spelling; the lexer keeps maximal munch so `..` still wins over `.`. *(`len(Team)` was
  considered and declined: three plausible meanings, and every one of them sizes
  enum-indexed tables wrong under headroom — the max is the true primitive. The other half
  of q7 as originally filed — `MAX_PROPS` as a sum — needed nothing: constant composition
  already expresses it.)*
- **Constants are platform-uniform — DECIDED (Glenn, 2026-08-05: *"yes, because server and
  clients will be on different platforms, but must always be able to communicate."*), a rule
  with a reason rather than an economy:** a schema has no platform conditionals. The protocol
  id hashes the schema files (§3.1), so one schema is one id on every platform — a
  platform-conditional constant reaching any range, bound or width would let two builds carry
  the same id with different wires, the exact bug class the id exists to make impossible. The
  survey's one platform-conditional (`FRAME_SAFETY`, two call sites, zero wire contact) stays
  hand-written in game code; if it ever wants central ownership it is tweakable timing
  tuning — config-layer material, not a schema constant.
- Inside a type body one token of lookahead disambiguates every item: `const` + `(` is a
  constant field (versus the top-level `const name =` declaration); `if` / `switch` /
  `align` / `reserved` are keywords; any other identifier begins a field. The grammar is
  LL(2) and hand-written recursive descent is the intended implementation.
- **Enum variants** are comma-separated identifiers, trailing comma allowed — DECIDED
  (Glenn, 2026-08-05: *"i prefer commas between enum definitions, flag definitions, lists
  in general."* / *"more flexible than space separated."*). **Every enum
  has `None = 0` implicitly — universal, never declared** (Glenn, 2026-08-05: *"all enums
  should have 0 = none, and then the max value above all tightly packed values"*); declared
  variants pack tightly from 1, max derives as the count (the `[max = K]` headroom attribute
  can still widen the wire). **Enum storage derives from the enum's own max** — the smallest
  unsigned integer that fits (his ask: *"it works out the underlying integer type based on
  the size of the enum values"*).
- **CANONICAL FORM ONLY — DECIDED (Glenn, 2026-08-05):** *"canonical enums are in the form
  0 = none, [1,max], dense non-sparse values."* / *"only enums i care about are in the
  canonical form."* **Explicit variant values are DECLINED, permanently** — no `= value`,
  no sparse enums, ever. Variants are comma-separated on his later preference the same
  day (the foreclosure argument that once tied the separator to `= value` is moot — the
  values are declined regardless). A drafted pinned-values proposal died here on his
  word; the record of it is in git.
- **THE ENUM FAMILY IS TWO DECLARATION FORMS** *(it was three for part of 2026-08-05 —
  `enum_index` was designed at the type level on Glenn's direction* ("suspect it's better
  done as a type... vs. doing it at serialize level")*, then CUT the same evening:
  "Let's cut enum_index for now." Its design — wire = value − 1 in [0, max − 1], storage
  keeping 0 as an unset sentinel — is recorded at §9 q11 for the day it returns, likely
  beside the claiming/index pass. The v1 cost of the cut, stated plainly: the surveyed
  create path's `[1, max]` wire is not expressible — a kind field is a plain `enum` and
  spends the unused None wire value, examples/README.md finding 1's original state.)*:
  - **`enum`** — the canonical with-None form above. Wire: raw value, **[0, max]**; `None`
    is a legal wire value (the null — the generated `MessageType`/`ObjectType` shape).
  - **`flags Name { ... }` — DECIDED (Glenn, 2026-08-05:** *"yes, we can and should
    support flags too. flags are uint64_t, one field per-bit, up to 64."* — and the
    keyword is his: *"dislike enum_flags, just flags."***) Mechanism:** each variant names
    one bit, assigned densely from bit 0 in declaration order, **up to 64**; **storage is
    `uint64` in every target** (his word); no implicit `None` — the empty mask is 0 and
    needs no name. Wire: **W raw bits, W = variant count** (`[max = K]` widens to K bits);
    every W-bit pattern is legal — a mask field's domain is all subsets. Each target
    exports one mask constant per variant (`1 << bit`), because mask tests are how flag
    state is consumed. **`flags` is a CONTEXTUAL keyword, not reserved** — it introduces a
    declaration at file scope only, so a *field* named `flags` (the real corpus's
    `Ship.flags`) stays fully legal; declarations at file scope otherwise begin with
    reserved words, so one token of lookahead disambiguates. *(Evidence: one named flags
    enum plus three anonymous `uint64` flags fields fly in the surveyed game with
    hand-kept masks.)*
  - **The derived-range rule, normative:** a field that indexes a declared set derives its
    range from the set, never restates it — both wires derive from the declaration;
    `min`/`max` are not valid on enum-family fields. *(The wider pattern — integer indexes
    over declared sets and collections — is the horizon's everything-is-an-index feature;
    `index` stays informally reserved, and its ranges will derive under this same rule.)*
  - Grammar: `Declaration` gains `Flags = "flags" ident [ Attributes ] VariantList NL` —
    `flags` contextual per the note above; `VariantList` is the comma form shared with
    `enum`.
- **Type references are order-free** — a type or enum may be used before its declaration,
  in any file of the unit. (Field *back-references* are not order-free; §4.5.)
- `if` nests freely inside blocks (`switch` is cut for now, §4.4).

### Attributes — per-field, optional, keyed — DECIDED

**Ranges and refinements are trailing per-field attributes in `[ ]`, not call syntax**
(Glenn, 2026-08-05: *"a more go like thing, where per-variable there are attributes
per-variable (optional) instead of the (min,max)"* — *"this way we can express multiple
things per-variable as needed"*; brackets his call the same day, **with his hedge kept
attached: "just a personal preference, could be wrong but let's see in time"** — held
loosely, cheap to revisit while nothing is implemented):

```
health      int16   [min = 0, max = MaxHealth]
thrust      int8    [min = 0, max = 100]
orientation float32 [min = -180.0, max = 180.0, resolution = 0.01]
sequence    uint16
```

- **Brackets never collide with array bounds, structurally** (Glenn, 2026-08-05: *"We can do
  the golang array thing, eg. prefix [] before the type... helps"*): an array bound is a
  **prefix** — `[<= MaxObjects]ObjectState`, Go's order — and attributes **trail** the
  complete type. Position alone disambiguates; no lookahead. Scalar constraints like
  `min`/`max` apply per element.
- **Valueless attributes are in** — deferred in the first draft of this section, restored
  the same day when the real need arrived: **object view markers** (§4.8) need
  `[interpolate]` and `[local]`, and the prefix-array decision had already removed the
  collision that motivated the deferral. **Attribute lists stay flat** (Glenn:
  `[interpolate, min = x, max = y]`) — a marker and its encoding parameters sit side by
  side; no nested argument syntax.

- **The line between positional and attribute:** a *size* that defines the type's shape stays
  positional — `bits(64)`, `string(64)`, `bytes(N)`, array bounds. A *constraint or
  refinement* of a named type is an attribute. The enum's `[max = 15]` was already this
  syntax; it is now the one general mechanism.
- **The vocabulary is typed and closed per compiler version — an unknown attribute is a
  compile error**, never a silently ignored string (the anti-Go-struct-tag decision: keyed
  like Go, checked like Go is not). v1: integers take `min`/`max` (both together or
  neither); `float32` takes `min`/`max`/`resolution` (all three together — see §4.3: this IS
  the compressed float); `enum` declarations take `max`; **the valueless view markers
  `interpolate` and `local` are in the vocabulary for `object` bodies** (§4.8 — this
  enumeration lagged the valueless-markers decision above until 2026-08-05, which made the
  spec's own §4.8 examples compile errors), **and so is `context = <name>`, legal only
  beside `[local]`** (§4.2 Contexts — the same lag, caught by a cold audit the same day
  the corpus started using it). Still PROPOSED, riding the corpus review:
  `quantize`/`round` inside the §4.8 view-encoding rules. *(The former `[no_none]` and
  `[unit]` markers are dead — their jobs moved to the type-tag mechanism and
  the type-tag mechanism below; a built-in `quat` and a class vocabulary each lived for
  under an hour on the way to it.)*
- **Attributes are the horizon's attachment point.** Per-field delta tiers, interpolation
  modes, struct-mapping targets — *"what properties and attributes per-property"* — land
  here as new keys with new generator passes, without touching the grammar again.

### Type tags — DECIDED (Glenn, 2026-08-05): types are the user's; meaning is claimed later

**The language pre-defines NO composite types** — no built-in vec3, no built-in quat, no
privileged class list (Glenn: *"we don't have a pre-defined set of types"* / *"it's meant
to be something bigger than space game, and somebody should be able to define their own
types in there"*). A user declares a type and may **tag** it:

```
type Vec3 [vec3] {
    x float64
    y float64
    z float64
}

type Quat [quat4] {
    x float64
    y float64
    z float64
    w float64
}
```

- **The type is entirely the user's** — name, body, component precision. The body alone
  defines storage and wire, like any type; a declared type composes anywhere in the unit,
  order-free — *"as if a keyword from that point forward"* (his phrase; it is the
  language's existing composition rule).
- **The tag is one user-chosen identifier, in its own namespace** — any identifier is
  legal there, unlike field-attribute keys, which stay closed and checked. **In v1 a tag
  is inert**: parsed, carried through the IR, emitted as an annotation on the generated
  type, and it changes zero generated code.
- **Meaning arrives by CLAIMING** (his call, over both a built-in type set and a
  compiler-known class vocabulary: *"I like the idea of claiming these tags and assigning
  meaning and 'how to' interpolate and do stuff with these types."*): a future pass — the
  delta pass first — claims a tag and assigns its semantics and generated actions
  (interpolation, prediction, normalization, render mapping). Claiming is an ordinary
  compiler-version event; the protocol id never moves for it (the id hashes the schema
  files, and the files already carried the tag). The claiming pass defines its own shape
  validation for claimed tags (e.g. `[quat4]` requiring four float components) and its
  own policy toward unclaimed strangers.
- **The architecture in his words:** *"so it's more like there is a series of types, and
  then there is something that defines needed actions for those types."* Types on one
  side; a layer that defines the needed actions for those types on the other. v1 ships
  the types; the actions layer opens with the delta pass — *"but now the set of types is
  entirely settable in schema language for deltas later on."* And the provenance, which
  is the best argument for it: *"this is really what i was doing btw. with the manually
  coded boiler plate for interpolate and quantize/unquantize"* — *"it's per-type but
  these types are for my game, they may not be general."* His hand-written code is
  already plain type structs plus separate per-type action functions, and those types
  are one game's, not the world's — which is precisely why the language pre-defines
  none: each schema declares its own types, and each game's actions bind to them by
  claiming. The tag mechanism is that practice made declarative, not an invention. The
  closing argument is his: *"it also lets my types be minimal for what my game needs,
  vs. trying to implement a set of first class types that are sufficient for any game
  (which is impossible)."* And the clincher from his own roadmap: *"all the types will
  change soon, we'll go fully fixed point in space game, and all scalars, quats and
  vectors will become fixed point, so these hard coded types are immediately obsolete."*
  — under user-defined types, the fixed-point migration (§4.10, runtime-first) is a
  re-declaration in his schemas, not a language change.

### Native type mapping — `cpp_native` / `cpp_include` (2026-08-12)

The proven derivation pattern (space game, the Render migration): the compiler generates
the **basis struct** — data members only, no behavior — and a hand math type **derives**
from it, adding operators and methods without touching layout:

```
type Vector3 [vec3, cpp_native = Vector3, cpp_include = "core_vector.h"] { ... }
```

```cpp
// core_vector.h, hand-written:
struct Vector3 : public space::Vector3 { /* operators, length(), ... */ };
```

With the mapping declared, generated C++ **storage speaks the native type**: every field
of a mapped type in every generated struct (states, data views, render types, tables)
declares `::Vector3` instead of `space::Vector3`, and every generated header that
references it emits `#include "core_vector.h"`. Simulation code then does math on
generated state directly — no conversion layer, no per-site casts. This is what
flatbuffers' `native_type` attribute gestured at, done soundly: derivation guarantees
layout identity (the emitted `static_assert`s — trivially-copyable, standard-layout —
still hold over native-typed storage), so relocatability and the parallel scatter/gather
property survive untouched.

The rules:

- **`cpp_native` takes an identifier** — the global (`::`-qualified) C++ type name.
  **`cpp_include` takes a quoted header path** — the header declaring it. They go
  together: one without the other is an error. String literals (`"..."`, no escapes)
  exist in the grammar for attribute values only.
- **Inside the basis type's own generated header the mapping is off** — references there
  keep the basis name. The native header includes that header to derive from the basis;
  a mapped reference would be circular. Sibling types declared in the same schema file
  therefore store the basis type, which is the correct default for pure wire compounds.
- **Language bindings never move the protocol id.** `cpp_*` attrs rename what one target
  CALLS the storage; they cannot change a wire bit. The canonical fingerprint skips
  them. (The protocol id itself hashes source files (§3.1), so editing a schema file to
  ADD a mapping still moves the id — the exclusion is about the semantic fingerprint,
  and about never letting a binding masquerade as a wire change.)
- **C++ only, by prefix.** Other targets ignore `cpp_*` keys. If C# or Go ever earn a
  native mapping, they get their own prefixed keys and their own soundness argument;
  nothing is shared but the spelling convention.

### Contexts — the idea is DECIDED (Glenn, 2026-08-05), the mechanism PROPOSED

**The need, his words:** *"OK regarding client/server #ifdefs, they're important!
Basically, i was using #if GAME_CLIENT and #if GAME_SERVER to specify differences in
types (always local...), on client and server, because they had to do slightly different
work."* And the constraint that shapes the answer: preprocessor emission is out —
*"this isn't good in a generated code, and languages that we are outputting to don't all
have preprocessor, so we can't do that."* **His design:** *"Idea. Contexts.schema where
we list contexts (in this case 'client' and 'server')"* — contexts are declared in the
schema, not baked into the language, exactly like the types.

**The PROPOSED mechanism (Rowan, 2026-08-05):**

```
// Contexts.schema
package example

contexts { client, server }
```

- **`contexts { ... }`** — one comma-separated list per unit (the same `VariantList` form
  as enums; `contexts` is contextual at file scope, like `flags`). Context names are
  user-chosen; lowercase by his usage.
- **Per-local field, an optional `context = <name>` attribute** — his exact form: *"and
  then per-local field, we can optionally say context = client, default attribute =
  context all."* So: `predicted_explode bool [local, context = client]`; an unscoped
  field defaults to `context = all` (every context); `all` is reserved and cannot be
  declared. **`context` is legal only beside `[local]`** — a wire field with a context is
  a compile error, because the wire must be identical on every side. The `context` key's
  legal values are closed *by the unit's own `contexts` declaration* rather than by the
  compiler.
- **Generation resolves the variance — no preprocessor anywhere, one output, a TYPE per
  context.** His form: *"Then when we have multiple contexts per-type, we can generate a
  type per-context, called, for example ClientShip, ServerShip (where it is the snake ->
  capital case for the context name)."* When a type or object carries context-scoped
  fields, the generator emits one variant per context, named by capitalizing the context
  onto the type — `ClientShipState`, `ServerShipState` — each holding the `all` fields
  plus its own context's; a type with no context-scoped fields generates once, unprefixed.
  Each side's code uses its own type, which is what the `#if` was approximating by hand.
  His ratification, with the escape hatch noted: *"this will work across all languages,
  and if i want to do funny stuff in C++ with #ifdefs, I can just #define Ship ClientShip
  if i really care to"* — the language stays preprocessor-free and the application keeps
  its freedom.
- **Wire and protocol id are context-invariant by construction:** context fields are
  local-only, so every context generates byte-identical wire code, and the id (which
  hashes the schema files, `Contexts.schema` included) is one id for all contexts.

### Constants and enums are exported — DECIDED

Glenn, 2026-08-04: *"We'll need support for constants too, because those are referred to from
the serialize functions"* — *"and enums."* Both are first-class in both directions:

- **Into the wire:** a `const` is usable in any range, bound, width or case label, folded at
  compile time; an enum is a wire type.
- **Out to the code:** every `const` and every `enum` is **exported in the generated output
  of all four targets** — `inline constexpr int64_t MaxPlayers = ...;` /
  `public const long MaxPlayers = ...;` / `const MaxPlayers int64 = ...` /
  `pub const MAX_PLAYERS: i64 = ...;` and the integer-backed enum types of
  §6.1 — because the values that shape the wire are the same values game code sizes its
  arrays and loops with. One declaration, everywhere, and the game references the schema's
  constant instead of a hand-copied twin.

The evidence this is load-bearing sits in Space Game's current flatbuffers layer:
**flatbuffers has no constants, so today's `definitions.fbs` encodes them as single-value
enums** — a real constant declaration is one of the concrete things schema improves on day
one. *(v1 constants are integers; if the corpus shows serialize functions referring to named
float constants — e.g. ranges for compressed floats — `const` widens to floats in that design
pass.)*

**The sentinel-zero counting convention — DECIDED (Glenn, 2026-08-06).** Entry 0 of every
enum is the none/sentinel value, so **the count of real things is always `Enum.Max`, never
`Enum.Max + 1`**. His words: *"0 is the 'none' or sentinel... This is the case for all enums,
so if you are to count the number of things, it is always going to be `<EnumName>.Max`"* —
with his two worked examples: `Team { None, Red, Blue }` → two real teams, `NumTeams =
Team.Max = 2`; `ShipType_None, ShipType_A, ShipType_B, ShipType_C` → three real ship types,
`ShipType.Max = 3`. A convention over the corpus and generated-code usage, not a language
rule — the language does not enforce entry 0's meaning — and the corpus follows it
everywhere (the 2026-08-06 `NumTeams` correction is the worked instance). **The why, his
words:** *"this lets us pass in the 'null', eg. 'no team' in the same variable that
specifies the team"* — optionality rides in-band, no separate has-flag. The
counterfactual, also his: *"without this, we have to do lots of annoying serialize
everywhere like, `bool hasTeam, enum team`, and check the `if hasTeam` every time.
Stupid."* And it composes with §4.2's zero-initialization: a zero-initialized enum field
**is** `None` by construction, so a fresh struct starts in the null state without any
code saying so.

### 4.3 Field types and their wire encodings

**The wire model — normative** *(added 2026-08-05: the table below defined every construct
only by pointing at its classic twin, whose contract lives outside this document; an
implementer working from the spec alone could not produce one correct byte. These are the
family's measured invariants, restated here as the spec's own)*:

- **Bit order is LSB-first**: successive values OR into the stream at increasing bit
  offsets, no per-value reversal — **bit i of the stream lives in byte i/8 at bit position
  i%8.**
- The writer accumulates into a 64-bit scratch flushed as **little-endian words**; the final
  flush zero-fills — the stream's logical length is **ceil(bits/8) bytes**, and `Write`
  returns exactly that.
- **All >32-bit quantities go low 32 bits first**, then the high remainder (`bits(N>32)`,
  `uint64`, `float64`, ranged encodings wider than 32 bits). **The 128-bit family and
  fixed point generalize the same rule: the value splits into full 32-bit groups from
  the least significant upward, the final group carrying the remainder, up to four
  groups** (STANDARD.md's own statement of the split).
- **Ranged integers encode `value − min`, unsigned, in exactly `bits_required(min, max)`
  bits** — the bit width of (max − min), computed in the range's own width (clz over 32 or
  64 bits as the range demands); no zigzag on the wire, ever.
- **`align` emits zero bits up to the next byte boundary — zero bits when already
  aligned**; readers verify the padding is zero.
- **Length prefixes** (`string(N)`, `bytes(N)`, `[<= N]T`, `[Min..N]T`) are ranged
  integers over their declared count range, per the row below.

The wire encodings are exactly classic serialize's — each row names its classic twin, which is
the wire oracle for the stated model. (Extracted contracts: `notes/serialize-cpp-api.md` and
siblings — design inputs, re-verified against library source at implementation time.)

| schema | wire | classic twin |
|---|---|---|
| `f bits(N)` | N raw bits, N in [1,64] | `serialize_bits` ([1,64]; >32 = low 32 bits first, then the high remainder) |
| `f intN` / `f uintN` (bare, N ∈ 8/16/32/64) | N raw bits (two's complement for signed) | `serialize_uint8/16/32/64`; signed raw is the same bits, cast |
| `f intN [min = A, max = B]` / `f uintN [min = A, max = B]` | minimal bits for the range, value − A; read rejects out-of-range; **the range must fit the declared storage** | `serialize_int` (≤32-bit int ranges) / `serialize_int64` / width-computed `serialize_bits` for full-unsigned ranges |
| `f uint128` (bare ONLY) | 128 raw bits — the low 64-bit half first, then the high half; representation-independent (native `__int128` and the emulated two-lane pair produce identical bytes) | `serialize_uint128` |
| `f int128 [min = A, max = B]` (range REQUIRED) | value − A in `bits_required128(A, B)` bits, 32-bit groups from the bottom; read rejects an offset above B − A — reject, never clamp; **where the range fits 64 bits or fewer the bytes are identical to `serialize_int64` over the same bounds.** Bare `int128` and ranged `uint128` are compile errors — serialize's own surface, mirrored exactly (uint→raw, int→ranged) | `serialize_int128` |
| `f fixed(I, F) [min = A, max = B]` (range REQUIRED; **SIGNED** — Glenn 2026-08-06, the unsigned spelling is OPEN, §9 q17) | Q I.F, the sign bit counting toward I; storage is a signed integer of exactly I+F bits (I+F ∈ 8/16/32/64/128, I ≥ 1, F ≥ 0); bounds are compile-time WHOLE UNITS fitting the Q format and int64; wire = raw − (A << F) in bitlen(B − A) + F bits, 32-bit groups from the bottom; read rejects above the raw range; round trip is EXACT (no quantization step), and with F = 0 the operation IS a ranged integer | `serialize_fixed` |
| `f bool` | 1 bit | `serialize_bool` |
| `f float32` | 32 raw IEEE-754 bits | `serialize_float` |
| `f float64` | 64 raw bits (low dword first) | `serialize_double` |
| `f float32 [min = A, max = B, resolution = R]` | quantized to ceil((B−A)/R) steps — the actual step is (B−A)/ceil((B−A)/R), ≤ R *(the row said "R-sized steps", exact only when (B−A)/R is integral — corrected 2026-08-05)*; read rejects values above the step count | `serialize_compressed_float` (exact formulas incl. the ceil, +0.5f rounding and clamp) — **the former `compressed_float` keyword, dissolved into attributes: storage stays `float32`, the attributes describe the wire** |
| `f Weapon` (an enum) | minimal bits for [0, max]; read rejects above max | `serialize_int` over [0, max] |
| ~~`f Kind` (an `enum_index`)~~ — CUT for now (Glenn, 2026-08-05); the design is recorded at §9 q11 | — | — |
| `f Damage` (a `flags` declaration, §4.2) | W raw bits, W = variant count (or [max]); every pattern legal; storage `uint64` in every target | `serialize_bits` |
| `f Inner` (a type) | Inner's fields, in place | `serialize_object` |
| `const(Value, Bits)` | the constant; read **rejects** any other value | `serialize_bits` + compare |
| `reserved(Bits)` | zeros; read rejects nonzero | `serialize_bits` + compare |
| `align` | zero-pad to the next byte boundary; read rejects nonzero padding | `serialize_align` |
| `f string(N)` | length in [0, N], align, then the used bytes — N = max length; the bound sizes the length prefix's bits | `serialize_string` with buffer N + 1 |
| `f bytes(N)` | identical shape: length in [0, N], align, then the used bytes | `serialize_int` + `serialize_bytes` |
| `f [N]T` | N elements, back to back | element per element |
| `f [<= N]T` / `f [Min..N]T` | count in [Min, N] encoded relative to Min, then that many elements | `serialize_int` + the element loop |

Arrays: the element may be any scalar or named type; arrays of arrays are not in v1 (wrap the
inner array in a type). Runtime-count arrays carry their own count on the wire — there is no
separately-declared count field in v1 (draft 1 sketched a back-referenced `: count(field)`
form; it was cut after a reader showed the draft's own example violated the draft's own
back-reference rule with it).

**Wire fidelity.** For every legal write, the bits are identical to the named classic twins —
serialize.modern's one documented deviation (`wstring_` alignment) does not arise because
schema emits sequential stream operations, and wide strings are deferred anyway. On the
*read* side, schema's generated readers enforce the language's validation rules uniformly
(e.g. the interior-null rule of §4.7), which can be stricter than a hand-written classic
reader; acceptance is uniform across the four targets, which is what the conformance gates
check.

**The count-range cap is lifted.** serialize.modern caps `array_n`'s count range at 16 because
each possible count is a separately spliced compile-time path. schema's generated code uses an
honest loop (§6.2), so `[<= N]T` is bounded only by what the count's integer range can
express.

### 4.4 Decisions: `if` — and `switch` is CUT for now

Conditional serialization branches on a previously serialized field — a back-reference. The
branch itself costs no wire bits; the referenced field was already paid for.

```
type Body {
    position Vec
    at_rest  bool
    if !at_rest {
        velocity         Vec
        angular_velocity Vec
    }
}
```

- `if cond { ... } else { ... }` — `cond` is a previously serialized `bool` field, optionally
  negated. Wire = the taken side only. A `bool` field followed by an `if` on it produces the
  identical wire to serialize.modern's fused `branch`.
- **`switch` / `case` — CUT from v1 (Glenn, 2026-08-05: *"cut for now."* / *"simple as
  possible first pass."*).** The trigger: the corpus's only `switch` example was removed as
  not useful, and §7.3's own doctrine says a construct no example needs is a construct v1
  may not need; the generated message/object dispatch never used it (that surface is
  compiler-emitted, §4.8). The keywords stay reserved, and the design is preserved here for
  its return: `switch field { case ... }` over a previously serialized integer or enum
  field; wire = the matching case's fields, a value matching no case serializing nothing,
  identically both directions; case labels are bare variant names for an enum subject and
  constant integer expressions for an integer subject; duplicate case values a compile
  error; `bits(N)` a legal subject; `case None:` legal. *(The corner rule that survives
  the cut because it binds enums generally: a user-declared variant literally named `None`
  is a compile error — the name is claimed by the implicit rule.)*

### 4.5 Back-references: the dominance rule

The referenced field must be declared **earlier in the same block or in an enclosing block** —
never in a sibling branch side, or inside an array element. Equivalently: the
referent is serialized on every path that reaches the reference. This is a lexical-scope
check in the resolver.

```
    has_delta bool
    if has_delta { ... }      // legal: same block, earlier — dominated
```

Forward references, references into a sibling branch side, and references to array
element fields are compile errors naming the offending reference and the rule. *(Draft 1
required the referent be unconditionally serialized at type top level, which outlawed the
example above and contradicted the fused-branch subsumption claim; the dominance rule is
strictly more expressive and equally static. The rule is stated over `if` sides; it
extends unchanged to `case` bodies when `switch` returns.)*

### 4.6 Shape checks

All compile errors with positions:

- `bits`, `const`, `reserved` widths outside [1,64]; a `const` value that does not fit its
  width.
- **The fixed and 128-bit family rules (2026-08-06, serialize's static_asserts restated
  as language rules so generated code cannot fail to compile):** `fixed(I, F)` requires
  I ≥ 1 (the sign bit counts toward I), F ≥ 0, and I + F equal to a storage width —
  8, 16, 32, 64 or 128; a `fixed` field requires `[min = A, max = B]` in whole units,
  A < B, both fitting the Q format's whole-unit domain [−2^(I−1), 2^(I−1) − 1] AND
  int64 (where the runtime's compile-time bound parameters live); `int128` requires
  `[min = A, max = B]` (bare `int128` is a compile error — serialize has no raw signed
  128-bit operation); `uint128` refuses `min`/`max` (ranged 128-bit is `int128`);
  specified defaults cover `int128`/`uint128` (fit-checked like any integer) but NOT
  `fixed` — a raw-scaled default invites unit confusion, and the door reopens with a
  real case.
- **A range that does not fit its declared storage:** `int8 [min = 0, max = 1000]` is a
  compile error — the range determines the wire, the type name determines the storage, and a
  legal wire value the storage truncates would be silent corruption that passes read
  validation. (This check left the spec in draft 2 as vestigial — storage was derived then;
  with the integer family naming storage explicitly, it returns with a real job. *The worked
  example here carried the dead call syntax `int8(0, 1000)` until 2026-08-05 — unparseable
  under the grammar since attributes landed.*)
- **Attribute discipline:** an unknown attribute key, a key repeated, an attribute on a type
  that does not take it, `min` without `max` (or vice versa), and `resolution` without both
  bounds are compile errors, each naming the field and the legal vocabulary for its type.
  **One scoped exception, stated rather than implied** *(the corpus sat on this conflict
  for a day — six fields — before a cold audit caught it, 2026-08-05)*: in the composite
  quantization form `[interpolate, quantize = K, max = B]` (§4.8 rule 2), `max` is the
  quantize bound and appears WITHOUT `min` by design — `min` is forbidden there, the
  domain being symmetric [-B, +B].
- **Degenerate ranges:** any ranged integer with min ≥ max (the diagnostic suggests `const`
  for a fixed value); `[Min..N]T` with Min ≥ N; `string(N)` with N < 2; `bytes(<= N)` with
  N < 1; **`bytes(N)` with N < 1; `[N]T` with N < 1; `[<= N]T` with N < 1** *(three holes
  closed 2026-08-05)*; an enum whose max is 0 (fewer than two wire values); `resolution` ≤ 0.
  Every runtime treats `min == max` range serialization as API misuse, so the language
  rejects what the runtimes would reject. An empty `type` or `message` body is legal — zero
  wire bits, the Heartbeat precedent; an empty `object` body is an error (it generates a
  meaningless view family); a unit with no `.schema` files is an error.
- **One flat namespace, and the compiler's claimed names — DECIDED (corner-pins list, Glenn 2026-08-05):**
  all five declaration kinds share one unit-level namespace (`const Foo` and `type Foo`
  collide). A unit with `message` declarations may not declare `MessageType` (already
  stated in §4.8); symmetrically, a unit with `object` declarations may not declare
  `ObjectType`, nor — per object `X` — the generated family names (`XState`, `XData_Deep`,
  `XData_Shallow`, `XData_Interpolate`); and no unit declares `ProtocolId`. Diagnostics name
  the generated artifact that claims the name.
- Enum `max` below variant count; enum values above `max` unreachable by construction.
  *(Said "count − 1" until 2026-08-05 — a leftover from a pack-from-0 model. Under implicit
  `None = 0` with variants packing from 1, the top variant's value IS the count, so
  `[max = count − 1]` would make it unencodable while passing the check.)*
- **Duplicate field names anywhere in one type — including across branch sides and cases.**
  One name, one field, declared once. (serialize.modern permits exclusive-side member reuse
  because members pre-exist there; schema owns the type, and unique names keep the
  flattened generated output unambiguous.)
- Back-reference violations (§4.5); `if` conditions that are not `bool` fields. *(The
  `switch` subject and case-value checks are shelved with the construct, §4.4.)*
- Cycles in type composition, undefined types, duplicate declarations, `package` mismatch.
- **Target-name safety:** a declaration or field name that is a reserved word in any target
  language (`type`, `match`, `impl`, `func`, `class`, ...) or that collides with another name
  after Go export-casing (`atRest` → `AtRest`) is rejected, with a diagnostic naming the
  target and the collision. No escaping machinery; rename at the source.

### 4.7 Strings and byte blocks — byte strings, one shape — DECIDED (Glenn, 2026-08-05: "fine"; unified the same evening)

**`string(N)` and `bytes(N)` are the same construct with two names**, unified on Glenn's
reading of the corpus (*"they're all fixed size buffers (both string and block data), and
they can all be 0 - max length entries serialized too"* / *"so this distinction is
incorrect."*): **N is the max length; the wire is length in [0, N], align, then the used
bytes; storage is a pre-allocated fixed buffer plus a used count.** The declared bound
exists for exactly one reason — *"the bounds are just to make sure we can serialize fewer
bits when we have a shorter string or smaller buffer value"* — it sizes the length
prefix's bits. The `<=` marker and a fixed-exact-length `bytes` form both died in the
unification (nothing used them; an exact-N form can return if a need appears). *(Until
this unification `string(N)`'s N was the classic BUFFER size, making the max length
N − 1 — an implementation detail leaking into the language, found when Glenn read the
corpus and asked why the two spellings differed.)*

`string(N)` carries **arbitrary bytes excluding 0x00** — all four generated readers
reject interior nulls; writes assert per §5. `bytes(N)` is the same wire with no
interior-null rule. The byte-string tightening is what lets the targets agree:

- Classic C++ `serialize_string` is strlen-based — it cannot represent interior nulls; a
  Go writer must not be able to produce a payload the C++ reader silently truncates.
- Rust `String` storage would impose UTF-8 validation the other targets lack, so Rust
  storage is a fixed byte buffer (§6.1) and text encoding is the application's concern.

For every legal write the wire bits are identical to `serialize_string` over a buffer of
N + 1. If UTF-8 text with enforced validity is wanted later, it is a new wire type, not a
redefinition of this one.

**Generated-code consequence, stated so no backend discovers it late** *(added 2026-08-05
from the contract audit)*: the runtimes' `serialize_string` is the **wire oracle** for
`string(N)`, not necessarily the emitted call. The §6.1 storage rules make the runtime
string methods unusable in two targets — C# stores a byte buffer but `SerializeString`
takes `ref string`; Rust stores a byte buffer but `serialize_string` takes `&mut String`
**and rejects non-UTF-8 bytes, which are legal here** — so the C# and Rust backends
compose the wire from primitives: length over [0, N], then align, then raw bytes. C++ and
Go may use their runtime string call for the framing. **The interior-null check is
generated-code validation in all four targets** — no runtime primitive performs it
(classic C++ read appends `'\0'` and would silently truncate).

### 4.8 Declaration sets — `message` and `object`, their types implicit

**DECIDED (Glenn, 2026-08-05):** each message is its own declaration; the discriminant enum,
the wire tag and the dispatch are the compiler's job, not the author's — *"I'd like
MessageType to be implicit in the generation, and for the messages definition in the schema
language to just be each message"* / *"This way we specify the minimal things, this means you
have to extract the message types yourself in a symbol table."*

```
message Ping {
    sequence uint16
}

message Chat {
    text string(MaxChatLength)
}

message Heartbeat {
    // empty body — presence is the payload
}
```

From the unit's `message` declarations the resolver extracts the message set and the compiler
generates, per target:

- **The `MessageType` enum, with `None = 0`** — *"Message types (and all other types...)
  should have 0 = no type, so we can express null"* — **then each message in a deterministic
  order. The order is SORTED BY NAME — DECIDED (Glenn, 2026-08-05: *"alphabetical order is
  better, because it's independent of formatting and cut & paste stuff."*).** The sort is
  bytewise ascending over the declared names (the same collation §3.1 pins for basenames —
  one rule, stated so two compiler builds agree). Tags are a
  pure function of the declared name set — independent of file layout, declaration order,
  and any cut-and-paste reshuffling. Renumbering on growth is wire-safe by construction:
  adding a message edits a schema file, the id moves, both sides regenerate together.
  *(Declaration order was proposed and overruled — its case was off-wire tag stability in
  logs and dashboards; his tiebreak is that sorted-by-name depends on nothing but the
  names. An explicit stable-tag attribute remains the q16-era escape if cross-unit
  composition ever needs one.)* `MessageType` is a claimed name: a unit with messages may
  not declare its own.
- **The wire framing**: the tag in minimal bits for `[0, count]` (the enum wire rule; read
  rejects tags above count), then the message body. **Tag 0 = None is a valid wire value
  meaning *no message*** — the null that gives message streams a natural terminator, the
  same shape as the surveyed protocol's `OBJECT_TYPE_NONE` sentinel.
- **`WriteMessage` / `ReadMessage`** plus a dispatch surface in each language's idiom: a real
  `enum Message { None, Chat(Chat), Ping(Ping), ... }` in Rust, an interface + type switch in
  Go, a base type + pattern match in C#, and in C++ whatever fits — union, variant, factory.
  **Representation is per-language and explicitly NOT part of the contract** (Glenn,
  2026-08-05: *"I do messages as unions in C++ but that doesn't mean they have to be or
  should be that way in other languages. That's just a C++ implementation detail."*) What
  binds every target is behavioral only: identical bytes, **reading `None` is a valid
  none-result, reading an out-of-range tag is a validation failure.**

  **The C++ representation — the CALLER'S CHOICE, DECIDED (Glenn, 2026-08-05:
  *"let's leave this up to the caller. I'd like for there to be an option to generate a
  regular union, and an option to use variant"*): `schema generate --cpp-message
  union|variant`, and the DEFAULT IS UNION** — a tagged struct over an anonymous union,
  the classic game idiom, constructed as the None message (the tag initializes to
  `None`; an arm's storage is established ZEROED when the arm is selected — by
  `ReadMessage` before it decodes, per §5's zero-baseline read rule, or by a writer
  assigning the arm) *(was "zero-initialized on construction": the constructor's
  whole-union memset zeroed the Block-sized union — ~2 KB per ~25 B message, measured
  at 60.6% of batch-read self-cycles on Zen 4 by the 2026-08-06 bench pass — while
  this same section pins representation as non-contract and the variant surface's
  monostate never zeroed payload storage; zeroing moved to arm selection, the exact
  work §5 requires and no more; repaired 2026-08-06)*, plain-switch dispatch,
  `GetMessageType` reads the tag field. The default was going to be variant until the
  measurement: isolating pure header cost (one identical representation-agnostic TU
  against each mode, arm64 clang), **union 0.09 s vs variant 0.13 s — the variant
  surface alone costs +44% header compile time**, and his ruling on the first, cruder
  number stands as the principle: *"0.17 -> 0.27 is not trivial for me. it's almost
  2X"* / *"I'm very negative on modern C++ for this reason... you almost always pay
  through the nose for it at compile time."*

  **The opt-in variant surface** (`--cpp-message variant`):
  `using Message = std::variant<std::monostate, <messages in tag order>>` — the variant
  INDEX equals the wire tag, `std::monostate` is `None = 0`. No heap ever
  (`std::variant` stores inline at the largest alternative — the union's exact
  footprint), trivially copyable (asserted in the test), dispatch via `switch` on
  `index()` + `get_if` (**no `std::visit` in generated code** — it remains a caller's
  option for compile-time exhaustiveness). Both representations are compiled and run in
  the test chain so neither rots; both speak the identical wire (the tag + the same
  per-message functions).

  **And the standing generation principle his ruling sets, wider than this choice:
  generated C++ defaults to the C-flavored idiom; modern constructs enter only behind
  opt-in flags, and any proposal to use one arrives with a measured compile-time cost,
  never an asserted one.**
- Every message is also an ordinary type: its own `Write`/`Read`/`MaxBits`/`MaxBytes`, usable
  standalone and composable as a field (the grammar's `ident // a declared type or enum`
  includes message names — a message IS an ordinary type). **An `object` name is NOT a field
  type in v1** *(DECIDED — corner-pins list, Glenn 2026-08-05: "Yes, sounds good.")*: an object has no single wire form (Deep vs Shallow),
  so `ship Ship` inside a message is a compile error naming this rule; view-qualified
  references are a later pass if ever needed. **And an `object` body admits plain fields
  only in v1** — no `if`/`switch`/`const()`/`reserved`/`align` — because the view-splitting
  rules assign *fields* to views and say nothing about what a branch means for
  Shallow/Interpolate/Quantize; the restriction lifts when the delta pass defines it.

#### Object sets — `object`, `ObjectType`, and the generated views

**DECIDED (Glenn, 2026-08-05) — the horizon's object layer arrived the same day.** `object`
declarations are tracked exactly like messages — *"you track all object types like messages,
and generate ObjectType per-type of object. Same as messages"* — so the resolver extracts
the object set and generates **`ObjectType` with `None = 0`** and each object in the same
deterministic order. The 0-reserved principle is now uniform across every generated
discriminant — and `ObjectType_None` is precisely the sentinel the surveyed baseline packets
already terminate with.

**The goal, his words:** *"generate ShipState, ShipData_Deep, ShipData_Shallow,
ShipData_Interpolate from one ship definition"* — *"there should be a single definition of
object Ship {} in the schema language that drives this generated code."* The view structs
exist **in generated code only**; the schema holds one definition per object.

**The views, his semantics verbatim — and the marker is named for the purpose, not the
plumbing:**

- **`[interpolate]`** — *"this is sent to the client for interpolation, it's visual
  state."* The marker was `shallow` for an hour; renamed on his call because *"shallow is an
  implementation detail on the way to interpolation on the client"* — the quantized shallow
  wire struct still exists, in generated code, as that implementation detail.
- **deep** — *"this is only sent to the client for client side prediction, eg. full state"*
  — and **deep is the default**: *"interpolated = off by default. deep by default"* — an
  unmarked field is full-state only.
- **`[local]`** — DECIDED (Glenn, 2026-08-05): simulation-only state that reaches no wire —
  lives in `ShipState`, absent from every network struct. *(He first spelled it
  `[unsynchronized]`, then took `[local]` on sight: "shorter. much shorter." — the register
  principle applied to a keyword.)*

**What one definition generates per object, per target** (shapes per language, behavior
identical — the message-set rule):

| artifact | contents |
|---|---|
| `ShipState` | every field — the simulation struct |
| `ShipData_Deep` + serialize | every non-`[local]` field, deep encodings |
| `ShipData_Shallow` + serialize | the `[interpolate]` fields, **quantized wire encodings** from the field's attributes — the wire-side implementation detail of interpolation |
| `ShipData_Interpolate` | the same `[interpolate]` fields in **continuous storage** (the un-quantized twin) |
| `Quantize` / `Unquantize` | the mapping pair between Interpolate and Shallow — the hand-written `Quantize()` in today's `Ship.h`, generated |

**PROPOSED view-encoding semantics (Rowan, 2026-08-05 — expanded from the three-sentence
draft-4 form; each rule is a transcription of the measured hand-written referent in
`Ship.h` / `core_quantize.h` / `core_delta.h`, so the risk of inventing wrong semantics is
minimal).** Inside an `object` body the attribute vocabulary widens: `interpolate` and
`local` (valueless, DECIDED) and `quantize` / `round` (valued, PROPOSED) join the set.
View-encoding attributes describe the SHALLOW wire only; the deep wire always uses the
bare storage type's encoding.

1. **`[interpolate]` alone** — the shallow wire is the deep encoding, unchanged.
2. **Composite quantization** — `[interpolate, quantize = K, max = B]` on a field whose
   type is a composite of float components applies component-wise: shallow wire per
   component is a ranged int `[−B·K, +B·K]`; write maps `c → floor(c·K + 0.5)`, clamped to
   the range; read rejects out-of-range; unquantize maps `q → q / K`. The quantized twin's
   storage derives from the range. All components are sent — no smallest-three; the delta
   pass owns cleverer encodings.
3. **No special composite cases in v1 — tags are inert (§4.2, Type tags).** Rule 2 is
   the whole of composite quantization: every composite quantizes per-component with an
   explicit `max` — a rotation field states its bound like anything else
   (`rotation Quat [interpolate, quantize = RotationUnits, max = 1]`; unit quaternions
   are always unit length — Glenn — so the bound is 1, written rather than implied).
   Rotation-specific actions (renormalize on unquantize, shortest-arc interpolation) are
   the delta pass claiming `[quat4]` — v1 generates the structural mapping only, and the
   application keeps its hand-written rotation actions meanwhile, exactly as it does
   today. *(Two earlier drafts — a built-in `quat` keyword, then a compiler-known class
   vocabulary — died the same day on his direction that the language pre-define no
   types.)*
4. **Ranged-int projection** — on `float32`/`float64`,
   `[interpolate, min = A, max = B, resolution = R]` reuses §4.3's compressed-float
   triple: min/max name the **CONTINUOUS domain**, the shallow wire is the int
   `[0, (B−A)/R]`, write maps `v → round((v−A)/R)` clamped, unproject maps `q → A + q·R`.
   The optional `round = nearest|up|down` key (default `nearest`, half away from zero)
   selects the write rounding — `up` exists because the surveyed health quantization's
   ceil is load-bearing: any positive health must quantize alive. *(Corpus consequence on
   approval: `thrust [interpolate, min = 0, max = 1, resolution = 0.01]` — the current
   `max = 100` conflates the wire-int range with the float domain, whose true bound is
   1.0 — and `health [..., resolution = 1, round = up]`.)*
5. **Interpolate storage** — projected fields (rule 4) are stored in the Interpolate
   struct in the WIRE integer domain and snap-interpolate, matching the shipped game;
   quantized composites (rules 2–3) are stored continuous (the un-quantized twin). *(The
   artifact table's blanket "continuous storage" phrasing is corrected by this rule — the
   real `ShipData_Interpolate` stores health/thrust as ints.)*

**§9 q13, RESOLVED for v1 — no interpolation generation in v1 at all.** v1 generates the
STRUCTS (State, Deep, Shallow, Interpolate) and the `Quantize`/`Unquantize` mapping pair —
his scope sentence exactly: *"just build the structs from the definition in schema lang to
start. (shallow, deep, interpolated, quantize)."* The `Interpolate(t, a, b, out)` FUNCTION
stays hand-written until the delta pass claims type tags and assigns actions (§4.2, Type
tags) — the measured fact that policy is 100% type-derived in all four real objects is
recorded there as that pass's design input; `lerp`, `slerp`, `snap`, `angle`, `smooth`
stay informally reserved as attribute values for it.

The worked example is [`examples/Objects.schema`](examples/Objects.schema) — verified
field-for-field against the real `Ship.h` on 2026-08-05: zero misclassifications, zero
invented fields. *(The one measured omission of that survey — the client-only
`predictedExplode` — is an omission no longer: it is declared as
`predicted_explode bool [local, context = client]` under the Contexts mechanism, and on
Glenn's ask the same evening the wrapper class's sim-local state — previous position,
collider count and armor table — was folded in too, so the generated per-context state
structs carry the FULL working ship state.)* **Missile, DynamicProp and Turret are
written beside it — all four objects are in the corpus.**

**Generated layout is the generator's — PROPOSED principle (Rowan, 2026-08-05).**
Hand-written code keeps struct-layout rules alive by comment, and prose-enforced layout is
a standing drift hazard. Generated structs invert that: field order inside every generated
view derives from need (wire order for wire structs, contiguous spans where machinery
wants them), never a convention a human maintains. Per-field interpolation POLICY is
resolved by q13's type-derived table (§9).

**And the payoff he named:** *"Once we have proper definitions of all object types, and
structs per-object type, we will be able to generate all the baseline and delta compression
code."* The object set is the prerequisite the delta pass was waiting for.

### 4.9 A complete example

```
// Messages.schema
package protocol

const MaxObjects    = 1024
const MaxChatLength = 256

type Vec {
    x float32
    y float32
    z float32
}

type ObjectState {
    id       int32 [min = 0, max = MaxObjects - 1]
    position Vec
    active   bool
    if active {
        orientation float32 [min = -180.0, max = 180.0, resolution = 0.01]
    }
}

message Ping {
    sequence uint16
}

message Pong {
    sequence uint16
}

message Chat {
    text string(MaxChatLength)
}

message Snapshot {
    base_sequence uint16
    objects       [<= MaxObjects]ObjectState
}
```

No `MessageType` enum is declared and no dispatch is written — the compiler extracts the
message set and generates both (§4.8): `MessageType { None = 0, Chat, Ping, Pong, Snapshot }`
and the per-language `WriteMessage`/`ReadMessage`. *(Ping and Pong may both name a field
`sequence` because they are separate types; §4.6's unique-names rule is per type. The
standing check is that every example in this spec and in `examples/` must compile under the
spec as written — the enum `MessageType` at the top of this example was deleted when message
sets arrived, exactly the boilerplate §4.8 exists to eat.)*

### 4.10 Deferred constructs

- **`int128` / `uint128` — LANDED 2026-08-06** (DECIDED as part of the integer family,
  Glenn 2026-08-05, with a named customer: **ludicrous mode** — *"we will need int 128
  for ludicrous mode :)"*). The named prerequisite resolved in order: the serialize C++
  runtime's phase-1 surface (`serialize_uint128` raw, `serialize_int128` ranged, the
  `serialize::uint128_t`/`int128_t` pair — native `__int128` or the emulated two-lane
  struct, byte-identical wire) shipped first, and the keywords went live against it —
  §4.3 rows, `examples128/` corpus, C++ emission. **The Go/Rust/C# backends REFUSE a
  unit using the family, by field name, until their serialize ports carry the surface**
  (the ports are in flight; a port landing lifts its refusal and joins the unit to the
  cross-language wire gate). Storage per target when they do: native `i128`/`u128`
  (Rust) and `Int128`/`UInt128` (C#), a two-qword struct in Go.
- **C++-style bitfields (`uint64 blah : 8`) — considered 2026-08-05, DECLINED across the
  targets, with Glenn's own expectation confirmed** (*"want to know if this is
  possible/advisable across our target generated languages. expect it will not be"* — it is
  not, for three reasons with a family receipt). (1) **The wire half already exists**:
  `bits(N)` is exactly the bitfield's wire — N bits, named, packed back to back — with
  honest per-field storage. (2) **As storage, only C++ has the syntax at all**, its layout
  is implementation-defined (order and padding vary by compiler — the reason serialize
  itself never uses bitfields), and C#/Go/Rust would need generated shift/mask accessors
  over a backing word — unnatural in all three. (3) **The killer is addressability**:
  generated serialize methods take `ref`/`&mut`/pointers, and bitfield members cannot be
  addressed — serialize.modern's own SCHEMA.md hit this exact wall: *"bit-field members
  cannot be used (widen them, or keep those packets on the streams)."* If memory packing of
  hot object arrays ever demands it, the door is an opt-in `[packed]` attribute on a type
  generating accessor-based storage — a generator-kind decision for the horizon, not a v1
  wire construct.
- **Fixed point — LANDED 2026-08-06 as `fixed(I, F)`**, in exactly the runtime-first
  order Glenn set (2026-08-05: *"we will also be doing fixed point in serialize there
  first, and then bringing that to this language"*): the wire construct was designed and
  proven in the serialize C++ library (`serialize_fixed`, its STANDARD.md section, its
  own goldens), and only then surfaced as this wire type — §4.3 row, §4.6 rules,
  `examples128/` corpus. **SIGNED only** (Glenn, 2026-08-06: fixed point is signed);
  the spelling is positional `fixed(I, F)` because the Q format is the type's shape,
  the same line `bits(N)`/`string(N)` sit on. **The unsigned spelling is an OPEN
  question with Glenn (§9 q17)** — derive signedness from `min < 0` vs an explicit
  `ufixed` — and no syntax is invented ahead of his call. The same port gap and refusal
  discipline as the 128-bit family applies (the bullet above).
- **Wide strings** (`serialize_wstring`): no near-term need; when added, they match the
  classic wire exactly (length, then unaligned 32 bits per code point).
- **Relative integers** (`serialize_int_relative`): the classic use is a strictly-increasing
  sequence across *array elements* (previous element's field feeding the next), which the
  back-reference rule cannot express; a scalar-to-scalar form inside one type earns too
  little to carry the construct. Deferred until a cross-element form is designed.
  **Confirmed against the game tree 2026-08-05:** every live use site (ten calls,
  `PacketReader.h`/`PacketWriter.h`) is the ascending-object-id chain inside
  sentinel-terminated, mid-stream-split packet streams — the constructs held at §9 q10 and
  the delta pass. `int_relative` is designed **with that pass**, never standalone; wide
  strings have zero usage anywhere in the tree.

### 4.11 Tables — the second wire, and reflection — DECIDED (Glenn, 2026-08-12)

`table X { ... }` declares a type that is also a **table-wire root**. The body grammar is
identical to `type` — every field kind, guard, default, and bound works the same, and a
table is usable anywhere a type is (message fields, arrays, other tables). What the
keyword changes is what the compiler EMITS for it, per Glenn's rulings verbatim:
*"reflection is NOT appropriate for types, but very appropriate for tables I think, and
across all languages generated"* and the versioning requirement (notes/table-wire.md).

**The closure.** The table closure is every `table` declaration plus every struct one
references through fields, transitively. Closure members get, in every generated
language:

1. **Table-wire codecs** (`TableWriteX` / `TableReadX`) — the evolution-tolerant TLV
   encoding specified in notes/table-wire.md: name-hash field ids, defaults elide,
   unknown fields skip, changed kinds skip, out-of-range clamps, all counted in a
   `TableReport`. This wire is for data that outlives builds (config, assets, settings,
   events); the realtime message wire (§4) is untouched and exact-match as ever.
2. **Reflection descriptors** (`TableTypeX()`) — static per-type field metadata: name,
   wire id and kind, array bounds and count companions, nested descriptor links,
   declared ranges, enum wire max and value→name functions, and branch guards. C++
   additionally carries storage offsets (zero-cost direct access); Go and C# carry
   generated get/set-by-name accessors instead, with set clamping to declared ranges
   exactly as the wire read does. No RTTI, no `System.Reflection`, no schema files at
   runtime — editors and tooling bind against the descriptors alone.

   **Data-oriented, by ruling** (Glenn, 2026-08-12: *"Keeping table reflection data
   separate from the table data itself enables fast iteration and walking"*): the
   descriptors are SEPARATE STATIC DATA, one set per type, shared by every instance —
   an instance is exactly its fields, carries zero reflection weight, and stays
   trivially copyable. A type's field descriptors are one flat contiguous array
   walked linearly; instance access is base + offset arithmetic (C++) or a generated
   direct accessor (Go/C#); nested types cost one hop to another flat array. Emitters
   in every language MUST keep this shape — no per-instance metadata, no fat objects,
   no pointer-chased descriptor graphs beyond the nested-type link.

Plain `type` declarations outside the closure get NONE of this — the realtime types pay
nothing for the table layer's existence.

**Capability rule.** A closure member must stay on table-wire kinds: `int128`/`uint128`,
`fixed(I, F)` and `bits` wider than 64 have no table-wire kind, and the CHECKER refuses
them inside the closure at compile time. Pack roots (`schema pack` manifest collection
types) must be declared `table`, and `EncodeTable`/`DecodeTable` require closure
membership — reflection and table codecs follow the declaration, never accident.

**Protocol id.** The keyword participates in the canonical form (§3.1), so marking a
table moves the unit's protocol id like any other declaration change. The table wire's
own compatibility does not depend on it: a table bin from a different protocol id still
reads under the permissive contract, which is the point.

## 5. Trust model — inherited

Reads validate everything — integer ranges, enum bounds, alignment padding, constants,
reserved bits, count bounds, string lengths, the interior-null rule, **and buffer
exhaustion: running out of input mid-read is a read failure like any other** *(added
2026-08-05 — it was the one malformed-input class the list omitted)* — and fail on any
violation, because network input is the trust boundary. Generated read code never lets a
value that controls iteration go unchecked before use (serialize.go documents this exact DoS
vector). **Read termination** *(DECIDED — corner-pins list, Glenn 2026-08-05: "Yes, sounds good.")*: `Read` consumes exactly the encoded
bits and reports bits consumed (so callers can frame several objects in one buffer); bytes
remaining after the last field are the caller's concern, not a validation failure —
`ReadMessage` streams end on the `None` tag by design.

Writes assume trusted data. Misuse follows each runtime's own convention: C++ debug asserts
(unchecked in release), **Go and Rust panic and C# throws on misuse in all build modes** —
the generated write code's job is to make misuse impossible by construction (bounds come from
the schema). Ranges are trusted inputs everywhere: generated code never feeds
attacker-influenced values as min/max.

**Read failure leaves the output object in an unspecified state**; callers use it only on
success. **Read success fully initializes it**: fields in untaken branches are set to their
zero values — 0, 0.0, false, empty bytes, zero count, **`None` for an enum, recursively
zeroed for a nested type, element-wise zeroed for a fixed array** *(the three cases DECIDED — corner-pins list, Glenn
2026-08-05; whole-object comparison needs them defined)*. **ZERO values, not specified
defaults** — the defaults of §4.2 are storage initialization at construction; the wire
contract stays a pure function of the encodings *(stated 2026-08-05 when defaults landed;
whether untaken branches should instead restore defaults is Glenn's call if a real case
ever wants it)*. **Scope, PROPOSED (found by the 2026-08-05 emission audit):** *fully
initializes* is relative to a zero-initialized output object — elements past a used count
and bytes past a used length are not rewritten by a successful read, so a REUSED output
object keeps stale tail data there (the classic runtime's own prefix convention).
Whole-object comparison in the conformance matrix is defined over a fresh output or the
used prefix; if tail-clearing on read is ever wanted instead, it is a generated-code
change, priced then. Write reads only taken fields.
This is what makes whole-object comparison in the conformance matrix well-defined.

## 6. Generated code

### 6.1 What is generated

Per `type`, per target:

1. **The type itself.** Storage, complete — **the integer family names its storage directly**
   (Glenn, 2026-08-05: the type name fixes the storage, the optional range refines the wire);
   everything else derives by fixed rule:

   | schema | C++ | C# | Go | Rust |
   |---|---|---|---|---|
   | `int8/16/32/64` (bare or ranged) | `int8_t/16/32/64_t` | `sbyte/short/int/long` | `int8/16/32/64` | `i8/16/32/64` |
   | `uint8/16/32/64` (bare or ranged) | `uint8_t/16/32/64_t` | `byte/ushort/uint/ulong` | `uint8/16/32/64` | `u8/16/32/64` |
   | `int128` (ranged) / `uint128` (raw) | `serialize::int128_t` / `serialize::uint128_t` (native `__int128` or the emulated pair) | `Int128` / `UInt128` *(port pending — backend refuses, §4.10)* | two-qword struct *(port pending — backend refuses)* | `i128` / `u128` *(port pending — backend refuses)* |
   | `fixed(I, F)` (SIGNED, ranged) | signed integer of I+F bits: `int8_t`..`int64_t`, `serialize::int128_t` at 128 — holding the RAW scaled value | *(port pending — backend refuses, §4.10)* | *(port pending — backend refuses)* | *(port pending — backend refuses)* |
   | `bits(N≤32)` / `bits(N>32)` | `uint32_t` / `uint64_t` | `uint` / `ulong` | `uint32` / `uint64` | `u32` / `u64` |
   | `bool` | `bool` | `bool` | `bool` | `bool` |
   | `float32` / `float64` (attributed or bare) | `float` / `double` | `float` / `double` | `float32` / `float64` | `f32` / `f64` |
   | enum `E` | `enum class E : uintN_t` (N = smallest fitting max) | `enum E : uintN` | `type E uintN` + consts | `#[repr(transparent)] struct E(pub uN)` + consts |
   | `flags E` | `uint64_t` + one mask const per variant | `ulong` + consts | `uint64` + consts | `u64` + consts |
   | `string(N)` | `char[N + 1]` + `int32_t` length | `byte[]` (capacity N, pre-allocated) + `int` length | `[N]byte` + `int32` length | `[u8; N]` + length |
   | `bytes(N)` | `uint8_t[N]` + `int32_t` length | `byte[]` (capacity N, pre-allocated) + `int` length | `[N]byte` + `int32` length | `[u8; N]` + length |
   | `[N]T` | `T[N]` | `T[N]` (pre-allocated) | `[N]T` | `[T; N]` |
   | `[<=N]T` | `T[N]` + `int32_t` count | `T[N]` (pre-allocated) + `int` count | `[N]T` + `int32` count | `[T; N]` + count |
   | `[Min..N]T` | as `[<=N]T` (count validated to [Min, N]) | as `[<=N]T` | as `[<=N]T` | as `[<=N]T` |
   | `f Inner` (a named type) | `Inner` | `Inner` | `Inner` | `Inner` |

   **The storage principle behind every row — DECIDED (Glenn, 2026-08-05):** *"we don't do
   anything dynamically sized by design in the schema"* — *"it's always pre-allocated fixed
   sized buffers on sender and receiver."* Generated storage never heap-allocates per
   message in any target: every buffer is fixed at its declared capacity with a used
   length/count beside it — no Go slices as storage, no Rust `Vec`, no growing containers.
   *(The table's C#/Go/Rust rows used dynamic containers until the principle was stated;
   corrected the same day.)*

   Enums are integer-backed named types in every target because `[max = ...]` headroom makes
   non-variant values wire-legal; a native Rust `enum` cannot hold them — C#'s `enum E : uint`
   can, natively, which is why it needs no newtype.

2. **`Write(buffer, object) -> bytesWritten`** — straight-line write code in wire order.
3. **`Read(buffer, object) -> ok/error`** — straight-line read code with full validation, in
   each runtime's native error idiom (`bool` in C++, `bool` + latched `Error` in C#, `error`
   in Go, `Result` in Rust). The consumed size (§5) surfaces per target idiom — a success
   value that carries bits consumed where the idiom allows, an out-parameter where it does
   not (C++'s `bool`); pinned per backend at implementation.
4. **`MaxBits` / `MaxBytes`** — constants: the longest path through the schema, with
   worst-case (7-bit) padding assumed at each alignment point. Size write buffers from
   `MaxBytes`. Conservative is correct for a buffer bound. **`MaxBytes` is rounded up to
   the 8-byte write-buffer granularity every serialize runtime requires** *(2026-08-05,
   found by the backend audit: every runtime asserts/panics on write buffers that are not
   a multiple of 8, so an exact-bytes constant advertised for buffer sizing was a
   guaranteed trap — the constant's one documented use)*.
5. **`ProtocolId`** — one constant per unit (§3).
6. **For a unit with `message` declarations (§4.8):** the `MessageType` enum (`None = 0`,
   then the messages sorted by name — DECIDED, §4.8),
   `WriteMessage`/`ReadMessage` with the tag framing, a message-level
   `MaxBytes` (tag plus the largest message), and the dispatch surface in each language's
   own idiom — representation per target, behavior identical. **The Go dispatch (BUILT
   2026-08-05): a `Message` interface the message structs satisfy, `nil` as the None
   terminator, a type switch — and reads land in a caller-supplied `MessageStorage`
   struct holding one field per message, the Go stand-in for the C++ tagged union, so
   the receive path never heap-allocates per message (the storage principle above,
   honored by the dispatch surface too).** In every target, `WriteMessage` validates the
   message BEFORE the tag rides the wire — an out-of-set value writes nothing, because a
   tag with no payload would desynchronize the stream — and both dispatch functions
   frame the tag through the `WriteMessageType`/`ReadMessageType` pair (one source).
7. **For a unit with `object` declarations (§4.8):** the `ObjectType` enum (`None = 0`,
   same deterministic order) and, per object, the DECIDED generated family — the full
   simulation struct, the Deep and Shallow wire structs with their split read/write pairs,
   the Interpolate struct, and the `Quantize`/`Unquantize` mapping pair (§4.8's artifact
   table). *(This list ended at item 6 while §4.8's object artifacts were DECIDED — an
   entire generated family missing from the generated-output contract; completed
   2026-08-05.)*

**Generated symbol naming — DECIDED (corner-pins list, Glenn 2026-08-05):** functions attach per target
idiom — C++: free functions `WriteShip(...)` in `namespace <package>`; C#:
`static class Schema` members in `namespace <Package>`; Go: free functions
`WriteShip(stream, &ship)` in package `<package>` (no overloading — the type name is in the
function name in every target for uniformity); Rust: module functions in `mod <package>`.
The `package` ident maps to the target's namespace/module/package concept verbatim.

**There is no generated measure function** — DECIDED (Glenn, 2026-08-04: measure *"was
always a bit of a hack"*). `Write` returns the actual size, `MaxBytes` sizes buffers, and
that covers the real uses. Anyone who genuinely needs per-object sizing can hand-write
stream code beside the generated code — the runtimes are unchanged and the two mix freely on
the same wire. If a real need appears, an opt-in exact measure (a generated running bit
count — the runtimes' `MeasureStream` is deliberately conservative and would not be used) is
a clean later addition.

The generated API mirrors serialize.modern's `schema<...>` members — `Write`, `Read`,
`MaxBits`, `MaxBytes` — so the two feel like one family.

**Canonical encoding is a CONTRACT:** for a given schema and target, equal
post-quantization values produce identical bytes — deterministically, forever, across
compiler versions; the golden-wire gate is what pins it (§3.1, §7.2). Consumers may
byte-compare encoded forms as a dirty/equality check; a canonicalization slip under that
use is a correctness bug, not a size regression. This was always true by construction; it
is stated so it cannot be quietly traded away.

*(A "derives" block — three generated helper functions, `Equal`/`Checksum`/`Print`, from
the external evaluation round — stood here for part of 2026-08-05 and was **DISCARDED on
Glenn's word the same day:** *"those concepts are from [the external engine]. Discard. we
don't use them."* / *"discard and throw in bin."* Git holds the design if a delta-pass need ever
resurrects any of it under our own name.)*

**Alignment is stream-relative**: a type containing `align`, `string` or `bytes` has
layout dependent on its entry bit offset. Generated functions are correct at any entry
offset; the same type works standalone and nested. `MaxBits` covers the worst case.

**Output layout — REVISED for C++ at implementation, DECIDED (Glenn, 2026-08-05):** the
C++ target emits **one header per schema file** — `examples/Constants.schema` →
`generated/cpp/Constants.h` — everything inside `namespace <package>`, **`#pragma once`**
(his call; *"if beneficial, you can use both sentinels and pragma once"* — pragma alone
until portability demands both), with cross-file `#include`s derived from actual
references, so the generated tree mirrors the schema tree a person navigates.

**REVISED AGAIN 2026-08-11 — the C++ header SPLITS into a data/wire pair,** decided
during the space game integration and announced in channel before landing: `<Base>.h`
is the DATA header (constants, enums, flags, structs, object view families,
MaxBits/MaxBytes bounds, the Message storage surface and `GetMessageType`) and includes
`serialize.h` only when a storage type demands it (int128/fixed); `<Base>Wire.h` is the
WIRE header (`Write*`/`Read*`, `Quantize`/`Unquantize`, the tag pairs and
`WriteMessage`/`ReadMessage`) and includes `<Base>.h` plus `serialize.h`, with
cross-file wire deps riding the deps' wire headers. **The finding that forced it:** the
space game vendors an older serialize as `core_serialize.h`, and both serialize
generations claim the same macro namespace (`write_int`, `serialize_int`, …14+ names) —
so a game basing its MATH TYPES on generated structs must be able to consume the data
half without inheriting the serialize runtime at all. Data consumers include `<Base>.h`;
wire consumers include `<Base>Wire.h`. A unit may not contain files named both `X` and
`XWire` (the emitter refuses the collision). Wire bytes are untouched by the split —
the full four-language suite and every wire golden held across the change. *(This
paragraph previously said one file per target per unit — `<package>.schema.h` — which
stands as the sketch for the single-file targets until each is implemented; per-file
output is the decided C++ shape.)* **The Go target (BUILT 2026-08-05) mirrors it: one
`.go` file per schema file, all in `package <package>` — Go packages are order-free
across files, so there is no topo sort and no include graph to refuse. The Rust target
(BUILT 2026-08-06): one module per schema file (lowercased basename) plus a generated
`lib.rs` declaring and glob re-exporting them; the C# target (BUILT 2026-08-06): one
`.cs` file per schema file, types at namespace level and every function and constant on
`public static partial class Schema`, in `namespace <Package>`. The unit-level dispatch
surface (the tag enums, tag pairs, dispatch value and functions) is emitted exactly ONCE
per unit in every target, in the topologically last carrying file, so declarations
spread across files never redeclare it.** Each generated
file is headed by the source file's BASENAME (never an invocation-relative path — the
same input must produce the same bytes wherever the compiler runs), the protocol id, and
a do-not-edit line. Output is **deterministic to the byte** for identical input; no
external formatter runs in the build or test path (goldens pin the emitters' own
output — clang-format/rustfmt version drift must not be able to break a golden). The
emitters are written to produce formatter-clean code instead — **except the Go emitter,
which routes its output through the stdlib `go/format` in-process (not an external
tool; versioned with the compiler's own toolchain): gofmt-clean by construction, and a
free refuser — generated code that does not parse fails generation loudly.** Build: **plain Makefile for
now, graduating to CMake as needed** (Glenn, 2026-08-05: *"use Makefile directly, for
now... probably preferable to keep it simple."* / *"We can graduate to CMake as
needed."*).

### 6.2 Code shape — a stated divergence from serialize.modern

serialize.modern's zero-overhead contract is enforced by disassembly: no calls, no loops, one
spliced constant path per branch outcome and per possible array count. That is the right
contract when fighting the template abstraction penalty from inside the consumer's compiler.
schema emits **source**, and its contract is different on purpose:

> **Generated code is the code a careful expert would hand-write against the runtime's
> serialize API: separate read and write functions, sequential field operations, honest loops
> for arrays, honest branches for `if`/`switch`.** Register allocation, unrolling and
> constant-folding are the target compiler's job — it sees straight-line calls into an
> inline-friendly library with every width and range a literal constant, which is precisely
> the input optimizers are built for.

**Ratified by the owner against his own objects, 2026-08-05:** *"for ship I don't want to
generate 'Serialize' functions anymore from schema. we should generate read/write functions,
completely custom and tailored... because now that the serialize is defined in schema, we
don't need a unified write anymore."* The unified `template <Stream> Serialize` was always
C++'s trick for keeping read and write from drifting apart — **the schema is that
single source of truth now, so the trick retires from generated code** and every target gets
split, tailored `Read`/`Write` per type and per view: *"this way we get all the benefits of
hard-coded read/writes in the gen code."* The runtimes' unified paths are untouched — *"that's
for people coding the serialize manually in their language and not using schema."*

What this buys: generated source stays small and reviewable, count ranges need no cap, and
four backends stay simple enough to verify against each other. What it forgoes: the
last-mile instruction-level guarantees of the TMP splicer. The v1 performance thesis is that
eliminating unified-serialize-function branching and runtime range computation captures most
of the win; if measurement against serialize.modern's schema mode on the C++ target shows a
gap worth closing, a bitpacker-level emission mode for fixed-layout structs is the planned v2
lever, behind the same byte-identity tests.

### 6.3 Per-target notes

| | C++ | C# | Go | Rust |
|---|---|---|---|---|
| emits against | `WriteStream`/`ReadStream` methods (or `serialize_*`-equivalent calls) | sealed `WriteStream`/`ReadStream` (`ref` params, `bool` returns + sticky `Error`) | `WriteStream`/`ReadStream` concrete types (no interface dispatch) | `WriteStream`/`ReadStream` via the `Stream` trait, monomorphized |
| error idiom | `return false` early-out | `bool` early-out; counts checked before loops; latched `Error` for callers | sticky stream errors; counts checked before loops; `return stream.Err()` | `?` propagation of `serialize::Error` |
| buffer contract | write buffers multiple of 8 (asserted); read allocations extend ≥8 bytes past packet data (required) | write buffers multiple of 8 (throws); reader takes (buffer, bytes), no slack required | write buffers multiple of 8; ≥7 bytes read slack for the fast path | write buffers multiple of 8; ≥8 bytes read slack for the fast path |

## 7. The compiler

Go, zero third-party dependencies, one static binary: `schema`.

```
schema check  [dir|files...]          // parse + typecheck; exit code for CI
schema generate --lang cpp,cs,go,rust [--cpp-message union|variant] --out <dir> [dir|files...]
schema id     [dir|files...]          // print the protocol id
schema fmt    [dir|files...]          // the canonical formatter, standalone (editors, hooks)
```

**Every command formats the unit's schema files IN PLACE before processing them —
DECIDED (Glenn, 2026-08-05: *"Can we do schemafmt just as like, a flag passed in to
schema tool"* / *"or even, just schema format every file before we process it."* /
*"we can be super opinionated here, it's fine :)"*).** One style, no options, no
separate binary; a file already in format is never touched (*"Obviously, if it is
already in format, we don't write to the file, capiche?"*). The consequence lands on
§3.1: the id is only ever computed over canonical bytes, so **the raw-byte hash IS a
canonical-form hash, structurally** — the §3.1 revisit clause is closed. The formatter
carries two built-in refusers: it re-parses its own output and structurally compares
the AST against the input's (refusing to write on any difference — a formatter must
never change meaning), and it verifies its own idempotence on every run.

### 7.1 Pipeline

```
*.schema → scanner → parser → AST → resolver/checker → IR → {cpp, csharp, go, rust} backends
```

- **Scanner/parser: hand-written, recursive descent**, in the style of the Go toolchain's own
  `go/scanner`/`go/parser`. No parser generators. The language is LL(2); hand-rolled parsing
  is what makes precise diagnostics cheap. The parser recovers at declaration boundaries so
  one error does not hide the rest.
- **Resolver/checker**: name resolution across the unit's files, constant folding
  (arbitrary-precision with per-use fit checks, §4.2), the shape checks of §4.6, the
  dominance rule of §4.5, and **the message-set AND object-set extractions** — the symbol
  tables over `message` and `object` declarations from which `MessageType`/`ObjectType`,
  the dispatch, and the per-object view families are generated (§4.8). *(The object half
  was DECIDED in §4.8 and missing here until 2026-08-05.)*
- **IR — the load-bearing design decision.** The checker lowers to a small, fully-resolved
  intermediate representation, and backends consume only the IR — a C++/Go/Rust divergence
  must be written into a printer to exist at all. **The IR preservation invariant: the IR
  keeps every parameter that affects (a) the bits written, (b) the value decoded from given
  bits, or (c) the set of inputs a read rejects.** Concretely: ranges stay exact `(min, max)`
  pairs with derived widths (width alone loses the reject set — `int(0,5)` and `int(0,7)`
  share a width and differ in what they reject); compressed floats keep `(min, max,
  resolution)` with derived step count (min and resolution determine decoded values);
  enums keep `(variant count, max)`; strings, bytes and arrays keep exact bounds; `const`
  keeps `(value, bits)`; back-references resolve to field indices; branches and counts are
  explicit; names of structs, fields and variants are carried — the backends print them.
  *(Until 2026-08-05 this bullet ended "the canonical encoding of this IR is the protocol-id
  input and a frozen public contract" — the dead draft-1/2 design; DECIDED §3.1 hashes the
  schema files and the IR is not in the id path at all. The IR's stability obligation is
  real but different: it is what the golden-wire gate (§7.2) holds still across compiler
  versions.)*
- **Backends: dumb printers.** Hand-written emitters (a small indent-aware writer helper),
  not `text/template` — codegen wants precision. Each backend is a single file a reviewer can
  hold.

### 7.2 Testing

**Status 2026-08-06: gates 1, 2 and 7 are LIVE for ALL FOUR TARGETS, and gate 4 is live
in pinned-instance form across the whole matrix** — `testdata/golden/` pins the
generated source of both C++ message representations, the Go package, the Rust crate and
the C# sources byte-for-byte plus the protocol id; `testdata/wire/` pins eleven
instances' wire bytes, written by the C++ test and **byte-compared by the Go, Rust and
C# tests (`test/go`, `test/rust`, `test/cs`), so cross-language wire identity is a
standing gate, not an assumption** (the C++ test also proves the two message
representations produce identical bytes); a fmt-drift gate asserts the corpus stays
formatter-canonical; and each target's test asserts the readers agree on what they
REJECT (interior nulls, nonzero reserved bits, corrupted constants, out-of-range
counts — with truncation surfacing as the stream's own error, never a content verdict).
`make test` runs all of it — seven binaries; `make update-goldens` re-pins DELIBERATELY.
**The fixed-point + 128-bit unit (`examples128/`, 2026-08-06) extends gates 1, 2 and 7
to the new wire families in C++ only** — its two wire goldens were DERIVED from
STANDARD.md's wire law independently of serialize and then asserted against the
generated writer (`test/ludicrous_main.cpp` carries the derivation field by field), and
the Go/Rust/C# backends' REFUSAL of the unit is itself a pinned test — so the port gap
is a named, tested state, not an untested absence (§4.10).
Gate 3 (the oracle) exists in seed form — the RigidBody and string classic twins in
`test/main.cpp` — and grows with the corpus; gate 4's full random matrix and gate 5 are
the remaining conformance work now that all four targets exist; gate 6 is live in
diagnostics-suite form (the break-the-language suite, 60+ cases), with fuzzing owed. Gate 6's diagnostics now also cover generated-name collisions: companion
length/count names, the Go dispatch method, and the per-declaration generated symbols
(Write*/Read*/New*/\*MaxBits/\*MaxBytes) are claimed names the checker refuses.

1. **Golden source tests**: schema in, generated source compared byte-for-byte against
   checked-in goldens, all four backends. Deterministic output makes this exact.
2. **Golden ids**: `schema id` over the conformance corpus pinned as exact values — the
   tripwire on §3.1's hash procedure (sort order, delimiting, truncation): any change to
   how the id is computed breaks every pinned value loudly. *(Re-pointed 2026-08-05 — this
   said "the frozen-encoding contract's tripwire," the dead IR-hash design's rationale;
   the gate itself was always right.)*
3. **The wire oracle**: for each conformance schema, a hand-written classic-serialize stream
   twin in C++. Generated C++ must produce byte-identical output to the twin and each must
   decode the other — the same gate serialize.modern runs, against the same oracle.
4. **The cross-language matrix — the whole point**: every writer × every reader (16 pairs),
   property-driven random instances, identical bytes, identical decoded values under §5's
   whole-object rule. **This needs per-target generated test scaffolding** — instance
   generators respecting every range/branch/count, and a canonical dump format for
   cross-language value comparison — budgeted as part of the conformance harness, not
   hand-written per schema.
5. **Malformed-input agreement**: bit-flip sweeps over valid packets; all four generated
   readers must agree on accept/reject — and, for accepted inputs, on the decoded value
   (§5 leaves rejected outputs unspecified) — serialize.modern's brute-force
   read-validation gate, extended across languages.
6. **Compiler robustness**: Go-native fuzzing on scanner, parser and checker; every
   diagnostic in the suite asserted by exact message and position.
7. **Golden wire bytes** *(added 2026-08-05 — §3.1 and §6.1 both leaned on this gate by
   name and the list did not contain it)*: per conformance schema, fixed instances with
   **checked-in expected wire bytes**, all four targets. This is the one artifact that
   survives a coordinated emitter-plus-oracle change, and a golden-wire break is the
   stop-the-line event §3.1 describes — never a quiet re-pin.

**Floating-point discipline, inherited from the serialize.cs port finding (§1):** all C++
conformance builds compile with `-ffp-contract=off`, and the golden-wire corpus includes an
FMA-boundary compressed-float value, so a contracted build fails gate 7 loudly rather than
diverging by one quantization step in production. *(§1 promised §7.2 inherits this rule;
until 2026-08-05 this section did not state it.)*

CI needs all four toolchains for gates 3–5 and 7; that cost is accepted — it is the
product's central claim.

### 7.3 The path — DECIDED order of work (Glenn, 2026-08-04)

1. **The spec and the language design.** This document. The language is the volatile part;
   everything downstream (compiler, three codegens) amplifies its changes, so it settles
   first.
2. **The examples corpus** (`examples/`): realistic `*.schema` files written against the spec
   — message sets, snapshots, the serialize.modern examples translated — **testing that the
   language can actually express those things**, on paper, before a line of compiler exists.
   The language iterates against the corpus; a construct no example needs is a construct v1
   may not need.
3. **Only then, implementation** — and the corpus graduates into `testdata/` and
   `conformance/` as the compiler's first test suite.

### 7.4 schemafmt — the one style — BUILT 2026-08-05, as the compiler's own front step (Glenn: "yes, schemafmt is important, same reason as per-go. there is a canonical syntax form from which we generate the hash"; his design the same evening — every command formats before processing, §7's CLI block carries the words)

gofmt's philosophy (§4.1): one style, no options. Codified from the corpus's own formatting
while it was five files; built as **the parser's first consumer** and run by every
`schema` command over the unit before processing — under §3.1's file-byte hash, a
later reformat of a deployed schema **moves its protocol id**, so canonicalization
had to land before any schema existed outside this repo, and it did: **the corpus was
canonicalized 2026-08-05 (whitespace-only, meaning-preservation enforced by the
formatter's AST-equivalence refuser) and the id settled at its canonical value —
the one §7.2's golden pins.** With the compiler formatting before hashing, §3.1's
raw-byte hash is a canonical-form hash structurally, with no parser in the id path —
the §3.1 revisit clause is closed. Rules:

1. **Indent: 4 spaces** per block level, never tabs (the corpus's unanimous vote). `{` on
   the construct's line; `} else {` on one line. One field or declaration per line.
2. **Alignment groups**: within a contiguous run of fields — broken by blank lines and by
   comment lines — pad names to align the type column and types to align the `[` attribute
   column. The same rule aligns `=` in const runs. Single space minimum between columns.
   *(The corpus already drifted on exactly this in ~330 lines: one unaligned const pair,
   and two enum braces treating a comment break differently — the argument for the tool.)*
3. **Attributes**: `[min = 0, max = MaxHealth]` — spaces around `=`, comma-space between
   entries, no padding inside the brackets. Valueless markers first, then valued keys.
4. **Expressions**: single spaces around binary operators.
5. **Enums**: one line while they fit; the wrap trigger is decided at the first multi-line
   instance (no line-length limit exists yet). *(q9 settled canonical whitespace variants,
   so one-line enum bodies are the standing form.)*
6. **switch/case**: `case` at the same indent as its `switch`; a single-item case body
   inline after the label, bodies column-aligned across the cases of one switch; multi-item
   bodies on following lines, one level in.
7. **Blank lines**: collapsed to one; preserved as group separators.
8. **Comments preserved, never reflowed** — file-header block, section dividers
   (`// ---- name ----`), and doc comments stay attached to what they precede.
9. **schemafmt never reorders declarations** — a formatter formats; it does not move code. (Tags are sorted-by-name (§4.8), so ordering carries no meaning —
   the aspect layout (§2) stays a convention.)

## 8. Repository layout

```
cmd/schema/            the CLI
internal/scanner/      tokens, positions
internal/parser/       AST construction, error recovery
internal/ast/
internal/check/        resolver, constant folding, shape checks, dominance rule
internal/ir/           the lowered form (wire-frozen; the id is §3.1's file hash)
internal/codegen/
    cpp/  csharp/  golang/  rust/
testdata/              golden schemas, golden generated source, golden ids, diagnostics
conformance/           oracle twins + the cross-language matrix harness
notes/                 extracted runtime API contracts (design inputs)
SPEC.md                this document
```

License: AGPL-3.0 (DECIDED, 2026-08-04). Repo private until Glenn opens it.

## 9. Open questions, gathered

*(Settled 2026-08-04/05: the protocol id — schema-file hash, §3.1 — and no generated
measure, §6.1. Both DECIDED.)*

1. ~~Strings as byte strings~~ — **DECIDED (Glenn, 2026-08-05: "fine")**: §4.7, with the
   generated-code consequence stated there (C#/Rust compose the wire from primitives; the
   interior-null check is generated-code validation in all four targets). The
   validated-UTF-8 door stays open as a future new type.
2. ~~**Storage-type overrides**~~ — **RESOLVED 2026-08-05 by the integer family**: storage
   is declared by the type name (`thrust int8 [min = 0, max = 100]`), no override mechanism
   needed. *(This row carried the dead call syntax until 2026-08-05.)*
3. ~~Wide strings and relative integers~~ — **DECIDED deferred (Glenn, 2026-08-05: "OK" /
   "yes, int_relative is used when doing delta encoding, which is later")**: wide strings
   appear nowhere in the game tree; all ten `int_relative` sites live inside the packet
   shapes the delta pass owns (§4.10's note).
4. ~~`schemafmt` timing~~ — **DECIDED v1 (Glenn, 2026-08-05: "yes, schemafmt is
   important, same reason as per-go. there is a canonical syntax form from which we
   generate the hash")**: built early, as the parser's first consumer; style rules §7.4,
   reviewed with the corpus.
5. ~~Doc comments~~ — **DECIDED deferred (Glenn, 2026-08-05: "don't care. leave for
   later.")**: the design is kept at §4.1 for the day it lands.
6. ~~A root/packet marker~~ — **DISCARDED (Glenn, 2026-08-05: "don't know what root
   packet marker is. did not ask for it.")** — a Rowan-invented v2 idea, not his ask;
   binned. *(Its one factual by-product stands independently in §3.2: id-scoping by
   reachability is incompatible with file hashing — recorded there, not here.)*
7. ~~Const expressions over enum counts~~ — **DECIDED (Glenn, 2026-08-05: "yes, but
   prefer Team.Max")**: `E.Max` (§4.2); `const NumTeams = Team.Max + 1`; `len(Team)`
   declined; the `MAX_PROPS` sum was already covered by constant composition.
8. ~~Platform-conditional constants~~ — **DECIDED (Glenn, 2026-08-05: "yes, because
   server and clients will be on different platforms, but must always be able to
   communicate.")**: constants are platform-uniform (§4.2); `FRAME_SAFETY` stays in game
   code.
9. ~~Explicit enum variant values, flag enums, the separator~~ — **DECIDED (Glenn,
   2026-08-05)**: canonical enums only — *"canonical enums are in the form 0 = none,
   [1,max], dense non-sparse values."* — explicit/sparse values declined permanently, so
   the whitespace separator stays; and flags supported — *"yes, we can and should support
   flags too. flags are uint64_t, one field per-bit, up to 64."* — as the `flags`
   form (§4.2, mechanism PROPOSED, rides the corpus review).
10. ~~Sentinel-terminated collections~~ — **DECIDED deferred (Glenn, 2026-08-05: "sure,
   that's out of scope")**: designed inside the object/delta pass, all three measured
   terminator idioms at once (bool-continuation, sentinel value, stop action); readers
   are structurally oblivious to packet splits, so the language owes readers only a
   terminated-stream loop.
11. **Enum subranges, and the enum-as-index pattern** — widened 2026-08-05 with Glenn's
   design thoughts for the future, banked verbatim. The surveyed create path writes an
   object kind as `[1, max]`, excluding `None` from the wire. With universal implicit
   `None = 0`, two related wants emerged: **(a) index enums without None** — *"for these
   index values, that index is not there... it's like, shifted left, 0 is no longer valid"*
   (laser_index/missile_index going *"off the enum directly and we don't have to repeat
   min/max"*); **(b) enum-as-index as an explicit language feature** — *"this enum -> an
   index to the values the enum represents, is a common pattern... tying it explicitly to
   the enum as a language feature/pattern helps avoid manual mistakes being made here in
   future"* — his working name: **`enum_index`**, and the pattern is wider than enums:
   *"in space game i turn everything into an index, it's a pattern. object ids, enums
   etc."* A field that indexes a declared set derives its range from the set, never
   restates it. **The requirement in his final form: both kinds exist** — *"sometimes you
   need to represent null, eg. an enum type that can have null (nothing), and other times
   it is an index of valid values in that enum (like laser index, missile index). we need
   to support both types of indexes driven by enums."* Design pass owed; evidence:
   `examples/README.md` finding 1 and `Ship.h`'s hand-repeated `[0, SHIP_MAX_LASERS-1]`
   bounds.
   **The design ran its full arc on 2026-08-05 and ended CUT — for now.** A per-field
   `[no_none]` attribute was the first draft and died on his call for type-level (*"suspect
   it's better done as a type... vs. doing it at serialize level."* / *"#4 agreed."*); the
   type-level `enum_index` form was then specified in full — wire = value − 1 in
   `[0, max − 1]` (his notation: *"enum_index serialization [0,Max-1] biased -1 on the
   wire"*), storage keeping 0 as an unset sentinel, never wire-legal — and confirmed
   (*"#3 is confirmed."*), then **cut the same evening: *"Let's cut enum_index for
   now."*** The full specification above is the record for the day it returns — likely
   with the claiming/index pass, where *"in space game i turn everything into an index"*
   points anyway. **v1 consequence, stated plainly:** a kind field is a plain `enum` and
   the `[1, max]` create wire is not expressible — one unused wire value spent per kind
   field, finding 1's original state. The derived-range rule stays normative; the general
   `index(Set)` feature stays deferred with `index` reserved. The manual-mistake class the
   requirement predicts was measured in the tree the same day (a wrong-enum `MAX` guard, a
   bound restated divergently on two paths of one field) — and fixed at his word.
12. ~~**`int` → `int32`?**~~ — **RESOLVED 2026-08-05 by the integer family**: bare `int` is
    gone; every integer names its width, Go-style, and `int`/`uint` are reserved purely to
    give a helpful diagnostic.
13. ~~Interpolation policy~~ — **RESOLVED 2026-08-05: no interpolation generation in v1
    at all** (§4.8). v1 builds the structs and the Quantize/Unquantize pair;
    `Interpolate()` stays hand-written until the delta pass claims type tags and
    assigns actions — **tag → specifying behavior, his architecture** (Glenn: *"this
    alone points to the tag -> specifying behavior being the correct way to go."*).
    Design input recorded for that pass: measured across all four real objects, policy
    is 100% type-derived today (lerp floats, shortest-arc nlerp rotations, snap
    discrete), and no field anywhere overrides its type's policy.
14. ~~The replication-policy boundary~~ — **DISCARDED (Glenn, 2026-08-05):** *"there are
    no send scheduling knobs in my game engine. i don't do priority/TTL/coherence."* /
    *"those concepts are from [the external engine]. Discard. we don't use them. our
    networking techniques are stronger than [theirs]"* — *"built around deltas and snapshots, not
    around priority accumulators and packing n most important objects into a small
    packet."* / *"discard and throw in bin."* No policy attributes, ever; the question
    came from an external evaluation of a different architecture and does not apply to
    this house. Schema fully owns serialization; nothing else was ever in scope.
15. ~~The engine interop surface~~ / 16. ~~The engine adoption asks~~ — **DISCARDED with
    the same ruling, 2026-08-05.** Externally-derived interop and adoption material is
    out of this repo; the exchange record lives outside it. If a cross-engine
    collaboration ever becomes real work, it starts fresh from what schema IS then.
17. **The unsigned fixed-point spelling — OPEN with Glenn (2026-08-06).** v1 ships
    `fixed(I, F)` SIGNED only, his confirmed baseline ("fixed point is signed" — the
    runtime's `serialize_fixed` accepts unsigned storage, so the wire construct already
    exists). The open choice is how the language would spell unsigned if it ever wants
    it: **(a) derive signedness from the bounds** — `min >= 0` means unsigned storage,
    no new syntax, but a field's storage type then changes when a bound crosses zero —
    versus **(b) an explicit `ufixed(I, F)`** — one more keyword, storage always
    manifest in the type name, the integer family's own int/uint precedent. No syntax
    is invented ahead of his call. The interim costs nothing on the wire — the wire is
    range-derived either way — signedness only decides which storage widths can host a
    given non-negative whole-unit domain (signed Q8.8 tops out at 127 units where
    unsigned reaches 255).

---

*Draft 2, Rowan, 2026-08-04 — from serialize.modern's SCHEMA.md and README (read whole), the
extracted serialize / serialize.go / serialize.rs contracts in `notes/`, Glenn's decisions of
tonight, and two independent cold reads of draft 1 (wire claims; language design and pipeline
feasibility) whose findings are folded in above with their reversals named. Wire claims name
their classic twins but are not yet pinned by tests; the conformance suite in §7.2 is what
converts this document from intent to property.*
