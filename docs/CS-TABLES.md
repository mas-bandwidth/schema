# C# tables

Generate with `schema generate --lang cs --out generated <schema-directory>`.
The generated unit targets the pinned .NET SDK in `.github/dotnet-version` and
uses `unsafe` for native rows, regions, cooks and builders. The packet surface
continues to use the same generated files.

Compile each generated `*Table.cs` together with its unit's `*Region.cs` files.
The region runtime owns the shared announcement and vocabulary definitions
used by managed tables as well as native storage.

## Choosing storage

Generated classes are convenient managed authoring values. They carry variable
graphs, shared pointers, blobs, lists and maps, and support file, JSON and cook
operations; the message form is a fixed table's alone (SPEC-TABLES §3.3). Their variable read path allocates managed values.

Generated `<Table>Row` structs describe native storage. The file form's
`LoadMeasure` sizes a caller-owned region; native loads fill it, and
native measure/save walk it directly. Data and node attribution can be supplied
separately. Preserve attribution for retain-unknown; it is unnecessary for an
ordinary relocated region's measure/save. A negative measure is a refusal and
must be checked before allocating or loading. `LoadVerdict` and `TableReport`
distinguish damage, evolution events and named refusals.

Message sizing reports expanded storage without allocating it, including known
constant list elements that occupy zero bits on the wire. Check the measured
size against the caller's capacity budget; native load refuses insufficient
capacity before filling the region. Managed authoring reads allocate their values.

Native offsets are relative to their slots. Move the entire data region as a
unit, then reacquire its root and derived pointers. A loaded collection's spare
word is padding, never authoring capacity. `CopyFrom` imports a native region
into a mutable builder and reconstructs its collection storage.

## Building and packing

Each generated `<Table>Builder` owns its arena. Dispose it when finished.
`GetRoot()` returns its mutable native root. `Alloc<T>()` creates generated row
storage with declared defaults; `AllocBytes` and `AllocString` create blob
storage. Set pointer slots with `TableArena.SetReference` and follow them with
`TableArena.At<T>`. Check allocation results before dereferencing.

`CreateWorker()` supplies an exclusive worker for one filling thread. Workers
carve ordinary nodes from stable slabs; large nodes use separate spans. Finish
all workers before saving, cooking, locking or disposing. List and map handles
come from the worker and the generated field descriptor. Capacity is distinct
from live count, and collection growth preserves existing element addresses.

`Measure`/`Save` and `CookMeasure`/`Cook` accept the mutable graph. `Lock()`
measures and packs reachable storage, verifies that packing consumes each
measured extent, then releases the slabs. It preserves sharing and refuses cycles.
After successful locking, `GetRoot()` returns null, workers stop allocating and
`AsConst()`/`Region` expose the packed root. Locking is one-way and idempotent.
`DataBytes` and `AttributionBytes` describe the two packed parts. A failed lock
leaves the builder available for correction or disposal.

`Load` on a builder fills its mutable graph directly. Check the returned bool;
a failed builder read requires discarding the partial value. In particular,
exceeding a list or map's int32 storage cap adds no report event of its own.

`TableAllocator` accepts a matched `Allocate`/`Free` function-pointer pair and
`Context`. Custom allocation must return zeroed, at least 16-aligned storage,
or null on failure. The default uses `NativeMemory`. The pair owns native arena
and packing allocations and native numbering scratch; it does not replace the
managed authoring classes' allocations. The generated block builder has its
own paired allocator contract.

## Wire and validation

The surface includes form-1 files, announcements and bitpacked form-2 message
batches, both cook byte orders, block build/open, JSON, reflection and UnitView.
All field shapes use the same descriptors: optional and keyed arrays, general
union arms, wide scalars, bounded strings, maps and unbounded lists.

Retain-unknown uses caller-owned byte and ID stores plus node attribution. Load
reports capacity losses; save reports retained records whose paths or identities
can no longer be placed. Retaining a fixed root and saving retained state as a
message are refused explicitly; retention is a variable table's, and a variable
table has no message form, so save retained state as a file.

`make test-cs` runs the integration, listing, differential fuzz, allocation,
negative-control and cook-open gates. The managed/native file and builder arms
use independent oracle paths; `--builder` checks builder completion without a
region preflight. `test/cs-tables/src/BuilderChecks.cs`, `RegionChecks.cs` and
`RetainChecks.cs` are executable ownership and lifetime examples. The shared
conformance runner compares canonical wire, JSON, messages and cooks with the
reference corpus. Benchmark output from a shared interactive machine is a
pairing check, not a release performance claim.

## The generated C# dialect

