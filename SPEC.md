# schema — specification (draft 1)

> **Status: DRAFT, pre-implementation.** This is the spec that precedes the decision to build,
> per house doctrine. Sections are marked **DECIDED** (Glenn, 2026-08-04) or **PROPOSED**
> (Rowan's recommendation, awaiting review). Nothing here is wire-tested yet.

## 1. What schema is

**schema** is a small language for describing bitpacked network data, and a compiler — written
in Go — that translates `*.schema` files into generated C++, Go and Rust source code targeting
the serialize family of libraries:

| target | runtime library |
|---|---|
| C++ | [serialize](https://github.com/mas-bandwidth/serialize) |
| Go | [serialize.go](https://github.com/mas-bandwidth/serialize.go) |
| Rust | [serialize.rs](https://github.com/mas-bandwidth/serialize.rs) |

All three runtimes are bit-for-bit wire compatible and pin that property in CI with golden
bytes. schema inherits that foundation: **a struct serialized by generated code in any target
language decodes identically in the other two.**

The idea is extracted from
[serialize.modern](https://github.com/mas-bandwidth/serialize.modern)'s compile-time schema
language, which proved two things: a schema that sees the packet as a type can generate
radically faster serialization than a runtime stream (~12x classic stream writes on the
benchmark packet), and it can hold wire compatibility to the byte while doing so. What the
template-metaprogramming host cannot escape is that the metaprogram re-runs inside every
consumer's C++ compiler, requires C++23, and can only ever emit C++. schema moves the same
computation into an external compiler: **run once, at code-generation time, emitting for every
platform, with zero compile-time tax on the consumer.**

### Goals

1. **One source of truth for a protocol.** Types and their wire encoding defined once, in
   `*.schema` files; read/write code generated for C++, Go and Rust.
2. **Minimal generated code.** Separate, straight-line read and write functions per type — the
   code a careful expert would hand-write against each serialize library — with no runtime
   schema interpretation, no reflection, and no `IsWriting` branching.
3. **Byte-identical wire output across all three targets**, enforced by a conformance matrix
   in CI, with classic serialize as the oracle.
4. **Compile-time cost near zero for consumers.** Generated code is plain source; the C++
   target requires only what classic serialize requires.
5. **Errors a person can act on.** Every diagnostic carries file:line:column and names the fix,
   holding the bar serialize.modern set with its named `static_assert`s — and exceeding it,
   because a real compiler can point at the exact token.

### Non-goals (v1)

- **No wire-format versioning.** See §3: versioning is by protocol id, deliberately.
- **No unbounded collections.** Everything on the wire has a declared bound, as everywhere in
  the serialize family.
- **No annotation of existing hand-written types.** schema owns the types it serializes and
  generates them (§6). Mapping onto pre-existing structs may come later.
- **No flatbuffers-style evolvable tables.** Named as a likely future direction for config and
  asset data (Glenn, 2026-08-04); explicitly out of scope for the bitpacked v1.
- **No self-describing wire data.** The wire stays an unattributed bit stream; all knowledge
  lives in the generated code on both ends.

## 2. The name and the files — DECIDED

The language is called **schema**. Schema files use the `.schema` extension, named in
UpperCamelCase after their contents: `Messages.schema`, `Snapshot.schema`. A compilation unit
is a set of schema files compiled together (one directory by default).

## 3. Versioning: the protocol id — DECIDED, mechanism PROPOSED

**Only two sides holding the same protocol id can talk. There is no versioning overhead on the
wire, no optional-field tags, no evolution machinery.** This is the serialize-family model:
for realtime network data you ensure client and server are at the same protocol version before
they exchange a byte, which radically simplifies everything downstream. (Glenn, 2026-08-04.)

**The protocol id is a hash of the schema itself** — DECIDED. The compiler computes it; nobody
maintains it by hand; it is impossible for two sides with different wire formats to
accidentally claim compatibility.

**PROPOSED mechanism:** the id is the low 64 bits of SHA-256 over the **canonical encoding of
the lowered IR** (§7), not over the raw file bytes. Two candidate definitions were considered:

| hash over | property |
|---|---|
| raw `*.schema` file bytes | simplest to explain; but a comment edit or reformat changes the id and forces a fleet redeploy over a change that did not touch the wire |
| canonical IR (**recommended**) | the id changes **iff the wire format or its validation changes** — field renames, comments, whitespace and declaration reshuffles that lower to the same IR keep the id; any change to bit layout, ranges, branch structure or constants moves it |

The IR hash is recommended because it makes the id mean exactly one thing: *these two sides
speak the same bits*. Either definition is a one-line change in the compiler; the choice is
Glenn's.

Generated code exposes the id as a constant (`ProtocolId` / `PROTOCOL_ID`), and `schema id`
prints it. Whether the id travels on the wire (a connect token, a `const` field, out of band)
is the application's choice — netcode-style stacks already carry one.

`reserved(bits)` fields remain useful *within* a protocol id: they stage claimed-but-unused
space so a planned extension flips reserved bits to real fields in one schema change.

## 4. The language

### 4.1 Lexical structure

- UTF-8 source. `//` line comments and `/* */` block comments.
- Identifiers: `[A-Za-z_][A-Za-z0-9_]*`. Type and struct names are conventionally
  UpperCamelCase, fields lowerCamelCase; the compiler does not enforce case.
- Integer literals: decimal, hex (`0x`), binary (`0b`). Float literals: decimal with optional
  exponent. Negative literals allowed where ranges permit.
- Newlines terminate declarations; no semicolons.

### 4.2 Declarations

A schema file contains, in any order:

```
package protocol                 // optional; namespaces the generated code

const MaxPlayers = 64            // compile-time integer constants, usable in any range/bound

enum Weapon {                    // wire = minimal bits for [0, max]
    Pistol
    Shotgun
    Rocket
}                                // max defaults to variant count - 1; pin headroom explicitly:
enum Item (max = 15) { Sword Shield Potion }

struct Vec { ... }               // the unit of serialization; see below
```

`package` maps to a C++ namespace, a Go package name, and a Rust module. Constants fold at
compile time and generate as constants in each target.

### 4.3 Field types

Inside a `struct`, each line declares a field: `name wiretype`. The wire type determines both
the generated storage type (§6) and the encoding. The encodings are exactly classic
serialize's — each row names its `serialize_*` twin, which is the wire oracle.

| schema | wire | classic twin |
|---|---|---|
| `f bits(N)` | N raw bits, N in [1,64] | `serialize_bits` / `serialize_bits64` |
| `f int(Min, Max)` | minimal bits for the range, value − Min; read rejects out-of-range | `serialize_int` |
| `f int64(Min, Max)` | minimal bits for the 64-bit range | `serialize_int64` |
| `f bool` | 1 bit | `serialize_bool` |
| `f float` | 32 raw bits | `serialize_float` |
| `f double` | 64 raw bits | `serialize_double` |
| `f compressed_float(Min, Max, Res)` | quantized to Res-sized steps; read rejects values above the step count | `serialize_compressed_float` |
| `f Weapon` (an enum) | minimal bits for [0, max]; read rejects above max | `serialize_int` over [0, max] |
| `f Inner` (a struct) | Inner's fields, in place | `serialize_object` |
| `const(Value, Bits)` | the constant; read **rejects** any other value | `serialize_bits` + compare |
| `reserved(Bits)` | zeros; read rejects nonzero | `serialize_bits` + compare |
| `align` | zero-pad to byte boundary; read rejects nonzero padding | `serialize_align` |
| `f string(N)` | length in [0, N−1], align, then the bytes | `serialize_string` |
| `f bytes(N)` | align, then exactly N bytes | `serialize_bytes` |
| `f bytes(<= N)` | length in [0, N], align, then the bytes | `serialize_int` + `serialize_bytes` |
| `f T[N]` | N elements, back to back | element per element |
| `f T[<= N]` / `f T[Min..N]` | count in [Min, N] relative to Min, then that many elements | `serialize_int` + element loop |
| `f int_relative(prevField)` | classic bucket encoding; values strictly increasing; read rejects violations | `serialize_int_relative` |

**Zero wire deviations.** serialize.modern's schema mode documents one deviation (`wstring_`
aligns before its code points, needed by its constant-offset machinery). schema's generated
code emits sequential stream operations and needs no such accommodation: every construct is
byte-identical to its classic twin, with no exceptions. Wide strings are deferred from v1
until a concrete need arrives; when added they match `serialize_wstring` exactly.

**The count-range cap is lifted.** serialize.modern caps `array_n`'s count range at 16 because
each possible count is a separately spliced compile-time path. schema's generated code uses an
honest loop (§6), so `T[<= N]` is bounded only by what the count's integer range can express.

### 4.4 Decisions: `if` and `switch`

Conditional serialization branches on a field **already serialized unconditionally earlier**
in the same struct — a back-reference. The branch itself costs no wire bits; the referenced
field was already paid for.

```
struct Body {
    position Vec
    atRest   bool
    if !atRest {
        velocity Vec
        angularVelocity Vec
    }
}

struct Message {
    type MessageType             // an enum, serialized here
    switch type {
    case Ping:      pingSequence bits(16)
    case Chat:      text string(256)
    case Move:      target Vec
    }                            // a value matching no case serializes nothing — symmetric
}
```

- `if cond { ... } else { ... }` — `cond` is a previously serialized `bool` field, optionally
  negated. Wire = the taken side only. (This subsumes serialize.modern's fused `branch`: a
  `bool` field followed by an `if` on it produces the identical wire.)
- `switch field { case ... }` — `field` is a previously serialized integer or enum field.
  Wire = the matching case's fields; no match, nothing — identically on write and read.
- Back-reference validation is a compile error, not a runtime surprise: forward references,
  references to fields inside a conditional, and references to never-serialized fields are
  each rejected with a diagnostic naming the offending reference and the rule.

Fields on exclusive branch sides may reuse names only if their types agree in the generated
struct (§6); the compiler flattens branch-local fields into the one generated type.

### 4.5 Shape checks

Everything serialize.modern validates with named `static_assert`s becomes a compiler
diagnostic with a source position:

- a range the declared wire type cannot hold, or a `bits(N)` width outside [1,64]
- the same field serialized twice (exclusive `if`/`switch` sides do not collide)
- `int_relative` referencing anything but an unconditionally-earlier integer field
- a `const`/`reserved` value that does not fit its width
- a count range wider than its element bound, a string/bytes bound below 1
- cycles in struct composition, undefined types, duplicate declarations

### 4.6 A complete example

```
// Messages.schema
package protocol

const MaxObjects = 1024
const MaxChatLength = 256

enum MessageType { Ping Pong Chat Snapshot }

struct ObjectState {
    id        int(0, MaxObjects - 1)
    position  Vec
    active    bool
    if active {
        orientation compressed_float(-180, 180, 0.01)
    }
}

struct Vec {
    x float
    y float
    z float
}

struct Message {
    crc32  bits(32)
    type   MessageType
    switch type {
    case Ping:     sequence bits(16)
    case Pong:     sequence bits(16)
    case Chat:     text string(MaxChatLength)
    case Snapshot:
        baseSequence bits(16)
        numObjects   int(0, MaxObjects)
        objects      ObjectState[<= MaxObjects] : count(numObjects)
    }
}
```

*(Syntax note: `objects T[<= N]` normally carries its own count on the wire; the
`: count(field)` form back-references an explicitly serialized count instead, for layouts
that need the count readable before the elements. Whether v1 keeps both forms or only the
self-counting one is an open question for review — the self-counting form covers the common
case with less to hold.)*

## 5. Trust model — inherited unchanged

Reads validate everything — integer ranges, enum bounds, alignment padding, constants,
reserved bits, count bounds, string lengths — and fail on any violation, because network input
is the trust boundary. Writes assume trusted data: unchecked in release, asserted in debug,
per each runtime's own conventions (Go's misuse panics, Rust's debug asserts, C++'s asserts).
Generated read code never lets a value that controls iteration go unchecked before use.

## 6. Generated code

### 6.1 What is generated per struct

For each `struct` in the compilation unit, in each target language:

1. **The type itself.** schema owns the types: `struct Vec { float x, y, z; }` in C++, an
   exported Go struct, a Rust struct. Storage types are derived from wire types by fixed
   rules (`int(min,max)` → `int32_t`/`int32`/`i32`; `bits(N≤32)` → `uint32_t`/`uint32`/`u32`,
   else 64-bit; `string(N)` → `char[N]` in C++, `string` in Go, `String` in Rust;
   `T[<= N]` → array + count in C++, slice in Go, `Vec<T>` in Rust — each validated against
   the bound on read).
2. **`Write(buffer, object) -> bytesWritten`** — straight-line write code, wire order, no
   read-side branching.
3. **`Read(buffer, object) -> ok/error`** — straight-line read code with full validation,
   using each runtime's native error idiom (`bool` in C++, `error` in Go, `Result` in Rust).
4. **`Measure(object) -> bits`** — the exact serialized size for this object, following its
   branches and counts.
5. **`MaxBits` / `MaxBytes`** — constants: the longest path through the schema. Size write
   buffers from `MaxBytes`.
6. **`ProtocolId`** — the compilation unit's 64-bit id (§3), one constant per package.

The generated API deliberately mirrors serialize.modern's `schema<...>` members
(`Write`/`Read`/`MeasureBits`/`MaxBytes`), so the two feel like one family.

### 6.2 Code shape — a stated divergence from serialize.modern

serialize.modern's zero-overhead contract is enforced by disassembly: no calls, no loops, one
spliced constant path per branch outcome and per possible array count. That is the right
contract when fighting the template abstraction penalty from inside the consumer's compiler.
schema emits **source**, and its contract is different on purpose:

> **Generated code is the code a careful expert would hand-write against the runtime's
> serialize API: separate read and write functions, sequential field operations, honest loops
> for arrays, honest branches for `if`/`switch`.** Register allocation, unrolling and
> constant-folding are the target compiler's job — it sees straight-line calls into a
> single-header/inline-friendly library with every width and range a literal constant, which
> is precisely the input optimizers are built for.

What this buys: generated source stays small and reviewable, count ranges need no cap, and
three backends stay simple enough to verify against each other. What it forgoes: the
last-mile instruction-level guarantees of the TMP splicer. The v1 performance thesis is that
eliminating the unified-serialize-function branching and the runtime range computations
(constants instead of variables at every call site) captures most of the win; if measurement
against serialize.modern's schema mode on the C++ target shows a gap worth closing, a
bitpacker-level emission mode for fixed-layout structs is the planned v2 lever, behind the
same byte-identity tests.

### 6.3 Per-target notes

| | C++ | Go | Rust |
|---|---|---|---|
| emits against | `serialize_*`-equivalent stream calls or direct `WriteStream`/`ReadStream` methods | `WriteStream`/`ReadStream`/`MeasureStream` concrete types (no interface dispatch) | `WriteStream`/`ReadStream`/`MeasureStream` via the `Stream` trait, monomorphized |
| error idiom | `return false` early-out | sticky stream errors + `return stream.Err()`; generated reads check counts before use | `?` propagation of `serialize::Error` |
| formatting | clang-format-stable output | `go/format` applied by the compiler | `rustfmt`-stable output |
| buffer contract | write buffers multiple of 8 (asserted); read allocations extend ≥8 bytes past packet data (required) | write buffers multiple of 8; ≥7 bytes read slack for the fast path | write buffers multiple of 8; ≥8 bytes read slack for the fast path |

Generated files carry a header naming the compiler version, the source schema files, and the
protocol id — and a do-not-edit line. Output is **deterministic to the byte** for identical
input (sorted iteration everywhere; no map-order or timestamp leaks), so generated code can be
committed and diffed meaningfully.

## 7. The compiler

Go, zero third-party dependencies, one static binary: `schema`.

```
schema check  [dir|files...]          // parse + typecheck; exit code for CI
schema generate --lang cpp,go,rust --out <dir> [dir|files...]
schema id     [dir|files...]          // print the protocol id
schema fmt    [dir|files...]          // canonical formatter (post-v1)
```

### 7.1 Pipeline

```
*.schema → scanner → parser → AST → resolver/checker → IR → {cpp, go, rust} backends
```

- **Scanner/parser: hand-written, recursive descent**, in the style of the Go toolchain's own
  `go/scanner`/`go/parser`. No parser generators. The language is small and LL; hand-rolled
  parsing is what makes precise, human diagnostics cheap.
- **Resolver/checker**: name resolution, constant folding, type and range checks (§4.5),
  back-reference validation via the same flattened-prefix walk serialize.modern proves out at
  compile time.
- **IR — the load-bearing design decision.** The checker lowers to a small, fully-resolved
  intermediate representation: ranges reduced to bit widths and offsets from Min, enums to
  ranges, back-references to field indices, branches and counts explicit, every constant
  folded. **The IR is the single source of truth for wire semantics.** Backends consume only
  the IR and never re-derive a width or a range — a C++/Go/Rust divergence must be written
  into a printer to exist at all. The canonical binary encoding of this IR is also what the
  protocol id hashes (§3, proposed).
- **Backends: dumb printers.** Hand-written emitters (a small indent-aware writer helper), not
  `text/template` — codegen wants precision. Each backend is a single file a reviewer can hold.

### 7.2 Testing

Layered, most of it inherited from how this family already tests:

1. **Golden source tests**: schema in, generated source compared byte-for-byte against
   checked-in goldens, for all three backends. Deterministic output makes this exact.
2. **The wire oracle**: for each conformance schema, a hand-written classic-serialize stream
   twin in C++. Generated C++ must produce byte-identical output to the twin and each must
   decode the other — the same gate serialize.modern runs, against the same oracle.
3. **The cross-language matrix — the whole point**: every writer × every reader
   (C++/Go/Rust, 9 pairs), property-driven random instances, identical bytes and identical
   decoded values, in CI.
4. **Malformed-input agreement**: bit-flip sweeps over valid packets; all three generated
   readers must agree on accept/reject and on the decoded value — the brute-force gate
   serialize.modern pins its read validation with, extended across languages.
5. **Compiler robustness**: Go-native fuzzing on the parser and checker; every diagnostic in
   the test suite asserted by exact message and position.

## 8. Repository layout

```
cmd/schema/            the CLI
internal/scanner/      tokens, positions
internal/parser/       AST construction
internal/ast/
internal/check/        resolver, types, ranges, back-references
internal/ir/           the lowered form + its canonical encoding (protocol id)
internal/codegen/
    cpp/  golang/  rust/
testdata/              golden schemas, golden generated source, diagnostic fixtures
conformance/           the oracle twins + cross-language matrix harness
SPEC.md                this document
```

License: AGPL-3.0 (DECIDED, 2026-08-04). Repo private until Glenn opens it.

## 9. Open questions, gathered

1. **Protocol id: IR hash vs raw file hash** (§3). Recommendation: IR hash. Glenn's call.
2. **`array : count(field)` back-referenced counts** — keep both array forms in v1, or only
   self-counting `T[<= N]`? (§4.6). Recommendation: self-counting only until a real layout
   needs the split; the fused form covers the serialize.modern examples.
3. **Storage-type overrides** — is derived storage (§6.1) enough for v1, or does Space Game
   need e.g. `u8` storage for a ranged int immediately?
4. **Wide strings** — deferred from v1 (§4.3). Confirm nothing near-term needs them.
5. **The `schema fmt` canonicalizer** — post-v1, unless the IR-hash decision makes a
   canonical form load-bearing earlier.

---

*Draft 1, Rowan, 2026-08-04 — from serialize.modern's SCHEMA.md (read whole), the serialize /
serialize.go / serialize.rs API contracts, and Glenn's decisions of tonight. Wire claims in
§4.3 name their classic twins but are not yet pinned by tests; the conformance suite in §7.2
is what converts this document from intent to property.*
