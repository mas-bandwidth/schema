# schema — specification

This document is the normative reference for the schema language, its wire
encodings, and the code its compiler generates. Where the compiler and this
document disagree, one of them is a bug (see VERSIONING.md).

## 1. What schema is

**schema** is a small language for describing bitpacked network data, and a
compiler — written in Go — that translates `*.schema` files into generated C,
C++, C#, Go, JavaScript and Rust source code targeting the serialize family of
runtime libraries:

| target | runtime library |
|---|---|
| C | [serialize.c](https://github.com/mas-bandwidth/serialize.c) |
| C++ | [serialize](https://github.com/mas-bandwidth/serialize) |
| C# | [serialize.cs](https://github.com/mas-bandwidth/serialize.cs) |
| Go | [serialize.go](https://github.com/mas-bandwidth/serialize.go) |
| JavaScript | [serialize.js](https://github.com/mas-bandwidth/serialize.js) |
| Rust | [serialize.rs](https://github.com/mas-bandwidth/serialize.rs) |

The runtimes are bit-for-bit wire compatible, pinned in CI with shared golden
bytes. schema inherits that foundation: **a type serialized by generated code
in any target language decodes identically in the others.**

The idea is extracted from
[serialize.modern](https://github.com/mas-bandwidth/serialize.modern)'s
compile-time schema language, which proved that a schema that sees the packet
as a type can generate radically faster serialization than a runtime stream
while holding wire compatibility to the byte. What a template-metaprogramming
host cannot escape is that the metaprogram re-runs inside every consumer's
C++ compiler, requires C++23, and can only ever emit C++. schema moves the
same computation into an external compiler: run once, at code-generation
time, emitting for every platform, with zero compile-time tax on the
consumer.

### Goals

1. **One source of truth for a protocol.** Types and their wire encoding are
   defined once, in `*.schema` files; read and write code is generated for
   every target language.
2. **Minimal generated code.** Separate, straight-line read and write
   functions per type — the code a careful expert would hand-write against
   each serialize library — with no runtime schema interpretation, no
   reflection, and no `IsWriting` branching.
3. **Byte-identical wire output across all targets**, enforced by a
   conformance matrix in CI, with classic serialize as the oracle. The
   generated readers also agree on what they **reject** — acceptance is part
   of the wire contract.
4. **Compile-time cost near zero for consumers.** Generated code is plain
   source; the C++ target requires only what classic serialize requires.
5. **Errors a person can act on.** Every diagnostic carries file:line:column
   and names the fix.

**Scope (v1).** schema fully defines the generated code for constants, enums,
flags, types, messages, tables and object definitions. Delta serialization is
out of scope for v1.

### Non-goals (v1)

- **No wire-format versioning on the realtime wire.** Versioning is by
  protocol id, deliberately (§3). Data that must outlive builds uses the
  table wire (§4.11), which is evolution-tolerant by design.
- **No unbounded collections.** Everything on the wire has a declared bound,
  as everywhere in the serialize family.
- **No annotation of existing hand-written types.** schema owns the types it
  serializes and generates them (§6); the `cpp_native` mapping (§4.2) lets
  hand-written types derive *from* generated ones, never the reverse.
- **No imports across compilation units.** One unit, one package, all files
  compiled together (§3.2).
- **No self-describing wire data.** The wire stays an unattributed bit
  stream; all knowledge lives in the generated code on both ends.
- **Deferred constructs:** wide strings (`serialize_wstring`) and relative
  integers (`serialize_int_relative`) are not in v1 — see §4.10.

The grammar must not claim syntax reserved for planned language passes:
`packet`, `delta`, `baseline` and `index` are usable as ordinary names today
but are earmarked for future constructs and must not be given other meanings.

## 2. The name and the files

The language is called **schema**. Schema files use the `.schema` extension,
named in UpperCamelCase after their contents.

The conventional file layout is aspect-oriented — `Constants.schema`,
`Enums.schema`, `Types.schema`, `Messages.schema`, `Objects.schema` — one
aspect per file. This is a convention the corpus and documentation follow and
`schemafmt` respects, never compiler-enforced; cross-file resolution is
order-free (§3.2), so the layout carries no semantic weight.

## 3. Versioning: the protocol id

**Only two sides holding the same protocol id can talk. There is no
versioning overhead on the wire, no optional-field tags, no evolution
machinery.** This is the serialize-family model: for realtime network data,
client and server ensure they are at the same protocol version before they
exchange a byte, which radically simplifies everything downstream.

**The protocol id is a hash of the schema's wire shape.** The compiler
computes it; nobody maintains it by hand; two sides with different wire
formats cannot accidentally claim compatibility.

### 3.1 The hash

**The id is the low 64 bits of SHA-256 — the final 8 bytes of the digest,
interpreted big-endian — over the unit's wire shape projection**
(`ir.WireProjection`, printable with `schema projection`): a canonical text
listing exactly the facts that determine the bytes on the wire, and nothing
else.

**Why a projection, and not the source text or the IR.** A source-text hash
is safe in the only direction that matters — it can produce a spurious
MISMATCH (peers refuse to talk when they could) but never a spurious MATCH
(peers agree when their bytes differ) — but it moves the id for a comment, a
renamed file, or an edit that costs zero wire bits, and each of those buys a
coordinated redeploy for nothing. Hashing the IR structs directly would fix
the churn and introduce the dangerous direction: any wire-affecting fact the
walk forgot becomes two incompatible builds shaking hands. The projection is
TEXT: what the id depends on can be printed, read and diffed, and a fact
missing from it is a review question rather than an implementation detail.

**Included — each fact moves the wire:** the package; message and object
ordinals (the tag IS the index); contexts; every type's field order; field
NAMES (the table wire's field identity is `fold16(fnv1a32(name))`); type
kind, width and signedness; declared bounds; array kind and bounds;
string/bytes capacity; float range, resolution and step count; fixed `I` and
`F`; quantize scale and bound; specified defaults (the table wire elides a
field sitting at its default); branch structure; `const`/`reserved`/`align`
items; enum max and storage bits; flags wire bits; union variant order,
count and payload type references (the tag is positional and the payload is
the wire — §4.8); `[local]` and `[interpolate]` markers.

**Excluded — each has no effect on the bytes:** comments and whitespace; file
names, file layout and declaration order; enum variant names, union variant
names included (the ordinal is the wire — renaming `Red` to `Crimson`, or a
union's `box` to `crate`, leaves every byte identical); `const`
declarations (their values are already resolved into the bounds above); type
tags and native-type attributes.

**The projection is versioned.** `ProjectionVersion` rides its first line, so
a change to the rendering itself moves every id deliberately and visibly
rather than silently.

**The property, tested in both directions**
(`internal/check/projection_test.go`): an edit that moves no bytes must not
move the id, and an edit that moves bytes must. The second is the one that
must never regress.

### 3.2 The unit

A compilation unit is a set of `*.schema` files compiled together (one
directory by default). **Exactly one `package` per unit** — a mismatch is an
error; `package` appears once per file, as its first declaration. Names
resolve across all files in the unit — a declaration may sit in any file, in
any order, with no semantic effect. One id per unit, exposed as a constant
(`ProtocolId` / `PROTOCOL_ID`) and printed by `schema id`.

The id depends on the projection's facts (§3.1) and nothing else.
Consequences, in both directions:

- **Every declaration contributes.** The projection lists all of the unit's
  declarations, not a reachable subset, so an unused helper type moves the id
  like a used one. Accepted: it fails safe (sides refuse to talk when they
  actually could), and under the ship-together model the redeploy was
  happening anyway.
- **File layout, declaration order, comments, whitespace, and enum variant
  renames move nothing.**
- **Adding a target language never moves the id**, and a compiler upgrade
  moves it only through a deliberate `ProjectionVersion` bump. The corollary
  is a real obligation on the compiler: the same schema must produce the same
  wire across compiler versions. The conformance corpus's golden wire bytes
  pin every construct's encoding permanently, and a compiler change that
  breaks a wire golden is a stop-the-line event, never a quiet fix (§7.2).

`reserved(bits)` fields remain useful *within* a protocol id — not to dodge
redeploys (claiming reserved bits moves the id like any other wire-shape
change), but to keep packet sizes and layout budgets stable while a protocol
grows into space already paid for.

Whether the id travels on the wire (a connect token, a `const` field, out of
band) is the application's choice — netcode-style stacks already carry one.

## 4. The language

### 4.1 Lexical structure

- **Encoding:** UTF-8 source; a UTF-8 BOM is rejected at parse. `//` line
  comments and `/* */` block comments (non-nesting). Comments are invisible to
  code generation and preserved by `schemafmt`.
- **Doc comments are deferred**, with the design pinned for when they land: a
  `//` comment block whose last line immediately precedes a declaration,
  field, or enum variant — no blank line between — becomes that item's doc
  comment, carried through the IR and emitted in each target's own idiom
  (`///` Rust, `/// <summary>` C#, preceding-line `//` Go, `//` C++).
- **Identifiers:** `[A-Za-z_][A-Za-z0-9_]*`. Conventions: type and enum names
  `UpperCamelCase`, enum variants `UpperCamelCase`, fields
  `lower_snake_case`, constants `UpperCamelCase`. The compiler does not
  enforce case; the corpus and all documentation follow it.
- **The register is Go-inspired.** Types are declared with `type`, not
  `struct`; declarations put the name first, then the type; scalar names are
  Go's (`float32` and `float64`, never `float`/`double`); `if` and `switch`
  take no parentheses; array bounds are a prefix (`[<= MaxObjects]ObjectState`);
  there are no semicolons; the canonical formatter is `schemafmt` — one
  style, no options.
- **Integer literals:** decimal, hex (`0x`), binary (`0b`). **Float
  literals** (decimal, with optional fraction and exponent) appear in float
  constants and as float attribute values (`min`/`max`/`resolution` on
  `float32`).
- **Punctuation and operators:** `{ } ( ) [ ] , : = ! . .. <= + - * / %`
  (maximal munch: `..` wins over `.`).
- **Reserved words:** `package const enum type table message object if else
  switch case align reserved`, the wire-type keywords `bits bool float32
  float64 string bytes fixed ufixed`, and the integer family `int8 int16
  int32 int64 uint8 uint16 uint32 uint64 int128 uint128` — plus `int` and
  `uint`, reserved so `int` gets a "did you mean int32?" diagnostic instead
  of a parse error. Reserved words cannot be used as names. Attribute keys
  (`min`, `max`, `resolution`, ...) are contextual — they live only inside
  `[ ]` and are not reserved.
- **Newlines terminate declarations and fields — there are no semicolons,
  like Go.** The newline is a terminator token, suppressed: immediately after
  `{`, `(`, `[`, `,`, `:`, `=`, `else`, and an infix operator; and
  immediately before `)`, `]`, `}`. Blank lines are insignificant. `{` sits
  on the same line as its construct; `} else {` is written on one line — a
  newline between `}` and `else` is tolerated by the parser and rewritten by
  the formatter. Two consequences make the grammar implementable as written:
  **a closing `}` also terminates the item before it** — in every production
  below, a trailing `NL` is satisfied by an actual newline or by the
  immediately following `}` — and **end-of-file synthesizes a terminator**,
  so a file without a trailing newline still parses. Within an enum or flags
  body, newlines around commas are ordinary whitespace (suppression after
  `,`) — variants are not items and need no terminator.

### 4.2 Grammar

EBNF (`NL` = the newline terminator; `{X}` repetition; `[X]` option):

```
File        = { Declaration } .
Declaration = Package | Const | Enum | Flags | TypeDecl | Message | Object | Union | Contexts .
Object      = "object" ident Block NL .
Flags       = "flags" ident [ Attributes ] VariantList NL .        // "flags" contextual, §4.2
Union       = "union" ident UnionBlock NL .                        // "union" contextual, §4.8
UnionBlock  = "{" { UnionVariant } "}" .
UnionVariant = ident ident NL .                                    // variant name, then its payload type
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
            | "ufixed" "(" IntExpr "," IntExpr ")"               // the unsigned sibling (§4.3)
            | ident .                                            // a declared type or enum
IntType     = "int8" | "int16" | "int32" | "int64"
            | "uint8" | "uint16" | "uint32" | "uint64" .
Bound       = IntExpr | "<=" IntExpr | IntExpr ".." IntExpr .

Attributes  = "[" Attr { "," Attr } "]" .                        // trailing, optional, per field
Attr        = ident "=" ( ConstExpr | ident | string )           // valued:    min = 0, round = up,
                                                                 //            cpp_include = "a.h"
            | ident .                                            // valueless: interpolate, local
                                                                 // (ident values are keyword-like:
                                                                 // each valued key declares whether
                                                                 // it takes an expression, a word,
                                                                 // or a quoted string)

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

- **Integer expressions** evaluate at compile time in arbitrary precision,
  Go's untyped-constant style, with a fit-check at each use site — so
  `0xFFFFFFFFFFFFFFFF` is writable for a full-range `uint64` bound while
  overflow at a use site stays a compile error. Division by zero, and a
  negative result where a width, bound or count is required, are compile
  errors. Integer division truncates toward zero, Go's rule. **Float
  attribute values (`min`/`max`/`resolution` on `float32`) are float
  expressions** — literals, const names, and arithmetic, per `FloatExpr`;
  integer constants convert in float positions.
- **Zero initialization is the rule, in every generated language, with an
  optional specified default overriding it.** Every generated type fully
  initializes to zero values on construction — 0, 0.0, false, `None` for an
  enum, `None` for a union, 0 for a flags mask, zeroed buffers and counts,
  recursively for nested types — in every target (C++ emits default member initializers; the other
  languages get it from their own rules, stated so no target can silently be
  the odd one out). A field may override its zero with `= value` after the
  attributes: `invulnerable bool [local] = true`. Defaults cover bool
  (`true`/`false`), integer and float fields (constant expressions,
  fit-checked like any use site), enum fields (a variant name), the 128-bit
  integers, and `fixed` (§4.6); arrays, strings, bytes, composite and union
  fields zero-initialize with no override. **The default is STORAGE initialization —
  what a freshly constructed object holds. It does not touch the wire**: per
  §5's read rule, fields in untaken branches read as ZERO values, not as
  defaults — the wire contract stays a pure function of the schema's
  encodings. (On the table wire, a field sitting at its specified default is
  elided — §4.11.)
- **Constants compose.** A `const` may reference any other `const` in the
  unit, order-free across files like type references; reference cycles are a
  compile error naming the cycle. Example:
  `const MaxPositionUnits = MaxWorldMeters * PositionUnits`.
- **Constants are typed, on Go's untyped-constant model.** A bare `const`
  takes its **kind** from its expression — integer (arbitrary-precision
  arithmetic, fit-checked at each use) or float (float64 arithmetic; `+ - *
  /` and parentheses, no `%`) — and converts wherever that kind fits: integer
  constants in any range, bound, width, case label or float position; float
  constants in float attribute values and float contexts. The kind follows
  Go's literal rule: a literal containing a decimal point or an exponent is a
  float literal, so a bare `const` whose expression contains a float literal
  or references a float constant **infers `float64`** — `const Rate = 1.5`
  exports in every target exactly as `const Rate float64 = 1.5` would; a bare
  integer expression infers `int64` storage as before. An **explicit type
  makes a typed constant** — `const MaxBounds uint64 = 12000` — which pins
  the exported type in every target.
- **Enum max references:** **`E.Max`** in any integer expression names enum
  `E`'s max — the derived count, or the widened `[max = K]` — the same number
  the enum's wire range and storage derive from. The count of wire values is
  `E.Max + 1` by ordinary constant arithmetic; the count of real (non-`None`)
  variants is `E.Max` (see the sentinel-zero convention below). Sizing
  enum-indexed tables with `E.Max + 1` stays correct under `[max]` headroom,
  where non-variant values are wire-legal. It works on generated sets too
  (`MessageType.Max`). `Max` after `.` is contextual, like attribute keys;
  the lexer keeps maximal munch, so `..` still wins over `.`.
- **Constants are platform-uniform.** A schema has no platform conditionals.
  One schema is one protocol id on every platform; a platform-conditional
  constant reaching any range, bound or width would let two builds carry the
  same id with different wires — the exact bug class the id exists to make
  impossible.
- **Parsing:** inside a type body one token of lookahead disambiguates every
  item: `const` + `(` is a constant field (versus the top-level
  `const name =` declaration); `if` / `switch` / `align` / `reserved` are
  keywords; any other identifier begins a field. The grammar is LL(2), and
  hand-written recursive descent is the intended implementation.
- **Enum variants** are comma-separated identifiers, trailing comma allowed.
  **Every enum has `None = 0` implicitly — universal, never declared.**
  Declared variants pack tightly from 1; max derives as the count; the
  `[max = K]` headroom attribute can widen the wire. **Enum storage derives
  from the enum's own max** — the smallest unsigned integer that fits.
- **Canonical form only.** Enums are always `0 = None`, then `[1, max]`,
  dense. There are no explicit variant values and no sparse enums.
- **The enum family is two declaration forms:**
  - **`enum`** — the canonical with-`None` form above. Wire: the raw value,
    in **[0, max]**; `None` is a legal wire value (the null — the shape of
    the generated `MessageType`/`ObjectType`).
  - **`flags Name { ... }`** — each variant names one bit, assigned densely
    from bit 0 in declaration order, **up to 64**; **storage is `uint64` in
    every target**; no implicit `None` — the empty mask is 0 and needs no
    name. Wire: **W raw bits, W = variant count** (`[max = K]` widens to K
    bits); every W-bit pattern is legal — a mask field's domain is all
    subsets. Each target exports one mask constant per variant (`1 << bit`),
    because mask tests are how flag state is consumed. **`flags` is a
    contextual keyword, not reserved** — it introduces a declaration at file
    scope only, so a *field* named `flags` stays fully legal; declarations at
    file scope otherwise begin with reserved words, so one token of lookahead
    disambiguates.
  - **The derived-range rule:** a field that indexes a declared set derives
    its range from the set, never restates it — both wires derive from the
    declaration; `min`/`max` are not valid on enum-family fields.
- **Type references are order-free** — a type or enum may be used before its
  declaration, in any file of the unit. (Field *back-references* are not
  order-free; §4.5.)
- `if` nests freely inside blocks (`switch` is not in v1; §4.4).

### Attributes — per-field, optional, keyed

Ranges and refinements are trailing per-field attributes in `[ ]`:

```
health      int16   [min = 0, max = MaxHealth]
thrust      int8    [min = 0, max = 100]
orientation float32 [min = -180.0, max = 180.0, resolution = 0.01]
sequence    uint16
```

- **Brackets never collide with array bounds, structurally:** an array bound
  is a **prefix** — `[<= MaxObjects]ObjectState`, Go's order — and attributes
  **trail** the complete type. Position alone disambiguates; no lookahead.
  Scalar constraints like `min`/`max` apply per element.
- **Valueless attributes** (the object view markers `[interpolate]` and
  `[local]`, §4.8) and valued keys sit side by side in one flat list —
  `[interpolate, min = x, max = y]` — valueless markers first, then valued
  keys; there is no nested argument syntax.
- **The line between positional and attribute:** a *size* that defines the
  type's shape stays positional — `bits(64)`, `string(64)`, `bytes(N)`,
  `fixed(I, F)`, array bounds. A *constraint or refinement* of a named type
  is an attribute. The enum's `[max = 15]` is the same syntax; it is one
  general mechanism.
- **The vocabulary is typed and closed per compiler version — an unknown
  attribute is a compile error**, never a silently ignored string. The
  vocabulary: integers take `min`/`max` (both together or neither); `float32`
  takes `min`/`max`/`resolution` (all three together — §4.3: this IS the
  compressed float); `fixed`/`ufixed`/`int128` take `min`/`max` (required —
  §4.3); enum declarations take `max`; type declarations take a tag and the
  `cpp_native`/`cpp_include` pair (below); `object` bodies additionally admit
  the view markers `interpolate` and `local`, `context = <name>` (legal only
  beside `[local]` — Contexts, below), and the view-encoding keys `quantize`
  and `round` (§4.8).

### Type tags — types are the user's; meaning is claimed later

**The language pre-defines no composite types** — no built-in vec3, no
built-in quat, no privileged class list. A user declares a type and may
**tag** it:

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

- **The type is entirely the user's** — name, body, component precision. The
  body alone defines storage and wire, like any type; a declared type
  composes anywhere in the unit, order-free.
- **The tag is one user-chosen identifier, in its own namespace** — any
  identifier is legal there, unlike field-attribute keys, which stay closed
  and checked. **In v1 a tag is inert**: parsed, carried through the IR,
  emitted as an annotation on the generated type, and it changes zero
  generated code.
- **Meaning arrives by claiming.** A future pass — the delta pass first —
  claims a tag and assigns its semantics and generated actions
  (interpolation, prediction, normalization, render mapping). Claiming is an
  ordinary compiler-version event, and the protocol id never moves for it:
  tags are excluded from the wire shape projection (§3.1). The claiming pass
  defines its own shape validation for claimed tags (e.g. `[quat4]` requiring
  four float components) and its own policy toward unclaimed strangers.
- The architecture: types on one side; a layer that defines the needed
  actions for those types on the other. v1 ships the types; each schema
  declares its own, and each application's actions bind to them by claiming.

### Native type mapping — `cpp_native` / `cpp_include`

The compiler generates the **basis struct** — data members only, no
behavior — and a hand-written math type **derives** from it, adding operators
and methods without touching layout:

```
type Vector3 [vec3, cpp_native = Vector3, cpp_include = "core_vector.h"] { ... }
```

```cpp
// core_vector.h, hand-written:
struct Vector3 : public space::Vector3 { /* operators, length(), ... */ };
```

With the mapping declared, generated C++ **storage speaks the native type**:
every field of a mapped type in every generated struct (states, data views,
render types, tables) declares `::Vector3` instead of `space::Vector3`, and
every generated header that references it emits `#include "core_vector.h"`.
Simulation code then does math on generated state directly — no conversion
layer, no per-site casts. Derivation guarantees layout identity — the emitted
`static_assert`s (trivially-copyable, standard-layout) still hold over
native-typed storage — so relocatability and the parallel scatter/gather
property (§4.11) survive untouched.

The rules:

- **`cpp_native` takes an identifier** — the global (`::`-qualified) C++ type
  name. **`cpp_include` takes a quoted header path** — the header declaring
  it. They go together: one without the other is an error. String literals
  (`"..."`, no escapes) exist in the grammar for attribute values only.
- **Inside the basis type's own generated header the mapping is off** —
  references there keep the basis name. The native header includes that
  header to derive from the basis; a mapped reference would be circular.
  Sibling types declared in the same schema file therefore store the basis
  type, which is the correct default for pure wire compounds.
- **Language bindings never move the protocol id.** `cpp_*` attributes rename
  what one target CALLS the storage; they cannot change a wire bit, and the
  projection (§3.1) excludes them.
- **C++ only, by prefix.** Other targets ignore `cpp_*` keys. If another
  target ever earns a native mapping, it gets its own prefixed keys and its
  own soundness argument; nothing is shared but the spelling convention.

### Contexts

Types sometimes need per-side local state — client-only or server-only
simulation fields — without preprocessor conditionals, which not all target
languages have. Contexts are declared in the schema, not baked into the
language:

```
// Contexts.schema
package example

contexts { client, server }
```

- **`contexts { ... }`** — one comma-separated list per unit (the same
  `VariantList` form as enums; `contexts` is contextual at file scope, like
  `flags`). Context names are user-chosen; lowercase by convention.
- **Per-`[local]` field, an optional `context = <name>` attribute:**
  `predicted_explode bool [local, context = client]`. An unscoped field
  defaults to `context = all` (every context); `all` is reserved and cannot
  be declared. **`context` is legal only beside `[local]`** — a wire field
  with a context is a compile error, because the wire must be identical on
  every side. The `context` key's legal values are closed *by the unit's own
  `contexts` declaration*, not by the compiler.
- **Generation resolves the variance — no preprocessor anywhere, one output,
  a type per context.** When a type or object carries context-scoped fields,
  the generator emits one variant per context, named by capitalizing the
  context onto the type — `ClientShipState`, `ServerShipState` — each holding
  the `all` fields plus its own context's. A type with no context-scoped
  fields generates once, unprefixed. Each side's code uses its own type.
- **Wire and protocol id are context-invariant by construction:** context
  fields are local-only, so every context generates byte-identical wire code,
  and the unit has one id for all contexts.

### Constants and enums are exported

Both are first-class in both directions:

- **Into the wire:** a `const` is usable in any range, bound, width or case
  label, folded at compile time; an enum is a wire type.
- **Out to the code:** every `const` and every `enum` is exported in the
  generated output of every target — `inline constexpr int64_t MaxPlayers =
  ...;` / `public const long MaxPlayers = ...;` / `const MaxPlayers int64 =
  ...` / `pub const MAX_PLAYERS: i64 = ...;` and the integer-backed enum
  types of §6.1 — because the values that shape the wire are the same values
  application code sizes its arrays and loops with. One declaration,
  everywhere; the application references the schema's constant instead of a
  hand-copied twin.

**The sentinel-zero counting convention.** Entry 0 of every enum is the
none/sentinel value, so **the count of real things is always `Enum.Max`,
never `Enum.Max + 1`**: `Team { None, Red, Blue }` has two real teams,
`NumTeams = Team.Max = 2`. Optionality rides in-band — "no team" travels in
the same variable as the team, with no separate has-flag — and it composes
with zero initialization: a zero-initialized enum field IS `None` by
construction, so a fresh struct starts in the null state without any code
saying so. This is a convention over the corpus and generated-code usage, not
a language rule — the language does not enforce entry 0's meaning.

**The extent is exported.** Every generated enum surface — each declared
enum and the generated `MessageType`/`ObjectType` tag enums — carries its
extent as a member named `Max` in the target's own convention: `E::Max`
(C++), `E.Max` (C#), `EMax` (Go), `E::MAX` (Rust), `E.Max` (JS), `E_MAX`
(C). Its value is the enum's max — the same number `E.Max` names in schema
expressions (§4.2): the highest wire-legal value, which under the
sentinel-zero convention is the count of real variants when no `[max]`
headroom widens it. Application code states ranges and asserts directly
against it (`ShipType`'s wire range is `[0, ShipType.Max]`, its real
variants `[1, ShipType.Max]`) instead of exporting a hand-declared count
constant that re-derives it. `Max` is therefore a reserved variant name —
declaring a variant named `Max` is refused at check time, exactly as `None`
is.

### 4.3 Field types and their wire encodings

**The wire model — normative:**

- **Bit order is LSB-first**: successive values OR into the stream at
  increasing bit offsets, no per-value reversal — **bit i of the stream lives
  in byte i/8 at bit position i%8.**
- The writer accumulates into a 64-bit scratch flushed as **little-endian
  words**; the final flush zero-fills — the stream's logical length is
  **ceil(bits/8) bytes**, and `Write` returns exactly that.
- **All >32-bit quantities go low 32 bits first**, then the high remainder
  (`bits(N>32)`, `uint64`, `float64`, ranged encodings wider than 32 bits).
  **The 128-bit family and fixed point generalize the same rule: the value
  splits into full 32-bit groups from the least significant upward, the final
  group carrying the remainder, up to four groups.**
- **Ranged integers encode `value − min`, unsigned, in exactly
  `bits_required(min, max)` bits** — the bit width of (max − min), computed
  in the range's own width (clz over 32 or 64 bits as the range demands); no
  zigzag on the wire, ever.
- **`align` emits zero bits up to the next byte boundary — zero bits when
  already aligned**; readers verify the padding is zero.
- **Length prefixes** (`string(N)`, `bytes(N)`, `[<= N]T`, `[Min..N]T`) are
  ranged integers over their declared count range, per the rows below.

The wire encodings are exactly classic serialize's — each row names its
classic twin, which is the wire oracle for the stated model.

| schema | wire | classic twin |
|---|---|---|
| `f bits(N)` | N raw bits, N in [1,64] | `serialize_bits` ([1,64]; >32 = low 32 bits first, then the high remainder) |
| `f intN` / `f uintN` (bare, N ∈ 8/16/32/64) | N raw bits (two's complement for signed) | `serialize_uint8/16/32/64`; signed raw is the same bits, cast |
| `f intN [min = A, max = B]` / `f uintN [min = A, max = B]` | minimal bits for the range, value − A; read rejects out-of-range; **the range must fit the declared storage** | `serialize_int` (≤32-bit int ranges) / `serialize_int64` / width-computed `serialize_bits` for full-unsigned ranges |
| `f uint128` (bare only) | 128 raw bits — the low 64-bit half first, then the high half; representation-independent (native `__int128` and the emulated two-lane pair produce identical bytes) | `serialize_uint128` |
| `f int128 [min = A, max = B]` (range required) | value − A in `bits_required128(A, B)` bits, 32-bit groups from the bottom; read rejects an offset above B − A — reject, never clamp; **where the range fits 64 bits or fewer the bytes are identical to `serialize_int64` over the same bounds.** Bare `int128` and ranged `uint128` are compile errors — serialize's own surface, mirrored exactly (uint→raw, int→ranged) | `serialize_int128` |
| `f fixed(I, F) [min = A, max = B]` (range required; **signed**) | Q I.F, the sign bit counting toward I; storage is a signed integer of exactly I+F bits (I+F ∈ 8/16/32/64/128, I ≥ 1, F ≥ 0); bounds are compile-time WHOLE UNITS fitting the Q format and int64; wire = raw − (A << F) in bitlen(B − A) + F bits, 32-bit groups from the bottom — **except A == B, which costs ZERO bits (not F): the reader materializes raw = A << F from the range alone (§4.6)**; read rejects above the raw range; round trip is EXACT (no quantization step), and with F = 0 the operation IS a ranged integer | `serialize_fixed` |
| `f ufixed(I, F) [min = A, max = B]` (range required; **unsigned**) | UQ I.F: no sign bit, whole-unit domain [0, 2^I); storage is an UNSIGNED integer of exactly I+F bits (I+F ∈ 8/16/32/64/128, I ≥ 1, F ≥ 0); bounds are compile-time WHOLE UNITS fitting the unsigned domain and int64 (so I ≥ 63 clamps to int64's ceiling); the wire law is fixed's own — raw − (A << F) in bitlen(B − A) + F bits, A == B costs ZERO bits, read rejects above the raw range, round trip EXACT. The raw values of wide formats legitimately fill uint64's HIGH HALF (above 2^63): every route through a signed-typed runtime API is a bit-exact cast or zero-extension, never sign extension, and the corpus pins that byte-for-byte | `serialize_fixed` (unsigned storage — the codec is storage-generic) |
| `f bool` | 1 bit | `serialize_bool` |
| `f float32` | 32 raw IEEE-754 bits | `serialize_float` |
| `f float64` | 64 raw bits (low dword first) | `serialize_double` |
| `f float32 [min = A, max = B, resolution = R]` | quantized to ceil((B−A)/R) steps — the actual step is (B−A)/ceil((B−A)/R), ≤ R; read rejects values above the step count | `serialize_compressed_float` (exact formulas incl. the ceil, +0.5f rounding and clamp); storage stays `float32` — the attributes describe the wire |
| `f Weapon` (an enum) | minimal bits for [0, max]; read rejects above max | `serialize_int` over [0, max] |
| `f Damage` (a `flags` declaration, §4.2) | W raw bits, W = variant count (or [max]); every pattern legal; storage `uint64` in every target | `serialize_bits` |
| `f Inner` (a type) | Inner's fields, in place | `serialize_object` |
| `f Shape` (a `union`, §4.8) | tag in minimal bits for [0, variant count] (0 = None, no payload), then the selected variant's payload only; read rejects a tag above the count | `serialize_int` over [0, count] + `serialize_object` on the selected arm |
| `const(Value, Bits)` | the constant; read **rejects** any other value | `serialize_bits` + compare |
| `reserved(Bits)` | zeros; read rejects nonzero | `serialize_bits` + compare |
| `align` | zero-pad to the next byte boundary; read rejects nonzero padding | `serialize_align` |
| `f string(N)` | length in [0, N], align, then the used bytes — N = max length; the bound sizes the length prefix's bits | `serialize_string` with buffer N + 1 |
| `f bytes(N)` | identical shape: length in [0, N], align, then the used bytes | `serialize_int` + `serialize_bytes` |
| `f [N]T` | N elements, back to back | element per element |
| `f [<= N]T` / `f [Min..N]T` | count in [Min, N] encoded relative to Min, then that many elements | `serialize_int` + the element loop |

Arrays: the element may be any scalar or named type; arrays of arrays are not
in v1 (wrap the inner array in a type). Runtime-count arrays carry their own
count on the wire — there is no separately-declared count field in v1.

**Wire fidelity.** For every legal write, the bits are identical to the named
classic twins — serialize.modern's one documented deviation (`wstring_`
alignment) does not arise because schema emits sequential stream operations,
and wide strings are deferred anyway. On the *read* side, schema's generated
readers enforce the language's validation rules uniformly (e.g. the
interior-null rule of §4.7), which can be stricter than a hand-written
classic reader; acceptance is uniform across the targets, which is what the
conformance gates check.

**The count-range cap is lifted.** serialize.modern caps `array_n`'s count
range at 16 because each possible count is a separately spliced compile-time
path. schema's generated code uses an honest loop (§6.2), so `[<= N]T` is
bounded only by what the count's integer range can express.

### 4.4 Decisions: `if` — and `switch` is not in v1

Conditional serialization branches on a previously serialized field — a
back-reference. The branch itself costs no wire bits; the referenced field
was already paid for.

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

- `if cond { ... } else { ... }` — `cond` is a previously serialized `bool`
  field, optionally negated. Wire = the taken side only. A `bool` field
  followed by an `if` on it produces the identical wire to serialize.modern's
  fused `branch`.
- **`switch` / `case` are reserved but not in v1.** The design is preserved
  here for its return: `switch field { case ... }` over a previously
  serialized integer or enum field; wire = the matching case's fields, a
  value matching no case serializing nothing, identically both directions;
  case labels are bare variant names for an enum subject and constant integer
  expressions for an integer subject; duplicate case values a compile error;
  `bits(N)` a legal subject; `case None:` legal. One corner rule survives the
  deferral because it binds enums generally: **a user-declared variant
  literally named `None` is a compile error** — the name is claimed by the
  implicit-`None` rule.

### 4.5 Back-references: the dominance rule

The referenced field must be declared **earlier in the same block or in an
enclosing block** — never in a sibling branch side, or inside an array
element. Equivalently: the referent is serialized on every path that reaches
the reference. This is a lexical-scope check in the resolver.

```
    has_delta bool
    if has_delta { ... }      // legal: same block, earlier — dominated
```

Forward references, references into a sibling branch side, and references to
array element fields are compile errors naming the offending reference and
the rule. The rule is stated over `if` sides; it extends unchanged to `case`
bodies when `switch` lands.

### 4.6 Shape checks

All compile errors with positions:

- `bits`, `const`, `reserved` widths outside [1,64]; a `const` value that
  does not fit its width.
- **The fixed and 128-bit family rules** (serialize's static_asserts restated
  as language rules, so generated code cannot fail to compile):
  `fixed(I, F)` requires I ≥ 1 (the sign bit counts toward I), F ≥ 0, and
  I + F equal to a storage width — 8, 16, 32, 64 or 128; a `fixed` field
  requires `[min = A, max = B]` in whole units, A ≤ B, both fitting the Q
  format's whole-unit domain [−2^(I−1), 2^(I−1) − 1] AND int64 (where the
  runtime's compile-time bound parameters live). `ufixed(I, F)` carries the
  same shape rules with the unsigned domain — I ≥ 1 still, bounds in
  [0, 2^I − 1] clamped to int64's ceiling — and its diagnostics name the
  `ufixed` spelling. `int128` requires `[min = A, max = B]` (bare `int128` is
  a compile error — serialize has no raw signed 128-bit operation); `uint128`
  refuses `min`/`max` (ranged 128-bit is `int128`). Specified defaults cover
  `int128`/`uint128` (fit-checked like any integer) and `fixed`: a fixed
  default is declared in WHOLE UNITS (the same domain as the bounds, so no
  raw/units confusion is possible) and must be EXACTLY representable —
  value × 2^F an integer, no rounding rule involved — `1.0` and `0.5` are
  legal in Q2.30, `0.1` is a compile error naming the constraint. Storage
  initializes to the raw scaled integer.
- **A range that does not fit its declared storage:**
  `int8 [min = 0, max = 1000]` is a compile error — the range determines the
  wire, the type name determines the storage, and a legal wire value the
  storage truncates would be silent corruption that passes read validation.
- **Attribute discipline:** an unknown attribute key, a key repeated, an
  attribute on a type that does not take it, `min` without `max` (or vice
  versa), and `resolution` without both bounds are compile errors, each
  naming the field and the legal vocabulary for its type. One scoped
  exception: in the composite quantization form
  `[interpolate, quantize = K, max = B]` (§4.8 rule 2), `max` is the quantize
  bound and appears WITHOUT `min` by design — `min` is forbidden there, the
  domain being symmetric [-B, +B].
- **Non-finite compressed-float parameters are rejected:** each of
  `min`/`max`/`resolution` (§4.3's triple, and §4.8 rule 4's reuse of it)
  must be finite at float64 AND at float32, where every runtime evaluates the
  triple — the diagnostic names the parameter. The grammar cannot spell NaN
  at all — a float literal beyond the double's range is a parse error,
  constant arithmetic is overflow-guarded, division by zero is refused — and
  an integer constant too large for float64 is an error in ANY float position
  rather than a silent infinity; the checker still carries the NaN arm as
  defense in depth.
- **Degenerate ranges: min == max is legal and costs zero bits.** A ranged
  integer, `int128`, `fixed` or `ufixed` field with equal bounds carries
  nothing on the wire; the reader recovers the value from the range alone
  (`min` — for fixed point, the raw `min << F`). The generators emit the
  write-side range refusal and no wire call at all, because a bit packer
  requires at least one bit — which also means generated code needs no
  degenerate support from any runtime. What stays an error: an INVERTED range
  (min > max) anywhere; `[Min..N]T` with Min ≥ N (`[N]T` is the degenerate
  spelling); `string(N)` with N < 2; `bytes(N)` with N < 1; `[N]T` with
  N < 1; `[<= N]T` with N < 1; `resolution` ≤ 0. An empty `type` or `message`
  body is legal — zero wire bits, presence as the payload; an empty `object`
  body is an error (it would generate a meaningless view family); a unit with
  no `.schema` files is an error.

  **An enum with no variants is legal.** It holds only the implicit
  `None = 0`, so its wire range is the degenerate `[0, 0]` and it costs zero
  bits — the value is recovered from the range alone, under the same rule as
  any degenerate range.
- **One flat namespace, and the compiler's claimed names:** all declaration
  kinds share one unit-level namespace (`const Foo` and `type Foo` collide).
  A unit with `message` declarations may not declare `MessageType`;
  symmetrically, a unit with `object` declarations may not declare
  `ObjectType`, nor — per object `X` — the generated family names (`XState`,
  `XData_Deep`, `XData_Shallow`, `XData_Interpolate`); and no unit declares
  `ProtocolId`. The checker likewise refuses user names that collide with
  per-declaration generated symbols (`Write*`/`Read*`/`New*`,
  `*MaxBits`/`*MaxBytes`, companion length/count names, the dispatch
  surface). Diagnostics name the generated artifact that claims the name.
- Enum `[max = K]` below the variant count.
- **Duplicate field names anywhere in one type — including across branch
  sides.** One name, one field, declared once. (schema owns the type, and
  unique names keep the flattened generated output unambiguous.)
- Back-reference violations (§4.5); `if` conditions that are not `bool`
  fields.
- Cycles in type composition, undefined types, duplicate declarations,
  `package` mismatch.
- **Target-name safety:** a declaration or field name that is a reserved word
  in any target language (`type`, `match`, `impl`, `func`, `class`, ...) or
  that collides with another name after Go export-casing (`atRest` →
  `AtRest`) is rejected, with a diagnostic naming the target and the
  collision. No escaping machinery; rename at the source.
- **Package-name safety:** the package ident maps to every target's
  namespace/module/package concept verbatim (§6.1), which exposes it to three
  collision classes no declaration or field name can hit, each refused with a
  diagnostic naming the mechanism and the fix (rename the package). (1) A
  target reserved word: `package for` would generate `namespace for` and
  `package for`. (2) A C standard library identifier visible at C++ namespace
  scope — functions, types, and object-like macros of the C11 headers, which
  implementations also declare in the global namespace: `package exit` would
  generate `namespace exit`, which clang refuses against `<cstdlib>`. The
  refused set is a curated, per-header list in the checker; exact
  case-sensitive match (C++ namespaces are case-sensitive), so `exits`,
  `exit2` and `Exit` stay legal. (3) The name `main`, which makes the
  generated Go a program package that cannot be imported.

### 4.7 Strings and byte blocks — byte strings, one shape

**`string(N)` and `bytes(N)` are the same construct with two names**: N is
the max length; the wire is length in [0, N], align, then the used bytes;
storage is a pre-allocated fixed buffer plus a used count. The declared bound
exists for exactly one reason — it sizes the length prefix's bits.

**`string(N)` payloads are well-formed UTF-8 by contract.** The wire shape is
unchanged and identical to `bytes(N)`; what the `string` spelling adds is a
CONTRACT: the payload is well-formed UTF-8 — the writer's obligation, never
the reader's check. Writing malformed UTF-8 is a writer contract violation,
surfaced by debug-only asserts where the language supports them (C and C++
assert through a generated validator, Rust through `debug_assert!`; Go and C#
assert nothing). There is no mandatory read-path validation and no
release-path cost anywhere, and the conformance vectors carry only valid
UTF-8. An application with genuinely arbitrary payloads uses `bytes(N)`,
which remains exactly that.

Beneath the contract, `string(N)` carries **bytes excluding 0x00** — all
generated readers reject interior nulls; writes assert per §5 (NUL is valid
UTF-8, so the interior-null rule is its own, stricter check). `bytes(N)` is
the same wire with no interior-null rule and no encoding contract. The
byte-string tightening is what lets the targets agree:

- Classic C++ `serialize_string` is strlen-based — it cannot represent
  interior nulls; a writer in another language must not be able to produce a
  payload the C++ reader silently truncates.
- Rust `String` storage would impose UTF-8 validation ON READ that no target
  performs (the contract is writer-trusted — a reader must accept what a
  non-conforming writer produced rather than fail on text), so Rust storage
  stays a fixed byte buffer (§6.1).

For every legal write the wire bits are identical to `serialize_string` over
a buffer of N + 1.

**Generated-code consequence, stated so no backend discovers it late:** the
runtimes' `serialize_string` is the **wire oracle** for `string(N)`, not
necessarily the emitted call. The §6.1 storage rules make the runtime string
methods unusable in two targets — C# stores a byte buffer but
`SerializeString` takes `ref string`; Rust stores a byte buffer but
`serialize_string` takes `&mut String` and rejects non-UTF-8 bytes, which are
legal here — so the C# and Rust backends compose the wire from primitives:
length over [0, N], then align, then raw bytes. C++ and Go may use their
runtime string call for the framing. **The interior-null check is
generated-code validation in every target** — no runtime primitive performs
it (classic C++ read appends `'\0'` and would silently truncate).

### 4.8 Declaration sets — `message` and `object`, their types implicit

Each message is its own declaration; the discriminant enum, the wire tag and
the dispatch are the compiler's job, not the author's.

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

From the unit's `message` declarations the resolver extracts the message set
and the compiler generates, per target:

- **The `MessageType` enum, with `None = 0`, then each message SORTED BY
  NAME** — bytewise ascending over the declared names. Tags are a pure
  function of the declared name set — independent of file layout, declaration
  order, and any cut-and-paste reshuffling. Renumbering on growth is
  wire-safe by construction: adding a message edits the unit, the id moves,
  both sides regenerate together. `MessageType` is a claimed name: a unit
  with messages may not declare its own.
- **The wire framing**: the tag in minimal bits for `[0, count]` (the enum
  wire rule; read rejects tags above count), then the message body. **Tag 0 =
  None is a valid wire value meaning *no message*** — the null that gives
  message streams a natural terminator.
- **`WriteMessage` / `ReadMessage`** plus a dispatch surface in each
  language's idiom: a real `enum Message { None, Chat(Chat), Ping(Ping), ...
  }` in Rust, an interface + type switch in Go, a base type + pattern match
  in C#, a tagged union (or opt-in `std::variant`) in C++. **Representation
  is per-language and explicitly NOT part of the contract.** What binds every
  target is behavioral only: identical bytes, **reading `None` is a valid
  none-result, reading an out-of-range tag is a validation failure.**

  **The C++ representation is the caller's choice:** `schema generate
  --cpp-message union|variant`, and the **default is union** — a tagged
  struct over an anonymous union, constructed as the None message (the tag
  initializes to `None`; an arm's storage is established ZEROED when the arm
  is selected — by `ReadMessage` before it decodes, per §5's zero-baseline
  read rule, or by a writer assigning the arm), plain-switch dispatch,
  `GetMessageType` reads the tag field.

  **The opt-in variant surface** (`--cpp-message variant`):
  `using Message = std::variant<std::monostate, <messages in tag order>>` —
  the variant INDEX equals the wire tag, `std::monostate` is `None = 0`. No
  heap ever (`std::variant` stores inline at the largest alternative — the
  union's exact footprint), trivially copyable (asserted in the test),
  dispatch via `switch` on `index()` + `get_if` (no `std::visit` in generated
  code — it remains a caller's option for compile-time exhaustiveness). Both
  representations are compiled and run in the test chain so neither rots;
  both speak the identical wire (the tag + the same per-message functions).

  **The standing generation principle, wider than this choice: generated C++
  defaults to the C-flavored idiom; modern constructs enter only behind
  opt-in flags, and any proposal to use one arrives with a measured
  compile-time cost, never an asserted one.**
- Every message is also an ordinary type: its own
  `Write`/`Read`/`MaxBits`/`MaxBytes`, usable standalone and composable as a
  field (the grammar's `ident // a declared type or enum` includes message
  names). **An `object` name is NOT a field type in v1**: an object has no
  single wire form (Deep vs Shallow), so `ship Ship` inside a message is a
  compile error naming this rule; view-qualified references are a later pass
  if ever needed. **And an `object` body admits plain fields only in v1** —
  no `if`/`switch`/`const()`/`reserved`/`align` — because the view-splitting
  rules assign *fields* to views and say nothing about what a branch means
  for Shallow/Interpolate/Quantize; the restriction lifts when the delta pass
  defines it.

#### Object sets — `object`, `ObjectType`, and the generated views

`object` declarations are tracked exactly like messages: the resolver
extracts the object set and generates **`ObjectType` with `None = 0`** and
each object in the same deterministic sorted order. The 0-reserved principle
is uniform across every generated discriminant.

The view structs exist **in generated code only**; the schema holds one
definition per object. The markers:

- **`[interpolate]`** — visual state, sent to the client for interpolation.
- **deep** — the default: an unmarked field is full-state only, sent for
  client-side prediction.
- **`[local]`** — simulation-only state that reaches no wire: lives in
  `ShipState`, absent from every network struct.

What one definition generates per object, per target (shapes per language,
behavior identical — the message-set rule):

| artifact | contents |
|---|---|
| `ShipState` | every field — the simulation struct |
| `ShipData_Deep` + serialize | every non-`[local]` field, deep encodings |
| `ShipData_Shallow` + serialize | the `[interpolate]` fields, **quantized wire encodings** from the field's attributes |
| `ShipData_Interpolate` | the same `[interpolate]` fields in continuous storage (see rule 5 for the projected-field exception) |
| `Quantize` / `Unquantize` | the mapping pair between Interpolate and Shallow |

**View-encoding semantics.** Inside an `object` body the attribute vocabulary
widens: `interpolate` and `local` (valueless) and `quantize` / `round`
(valued) join the set. View-encoding attributes describe the SHALLOW wire
only; the deep wire always uses the bare storage type's encoding.

1. **`[interpolate]` alone** — the shallow wire is the deep encoding,
   unchanged.
2. **Composite quantization** — `[interpolate, quantize = K, max = B]` on a
   field whose type is a composite of float components applies
   component-wise: the shallow wire per component is a ranged int
   `[−B·K, +B·K]`; write maps `c → floor(c·K + 0.5)`, clamped to the range;
   read rejects out-of-range; unquantize maps `q → q / K`. The quantized
   twin's storage derives from the range. All components are sent — no
   smallest-three; the delta pass owns cleverer encodings.

   **Fixed components dissolve quantization.** When an object's
   `[interpolate]` fields are all already wire-domain — fixed-component
   composites (a `fixed(I, F)` component IS its own quantization: storage and
   wire share one integer domain, ranged by the component's declared bounds),
   plain integers, or int-composite types — the `QuantizeX`/`UnquantizeX`
   pair would be a pure member copy, and the backends do NOT emit it:
   Interpolate and Shallow are the same values.

2b. **Fixed-composite shallow narrowing.** `[interpolate, quantize = K]` on a
   composite whose components are ALL `fixed(I, F)` scalars narrows the
   shallow wire instead of dissolving: deep values keep full precision;
   shallow (non-simulating) values are quantized down to K, the kept
   resolution in quantized units per whole unit. K must be a positive power
   of two with log2(K) ≤ F (the shallow wire cannot be finer than the
   storage; K = 2^F keeps everything and the pair degenerates to copies). No
   `max` here: each component's shallow wire range is its own whole-unit
   `[min, max]` scaled by K — bounds every fixed component already declares
   (§4.3) — and its shallow storage is the smallest signed integer holding
   that range. The `Quantize`/`Unquantize` pair returns, as pure integer
   arithmetic with no floating point anywhere: quantize is round-to-nearest
   with **ties away from zero** over `drop = F − log2(K)` dropped bits —
   `( raw + half ) >> drop` for `raw ≥ 0`,
   `−( ( −raw + half ) >> drop )` for `raw < 0`, with
   `half = 1 << (drop−1)`, arithmetic on int64 (in-bounds raws cannot
   overflow the add or the negation: checker-enforced bounds leave 2^(F−1)
   of headroom past any legal raw) — and unquantize is the left shift back
   (dropped bits return as zeros). Components wider than 64 bits are a
   compile error (the arithmetic runs in int64), and so are `ufixed`
   components: a wide ufixed raw legitimately fills uint64's high half,
   where these int64 shifts are wrong — the unsigned door needs its own
   arithmetic before it opens, and the diagnostic says so. Un-narrowed
   `[interpolate]` composites of ufixed components dissolve fine, because
   dissolving just delegates to the component encodings. The deep wire is
   untouched: full precision, the component's own ranged fixed encoding. The
   two forms compose field-by-field in one object.

   **The one rounding rule: fixed point rounds half AWAY FROM ZERO,
   everywhere it rounds.** This rule, rule 4's `nearest` default, and the
   data compiler's rational rounding are the SAME rule, stated once, here.
   The per-language hazard, named: the naive arithmetic shift FLOORS, so on
   negative ties every target language would agree with every other and all
   of them would be wrong against this rule — cross-language identity cannot
   catch it; only the pinned negative-tie conformance vector can (§7.2).
   Implement the sign mirror, not the bare shift.

3. **No special composite cases in v1 — tags are inert (§4.2, Type tags).**
   Rules 2 and 2b are the whole of composite quantization: every composite
   quantizes per-component — a float rotation field states its bound like
   anything else (`rotation Quat [interpolate, quantize = RotationUnits,
   max = 1]`; unit quaternions are always unit length, so the bound is 1,
   written rather than implied). Rotation-specific actions (renormalize on
   unquantize, shortest-arc interpolation) belong to the future pass that
   claims `[quat4]` — v1 generates the structural mapping only, and the
   application keeps its hand-written rotation actions meanwhile.
4. **Ranged-int projection** — on `float32`/`float64`,
   `[interpolate, min = A, max = B, resolution = R]` reuses §4.3's
   compressed-float triple: min/max name the **CONTINUOUS domain**, the
   shallow wire is the int `[0, (B−A)/R]`, write maps `v → round((v−A)/R)`
   clamped, unproject maps `q → A + q·R`. The optional
   `round = nearest|up|down` key (default `nearest`, half away from zero —
   the one fixed-point rounding rule above) names the write rounding.
   **`round` is ADVISORY METADATA in v1:** under rule 5 a projected field is
   STORED in the wire-integer domain in both the Shallow and Interpolate
   views, so the float-to-step conversion is the application's, not generated
   code's — no generated byte depends on `round`, and nothing in v1 enforces
   it. Every backend carries the declared rounding into the generated storage
   comment for the application to honour (`up` exists because a
   health-style quantization's ceil is load-bearing: any positive health must
   quantize alive). If a generated projection helper ever lands, `round` is
   its law and this rule tightens from advisory to normative.
5. **Interpolate storage** — projected fields (rule 4) are stored in the
   Interpolate struct in the WIRE integer domain and snap-interpolate;
   quantized composites (rules 2–3) are stored continuous (the un-quantized
   twin).

**No interpolation generation in v1.** v1 generates the STRUCTS (State, Deep,
Shallow, Interpolate) and the `Quantize`/`Unquantize` mapping pair. The
`Interpolate(t, a, b, out)` FUNCTION stays hand-written until a later pass
claims type tags and assigns actions (§4.2, Type tags); `lerp`, `slerp`,
`snap`, `angle`, `smooth` stay informally reserved as attribute values for
it.

The worked example is
[`examples/Objects.schema`](examples/Objects.schema); Missile, DynamicProp
and Turret are written beside it — all four objects are in the corpus.

**Generated layout is the generator's.** Field order inside every generated
view derives from need (wire order for wire structs, contiguous spans where
machinery wants them), never a convention a human maintains.

#### Unions — `union`, first-class one-of fields

A `union` declares a type that holds **at most one** of a named set of
payloads. It is the message set's tagged-union machinery promoted to a
declarable type, replacing the bool-guard idiom (`has_box bool` / `if
has_box { box BoxCollider }` repeated per shape), which spends one bit per
absent arm and makes illegal states representable — zero payloads, or
several at once.

```
union ColliderShape {
    box     BoxCollider
    sphere  SphereCollider
    capsule CapsuleCollider
    hull    HullCollider
}
```

- **Grammar.** `union` is contextual like `flags`: `union ident { ... }`
  declares a union; `union` remains usable as an ordinary name everywhere
  else. Each body row is `ident ident NL` — a variant name (field-style
  lower_snake, unique within the union), then its payload type. A variant
  row takes no attributes, no default, no bound — a row names a thing, it
  does not describe a wire refinement. A union field likewise takes no
  attributes and no `= default` (it zero-initializes to None, joining
  arrays, strings, bytes and composites in §4.2's no-override list).
- **Payloads are declared types.** A variant's payload must name a declared
  `type` (or `table` — the declaration kind does not matter here, the type
  does). An enum, flags, object, message or union name is not a payload in
  v1 — wrap it in a type; scalar and array payloads likewise (the parser
  names this rule when a scalar keyword or `[` appears in payload
  position). Follow-ons if a real case wants them. Composition cycles
  through unions are compile errors exactly like type cycles (a payload
  that contains its own union has infinite size). A union with **zero
  variants is legal**, mirroring the empty enum (§4.6): it holds only None,
  its tag range is the degenerate [0, 0], and it costs zero bits.
- **The implicit None row.** Entry 0 of every union is **None — no
  payload** — mirroring the enum sentinel-zero convention and the message
  stream's `None` terminator: optionality rides in-band, a zero-initialized
  union field IS the empty union by construction (zero-value lists in §4.2
  and §5 include "None for a union"), and no separate has-flag exists to
  disagree with the tag. **Reserved variant names are checked over the
  EXPORTED spelling**: any variant whose exported form (the field-name
  mapping) is `None` or `Max` is refused — `none` and `max` included, not
  just the literal spellings.
- **The tag enum is generated, named `<Union>Type`** — the same shape as
  `MessageType`: `None = 0`, then each variant **in declared order**
  (exported spelling per target, the field-name mapping), dense from 1, plus
  the exported `Max` extent; storage per the enum storage rule, the
  smallest unsigned integer fitting max. Declared order, not sorted: a
  union is one declaration whose author states the order, exactly as an
  enum's variants do — and reordering variants is a wire change (see id,
  below), so the spelling of the source is the truth of the wire.
  `<Union>Type` and its member constants are claimed names, and the claimed
  set covers generated-vs-generated collisions too: in a unit with
  messages, a union named `Message` is refused (its `MessageType`,
  `WriteMessage`, `ReadMessage` collide with the dispatch surface), and
  likewise `Object` against the object tag surface. **Generated sets are
  usable in constant expressions and nowhere else**: `<Union>Type.Max`,
  `MessageType.Max` and `ObjectType.Max` all work in integer expressions
  (§4.2); no generated set is a declarable field type. Variant names pass
  the same target-name safety and post-export uniqueness rules as field
  names (§4.6): a variant named a target's reserved word is refused, and
  two variants whose exported spellings collide (`box_a`/`boxA`) are
  refused.
- **The wire.** The tag encodes in **minimal bits for `[0, variant
  count]`** (the enum wire rule), then **the selected variant's payload
  only**. Tag 0 = None costs the tag bits and nothing else. The read path
  **rejects a tag above the count** — refusal, never clamping, the ranged-
  integer rule. MaxBits = tag bits + the largest payload's MaxBits.
  `Write<Union>` follows `WriteMessage`'s rule (§6.1): the tag is validated
  BEFORE it rides — an out-of-set tag value in storage writes nothing and
  fails, it never desyncs the stream.
- **Read semantics are §5's.** The selected arm is **zero-established at
  selection** before its payload decodes — the message union's rule exactly.
  Arms not selected by a read are unspecified: in the C/C++ union
  representation their bytes are indeterminate; in targets whose storage
  lays every arm out separately (Go, C#, JS) an unselected arm keeps
  whatever it last held, the `MessageStorage` reuse discipline (§5's
  stale-tail carve-out extends to unselected union arms; whole-object
  comparison in the conformance matrix is over a fresh output or the
  selected arm). Consumers read the selected arm only. Nothing branches on
  a union's tag in v1 — `if` takes bools only (§4.4) — and a `switch` over
  `<Union>Type` is ruled-not-now, banked with §4.4's switch design.
- **A union field.** A union name is a field type inside `type` and
  `message` bodies, arrays included (`shapes [<= 4]ColliderShape`). Not in
  v1, both stated over the COMPOSITION CLOSURE so nesting cannot smuggle
  one through: an `object` body may not reach a union through any field's
  type, transitively (the view-splitting rules say nothing about what
  Shallow/Interpolate mean for a one-of), and no **table-closure member**
  may declare a union-typed field, transitively through arrays and
  branches (the table wire's evolution semantics — elision, unknown-field
  skip — are undefined over a one-of). A `table` used as a union PAYLOAD
  stays legal — the table is a closure root either way; it is the union
  that may not sit on a closure path. Both are compile errors naming this
  rule, and both are follow-on passes, not rulings against.
- **The id moves.** A union is wire structure: its variant order, count and
  payload type references all shape bytes, so they project into the
  protocol id (§3.1) — declaring a union, adding, removing or reordering
  variants, or changing a payload type moves the unit's id. Renaming a
  VARIANT does **not** move it: the ordinal is the wire, the enum-variant
  rule exactly (§3.1) — renaming `box` to `crate` leaves every byte
  identical.
- **Pack/JSON authoring** (the data compiler's dense-wire instance
  encoding, `internal/pack` — the wire oracle that packs the pinned
  conformance instances; a table-closure manifest cannot carry a union
  until the closure lift above, so this rule reaches the manifest surface
  then): a union value is **a single-key object, the key naming the
  variant in its source spelling** — `{ "sphere": { "radius": 2.5 } }`,
  and the key's value must be a JSON object. JSON `null`, or leaving the
  field absent, is None; `null` UNDER a variant key is a refusal, as is an
  object with zero keys or more than one — a one-of holds one thing, and
  the encoder does not guess which.
- **Generated code, per target** — the message-set rule verbatim:
  representation is per-language and explicitly NOT part of the contract;
  what binds every target is behavioral only — identical bytes, None is a
  valid empty read, an out-of-range tag is a validation failure. C++ reuses
  the message union shape: a struct holding the `<Union>Type type;` tag
  over an anonymous union of the arms (member names = variant names),
  constructed as None, trivially copyable (asserted); a variant named
  `type` is refused at check time, the message-member rule. C mirrors it
  with its named `as` union. Go, C# and JS lay the tag beside one
  pre-allocated arm per variant (the `MessageStorage` stand-in — nothing
  heap-allocates per value). Rust holds the value as a real
  `enum <Union> { None, Box(BoxCollider), ... }`, `None` the default — and
  STILL emits the `<Union>Type` tag newtype beside it, exactly as
  `MessageType` exists beside `enum Message`: the tag surface (constants,
  `Max`) is uniform across targets whatever the value representation.
  Every union also gets `Write<Union>`/`Read<Union>` wire functions and
  `<Union>MaxBits`/`<Union>MaxBytes`, composable exactly like a type's.

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

No `MessageType` enum is declared and no dispatch is written — the compiler
extracts the message set and generates both (§4.8):
`MessageType { None = 0, Chat, Ping, Pong, Snapshot }` and the per-language
`WriteMessage`/`ReadMessage`. (Ping and Pong may both name a field `sequence`
because they are separate types; §4.6's unique-names rule is per type.)

### 4.10 Deferred constructs

- **C++-style bitfields (`uint64 blah : 8`) — declined across the targets**,
  for three reasons. (1) The wire half already exists: `bits(N)` is exactly
  the bitfield's wire — N bits, named, packed back to back — with honest
  per-field storage. (2) As storage, only C++ has the syntax at all, its
  layout is implementation-defined (order and padding vary by compiler — the
  reason serialize itself never uses bitfields), and the other targets would
  need generated shift/mask accessors over a backing word — unnatural in all
  of them. (3) The decisive reason is addressability: generated serialize
  methods take `ref`/`&mut`/pointers, and bitfield members cannot be
  addressed. If memory packing of hot object arrays ever demands it, the door
  is an opt-in `[packed]` attribute on a type generating accessor-based
  storage — a generator-kind decision for a later pass, not a v1 wire
  construct.
- **Wide strings** (`serialize_wstring`): deferred — no near-term need — but
  the WIRE is decided so the deferral cannot foreclose it: length, then one
  unaligned 32-bit group per **UTF-16 CODE UNIT** — not per code point — and
  the payload is **well-formed UTF-16 by contract**, writer-trusted per §5's
  doctrine. Surrogate PAIRS are valid (full Unicode: an astral character is
  two groups); an UNPAIRED surrogate is a writer contract violation,
  debug-asserted where the language supports it. 2-byte and 4-byte `wchar_t`
  platforms must produce IDENTICAL bytes: the 4-byte platform converts at the
  boundary — splits astral code points into surrogate pairs on write,
  recombines on read. Basic-plane text is unaffected on every platform.
- **Relative integers** (`serialize_int_relative`): deferred. The classic use
  is a strictly-increasing sequence across *array elements* (the previous
  element's field feeding the next), which the back-reference rule cannot
  express; a scalar-to-scalar form inside one type earns too little to carry
  the construct. It is designed with the delta pass, never standalone. **The
  semantics are pinned ahead of it:** strictly increasing, positive only —
  `current > previous`, the reader fails otherwise; no wrap semantics exist,
  and a caller with a wrapping counter unwraps before serializing.

### 4.11 Tables — the second wire, and reflection

`table X { ... }` declares a type that is also a **table-wire root**. The
body grammar is identical to `type` — every field kind, guard, default, and
bound works the same, and a table is usable anywhere a type is (message
fields, arrays, other tables). What the keyword changes is what the compiler
EMITS for it.

**The closure.** The table closure is every `table` declaration plus every
struct one references through fields, transitively. Closure members get, in
every generated language except JavaScript (whose table wire does not exist
yet):

1. **Table-wire codecs** (`TableWriteX` / `TableReadX`; `table_write_x` /
   `table_read_x` in Rust's own naming — the bytes are identical) — the
   evolution-tolerant TLV encoding specified in notes/table-wire.md:
   name-hash field ids, defaults elide, unknown fields skip, changed kinds
   skip, out-of-range clamps, all counted in a `TableReport`. This wire is
   for data that outlives builds (config, assets, settings, events); the
   realtime message wire (§4) is untouched and exact-match as ever.
2. **Reflection descriptors** (`TableTypeX()`) — static per-type field
   metadata: name, wire id and kind, array bounds and count companions,
   nested descriptor links, declared ranges, enum wire max and value→name
   functions, and branch guards. C++ additionally carries storage offsets
   (zero-cost direct access); Go and C# carry generated get/set-by-name
   accessors instead, with set clamping to declared ranges exactly as the
   wire read does. No RTTI, no `System.Reflection`, no schema files at
   runtime — editors and tooling bind against the descriptors alone.

   **The descriptors are data-oriented, and emitters must keep this shape:**
   SEPARATE STATIC DATA, one set per type, shared by every instance — an
   instance is exactly its fields, carries zero reflection weight, and stays
   trivially copyable. A type's field descriptors are one flat contiguous
   array walked linearly; instance access is base + offset arithmetic (C++)
   or a generated direct accessor (Go/C#); nested types cost one hop to
   another flat array. No per-instance metadata, no fat objects, no
   pointer-chased descriptor graphs beyond the nested-type link.

Plain `type` declarations outside the closure get NONE of this — the realtime
types pay nothing for the table layer's existence.

**Capability rule.** A closure member must stay on table-wire kinds:
`int128`/`uint128`, `fixed(I, F)`/`ufixed(I, F)` and `bits` wider than 64
have no table-wire kind, and the checker refuses them inside the closure at
compile time. Pack roots (`schema pack` manifest collection types) must be
declared `table`, and `EncodeTable`/`DecodeTable` require closure
membership — reflection and table codecs follow the declaration, never
accident.

**Protocol id.** The `table` keyword participates in the wire shape
projection (§3.1), so marking a table moves the unit's protocol id like any
other declaration change. The table wire's own compatibility does not depend
on it: a table bin from a different protocol id still reads under the
permissive contract, which is the point.

## 5. Trust model — inherited

**Reads validate everything** — integer ranges, enum bounds, alignment
padding, constants, reserved bits, count bounds, string lengths, the
interior-null rule, and buffer exhaustion (running out of input mid-read is a
read failure like any other) — and fail on any violation, because network
input is the trust boundary. Generated read code never lets a value that
controls iteration go unchecked before use.

**Read termination:** `Read` consumes exactly the encoded bits and reports
bits consumed, so callers can frame several objects in one buffer; bytes
remaining after the last field are the caller's concern, not a validation
failure — `ReadMessage` streams end on the `None` tag by design.

**Writes assume trusted data.** The write path is trusted; writing correctly
is the caller's responsibility. Writer inputs are stated as OBLIGATIONS, not
defined behaviors: the spec owes a conforming writer exact bytes and owes a
misbehaving writer nothing. The read side is untouched by this doctrine —
readers face untrusted data and keep every mandated check above.

Within that doctrine, misuse surfaces by each runtime's own convention: C and
C++ debug-assert (unchecked in release); Go and Rust panic and C# throws on
misuse in all build modes. The generated write code's job is to make misuse
impossible by construction — bounds come from the schema. Costlier contracts
assert in DEBUG ONLY everywhere (§4.7's UTF-8 well-formedness contract is the
type case: an O(n) check no release path should carry). Ranges are trusted
inputs everywhere: generated code never feeds attacker-influenced values as
min/max.

**Read failure leaves the output object in an unspecified state**; callers
use it only on success. **Read success fully initializes it**: fields in
untaken branches are set to their zero values — 0, 0.0, false, empty bytes,
zero count, `None` for an enum, `None` for a union, recursively zeroed for a
nested type, element-wise zeroed for a fixed array. **Zero values, not
specified defaults** — the defaults of §4.2 are storage initialization at
construction; the wire contract stays a pure function of the encodings.
*Fully initializes* is relative to a zero-initialized output object:
elements past a used count and bytes past a used length are not rewritten by
a successful read, and a union's UNSELECTED arms are not rewritten either
(the selected arm is zero-established, §4.8), so a REUSED output object
keeps stale tail data there (the classic runtime's own prefix convention). Whole-object comparison in the conformance matrix is
defined over a fresh output or the used prefix. Write reads only taken
fields.

## 6. Generated code

### 6.1 What is generated

Per `type`, per target:

1. **The type itself.** Storage, complete — the integer family names its
   storage directly (the type name fixes the storage; the optional range
   refines the wire); everything else derives by fixed rule:

   | schema | C++ | C# | Go | Rust |
   |---|---|---|---|---|
   | `int8/16/32/64` (bare or ranged) | `int8_t/16/32/64_t` | `sbyte/short/int/long` | `int8/16/32/64` | `i8/16/32/64` |
   | `uint8/16/32/64` (bare or ranged) | `uint8_t/16/32/64_t` | `byte/ushort/uint/ulong` | `uint8/16/32/64` | `u8/16/32/64` |
   | `int128` (ranged) / `uint128` (raw) | `serialize::int128_t` / `serialize::uint128_t` (native `__int128` or the emulated pair) | `Int128Value` / `UInt128Value` (the emulated pair, every TFM) | `serialize.Int128` / `serialize.Uint128` (the two-qword pair) | `i128` / `u128` (native) |
   | `fixed(I, F)` (signed, ranged) | signed integer of I+F bits holding the RAW scaled value, in every target: `int8_t`..`int64_t`, `serialize::int128_t` at 128 | `sbyte`..`long`, `Int128Value` at 128 | `int8`..`int64`, `serialize.Int128` at 128 | `i8`..`i64`, `i128` at 128 |
   | `ufixed(I, F)` (unsigned, ranged) | unsigned integer of I+F bits holding the RAW scaled value: `uint8_t`..`uint64_t`, `serialize::uint128_t` at 128 | `byte`..`ulong`, `UInt128Value` at 128 | `uint8`..`uint64`, `serialize.Uint128` at 128 | `u8`..`u64`, `u128` at 128 |
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

   The C target mirrors the C++ storage rules in C's own types; JavaScript
   storage is `Number` for wire widths of 32 bits or fewer and `BigInt` for
   64 and 128 (the serialize.js value-domain seam).

   **The storage principle behind every row: nothing is dynamically sized.**
   Generated storage never heap-allocates per message in any target: every
   buffer is fixed at its declared capacity with a used length/count beside
   it — no Go slices as storage, no Rust `Vec`, no growing containers.

   Enums are integer-backed named types in every target because `[max = ...]`
   headroom makes non-variant values wire-legal; a native Rust `enum` cannot
   hold them — C#'s `enum E : uint` can, natively, which is why it needs no
   newtype.

2. **`Write(buffer, object) -> bytesWritten`** — straight-line write code in
   wire order.
3. **`Read(buffer, object) -> ok/error`** — straight-line read code with full
   validation, in each runtime's native error idiom (`bool` in C and C++,
   `bool` + latched `Error` in C#, `error` in Go, `Result` in Rust, `bool` +
   the stream's latched `error` in JS — generated validation refusals return
   `false` latching nothing, so callers tell the two channels apart exactly
   as in C#). The consumed size (§5) surfaces per target idiom — a success
   value that carries bits consumed where the idiom allows, an out-parameter
   where it does not.
4. **`MaxBits` / `MaxBytes`** — constants: the longest path through the
   schema, with worst-case (7-bit) padding assumed at each alignment point.
   Size write buffers from `MaxBytes`; conservative is correct for a buffer
   bound. **`MaxBytes` is rounded up to the 8-byte write-buffer granularity
   every serialize runtime requires.**
5. **`ProtocolId`** — one constant per unit (§3).
6. **For a unit with `message` declarations (§4.8):** the `MessageType` enum
   (`None = 0`, then the messages sorted by name),
   `WriteMessage`/`ReadMessage` with the tag framing, a message-level
   `MaxBytes` (tag plus the largest message), and the dispatch surface in
   each language's own idiom — representation per target, behavior identical.
   The Go dispatch: a `Message` interface the message structs satisfy, `nil`
   as the None terminator, a type switch — and reads land in a
   caller-supplied `MessageStorage` struct holding one field per message, the
   Go stand-in for the C++ tagged union, so the receive path never
   heap-allocates per message. In every target, `WriteMessage` validates the
   message BEFORE the tag rides the wire — an out-of-set value writes
   nothing, because a tag with no payload would desynchronize the stream —
   and both dispatch functions frame the tag through the
   `WriteMessageType`/`ReadMessageType` pair (one source).
7. **For a unit with `object` declarations (§4.8):** the `ObjectType` enum
   (`None = 0`, same deterministic order) and, per object, the generated view
   family — the full simulation struct, the Deep and Shallow wire structs
   with their split read/write pairs, the Interpolate struct, and the
   `Quantize`/`Unquantize` mapping pair (§4.8's artifact table).

**Generated symbol naming:** functions attach per target idiom — C++: free
functions `WriteShip(...)` in `namespace <package>`; C#: `static class
Schema` members in `namespace <Package>`; Go: free functions
`WriteShip(stream, &ship)` in package `<package>` (no overloading — the type
name is in the function name in every target, for uniformity); Rust: module
functions in `mod <package>`; JS: exported free functions
`WriteShip(stream, value)`, one ES module per schema file. The `package`
ident maps to the target's namespace/module/package concept verbatim. JS
storage members are PascalCase via the same mapping as the Go target — not
camelCase — so the checker's collision registry covers JS with zero new
rules; the generated function names are the family's own
`WriteX`/`ReadX`/`ZeroX`/`QuantizeX` (the runtime's methods stay camelCase —
the seam is the stream parameter).

**There is no generated measure function.** `Write` returns the actual size,
`MaxBytes` sizes buffers, and that covers the real uses. Anyone who genuinely
needs per-object sizing can hand-write stream code beside the generated
code — the runtimes are unchanged and the two mix freely on the same wire. If
a real need appears, an opt-in exact measure (a generated running bit count)
is a clean later addition.

The generated API mirrors serialize.modern's `schema<...>` members — `Write`,
`Read`, `MaxBits`, `MaxBytes` — so the two feel like one family.

**Canonical encoding is a CONTRACT:** for a given schema and target, equal
post-quantization values produce identical bytes — deterministically,
forever, across compiler versions; the golden-wire gate is what pins it
(§3.1, §7.2). Consumers may byte-compare encoded forms as a dirty/equality
check; a canonicalization slip under that use is a correctness bug, not a
size regression.

**Alignment is stream-relative**: a type containing `align`, `string` or
`bytes` has layout dependent on its entry bit offset. Generated functions are
correct at any entry offset; the same type works standalone and nested.
`MaxBits` covers the worst case.

**Output layout.** Each target emits one generated file per schema file —
`examples/Constants.schema` → `generated/cpp/Constants.h` — so the generated
tree mirrors the schema tree a person navigates.

- **C++: the header splits into a data/wire pair.** `<Base>.h` is the DATA
  header (constants, enums, flags, structs, object view families,
  MaxBits/MaxBytes bounds, the Message storage surface and `GetMessageType`)
  and includes `serialize.h` only when a storage type demands it
  (int128/fixed); `<Base>Wire.h` is the WIRE header (`Write*`/`Read*`,
  `Quantize`/`Unquantize`, the tag pairs and `WriteMessage`/`ReadMessage`)
  and includes `<Base>.h` plus `serialize.h`, with cross-file wire deps
  riding the deps' wire headers. The split exists so a consumer can base its
  math types on generated structs without inheriting the serialize runtime at
  all (a codebase may vendor an older serialize generation whose macro names
  collide): data consumers include `<Base>.h`; wire consumers include
  `<Base>Wire.h`. A unit may not contain files named both `X` and `XWire` —
  the emitter refuses the collision. Everything sits in
  `namespace <package>`, under `#pragma once`, with cross-file `#include`s
  derived from actual references.
- **C:** the same data/wire header pair per schema file (`<Base>.h` /
  `<Base>Wire.h`), mirroring the C++ split in C's own types.
- **Go:** one `.go` file per schema file, all in `package <package>` — Go
  packages are order-free across files, so there is no topo sort and no
  include graph to refuse.
- **Rust:** one module per schema file (lowercased basename) plus a generated
  `lib.rs` declaring and glob re-exporting them.
- **C#:** one `.cs` file per schema file, types at namespace level and every
  function and constant on `public static partial class Schema`, in
  `namespace <Package>`.
- **JavaScript:** one ES module per schema file, cross-file `import`s derived
  from actual references; classes whose constructors initialize every member
  in declaration order (specified defaults live in construction; `ZeroX` is
  the §5 zero form). **Generated JS never imports the serialize runtime** —
  every wire call is a method on the stream parameter, so no wiring file
  exists and the checked/production fork stays where it lives, in the
  runtime's own load-time mode selection (generated code never reads
  `NODE_ENV`).

The unit-level dispatch surface (the tag enums, tag pairs, dispatch value and
functions) is emitted exactly ONCE per unit in every target, in the
topologically last carrying file, so declarations spread across files never
redeclare it. Each generated file is headed by the source file's BASENAME
(never an invocation-relative path — the same input must produce the same
bytes wherever the compiler runs), the protocol id, and a do-not-edit line.
Output is **deterministic to the byte** for identical input; no external
formatter runs in the build or test path (goldens pin the emitters' own
output — formatter version drift must not be able to break a golden). The
emitters are written to produce formatter-clean code instead — except the Go
emitter, which routes its output through the stdlib `go/format` in-process
(not an external tool; versioned with the compiler's own toolchain):
gofmt-clean by construction, and a free refuser — generated code that does
not parse fails generation loudly.

### 6.2 Code shape — a stated divergence from serialize.modern

serialize.modern's zero-overhead contract is enforced by disassembly: no
calls, no loops, one spliced constant path per branch outcome and per
possible array count. That is the right contract when fighting the template
abstraction penalty from inside the consumer's compiler. schema emits
**source**, and its contract is different on purpose:

> **Generated code is the code a careful expert would hand-write against the
> runtime's serialize API: separate read and write functions, sequential
> field operations, honest loops for arrays, honest branches for `if`.**
> Register allocation, unrolling and constant-folding are the target
> compiler's job — it sees straight-line calls into an inline-friendly
> library with every width and range a literal constant, which is precisely
> the input optimizers are built for.

Read and write are generated as separate, tailored functions per type and per
view — no unified `Serialize` template. A unified function was always C++'s
trick for keeping read and write from drifting apart; the schema is that
single source of truth now, so the trick retires from generated code. The
runtimes' own unified paths are untouched — they serve people hand-writing
serialization beside the generated code.

What this buys: generated source stays small and reviewable, count ranges
need no cap, and the backends stay simple enough to verify against each
other. What it forgoes: the last-mile instruction-level guarantees of the TMP
splicer. The v1 performance thesis is that eliminating unified-function
branching and runtime range computation captures most of the win; if
measurement against serialize.modern's schema mode on the C++ target shows a
gap worth closing, a bitpacker-level emission mode for fixed-layout structs
is the planned v2 lever, behind the same byte-identity tests.

### 6.3 Per-target notes

| | C++ | C# | Go | Rust |
|---|---|---|---|---|
| emits against | `WriteStream`/`ReadStream` methods (or `serialize_*`-equivalent calls) | sealed `WriteStream`/`ReadStream` (`ref` params, `bool` returns + sticky `Error`) | `WriteStream`/`ReadStream` concrete types (no interface dispatch) | `WriteStream`/`ReadStream` via the `Stream` trait, monomorphized |
| error idiom | `return false` early-out | `bool` early-out; counts checked before loops; latched `Error` for callers | sticky stream errors; counts checked before loops; `return stream.Err()` | `?` propagation of `serialize::Error` |
| buffer contract | write buffers multiple of 8 (asserted); read allocations extend ≥8 bytes past packet data (required) | write buffers multiple of 8 (throws); reader takes (buffer, bytes), no slack required | write buffers multiple of 8; ≥7 bytes read slack for the fast path | write buffers multiple of 8; ≥8 bytes read slack for the fast path |

This table predates the C and JavaScript targets: C's contract is
serialize.c's own (it adopts C++'s align-up rules — write buffers a multiple
of 8, read allocations extending ≥8 bytes past packet data), and
JavaScript's is serialize.js's.

## 7. The compiler

Go, zero third-party dependencies, one static binary: `schema`.

```
schema check      [--verbose] [dir|files...]   // parse + typecheck; exit code for CI
schema generate   [--lang c|cpp|cs|go|js|rust] [--cpp-message union|variant]
                  [--out <dir>] [--verbose] [dir|files...]
schema id         [dir|files...]          // print the protocol id
schema projection [dir|files...]          // print the wire shape projection (§3.1)
schema fmt        [--verbose] [dir|files...]   // the canonical formatter, standalone (editors, hooks)
schema pack       [--verbose] <manifest.json>  // the data compiler: JSON instance files -> a
                                          // versioned, hashed .bin per the manifest's
                                          // ordered collections (§4.11)
schema version
```

Success is silent. Commands whose printed output is their answer (`id`,
`projection`, `version`) print it; everything else prints nothing unless
`--verbose` asks for the per-file report — the files `generate` and `pack`
wrote, the files the formatter rewrote, `check`'s ok line. Errors and
diagnostics always reach stderr, and exit codes do not depend on verbosity.

**Every command formats the unit's schema files in place before processing
them.** One style, no options, no separate binary; a file already in format
is never touched. The formatter carries two built-in refusers: it re-parses
its own output and structurally compares the AST against the input's,
refusing to write on any difference — a formatter must never change
meaning — and it verifies its own idempotence on every run.

### 7.1 Pipeline

```
*.schema → scanner → parser → AST → resolver/checker → IR → {c, cpp, csharp, golang, js, rust} backends
```

- **Scanner/parser: hand-written, recursive descent**, in the style of the Go
  toolchain's own `go/scanner`/`go/parser`. No parser generators. The
  language is LL(2); hand-rolled parsing is what makes precise diagnostics
  cheap. The parser recovers at declaration boundaries so one error does not
  hide the rest.
- **Resolver/checker**: name resolution across the unit's files, constant
  folding (arbitrary-precision with per-use fit checks, §4.2), the shape
  checks of §4.6, the dominance rule of §4.5, and the message-set and
  object-set extractions — the symbol tables over `message` and `object`
  declarations from which `MessageType`/`ObjectType`, the dispatch, and the
  per-object view families are generated (§4.8).
- **IR — the load-bearing design decision.** The checker lowers to a small,
  fully-resolved intermediate representation, and backends consume only the
  IR — a per-target divergence must be written into a printer to exist at
  all. **The IR preservation invariant: the IR keeps every parameter that
  affects (a) the bits written, (b) the value decoded from given bits, or (c)
  the set of inputs a read rejects.** Concretely: ranges stay exact
  `(min, max)` pairs with derived widths (width alone loses the reject set —
  a [0,5] and a [0,7] range share a width and differ in what they reject);
  compressed floats keep `(min, max, resolution)` with derived step count
  (min and resolution determine decoded values); enums keep
  `(variant count, max)`; strings, bytes and arrays keep exact bounds;
  `const` keeps `(value, bits)`; back-references resolve to field indices;
  branches and counts are explicit; names of structs, fields and variants are
  carried — the backends print them. The IR's stability obligation is what
  the golden-wire gate (§7.2) holds still across compiler versions.
- **Backends: dumb printers.** Hand-written emitters (a small indent-aware
  writer helper), not `text/template` — codegen wants precision. Each backend
  is a single file a reviewer can hold.

### 7.2 Testing

The conformance program is seven gates. `make test` runs all of it;
`make update-goldens` re-pins deliberately.

1. **Golden source tests**: schema in, generated source compared
   byte-for-byte against checked-in goldens, every backend. Deterministic
   output makes this exact.
2. **Golden ids**: `schema id` over the conformance corpus pinned as exact
   values — the tripwire on §3.1's hash: any change to how the id is computed
   breaks every pinned value loudly.
3. **The wire oracle**: for each conformance schema, a hand-written
   classic-serialize stream twin in C++. Generated C++ must produce
   byte-identical output to the twin and each must decode the other — the
   same gate serialize.modern runs, against the same oracle.
4. **The cross-language matrix — the whole point**: every writer × every
   reader, property-driven random instances, identical bytes, identical
   decoded values under §5's whole-object rule. This needs per-target
   generated test scaffolding — instance generators respecting every
   range/branch/count, and a canonical dump format for cross-language value
   comparison — budgeted as part of the conformance harness, not
   hand-written per schema.
5. **Malformed-input agreement**: bit-flip sweeps over valid packets; all
   generated readers must agree on accept/reject — and, for accepted inputs,
   on the decoded value (§5 leaves rejected outputs unspecified).
6. **Compiler robustness**: Go-native fuzzing on scanner, parser and checker;
   every diagnostic in the suite asserted by exact message and position. The
   diagnostics suite covers generated-name collisions: companion length/count
   names, the dispatch surface, and the per-declaration generated symbols are
   claimed names the checker refuses (§4.6).
7. **Golden wire bytes**: per conformance schema, fixed instances with
   checked-in expected wire bytes, every target. This is the one artifact
   that survives a coordinated emitter-plus-oracle change, and a golden-wire
   break is the stop-the-line event §3.2 describes — never a quiet re-pin.

**Floating-point discipline.** Strict IEEE-754 arithmetic is the normative
wire for compressed floats: a fused multiply-add diverges by one quantization
step at boundary values, so all C and C++ conformance builds compile with
`-ffp-contract=off`, and the golden-wire corpus pins compressed-float values
chosen to DISCRIMINATE — values where a fused or double-precision build
produces a different step index — with the discrimination property itself
asserted in the C and C++ legs, so a vector cannot quietly stop
discriminating. The corpus likewise pins a negative fixed-point tie value
that distinguishes half-away-from-zero rounding from the naive arithmetic
shift (§4.8 rule 2b). The generated quantize arithmetic is emitted
contraction-proof besides — the product lands in a named local in C/C++, an
explicit float64 conversion in Go — and a quantize scale the double domain
cannot hold exactly is a §4.6 compile error.

CI needs every target toolchain for gates 3–5 and 7; that cost is accepted —
it is the product's central claim.

### 7.3 The path — the order of work

1. **The spec and the language design.** This document. The language is the
   volatile part; everything downstream (compiler, backends) amplifies its
   changes, so it settles first.
2. **The examples corpus** (`examples/`): realistic `*.schema` files written
   against the spec — testing that the language can actually express those
   things, on paper, before a line of compiler exists. The language iterates
   against the corpus; a construct no example needs is a construct the
   language may not need.
3. **Only then, implementation** — and the corpus graduates into `testdata/`
   as the compiler's first test suite.

### 7.4 schemafmt — the one style

gofmt's philosophy: one style, no options. Built as the parser's first
consumer and run by every `schema` command over the unit before processing.
Rules:

1. **Indent: 4 spaces** per block level, never tabs. `{` on the construct's
   line; `} else {` on one line. One field or declaration per line.
2. **Alignment groups**: within a contiguous run of fields — broken by blank
   lines and by comment lines — pad names to align the type column and types
   to align the `[` attribute column. The same rule aligns `=` in const runs.
   Single space minimum between columns.
3. **Attributes**: `[min = 0, max = MaxHealth]` — spaces around `=`,
   comma-space between entries, no padding inside the brackets. Valueless
   markers first, then valued keys.
4. **Expressions**: single spaces around binary operators.
5. **Enums**: one line while they fit; the wrap trigger is decided at the
   first multi-line instance (no line-length limit exists yet).
6. **switch/case** (reserved for `switch`'s return): `case` at the same
   indent as its `switch`; a single-item case body inline after the label,
   bodies column-aligned across the cases of one switch; multi-item bodies on
   following lines, one level in.
7. **Blank lines**: collapsed to one; preserved as group separators.
8. **Comments preserved, never reflowed** — file-header block, section
   dividers (`// ---- name ----`), and doc comments stay attached to what
   they precede.
9. **schemafmt never reorders declarations** — a formatter formats; it does
   not move code. (Tags are sorted by name (§4.8), so ordering carries no
   meaning — the aspect layout (§2) stays a convention.)

## 8. Repository layout

The public Go API is `compiler/` and `ir/`; everything under `internal/` is
implementation, with no compatibility promise (VERSIONING.md).

```
cmd/schema/            the CLI — a client of the public API, and nothing more
compiler/              PUBLIC: the driver — load, generate, pack, format, and
                       the generator registration interface
ir/                    PUBLIC: the lowered form; the wire shape projection
                       (§3.1); the derived parameters the backends share
internal/scanner/      tokens, positions
internal/parser/       AST construction, error recovery
internal/ast/
internal/check/        resolver, constant folding, shape checks, dominance rule,
                       the protocol id
internal/format/       schemafmt
internal/pack/         the data compiler (schema pack)
internal/codegen/      c/  cpp/  csharp/  golang/  js/  rust/ — registered on
                       the driver through the public generator interface
internal/fuzz/         compiler fuzzing (gate 6)
internal/publicapi/    the acceptance gate: an external module, public API only
examples/              the corpus — always compiles under this spec as written
examples128/           the fixed-point + 128-bit corpus
testdata/              golden generated source, golden ids, golden wire bytes,
                       table goldens
test/                  the per-language wire test legs
bench/                 the benchmarking program (see bench/BENCH-STANDARD.md)
notes/                 extracted runtime API contracts (design inputs, non-normative)
SPEC.md                this document
```

License: AGPL-3.0, with an explicit additional permission for generated
output — see LICENSE.

## 9. Open questions, gathered

Rows keep their numbers permanently — code and corpus cite them as `§9 qN`.
Every row to date is settled, deferred with its design banked, or discarded.

1. ~~Strings as byte strings~~ — settled: §4.7. One shape for
   `string`/`bytes`; C# and Rust compose the wire from primitives; the
   interior-null check is generated-code validation in every target.
2. ~~Storage-type overrides~~ — settled by the integer family: storage is
   declared by the type name (`thrust int8 [min = 0, max = 100]`); no
   override mechanism exists.
3. ~~Wide strings and relative integers~~ — deferred: §4.10. Wide strings
   have no usage anywhere in the surveyed tree; every `int_relative` use site
   lives inside the packet shapes the delta pass owns.
4. ~~`schemafmt` timing~~ — settled: built early, as the parser's first
   consumer; rules in §7.4.
5. ~~Doc comments~~ — deferred; the design is kept at §4.1.
6. ~~A root/packet marker~~ — discarded.
7. ~~Const expressions over enum counts~~ — settled: `E.Max` (§4.2);
   `const NumTeams = Team.Max + 1`. `len(Team)` was declined: it has three
   plausible meanings, and every one sizes enum-indexed tables wrong under
   `[max]` headroom — the max is the true primitive.
8. ~~Platform-conditional constants~~ — settled: constants are
   platform-uniform (§4.2); platform-varying tuning stays in application
   code.
9. ~~Explicit enum variant values, flag enums, the separator~~ — settled:
   canonical enums only (§4.2) — explicit and sparse values are declined
   permanently; `flags` is supported (§4.2); variants are comma-separated.
10. ~~Sentinel-terminated collections~~ — deferred to the object/delta pass,
    which designs all three measured terminator idioms at once
    (bool-continuation, sentinel value, stop action). Readers are
    structurally oblivious to packet splits, so the language owes readers
    only a terminated-stream loop.
11. **Enum subranges, and the enum-as-index pattern** — deferred; the design
    is banked here for its return, likely with the claiming/index pass. Two
    kinds of enum-driven value are both required: an enum that can carry null
    (`None` — the plain `enum`), and an index over an enum's valid values
    that excludes it (laser index, missile index). The banked `enum_index`
    design: a type-level declaration; wire = value − 1 in [0, max − 1];
    storage keeps 0 as an unset sentinel that is never wire-legal. The v1
    consequence, stated plainly: a kind field is a plain `enum`, so a
    `[1, max]` create wire is not expressible — one unused wire value is
    spent per kind field. The derived-range rule stays normative (§4.2); the
    general `index(Set)` feature stays deferred with `index` informally
    reserved (§1).
12. ~~`int` → `int32`?~~ — settled by the integer family: bare `int` is
    gone; every integer names its width, and `int`/`uint` are reserved purely
    to give a helpful diagnostic.
13. ~~Interpolation policy~~ — settled: no interpolation generation in v1
    (§4.8). `Interpolate()` stays hand-written until a claiming pass assigns
    per-tag actions (§4.2, Type tags).
14. ~~The replication-policy boundary~~ — discarded: no send-scheduling
    knobs (priority/TTL/coherence) exist in this architecture, and no policy
    attributes ever will. schema fully owns serialization; nothing else was
    ever in scope.
15. ~~The engine interop surface~~ / 16. ~~The engine adoption asks~~ —
    discarded; externally-derived interop and adoption material is out of
    this repo.
17. ~~The unsigned fixed-point spelling~~ — settled: the explicit
    `ufixed(I, F)` keyword (§4.3, §4.6), storage always manifest in the type
    name — the integer family's own int/uint precedent. Deriving signedness
    from `min >= 0` was declined because a field's storage type would
    silently change when a bound crossed zero. One scope fence, recorded at
    §4.8 rule 2b: shallow narrowing stays signed-only until the unsigned
    shift arithmetic is designed.
