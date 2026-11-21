# Schema for AI assistants

This page is for an AI coding assistant that is using schema inside someone's
game or service. It covers what schema is for, the whole definition language,
the commands, how each generated language is called, and the mistakes that
come up most often. Contributors to the compiler itself start at
[AGENTS.md](../AGENTS.md) and [CONTRIBUTING.md](CONTRIBUTING.md) instead.

Where this page and a specification disagree, the specification is right:
[SPEC.md](SPEC.md) for the language and the packet wire,
[SPEC-TABLES.md](SPEC-TABLES.md) for tables. Every `schema` block on this page
is checked and generated in every live language by
`compiler/aidoc_test.go`, so the examples compile as written.

To give a project's own assistant the short version, run `schema help ai` and
paste its output into that project's `AGENTS.md` or `CLAUDE.md`. Its text is
[cmd/schema/help/ai.md](../cmd/schema/help/ai.md).

## What schema is, and when to use it

You write `.schema` files that declare constants, enums, flags, types, unions
and tables. The compiler generates structs and read/write code in each target
language. Every language writes the same bits for the same values, and every
reader validates its input against the declaration.

Reach for schema for:

- packets between a game client and server (`type`, bit-packed, smallest on
  the wire);
- messages between a game server and a backend that deploy separately
  (`fixed table`, or the message form of a fixed table);
- save games, config files, editor and tool output, anything a later build
  must still read (`table`);
- game assets cooked to a binary the game opens without parsing (`table` plus
  `schema cook`).

Do not reach for schema when:

- you need RPC or service stubs: schema has no service definitions;
- you need zero-copy random access on the packet wire: a `type` read decodes
  the whole value (a cooked `table` is the zero-copy form);
- your packet peers deploy independently and must talk across versions over
  one connection: the packet wire refuses a different protocol id by design.
  Use a `table` (or its message form) for that traffic.

[FAQ.md](FAQ.md) compares schema with Protobuf, FlatBuffers and Cap'n Proto.

## Getting the compiler

The compiler is one Go binary, `schema`. Build or install it as the
[README](../README.md) describes. In this repository, `make` builds it at
`bin/schema`.

## Files, units and generated code

- A **unit** is one directory of `.schema` files compiled together. Every file
  in it starts with the same `package` line. Declarations may be used before
  they are declared, in any file of the unit.
- Generated files are output. Never edit one by hand: change the `.schema`
  file and generate again. Every generated file's header names the schema
  file it came from.
- `schema fmt` is the only command that writes a `.schema` file. Every other
  command only reads them.
