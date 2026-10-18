# Go generated-size survey

Measured at `sprint/schema-go` @ `19b8330208c2639d5b2d4653218b09c3aafd8724`
(hulk, 2026-10-18). This page measures which code the Go emitter
(`internal/codegen/gotable`) writes the same way in every table, or in every
unit, and so could live once in `tables/goruntime` instead of in each
generated unit. It changes no code. Each row is a proposed cut, and each cut
is its own pro card (listed at the end).

The rule is "less is more". A cut moves code and keeps behaviour: the same
wire bytes, reports, refusals and exported names. Where a move would change
behaviour, it is not a cut, and the page says why.

## The two units measured

| unit | runtime | files | lines | bytes |
|---|---|---:|---:|---:|
| `tables/examples` (9 schema files, one package) | standalone (default) | 39 | 63958 | 2095801 |
| `tables/examples` | `runtime=shared` | 39 | 62260 | 2053491 |
| one table: `package probe` + `fixed table Pos { x int32; y int32 }` | standalone | 9 | 9001 | 284667 |
| one table | `runtime=shared` | 9 | 7298 | 228248 |
| `tables/goruntime/goruntime.go` (counted once) | | 1 | 2061 | 69768 |

The card's RED AT BASE was measured at `caea41783528` (55768 and 8270 lines).
The feature cards that have landed since then grew both numbers. The figures
above replace those.

`runtime=shared` saves 1698 lines in `tables/examples` and 1703 in the
one-table unit. It cuts only the declarations that the home `<Package>Table.go`
and `<Package>TableMessage.go` carry byte-for-byte in every one of the six
`sharedProbes`. In the one-table unit it removes 2016 lines of runtime: 1698
from `ProbeTable.go` and 318 from `ProbeTableMessage.go`. It adds a 313-line
alias shim to `ProbeTable.go`, so `ProbeTable.go` goes from 2968 to 1583 lines
and `ProbeTableMessage.go` from 663 to 345, a net saving of 1703. In
`tables/examples` the same files go from 5428 to 4045 and from 1537 to 1222,
a net saving of 1698. Its shim is 593 lines, larger because the unit uses more
of the shared names.

**Long-term targets:** one-table unit 2500 lines, `tables/examples` 40000
lines, with the shared runtime counted once and not per unit.

## Method

1. Build the compiler at BASE. `cmd/schema generate` has no flag for the
   `runtime` option, so a copy of the tree in `.cmp/` (excluded from git)
   passes `runtime` from the environment to `compiler.Options`. The
   committed tree is unchanged.
2. Generate both units with `--lang go` in both runtime modes. Also generate
   the two rich `sharedProbes` shapes (`P2`: enum, flags, union, keyed and
   optional fields; `P4`: variable root with lists, pointers, maps and
   unions). They are the third and fourth units for the across-unit
   comparison.
3. Parse every generated file with `go/parser` and split it into top-level
   declarations. A declaration's lines include its doc comment and the blank
   line after it.
4. **Per-table families.** Within `tables/examples`, tokenise each
   declaration (`go/scanner`, so comments do not count). Replace each schema
   type name, and the package name, wherever it appears inside an identifier
   or a string with one placeholder. Replace each integer literal with
   another. Declarations with equal token streams form one family. A family
   whose copies differ only in names is "names-only". One that also differs
   in integers is "names+ints": the integers become parameters.
5. **Per-unit declarations.** A declaration whose name holds no table name
   is unit-level. It is *uniform* when every unit that declares it has
   byte-identical text, and at least two units declare it. It is *movable*
   when it is uniform and everything it names is movable too. A method
   moves with its type. This is the same closure that `buildSharedRuntime`
   applies (rules a and b). Rule c, unexported members, is checked by
   `buildSharedRuntime` itself when a cut lands. The shim's existing alias
   lines are not counted as movable code.
