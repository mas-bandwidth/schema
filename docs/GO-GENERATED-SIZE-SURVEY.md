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
`sharedProbes`. In the one-table unit, 1561 lines of it (`ProbeTable.go`
2968 -> 1583, `ProbeTableMessage.go` 663 -> 345) are replaced by a 313-line
alias shim.

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
`tables/examples` (shared mode, 62260 lines). Savings are lines removed from
each unit. The code is then written once in `tables/goruntime`.

| # | cut | emitter (file: function) | one-table saving | examples saving | parameters | why behaviour is unchanged |
|---|---|---|---:|---:|---|---|
| U1 | the shared runtime also covers the JSON, cook, block and view runtime files, not only `<Package>Table.go` and `<Package>TableMessage.go` | `gotable.go`: `buildSharedRuntime`, `GenerateRuntimeSurface` (the cut). The code comes from `json.go`: `generateJsonFiles`; `cookwrite.go` (`goCookWriteRuntime`); `block.go`; `view.go` | 767 (Json 620, Cook 80, Block 57, View 10) | 767 | none: these declarations are already byte-identical and closed | the declarations are byte-identical in every unit. Only their home changes: same text, one copy. |
| U2 | share a declaration that is identical wherever it is present, instead of requiring it in every probe | `gotable.go`: `buildSharedRuntime` (the condition `m[text] == len(probes)`). The code is the fixed-form compiler in `fixedruntime.go` (`tableFixedCompile`, `tableFixedCompileEntry`, `tableFixedPush`, `tableFixedLineagePlans`, `tableFixedMatchChildren`, ...) | 444 | 878 | none | a unit that does not use one of these never calls it. The Go linker drops what is unreferenced, and a unit that does use it gets the same text it has today. |
| U3 | superset runtime structs and bodies: emit the optional runtime fields and branches always, instead of splicing them in per unit with `tableSourceReplace` | `wire.go`: `tableWireRuntime` (`TableIds.Numbering`, `TableReader.Nodes`); `retain.go` (the `TableTypeInfo` retain hooks); `message.go` (`TableMessageReader.Nodes`); `lists.go` (`TableNodeMap.Carve`); `textlist.go` (`TableTypeInfo.JsonArray`, the `tableJsonReadScalar` branch); `regions.go`, `messageregion.go`, `retainmessage.go` (the `tableRegion*` and `tableMessage*` body variants). There are 101 `tableSourceReplace` sites; each one that only adds a field or an unreachable branch is in scope | 416 | joint with U4 and U5: 6670 through the cards below (upper bound 6759) | none | an added field is zero and unused in a unit without the feature. An added branch is selected by a field kind that unit does not declare, so it is unreachable. Wire bytes, reports and refusals are identical. The exported structs gain fields: an additive API change, which the card names. |
| U4 | a unit-sized id store: `TableIds` (and `tableRetainPath`) hold slices over arrays the unit declares, instead of arrays of the unit constant `tableIdCapacity` (or `tableRetainPathDepth`) | `wire.go`: `tableWireRuntime` (`const tableIdCapacity = %d`, `tableIdBuckets`); `retain.go` (`tableRetainPathDepth`) | 3154 | (joint, above) | the unit's capacity and bucket count, carried by the store | the capacity and the overflow point stay the unit's own. This is a cut only if allocation stays where it is today: `tableWireRuntime`'s comment, "Neither path needs a map or a heap allocation", is a contract. The allocation tests (`fixedloadallocs_test.go` and the `testing.AllocsPerRun` pins) must not move. If escape analysis puts the store on the heap, the move changes behaviour and this is not a cut. |
| U5 | unit constants as data: the shared code reads `BuildVersion`, `ProtocolId`, `tableCookBuildVersion`, `tableMessageEntries`, `tableAnnouncement`, `tableUnitView` and the cook/block record lists from a unit descriptor the unit registers, instead of naming them | `cook.go` (`BuildVersion`), `cookwrite.go` (`tableCookBuildVersion`), `message.go` (`tableMessageEntries`), `view.go` (`tableUnitView`), `gotable.go` | 403 | (joint, above) | the unit descriptor | the same values reach the same comparisons. Only the place they are read from changes. |
| U6 | unexported shared names are referenced as `goruntime.X` directly, with no alias in the unit | `gotable.go`: `buildSharedRuntime` (the shim), `rtShim`, `sharedRuntime.apply` | 132 code lines today (about 200 with blank lines) | 132 (about 200) | none | an alias and its target are the same declaration. U1 to U5 make about 196 more unexported names shared; without U6 each would cost about 2 alias lines. |

