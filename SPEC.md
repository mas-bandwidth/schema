# schema — specification (draft 4)

> **Status: DRAFT, pre-implementation — the language is the deliverable right now.** This
> spec precedes the decision to build (house doctrine), and deliberately so: the language is
> the part that will evolve, and every change to it ripples into the compiler and three
> codegens (Glenn, 2026-08-04). **§4 — the language — is the chapter under active design and
> the one to review hardest.** §6–§8 (generated code, compiler, layout) are engineering
> sketches recorded so decisions have somewhere to live; they are not started and stay cheap
> to change until the language settles. Sections are marked **DECIDED** (Glenn, 2026-08-04)
> or **PROPOSED** (Rowan's recommendation, awaiting review). Nothing here is wire-tested yet.
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

## 1. What schema is

**schema** is a small language for describing bitpacked network data, and a compiler — written
in Go — that translates `*.schema` files into generated C++, C#, Go and Rust source code
targeting the serialize family of libraries:

| target | runtime library |
|---|---|
| C++ | [serialize](https://github.com/mas-bandwidth/serialize) |
| C# | [serialize.cs](https://github.com/mas-bandwidth/serialize.cs) — ported 2026-08-05 (private; 32/32 tests, wire-verified vs C++ both directions; license + CI pending) |
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
  `delta`, `baseline` are informally reserved for future use).

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
options and user settings are four expressions.

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

## 3. Versioning: the protocol id — DECIDED, mechanism PROPOSED

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

Definition, precise so two compiler builds agree: the unit's `*.schema` files ordered by
sorted basename, each file's raw bytes hashed in sequence; low 64 bits of the SHA-256.

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

### 3.2 The unit

A compilation unit is a set of `*.schema` files compiled together (one directory by default).
**Exactly one `package` per unit** — a mismatch is an error. Names resolve across all files in
the unit; the id depends only on the set of declarations, never on their file layout. One id
per unit, exposed as a constant (`ProtocolId` / `PROTOCOL_ID`) and printed by `schema id`.

**Known consequence, documented rather than hidden:** every declaration in the unit
contributes to the id, so adding an unused helper type moves it. A root/packet marker that
scopes the id to reachable declarations is the natural door (§9, open question 7).

`reserved(bits)` fields remain useful *within* a protocol id — not to dodge redeploys (any
claim of reserved bits moves the id like any other change), but to keep packet sizes and
layout budgets stable while a protocol grows into space already paid for.

Whether the id travels on the wire (a connect token, a `const` field, out of band) is the
application's choice — netcode-style stacks already carry one.

## 4. The language

### 4.1 Lexical structure

- UTF-8 source. `//` line comments and `/* */` block comments (non-nesting).
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
- **Punctuation and operators:** `{ } ( ) [ ] , : = ! .. <= + - * / %`.
- **Reserved words:** `package const enum type message object if else switch case align
  reserved`
  and the wire-type keywords `bits bool float32 float64 string bytes` plus the
  integer family `int8 int16 int32 int64 uint8 uint16 uint32 uint64` (and `int128 uint128
  int uint`, reserved — the first two for the deferred 128-bit construct (§4.10), the bare
  two so `int` gets a "did you mean int32?" diagnostic instead of a parse error).
  Reserved words cannot be used as names. Attribute keys (`min`, `max`, `resolution`, ...)
  are contextual — they live only inside `[ ]` and are not reserved.
- **Newlines terminate declarations and fields — there are no semicolons, like Go.** The
  newline is a terminator token,
  suppressed: immediately after `{`, `(`, `[`, `,`, `:`, `=`, `else`, and an infix operator;
  and immediately before `)`, `]`, `}`. Blank lines are insignificant. `{` sits on the same
  line as its construct; `} else {` is written on one line.

### 4.2 Grammar

EBNF (`NL` = the newline terminator; `{X}` repetition; `[X]` option):

```
File        = { Declaration } .
Declaration = Package | Const | Enum | TypeDecl | Message | Object .
Object      = "object" ident Block NL .
Package     = "package" ident NL .
Const       = "const" ident [ ConstType ] "=" ConstExpr NL .
ConstType   = IntType | "float32" | "float64" .
ConstExpr   = IntExpr | FloatExpr .
Enum        = "enum" ident [ Attributes ] "{" { ident } "}" NL .
TypeDecl    = "type" ident Block NL .
Message     = "message" ident Block NL .

Block       = "{" { Item } "}" .
Item        = Field | ConstField | Reserved | Align | If | Switch .
Field       = ident Type [ Attributes ] NL .
ConstField  = "const" "(" IntExpr "," IntExpr ")" NL .          // (value, bits)
Reserved    = "reserved" "(" IntExpr ")" NL .
Align       = "align" NL .

Type        = [ "[" Bound "]" ] Scalar .                         // array bound is a PREFIX, Go's order
Scalar      = IntType
            | "bits" "(" IntExpr ")"
            | "bool" | "float32" | "float64"
            | "string" "(" IntExpr ")"
            | "bytes" "(" [ "<=" ] IntExpr ")"
            | ident .                                            // a declared type or enum
IntType     = "int8" | "int16" | "int32" | "int64"
            | "uint8" | "uint16" | "uint32" | "uint64" .
Bound       = IntExpr | "<=" IntExpr | IntExpr ".." IntExpr .

Attributes  = "[" Attr { "," Attr } "]" .                        // trailing, optional, per field
Attr        = ident "=" ( IntExpr | FloatLit )                   // valued:    min = 0
            | ident .                                            // valueless: interpolated, local

If          = "if" [ "!" ] ident Block [ "else" Block ] NL .
Switch      = "switch" ident "{" { Case } "}" NL .
Case        = "case" CaseLabel ":" { Item } .                    // ends at next case or }
CaseLabel   = ident | IntExpr .                                  // variant name, or integer

IntExpr     = integer expression over literals and const names:
              "+" "-" "*" "/" "%", unary "-", parentheses.
```

- **Integer expressions** evaluate in checked signed 64-bit at compile time. Overflow,
  division by zero, and a negative result where a width, bound or count is required are
  compile errors. Float attribute values (`min`/`max`/`resolution` on `float32`) are float
  literals only in v1.
- **Constants compose** — DECIDED (Glenn, 2026-08-05: *"constants can refer to other
  constants, so we can build up things, const C = A * 2 + B"*). A `const` may reference any
  other `const` in the unit, order-free across files like type references; reference cycles
  are a compile error naming the cycle. The corpus exercises it:
  `const MaxPositionUnits = MaxWorldMeters * UnitsPerMeter`.
- **Constants are typed, and not just integers** — DECIDED (Glenn, 2026-08-05: *"They won't
  just be integer"*), on Go's untyped-constant model: a bare `const` takes its **kind** from
  its expression — integer (checked signed 64-bit arithmetic) or float (float64 arithmetic;
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
- Inside a type body one token of lookahead disambiguates every item: `const` + `(` is a
  constant field (versus the top-level `const name =` declaration); `if` / `switch` /
  `align` / `reserved` are keywords; any other identifier begins a field. The grammar is
  LL(2) and hand-written recursive descent is the intended implementation.
- **Enum variants** are whitespace-separated identifiers (newlines included), numbered 0..n−1
  in declaration order. Explicit variant values are not in v1 — noted because the bare
  space-separated form forecloses adding `= value` without a separator change.
- **Type references are order-free** — a type or enum may be used before its declaration,
  in any file of the unit. (Field *back-references* are not order-free; §4.5.)
- `if` and `switch` nest freely inside blocks and case bodies.

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
  `[interpolated]` and `[local]`, and the prefix-array decision had already removed the
  collision that motivated the deferral. **Attribute lists stay flat** (Glenn:
  `[interpolated, min = x, max = y]`) — a marker and its encoding parameters sit side by
  side; no nested argument syntax.

- **The line between positional and attribute:** a *size* that defines the type's shape stays
  positional — `bits(64)`, `string(64)`, `bytes(<= N)`, array bounds. A *constraint or
  refinement* of a named type is an attribute. The enum's `[max = 15]` was already this
  syntax; it is now the one general mechanism.
- **The vocabulary is typed and closed per compiler version — an unknown attribute is a
  compile error**, never a silently ignored string (the anti-Go-struct-tag decision: keyed
  like Go, checked like Go is not). v1: integers take `min`/`max` (both together or
  neither); `float32` takes `min`/`max`/`resolution` (all three together — see §4.3: this IS
  the compressed float); `enum` declarations take `max`.
- **Attributes are the horizon's attachment point.** Per-field delta tiers, interpolation
  modes, struct-mapping targets — *"what properties and attributes per-property"* — land
  here as new keys with new generator passes, without touching the grammar again.

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

### 4.3 Field types and their wire encodings

The wire encodings are exactly classic serialize's — each row names its classic twin, which is
the wire oracle. (Contracts: `notes/serialize-cpp-api.md`.)

| schema | wire | classic twin |
|---|---|---|
| `f bits(N)` | N raw bits, N in [1,64] | `serialize_bits` ([1,64]; >32 = low 32 bits first, then the high remainder) |
| `f intN` / `f uintN` (bare, N ∈ 8/16/32/64) | N raw bits (two's complement for signed) | `serialize_uint8/16/32/64`; signed raw is the same bits, cast |
| `f intN [min = A, max = B]` / `f uintN [min = A, max = B]` | minimal bits for the range, value − A; read rejects out-of-range; **the range must fit the declared storage** | `serialize_int` (≤32-bit int ranges) / `serialize_int64` / width-computed `serialize_bits` for full-unsigned ranges |
| `f bool` | 1 bit | `serialize_bool` |
| `f float32` | 32 raw IEEE-754 bits | `serialize_float` |
| `f float64` | 64 raw bits (low dword first) | `serialize_double` |
| `f float32 [min = A, max = B, resolution = R]` | quantized to R-sized steps; read rejects values above the step count | `serialize_compressed_float` (exact formulas incl. the ceil, +0.5f rounding and clamp) — **the former `compressed_float` keyword, dissolved into attributes: storage stays `float32`, the attributes describe the wire** |
| `f Weapon` (an enum) | minimal bits for [0, max]; read rejects above max | `serialize_int` over [0, max] |
| `f Inner` (a type) | Inner's fields, in place | `serialize_object` |
| `const(Value, Bits)` | the constant; read **rejects** any other value | `serialize_bits` + compare |
| `reserved(Bits)` | zeros; read rejects nonzero | `serialize_bits` + compare |
| `align` | zero-pad to the next byte boundary; read rejects nonzero padding | `serialize_align` |
| `f string(N)` | length in [0, N−1], align, then the bytes | `serialize_string` |
| `f bytes(N)` | align, then exactly N bytes | `serialize_bytes` |
| `f bytes(<= N)` | length in [0, N], align, then the bytes | `serialize_int` + `serialize_bytes` |
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

### 4.4 Decisions: `if` and `switch`

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
- `switch field { case ... }` — `field` is a previously serialized integer or enum field.
  Wire = the matching case's fields; a value matching no case serializes nothing —
  identically on write and read, so the wire stays symmetric.
- **Case labels:** for an enum subject, bare variant names of that enum (resolved through the
  subject's type); for an integer subject, constant integer expressions. Duplicate case
  values are a compile error.

### 4.5 Back-references: the dominance rule

The referenced field must be declared **earlier in the same block or in an enclosing block** —
never in a sibling branch side, another case, or inside an array element. Equivalently: the
referent is serialized on every path that reaches the reference. This is a lexical-scope
check in the resolver.

```
case Snapshot:
    has_delta bool
    if has_delta { ... }      // legal: same block, earlier — dominated
```

Forward references, references into sibling branches or other cases, and references to array
element fields are compile errors naming the offending reference and the rule. *(Draft 1
required the referent be unconditionally serialized at type top level, which outlawed the
example above and contradicted the fused-branch subsumption claim; the dominance rule is
strictly more expressive and equally static.)*

### 4.6 Shape checks

All compile errors with positions:

- `bits`, `const`, `reserved` widths outside [1,64]; a `const` value that does not fit its
  width.
- **A range that does not fit its declared storage:** `int8(0, 1000)` is a compile error —
  the range determines the wire, the type name determines the storage, and a legal wire
  value the storage truncates would be silent corruption that passes read validation. (This
  check left the spec in draft 2 as vestigial — storage was derived then; with the integer
  family naming storage explicitly, it returns with a real job.)
- **Attribute discipline:** an unknown attribute key, a key repeated, an attribute on a type
  that does not take it, `min` without `max` (or vice versa), and `resolution` without both
  bounds are compile errors, each naming the field and the legal vocabulary for its type.
- **Degenerate ranges:** any ranged integer with min ≥ max (the diagnostic suggests `const`
  for a fixed value); `[Min..N]T` with Min ≥ N; `string(N)` with N < 2; `bytes(<= N)` with
  N < 1; an enum whose max is 0 (fewer than two wire values); `resolution` ≤ 0. Every
  runtime treats `min == max` range serialization as API misuse, so the language rejects
  what the runtimes would reject.
- Enum `max` below variant count − 1; enum values above `max` unreachable by construction.
- **Duplicate field names anywhere in one type — including across branch sides and cases.**
  One name, one field, declared once. (serialize.modern permits exclusive-side member reuse
  because members pre-exist there; schema owns the type, and unique names keep the
  flattened generated output unambiguous.)
- Back-reference violations (§4.5); a `switch` subject that is not an integer or enum field;
  duplicate case values; `if` conditions that are not `bool` fields.
- Cycles in type composition, undefined types, duplicate declarations, `package` mismatch.
- **Target-name safety:** a declaration or field name that is a reserved word in any target
  language (`type`, `match`, `impl`, `func`, `class`, ...) or that collides with another name
  after Go export-casing (`atRest` → `AtRest`) is rejected, with a diagnostic naming the
  target and the collision. No escaping machinery; rename at the source.

### 4.7 Strings are byte strings — PROPOSED

`string(N)` carries **arbitrary bytes excluding 0x00**, length in [0, N−1]. All four
generated readers reject interior nulls; writes assert per §5. This single tightening is what
lets the targets agree:

- Classic C++ `serialize_string` is strlen-based `char[N]` — it cannot represent interior
  nulls; a Go writer must not be able to produce a payload the C++ reader silently truncates.
- Rust `String` storage would impose UTF-8 validation the other targets lack, so Rust storage
  is `Vec<u8>` (§6.1) and text encoding is the application's concern.

For every legal write the wire bits are identical to `serialize_string`. If UTF-8 text with
enforced validity is wanted later, it is a new wire type, not a redefinition of this one.

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
  order** (his words). The order is **sorted by message name** (PROPOSED: independent of file
  layout and declaration shuffles; declaration-order is the alternative if he prefers
  append-at-the-end reading). `MessageType` is a claimed name: a unit with messages may not
  declare its own.
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
- Every message is also an ordinary type: its own `Write`/`Read`/`MaxBits`/`MaxBytes`, usable
  standalone and composable as a field.

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

- **`[interpolated]`** — *"this is sent to the client for interpolation, it's visual
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
| `ShipData_Shallow` + serialize | the `[interpolated]` fields, **quantized wire encodings** from the field's attributes — the wire-side implementation detail of interpolation |
| `ShipData_Interpolate` | the same `[interpolated]` fields in **continuous storage** (the un-quantized twin) |
| `Quantize` / `Unquantize` | the mapping pair between Interpolate and Shallow — the hand-written `Quantize()` in today's `Ship.h`, generated |

**PROPOSED view-encoding syntax** (the delta-pass design surface, opened early — flat
attribute lists, his form): `[interpolated]` alone means *same encoding as deep*;
`[interpolated, quantize = K, max = Bound]` means *quantized at scale K within ±Bound on the
shallow wire, continuous in Interpolate*; `[interpolated, min = A, max = B]` on a float
means *ranged-int projection on the shallow wire* (today's health/thrust pattern). The
worked example is [`examples/Objects.schema`](examples/Objects.schema); **Missile,
DynamicProp and Turret follow once the Ship shape is approved** (his list, same day).

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

- **`int128` / `uint128`** — DECIDED as part of the integer family (Glenn, 2026-08-05), with
  a named customer: **ludicrous mode** (*"we will need int 128 for ludicrous mode :)"*).
  Deferred on a named prerequisite: **no serialize runtime speaks 128 bits today** — the wire
  construct (two qwords, low first, per the family's low-first rule) needs serialize methods
  added across all four runtimes before the keyword goes live. Storage is native in Rust
  (`i128`/`u128`) and C# (`Int128`/`UInt128`), emulated in C++ and Go (two-qword structs).
  The surveyed delta-prediction arithmetic already needs 128-bit intermediates too. Keywords
  reserved now so nothing squats on them.
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
- **Fixed point** — coming, runtime-first (Glenn, 2026-08-05: *"we will also be doing fixed
  point in serialize there first, and then bringing that to this language"*). The sequencing
  is the decision: the wire construct gets designed and proven in the serialize C++ library,
  propagated across the family, and only then surfaces as a schema wire type — the same
  order serialize.cs followed. `fixed` is informally reserved alongside the horizon words.
- **Wide strings** (`serialize_wstring`): no near-term need; when added, they match the
  classic wire exactly (length, then unaligned 32 bits per code point).
- **Relative integers** (`serialize_int_relative`): the classic use is a strictly-increasing
  sequence across *array elements* (previous element's field feeding the next), which the
  back-reference rule cannot express; a scalar-to-scalar form inside one type earns too
  little to carry the construct. Deferred until a cross-element form is designed.

## 5. Trust model — inherited

Reads validate everything — integer ranges, enum bounds, alignment padding, constants,
reserved bits, count bounds, string lengths and the interior-null rule — and fail on any
violation, because network input is the trust boundary. Generated read code never lets a
value that controls iteration go unchecked before use (serialize.go documents this exact DoS
vector).

Writes assume trusted data. Misuse follows each runtime's own convention: C++ debug asserts
(unchecked in release), **Go and Rust panic and C# throws on misuse in all build modes** —
the generated write code's job is to make misuse impossible by construction (bounds come from
the schema). Ranges are trusted inputs everywhere: generated code never feeds
attacker-influenced values as min/max.

**Read failure leaves the output object in an unspecified state**; callers use it only on
success. **Read success fully initializes it**: fields in untaken branches are set to their
zero values (0, 0.0, false, empty bytes, zero count). Write reads only taken fields. This is
what makes whole-object comparison in the conformance matrix well-defined.

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
   | `bits(N≤32)` / `bits(N>32)` | `uint32_t` / `uint64_t` | `uint` / `ulong` | `uint32` / `uint64` | `u32` / `u64` |
   | `bool` | `bool` | `bool` | `bool` | `bool` |
   | `float32` / `float64` (attributed or bare) | `float` / `double` | `float` / `double` | `float32` / `float64` | `f32` / `f64` |
   | enum `E` | `enum class E : uint32_t` | `enum E : uint` | `type E uint32` + consts | `#[repr(transparent)] struct E(pub u32)` + consts |
   | `string(N)` | `char[N]` | `byte[]` (bound-checked) | `string` | `Vec<u8>` (bound-checked) |
   | `bytes(N)` | `uint8_t[N]` | `byte[]` (length-checked) | `[N]byte` | `[u8; N]` |
   | `bytes(<=N)` | `uint8_t[N]` + `int32_t` count | `byte[]` (bound-checked) | `[]byte` (cap-checked) | `Vec<u8>` (bound-checked) |
   | `[N]T` | `T[N]` | `T[]` (length-checked) | `[N]T` | `[T; N]` |
   | `[<=N]T` | `T[N]` + `int32_t` count | `T[]` (bound-checked) | `[]T` (bound-checked) | `Vec<T>` (bound-checked) |

   Enums are integer-backed named types in every target because `[max = ...]` headroom makes
   non-variant values wire-legal; a native Rust `enum` cannot hold them — C#'s `enum E : uint`
   can, natively, which is why it needs no newtype.

2. **`Write(buffer, object) -> bytesWritten`** — straight-line write code in wire order.
3. **`Read(buffer, object) -> ok/error`** — straight-line read code with full validation, in
   each runtime's native error idiom (`bool` in C++, `error` in Go, `Result` in Rust).
4. **`MaxBits` / `MaxBytes`** — constants: the longest path through the schema, with
   worst-case (7-bit) padding assumed at each alignment point. Size write buffers from
   `MaxBytes`. Conservative is correct for a buffer bound.
5. **`ProtocolId`** — one constant per unit (§3).
6. **For a unit with `message` declarations (§4.8):** the `MessageType` enum (`None = 0`,
   then the messages, sorted by name), `WriteMessage`/`ReadMessage` with the tag framing,
   and the dispatch surface in each language's own idiom — representation per target,
   behavior identical.

**There is no generated measure function** — DECIDED (Glenn, 2026-08-04: measure *"was
always a bit of a hack"*). `Write` returns the actual size, `MaxBytes` sizes buffers, and
that covers the real uses. Anyone who genuinely needs per-object sizing can hand-write
stream code beside the generated code — the runtimes are unchanged and the two mix freely on
the same wire. If a real need appears, an opt-in exact measure (a generated running bit
count — the runtimes' `MeasureStream` is deliberately conservative and would not be used) is
a clean later addition.

The generated API mirrors serialize.modern's `schema<...>` members — `Write`, `Read`,
`MaxBits`, `MaxBytes` — so the two feel like one family.

**Alignment is stream-relative**: a type containing `align`, `string` or `bytes` has
layout dependent on its entry bit offset. Generated functions are correct at any entry
offset; the same type works standalone and nested. `MaxBits` covers the worst case.

**Output layout**: one file per target per unit — `<package>.schema.h` (header-only, C++),
`<package>.schema.cs`, `<package>_schema.go`, `<package>_schema.rs` — each headed by the
compiler version, the source files, the protocol id, and a do-not-edit line. Output is **deterministic to the
byte** for identical input; no external formatter runs in the build or test path (goldens pin
the emitters' own output — clang-format/rustfmt version drift must not be able to break a
golden). The emitters are written to produce formatter-clean code instead.

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
three backends stay simple enough to verify against each other. What it forgoes: the
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
schema generate --lang cpp,cs,go,rust --out <dir> [dir|files...]
schema id     [dir|files...]          // print the protocol id
schemafmt     [dir|files...]          // the canonical formatter — gofmt's philosophy:
                                      // one style, no options ("schema fmt" is its alias)
```

### 7.1 Pipeline

```
*.schema → scanner → parser → AST → resolver/checker → IR → {cpp, go, rust} backends
```

- **Scanner/parser: hand-written, recursive descent**, in the style of the Go toolchain's own
  `go/scanner`/`go/parser`. No parser generators. The language is LL(2); hand-rolled parsing
  is what makes precise diagnostics cheap. The parser recovers at declaration boundaries so
  one error does not hide the rest.
- **Resolver/checker**: name resolution across the unit's files, constant folding (checked
  signed 64-bit), the shape checks of §4.6, the dominance rule of §4.5, and **the message-set
  extraction** — the symbol table over `message` declarations from which `MessageType` and
  the dispatch are generated (§4.8).
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
  explicit; names of structs, fields and variants are carried (§3.1). The canonical encoding
  of this IR is the protocol-id input and a frozen public contract.
- **Backends: dumb printers.** Hand-written emitters (a small indent-aware writer helper),
  not `text/template` — codegen wants precision. Each backend is a single file a reviewer can
  hold.

### 7.2 Testing

1. **Golden source tests**: schema in, generated source compared byte-for-byte against
   checked-in goldens, all three backends. Deterministic output makes this exact.
2. **Golden ids**: `schema id` over the conformance corpus pinned as exact values — the
   frozen-encoding contract's tripwire.
3. **The wire oracle**: for each conformance schema, a hand-written classic-serialize stream
   twin in C++. Generated C++ must produce byte-identical output to the twin and each must
   decode the other — the same gate serialize.modern runs, against the same oracle.
4. **The cross-language matrix — the whole point**: every writer × every reader (16 pairs),
   property-driven random instances, identical bytes, identical decoded values under §5's
   whole-object rule. **This needs per-target generated test scaffolding** — instance
   generators respecting every range/branch/count, and a canonical dump format for
   cross-language value comparison — budgeted as part of the conformance harness, not
   hand-written per schema.
5. **Malformed-input agreement**: bit-flip sweeps over valid packets; all three generated
   readers must agree on accept/reject and on the decoded value — serialize.modern's
   brute-force read-validation gate, extended across languages.
6. **Compiler robustness**: Go-native fuzzing on scanner, parser and checker; every
   diagnostic in the suite asserted by exact message and position.

CI needs all three toolchains for gates 3–5; that cost is accepted — it is the product's
central claim.

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

## 8. Repository layout

```
cmd/schema/            the CLI
internal/scanner/      tokens, positions
internal/parser/       AST construction, error recovery
internal/ast/
internal/check/        resolver, constant folding, shape checks, dominance rule
internal/ir/           the lowered form + its canonical encoding (protocol id)
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

1. **Strings as byte strings** (§4.7) — confirm; a validated UTF-8 wire type can come later.
2. ~~**Storage-type overrides**~~ — **RESOLVED 2026-08-05 by the integer family**: storage
   is declared by the type name (`thrust int8(0, 100)`), no override mechanism needed.
3. **Wide strings and relative integers deferred** (§4.10) — confirm nothing near-term needs
   them; int_relative wants a cross-element design.
4. **`schemafmt` timing** — the tool is DECIDED (name, gofmt philosophy: one style, no
   options); is it v1 alongside the compiler, or fast-follow? Its style rules should be
   written while the corpus is small either way.
5. **Doc comments** carried into generated code — cheap, worth having; v1 or later?
6. **A root/packet marker** — scopes the protocol id to reachable declarations (fixes the
   unused-helper-moves-the-id wart, §3.2), names which structs get buffer-sizing guidance,
   and is the natural place for a future `packet` keyword. v1 or v2?
7. **Const expressions over enum counts** — `Constants.h` computes `NUM_TEAMS =
   Team_MAX + 1` and `MAX_PROPS` as a sum of limits; the language wants a way for a `const`
   to reference an enum's variant count / max (`len(Team)`? `Team.max`?). Surfaced
   2026-08-05.
8. **Platform-conditional constants** — `FRAME_SAFETY` differs under `__linux__`; schema
   constants are platform-uniform today. Stays hand-written in game code, or gains a
   target/platform story later. Surfaced 2026-08-05.
9. **Explicit enum variant values and flag enums.** v1 numbers variants implicitly 0..n−1;
   Space Game's real enums all write `None = 0` explicitly and one is a `(bit_flags)` mask.
   Implicit numbering happens to match the None-first style, but the evidence says explicit
   values (and possibly a `flags` enum form whose wire is `bits(N)`) deserve a v1 decision,
   not a deferral — this also settles whether variants stay whitespace-separated or gain a
   separator to make room for `= value`.
10. **Sentinel-terminated collections.** The surveyed baseline/delta/explosion packets do not
   use counted arrays — they are bool/sentinel-terminated element streams (serialize.go ships
   `Continue`/`Until` helpers for exactly this), sized by mid-stream packet splitting. v1
   cannot express them. The splitting half is emitter-driven and probably stays application
   code; whether the plain bool-continuation list deserves a v1 construct is the open half.
   Evidence: `examples/README.md`, finding 3.
11. **Enum subranges.** The surveyed create path writes an object kind as `[1, max]`,
   excluding the `None` variant from the wire; schema's enum wire is always `[0, max]`.
   Cheap to live without (one wasted wire value), cheap to add (`kind CraftKind(1, max)` or a
   `no-None` form). Evidence: `examples/README.md`, finding 1.
12. ~~**`int` → `int32`?**~~ — **RESOLVED 2026-08-05 by the integer family**: bare `int` is
    gone; every integer names its width, Go-style, and `int`/`uint` are reserved purely to
    give a helpful diagnostic.

---

*Draft 2, Rowan, 2026-08-04 — from serialize.modern's SCHEMA.md and README (read whole), the
extracted serialize / serialize.go / serialize.rs contracts in `notes/`, Glenn's decisions of
tonight, and two independent cold reads of draft 1 (wire claims; language design and pipeline
feasibility) whose findings are folded in above with their reversals named. Wire claims name
their classic twins but are not yet pinned by tests; the conformance suite in §7.2 is what
converts this document from intent to property.*