6. To measure a cut that would make a non-uniform declaration uniform (for
   example, always emitting a struct's optional field), treat that
   declaration as uniform and rerun step 5. A cut's saving is the difference
   in movable lines.


## Unit-level cuts: code the same in every unit

Measured in the one-table unit (shared mode, 7298 lines) and in
`tables/examples` (shared mode, 62260 lines). Every unit-level declaration
appears once per unit, so `tables/examples` holds one copy of each and its
saving is the size of that copy. Savings are lines removed from each unit;
the code is then written once in `tables/goruntime`. The rows are sorted
largest one-table saving first. Each U3-U5 saving is the step it adds in the
order U3, U4, U5.

| # | cut | emitter (file: function) | copies in examples | one-table saving | examples saving | parameters | why behaviour is unchanged |
|---|---|---|---:|---:|---:|---|---|
| U4 | a unit-sized id store: `TableIds` (and `tableRetainPath`) hold slices over arrays the unit declares, instead of arrays sized by the unit constant `tableIdCapacity` (or `tableRetainPathDepth`) | `wire.go`: `tableWireRuntime` (`const tableIdCapacity = %d`, `tableIdBuckets`); `retain.go` (`tableRetainPathDepth`) | 1 | 3154 | joint with U3 and U5 (below) | the unit's capacity and bucket count, carried by the store | **Unverified: a conditional cut.** The capacity and the overflow point stay the unit's own. But `tableWireRuntime`'s comment, "Neither path needs a map or a heap allocation", is a contract, and the slices must not move the store to the heap. This survey did not check escape analysis. The cut holds only if every `testing.AllocsPerRun` pin (`fixedloadallocs_test.go`, `strictreadalloc_test.go` and the rest) is unchanged at the card's head. If one moves, the move changes behaviour, it is not a cut, and the savings that depend on it (below) do not arrive. |
| U1 | the shared runtime also covers the JSON, cook, block and view runtime files, not only `<Package>Table.go` and `<Package>TableMessage.go` | `gotable.go`: `buildSharedRuntime`, `GenerateRuntimeSurface` (the cut). The code comes from `json.go`: `generateJsonFiles`; `cookwrite.go` (`goCookWriteRuntime`); `block.go`; `view.go` | 1 | 767 (Json 620, Cook 80, Block 57, View 10) | 767 | none: these declarations are already byte-identical and closed | the declarations are byte-identical in every unit. Only their home changes: same text, one copy. |
| U2 | share a declaration that is identical wherever it is present, instead of requiring it in every probe | `gotable.go`: `buildSharedRuntime` (the condition `m[byKey[k].text] == len(probes)`). The code is the fixed-form compiler in `fixedruntime.go` (`tableFixedCompile`, `tableFixedCompileEntry`, `tableFixedPush`, `tableFixedLineagePlans`, `tableFixedMatchChildren`, ...) | 1 | 444 | 878 | none | a unit that does not use one of these never calls it. The Go linker drops what is unreferenced, and a unit that does use it gets the same text it has today. |
| U3 | superset runtime structs and bodies: emit the optional runtime fields and branches always, instead of splicing them in per unit with `tableSourceReplace` | `wire.go`: `tableWireRuntime` (`TableIds.Numbering`, `TableReader.Nodes`); `retain.go` (the `TableTypeInfo` retain hooks); `message.go` (`TableMessageReader.Nodes`, the `TableMessageWriter` numbering fields); `lists.go` (`TableNodeMap.Carve`); `textlist.go` (`TableTypeInfo.JsonArray`, the `tableJsonReadScalar` branch); `regions.go`, `jsongraph.go`, `messageregion.go`, `retainmessage.go` (the `tableRegion*`, `tableJsonGraph` and `tableMessage*` variants). There are 101 `tableSourceReplace` sites; each one that only adds a field or an unreachable branch is in scope | 1 | 416 | joint with U4 and U5: 6670 through the cards below (upper bound 6759) | none | **Runtime behaviour is unchanged, but the exported API changes, additively.** At run time an added field is zero and unused in a unit without the feature. An added branch is selected by a field kind that unit does not declare, so it is unreachable. Wire bytes, reports and refusals are identical. But exported structs (`TableReader`, `TableMessageReader`, `TableTypeInfo`, `TableIds`) gain exported fields in units that lack the feature. That is an API change, so the cards that carry U3 (C2-C5) need the owner's yes before they are dealt. |
| U5 | unit constants as data: the shared code reads `BuildVersion`, `tableCookBuildVersion`, `tableMessageEntries`, `tableAnnouncement` and the cook record list from a unit descriptor the unit registers, instead of naming them | `cook.go` (`BuildVersion`), `cookwrite.go` (`tableCookBuildVersion`, `tableCookRecords`), `message.go` (`tableMessageEntries`, `tableAnnouncement`) | 1 | 403 | (joint, above) | the unit descriptor | the same values reach the same comparisons. Only the place they are read from changes. Not carded: `tableUnitView`, `tableViewPacketTypes`, `tableViewPacketUnions` and `tableBlockRecords` (6 lines) and `ProtocolId`/`ErrValidation` (13 lines, in `Pos.go`, written by the packet emitter `internal/codegen/golang`, outside this survey). Those 19 lines are inside the 403. |
| U6 | unexported shared names are referenced as `goruntime.X` directly, with no alias in the unit | `gotable.go`: `buildSharedRuntime` (the shim), `rtShim`, `sharedRuntime.apply` | 1 | 132 code lines today (about 200 with blank lines) | 132 (about 200) | none | an alias and its target are the same declaration. U1 to U5 make about 196 more unexported names shared; without U6 each would cost about 2 alias lines. |

U3, U4 and U5 are one chain. A declaration becomes uniform only when every
splice that varies it is gone, and a declaration moves only when everything it
names moves too. In the one-table unit the chain gives 1211 -> 1627 -> 4781 ->
5184 (U3, then U4, then U5). In `tables/examples`, `TableTypeInfo`,
`TableNodeMap`, `TableIds` and `tableRetainPathDepth` each sit under about
5700 of the 6759 lines, so no one link unlocks much alone. Together they reach
the measured upper bound: 8404 movable lines, against 1645 movable today.

## Per-table cuts: helpers written once per table

Measured in `tables/examples` (shared mode). Each family is one exported
per-table function or `var` whose body is the same in every table, up to the
table's names (and in some families its integers). The exported name stays,
with its signature and doc, as a wrapper over one unit-level helper. That
helper is table-independent, so U1 and U2 then carry it into
`tables/goruntime`. For `<T>FixedLineageHoles` and `<T>FixedPlanMax`, which
are `var`s with a function-literal initialiser, the `var` stays and its
initialiser becomes a call.

Net saving = copies x (body lines - wrapper lines) - one body. A wrapper is
3 lines, or 6 for the `Open` methods, which also fill their receiver. Rows are
sorted largest saving first. The "- one body" is the standalone-mode figure,
where the helper stays in the unit. In shared mode C1 moves the helper into
`tables/goruntime`. Each family then saves one more body (779 lines in
`tables/examples` across T1-T9), and the one-table unit saves body - wrapper
for each family it carries. That last figure was not measured per family
here, and is at most about 700 lines. The card measures both.

Where a family has two shapes, its copies are written "a + b". `<T>LoadTolerant`
has 22 copies: 18 of shape a and 4 of shape b, both 57 lines. `<T>Load` and
`<T>ReadStrict` appear in 18 of those 22 tables, the ones with that read entry.
`<T>LoadText` is the same: 18 + 4 copies, both 31 lines.

| # | family | emitter (file: function) | copies | body lines | net saving | parameters | behaviour note |
|---|---|---|---:|---:|---:|---|---|
| T2 | `<T>FixedLoadTolerant` (shape a, shape b), `<T>FixedLoad`, `<T>FixedSave`, `<T>FixedSaveReason`, `<T>FixedLineageHoles`, `<T>FixedPlanMax` | `fixedform.go`: `emitFixedRoot` | 9 + 3, 12, 8, 12, 12, 12 | 114 / 111, 19, 24, 9, 10, 9 | 1615 = 885 + 213 + 173 + 144 + 63 + 74 + 63 | element size, the per-record codec, the lineage table | the same plan walk over the same bytes. Only the per-record codec is per table. |
| T1 | `<T>LoadTolerant` (shape a, shape b), `<T>Load`, `<T>ReadStrict` | `read.go`: `emitTableRead` | 18 + 4, 18, 18 | 57 / 57, 14, 12 | 1408 = 915 + 159 + 184 + 150 | storage pointer, the table's `LoadBody` and `Reset` as `func(*TableReader, unsafe.Pointer) bool` and `func(unsafe.Pointer)` | **Not through the existing descriptor hook.** `TableTypeInfo.LoadBody` takes the reader *by value*, but `LoadTolerant` reads `r.Offset` after the body for its early-end check. The helper needs a pointer-reader hook, or the end check would see a stale cursor. |
| T4 | `<T>LoadText` (shape a, shape b), `<T>FromJsonWith`, `<T>SaveText` | `json.go`: `generateJsonFiles`, `writeVariableTextCarrier` | 18 + 4, 18, 18 | 31 / 31, 14, 11 | 871 = 473 + 81 + 184 + 133 | `*TableTypeInfo`, builder hooks | the JSON walk is already descriptor-driven. Only the entry boilerplate repeats. |
| T3 | `(*<T>Cook).Open`, `<T>At` | `cook.go`: `emitOpen`, `emitAt` | 19, 19 | 43, 10 | 783 = 660 + 123 | the minimum data length (the only integer that differs: 68 vs 28) | the same header checks in the same order, with the same refusal for each. |
| T5 | `<T>SaveMessages`, `<T>MeasureMessages` | `messagewrite.go`: `emitMessageWrite` | 18, 18 | 27, 19 | 674 = 405 + 269 | `*TableTypeInfo` | same batch bound, same refusal names |
| T6 | `<T>LoadMessagesTolerant`, `<T>LoadMessages` | `messageread.go`: `emitMessageRead` | 18, 18 | 20, 19 | 555 = 286 + 269 | `*TableTypeInfo`, element size | same |
| T9 | `<T>Save`, `<T>Measure`, `<T>SaveBody`, `<T>MeasureBody` | `write.go`: `emitTableSave`, `emitTableMeasure`, `emitTableWrite`, `emitArithMeasureBody` | 18, 16, 19, 17 | 13, 13, 7, 7 | 444 = 167 + 147 + 69 + 61 | the table's `SaveBody` | `var ids TableIds` stays on the stack in the wrapper, or the helper takes it by pointer: the allocation pins decide (see U4) |
| T8 | `(*<T>BlockStorage).Create`, `<T>BlockBegin`, `<T>BlockBytes` | `blockbuild.go`: `emitBlockBuild` | 13, 12, 12 | 17, 18, 7 | 368 = 165 + 162 + 41 | the projection size, counts | same |
| T7 | `(*<T>Block).Open` | `block.go`: `emitBlockOpen` | 12 | 36 | 324 | the block layout descriptor | same checks, same refusals |

Per-table total: 7042 lines in `tables/examples`. In the one-table unit
these families have one copy each, so they save nothing there.

Not cuts: the per-table `init` blocks (17 + 15 copies, of 13 and 7 lines).
They register per-table data, and their bodies differ by the table's field
list. The `<T>MeasureReason` and `*BodyRetain` one-liners (7-8 lines after
gofmt) each save less than 70 lines and are left to the size-budget card.

## How far the cuts reach

The columns follow the cards below, in their landing order. U6 is counted
as its 132 code lines only. Counted the way step 3 counts, with its blank
lines, it is about 200, so C1's figures are a lower bound. The one-table
unit gains 1211 + 132 from C1, then 3954 from C2-C7. That is 19 lines less
than the 3973 of the U3-U5 chain above: the view tables and `ProtocolId`,
which no card carries. `tables/examples` gains 1645 + 132, then 6670.

| | today (shared) | after C1 (U1, U2, U6) | after C2-C7 | after P1-P8 | target |
|---|---:|---:|---:|---:|---:|
| one-table unit | 7298 | 5955 | ~2000 (+~100-200 exported aliases: ~2100-2200) | up to ~700 less (shared mode; see the per-table note) | 2500 |
| `tables/examples` | 62260 | 60483 | 53813 (with T3's 783 in C6: 53030) | ~46800 (+~200 aliases) | 40000 |
| `tables/goruntime` (once) | 2061 | ~3700 | ~10500 | ~11300 (+779 helper bodies) | |

**The one-table unit** reaches the 2500 target with the unit-level cuts
alone, and only if U4 holds its allocation pins. U4 sits under 3154 of the
3954 lines. If U4 fails, the chain stops after C1 and parts of C3, and the
one-table unit ends near 5900 lines. What remains at about 2000 lines is
mostly per-table code: `PosTable.go` (607 lines), `PosTableMessage.go` (227),
`Pos.go` (19) and the roughly 500 per-table lines of `PosBlock.go` and
`PosCook.go`. The rest is file headers, imports, the exported aliases, and
173 lines of per-unit variants (`init` blocks, small `var` tables, the 19
uncarded lines). Those 173 lines are the sum of every unit-level declaration
in the one-table unit whose text differs between units.

**`tables/examples`** stops about 7000 lines short of 40000. What remains is
per-table code whose body differs by the table's fields: the emitted
`LoadBody`, `SaveBody` and message-body codecs, `Reset`, the descriptor
`Fields` tables and the `init` registrations. By name there are 96 families,
with 44076 lines beyond one copy each, but their bodies are not the same
function. Closing that gap means descriptor-driven field codecs, which walk
`TableFieldInfo` instead of emitting per-field code. That is a design change
with a performance trade, not a move of identical code. It is outside this
survey's definition of a cut, and it needs the owner's decision before anyone
cards it.

## Proposed pro cards

`tables/goruntime/goruntime.go` and `generated/bench/**` are regenerated (with
`make go-runtime` and the `make/go.mk` rules), never hand-edited. C1 changes
only `runtime=shared` output, so the bench trees stay as they are. A test file
marked NEW does not exist yet; the card creates it.

**A second deviation: `generated/bench/**` is in every card's PATHS except
C1's.** The brief's PATHS are the emitter file, its test and
`tables/goruntime`. But the GENERATED FILES rule requires every emitter change
to regenerate and commit the bench trees. That output is regenerated, never
hand-edited, so sharing it between cards is not a hand-edited overlap. It does
mean that no two cards can be read or landed at once.

**Every card needs `schema-go-generated-size-budget`.** That card does not
exist in the tree yet, and DONE-WHEN item 4 lowers its budget constants. Each
cut card therefore needs it landed first, as its DEPENDS-ON.

**One deviation from the brief's one-emitter-file rule: C5.** The
variable-table runtime declarations become uniform only when the splices in
`regions.go`, `lists.go`, `textlist.go`, `jsongraph.go`, `messageregion.go`
and `retainmessage.go` are all gone. Measured separately, each would save 0,
which no DONE-WHEN could lower a budget by. They are therefore one card, with
one measured saving. No other card touches those files.

The C-cards form a chain. Each needs the one before it, and their savings are
**cumulative, in the order shown** (one-table unit / `tables/examples`). They
all regenerate `goruntime.go`, which is one more reason they land in order.
The P-cards are independent of each other in content, and each needs C1 so
that its new unit-level helper is shared. They regenerate `goruntime.go` and
the bench trees too, so they also land one at a time, in any order, each
regenerated on the previous one's tip.

| order | card id | cut | PATHS | saving (one-table / examples) |
|---|---|---|---|---:|
| C1 | `schema-go-cut-shared-runtime-reach` | U1 + U2 + U6 | `internal/codegen/gotable/gotable.go`, `internal/codegen/gotable/sharedruntime_test.go`, `tables/goruntime/goruntime.go` | 1343 / 1777 |
| C2 | `schema-go-cut-wire-runtime-uniform` | U3 for `TableReader.Nodes` and `TableIds.Numbering`; U4, the unit-sized id store (needs the owner's yes: API, U3) | `internal/codegen/gotable/wire.go`, `internal/codegen/gotable/wire_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | +594 / +4 |
| C3 | `schema-go-cut-message-runtime-uniform` | U3 for `TableMessageReader.Nodes` and the `TableMessageWriter` numbering fields (needs the owner's yes: API, U3); U5 for `tableMessageEntries`, `tableAnnouncement`, `TableMessageEntriesHere`, `TableMessageRefBitsHere` | `internal/codegen/gotable/message.go`, `internal/codegen/gotable/message_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | +80 / +181 |
| C4 | `schema-go-cut-retain-runtime-uniform` | U3 for the `TableTypeInfo` retain hooks (needs the owner's yes: API, U3); U4 for `tableRetainPathDepth` | `internal/codegen/gotable/retain.go`, `internal/codegen/gotable/retain_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | +2930 / +55 |
| C5 | `schema-go-cut-variable-runtime-uniform` | U3 for `TableNumbering`, `tableNumber`, `TableNodeMap` (with its `Carve` splice), `tableJsonGraph`, `TableTypeInfo.JsonArray` and the `tableJsonReadScalar` text-list branch, and the `tableRegion*`, `tableBuilderLoad`, `tableRegionLoadRetain` and `tableMessageExtent` / `tableMessageLoadInto*` body variants (needs the owner's yes: API, U3) | `internal/codegen/gotable/regions.go`, `lists.go`, `textlist.go`, `jsongraph.go`, `messageregion.go`, `retainmessage.go`; tests `regions_test.go`, `lists_test.go`, NEW `textlist_test.go`, NEW `messageregion_test.go`, NEW `jsongraph_test.go`, NEW `retainmessage_test.go`; `tables/goruntime/goruntime.go`, `generated/bench/**` | +0 / +6011 |
| C6 | `schema-go-cut-cook-runtime-and-entry` | U5 for `BuildVersion`, plus T3 (both are in `cook.go`) | `internal/codegen/gotable/cook.go`, NEW `internal/codegen/gotable/cook_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | +194 / +194 + 783 |
| C7 | `schema-go-cut-cookwrite-runtime-uniform` | U5 for `tableCookBuildVersion`, the `tableCookWriter` variants and `tableCookRecords` | `internal/codegen/gotable/cookwrite.go`, `internal/codegen/gotable/cookwrite_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | +156 / +225 |
| P1 | `schema-go-cut-fixed-root-entry` | T2 | `internal/codegen/gotable/fixedform.go`, `internal/codegen/gotable/fixedform_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | 0 / 1615 |
| P2 | `schema-go-cut-table-read-entry` | T1 | `internal/codegen/gotable/read.go`, NEW `internal/codegen/gotable/read_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | 0 / 1408 |
| P3 | `schema-go-cut-json-entry` | T4 | `internal/codegen/gotable/json.go`, NEW `internal/codegen/gotable/json_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | 0 / 871 |
| P4 | `schema-go-cut-message-write-entry` | T5 | `internal/codegen/gotable/messagewrite.go`, NEW `internal/codegen/gotable/messagewrite_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | 0 / 674 |
| P5 | `schema-go-cut-message-read-entry` | T6 | `internal/codegen/gotable/messageread.go`, NEW `internal/codegen/gotable/messageread_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | 0 / 555 |
| P6 | `schema-go-cut-table-write-entry` | T9 | `internal/codegen/gotable/write.go`, NEW `internal/codegen/gotable/write_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | 0 / 444 |
| P7 | `schema-go-cut-block-build-entry` | T8 | `internal/codegen/gotable/blockbuild.go`, `internal/codegen/gotable/blockbuild_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | 0 / 368 |
| P8 | `schema-go-cut-block-open-entry` | T7 | `internal/codegen/gotable/block.go`, NEW `internal/codegen/gotable/block_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | 0 / 324 |

**DONE-WHEN for every card.**
1. The cut's declarations exist once, in `tables/goruntime`. For P1-P8 and
   T3, that means one unit-level helper with per-table wrappers that keep
   their exported name, signature and doc.
2. `TestGoSharedRuntimePackageIsCurrent`,
   `TestGoSharedRuntimeIsOptionalAndEquivalent` and `internal/ci`
   `TestGoBenchTreesAreCurrent` pass.
3. The full gate of this card (STEP 5) passes at the card's head. That
   includes every `testing.AllocsPerRun` pin, unchanged. For C2 and C4, which
   carry U4, this is the test of whether U4 is a cut at all.
4. `schema-go-generated-size-budget`'s budget constants for the one-table
   unit and `tables/examples` are lowered by the saving in the table above.
   Re-measure at the card's own base with that card's instrument, because the
   numbers here are at `19b833020`.
5. STEP 5b shows zero `--lang cpp` differences.

**STEP 5b for every card.** Build the compiler at BASE and at head. For every
`*.schema` in `git ls-files` at BASE, run `generate --lang go` and
`generate --lang cpp` with both binaries, keeping each exit status and its
stderr. The cpp trees are identical (`diff -rq` is empty). The go trees differ
only in the files and declarations the cut moves. Exit statuses and stderr are
identical for both languages.

**What the cards leave.** `tables/examples` keeps 89 lines between the cards'
cumulative 8315 (1645 + 6670) and the 8404 upper bound, which counts every
unit-level declaration as uniform. These are single-digit-line declarations,
including the view and block record tables (6 lines) and `ProtocolId` (13),
that the size-budget card can pick up one at a time.

## Limits of this survey

- "Uniform" was judged over four units: the one-table unit, `tables/examples`,
  `P2` and `P4`. `buildSharedRuntime` judges over all six `sharedProbes`. A
  declaration that is uniform here but varies in a probe not measured here
  (`W` with `wstring`, or the `table A { n int32 name string(8) }` shape)
  is refused by the card's own `TestGoSharedRuntimePackageIsCurrent`, so it
  is not lost.
- Rule c of `buildSharedRuntime` (code outside the shared set must not reach
  an unexported member) was not modelled. A card can lose part of its saving
  to it. The card measures what it actually moves.
- Wrapper sizes (3 lines, or 6 for `Open`) are estimates after gofmt. The
  per-table savings are net of them, and the card measures the real figure.
- U4's escape analysis was not checked. See its row.
- Savings are in lines. A line is not a byte, and not compile time, and none
  of those were measured here.