U3, U4 and U5 are one chain in `tables/examples`. `TableTypeInfo`,
`TableNodeMap`, `TableIds` and `tableRetainPathDepth` each sit under about 5700
of the 6759 lines. No one of them unlocks much alone, and all three together
reach the measured upper bound (8404 movable lines in examples, against 1645
movable today). In the one-table unit the chain is in sequence: U3 then U4 then
U5 gives 1211 -> 1627 -> 4781 -> 5184.

## Per-table cuts: helpers written once per table

Measured in `tables/examples` (shared mode). Each family is one exported
per-table function whose body is the same in every table, up to the table's
names (and in some families its integers). The exported function stays, with
its name, signature and doc, as a wrapper over one unit-level helper. That
helper is table-independent, so U1 and U2 then carry it into `tables/goruntime`.
Net saving = copies x (body lines - wrapper lines) - one body. A wrapper is 3
lines, or 6 for the `Open` methods, which also fill their receiver.

| # | family | emitter (file: function) | copies | body lines | net saving | parameters | behaviour note |
|---|---|---|---:|---:|---:|---|---|
| T1 | `<T>LoadTolerant` (2 shapes), `<T>Load`, `<T>ReadStrict` | `read.go`: `emitTableRead` | 22, 18, 18 | 57, 14, 12 | 1408 | storage pointer, the table's `LoadBody` and `Reset` as `func(*TableReader, unsafe.Pointer) bool` and `func(unsafe.Pointer)` | **Not through the existing descriptor hook.** `TableTypeInfo.LoadBody` takes the reader *by value*, but `LoadTolerant` reads `r.Offset` after the body for its early-end check. The helper needs a pointer-reader hook, or the end check would see a stale cursor. |
| T2 | `<T>FixedLoadTolerant` (2 shapes), `<T>FixedLoad`, `<T>FixedSave`, `<T>FixedSaveReason`, `<T>FixedLineageHoles`, `<T>FixedPlanMax` | `fixedform.go`: `emitFixedRoot` | 12, 12, 8, 12, 12, 12 | 114/111, 19, 24, 9, 10, 9 | 1615 | element size, the per-record codec, the lineage table | the same plan walk over the same bytes. Only the per-record codec is per table. |
| T4 | `<T>LoadText` (2 shapes), `<T>FromJsonWith`, `<T>SaveText` | `json.go`: `generateJsonFiles`, `writeVariableTextCarrier` | 22, 18, 18 | 31, 14, 11 | 871 | `*TableTypeInfo`, builder hooks | the JSON walk is already descriptor-driven. Only the entry boilerplate repeats. |
| T3 | `(*<T>Cook).Open`, `<T>At` | `cook.go`: `emitOpen`, `emitAt` | 19, 19 | 43, 10 | 783 | the minimum data length (the only integer that differs: 68 vs 28) | the same header checks in the same order, with the same refusal for each. |
| T5 | `<T>SaveMessages`, `<T>MeasureMessages` | `messagewrite.go`: `emitMessageWrite` | 18, 18 | 27, 19 | 674 | `*TableTypeInfo` | same batch bound, same refusal names |
| T6 | `<T>LoadMessagesTolerant`, `<T>LoadMessages` | `messageread.go`: `emitMessageRead` | 18, 18 | 20, 19 | 555 | `*TableTypeInfo`, element size | same |
| T9 | `<T>Save`, `<T>Measure`, `<T>SaveBody`, `<T>MeasureBody` | `write.go`: `emitTableSave`, `emitTableMeasure`, `emitTableWrite`, `emitArithMeasureBody` | 18, 16, 19, 17 | 13-14, 13, 7, 7 | 444 | the table's `SaveBody` | `var ids TableIds` stays on the stack in the wrapper, or the helper takes it by pointer: the allocation pins decide (see U4) |
| T8 | `(*<T>BlockStorage).Create`, `<T>BlockBegin`, `<T>BlockBytes` | `blockbuild.go`: `emitBlockBuild` | 13, 12, 12 | 17, 18, 7 | 368 | the projection size, counts | same |
| T7 | `(*<T>Block).Open` | `block.go`: `emitBlockOpen` | 12 | 36 | 324 | the block layout descriptor | same checks, same refusals |

Per-table total: 7042 lines in `tables/examples`. In the one-table unit
these families have one copy each, so they save nothing there.