- Names: types, enums, variants and constants `UpperCamelCase`; fields
  `lower_snake_case`. Each generator maps them to its own language's style
  (`health` is `Health` in Go and C#). A name that is a reserved word in any
  supported language is refused whatever language you generate (Go escapes
  its own words instead), so a name that works for one target works for all
  of them.
- The generated code exports the unit's protocol id as a constant
  (`ProtocolId` or `PROTOCOL_ID`); `schema id` prints it
  ([SPEC.md](SPEC.md) §3.2).

## The language on one page

A complete unit:

```schema
package game

const MaxHealth = 1000

enum ShipType { Fighter, Corvette, Bomber }

flags ShipFlags { Firing, Thrusting, Disabled }

type Vec3
{
    x float32
    y float32
    z float32
}

type ShipState
{
    ship_type  ShipType
    ship_flags ShipFlags
    position   Vec3
    health     int32 | min = 0, max = MaxHealth
}
```

Layout rules ([SPEC.md](SPEC.md) §4.1): newlines end declarations and fields,
there are no semicolons, `//` and `/* */` are comments, and a field line is
name, type, optional `= default`, then an optional qualification section after
`|` that runs to the end of the line. Braces go on their own line (Allman); a
one-line `{ ... }` list stays on one line. `schema fmt` writes the canonical
layout.

The examples below leave out the `package` line. Each one is a complete unit
once a `package` line is added.

### const

```schema
const MaxPlayers = 64
const MaxSlots   = MaxPlayers * 2
const TickRate float32 = 60.0
const ProbeId uint64   = 0xDEADBEEF
```

Constants are compile-time integer or float expressions. A bare constant takes
its type from its expression; an explicit type pins the type every language
exports. Every constant is exported in every generated language.

### enum

```schema
enum Team { Red, Blue }

const NumTeams = Team.Max
```

Every enum has an implicit `None = 0`; declared variants number from 1, dense,
with no explicit values. `E.Max` is the number of declared variants (here 2).
A variant named `None` or `Max` is refused. Storage and wire width derive from
the variant count; an enum takes no `| max`.

### flags

```schema
flags Perks | max = 8
{ Shielded, Cloaked, Turbo }

const PerkCount = Perks.Count
```

One bit per variant, from bit 0 in declaration order, up to 64. Storage is
`uint64` in every language. `| max = K` reserves K wire bits for later
variants. `F.Count` is the declared count; flags have no `.Max`.

### type

```schema
type Vec2
{
    x float32
    y float32
}

type Transform
{
    position Vec2
    rotation float32
}
```

A `type` is a value: it nests by value, never recursively, and rides the
packet wire bit-packed with no field ids. A `type` body refuses pointers,
maps, unbounded arrays, `?` optionals and `timestamp`; those are table
features.

### Integers, bits and bool

```schema
type Integers
{
    raw    uint16
    health int32 | min = 0, max = 1000
    ammo   uint8 | max = 200
    packed bits(5)
    alive  bool
}
```

A bare `intN`/`uintN` costs N bits. A ranged integer costs only the bits its
range needs (`health` above is 10 bits) and a read refuses a value outside the
range. An unsigned field may give `max` alone, meaning `min = 0`; otherwise
`min` and `max` come together. `bits(N)` is N raw bits, N from 1 to 64.
`bool` is one bit. There is no `int` or `uint`.

### Floats and fixed point

```schema
type Numbers
{
    exact float64
    yaw   float32 | min = -180.0, max = 180.0, resolution = 0.01
    x     fixed(48, 16) | min = -100000, max = 100000
    speed ufixed(16, 16) | min = 0, max = 500
}
```

`float32` and `float64` carry finite values only: a NaN or an infinity is
refused on write and on read. A `float32` with `min`, `max` and `resolution`
(all three) is quantized to the fewest bits for that step. `fixed(I, F)` is
signed Q I.F and `ufixed(I, F)` unsigned, with I+F one of 8, 16, 32, 64 or
128; both require `min` and `max` in whole units and round-trip exactly. Use
fixed point where simulation must be bit-identical across machines. There is
no `float16`; use `bits(16)` and convert in your code.

### 128-bit integers

```schema
type Ids
{
    guid  uint128
    score int128 | min = -1000000000000000000000, max = 1000000000000000000000
}
```

`uint128` is bare only; `int128` requires a range.

### Strings, bytes and arrays

```schema
const MaxItems = 16

type Inventory
{
    name  string(32)
    tag   bytes(8)
    slots [4]uint8
    items [..MaxItems]uint16
    party [1..4]uint32
}
```

`string(N)` is UTF-8 text of at most N bytes with no zero byte; `bytes(N)` is
at most N raw bytes. There is no wide string: `wstring` is refused, and you
convert to UTF-8 at your own boundary. An array bound is a prefix: `[N]T` is
exactly N elements, `[..N]T` is 0 to N, `[A..B]T` is A to B, and the count
rides on the wire. There are no arrays of arrays (wrap the inner array in a
type). Element bounds apply per element:
`[..8]int16 | min = 0, max = 100` would bound each element.

### if / else

```schema
type Body
{
    at_rest bool
    if !at_rest
    {
        speed float32
    }
    else
    {
        sleep_ticks uint16
    }
}
```

`if` branches on an earlier `bool` field of the same or an enclosing block,
optionally negated. Only the taken side is on the wire. There is no `switch`
yet; use a `union` to pick one of several payloads.

### Wire constants, reserved bits and alignment

```schema
type Header
{
    const(0xAB, 8)
    version bits(3)
    reserved(5)
    align
    sequence uint16
}
```

`const(value, bits)` writes a fixed value and a read refuses any other.
`reserved(bits)` writes zeros and a read refuses nonzero. `align` pads with
zero bits to the next byte.

### Defaults

```schema
enum Mode { Idle, Run }

type Spawn
{
    active bool = true
    hp     int32 = 100 | min = 0, max = 100
    mode   Mode = Run
    w      fixed(2, 30) = 1.0 | min = -1, max = 1
}
```

Everything starts at zero (`None` for an enum and a union, empty for strings,
counts at their minimum) unless a field gives `= value`. The default goes
before the `|`. A range that excludes zero needs a default inside it. A
`string(N)` or `bytes(N)` field takes a quoted default (`name string(16) =
"player"`) and a flags field a list of variants (`caps Caps = { Jump }`);
`--lang python` carries those three default kinds on a table and refuses them
on a packet `type` by name today (the python form is a named follow-on). Arrays,
nested types and unions take no field default. On a
table, a field equal to its default is left off the wire and an absent field
reads as its default, so changing a table default changes what stored files
mean.

### union

```schema
type Ping { sequence uint16 }

type Chat { text string(256) }

union Message
{
    ping Ping
    chat Chat
    quit
}
```

A union holds at most one arm. The tag is generated (`MessageType` with
`None = 0`, then one value per arm) and rides in the fewest bits for the arm
count; a zero union is `None`. An arm with no type (`quit`) carries no
payload. In a `type`, an arm with a payload must name a declared `type`; an
arm of a scalar, string, array, table or pointer is legal inside tables
(below). There is no `message` keyword: a message set is a union plus your
own framing ([SPEC.md](SPEC.md) §4.8, §4.11).

### Doc comments and tags

```schema
/// Hull settings a designer tunes.
type Hull | designer_facing
{
    /// Structural health. Zero is destroyed.
    health int32 | ui_slider, min = 0, max = 1000
}
```

`///` lines directly above an item are its doc comment and reach the
generated code and the table descriptors; plain `//` lines never do. A bare
word after `|` is a tag: your own vocabulary, carried into descriptors, and it
changes no byte and no id. A bare word that spells a valued key (`| min`) is
refused. C++ can map a type onto your own math type with
`| cpp_native = Vec3, cpp_include = "vec3.h"`; the mapping applies where
another file of the unit uses the type, so a one-file unit refuses it
([SPEC.md](SPEC.md) §4.2).

### Tables

A `table` body uses the same field grammar as a `type` and adds the features
below. Tables never move the protocol id.

```schema
table ShipConfig
{
    health      float32 = 100.0
    armor_level int32 = 1 | min = 0, max = 10
    name        string(32)
}
```

A plain `table` is the **variable** class: self-describing, fields identified
by the hash of their names, so any build reads any other build's file.
Unknown fields are skipped and counted, absent fields read as their defaults,
and a narrower integer widens on read. Every such event is counted in a read
report; damaged framing refuses the whole load.

```schema
fixed table Physics
{
    mass float32 = 1.0 | min = 0.1, max = 100000.0, resolution = 0.1
    drag float32
}
```

A `fixed table` is a constant-size record: the speed of a `type` with
append-only versioning. Its closure refuses pointers, `*bytes`, `*string`,
maps, unbounded arrays, `if` branches and nested variable tables, each by
name. Only a fixed table has the message form (form byte `2`), a bitpacked
batch for traffic between builds ([SPEC-TABLES.md](SPEC-TABLES.md) §3.3).

Table field features:

```schema
enum ShipType { Fighter, Bomber }

table Settings { volume uint8 }

table Node
{
    value int32
    next  *Node
}

table SaveGame
{
    settings ?Settings
    best     ?int32
    per_type [ShipType]uint16
    scores   map[string(32)]int32
    log      []int32
    head     *Node
    icon     *bytes
    motto    *string
}
```

- `?T`: optional value with a generated presence flag; no pointer, no
  allocation.
- `[E]T`: an enum-keyed array, one slot per declared variant, none for
  `None`.
- `map[K]V`: K is `string(N)` or an integer; V is any field type.
- `[]T`: an unbounded array; the data decides the count.
- `*T`: a pointer to a table, for recursion, sharing and large subtrees, not
  for optionality. `*bytes` and `*string` are a blob at its used size.

Any pointer, map or unbounded array makes the holder a variable table.

```schema
table Login { name string(32) }

table Logout { }

union ToolBody
{
    login  Login
    logout Logout
    count  int32
}

table Batch
{
    bodies [..8]ToolBody
}
```

Inside tables a union arm may be any field type, a table included, and an
array of unions is a batch of messages.

Identity and evolution markers:

```schema
table Profile | was = "Player"
{
    speed     float32 | was = "velocity"
    id        uint64  | required
    opened_at timestamp
}

fixed table Stats
{
    armor   uint8 | deprecated
    shields uint8
}

table Pong | forms = "wire,json"
{
    sequence uint16
}
```

- `| was = "old_name"` keeps a renamed table, field, enum variant or union arm
  reading its old data ([SPEC-TABLES.md](SPEC-TABLES.md) §5). `was` names the
  first wire name forever.
- `| deprecated` retires a fixed-table field in place; its slot stays
  ([SPEC-TABLES.md](SPEC-TABLES.md) §2.10).
- `| forms = "..."` narrows which forms a table carries
  ([SPEC-TABLES.md](SPEC-TABLES.md) §2.12).
- `| required` makes a reader refuse a record missing that field
  (`missing_required`). It is refused on a `type` field, on `?T` and in a
  fixed table ([SPEC-TABLES.md](SPEC-TABLES.md) §2.3).
- `timestamp` is a signed 64-bit count of nanoseconds since the Unix epoch,
  zero at 1970-01-01T00:00:00Z, and RFC 3339 text in JSON. It is a bare table
  field: an array, optional, map value or union arm of timestamps is refused
  ([SPEC.md](SPEC.md) §4.13).

Choosing a class ([SPEC-TABLES.md](SPEC-TABLES.md), "How to choose"): one build
on both ends and speed above all, use `type`; small, every field bounded, and
it must cross builds, use `fixed table`; large, sparse, free-form or
unbounded, use `table`.

## Commands

Run `schema` with no arguments for the full list, and `schema help ai` for the
block to paste into a project. Success is silent; `--verbose` lists what a
command did. Errors always print, one diagnostic per line.

| command | what it does |
|---|---|
| `schema check <dir>` | parse and check the unit; prints nothing on success |
| `schema fmt <dir>` | rewrite files in the one canonical style |
| `schema generate --lang <lang> --out <out> <dir>` | write generated code |
| `schema id <dir>` | print the protocol id (packet wire) |
| `schema projection <dir>` | print the text the protocol id hashes |
| `schema build-version <dir>` | print the build version (cooked tables) |
| `schema tables-baseline <dir>` | print or move the committed tables baseline |
| `schema lock <dir>` | write `schema.lock`, the fixed tables' append-only record |
| `schema pack --root T --out F <tree> <dir>` | a directory tree of JSON becomes one table file |
| `schema unpack --root T --in F <tree> <dir>` | a table file becomes the tree again |
| `schema cook --root T --in F --out C <dir>` | a table file becomes a cooked asset |
| `schema cook-check <file.cook> <dir>` | validate an untrusted cook |
| `schema uncook --root T --in C --out F <dir>` | a cook becomes a table file again |
| `schema testgen --lang go\|lua\|squirrel <dir>` | write round-trip and damage tests for every table |
| `schema pin <version> <dir>...` | move every Go `go:generate` compiler pin |
| `schema version` | print the compiler's build identity |
| `schema help ai` | print the guidance block for a project's AGENTS.md |

`--lang` takes `c`, `cpp`, `cs` (C# (modern): .NET 8+, C# 12), `dart`,
`elixir`, `go`, `goose`, `java`, `js`, `lobster`, `lua`, `luau`, `python`,
`ruby`, `rust`, `squirrel` and `unity` (C# (unity): C# 9 over
netstandard2.1, the surface Unity compiles, [UNITY.md](UNITY.md)). Moo is
generated only by a compiler built with `go build -tags schema_leg_moo`.
`--surface` (Go and Python) narrows the table surfaces emitted and
`--runtime` (Go) picks the shared or standalone table runtime
([CLI.md](CLI.md), [USAGE.md](USAGE.md)).

## Calling the generated code

The shape is the same everywhere: build a value, write it to a buffer, read
bytes back into a value, and treat a failed read as hostile input to drop.
Writes check values against the declaration by the language's own idiom
([SPEC.md](SPEC.md) §5.1): C, C++, C# (modern), C# (unity), Rust, Java and
Dart check in debug builds only (an assert), and so do Lobster's packet
writers unless built with `--runtime-no-asserts`; the other languages refuse
in every build. A read checks everything in every build.

Each snippet below is taken from a test that compiles and runs it, over the
`ShipCreate` type in [examples/Types.schema](../examples/Types.schema) unless
another unit is named. Where the test builds its stream in a helper, the
helper's body is written inline. `check` and its kin are the tests' own
assertion helpers.

**C++** ([test/main.cpp](../test/main.cpp)), against the
[serialize](https://github.com/mas-bandwidth/serialize) runtime:

```cpp
serialize::WriteStream ws( buffer, sizeof( buffer ) );
check( WriteShipCreate( ws, in ) );          // a debug build asserts on a bad value
ws.Flush();
ShipCreate out;
serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
check( ReadShipCreate( rs, out ) );          // false: refused, drop it
```

**C** ([test/c/main.c](../test/c/main.c)), against serialize.c; names are
prefixed with the package:

```c
serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
check( example_write_ship_create( &w, &in ), "write ShipCreate" );
serialize_write_flush( &w );
serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
check( example_read_ship_create( &r, &out ), "read ShipCreate" );
```

**C# (modern)** ([test/cs/src/Program.cs](../test/cs/src/Program.cs)) and
**C# (unity)** ([test/unity/src/Program.cs](../test/unity/src/Program.cs)),
against serialize.cs; types are structs passed by `ref`:

```csharp
WriteStream ws = new WriteStream(new byte[2048]);
Check(WriteShipCreate(ws, ref input), "write ShipCreate");
ws.Flush();
byte[] wire = ws.Data.ToArray();
ShipCreate output = new ShipCreate();
Check(ReadShipCreate(new ReadStream(wire), ref output), "read ShipCreate");
```

**Go** ([test/go/main.go](../test/go/main.go)), against serialize.go; write and
read return an `error`:

```go
ws := serialize.NewWriteStream(buffer)
checkErr(example.WriteShipCreate(ws, &in), "write ShipCreate")
ws.Flush()
out := example.ShipCreate{}
rs := serialize.NewReadStream(ws.Data())
checkErr(example.ReadShipCreate(rs, &out), "read ShipCreate")
```

**Rust** ([test/rust/src/main.rs](../test/rust/src/main.rs)), against
serialize.rs; write and read return a `Result`:

```rust
let mut ws = WriteStream::new(&mut buffer);
check_err(write_ship_create(&mut ws, &input), "write ShipCreate");
ws.flush();
let n = ws.bytes_processed() as usize;
let mut rs = ReadStream::new(&buffer, n);
check_err(read_ship_create(&mut rs, &mut out), "read ShipCreate");
```

**JavaScript** ([test/js/main.mjs](../test/js/main.mjs)), the runtime tier
against serialize.js; the flat tier (`<Base>Flat.js`, no dependency) is in
[USAGE.md](USAGE.md#per-language-notes):

```js
const ws = new WriteStream(new Uint8Array(2048));
check(ex.WriteShipCreate(ws, inp), "write ShipCreate");
ws.flush();
const out = new ex.ShipCreate();
const rs = new ReadStream(ws.data());
check(ex.ReadShipCreate(rs, out), "read ShipCreate");   // false: refused
```

**Java** ([test/java/Main.java](../test/java/Main.java)), self-contained;
size the buffer with `<name>MaxBytes`:

```java
final int n = Types.writeShipCreate(inp, writeBuf);   // asserts under -ea
final byte[] wire = java.util.Arrays.copyOf(writeBuf, n);
check(Types.readShipCreate(out, wire, n * 8), "read ShipCreate no-flags");
```

**Dart** ([test/dart/main.dart](../test/dart/main.dart)), self-contained, on
`ByteData` views:

```dart
final n = writeShipCreate(inp, writeView);            // asserts in debug
final wire = Uint8List.fromList(written(n));
check(readShipCreate(out, ByteData.sublistView(wire), n * 8), 'read ShipCreate no-flags');
```

**Elixir** ([test/elixir/suite.exs](../test/elixir/suite.exs)),
self-contained. `write_<name>!/1` returns the binary or raises
`ArgumentError`; `write_<name>/1` returns `{:ok, binary}` or
`{:error, message}`; a read returns `{:ok, value}` or `:error`:

```elixir
wire = Example.Types.write_ship_create!(inp2)
{:ok, out2} = Example.Types.read_ship_create(wire, byte_size(wire) * 8)
```

**Python** ([USAGE.md](USAGE.md#per-language-notes), run by
`make tables-python-usage`): a read raises `Refused`, a write raises
`InvalidValue`:

```python
data = ship.write()                          # the packed bits, as bytes
back = ShipCreate.read(data)                 # the whole value, or raises Refused
try:
    ShipCreate.read(data[:1])                # a truncated packet
except Refused as refusal:
    print("refused:", refusal.reason)        # past_end
```

**Ruby** ([USAGE.md](USAGE.md#per-language-notes), run by
`make tables-ruby-usage`): the same shape, `Example::Refused` and
`Example::InvalidValue`:

```ruby
data = ship.write                              # the packed bits, as an ASCII-8BIT String
back = Example::ShipCreate.read(data)          # the whole value, or raises Refused
```

**Lua** ([internal/codegen/lua/write_test.go](../internal/codegen/lua/write_test.go),
[read_test.go](../internal/codegen/lua/read_test.go)); Luau is the same
shape with `require("./Types")` and a `buffer`
([internal/codegen/luau/write_test.go](../internal/codegen/luau/write_test.go)):

```lua
local inst = Types.NewShipCreate()
local stream = serialize.WriteStream.new(2048)
assert(Types.WriteShipCreate(stream, inst))
stream:flush()
local got = Types.NewShipCreate()
local ok = Types.ReadShipCreate(serialize.ReadStream.new(stream:data()), got)   -- false: refused
```

**Squirrel** ([internal/codegen/squirrel/squirrel_test.go](../internal/codegen/squirrel/squirrel_test.go)),
over a two-file unit whose type is `B`:

```squirrel
local ws = serialize.WriteStream(blob(64));
if (!O.WriteB(ws, v)) throw "write: " + ws.error();
ws.flush();
local r = O.B();
local rs = serialize.ReadStream(ws.data());
if (!O.ReadB(rs, r)) throw "read: " + rs.error();
```

**Goose** ([test/goose/main.goose](../test/goose/main.goose)): a write returns
a refusal code (`SERIALIZE_ERROR_NONE` on success, named by
`serialize_refusal_name`), a read returns a `bool`:

```goose
var v = example::ShipCreate { .. };
var w = serialize_write_stream_init(wbuf);
ok(example::WriteShipCreate(w, v), "write ShipCreate");
serialize_flush(w);
var out = example::ShipCreate { .. };
var r = serialize_read_stream_init_padded(rbuf, image[0..imageLen]);   // a copy of the written bytes
check(example::ReadShipCreate(r, out), "read ShipCreate");
```

**Lobster** ([internal/codegen/lobster/write_test.go](../internal/codegen/lobster/write_test.go));
a package name that collides with a runtime binding (`p`, for one) is refused
by name, so rename the package:

```lobster
let v = example.NewShipCreate()
let w = serialize_write_stream(ensure_size("", 2048, 0))
assert example.WriteShipCreate(w, v)
w.flush()
```

**Tables.** A table generates its own surface beside the packet code: save and
load, a read report, the JSON text form, and the cook and block forms. In Go
([USAGE.md](USAGE.md#per-language-notes), run by `make tables-go-usage`
against the `tables/pointers` unit):

```go
size, err := graphdemo.SceneMeasureReason(source.GetRoot(), &source.Arena)
if err != nil { panic(err) }
wire := make([]byte, size)
if graphdemo.SceneSave(source.GetRoot(), wire, &source.Arena) != size {
    panic("save failed")
}
strict, err := graphdemo.SceneReadStrict(wire)   // err names the refusal
```

The table call in every language, with compiled examples, is in
[USAGE.md, Per-language notes](USAGE.md#per-language-notes).

## The rules that trip newcomers

- **Any edit to a `type` moves the protocol id**, and so does an edit to an
  enum or union a `type` uses, any `flags` declaration, and even an unused
  helper `type`. Peers on different ids refuse each other: client and server
  deploy together. Renaming an enum variant or union arm moves it too,
  because variant order is the wire. A comment, a `///` doc, a tag or a
  renamed `const` does not ([FAQ.md](FAQ.md), [VERSIONING.md](VERSIONING.md)).
- **Data that outlives a build belongs in a table.** A replay or save stored
  as packets dies at the first `type` edit.
- **Reads refuse; they never clamp on the packet wire.** An out-of-range
  integer, a count or length past its bound, an enum value that is not a
  variant, a union tag past its arms, ill-formed UTF-8, a NaN, nonzero
  reserved or padding bits, leftover bytes after the value: each fails the
  read in every language and every build ([SPEC.md](SPEC.md) §5).
- **Writes are your responsibility.** In C, C++, C#, Rust, Java and Dart the
  write checks are debug asserts only; a release build writes what you give
  it and the reader on the other end refuses it ([SPEC.md](SPEC.md) §5.1).
  A bare `intN` value outside N bits is not caught by any reader: only the
  writer's check guards it.
- **After a refused read, the value is unspecified**: use it only on success.
  A successful read zeroes fields in branches that were not taken.
- **One read is one packet.** The packet read checks the end of the buffer;
  frame several values with a containing `type` or an array, not repeated
  reads of one buffer.
- **Enums count from 1.** `None = 0` is implicit, so the number of real
  variants is `E.Max`, and an enum-sized array is `[E.Max]T`.
- **`flags` are positional on the table wire.** Append new variants at the
  end; inserting or reordering silently remaps stored bits. Removing one
  frees no bit.
- **A bare rename of a table field orphans its data.** Use `| was`.
- **Changing a table default changes what stored files mean**, because a
  field equal to its default is not written.
- **No recursion in a `type`.** `type Node { children [..4]Node }` is a
  composition cycle. Use a table with `*Node`.
- **No `?T`, maps or unbounded arrays in a `type`.** Use a `bool` and an
  `if`, a `union` whose `None` is the absence, or a counted array of pairs.
- **`message`, `object`, `wstring`, `int`, `uint`, `switch`, `| round` and
  `| doc` are refused by name**, each with a diagnostic naming the spelling
  that works ([SPEC.md](SPEC.md) §4.11, §4.12).
- **A diagnostic says what the input wants.** Do what it says. If it is
  unclear, that is a bug in the diagnostic, not a reason to work around it.

## Changing a schema without breaking old data

From [VERSIONING.md](VERSIONING.md), which is the full statement:

- **Packet wire (`type`).** There is no evolution. Change it and redeploy
  both ends together. Check with `schema id` before and after.
- **Variable `table`.** Add a field anywhere; remove one; reorder freely.
  Rename only with `| was = "old_name"`, and keep the first name forever.
  Widen an integer (same signedness) or `float32` to `float64`: new builds
  read old files exactly, old builds refuse the new field as `kind_mismatch`,
  so flip every reader first or add a new field beside the old one. Do not
  re-add a removed name with a new meaning. Do not change a default in
  place; add a new field.
- **`fixed table`.** Append new fields at the bottom only. Retire a field with
  `| deprecated`; it keeps its slot. Never reorder or remove. To compact,
  declare a new table under a new name. `schema lock` records the layout and
  `schema check` refuses an edit that breaks it.
- **Enums and unions in tables** ride by name: insert a variant anywhere; a
  name an old reader does not know reads as `None` and is counted. Rename
  with `was`.
- **Flags:** append at the end only.
- **Commit a tables baseline** (`schema tables-baseline --update --reason
  "..."`) before the first build whose data leaves the building. It refuses
  the edits the wire cannot report, such as inserting or reordering a flags
  variant, and warns on a bare rename ([SPEC-TABLES.md](SPEC-TABLES.md) §4.1,
  §18).

## Where the normative answer lives

| topic | page |
|---|---|
| grammar, field types, packet wire, protocol id | [SPEC.md](SPEC.md) §4, §3 |
| reads and the write contract per language | [SPEC.md](SPEC.md) §5, §5.1 |
| tables: declarations, classes, pointers, maps | [SPEC-TABLES.md](SPEC-TABLES.md) §2 |
| table wire, message form, fixed form | [SPEC-TABLES.md](SPEC-TABLES.md) §3 |
| table versioning and the read report | [SPEC-TABLES.md](SPEC-TABLES.md) §4, [VERSIONING.md](VERSIONING.md) |
| cooked assets | [SPEC-TABLES.md](SPEC-TABLES.md) §7 |
| JSON text form | [SPEC-TABLES.md](SPEC-TABLES.md) §16 |
| a guided walk through every feature | [TUTORIAL.md](TUTORIAL.md) |
| every feature with its generated code, per-language notes | [USAGE.md](USAGE.md) |
| commands | [CLI.md](CLI.md) and `schema` with no arguments |
| C# (unity) | [UNITY.md](UNITY.md) |
| comparisons, limits, honest answers | [FAQ.md](FAQ.md), [COMPARISON.md](COMPARISON.md) |
| what each language carries today | [ROADMAP.md](../ROADMAP.md) |
