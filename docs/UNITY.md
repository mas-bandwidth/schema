# Unity C#: `--lang unity`

Schema has two C# generators, and they are two generators, not one with a
switch (Glenn, 2026-10-27: "I don't want a half-assed compromise, I'm talking
TWO generators"):

- **`--lang cs`** is the best modern C#: .NET 8 and later, C# 12, structs with
  `[InlineArray]` buffers, taken by `ref`.
- **`--lang unity`** is the best Unity C#, and it is the one most C# users of
  schema will use. It targets what every Unity from 2021.2 through Unity 6
  compiles: **C# 9 over the netstandard2.1 API surface**, under IL2CPP and AOT,
  with Burst in the picture.

The two share the IR, the name mapping (`ir.GoExportName`, so the checker's
collision registry covers both) and the wire, byte for byte. They share no
emitter: `internal/codegen/unity` is its own package, and it emits packet types
today. The Unity table backend is a named follow-on, and `--lang unity` refuses
a unit that declares tables, by name.

## What a Unity project already does

The design follows the packages Unity ships for the same job.

- **Netcode for GameObjects** serializes through `FastBufferWriter` and
  `FastBufferReader`, structs over unmanaged memory, and its own types implement
  `INetworkSerializable` as structs. Its guidance for traffic sent every tick is
  to keep message types unmanaged and allocation-free.
- **Unity Transport** writes through `DataStreamWriter`, a struct over a
  `NativeArray<byte>` that Burst compiles, bit-packing included.
- **Unity.Collections** carries text and small arrays inline:
  `FixedString32Bytes` is UTF-8 bytes and a length inside the struct, and
  `FixedList32Bytes<T>` is a count and elements inside the struct. Both exist so
  a value can live in a `NativeArray<T>`, an `IComponentData` or a Burst job.
- **Burst** compiles only unmanaged code: structs whose closure holds no
  reference, no class, no managed array, no `string`.
- **IL2CPP** compiles ahead of time: no `Reflection.Emit`, no runtime code
  generation, generic virtual methods over value types are a known source of
  missing instantiations, and managed code stripping removes what nothing
  references statically.
- **Assembly definitions** (`.asmdef`) are how a Unity project draws assembly
  boundaries, and "Allow 'unsafe' Code" is a per-assembly opt-in many teams keep
  off.

Generated code cannot reference Unity.Collections or Netcode: it has to build
outside the editor, and schema's CI runs no Unity. So the generator takes their
*shape* — unmanaged structs with inline fixed-capacity storage — in plain C#.

## Storage, per field kind

Every packet `type` and `union` is a `[Serializable] public struct`. Its closure
is unmanaged, so every generated type satisfies `where T : unmanaged`: it can
sit in a `NativeArray<T>`, in an ECS component and in a Burst job, and
`default(T)` costs nothing and allocates nothing.

| schema | Unity C# storage | why |
|---|---|---|
| `int8`..`int64`, `uint8`..`uint64` | `sbyte`..`long`, `byte`..`ulong` | SPEC §6.1's C# column |
| `int128` / `uint128`, 128-bit `fixed` | `Int128Value` / `UInt128Value` | serialize.cs's emulated pair: two `ulong`s, unmanaged, on every TFM |
| `fixed(I, F)` / `ufixed(I, F)` | the raw scaled integer of I+F bits | the family's fixed storage convention |
| `bits(N)` | `uint` (N ≤ 32) / `ulong` | |
| `bool`, `float32`, `float64` | `bool`, `float`, `double` | |
| `enum E` | `enum E : byte`..`ulong` | open over its backing, so `\| max` headroom is representable |
| `flags E` | `[Flags] enum E : ulong` | the full width SPEC §4.2 requires |
| `string(N)` | `Buffer<N><byte>` + `int <Name>Length` | UTF-8 inline, the `FixedString` shape |
| `wstring(N)` | `Buffer<N><char>` + `int <Name>Length` | UTF-16 code units inline |
| `bytes(N)` | `Buffer<N><byte>` + `int <Name>Length` | |
| `[N]T` | `Buffer<N><T>` | inline, the `FixedList` shape |
| `[A..N]T` | `Buffer<N><T>` + `int <Name>Count` | count born at A |
| a named type | the struct, by value | |
| a `union` | a struct: `Type` tag + one field per arm | arms are not overlapped; see below |

### Buffers

C# 9 has no `[InlineArray]` (that is C# 12, and it is what `--lang cs` uses).
The generator emits, once per unit in the file that carries the protocol id, one
generic struct per capacity the unit uses:

```csharp
[Serializable]
[StructLayout(LayoutKind.Sequential)]
public struct Buffer8<T> where T : unmanaged
{
    public const int Capacity = 8;

    public T E0, E1, E2, E3, E4, E5, E6, E7;

    public T this[int index] { get; set; }   // bounds-checked, by value
}

public static class Buffers
{
    public static Span<T> AsSpan<T>(ref this Buffer8<T> buffer) where T : unmanaged;
}
```

`buffer[i]` reads and writes an element by value; `buffer.AsSpan()[i]` is the
element by `ref`, which is how a struct element is edited in place
(`value.Inputs.AsSpan()[3].Fire = true`). The view is an extension on
`ref this` because C# 9 will not let a struct member return a reference into
the struct's own storage. Both are bounds-checked: an index past the capacity
throws `IndexOutOfRangeException`, never touches the next field.

**The layout is what makes the view legal**: N fields of one unmanaged `T`,
declared in index order, under `LayoutKind.Sequential`, so element i lives at
`i * sizeof(T)` from `E0`, and `MemoryMarshal.CreateSpan(ref E0, N)` spans
exactly the N fields. `test/unity` holds that on every run for the element kinds
the corpus stores (a byte, a struct carrying `bool`s, a union, a `ulong`).
A sequential struct of one field type is laid out in declaration order; the
test asserts it on CoreCLR, and the same assertion on Mono and IL2CPP is owed
(see "What is not verified").

**Why not `fixed` buffers.** `fixed byte name[N]` is the other inline form C# 9
has, and Unity's own code uses it. It was declined: it needs `unsafe` in every
consuming assembly ("Allow 'unsafe' Code" on the asmdef), it holds only
primitive elements, so arrays of structs would still need the form above, and
two mechanisms for one concept is the compromise this generator exists to avoid.
The generated code contains no `unsafe` at all, and the consuming asmdef does
not need the option.

**Why not `T[]` and `string`.** A managed array or string per field is a heap
object per value, a GC cost per message, and a type Burst refuses and a
`NativeArray<T>` cannot hold. SPEC §6.1's storage principle ("nothing is
dynamically sized") and Unity's own guidance agree.

### Unions

A union is a struct holding the `Type` tag and one field per arm, side by side.
The arms are not overlapped with `[StructLayout(LayoutKind.Explicit)]`. Overlap
would save memory, but explicit layout over arms that hold `bool`s and generic
buffer instantiations is exactly the layout question that differs between
CoreCLR, Mono and IL2CPP, and none of the three is checked against it here; a
sequential struct is the one layout every runtime agrees on. Overlap is a
measured size optimisation for later, not a default. A read initializes the
selected arm from its construction form; the other arms are unspecified
(SPEC §4.8).

## The API

The family's API (SPEC §6.1, §6.3 C# row), on `public static partial class
Schema` in `namespace <Package>`:

```csharp
public static bool WriteShipCreate(WriteStream stream, ref ShipCreate value);
public static bool ReadShipCreate(ReadStream stream, ref ShipCreate value);
public const int ShipCreateMaxBits = ...;
public const int ShipCreateMaxBytes = ...;   // size write buffers from this
public static void ZeroShipCreate(ref ShipCreate value);   // value = default
```

- **Static functions, value by `ref`.** No copy of a large struct, no boxing, no
  interface dispatch, no virtual call: IL2CPP emits a direct call, and nothing is
  generic, so there is no instantiation for AOT to miss and nothing reflection
  reaches, so stripping cannot remove what the code calls. No `link.xml` and no
  `[Preserve]` are needed.
- **Against serialize.cs's sealed `WriteStream` / `ReadStream`**, which target
  netstandard2.1 at C# 9 themselves. The generated code compiles against
  serialize.cs v1.9.1, the release the tree pins.
- **The construction form.** C# 9 has no parameterless struct constructor, so a
  type whose closure declares a default or a counted array born above zero gets
  `Schema.New<T>()` and `Schema.Init<T>(ref value)`; for every other type
  `new T()` and `default` already are the construction form. `default` is
  always the §5 zero form.
- **Errors.** `bool` returns. A read-side validation failure returns false
  without latching; a stream failure latches on `stream.Error`. Write-side
  contracts ride on `Debug.Assert`, gone from a release build, exactly as in
  serialize.cs (CONTRIBUTING rule 4): `Debug.Assert` is
  `[Conditional("DEBUG")]`, so the call and its arguments compile out of any
  build that does not define `DEBUG`.
- **Debug names.** `EnumName<E>`, `FlagName<F>` and `FlagNames<F>` for logs and
  tools; nothing on the wire path calls them.

## Using it in a Unity project

1. Put serialize.cs's `src/Serialize.cs` and `src/Int128Pair.cs` in their own
   folder with an assembly definition (`MasBandwidth.Serialize`).
2. Generate into a folder of its own: `schema generate --lang unity --out
   Assets/Net/Generated schema/`. Give it an asmdef that references the
   serialize one:

```json
{
    "name": "Game.Protocol",
    "references": [ "MasBandwidth.Serialize" ],
    "allowUnsafeCode": false,
    "noEngineReferences": true,
    "autoReferenced": true
}
```

`noEngineReferences` is right: the generated code references no `UnityEngine`
type, so the protocol assembly builds for a dedicated server, a headless test or
a plain .NET tool unchanged. The generator does not write the asmdef itself:
two units may share one output directory (SPEC §3.2), Unity allows one asmdef
per folder, and the assembly's name and references are the project's decision.

3. With Netcode for GameObjects or Unity Transport, the generated code writes
   into a serialize.cs stream and the transport carries its bytes:
   `Schema.WriteX(stream, ref msg)`, `stream.Flush()`, then copy `stream.Data`
   into the transport's writer. A `WriteStream` and its buffer are reused across
   messages (`stream.Reset(buffer)`), so the steady state allocates nothing
   beyond that one copy.

## How it is tested

No Unity editor runs in CI, so the Unity dialect is reproduced where CI can run
it: every generated unit is compiled as a **netstandard2.1 library at
`LangVersion` 9, warnings as errors, the SDK's analyzers on, nullable off and
unsafe code disallowed** (`test/unity/UnityDialect.props`), with serialize.cs
referenced as its own netstandard2.1 assembly. A small .NET host drives it:

- `make unity-wire` — `test/unity`: the examples and the 128-bit corpus, every
  instance written and byte-compared against the C++-pinned goldens in
  `testdata/wire`, read back, branch-zeroed and refused where hostile; Debug and
  Release. The buffer layout is asserted first.
- `make packet-defaults-unity` — string, bytes and flags defaults against the
  C++ pins and the construction form, storage byte for byte.
- `make packet-utf8-unity` and `make packet-wide-unity` — the read-side UTF-8
  and UTF-16 refusals against the C++ oracle over the pinned rows and a bit-flip
  sweep.
- each with a negative control that plants a defect in a copy of the generated
  source and requires the gate to go red for that reason.

`make test-unity` runs all of it. `internal/goldens` pins the generated source
(`testdata/golden/unity`, `ludicrous/unity`, `packet-wide/unity`).

## What is not verified

- **An editor or player build.** The dialect compile is the netstandard2.1
  surface and C# 9; it is not Unity's Roslyn fork, Mono or IL2CPP. The buffer
  layout is asserted on CoreCLR only.
- **Burst.** The storage is unmanaged by construction and nothing in it is
  managed, but no Burst compiler has seen the generated types, and the
  `AsSpan` view (`MemoryMarshal.CreateSpan`) inside Burst-compiled code is
  unchecked. The wire functions are not Burst-callable: serialize.cs's streams
  are sealed classes.

## What serialize.cs would add for Unity

Reported, not worked around in generated code:

- **A Unity package**: a `package.json` and an asmdef, so a project installs the
  runtime from the Package Manager by git URL instead of copying two files.
- **A Burst-callable stream**: a struct writer and reader over unmanaged memory
  (a `NativeArray<byte>`'s pointer, or a `Span<byte>`), the `DataStreamWriter`
  shape, so the generated wire functions could run inside a job. Today the
  streams are sealed classes over `byte[]`, which keeps them off Burst and costs
  one copy into a `NativeArray`.