The generator writes C# that reads as the language's own, at the SDK's default
strictness: `dotnet format` with no `.editorconfig` beside the generated units,
and the Roslyn analyzers the SDK ships with `EnableNETAnalyzers=true`,
`AnalysisLevel=latest` and `AnalysisMode=Default`, no stricter mode and no
suppression file. `test/cs/schematest.csproj` and
`test/conformance/cs/schemaconformance.csproj` pin those three properties, and
the gates hand the same set to every other project they check. A finding is
fixed in the emitter (`internal/codegen/csharp`, `internal/codegen/cstable`),
never silenced. Two gates hold it, and each runs both halves and names every
finding by rule id and file before it fails:

- `make packet-clean-cs` holds the packet units (the examples, the 128-bit
  examples, the bench and RealWorld corpora, and the packet-defaults,
  packet-text units). It is green.
  `packet-clean-cs-negative-control` plants a misindented block and an unused
  private field in one copied unit and requires `error WHITESPACE` and
  `error CS0169` on that file.
- `make tables-cs-clean` holds the table units the conformance project
  compiles. It is green and required by `make test-cs`. At the gate's fix it
  found 152,044 WHITESPACE findings in 295 units (issue #547): the table
  emitters wrote compact expressions (`if(a<=b)`, `f(x,y)`, `(ulong) x`), a
  brace at the end of a line, case blocks at the label's indentation and
  object initializers several members to a line. Each is fixed in the emitter
  under `internal/codegen/cstable`, never by reformatting the output.
  `tables-cs-clean-negative-control` plants one misindented statement in a
  copy of `a1/A1Table.cs` and requires `error WHITESPACE` on that file and on
  no other.

Both gates skip when the pinned `dotnet` is not on PATH, and fail instead under
`SCHEMA_REQUIRE_CORPUS=1`.

What the generator holds:

- **Formatter-owned text.** Whitespace, indentation (four spaces), brace
  placement (each brace on its own line) and spacing around operators and
  after commas and keywords belong to `dotnet format`; the emitter writes what
  the formatter would write. `packet-clean-cs` holds this for the packet units
  and `tables-cs-clean` for the table units. The runtime text the table
  emitters carry as Go string constants is written in the formatter's form,
  with each brace on its own line, one member per line in a multi-line object
  initializer, a case block indented one level past its label, and `{ }` for
  an empty initializer.
- **Naming.** Types, members and enumerators are PascalCase from the schema's
  names. The namespace is the schema package, capitalised (`Example`,
  `Tblrt1`). An enum declares its wire-width underlying type (`enum Team : byte`). A packet `type` or union is a `public struct`
  with public fields and `[InlineArray]` buffers, taken by `ref`, in a unit
  without tables; in a unit with tables, packet declarations and managed
  table values are each a `public sealed class` with public fields until the
  table surface takes values by ref. Native rows, cooks and blocks are
  `struct`s, `unsafe readonly` where they point at memory.
- **`public static partial class Schema`.** C# has no namespace-level functions
  or constants, so each unit's functions and constants live in one static class
  per namespace, partial, one slice per generated file: `Schema.ProtocolId`, the
  `Read`/`Write` functions, `EnumName<T>` and `FlagName<T>`. The compiler's
  TreatWarningsAsErrors and the analyzer half of both gates hold it.
- **Nullable annotations.** The generated code carries none, and no
  `#nullable` directive. The projects compile it under `<Nullable>annotations`,
  which admits the runtime's own `object?` annotations without turning on
  nullable warnings. A `?` reference annotation in a generated unit would be a
  new convention, not an accident, and lands with a project change.
- **Unsafe.** The packet units use none. The table units use `unsafe` in the
  native surfaces only, the `*Block.cs`, `*Cook.cs` and `*Region.cs` units
  (`unsafe readonly struct`, function-pointer allocators), and the projects
  that compile them set `AllowUnsafeBlocks`. The managed `*Table.cs` classes
  stay safe code.
- **Contracts.** Read-side validation returns `false`; write-side contracts are
  `Debug.Assert`, so they compile out of a release build as serialize.cs's own
  `WriteStream` does. The packet units' small fixed bodies carry
  `[MethodImpl(MethodImplOptions.AggressiveInlining)]`.
- **Generated-code marker.** Each unit opens with `// Code generated by the schema compiler ... DO NOT EDIT.` Roslyn does not read that line as its
  `<auto-generated>` marker, so the analyzers run over the generated units at
  full default severity rather than skipping them as generated code (a planted
  `object.ReferenceEquals` on an `int` turns the build red on CA2013).