Not cuts: the per-table `init` blocks (17 + 15 copies, 13 and 7 lines). They
register per-table data, and their bodies differ by the table's field list. The
`<T>MeasureReason` / `*BodyRetain` one-liners (7-8 lines after gofmt) save less
than 70 lines each and are left to the size-budget card.

## How far the cuts reach

| | today (shared) | after U1+U2 | after U3-U5 | after U6 | after T1-T9 | target |
|---|---:|---:|---:|---:|---:|---:|
| one-table unit | 7298 | 6087 | 2133 | ~2000 (+~100-200 exported aliases: ~2100-2200) | same | 2500 |
| `tables/examples` | 62260 | 60615 | 53945 | ~53810 | ~46800 (+~200 aliases) | 40000 |
| `tables/goruntime` (once) | 2061 | ~3700 | ~10500 | ~10500 | ~10700 | |

The "after" columns follow the cards below, in their landing order (one-table unit 1211 + 3954; examples 1645 + 6670). The one-table target is reached by the unit-level cuts alone, and U4 is the
largest of them. `tables/examples` stops about 7000 lines short of 40000. What
remains is per-table code whose body differs by the table's fields: the
emitted `LoadBody`/`SaveBody`/message-body codecs, `Reset`, the descriptor
`Fields` tables and the `init` registrations. By name there are 96 families
with 44076 lines beyond one copy each, but their bodies are not the same
function. Closing that gap means descriptor-driven field codecs, which walk
`TableFieldInfo` instead of emitting per-field code. That is a design change
with a performance trade, not a move of identical code. It is outside this
survey's definition of a cut, and it needs the owner's decision before anyone
cards it.

## Proposed pro cards

Each card's PATHS are one emitter file, its `_test.go`, and `tables/goruntime`
(regenerated with `make go-runtime`, never hand-edited). No two sibling cuts
share an emitter file: U1, U2 and U6 all edit `gotable.go`'s shared-runtime
derivation, so they are one card. Every card's DONE-WHEN lowers
`schema-go-generated-size-budget`'s budget constants by the saving measured
here, and has a STEP 5b in which only `--lang go` output differs and
`--lang cpp` has zero differences.

The unit-level cards form a chain. A declaration becomes uniform only when
every splice that varies it is gone, so most of the saving arrives when the
last link lands. Their savings below are therefore **cumulative, in the
landing order shown** (one-table unit / `tables/examples`), and each card
needs the one before it. The per-table cards (P1-P8) are independent of each
other. Each needs C1, so that its new unit-level helper is shared.

| order | card id | cut | PATHS | saving (one-table / examples) |
|---|---|---|---|---:|
| C1 | `schema-go-cut-shared-runtime-reach` | U1 + U2 + U6 (changes `runtime=shared` output only; standalone is unchanged) | `internal/codegen/gotable/gotable.go`, `internal/codegen/gotable/sharedruntime_test.go`, `tables/goruntime/goruntime.go` | 1343 / 1777 |
| C2 | `schema-go-cut-wire-runtime-uniform` | U3 for `TableReader.Nodes` and `TableIds.Numbering`, plus U4 (the unit-sized id store) | `internal/codegen/gotable/wire.go`, its `_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | +594 / +4 |
| C3 | `schema-go-cut-message-runtime-uniform` | U3 for `TableMessageReader.Nodes` and the `TableMessageWriter` numbering fields; U5 for `tableMessageEntries`, `tableAnnouncement`, `TableMessageEntriesHere`, `TableMessageRefBitsHere` | `internal/codegen/gotable/message.go`, its `_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | +80 / +181 |
| C4 | `schema-go-cut-retain-runtime-uniform` | U3 for the `TableTypeInfo` retain hooks and the retain copies of the region bodies; U4 for `tableRetainPathDepth` | `internal/codegen/gotable/retain.go`, its `_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | +2930 / +55 |
| C5 | `schema-go-cut-region-runtime-uniform` | U3 for `TableNumbering`, `tableNumber`, `TableNodeMap` and the `tableRegion*` / `tableBuilderLoad` body variants | `internal/codegen/gotable/regions.go`, its `_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | C5-C8 jointly: 0 / +5058, then +953 more |
| C6 | `schema-go-cut-lists-runtime-uniform` | U3 for the `TableNodeMap.Carve` splice | `internal/codegen/gotable/lists.go`, its `_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | (joint, C5) |
| C7 | `schema-go-cut-textlist-runtime-uniform` | U3 for `TableTypeInfo.JsonArray` and the `tableJsonReadScalar` text-list branch | `internal/codegen/gotable/textlist.go`, its `_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | (joint, C5) |
| C8 | `schema-go-cut-messageregion-runtime-uniform` | U3 for the `tableMessageExtent` / `tableMessageLoadInto*` variants | `internal/codegen/gotable/messageregion.go`, its `_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | (joint, C5) |
| C9 | `schema-go-cut-cookwrite-runtime-uniform` | U5 for `tableCookBuildVersion`, the `tableCookWriter` variants and `tableCookRecords` | `internal/codegen/gotable/cookwrite.go`, `internal/codegen/gotable/cookwrite_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | +156 / +225 |
| P1 | `schema-go-cut-cook-open-at` | T3, plus U5 for `BuildVersion` (both are in `cook.go`) | `internal/codegen/gotable/cook.go`, its `_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | +194 / +194 + 783 |
| P2 | `schema-go-cut-table-read-entry` | T1 | `internal/codegen/gotable/read.go`, its `_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | 0 / 1408 |
| P3 | `schema-go-cut-fixed-root-entry` | T2 | `internal/codegen/gotable/fixedform.go`, `internal/codegen/gotable/fixedform_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | 0 / 1615 |
| P4 | `schema-go-cut-json-entry` | T4 | `internal/codegen/gotable/json.go`, its `_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | 0 / 871 |
| P5 | `schema-go-cut-message-write-entry` | T5 | `internal/codegen/gotable/messagewrite.go`, its `_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | 0 / 674 |
| P6 | `schema-go-cut-message-read-entry` | T6 | `internal/codegen/gotable/messageread.go`, its `_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | 0 / 555 |
| P7 | `schema-go-cut-table-write-entry` | T9 | `internal/codegen/gotable/write.go`, its `_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | 0 / 444 |
| P8a | `schema-go-cut-block-open-entry` | T7 | `internal/codegen/gotable/block.go`, its `_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | 0 / 324 |
| P8b | `schema-go-cut-block-build-entry` | T8 | `internal/codegen/gotable/blockbuild.go`, `internal/codegen/gotable/blockbuild_test.go`, `tables/goruntime/goruntime.go`, `generated/bench/**` | 0 / 368 |

