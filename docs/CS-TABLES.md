# C# tables

Generate with `schema generate --lang cs --out generated <schema-directory>`.
The generated unit targets the pinned .NET SDK in `.github/dotnet-version` and
uses `unsafe` for native rows, regions, cooks and builders. The packet surface
continues to use the same generated files.

## Choosing storage

Generated classes are convenient managed authoring values. They carry variable
graphs, shared pointers, blobs, lists and maps, and support file, message, JSON
and cook operations. Their variable read path allocates managed values.

Generated `<Table>Row` structs describe native storage. File and message
`LoadMeasure` overloads size a caller-owned region; native loads fill it, and
native measure/save walk it directly. Data and node attribution can be supplied
separately. Preserve attribution for retain-unknown; it is unnecessary for an
ordinary relocated region's measure/save. A negative measure is a refusal and
must be checked before allocating or loading. `LoadVerdict` and `TableReport`
distinguish damage, evolution events and named refusals.

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
exceeding the list's int32 storage cap adds no report event of its own.

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
union arms, wide scalars, bounded UTF-16 strings, maps and unbounded lists.
`*wstring` remains outside the compiler's current table vocabulary; the native
UTF-16 allocation helper does not make it a supported wire pointer type.

Retain-unknown uses caller-owned byte and ID stores plus node attribution. Load
reports capacity losses; save reports retained records whose paths or identities
can no longer be placed. Retaining a fixed root and saving retained state as a
message are refused explicitly. Save each retained message region as a file.

`make test-cs` runs the integration, listing, differential fuzz, allocation,
negative-control and cook-open gates. The managed/native file and builder arms
use independent oracle paths; `--builder` checks builder completion without a
region preflight. `test/cs-tables/src/BuilderChecks.cs`, `RegionChecks.cs` and
`RetainChecks.cs` are executable ownership and lifetime examples. The shared
conformance runner compares canonical wire, JSON, messages and cooks with the
reference corpus. Benchmark output from a shared interactive machine is a
pairing check, not a release performance claim.
