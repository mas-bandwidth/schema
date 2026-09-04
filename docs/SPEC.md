# schema — specification

This document is the normative reference for the schema language, its wire
encodings, and the code its compiler generates. Where the compiler and this
document disagree, one of them is a bug (see VERSIONING.md).

## 1. What schema is

**schema** is a small language for describing bitpacked network data, and a
compiler — written in Go — that translates `*.schema` files into generated C,
C++, C#, Dart, Elixir, Go, Java, JavaScript and Rust source code. Six of the
nine targets read and write through a serialize-family runtime library; the
other three carry the bit reader and writer in the generated code itself and
need nothing but their own toolchain:

| target | runtime library |
|---|---|
| C | [serialize.c](https://github.com/mas-bandwidth/serialize.c) |
| C++ | [serialize](https://github.com/mas-bandwidth/serialize) |
| C# | [serialize.cs](https://github.com/mas-bandwidth/serialize.cs) |
| Dart | none — the generated code is self-contained |
| Elixir | none — the generated code is self-contained |
| Go | [serialize.go](https://github.com/mas-bandwidth/serialize.go) |
| Java | none — the generated code is self-contained |
| JavaScript | [serialize.js](https://github.com/mas-bandwidth/serialize.js) |
| Rust | [serialize.rs](https://github.com/mas-bandwidth/serialize.rs) |

The runtimes are bit-for-bit wire compatible, pinned in CI with shared golden
bytes, and the self-contained targets are held to the same goldens. schema
inherits that foundation: **a type serialized by generated code in any
target language decodes identically in the others.**

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
flags, types and unions — a pure data contract with zero protocol
conventions. Delta serialization is out of scope for v1.

### Non-goals (v1)

- **No wire-format versioning on the TYPE wire.** Versioning here is by
  protocol id, deliberately (§3): hardcoded structs, one id, same-or-refuse.
  Data that must outlive the build that wrote it belongs to the other wire —
  `table` declarations, versioned in-wire by field id, where any reader reads
  any data and the differences are reported rather than fatal
  (SPEC-TABLES.md). This document specifies the type wire; the two share one
  language, one unit and one compiler, and nothing else.
- **No unbounded collections.** Everything on the wire has a declared bound,
  as everywhere in the serialize family.
- **No annotation of existing hand-written types.** schema owns the types it
  serializes and generates them (§6); the `cpp_native` mapping (§4.2) lets
  hand-written types derive *from* generated ones, never the reverse.
- **No imports across compilation units.** One unit, one package, all files
  compiled together (§3.2).
- **No self-describing wire data.** The wire stays an unattributed bit
  stream; all knowledge lives in the generated code on both ends.
- **Relative integers are out of scope by decision:** `serialize_int_relative`
  belongs to the delta layer, which is replicant's and serialize.pro's, not
  schema's (§4.10).

The grammar must not claim syntax reserved for planned language passes:
`packet`, `delta`, `baseline` and `index` are usable as ordinary names today
but are earmarked for future constructs and must not be given other meanings.

## 2. The name and the files

The language is called **schema**. Schema files use the `.schema` extension,
named in UpperCamelCase after their contents.

The conventional file layout is aspect-oriented — `Constants.schema`,
`Enums.schema`, `Types.schema`, `Wire.schema` — one aspect per file. This is a convention the corpus and documentation follow and
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

**Included — each fact moves the wire:** the package; every type's field
order; field NAMES; type kind, width and signedness; declared bounds; array
kind and bounds; string/wstring/bytes capacity; float range, resolution and
step count; fixed `I` and `F`; specified defaults (both ends must agree on the
value a constructor materializes — untaken branches read as ZEROS, never
defaults, per §5);
branch structure; `const`/`reserved`/`align` items; enum max and storage
bits; **every enum's variant names in declaration order**; flags wire bits
and **every flags declaration's variant names in declaration order**; union
arm order and count, **every arm's NAME in declaration order**, and each
ARM's OWN FIELD FACTS. The tag is positional and the arm is the wire (§4.8),
so an arm contributes what a field of its type contributes, and an arm with
no payload contributes that it has none. The
projection also carries FROZEN tokens — `table=false message=false` on
every type line and `round=nearest` on every compressed-float field line —
kept so the refusals of §4.11 moved no id; dropping one is a
`ProjectionVersion` bump.

**Why the names, when the ordinal is the wire.** An enum value rides as its
declaration ordinal, a flags variant as its bit position, a union arm as its
tag, so a REORDER changes what every stored ordinal, every set bit and every
tag means while the shape — the count, the storage, the mask width, the
payload types — stays exactly where it was. The ordered names are the only
record of which ordinal means what; without them the two builds either side
of an alphabetized enum hold one id and read each other's values as the wrong
variant, which is the spurious MATCH this projection exists to refuse. A
union's arm names are load-bearing in one shape the payload types cannot
describe: two arms of the SAME payload type reorder with every projected type
unmoved, and tag 1 then means one arm on one build and the other on the
other. The consequence throughout is that a RENAME moves the id too, since
order is spelled in names and nothing else. That is the price and it is free:
both sides of a connection redeploy together, so the rename buys the redeploy
that was already happening.

**Excluded — each has no effect on the bytes:** comments and whitespace; file
names, file layout and declaration order; `const`
declarations (their values are already resolved into the bounds above); type
tags and native-type attributes.

**A union with a `table` arm is excluded WHOLE**, as the table itself is. A
table has no packet wire, so an arm that names one has no packet encoding to
project (SPEC-TABLES.md §2.6).

**The projection carries two version lines.** `ProjectionVersion` rides the
first, so a change to the RENDERING moves every id deliberately and visibly
rather than silently. `WireLaw` rides the second, and it is the CODEC LAW's
version: the compiler's own rules for turning a value into bytes and bytes
back into a value, which the rendering cannot see. **Any compiler change that
can alter, for the same schema and the same values, the encoded bytes, the
inputs accepted, the reads rejected, the defaults materialized, or a numeric
conversion bumps `WireLaw`, and every protocol id in existence moves with
it** — in a minor release, announced first. The invariant the line exists to
hold: no generated byte and no read decision may change for the same schema
and input without the protocol id changing. Generated files carry the id
alone, so a bump costs no per-release churn in the tree.

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
- **File layout, declaration order across files, comments and whitespace move
  nothing.** Variant order inside an enum or a flags declaration is not
  declaration order in this sense: it is the wire, and it moves the id (§3.1).
- **Adding a target language never moves the id**, and a compiler upgrade
  moves it only through a deliberate `ProjectionVersion` or `WireLaw` bump.
  The corollary is a real obligation on the compiler: the same schema must
  produce the same wire across compiler versions, which is what the second
  line holds. The conformance corpus's golden wire bytes
  pin every construct's encoding permanently, and a compiler change that
  breaks a wire golden is a stop-the-line event, never a quiet fix (§7.2).

`reserved(bits)` fields remain useful *within* a protocol id — not to dodge
redeploys (claiming reserved bits moves the id like any other wire-shape
change), but to keep packet sizes and layout budgets stable while a protocol
grows into space already paid for.

Whether the id travels on the wire (a connect token, a `const` field, out of
band) is the application's choice — netcode-style stacks already carry one.

**The protocol id is the type wire's id and NOTHING else, and the boundary
is worth stating from this side too.** `table` declarations (SPEC-TABLES.md)
never enter the projection, so no table edit can move this id and no table
edit forces a lockstep redeploy. What a table edit moves instead is the unit's
BUILD VERSION (SPEC-TABLES.md §20) — the design's OTHER id, one digest over
every fact a cooked asset's bytes depend on, this id among them, and the key
a cooked asset is stored under. So a type edit moves both ids and a table edit
moves only that one. Peers connect on equal protocol ids and may differ in
build version; a build version never gates a connection, and it is derived
from this projection rather than carried in it.

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
  take no parentheses; array bounds are a prefix (`[..MaxObjects]ObjectState`);
  there are no semicolons; the canonical formatter is `schemafmt` — one
  style, no options.
- **Integer literals:** decimal, hex (`0x`), binary (`0b`). **Float
  literals** (decimal, with optional fraction and exponent) appear in float
  constants and as float attribute values (`min`/`max`/`resolution` on
  `float32`).
- **Punctuation and operators:** `{ } ( ) [ ] , : = ! ? . .. | + - * / %`
  (maximal munch: `..` wins over `.`). `?` is the OPTIONAL type prefix
  (§4.2's grammar), and it is accepted in a table body only — a `type` body
  refuses one by name (SPEC-TABLES.md §2.3). `|` opens a line's qualification
  section (§4.2, Attributes) and claims the rest of the line — the newline
  or a `//` comment terminates it, and no newline suppression applies after
  `|` or after a `,` inside a qualification. `<=` is not in the language: a count
  bound is a range literal (`[..N]`, `[A..B]`), never a truncated
  comparison, and the retired `[<= N]` spelling is refused with the
  replacement named.
- **Reserved words:** `package const enum type table map message object if else
  switch case align reserved`, the wire-type keywords `bits bool float32
  float64 string wstring bytes fixed ufixed`, and the integer family `int8
  int16 int32 int64 uint8 uint16 uint32 uint64 int128 uint128` — plus `int` and
  `uint`, reserved so `int` gets a "did you mean int32?" diagnostic instead
  of a parse error. Reserved words cannot be used as names. Attribute keys
  (`min`, `max`, `resolution`, ...) are contextual — they live only right of
  `|` and are not reserved.
- **Newlines terminate declarations and fields — there are no semicolons,
  like Go.** The newline is a terminator token, suppressed: immediately after
  `{`, `(`, `[`, `,`, `:`, `=`, `else`, and an infix operator; and
  immediately before `)`, `]`, `}`. **Right of `|`, ALL suppression is
  off** — from the pipe to the physical end of line, the newline always
  terminates (that is the no-wrap guarantee of §4.2), and a `/* */` block
  comment is refused there (its swallowed newline would let the section
  silently span lines). Blank lines are insignificant. **The house
  brace style is Allman**: a multi-line block's opening `{` stands on its
  own line, and `else` stands alone between its braces. A one-line
  `{ ... }` group — an inline enum list, a one-line type body — closes on
  its own line and stays whole. The parser reads BOTH placements (a
  line-oriented grammar treats a lone `{` line as the block opener); the
  formatter normalizes to Allman as the one canonical output. This is the
  language's own style, not a borrowed one ("we stop looking like a dialect
  of golang — we're not"). Two consequences make the grammar implementable as written:
  **a closing `}` also terminates the item before it** — in every production
  below, a trailing `NL` is satisfied by an actual newline or by the
  immediately following `}` — and **end-of-file synthesizes a terminator**,
  so a file without a trailing newline still parses. Within an enum or flags
  body, newlines around commas are ordinary whitespace (suppression after
  `,`) — variants are not items and need no terminator.

### 4.2 Grammar

EBNF (`NL` = the newline terminator; `{X}` repetition; `[X]` option). The
productions elide one tolerance stated in §4.1: a newline is accepted before
any `Block`, `UnionBlock` or `VariantList` opener — Allman is the canonical
placement and the formatter normalizes to it:

```
File        = { Declaration } .
Declaration = Package | Const | Enum | Flags | TypeDecl | TableDecl | Union .
Flags       = "flags" ident ( VariantList
            | AttrSection NL VariantList ) NL .                // "flags" contextual, §4.2;
                                                                   // a qualified declaration's
                                                                   // body opens on the NEXT line
Union       = "union" ident UnionBlock NL .                        // "union" contextual, §4.8
UnionBlock  = "{" { UnionVariant } "}" .
UnionVariant = ident [ ArmType [ AttrSection ] ] NL .              // AN ARM IS A FIELD LINE (§4.8):
                                                                   // the arm's name, any field type,
                                                                   // and the value-shaping attributes
                                                                   // that type takes. A BARE NAME is an
                                                                   // arm with NO PAYLOAD. No "= Default",
                                                                   // no "?", no was/json: each is
                                                                   // refused at the arm (§4.8,
                                                                   // SPEC-TABLES.md §2.6)
ArmType     = [ "[" Bound "]" ] Scalar .                           // the field Type without its "?".
                                                                   // An arm that names neither a
                                                                   // declared `type` nor nothing at all
                                                                   // is legal inside a table closure
                                                                   // only (SPEC-TABLES.md §2.6)
Package     = "package" ident NL .
Const       = "const" ident [ ConstType ] "=" ConstExpr NL .
ConstType   = IntType | "float32" | "float64" .
ConstExpr   = IntExpr | FloatExpr .
Enum        = "enum" ident ( VariantList
            | AttrSection NL VariantList ) NL .
VariantList = "{" [ ident { "," ident } [ "," ] ] "}" .            // commas; trailing comma OK
TypeDecl    = "type" ident ( Block
            | AttrSection NL Block ) NL .               // qualifiers = the type TAG and the
                                                        // cpp_* pair, §4.2
TableDecl   = "table" ident ( Block
            | AttrSection NL Block ) NL .               // the TABLE wire, SPEC-TABLES.md —
                                                        // a type body, plus pointers and `was`.
                                                        // A table declaration takes no
                                                        // qualifier of its own (§4.2)

Block       = "{" { Item } "}" .
Item        = Field | ConstField | Reserved | Align | If .
Field       = ident Type [ "=" Default ] [ AttrSection ] NL .   // the default DEFINES, so it
                                                                 // precedes the qualification
Default     = ConstExpr | ident .                    // specified default (see below):
                                                     // ident = true | false | an enum variant
ConstField  = "const" "(" IntExpr "," IntExpr ")" NL .          // (value, bits)
Reserved    = "reserved" "(" IntExpr ")" NL .
Align       = "align" NL .

Type        = [ "?" ] [ "[" Bound "]" ] ( Scalar | Map ) .       // array bound is a PREFIX, Go's order.
                                                                 // "?" is the OPTIONAL prefix
                                                                 // (SPEC-TABLES.md §2.3): the value plus
                                                                 // a generated presence bool. TABLE
                                                                 // BODIES ONLY — a type body refuses one
                                                                 // by name, as it does a pointer
Map         = "map" "[" Scalar "]" Type .                        // a LOOKUP over entries the wire
                                                                 // carries as a sorted array of one
                                                                 // generated { key, value } table
                                                                 // (SPEC-TABLES.md §2.8). The KEY is
                                                                 // string(N) or an integer kind, bare;
                                                                 // the VALUE is a whole field type, so
                                                                 // maps nest. TABLE BODIES ONLY — a type
                                                                 // body refuses one by name, as it does
                                                                 // a pointer
Scalar      = IntType
            | "int128" | "uint128"                               // 128-bit integers (§4.3);
                                                                 // field types only, not ConstType
            | "bits" "(" IntExpr ")"
            | "bool" | "float32" | "float64"
            | "string" "(" IntExpr ")"
            | "wstring" "(" IntExpr ")"                          // wstring(N) — N in UTF-16 CODE
                                                                 // UNITS (§4.12)
            | "bytes" "(" IntExpr ")"
            | "fixed" "(" IntExpr "," IntExpr ")"                // fixed(I, F) — signed Q I.F (§4.3);
                                                                 // the Q format is the type's SHAPE,
                                                                 // so it is positional like bits(N)
            | "ufixed" "(" IntExpr "," IntExpr ")"               // the unsigned sibling (§4.3)
            | "*" ident                                          // a POINTER to a declared table
                                                                 // (SPEC-TABLES.md §2.1);
                                                                 // TABLE BODIES ONLY — a type
                                                                 // body refuses one by name
            | ident .                                            // a declared type or enum
IntType     = "int8" | "int16" | "int32" | "int64"
            | "uint8" | "uint16" | "uint32" | "uint64" .
Bound       = IntExpr | ".." IntExpr | IntExpr ".." IntExpr .       // [N] exact; [..N] = [0..N],
                                                                    // "up to N"; [A..B] = count in [A, B].
                                                                    // An exact bound NAMING A DECLARED
                                                                    // ENUM is an ENUM-KEYED array: exactly
                                                                    // E.Max slots, one per named variant,
                                                                    // key k at index k-1, nothing stored
                                                                    // for None (SPEC-TABLES.md §2.4).
                                                                    // On this wire it IS [E.Max]T

AttrSection = "|" Attr { "," Attr } .                            // runs to END OF LINE: the newline
                                                                 // or a // comment terminates it —
                                                                 // nothing follows a qualification
Attr        = ident "=" ( ConstExpr | ident | string )           // valued:    min = 0, max = 100,
                                                                 //            cpp_include = "a.h"
            | ident .                                            // valueless: a type tag (§4.2)
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
              references ( ident "." "Max" | ident "." "Count" ):
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
  type — the default DEFINES the fresh value, so it precedes any `|`
  qualification: `active bool = true`. Defaults cover bool
  (`true`/`false`), integer and float fields (constant expressions,
  fit-checked like any use site), enum fields (a variant name), the 128-bit
  integers, and `fixed` (§4.6); arrays, strings, bytes, composite and union
  fields zero-initialize with no override. **The default is STORAGE initialization —
  what a freshly constructed object holds. It does not touch the wire**: per
  §5's read rule, fields in untaken branches read as ZERO values, not as
  defaults — the wire contract stays a pure function of the schema's
  encodings.
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
  `E`'s max — the derived count, or the widened `| max = K` — the same number
  the enum's wire range and storage derive from. The count of wire values is
  `E.Max + 1` by ordinary constant arithmetic; the count of real (non-`None`)
  variants is `E.Max` (see the sentinel-zero convention below), and that is
  the count an enum-indexed array is sized to: `[E.Max]T`, or the `[E]T`
  spelling that resolves to it (SPEC-TABLES.md §2.4). Under `max` headroom
  the reserved values above the last variant are wire-legal and name no slot.
  It works on the generated tag set
  too (a union's `<Union>Type.Max` — §4.8); generated sets resolve in
  constant expressions and nowhere else.
- **Declared-count references:** **`.Count`** in any integer expression names
  the DECLARED variant count of an enum or a flags declaration — one word
  meaning one thing in both. `E.Count` excludes an enum's implicit `None`;
  `F.Count` counts the named bits. Under `| max = K` headroom the count and
  the extent part — an enum's `Max` is the widened extent and a flags
  declaration's wire width is K, while `Count` stays the count — and without
  headroom `E.Count` equals `E.Max`. `[..E.Count]T` is therefore a counted
  array over the declared variants, where `[E.Max]T` is the keyed array with
  one slot per admitted ordinal. **Flags
  have `F.Count`, not `F.Max`** — a flags declaration is a set of
  independent bits, not a range with a top, so max-of-what is exactly the
  confusion `.Max` would invite and the compiler refuses it naming the
  split. `Max` and `Count` after `.` are contextual, like
  attribute keys; the lexer keeps maximal munch, so `..` still wins over
  `.`.
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
  `| max = K` headroom attribute can widen the wire. **Enum storage derives
  from the enum's own max** — the smallest unsigned integer that fits.
- **Canonical form only.** Enums are always `0 = None`, then `[1, max]`,
  dense. There are no explicit variant values and no sparse enums.
- **The enum family is two declaration forms:**
  - **`enum`** — the canonical with-`None` form above. Wire: the raw value,
    in **[0, max]**; `None` is a legal wire value (the null — the shape the
    generated `<Union>Type` tag follows too).
  - **`flags Name { ... }`** — each variant names one bit, assigned densely
    from bit 0 in declaration order, **up to 64**; **storage is `uint64` in
    every target**; no implicit `None` — the empty mask is 0 and needs no
    name. Wire: **W raw bits, W = variant count** (`| max = K` widens to K
    bits); every W-bit pattern is legal — a mask field's domain is all
    subsets. **More than 64 variants is a compile error** — one bit per
    variant, and the storage is `uint64`. Each target exports one mask
    constant per variant (`1 << bit`),
    because mask tests are how flag state is consumed, **plus the `Count`
    constant** — the declared variant count, spelled per target beside the
    enum extents — the spelling is each target's own, and all nine emit it:
    `CapsCount` in C++, C#, Go and JS; `CAPS_COUNT` in C and Rust;
    `capsCount` in Dart and Java; `caps_count/0` in Elixir.
    **`flags` is a
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

### Attributes — the qualification section, after `|`

A line is **definition, then qualification**: everything that defines the
value — name, type, specified default — comes first, and the qualifiers
follow a `|`, running to the end of the line:

```
health      int16   | min = 0, max = MaxHealth
thrust      int8    | min = 0, max = 100
orientation float32 | min = -180.0, max = 180.0, resolution = 0.01
w           fixed(2, 30) = 1.0 | min = -1, max = 1
sequence    uint16
```

- **The section is terminated by the newline or by a `//` comment** —
  `thrust int8 | min = 0, max = 100 // engine output` ends at the comment.
  (An ordinary comment BEFORE any pipe wins by ordinary lexing:
  in `foo uint8 // c | min = 0` the `|` is comment text and the field carries
  no qualification.) A `|` with no qualifier before the terminator is a
  parse error ("empty qualification section — write the qualifiers after |
  or drop it"), and `|` on a line that admits no qualification — a
  constant or a `union` declaration — is
  refused with the reason named (`|` is never an operator; the language has
  no bitwise-or). It cannot wrap: the pipe claims the rest of the line and
  nothing else can follow it. That **no-exit property is a guarantee**, not
  an accident — nothing can be appended past the qualifiers, so the
  definition/qualification partition cannot erode; and the zone right of
  the pipe is deliberately reserved room ("we can get much more creative in
  future after `|` ... if we need to" — the design's stated headroom).
- **The specified default sits BEFORE the pipe** — `= 1.0` defines what a
  fresh value holds, so it belongs to the definition, not the
  qualification: `w fixed(2, 30) = 1.0 | min = -1, max = 1`.
- **Declaration lines take the same section**: a type tag or native-type
  binding, an enum's or flags' `max` headroom —
  `type Quat | quat4`, `enum Weapon | max = 15`, `flags Damage | max = 8` —
  and the body brace opens on the next line — with a qualification section
  that is forced (the section runs to the end of the line), and everywhere
  else it is the Allman house style (§4.1):

  ```
  type Quat | quat4, cpp_native = quat_t, cpp_include = "core_math.h"
  {
      x float64
      ...
  }

  enum Weapon | max = 15
  { Laser, Missile, Railgun }
  ```

  A one-line body (`{ Laser, Missile }`, `type Box { x uint8 }`) stays
  whole on its line — braces around a group that closes on the same line
  are a list, not a block.
- **Array bounds are not attributes** and are untouched: a bound is a
  **prefix** — `[..MaxObjects]ObjectState`, Go's order — part of the type's
  shape. `[` after the complete type (or after its `= default`) opened the
  RETIRED trailing attribute block; the parser refuses it with the `|`
  spelling named. Scalar
  constraints like `min`/`max` apply per element.
- **Valueless qualifiers** (a type tag, §4.2) and valued keys sit side by
  side in one flat list — `| vec3, cpp_native = VecMath` — valueless
  markers first, then valued keys; there is no nested argument syntax.
- **The line between positional and attribute:** a *size* that defines the
  type's shape stays positional — `bits(64)`, `string(64)`, `wstring(64)`,
  `bytes(N)`, `fixed(I, F)`, array bounds. A *constraint or refinement* of a
  named type is a qualifier. The enum's `| max = 15` is the same syntax; it is one
  general mechanism. The glyph is deliberately the language's own — a DSL
  owns its spelling, and familiarity to other languages' readers is not a
  design constraint where clarity is better served ("let's be bold and do
  our own shit! It's a DSL anyway").
- **The vocabulary is typed and closed per compiler version — an unknown
  attribute is a compile error**, never a silently ignored string. The
  vocabulary: integers take `min`/`max` (both together or neither); `float32`
  takes `min`/`max`/`resolution` (all three together — §4.3: this IS the
  compressed float); `fixed`/`ufixed`/`int128` take `min`/`max` (required —
  §4.3); enum declarations take `max`; type declarations take a tag and the
  `cpp_native`/`cpp_include` pair (below); a field of a **table** body takes
  `was` (below); and a **table declaration** takes none.
- **`was = "old_name"` — the rename attribute, table bodies only**
  (SPEC-TABLES.md §5). A table field's wire id is the hash of its name, so a
  bare rename would orphan every byte ever written under the old one; `was`
  keeps the old identity through the rename: `speed float32 | was =
  "velocity"`. It takes the old name as a QUOTED STRING. On a `type` field it
  is refused by name: the packet wire is positional, so a rename orphans no
  stored value and there is no identity for `was` to carry. It is not a free
  edit — field NAMES ride in the projection (§3.1), so renaming a `type`
  field moves the protocol id and both sides redeploy together.

### Type tags — types are the user's; meaning is claimed later

**The language pre-defines no composite types** — no built-in vec3, no
built-in quat, no privileged class list. A user declares a type and may
**tag** it:

```
type Vec3 | vec3
{
    x float64
    y float64
    z float64
}

type Quat | quat4
{
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
  defines its own shape validation for claimed tags (e.g. `| quat4` requiring
  four float components) and its own policy toward unclaimed strangers.
- The architecture: types on one side; a layer that defines the needed
  actions for those types on the other. v1 ships the types; each schema
  declares its own, and each application's actions bind to them by claiming.

### Native type mapping — `cpp_native` / `cpp_include`

The compiler generates the **basis struct** — data members only, no
behavior — and a hand-written math type **derives** from it, adding operators
and methods without touching layout:

```
type Vector3 | vec3, cpp_native = Vector3, cpp_include = "core_vector.h"
{ ... }
```

```cpp
// core_vector.h, hand-written:
struct Vector3 : public space::Vector3 { /* operators, length(), ... */ };
```

With the mapping declared, generated C++ **storage speaks the native type**:
every field of a mapped type in every generated struct declares
`::Vector3` instead of `space::Vector3`, and
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
enum and the generated `<Union>Type` tag enums — carries its
extent as a member named `Max` in the target's own convention: `E::Max`
(C++), `E.Max` (C#), `EMax` (Go), `E::MAX` (Rust), `E.Max` (JS), `E_MAX`
(C), `E.max` (Dart and Java), `E.max/0` (Elixir). Its value is the enum's
max — the same number `E.Max` names in schema
expressions (§4.2): the highest wire-legal value, which under the
sentinel-zero convention is the count of real variants when no `max`
headroom widens it. Application code states ranges and asserts directly
against it (`ShipType`'s wire range is `[0, ShipType.Max]`, its real
variants `[1, ShipType.Max]`) instead of exporting a hand-declared count
constant that re-derives it. `Max` is therefore a reserved variant name —
declaring a variant named `Max` is refused at check time, exactly as `None`
is.

**The declared count is exported too.** Every declared enum carries its
**`Count`** beside its `Max`: the number of DECLARED variants, excluding the
implicit `None`. It is the same number `E.Count` names in schema expressions
(§4.2), it equals `Max` when no `| max = K` headroom widens the enum, and it
is below `Max` when one does — which is the whole reason it is exported. All
nine targets spell it their own way, the spelling each already uses for a
flags declaration's `Count`: `E::Count` (C++), `E.Count` (C#), `ECount`
(Go), `E::COUNT` (Rust), `E.Count` (JS), `E_COUNT` (C), `E.count` (Dart and
Java), `E.count/0` (Elixir). `Count` is a reserved variant name for the same
reason `Max` is, and it is a claimed name under §4.6 — a declaration whose
generated symbol would collide with an enum's `Count` is refused, naming the
enum. The generated `<Union>Type` tag enums carry `Max` alone: a tag set
takes no headroom, so its count and its extent are one number.

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
- **Length prefixes** (`string(N)`, `wstring(N)`, `bytes(N)`, `[..N]T`,
  `[Min..N]T`) are ranged integers over their declared count range, per the
  rows below.

The wire encodings are exactly classic serialize's — each row names its
classic twin, which is the wire oracle for the stated model.

| schema | wire | classic twin |
|---|---|---|
| `f bits(N)` | N raw bits, N in [1,64] | `serialize_bits` ([1,64]; >32 = low 32 bits first, then the high remainder) |
| `f intN` / `f uintN` (bare, N ∈ 8/16/32/64) | N raw bits (two's complement for signed) | `serialize_uint8/16/32/64`; signed raw is the same bits, cast |
| `f intN | min = A, max = B` / `f uintN | min = A, max = B` | minimal bits for the range, value − A; read rejects out-of-range; **the range must fit the declared storage** | `serialize_int` (≤32-bit int ranges) / `serialize_int64` / width-computed `serialize_bits` for full-unsigned ranges |
| `f uint128` (bare only) | 128 raw bits — the low 64-bit half first, then the high half; representation-independent (native `__int128` and the emulated two-lane pair produce identical bytes) | `serialize_uint128` |
| `f int128 | min = A, max = B` (range required) | value − A in `bits_required128(A, B)` bits, 32-bit groups from the bottom; read rejects an offset above B − A — reject, never clamp; **where the range fits 64 bits or fewer the bytes are identical to `serialize_int64` over the same bounds.** Bare `int128` and ranged `uint128` are compile errors — serialize's own surface, mirrored exactly (uint→raw, int→ranged) | `serialize_int128` |
| `f fixed(I, F) | min = A, max = B` (range required; **signed**) | Q I.F, the sign bit counting toward I; storage is a signed integer of exactly I+F bits (I+F ∈ 8/16/32/64/128, I ≥ 1, F ≥ 0); bounds are compile-time WHOLE UNITS fitting the Q format and int64; wire = raw − (A << F) in bitlen(B − A) + F bits, 32-bit groups from the bottom — **except A == B, which costs ZERO bits (not F): the reader materializes raw = A << F from the range alone (§4.6)**; read rejects above the raw range; round trip is EXACT (no quantization step), and with F = 0 the operation IS a ranged integer | `serialize_fixed` |
| `f ufixed(I, F) | min = A, max = B` (range required; **unsigned**) | UQ I.F: no sign bit, whole-unit domain [0, 2^I); storage is an UNSIGNED integer of exactly I+F bits (I+F ∈ 8/16/32/64/128, I ≥ 1, F ≥ 0); bounds are compile-time WHOLE UNITS fitting the unsigned domain and int64 (so I ≥ 63 clamps to int64's ceiling); the wire law is fixed's own — raw − (A << F) in bitlen(B − A) + F bits, A == B costs ZERO bits, read rejects above the raw range, round trip EXACT. The raw values of wide formats legitimately fill uint64's HIGH HALF (above 2^63): every route through a signed-typed runtime API is a bit-exact cast or zero-extension, never sign extension, and the corpus pins that byte-for-byte | `serialize_fixed` (unsigned storage — the codec is storage-generic) |
| `f bool` | 1 bit | `serialize_bool` |
| `f float32` | 32 raw IEEE-754 bits | `serialize_float` |
| `f float64` | 64 raw bits (low dword first) | `serialize_double` |
| `f float32 | min = A, max = B, resolution = R` | quantized to ceil((B−A)/R) steps — the actual step is (B−A)/ceil((B−A)/R), ≤ R; read rejects values above the step count | `serialize_compressed_float` (exact formulas incl. the ceil, +0.5f rounding and clamp); storage stays `float32` — the attributes describe the wire |
| `f Weapon` (an enum) | minimal bits for [0, max]; read rejects above max | `serialize_int` over [0, max] |
| `f Damage` (a `flags` declaration, §4.2) | W raw bits, W = variant count (or the widened max); every pattern legal; storage `uint64` in every target | `serialize_bits` |
| `f Inner` (a type) | Inner's fields, in place | `serialize_object` |
| `f Shape` (a `union`, §4.8) | tag in minimal bits for [0, variant count] (0 = None, no payload), then the selected variant's payload only; read rejects a tag above the count | `serialize_int` over [0, count] + the selected arm's own call: `serialize_object` for a declared `type`, this table's row for that arm's type otherwise, and no call at all for a payload-free arm |
| `const(Value, Bits)` | the constant; read **rejects** any other value | `serialize_bits` + compare |
| `reserved(Bits)` | zeros; read rejects nonzero | `serialize_bits` + compare |
| `align` | zero-pad to the next byte boundary; read rejects nonzero padding | `serialize_align` |
| `f string(N)` | length in [0, N], align, then the used bytes — N = max length; the bound sizes the length prefix's bits | `serialize_string` with buffer N + 1 |
| `f wstring(N)` | length in [0, N], **no alignment**, then one 32-bit group per UTF-16 code unit. N is the max length in CODE UNITS, and the bound sizes the length prefix's bits (§4.12) | `serialize_wstring` with buffer N + 1 |
| `f bytes(N)` | identical shape: length in [0, N], align, then the used bytes | `serialize_int` + `serialize_bytes` |
| `f [N]T` | N elements, back to back | element per element |
| `f [..N]T` / `f [Min..N]T` | count in [Min, N] encoded relative to Min, then that many elements | `serialize_int` + the element loop |

Arrays: the element may be any scalar or named type; arrays of arrays are not
in v1 (wrap the inner array in a type). Runtime-count arrays carry their own
count on the wire — there is no separately-declared count field in v1.

**Wire fidelity.** For every legal write, the bits are identical to the named
classic twins. **Classic `serialize_wstring` is the normative twin for
`wstring(N)`**: serialize.modern's `wstring_` inserts an `align` between the
length and the code units, and that alignment is the one thing schema does
not do (§4.12). On the *read* side, schema's generated readers enforce the
language's validation rules uniformly (e.g. the interior-null rule of §4.7
and the UTF-16 well-formedness rules of §4.12), which can be stricter than a
hand-written classic reader; acceptance is uniform across the targets, which
is what the conformance gates check.

**The count-range cap is lifted.** serialize.modern caps `array_n`'s count
range at 16 because each possible count is a separately spliced compile-time
path. schema's generated code uses an honest loop (§6.2), so `[..N]T` is
bounded only by what the count's integer range can express.

### 4.4 Decisions: `if` — and `switch` is not in v1

Conditional serialization branches on a previously serialized field — a
back-reference. The branch itself costs no wire bits; the referenced field
was already paid for.

```
type Body
{
    position Vec
    at_rest  bool
    if !at_rest
    {
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
  requires `| min = A, max = B` in whole units, A ≤ B, both fitting the Q
  format's whole-unit domain [−2^(I−1), 2^(I−1) − 1] AND int64 (where the
  runtime's compile-time bound parameters live). `ufixed(I, F)` carries the
  same shape rules with the unsigned domain — I ≥ 1 still, bounds in
  [0, 2^I − 1] clamped to int64's ceiling — and its diagnostics name the
  `ufixed` spelling. `int128` requires `| min = A, max = B` (bare `int128` is
  a compile error — serialize has no raw signed 128-bit operation); `uint128`
  refuses `min`/`max` (ranged 128-bit is `int128`). Specified defaults cover
  `int128`/`uint128` (fit-checked like any integer) and `fixed`: a fixed
  default is declared in WHOLE UNITS (the same domain as the bounds, so no
  raw/units confusion is possible) and must be EXACTLY representable —
  value × 2^F an integer, no rounding rule involved — `1.0` and `0.5` are
  legal in Q2.30, `0.1` is a compile error naming the constraint. Storage
  initializes to the raw scaled integer.
- **A range that does not fit its declared storage:**
  `int8 | min = 0, max = 1000` is a compile error — the range determines the
  wire, the type name determines the storage, and a legal wire value the
  storage truncates would be silent corruption that passes read validation.
- **A range that excludes zero requires a declared default in range:**
  `x uint8 | min = 1, max = 255` is a compile error, because zero
  initialization is the rule (§4.2) and a field whose range starts above zero
  or ends below it would be born outside its own range —
  `x uint8 = 1 | min = 1, max = 255` is the fix, on integer, `int128`,
  `fixed`/`ufixed` and compressed-float ranges alike. An array takes no
  specified default, so an array field's range must reach zero.
- **Attribute discipline:** an unknown attribute key, a key repeated, an
  attribute on a type that does not take it, `min` without `max` (or vice
  versa), and `resolution` without both bounds are compile errors, each
  naming the field and the legal vocabulary for its type.
- **Non-finite compressed-float parameters are rejected:** each of
  `min`/`max`/`resolution` (§4.3's triple)
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
  spelling); `string(N)` and `wstring(N)` with N < 2; `bytes(N)` with N < 1;
  `[N]T` with N < 1; `[..N]T` with N < 1; `resolution` ≤ 0. An empty `type`
  body is legal — zero wire bits, presence as the payload; a unit with
  no `.schema` files is an error.

  **An enum with no variants is legal.** It holds only the implicit
  `None = 0`, so its wire range is the degenerate `[0, 0]` and it costs zero
  bits — the value is recovered from the range alone, under the same rule as
  any degenerate range.
- **A derived size past the cap:** an array bound, a `string(N)`, a
  `wstring(N)` and a `bytes(N)` are capped at int32 one at a time (§4.3),
  and what they multiply
  to is capped too. A field's wire width, an array's whole storage, a record's
  storage and a `MaxBytes` buffer bound are each refused past 2^40 bytes
  (2^43 bits), with a diagnostic naming the field and the product, so no
  generated file can carry a size the arithmetic does not represent.
- **One flat namespace, and the compiler's claimed names:** all declaration
  kinds share one unit-level namespace (`const Foo` and `type Foo` collide),
  and no unit declares `ProtocolId`. The checker likewise refuses user names
  that collide with per-declaration generated symbols
  (`Write*`/`Read*`/`New*`, `*MaxBits`/`*MaxBytes`, companion length/count
  names, an enum's `Max` and `Count`, a flags declaration's `Count`, a
  union's generated tag surface). Diagnostics name the generated
  artifact that claims the name.
- Enum `| max = K` below the variant count.
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

**`string(N)` payloads are well-formed UTF-8, and the reader enforces it.**
The wire shape is unchanged and identical to `bytes(N)`. What the `string`
spelling adds is a read-side rule, stated once here and holding in all nine
targets, in every build mode: **a payload that is not well-formed UTF-8
fails the read.** Not well formed means not well formed under Unicode Table
3-7, so overlong forms, surrogates encoded in UTF-8, code points above
`10FFFF`, truncated sequences and the bytes `0xFE` and `0xFF` all fail. This
is classic `serialize_string`'s own rule, mirrored exactly. **A refusal is
terminal** (§5), and refusal is the only conforming answer: no target traps,
panics or aborts on a malformed payload. An application with genuinely
arbitrary payloads uses `bytes(N)`, which remains exactly that.

Beneath the encoding rule, `string(N)` carries **bytes excluding 0x00** —
all generated readers reject interior nulls, and writes assert per §5 (NUL
is valid UTF-8, so the interior-null rule is its own, stricter check).
`bytes(N)` is the same wire with no interior-null rule and no encoding rule.
The byte-string tightening is what lets the targets agree:

- Classic C++ `serialize_string` is strlen-based — it cannot represent
  interior nulls; a writer in another language must not be able to produce a
  payload the C++ reader silently truncates.
- Rust `String` storage would heap-allocate per value, which §6.1 forbids,
  so Rust storage stays a fixed byte buffer and the encoding rule is a
  validation pass over those bytes rather than a conversion into a native
  string.

For every legal write the wire bits are identical to `serialize_string` over
a buffer of N + 1.

**Generated-code consequence, stated so no backend discovers it late:** the
runtimes' `serialize_string` is the **wire oracle** for `string(N)`, not
necessarily the emitted call. **All nine backends compose the framing from
primitives** — length over [0, N], then align, then raw bytes — and none
emits a runtime string call. C# and Rust have no alternative: the §6.1
storage rules make the runtime string method unusable there — C# stores a
byte buffer but `SerializeString` takes `ref string`; Rust stores a byte
buffer but `serialize_string` takes `&mut String`, which allocates.
Dart, Java and Elixir have no runtime to call.
C++, C, Go and JavaScript may use their runtime string call for the framing
and do not. **The encoding check and the interior-null check are
generated-code validation in every target** — no runtime primitive performs
the second, and the first has to hold identically in the three targets that
have no runtime at all (classic C++ read appends `'\0'` and would silently
truncate).

**Goldens.** The read-side rule is pinned to serialize's shared corpus
(`conformance/string.txt`), which a `string(15)` field reproduces exactly,
its 4-bit length field being the corpus's `buffer_size` 16. The thirteen
refused vectors are `string-refuse-overlong-two-byte-nul`,
`string-refuse-overlong-two-byte-slash`,
`string-refuse-overlong-three-byte-slash`,
`string-refuse-surrogate-encoded-in-utf8`,
`string-refuse-low-surrogate-encoded-in-utf8`,
`string-refuse-code-point-above-10FFFF`, `string-refuse-five-byte-sequence`,
`string-refuse-lone-continuation-byte`,
`string-refuse-continuation-byte-after-ascii`,
`string-refuse-truncated-two-byte-sequence`,
`string-refuse-truncated-three-byte-sequence`, `string-refuse-byte-fe` and
`string-refuse-byte-ff`. Each is pinned beside the accepted vector the
corpus pairs it with, so a reader that refuses a byte class wholesale fails
an accept: `string-accept-shortest-two-byte-sequence`,
`string-accept-shortest-three-byte-sequence`,
`string-accept-just-below-the-surrogate-block`,
`string-accept-just-above-the-surrogate-block`,
`string-accept-astral-code-point`, `string-accept-the-largest-code-point`,
`string-accept-the-same-two-byte-sequence-completed` and
`string-accept-the-same-three-byte-sequence-completed`.

**Wide text is a separate construct**, not a spelling of this one: `wstring(N)`
carries UTF-16 code units, counts its bound in code units, and performs no
alignment (§4.12).

### 4.8 Unions — `union`, first-class one-of fields

A `union` declares a type that holds **at most one** of a named set of
payloads — first-class tagged-union machinery as a declarable type,
replacing the bool-guard idiom (`has_box bool` / `if has_box { box
BoxCollider }` repeated per shape), which spends one bit per absent arm and
makes illegal states representable — zero payloads, or several at once.

```
union ColliderShape
{
    box     BoxCollider
    sphere  SphereCollider
    capsule CapsuleCollider
    hull    HullCollider
}
```

An arm is a FIELD LINE, so an arm's type is any type a field's is, and an arm
may name no type at all:

```
union Value
{
    count  int32 | min = 0, max = 100
    label  string(64)
    offset Vec3
    ping
}
```

- **Grammar.** `union` is contextual like `flags`: `union ident { ... }`
  declares a union; `union` remains usable as an ordinary name everywhere
  else. **A body row IS a field line**, and the two are one shape under two
  productions (§4.2): `Field` carries a default and the optional `?`, and
  `UnionVariant` carries neither. A row is a variant name (field-style
  lower_snake, unique within the union), then the arm's type, then the
  value-shaping attributes that type takes: `| min`, `| max`, a compressed
  float's range and resolution. **A row that is a BARE NAME is an arm with
  no payload** (below). **What a row may not take is SPEC-TABLES.md §2.6's
  list**, each refused by name. A union FIELD likewise takes no
  attributes and no `= default` (it zero-initializes to None, joining
  arrays, strings, wide strings, bytes and composites in §4.2's no-override
  list).
- **An arm is any field type, or none.** A variant's payload is what a
  field's type can be: a scalar with its bounds, a compressed float, a
  `fixed(I, F)`, a 128-bit integer, an `enum`, a `flags` mask, `string(N)`,
  `wstring(N)`, `bytes(N)`, a bounded array `[N]T` or `[..N]T`, a declared
  `type`, another union, and, inside a table closure, a `table` and a pointer `*T`
  (SPEC-TABLES.md §2.6). **An arm may also have NO PAYLOAD**, written as
  its name alone: the arm selects and carries nothing, which is what a
  one-of wants for a case that is a fact rather than a value. It is not
  `None`, which says no arm was selected at all. Composition cycles through
  unions are compile errors exactly like type cycles (a payload that
  contains its own union has infinite size), and an arm whose type is a
  union joins that graph like any other edge. A union with **zero variants
  is legal**, mirroring the empty enum (§4.6): it holds only None, its tag
  range is the degenerate [0, 0], and it costs zero bits.
- **A union whose arms do not all name declared `type`s is a TABLE-CLOSURE
  construct** (SPEC-TABLES.md §2.6, §11), a `table` arm and a payload-free
  arm included: its shape is emitted beside the tables, and a `type` body
  refuses it by name. **The packet wire's rule is nevertheless fixed, so no
  port guesses it**: an arm rides after the tag as the field encoding its
  type already has on this wire, a bounded integer in its minimal bits, a
  compressed float in its steps, a string in §4.7's form, and nothing new
  is encoded. What waits is that encoding in nine backends, which is the
  named follow-on (SPEC-TABLES.md §15).
- **The implicit None row.** Entry 0 of every union is **None — no
  payload** — mirroring the enum sentinel-zero convention: optionality
  rides in-band as the natural stream terminator, a zero-initialized
  union field IS the empty union by construction (zero-value lists in §4.2
  and §5 include "None for a union"), and no separate has-flag exists to
  disagree with the tag. **Reserved variant names are checked over the
  EXPORTED spelling**: any variant whose exported form (the field-name
  mapping) is `None` or `Max` is refused — `none` and `max` included, not
  just the literal spellings.
- **The tag enum is generated, named `<Union>Type`**: `None = 0`, then
  each variant **in declared order**
  (exported spelling per target, the field-name mapping), dense from 1, plus
  the exported `Max` extent; storage per the enum storage rule, the
  smallest unsigned integer fitting max. Declared order, not sorted: a
  union is one declaration whose author states the order, exactly as an
  enum's variants do — and reordering variants is a wire change (see id,
  below), so the spelling of the source is the truth of the wire.
  `<Union>Type` and its member constants are claimed names, and the claimed
  set covers generated-vs-generated collisions too — two declarations whose
  generated symbols collide are refused as one namespace. **Generated sets
  are usable in constant expressions and nowhere else**: `<Union>Type.Max`
  works in integer expressions (§4.2); no generated set is a declarable
  field type. Variant names pass
  the same target-name safety and post-export uniqueness rules as field
  names (§4.6): a variant named a target's reserved word is refused, and
  two variants whose exported spellings collide (`box_a`/`boxA`) are
  refused.
- **The wire.** This bullet is the TYPE wire's, which a union of declared
  `type` arms rides and a table-closure union does not ride at all (above).
  The tag encodes in **minimal bits for `[0, variant
  count]`** (the enum wire rule), then **the selected variant's payload
  only**, the payload being that arm type's own encoding whatever the type.
  **A payload-free arm is the tag alone.** It costs what tag 0 costs, and
  the two are told apart by the tag's value and by nothing else.
  Tag 0 = None costs the tag bits and nothing else. The read path
  **rejects a tag above the count** — refusal, never clamping, the ranged-
  integer rule. MaxBits = tag bits + the largest payload's MaxBits.
  `Write<Union>` validates the tag BEFORE it rides — an out-of-set tag
  value in storage writes nothing and fails, it never desyncs the stream.
- **Read semantics are §5's.** The selected arm is **zero-established at
  selection** before its payload decodes. Arms not selected by a read are
  unspecified: in the C/C++ union representation their bytes are
  indeterminate; in targets whose storage lays every arm out separately
  (Go, C#, JS) an unselected arm keeps whatever it last held — the reused-
  storage discipline (§5's stale-tail carve-out extends to unselected union
  arms; whole-object comparison in the conformance matrix is over a fresh
  output or the selected arm). Consumers read the selected arm only. Nothing branches on
  a union's tag in v1 — `if` takes bools only (§4.4) — and a `switch` over
  `<Union>Type` is ruled-not-now, banked with §4.4's switch design.
- **A union field.** A union name is a field type inside `type` bodies,
  arrays included (`shapes [..4]ColliderShape`).
- **The id moves.** A union is wire structure: its arm order and count, every
  arm's NAME, and every arm's own field facts all shape bytes, so they
  project into the protocol id (§3.1). Declaring a union, adding, removing or
  reordering arms, changing an arm's type or its bounds, and RENAMING an arm
  each move the unit's id, and an arm's type moves it exactly as a field of
  that type in a `type` body would. The names are why a reorder is visible
  even when two arms carry the same type, and the rename is the price of
  that, exactly as it is for an enum or flags variant (§3.1). The one union
  outside all of this is a union with a `table` arm, which projects nothing
  at all, as the table does (§3.1). A union edit also moves the unit's BUILD
  VERSION (SPEC-TABLES.md §20).
- **Generated code, per target**: representation is per-language and
  explicitly NOT part of the contract; what binds every target is
  behavioral only — identical bytes, None is a valid empty read, an
  out-of-range tag is a validation failure. C++ generates a struct holding
  the `<Union>Type type;` tag over an anonymous union of the arms (member
  names = variant names), constructed as None, trivially copyable
  (asserted); a variant named `type` is refused at check time — the tag
  field's own name. **What an arm's own storage looks like is
  SPEC-TABLES.md §2.6's**, which states the companion an arm may need and
  the payload-free arm, whose whole surface is its tag value.
  C mirrors it with its named `as` union. Go, C#, JS, Dart,
  Java and Elixir lay the tag beside one pre-allocated arm per variant —
  nothing heap-allocates per value. Rust holds the value as a real
  `enum <Union> { None, Box(BoxCollider), ... }`, `None` the default — and
  STILL emits the `<Union>Type` tag newtype beside it: the tag surface
  (constants, `Max`) is uniform across targets whatever the value
  representation. Every union also gets `Write<Union>`/`Read<Union>` wire
  functions and `<Union>MaxBits`/`<Union>MaxBytes`, composable exactly
  like a type's.

### 4.9 A complete example

```
// Wire.schema
package protocol

const MaxObjects    = 1024
const MaxChatLength = 256

type Vec
{
    x float32
    y float32
    z float32
}

type ObjectState
{
    id       int32 | min = 0, max = MaxObjects - 1
    position Vec
    active   bool
    if active
    {
        orientation float32 | min = -180.0, max = 180.0, resolution = 0.01
    }
}

type Ping
{
    sequence uint16
}

type Pong
{
    sequence uint16
}

type Chat
{
    text string(MaxChatLength)
}

type Snapshot
{
    base_sequence uint16
    objects       [..MaxObjects]ObjectState
}

union Payload
{
    ping     Ping
    pong     Pong
    chat     Chat
    snapshot Snapshot
}
```

No tag enum is declared and no dispatch is written — the compiler generates
`PayloadType { None = 0, Ping, Pong, Chat, Snapshot }` and
`WritePayload`/`ReadPayload` from the union declaration (§4.8), and the
primitives compose into whatever framing the protocol wants: a `[..N]` array
of `Payload`, or a hand-written loop ending on the in-band `None`. (Ping and
Pong may both name a field `sequence` because they are separate types;
§4.6's unique-names rule is per type.)

### 4.10 Declined constructs

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
- **Relative integers (`serialize_int_relative`) are out of scope for
  schema.** The construct belongs to the delta layer, and the delta layer is
  replicant's and serialize.pro's, where it sits beside the entropy coding
  (rANS) that the same traffic wants. schema declares types and bitpacks
  them, so the primitive is not a v1 omission waiting to be filled. The
  classic use is a strictly-increasing sequence across array elements, with
  the previous element's field feeding the next, which the back-reference
  rule of §4.5 does not express, and a scalar-to-scalar form inside one type
  earns too little to carry the construct on its own.

### 4.11 Reserved and refused by name

schema declares a pure data contract — hardcoded structs, one protocol id,
same-or-refuse (§3) — and constructs outside that contract are refused BY
NAME rather than falling into a generic parse error:

- **`message`** — messages are not part of the language: a message set is a
  union of payload types plus your own framing (§4.8, and the pattern in
  §4.9's example).
- **`object`** — objects are not part of the language; wire types are
  `type`.
- **`contexts`** (contextual, at file scope) — build contexts are not part
  of the language; every field of a type is identical on every peer.
- **`round`** (the attribute) — rounding is not an attribute: it is the one
  fixed-point rule, half away from zero, everywhere (§4.3).

The projection (§3.1) keeps FROZEN tokens — `table=false message=false` on
every type line, `round=nearest` on every compressed-float field line — so
each refusal was id-neutral for every unit that never declared the
construct; changing a token is a `ProjectionVersion` bump, taken
deliberately or not at all. `table` declarations (SPEC-TABLES.md) never
enter the projection at all — a `type` line's token is `table=false`
forever, and packets and tables version independently.

### 4.12 Wide strings: `wstring(N)`

**Declaration.** `f wstring(N)`. **N is the capacity in UTF-16 CODE UNITS**,
not in code points and not in bytes, so a character outside the basic plane
occupies two of them. The bound is positional, part of the type's shape,
exactly as `string(N)`'s is, and it does two jobs: it sizes the length
prefix's bits and it sizes the storage. A `wstring` field takes no
attributes and no `= default` (§4.2), and `wstring(N)` with N below 2 is a
compile error, the same floor `string(N)` carries (§4.6).

**Why the language carries a wide type at all:** on a host whose native text
is already UTF-16, the wire and the string hold the same units, so text
crosses the boundary as a copy of code units rather than as a transcode.

**The wire.** Two steps, and no alignment in either of them:

1. **the length**, a ranged integer over [0, N] in `bits_required(0, N)`
   bits, encoded like any other ranged integer (§4.3),
2. **each code unit as a 32-BIT GROUP**, in order, packed LSB-first like
   every other value.

`MaxBits` for the field is therefore `bits_required(0, N) + 32 * N`, with no
padding term. A `wstring(N)` field introduces no alignment point, so unlike
`string` and `bytes` it does not make its type's layout depend on the entry
bit offset (§6.1).

**Classic `serialize_wstring` is normative.** serialize.modern's `wstring_`
inserts an `align` between the length and the code units, and that align is
the one thing schema does not do. The classic twin for the row is
`serialize_wstring` over a buffer of N + 1, whose length field is
`serialize_int( length, 0, buffer_size - 1 )` and whose first group begins
at the bit immediately after it.

**Validation: what every generated reader enforces, uniformly.** The rules
are stated once here and hold in all nine targets, in every build mode, in
this order:

- **The length.** A decoded length outside [0, N] fails the read. The check
  runs before the group loop, so the length never drives a copy it has not
  been bounded for.
- **The group value.** A group above `0xFFFF` is not a UTF-16 code unit and
  fails the read, on every target and every platform, whatever local wide
  character type the host happens to have.
- **The interior null.** A zero group among the transmitted groups fails the
  read. This is §4.7's interior-null rule in code-unit terms, and it exists
  for the same reason: a payload whose wire length disagrees with the length
  a downstream consumer computes from a terminator carries two lengths, and
  everything between them rides past whichever side uses the other.
- **Surrogate pairing.** A high surrogate (`0xD800`-`0xDBFF`) not
  immediately followed by a low surrogate (`0xDC00`-`0xDFFF`) fails, a low
  surrogate not immediately preceded by a high surrogate fails, and a high
  surrogate as the final transmitted group fails. Well-formed **pairs** are
  valid, and they are how astral text travels.
- **Exhaustion.** Running out of input mid-read fails, like any other read
  (§5).

**What no reader enforces**, stated so that no target can be stricter than
another: nothing else about the text is examined. Noncharacters are
accepted, `0xFFFF` included. There is no normalization, no case folding, no
code-point count, and no check that the code units spell anything in
particular. A reader that adds a check here is as wrong as one that drops a
check above, because what the nine targets owe each other is an identical
accept or reject verdict on identical bytes.

**A refusal is terminal.** Nothing after a failing read has a defined
position, so the read fails in the target's own error idiom (§6.1) and
leaves the output object unspecified (§5). Refusal is the only conforming
answer: no target traps, panics or aborts on a malformed payload.

**The two text types are symmetric on read:** `string(N)` refuses malformed
UTF-8 (§4.7) and `wstring(N)` refuses unpaired surrogates, both on the read
path, both in all nine targets, and both terminal.

**The write side** follows §5's doctrine and §4.7's precedent exactly. The
length bound is checked on every write in every target, because it guards
the copy. UTF-16 well-formedness is a writer obligation whose enforcement is
the read-side refusal above, so no write-side check is load-bearing anywhere
and none is required of a target. A code unit above `0xFFFF` cannot be
written at all, because the storage holds 16 bits per unit in every target.

**Storage, and the conversion rule at the boundary.** Storage is a
pre-allocated buffer of UTF-16 code units with a used length beside it,
under §6.1's rule that nothing is dynamically sized, and the generated read
and write paths transcode in no target:

| target | storage | boundary conversion |
|---|---|---|
| C++ | `char16_t[N + 1]` + `int32_t` length | none. A caller holding `std::u16string`, or `wchar_t[]` on Windows, copies code units. The field is code units and never `wchar_t`, so a 4-byte `wchar_t` caller splits astral code points into pairs in its own code |
| C | `uint16_t[N + 1]` + `int32_t` length | none, the C++ rule in C's own types |
| C# | `char[]` (capacity N, pre-allocated) + `int` length | none. `char` IS a UTF-16 code unit: `new string(buf, 0, len)` and `String.CopyTo` copy |
| Java | `char[N]` + `int` length | none. `char` IS a UTF-16 code unit: `new String(buf, 0, len)` and `String.getChars` copy |
| JavaScript | `Uint16Array(N)` + length | none. A JS string is UTF-16 code units: `String.fromCharCode` and `charCodeAt` copy |
| Dart | `Uint16List(N)` + `int` length | none. A Dart `String` is UTF-16 code units: `String.fromCharCodes` and `String.codeUnits` copy |
| Go | `[N]uint16` + `int32` length | a transcode, in application code, in both directions: `utf16.Encode` from the runes of a Go string on the way in, `utf16.Decode` on the way out |
| Rust | `[u16; N]` + length | a transcode, in application code, in both directions: `str::encode_utf16` on the way in, `String::from_utf16` on the way out |
| Elixir | a binary of 16-bit little-endian code units, `byte_size(v) >>> 1` being the used length | a transcode, in application code, in both directions: `:unicode.characters_to_binary(s, :utf8, {:utf16, :little})` on the way in and the inverse on the way out. The little-endian convention is in-memory only and never reaches the wire, which carries each code unit as the 32-bit group above |

**The honest cost, per target.** On C#, Java, JavaScript, Dart, and C++ or C
on a UTF-16 host, the boundary is a copy of code units with no transcoding
step, which is the reason the type exists. On Go, Rust and Elixir, whose
native text is UTF-8, the transcode is real and is paid in application code
in both directions. No target transcodes inside the generated read or write
path, and no target allocates per value.

**Goldens.** The wire is pinned to serialize's shared corpus
(`conformance/wstring.txt`), which a `wstring(7)` field reproduces exactly,
its 3-bit length field being the corpus's `buffer_size` 8. **The row's own
golden is `wstring-worked-example`**, three basic-plane code units in 13
bytes. The accepted set at `wstring(7)` adds `wstring-empty`,
`wstring-single-basic-plane-character`, `wstring-accept-group-ffff`,
`wstring-accept-just-below-the-surrogate-block`,
`wstring-accept-just-above-the-surrogate-block`,
`wstring-accept-surrogate-pair` and
`wstring-accept-two-basic-plane-groups`, and at `wstring(4)` it adds
`wstring-accept-length-inside-a-five-character-buffer`. The refused set,
which gate 5 holds every target to together, is the corpus's seventeen
refusals: the two group-above-`0xFFFF` vectors, the seven unpaired-surrogate
vectors, the three interior-null vectors, the two out-of-range length
vectors, and the three past-end vectors. Two corpus vectors have no schema
declaration that reproduces them, `wstring-buffer-size-1-zero-bit-length-field`
and `wstring-no-alignment-before-the-characters`, because their
`buffer_size` of 1 and 2 sits below the N floor of 2. The no-alignment
property loses nothing by it: the worked example's first group begins at bit
3, so an align before the groups moves every byte after it.

Beside the corpus, the cross-language matrix carries a `wstring(7)` field
holding serialize.js's interop cases: empty, three basic-plane code units,
`0xE000`, `0xFFFF`, an astral pair between two basic-plane units, and seven
code units, the most the bound carries.

## 5. Trust model — inherited

**Reads validate everything** — integer ranges, enum bounds, alignment
padding, constants, reserved bits, count bounds, string lengths, the
interior-null rule, the UTF-8 and UTF-16 well-formedness rules of §4.7 and
§4.12, and buffer exhaustion (running out of input mid-read is a
read failure like any other) — and fail on any violation, because network
input is the trust boundary. Generated read code never lets a value that
controls iteration go unchecked before use.

**Read termination:** `Read` consumes exactly the encoded bits and reports
bits consumed, so callers can frame several objects in one buffer; bytes
remaining after the last field are the caller's concern, not a validation
failure — a stream framed over a union ends on the in-band `None` tag by
design.

**Writes assume trusted data.** The write path is trusted; writing correctly
is the caller's responsibility. Writer inputs are stated as OBLIGATIONS, not
defined behaviors: the spec owes a conforming writer exact bytes and owes a
misbehaving writer nothing. The read side is untouched by this doctrine —
readers face untrusted data and keep every mandated check above.

Within that doctrine, misuse surfaces by each target's own convention — a
language verifies correctness the way that language verifies correctness —
and the list is exhaustive at all nine. **Three debug-assert**, unchecked in
release: C++ through `serialize_assert`, Dart and Java through the language's
own `assert`, live under `--enable-asserts` and `-ea` (Java's contracts ride
one `check<Name>` predicate call, so a dormant assert costs the JIT one
inlining slot rather than a body). **One raises in every build**: Elixir's
`ArgumentError`, the BEAM having no dormant assert to compile out.
**Five return failure from the write** rather than invent an assert their
language does not have: C `0`, C# and JavaScript `false`, Go
`serialize.ErrValueOutOfRange`, Rust `Err(serialize::Error::ValueOutOfRange)`
— and JavaScript's flat tier, which forks checked/production at module load
(§6.1), refuses with `-1` on the checked side and trusts the caller on the
production one. **No target panics and none throws**: Elixir's raise is the
only unwinding path in the nine, and it is the BEAM's own. The generated
write code's job is to
make misuse impossible by construction — bounds come from the schema. Costlier contracts
assert in DEBUG ONLY, and only where a target carries one at all. Text
well-formedness is not one of them. UTF-8 in `string(N)` and UTF-16 in
`wstring(N)` are READ-side refusals in every target (§4.7, §4.12), so the
guarantee rests on the reader rather than on a write-side check some
targets carry and others do not. Ranges are trusted
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
   | `wstring(N)` | `char16_t[N + 1]` + `int32_t` length | `char[]` (capacity N, pre-allocated) + `int` length | `[N]uint16` + `int32` length | `[u16; N]` + length |
   | `bytes(N)` | `uint8_t[N]` + `int32_t` length | `byte[]` (capacity N, pre-allocated) + `int` length | `[N]byte` + `int32` length | `[u8; N]` + length |
   | `[N]T` | `T[N]` | `T[N]` (pre-allocated) | `[N]T` | `[T; N]` |
   | `[..N]T` | `T[N]` + `int32_t` count | `T[N]` (pre-allocated) + `int` count | `[N]T` + `int32` count | `[T; N]` + count |
   | `[Min..N]T` | as `[..N]T` (count validated to [Min, N]) | as `[..N]T` | as `[..N]T` | as `[..N]T` |
   | `f Inner` (a named type) | `Inner` | `Inner` | `Inner` | `Inner` |

   The C target mirrors the C++ storage rules in C's own types; JavaScript
   storage is `Number` for wire widths of 32 bits or fewer and `BigInt` for
   64 and 128 (the serialize.js value-domain seam). `wstring(N)` storage in
   all nine targets, and the conversion rule each one carries at the
   boundary, is §4.12's own table.

   **The storage principle behind every row: nothing is dynamically sized.**
   Generated storage never heap-allocates per value in any target: every
   buffer is fixed at its declared capacity with a used length/count beside
   it — no Go slices as storage, no Rust `Vec`, no growing containers.

   Enums are integer-backed named types in every target because `| max = ...`
   headroom makes non-variant values wire-legal; a native Rust `enum` cannot
   hold them — C#'s `enum E : uint` can, natively, which is why it needs no
   newtype.

2. **`Write(buffer, object) -> bytesWritten`** — straight-line write code in
   wire order.
3. **`Read(buffer, object) -> ok/error`** — straight-line read code with full
   validation, in each target's native error idiom — the list is exhaustive
   at all nine: `int` 1/0 in C, `bool` in C++, Dart and Java, `bool` +
   latched `Error` in C#, `error` in Go, `Result` in Rust, `{:ok, value}` or
   `:error` in Elixir, and `bool` + the stream's latched `error` in JS
   (generated validation refusals return `false` latching nothing, so callers
   tell the two channels apart exactly as in C#). The consumed size (§5)
   surfaces per target idiom — a success value that carries bits consumed
   where the idiom allows, an out-parameter where it does not.
4. **`MaxBits` / `MaxBytes`** — constants: the longest path through the
   schema, with worst-case (7-bit) padding assumed at each alignment point.
   Size write buffers from `MaxBytes`; conservative is correct for a buffer
   bound. **`MaxBytes` is rounded up to the 8-byte write-buffer granularity
   every serialize runtime requires.**
5. **`ProtocolId`** — one constant per unit (§3).
6. **For a `union` declaration (§4.8):** the `<Union>Type` tag enum
   (`None = 0`, then the variants in declared order), the value
   representation in each language's own idiom, and
   `Write<Union>`/`Read<Union>` with the tag framing — representation per
   target, behavior identical.

**Generated symbol naming:** functions attach per target idiom — C:
lower_snake free functions `write_ship_create(stream, value)`, with
`schema_`-prefixed internal helpers; C++: free
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
`WriteX`/`ReadX`/`ZeroX` (the runtime's methods stay camelCase —
the seam is the stream parameter).

**There is no generated measure function.** `Write` returns the actual size,
`MaxBytes` sizes buffers, and that covers the real uses. Anyone who genuinely
needs per-value sizing can hand-write stream code beside the generated
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

**Reflection is emitted, never requested.** Every unit gets one view file
carrying the reflection descriptors of every declaration and the registry
over them; nothing in the schema asks for it and no flag selects it. A
project that walks declarations compiles that file; a project that does not
never includes it and pays nothing for it. The surface it carries, and the
gates it is held to, are SPEC-TABLES.md §8.

**Output layout.** Each target emits one generated file per schema file —
`examples/Constants.schema` → `generated/cpp/Constants.h` — so the generated
tree mirrors the schema tree a person navigates. What a target adds to that
one file is its own, and the bullets below state it in full: a wire header
beside a data header, a table-side file per surface, a runtime home per unit,
or a file per declaration where the language demands one.

- **C++: the header splits into a data/wire pair.** `<Base>.h` is the DATA
  header (constants, enums, flags, structs and MaxBits/MaxBytes bounds)
  and includes `serialize.h` only when a storage type demands it
  (int128/fixed); `<Base>Wire.h` is the WIRE header (`Write*`/`Read*` and
  the union tag pairs)
  and includes `<Base>.h` plus `serialize.h`, with cross-file wire deps
  riding the deps' wire headers. The split exists so a consumer can base its
  math types on generated structs without inheriting the serialize runtime at
  all (a codebase may vendor an older serialize generation whose macro names
  collide): data consumers include `<Base>.h`; wire consumers include
  `<Base>Wire.h`. A unit may not contain files named both `X` and `XWire` —
  the emitter refuses the collision. Everything sits in
  `namespace <package>`, under `#pragma once`, with cross-file `#include`s
  derived from actual references. **The type wire is HEADER-ONLY**, and
  stays so by ruling ("I think it's OK for types to remain header only"): a
  type is a struct and its codec, both of which a compiler folds into the
  caller, so there is nothing to compile once and link. A unit that declares
  TABLES emits two further pairs per schema file. `<Base>Table.h` /
  `<Base>Table.cpp` carries the table wire's codecs, its reflection
  descriptors, its TEXT FORM and the cooked form's `<Name>Open`
  (SPEC-TABLES.md §7) — a pair rather than a header because the table wire
  has a RUNTIME the type wire has no equivalent of (SPEC-TABLES.md §6.1,
  §13.5). Beside it, `<Base>Block.h` /
  `<Base>Block.cpp` carries the block form, which nothing declares and every
  fixed table has (SPEC-TABLES.md §19): include the header and compile the
  source beside it only if you use the form, and `<Base>Table.h` carries not
  one symbol of it. They are table-side files and add nothing to a table-free
  unit. **Every unit emits one further pair per UNIT** —
  `<Package>View.h` and `<Package>View.cpp` — carrying the unit registry and
  the reflection descriptors of every declaration (SPEC-TABLES.md §8.3,
  §8.5). It is per unit rather than per schema file because the registry is
  the set of everything the unit declares, and it is named for the package
  because two units may share one output directory (§3.2). It includes the
  unit's DATA headers and no wire header, so a tool that walks declarations
  inherits no serialize runtime — and a project that never walks them never
  includes the header or compiles the source.
- **C:** the same data/wire header pair per schema file (`<Base>.h` /
  `<Base>Wire.h`), mirroring the C++ split in C's own types. A unit that
  declares TABLES emits one further pair per schema file — `<Base>Table.h`
  and `<Base>Table.c` — on the C++ pair's terms and for the C++ pair's
  reason, plus `<Base>Block.h` / `<Base>Block.c` for the block form
  (SPEC-TABLES.md §19). The split is WIDER here than in C++: C has no inline
  variables, so the reflection descriptors and the text form's generic walk
  are DEFINED in the `.c` and only declared in the header, and a translation
  unit that includes the header for the wire codecs pays for neither.
  **Every external the table backend emits carries the package** —
  `schema_<package>_<type>_<what>_` — because C has no namespace and two
  units whose type names collide have to LINK together, which is what the
  conformance driver itself does. The name-first surface SPEC-TABLES.md §11
  states is `static` in the header and forwards to them, and it is **spelled in
  the convention this target's PACKET half already uses** — types PascalCase,
  functions and file-scope constants snake_case, macros SCREAMING_SNAKE under
  `SCHEMA_` — so §11's `<Name>Load` is `<name>_load` here, `<name>_from_json`,
  `<name>_table_type`, `<name>_block_open`. §11 names the SUFFIX SET a
  declaration may not collide with; each target spells that set in its own
  language, exactly as Rust spells it `<name>_load` and Go, C# and C++ spell it
  `<Name>Load`. A table half that spelled C++'s casing in C would be the only
  place in this compiler where two halves of one target disagree. Two units
  still cannot be included into ONE translation unit, which is the C target's
  standing limit and unchanged by tables.
  **The C target reserves `SCHEMA_` and `schema_`**, and it is the one place a
  generated name and a declared name meet with no compiler between them:
  constants, enum variants and flag masks are all `#define`s here, so a
  collision with a generated macro is a SILENT REWRITE rather than a
  redeclaration error — the generator's `#ifndef` sees the user's definition
  standing and skips its own. The front end refuses a declaration whose C
  spelling is one of the macros the generated sources define.
- **Go:** one `.go` file per schema file, all in `package <package>` — Go
  packages are order-free across files, so there is no topo sort and no
  include graph to refuse. A unit that declares TABLES grows three further
  files per schema file — `<Base>Table.go` with the table wire's codecs and
  its reflection descriptors, `<Base>Block.go` (SPEC-TABLES.md §19) and
  `<Base>Cook.go` (SPEC-TABLES.md §7) for the two accelerators — plus one
  `<Home>TableJson.go` per unit carrying the TEXT FORM's single generic walk
  over those descriptors (SPEC-TABLES.md §16). A Go package compiles whole,
  so the separate file does not let a consumer leave the walk out of the
  build; the LINKER drops what nothing calls, and the file is what keeps that
  legible. A table-free unit grows none of it.
- **Rust:** one module per schema file (lowercased basename) plus a generated
  `lib.rs` declaring and glob re-exporting them. A unit that declares TABLES
  grows three per-file modules and three per-UNIT runtimes, all declared by
  that same crate root: `<base>_table.rs` with the table wire's codecs, its
  reflection descriptors and its TEXT FORM (SPEC-TABLES.md §16);
  `<base>_cook.rs` with the cooked form's `<Name>Cook` handles, their open
  paths and the layout const asserts those rest on (§7, §20.3);
  `<base>_block.rs` with
  the block form's projection and open path (§19); and beside them
  `table_runtime.rs`, `cook_runtime.rs` and `block_runtime.rs`, which carry
  each surface's shared runtime and the text form's one generic walk. **The
  three runtimes are named by the PACKAGE and not by a file**, on the rule
  §19.2 states for every port: a unit is one crate, so a second copy would be
  a duplicate definition rather than C++'s harmless re-inclusion behind a
  guard, and a runtime that lived in whichever file sorted first would
  relocate whole whenever a corpus file sorted earlier. Two more modules are
  always compiled and belong to neither accelerator: `build_version.rs`
  (SPEC-TABLES.md §20 — one digest answering "which build?", which both the
  block form and the cooked form compare against) and `<base>_records.rs` (the
  blittable `<Name>Row` records with their layout contract, because a cooked
  record IS the blittable row). **The block and cook modules sit behind cargo
  features, both on by default**, so a consumer that reaches for neither
  compiles neither — the Rust answer to §19's "the form costs nothing unless
  you include it". A table-free unit grows none of it, and its packet modules
  are byte-identical either way.
- **C#:** one `.cs` file per schema file, types at namespace level and every
  function and constant on `public static partial class Schema`, in
  `namespace <Package>`. A unit that declares TABLES emits three further
  files per schema file — `<Base>Table.cs` carrying the table wire's codecs,
  its reflection descriptors and its TEXT FORM (SPEC-TABLES.md §16), and
  `<Base>Block.cs` and `<Base>Cook.cs` for the two accelerators (§19, §7) —
  plus one RUNTIME HOME per unit and per surface, `<Package>Table.cs`,
  `<Package>Block.cs` and `<Package>Cook.cs`, where everything shared lands.
  Each of the three is one file rather than the C++ header/source pair
  because a unit's C# files compile into one assembly, so the shared runtime
  and the text form's generic walk are emitted once per unit rather than once
  per translation unit behind a guard; the home is named for the PACKAGE on
  §19.2's rule for every port. Every unit emits one further file per unit,
  `<Package>View.cs`, on the same terms as the C++ pair above.
- **Dart:** one library per schema file, cross-file `import`s derived from
  actual references, with `show` clauses naming exactly the symbols used. A
  unit that declares TABLES grows three further libraries per schema file —
  `<Base>Table.dart` (the table wire's codecs, its reflection descriptors and
  its TEXT FORM, SPEC-TABLES.md §16), `<Base>Block.dart` and
  `<Base>Cook.dart` (the two accelerators, §19 and §7) — plus one RUNTIME HOME
  per unit and per surface, `<Package>Table.dart`, `<Package>Block.dart` and
  `<Package>Cook.dart`, which the per-file libraries import. **A Dart library
  IS a file**, so a runtime shared across a unit's files must be public, and
  the home is named for the PACKAGE on §19.2's rule for every port: a runtime
  that lived in whichever file sorted first would relocate whole the day a
  corpus file sorted earlier. The backend spells **no private library-scope
  name at all** — a schema identifier may begin with an underscore, so a
  generated `_foo` at top level would be a collision no registry covers
  (SPEC-TABLES.md §11). A table-free unit grows none of it, and its packet
  libraries are byte-identical either way.
- **Java:** one `.java` file per schema file for the packet half, in
  `package <package>`. A unit that declares TABLES emits `<Base>Table.java`
  per schema file with the table wire's codecs and its reflection
  descriptors, and then FANS OUT: **a public Java type lives in a file of its
  own name**, so the two accelerators are one file per declaration rather
  than one per schema file — `<Name>Block.java` (SPEC-TABLES.md §19) and
  `<Name>Cook.java` (§7) per table, and `<Name>Row.java` for every blittable
  record in the closure, plain `type` members included (§20.3). The shared
  runtime fans out the same way, one file per runtime type
  (`TableReader.java`, `TableReport.java`, `TableJson.java` and the rest),
  which is file-order independent by construction rather than by a rule and
  is why this port needs no named home; each of those spellings is claimed
  for every backend (SPEC-TABLES.md §11). `BuildVersion.java` is always
  emitted beside them and belongs to neither accelerator (§20). A table-free
  unit grows none of it.
- **Elixir:** one `.ex` file per schema file, carrying one `defmodule` per
  declaration under the unit's own namespace plus the file-scope module
  `<Ns>.<Base>` for constants, flags masks and the file's codecs. A unit that
  declares TABLES grows one further file per schema file — `<Base>Table.ex`
  with the table wire's codecs, its reflection descriptors and its TEXT FORM
  (SPEC-TABLES.md §16) — the two ACCELERATORS' READ side beside it,
  `<Base>Block.ex` (§19) and `<Base>Cook.ex` (§7), and three per-UNIT runtimes:
  `TableRuntime.ex` with the shared wire runtime and the text form's one
  generic walk, `BlockRuntime.ex` and `CookRuntime.ex`. `BuildVersion.ex`
  (SPEC-TABLES.md §20) is always emitted and belongs to neither accelerator,
  because a build version answers "which build?" and not "which form?". Each
  runtime is named for the PACKAGE and not for a file, on the rule §19.2 states
  for every port: a unit's modules compile into one application, so a second
  copy would be a duplicate module rather than C++'s harmless re-inclusion
  behind a guard. **The two accelerators are READ-ONLY here**, and that is the
  language rather than the port: a BEAM term has no layout a producer could
  write, so this backend opens a block or a cook another build wrote and reads
  every slot at its offset. A table-free unit grows none of it, and its packet
  modules are byte-identical either way.
- **JavaScript:** one ES module per schema file, cross-file `import`s derived
  from actual references; classes whose constructors initialize every member
  in declaration order (specified defaults live in construction; `ZeroX` is
  the §5 zero form). **Generated JS never imports the serialize runtime** —
  every wire call is a method on the stream parameter, so no wiring file
  exists and this tier's checked/production fork stays where it lives, in the
  runtime's own load-time mode selection: the runtime-tier module reads no
  `NODE_ENV` of its own. Beside it, **every schema file that declares types
  also emits `<Base>Flat.js`** — the FLAT tier, a single-word bitpacker
  inlined at every field with no runtime import at all, held byte-identical
  to the runtime tier by a standing gate. That tier owns the fork itself:
  `NODE_ENV` is read ONCE at module load, exactly as `serialize.js`'s own
  `src/mode.js` does, and whole write variants are selected at export, so a
  bundler that statically replaces `NODE_ENV` tree-shakes the checked writers
  out. The READ side is never configurable in either tier. A unit that
  declares TABLES emits three further modules per
  schema file — `<Base>Table.js` for the table wire's codecs, its reflection
  descriptors and its TEXT FORM (SPEC-TABLES.md §16), and `<Base>Block.js`
  and `<Base>Cook.js` for the two accelerators' READ side (SPEC-TABLES.md
  §19, §7). They are three modules rather than the C# single file because a
  block and a cook are compiled by a consumer that opts into them, and
  because a Table module carrying one block symbol is what the zero-cost gate
  refuses. Everything SHARED by a unit — the table runtime, the text form's
  generic walk, the record accessors — is emitted ONCE into
  `<Package>Table.js` / `<Package>Block.js` / `<Package>Cook.js` and imported
  everywhere else: an ES module is file-scoped, so a second copy would be a
  second, unequal binding, and naming the home for the PACKAGE keeps file
  order from moving it (SPEC-TABLES.md §19.2).

Each generated file is headed by the source file's BASENAME
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
branching and runtime range computation captures most of the win; a
bitpacker-level emission mode for fixed-layout structs remains available
behind the same byte-identity tests if measurement ever shows a gap worth
closing.

### 6.3 Per-target notes

All nine targets are covered, in the two groups §1 draws. The six that read
and write through a serialize-family runtime:

| | C | C++ | C# | Go | JavaScript | Rust |
|---|---|---|---|---|---|---|
| emits against | `serialize_write_stream_t`/`serialize_read_stream_t` free functions | `WriteStream`/`ReadStream` methods (or `serialize_*`-equivalent calls) | sealed `WriteStream`/`ReadStream` (`ref` params, `bool` returns + sticky `Error`) | `WriteStream`/`ReadStream` concrete types (no interface dispatch) | methods on the stream parameter — generated JS never imports the runtime | `WriteStream`/`ReadStream` via the `Stream` trait, monomorphized |
| error idiom | `int` 1/0 early-out; the stream latches the error | `return false` early-out | `bool` early-out; counts checked before loops; latched `Error` for callers | sticky stream errors; counts checked before loops; `return stream.Err()` | `bool` early-out; validation failures return false without latching, stream failures latch on `stream.error` | `?` propagation of `serialize::Error` |
| buffer contract | write buffers multiple of 8; read allocations extend ≥8 bytes past packet data (required) | write buffers multiple of 8 (asserted); read allocations extend ≥8 bytes past packet data (required) | write buffers multiple of 8 (throws); reader takes (buffer, bytes), no slack required | write buffers multiple of 8; ≥7 bytes read slack for the fast path | caller-owned DataViews; the flat tier requires ≥8 bytes read slack past the payload | write buffers multiple of 8; ≥8 bytes read slack for the fast path |

The three self-contained targets — no runtime library, so there is no stream
object to emit against; the bit reader and writer live in the generated code:

| | Dart | Java | Elixir |
|---|---|---|---|
| emits against | free functions over a caller-owned `ByteData` view | `static` methods on the file's class over a caller-owned `byte[]` | module functions over a binary; read and write thread the output, scratch and bit position as rebound accumulators — no stream struct |
| error idiom | `bool` early-out | `boolean` early-out | `{:ok, value}` or `:error`; the body throws `:invalid` to a catch at the surface |
| buffer contract | write buffers hold `<name>MaxBytes` (a multiple of 8); read buffers need NO slack past the payload | write buffers hold `<name>MaxBytes` (a multiple of 8); read buffers need NO slack past the payload | the writer returns the binary; read buffers need NO slack past the payload |

## 7. The compiler

Go, zero third-party dependencies, one static binary: `schema`.

```
schema check      [--verbose] [dir|files...]   // parse + typecheck; exit code for CI
schema generate   [--lang c|cpp|cs|dart|elixir|go|java|js|rust]
                  [--out <dir>] [--verbose] [dir|files...]
schema id         [dir|files...]               // print the protocol id
schema projection [dir|files...]               // print the wire shape projection (§3.1)
schema build-version [--facts] [dir|files...]  // the cook/block id, and the text it digests (SPEC-TABLES.md §20)
schema tables-baseline [--update --reason "..."] [--verbose] [dir|files...]
                                               // the table wire's evolution gate (SPEC-TABLES.md §18)
schema fmt        [--verbose] [dir|files...]   // the canonical formatter, and the only command that writes a schema file
schema pack       --root <Table> --out <file> [--tolerate] [--verbose] <tree-dir> [dir|files...]
schema unpack     --root <Table> --in  <file> [--one-file] [--tolerate] [--verbose] <tree-dir> [dir|files...]
                                               // a text tree to a table-wire file, and back (SPEC-TABLES.md §17)
schema cook       --root <Table> --in  <file|tree-dir> --out <file>
                  [--byte-order little|big] [--attribution <file>] [--tolerate] [--verbose] [dir|files...]
schema cook-check [--root <Table>] [--attribution <file>] [--verbose] <file.cook> [dir|files...]
schema uncook     --root <Table> --in  <file.cook> --out <file> [--attribution <file>] [--verbose] [dir|files...]
                                               // the cooked form: produce, validate, and back to the wire (SPEC-TABLES.md §7)
schema version
schema help
```

**Thirteen commands, plus `help`, which prints exactly the thirteen.** Six
serve the type wire this document specifies; the other seven belong to the
table wire and are specified in SPEC-TABLES.md, because one binary reads one
set of declarations and both wires are declared in it.

Success is silent. Commands whose printed output is their answer (`id`,
`projection`, `build-version`, `version`) print it; everything else prints
nothing unless `--verbose` asks for the per-file report — the files
`generate` wrote, the files `fmt` rewrote, `check`'s ok line, the
header facts a cook was written with. Errors and
diagnostics always reach stderr, and exit codes do not depend on verbosity.
`pack` and `unpack` exit nonzero when their read report is not silent, and
`--tolerate` accepts it.

**`fmt` is the only command that writes a schema file.** Every other command
loads the unit as it sits on disk and leaves it byte for byte alone, so a
read-only checkout, a sandboxed build, a concurrent generation and an editor
integration all work, and a command that answers a question never edits the
source it answered about. Formatting decides no answer: the unit a canonical
file declares is the unit its unformatted twin declares, and the protocol id
depends only on the wire shape (§3.1). One style, no options, no separate
binary; a file already in format is never touched. The formatter carries two
built-in refusers: it re-parses its own output and structurally compares the
AST against the input's, refusing to write on any difference (a formatter must
never change meaning), and it verifies its own idempotence on every run.

### 7.1 Pipeline

```
*.schema → scanner → parser → AST → resolver/checker → IR →
         {c, cpp, csharp, dart, elixir, golang, java, js, rust} backends
```

- **Scanner/parser: hand-written, recursive descent**, in the style of the Go
  toolchain's own `go/scanner`/`go/parser`. No parser generators. The
  language is LL(2); hand-rolled parsing is what makes precise diagnostics
  cheap. The parser recovers at declaration boundaries so one error does not
  hide the rest.
- **Resolver/checker**: name resolution across the unit's files, constant
  folding (arbitrary-precision with per-use fit checks, §4.2), the shape
  checks of §4.6, and the dominance rule of §4.5.
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
  is a small package a reviewer can hold (two to seven files).

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
discriminating.

CI needs every target toolchain for gates 3–5 and 7; that cost is accepted —
it is the product's central claim.

### 7.3 The corpus discipline

The examples corpus (`examples/`) is the language's proving ground: a
construct earns its place by a realistic example needing it, and a construct
no example needs is a construct the language may not need. The corpus must
always compile under the spec as written (`make check`), and it feeds the
golden pins of §7.2 — so the language's own examples are also the compiler's
first test suite.

### 7.4 schemafmt — the one style

gofmt's philosophy: one style, no options. Built as the parser's first
consumer, and run by `schema fmt`, which is the only command that writes a
schema file. Rules:

1. **Indent: 4 spaces** per block level, never tabs. **Braces are Allman**
   (§4.1): a multi-line block's `{` on its own line at the construct's
   depth, `else` alone between its braces; a one-line `{ ... }` group stays
   whole. One field or declaration per line.
2. **Alignment groups**: within a contiguous run of fields — broken by blank
   lines and by comment lines — pad names to align the type column, and pad
   past the longest DEFINITION (the type plus any `= default`) to align the
   `|` qualification column. The same rule aligns `=` in const runs. Single
   space minimum between columns.
3. **Qualifications**: `| min = 0, max = MaxHealth` — a space each side of
   `|`, spaces around `=`, comma-space between entries. Valueless markers
   first, then valued keys. A trailing `//` comment survives after the
   section.
3b. **Migration is a one-shot mode, not the formatter's default**:
   `schema fmt -migrate` additionally accepts the two RETIRED spellings —
   the trailing `[ ... ]` attribute block (with its default after the
   attributes) and the `[<= N]` bound — and re-emits the file in the
   CURRENT canonical form: qualifiers moved right of `|`, the default moved
   before the pipe, a qualified declaration's body brace moved to the next
   line, then the ordinary rules above. An attribute block wrapped across
   lines (the old grammar allowed it) is refused with "unwrap the attribute
   block first" — a wrapped block can carry interior comments that have no
   home right of a pipe. Plain `schema fmt` refuses retired spellings
   exactly as the compiler does.
4. **Expressions**: single spaces around binary operators.
5. **Enums**: the variant list is one line while it fits (a qualified enum
   is two lines by grammar — the list is the measured unit); the wrap
   trigger is decided at the first multi-line instance (no line-length
   limit exists).
6. **switch/case** (reserved for `switch`'s return): `case` at the same
   indent as its `switch`; a single-item case body inline after the label,
   bodies column-aligned across the cases of one switch; multi-item bodies on
   following lines, one level in.
7. **Blank lines**: collapsed to one; preserved as group separators.
8. **Comments preserved, never reflowed** — file-header block, section
   dividers (`// ---- name ----`), and doc comments stay attached to what
   they precede.
9. **schemafmt never reorders declarations** — a formatter formats; it does
   not move code. (Declaration order at file scope carries no wire meaning
   (§3.1), so the aspect layout (§2) stays a convention. The order of
   variants INSIDE an enum, a `flags` or a union is the opposite — declared
   order is the wire, an enum's ordinal and a `flags` bit position and a
   union's tag alike (§4.2, §4.8) — and that is the second reason a
   formatter never sorts.)

## 8. Repository layout

The public Go API is `compiler/` and `ir/`; everything under `internal/` is
implementation, with no compatibility promise (VERSIONING.md).

```
cmd/schema/            the CLI — a client of the public API, and nothing more
compiler/              PUBLIC: the driver — load, generate, format, and
                       the generator registration interface
ir/                    PUBLIC: the lowered form; the wire shape projection
                       (§3.1); the derived parameters the backends share
internal/scanner/      tokens, positions
internal/parser/       AST construction, error recovery
internal/ast/
internal/check/        resolver, constant folding, shape checks, dominance rule,
                       the protocol id
internal/format/       schemafmt
internal/codegen/      c/  cpp/  csharp/  dart/  elixir/  golang/  java/  js/
                       rust/ — registered on the driver through the public
                       generator interface; ctable/, cpptable/, cstable/,
                       darttable/, elixirtable/, gotable/, javatable/,
                       jstable/ and rusttable/ are the table emitters, one
                       per backend, all nine (SPEC-TABLES.md)
internal/fuzz/         compiler fuzzing (gate 6)
internal/publicapi/    the acceptance gate: an external module, public API only
examples/              the corpus — always compiles under this spec as written
examples128/           the fixed-point + 128-bit corpus
testdata/              golden generated source, golden ids, golden wire bytes
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
   `string`/`bytes`; every backend composes the wire from primitives; the
   UTF-8 encoding check and the interior-null check are generated-code
   validation in every target.
2. ~~Storage-type overrides~~ — settled by the integer family: storage is
   declared by the type name (`thrust int8 | min = 0, max = 100`); no
   override mechanism exists.
3. ~~Wide strings and relative integers~~ — wide strings are settled:
   `wstring(N)` is §4.12, its wire the classic `serialize_wstring` and its
   reader rules uniform across the nine targets. Relative integers are out
   of scope by decision (§4.10): the construct belongs to the delta layer,
   which is replicant's and serialize.pro's.
4. ~~`schemafmt` timing~~ — settled: built early, as the parser's first
   consumer; rules in §7.4.
5. ~~Doc comments~~ — deferred; the design is kept at §4.1.
6. ~~A root/packet marker~~ — discarded.
7. ~~Const expressions over enum counts~~ — settled: `E.Max` (§4.2);
   `const NumTeams = Team.Max`. `len(Team)` was declined: it has three
   plausible meanings, and every one sizes enum-indexed tables wrong under
   `max` headroom — the max is the true primitive.
8. ~~Platform-conditional constants~~ — settled: constants are
   platform-uniform (§4.2); platform-varying tuning stays in application
   code.
9. ~~Explicit enum variant values, flag enums, the separator~~ — settled:
   canonical enums only (§4.2) — explicit and sparse values are declined
   permanently; `flags` is supported (§4.2); variants are comma-separated.
10. ~~Sentinel-terminated collections~~ — deferred to the delta pass,
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
13. ~~Interpolation policy~~ — settled: no interpolation generation;
    interpolation stays hand-written until a claiming pass assigns per-tag
    actions (§4.2, Type tags).
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
    silently change when a bound crossed zero.