"its `_test.go`" means the emitter file's own test file, or a new
`<file>_test.go` beside it. `tables/goruntime/goruntime.go` and
`generated/bench/**` are regenerated (`make go-runtime` and the
`make/go.mk` rules), never hand-edited. C1 changes only `runtime=shared`
output, so the bench trees stay as they are.

The C-cards share `tables/goruntime/goruntime.go`. It is a generated file,
and each card regenerates it, so it is not a hand-edited overlap. Two such
cards still conflict on merge, though, which is why the C-cards land in
order.

**DONE-WHEN for every card.**
1. The cut's declarations exist once, in `tables/goruntime` (for P1-P8, as
   one unit-level helper with per-table wrappers that keep their exported
   name, signature and doc).
2. `TestGoSharedRuntimePackageIsCurrent`,
   `TestGoSharedRuntimeIsOptionalAndEquivalent` and `internal/ci`
   `TestGoBenchTreesAreCurrent` pass.
3. The full gate of this card (STEP 5) passes at the card's head. That
   includes every `testing.AllocsPerRun` pin, unchanged.
4. `schema-go-generated-size-budget`'s budget constants for the one-table
   unit and `tables/examples` are lowered by the saving in the table above.
   Re-measure at the card's own base with that card's instrument, because
   the numbers here are at `19b833020`.
5. STEP 5b shows zero `--lang cpp` differences.

**STEP 5b for every card.** Build the compiler at BASE and at head. For every
`*.schema` in `git ls-files` at BASE, run `generate --lang go` and
`generate --lang cpp` with both binaries, keeping each exit status and its
stderr. The cpp trees are identical (`diff -rq` empty). The go trees differ
only in the files and declarations the cut moves. Exit statuses and stderr
are identical for both languages.

**What the cards do not reach.** After C1-C9:

- **The one-table unit** keeps 173 lines of per-unit variants: `init` blocks,
  small `var` tables, and `ProtocolId`, which is in `Pos.go`, the packet
  emitter's file (`internal/codegen/golang`), outside this survey.
- **`tables/examples`** keeps 89 lines between the cards' cumulative 8315
  (1645 + 6670) and the 8404 upper bound (the upper bound counts every
  unit-level declaration as uniform). These are single-digit-line
  declarations, including the view and block record tables (6 lines) and
  `ProtocolId` (13 lines), that the size-budget card can pick up one at a
  time.

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
- Savings are in lines. A line is not a byte, and not compile time, and none
  of those were measured here.
