# Using schema

Every feature of the language, with the code it generates.

The generated code in this guide is real compiler output, not an
approximation — if it looks surprising, it is because that is what the
compiler does.

- [The five minute version](#the-five-minute-version)
- [Declarations](#declarations)
  - [const](#const) · [enum](#enum) · [flags](#flags) · [type](#type) ·
    [message](#message) · [table](#table) · [object](#object)
- [Field types](#field-types)
  - [Integers](#integers) · [Ranged integers](#ranged-integers) ·
    [bits(N)](#bitsn) · [bool](#bool) · [Floats](#floats) ·
    [Compressed floats](#compressed-floats) · [fixed(I, F)](#fixedi-f) · [ufixed(I, F)](#ufixedi-f) ·
    [Strings and bytes](#strings-and-bytes) · [Arrays](#arrays) ·
    [Composition](#composition)
- [Branches: if / else](#branches-if--else)
- [Defaults](#defaults)
- [The two wires](#the-two-wires)
- [Reading untrusted data](#reading-untrusted-data)
- [Packing JSON into binary](#packing-json-into-binary)
- [The protocol id](#the-protocol-id)
- [Embedding the compiler](#embedding-the-compiler)
- [Per-language notes](#per-language-notes)

---

## The five minute version

Write a `.schema` file:

```
package tour

const MaxHealth = 1000

enum ShipType { Fighter, Corvette, Bomber }

type Vector3 {
    x float64
    y float64
    z float64
}

message ShipCreate {
    ship_type ShipType
    position  Vector3
    health    int32 [min = 0, max = MaxHealth]
}
```

Generate:

```
schema generate --lang cpp --out generated/cpp .
```

You get a struct and a pair of wire functions:

```cpp
struct ShipCreate {
    ShipType ship_type = ShipType::None;
    Vector3 position;
    int32_t health = 0; // wire [0, 1000]
};

inline bool WriteShipCreate( serialize::WriteStream & stream, const ShipCreate & value );
inline bool ReadShipCreate( serialize::ReadStream & stream, ShipCreate & value );
```

Run the same command with `--lang cs`, `--lang go`, `--lang js` or
`--lang rust` and you get the equivalent in that language — writing the
**same bits**.

---

## Declarations

### const

```
const MaxHealth = 1000
const Pi float64 = 3.14159265359
```

Constants are exported into every generated language, so the bound you
declared is the bound your code compares against — no second copy to drift.
They may reference each other in any order, across files.

An untyped constant takes the type its use needs. Name a type explicitly
when you care.

### enum

```
enum ShipType { Fighter, Corvette, Bomber }
```

**Every enum has an implicit `None = 0`.** Variants pack from 1. A
zero-initialized enum field is therefore the null, in band, and you never
need a separate has-flag beside it:

```cpp
enum class ShipType : uint8_t { None = 0, Fighter = 1, Corvette = 2, Bomber = 3 };
```

On the wire it costs `bitsRequired(variant count)` — 2 bits here, for four
values. Declaring `enum E [max = 15] { ... }` reserves headroom so you can
add variants later without moving the field width.

### flags

```
flags Capabilities { Cloak, Shield, Warp }
```

One bit per variant, consumed as a mask. Storage is `uint64` in every
language; the wire is exactly as many bits as there are variants:

```cpp
using Capabilities = uint64_t;
inline constexpr Capabilities Capabilities_Cloak  = 1ull << 0;
inline constexpr Capabilities Capabilities_Shield = 1ull << 1;
inline constexpr Capabilities Capabilities_Warp   = 1ull << 2;
```

### type

A plain struct. Composes into messages, tables and other types:

```
type Vector3 {
    x float64
    y float64
    z float64
}
```

### message

A message is a type that also joins the unit's **dispatch surface**: the
compiler emits a `MessageType` enum, tag read/write functions, and
`WriteMessage`/`ReadMessage` that dispatch on it. That is how you get a
single entry point for a packet stream.

### table

Declaring `table` instead of `type` puts a type on the **table wire** — the
evolution-tolerant encoding for data that outlives builds. See
[The two wires](#the-two-wires).

### object

An object declares state that is replicated in more than one form — a full
state, a deep wire, a lightweight shallow wire, and an interpolation view.
Fields carry `[interpolate]` and `[local]` markers to say which views they
belong to. This is the delta/snapshot machinery; if you are not writing a
replication layer you will not need it.

---

## Field types

### Integers

`int8` `int16` `int32` `int64` `int128` and their `uint` counterparts. Full
width on the wire unless you give a range.

### Ranged integers

```
health int32 [min = 0, max = MaxHealth]
```

The range is **part of the type**, and the wire cost follows from it. This
is the single most useful feature in the language: 1001 values need 10 bits,
so that is what it writes.

```cpp
serialize_assert( value.health >= 0 && value.health <= MaxHealth );
write_bits( stream, uint32_t( value.health ), 10 );
```

A read outside the range **fails** — see
[Reading untrusted data](#reading-untrusted-data).

### bits(N)

```
tag bits(12)
```

An unsigned field of exactly N bits, when you want to say the width
directly.

### bool

One bit.

### Floats

`float32` and `float64`, written whole (32 and 64 bits).

### Compressed floats

```
throttle float32 [min = 0, max = 1, resolution = 0.01]
```

A float quantized onto a grid: the wire carries the step index, not the
float. 101 steps here, so 7 bits instead of 32. Add `round = up` / `down` /
`nearest` to control the write rounding.

### fixed(I, F)

```
position fixed(48, 16) [min = -30000, max = 30000]
```

Signed fixed point: `I` integer bits (the sign bit counts), `F` fractional
bits, stored as a raw scaled integer of exactly `I + F` bits. Bounds are in
**whole units**.

Fixed point is what you use when a value must be **bit-identical across
machines**. Floating point is not: the same expression can differ by an ulp
between compilers and architectures, which is fatal for lockstep simulation
or any determinism-dependent design. A fixed value is an integer — it adds
exactly, everywhere.

A fixed component is also **its own quantization**: storage and wire share
one integer domain, so there is no separate quantize step to keep in sync.

### ufixed(I, F)

```
distance ufixed(48, 16) [min = 0, max = 60000]
```

The unsigned sibling: no sign bit, whole-unit domain `[0, 2^I)`, same `F`
semantics, stored as an **unsigned** integer of exactly `I + F` bits. Use it
when the value cannot be negative and you want the storage type to say so —
unsigned Q8.8 reaches 255 whole units where signed Q8.8 tops out at 127.
Everything else — required whole-unit bounds, exact round trips, defaults,
degenerate ranges — works exactly as for `fixed`.

### Strings and bytes

```
name string(32)
blob bytes(1024)
```

Both are length-prefixed with a maximum. Storage carries the buffer and the
used length beside it:

```cpp
char name[MaxName + 1] = {};
int32_t name_length = 0;
```

The write refuses embedded NULs and any length past the maximum; the read
validates both.

### Arrays

```
fixed_size   [4]Vector3
counted      [<= 16]uint32
ranged_count [2..8]uint32
```

A fixed array always writes N elements. A counted array writes the count
first, in the fewest bits that can express the bound, then that many
elements.

### Composition

Types nest freely. A composed field writes its parts inline — no pointers,
no offsets, no indirection:

```cpp
if ( !WriteVector3( stream, value.position ) )
{
    return false;
}
```

---

## Branches: if / else

```
message ShipCreate {
    at_rest bool
    if !at_rest {
        velocity Vector3
    }
}
```

The condition must be a `bool` field **already written** — the reader has to
know it before it can decide what comes next. Storage holds both sides; the
wire carries only the taken one:

```cpp
write_bool( stream, value.at_rest );
if ( !value.at_rest )
{
    if ( !WriteVector3( stream, value.velocity ) )
    {
        return false;
    }
}
```

**A read zeroes the untaken side.** You never inherit a stale value from a
previous packet through a branch that was not taken this time.

---

## Defaults

```
health int32 [min = 0, max = 1000] = 100
w      fixed(2, 30) [min = -1, max = 1] = 1.0
```

Everything zero-initializes unless a default says otherwise. Defaults are
for values whose *rest state is not zero* — a quaternion's `w`, a health
that starts full.

A fixed default must be **exactly representable** in its format; the
compiler refuses one that would silently round.

---

## The two wires

**The message wire** is bit-packed and decided at compile time. Nothing on
it identifies fields — both sides know the layout because they were
generated from the same schema. That is what makes it small and fast, and
why versioning is by [protocol id](#the-protocol-id).

**The table wire** is for data that outlives builds — config, assets, saved
settings. Fields are identified by name hash, defaults elide, unknown fields
skip, removed fields default, changed types skip rather than misdecode, and
out-of-range values clamp. Every such event is counted in a report you can
inspect. Add or remove a property and older readers keep working.

Declaring `table` puts a type and everything it references on that wire, and
generates:

- **Codecs** — `TableWriteX` / `TableReadX`.
- **Reflection** — `TableTypeX()` field descriptors: names, wire ids,
  bounds, enum value names. Flat arrays, no per-instance weight, no RTTI, no
  `System.Reflection`, and no schema files shipped at runtime.

Table storage is **relocatable by construction** — trivially copyable,
standard layout, no pointers — so instances can be memcpy'd, memory-mapped,
shared between processes, or built in parallel across threads and gathered
by concatenation.

Use the message wire for gameplay; use tables for anything you will still be
reading after three more builds ship.

---

## Reading untrusted data

Generated readers are written on the assumption that the bytes are hostile.
A value outside its declared range is **refused, not clamped** — the read
returns failure and you drop the packet:

```cpp
// NOTE the buffer contract: in C++ and C the ALLOCATION must extend at least
// 8 bytes past the packet data. The reader pulls a 64-bit word at a time, so a
// buffer sized exactly to the packet is read past its end — on every packet,
// not just malformed ones. Size the receive buffer with slack; pass the true
// length.
uint8_t buffer[MaxPacketBytes + 8];
int bytes = recv( ... );

ShipCreate value;
serialize::ReadStream stream( buffer, bytes );
if ( !ReadShipCreate( stream, value ) )
{
    // malformed or hostile — drop it
}
```

The slack requirement differs per language: **C++ ≥8 bytes, C ≥8 bytes
(serialize.c adopted C++'s align-up contract — its PR #21), Go ≥7, Rust ≥8,
C# none.** The C++/Go/Rust/C# columns are normative in [SPEC.md](SPEC.md)
§6.3; §6.3 predates the C target, so C's contract is serialize.c's own. Write
buffers are a multiple of 8 in every language.

That covers ranges, counts past an array bound, string lengths past their
maximum, enum values outside the declared range, and reads that run past the
end of the buffer.

One precision worth having: an enum read is bounded by the enum's declared
**max**, not by its variant count. For a plain `enum E { A, B, C }` those are
the same thing and a non-variant cannot survive a read. But `enum E [max = 15]
{ A, B }` deliberately reserves headroom so variants can be added later without
moving the field width — and a read of that enum accepts anything in `[0, 15]`.
That is the point of the headroom, but it means a value you have not defined
yet can arrive, and your `switch` should have a default. The same VALIDATION rules hold in all six languages, because
the same compiler wrote all six — the buffer-slack contract above is the one
thing that differs per language.

The one deliberate exception is the table wire, where out-of-range values
*clamp* and are counted in the report — because table data is meant to
survive schema drift, and refusing to load a config over one stale field
would be worse than clamping it.

---

## Writes are the caller's responsibility

**The validation guarantee is on reads.** That is where hostile input arrives,
and it holds in every language.

**Each language uses its own correctness idiom on the write side.** C++ has
`assert`/`NDEBUG`, a check that disappears in a release build, so the C++
backend uses `serialize_assert`. Go has no assert idiom, so it returns
`ErrValueOutOfRange`. Rust and C# likewise return an error rather than invent
one. The rule is that a language should verify correctness the way that
language verifies correctness — not that every target behaves identically here.

So writing `health = 2000` into a field declared `[min = 0, max = 1000]`
asserts in a C++ debug build, silently writes the truncated low bits in a C++
release build, and returns an error in the other three.

Do not build on any of it. **Keep your values inside their declared bounds on
the write side** — your simulation already knows they are, and that is the only
assumption the wire makes. The C++ behaviour in particular is the right one for
a game shipping at 60 Hz, where re-checking every field of every packet in
release is a cost with no buyer.

## Packing JSON into binary

`schema pack` compiles directories of JSON into a single binary container,
per a manifest:

```
schema pack PackManifest.json
```

The manifest names the output and the collections that fill it — a single
JSON file for a singleton, or a directory keyed by an enum:

```json
{
  "unit": ".",
  "outputs": [
    {
      "file": "Config.bin",
      "collections": [
        { "type": "GlobalConfig", "file": "Config/Global.json" },
        { "type": "TeamConfig", "dir": "Config/Teams",
          "key_enum": "Team", "key_field": "team" }
      ]
    }
  ]
}
```

Packing validates as it goes: values are checked against the schema's
ranges, a keyed collection missing a variant's file is an error, and so is a
stray file that matches no variant — which catches both a forgotten config
and a stale one.

The result is one file with a schema hash stamped in it, read through the
generated table codecs, in any of the five table-wire languages (C, C++, C#,
Go and Rust — JavaScript has no table backend yet).

---

## The protocol id

The compiler derives a **protocol id** from the schema and exports it. Two
peers at the same id speak identical bits; two peers at different ids should
not talk to each other at all. Check it during your handshake.

There is no version tag on the wire — that is the point. The id is how you
find out, once, at connect time, instead of paying for it on every packet.

---

## Embedding the compiler

The compiler is a Go library as well as a binary. `cmd/schema` is a client of
that library and holds nothing else — the flags, the printing, the exit codes —
so anything the CLI does, your program can do.

```go
import (
	"github.com/mas-bandwidth/schema/compiler"
	"github.com/mas-bandwidth/schema/ir"
)

c := compiler.New() // the six built-in targets, registered

paths, err := compiler.GatherPaths([]string{"schemas"}) // one directory is one unit
unit, err := c.Load(paths)                              // format-free: nothing is written

fmt.Printf("package %s, protocol id 0x%016x\n", unit.Package, unit.ProtocolId)

files, err := c.Generate(unit, "cpp", compiler.Options{"cpp-message": "variant"})
for name, data := range files {
	os.WriteFile(filepath.Join(outDir, name), data, 0o644)
}
```

`Generate` writes nothing: it returns the emitted files keyed by name, so the
bytes can go to a build directory, an archive, or straight into a comparison.
`Options` carries the per-target settings the CLI spells as flags —
`"cpp-message"` is the only one the built-in targets read today.

A unit that does not compile comes back as `compiler.Diagnostics`, the whole
error list rather than the first one:

```go
unit, err := c.Load(paths)
if diags, ok := errors.AsType[compiler.Diagnostics](err); ok {
	for _, e := range diags {
		fmt.Fprintln(os.Stderr, e)
	}
}
```

The CLI formats every file in place before reading it (schemafmt, one style, no
options). That is a policy, not a law, and it is two fields:

```go
c.FormatInPlace = true
c.OnFormat = func(path string) { fmt.Printf("formatted %s\n", path) }
```

For formatting on its own there is `compiler.FormatFile(path)` — canonicalize
in place, reporting whether the file actually changed — and `compiler.Format(path,
src)`, which formats bytes and touches nothing, for a drift check that must not
repair what it is measuring.

### Your own generator

A generator is an interface, and the built-in backends have no private path in:
they are registered exactly the way yours is.

```go
// Markdown docs for every message in the unit.
type docs struct{}

func (docs) Names() []string { return []string{"docs"} } // `--lang docs`

func (docs) Generate(u *ir.Unit, _ compiler.Options) (map[string][]byte, error) {
	var b strings.Builder
	for _, name := range u.Messages {
		st := u.Structs[name]
		fmt.Fprintf(&b, "## %s — %d bits max\n", name, ir.MaxBitsStruct(st))
		for _, f := range st.Fields {
			fmt.Fprintf(&b, "- `%s` (%d bits)\n", f.Name, ir.MaxBitsField(f))
		}
	}
	return map[string][]byte{"messages.md": []byte(b.String())}, nil
}

c := compiler.New()
if err := c.Register(docs{}); err != nil { // refuses a name a target already holds
	return err
}
fmt.Println(c.Targets()) // [c cpp cs docs go js rust]
files, err := c.Generate(unit, "docs", nil)
```

The unit your generator receives is the same fully-resolved [`ir`](https://pkg.go.dev/github.com/mas-bandwidth/schema/ir)
the built-in backends read: types resolved, constants folded, ranges and bit
widths derived, wire order fixed. The derived parameters the six emitters
share are functions there rather than per-backend arithmetic — `ir.BitsRequired`,
`ir.MaxBitsStruct`, `ir.MaxBytes`, `ir.CompressedFloatParams`,
`ir.AlignedFixedByteArrays`, `ir.FieldId` — so a new target computes the same
widths as the old ones by construction, not by care.

### The data compiler

`schema pack` is a driver call too, and like `Generate` it hands back bytes:

```go
unit, outputs, err := c.Pack("PackManifest.json")
for _, o := range outputs {
	fmt.Printf("%s: %d bytes, content hash 0x%016x\n", o.File, len(o.Bytes), o.ContentHash)
	os.WriteFile(o.File, o.Bytes, 0o644)
}
```

`compiler.Version()` and `compiler.UserAgent()` report which build of the
compiler is running, for a tool that stamps its own output.

The exported surface of `compiler` and `ir` is under semver from the release
that first carries it; everything under `internal/` is not
([VERSIONING.md](VERSIONING.md)).

---

## Per-language notes

**C++** — header-only generated output, targeting
[serialize](https://github.com/mas-bandwidth/serialize). A type can map to
your own math type with `[cpp_native = Vector3, cpp_include = "vec.h"]`, so
simulation code does math directly on generated storage.

**C#** — C# 9 / netstandard2.1-clean, so it runs on Unity-class runtimes.
Reads scalars without boxing; `AppendTableX` writers reuse a buffer.

**Go** — `AppendTableX` append-form writers over a reused buffer; accessors
avoid allocation.

**Rust** — no `unsafe` in generated code, `Result`-returning read and write.
Table-wire parity since v1.5.0: codecs are `table_write_x` / `table_read_x`,
following Rust naming.

**JavaScript** — ES modules, zero dependencies, Number storage for widths of
32 bits or fewer, BigInt for 64 and 128. Two codecs are generated over the
same classes, selected per call site by import path:

The **flat tier** (`WireFlat.js`, emitted beside every `Wire.js`) is the
production packet path — the fastest correct implementation, which is the one
we use. The serialize.js bitpacker is inlined at every field with constant
widths and masks, zero function calls; it imports nothing, runs on caller-owned
buffers, and measures 8–10x the runtime tier (within a few x of native C):

```js
import { ShipCreate } from "./Wire.js";
import { WriteShipCreateFlat, ReadShipCreateFlat, FLAT_READ_SLACK } from "./WireFlat.js";
import { ShipCreateMaxBytes } from "./Wire.js";

const buf = new Uint8Array(ShipCreateMaxBytes);        // write: MaxBytes suffices
const view = new DataView(buf.buffer);
const bytes = WriteShipCreateFlat(ship, view);          // bytes written, or -1 (checked refusal)

const rbuf = new Uint8Array(bytes + FLAT_READ_SLACK);   // read: 8 bytes of slack past the payload
rbuf.set(buf.subarray(0, bytes));                       // (copy exactly-sized receive buffers in)
const ok = ReadShipCreateFlat(out, new DataView(rbuf.buffer), bytes * 8);
```

Reads validate in every mode; writes fork checked/production once at module
load on `NODE_ENV` (bundlers tree-shake the checked writer out of production
bundles). Messages also get batch entries (`Write<Name>FlatArray` /
`Read<Name>FlatArray` — back-to-back packets, the whole loop in one call) and
the flat dispatch pair `WriteMessageFlat` / `ReadMessageFlat`.

The **runtime tier** (`Wire.js` + [serialize.js](https://github.com/mas-bandwidth/serialize.js)
streams) is the diagnostic and reference surface: sticky latched errors that
say which operation failed and why, `MeasureStream`, per-op checked
granularity, and the only tier for object views. Both tiers emit identical
bytes for identical values — a standing CI gate — so the debugging story is
one import away: re-read a failing buffer through the runtime tier and read
`stream.error`. Message wire only: the table wire has no JavaScript backend
yet. Everything measured so far is node/V8; the flat modules are pure spec'd
ECMAScript with no node APIs, but browser-engine gates and numbers are still
owed before browser claims.

All six are generated from the same IR and compared against each other in CI
on every push. The wire is bit-packed, so the property being checked is
bit-identity — if they ever disagree by one bit, the build fails.

---

For the exact rules — grammar, wire law, every edge case — see
[SPEC.md](SPEC.md).
