# One read: the plan to cut the tolerant table read

Glenn, 2026-11-03: *"Cut the tolerant wire. It's too much work. It was a mistake
to try to do it."* And: *"That's my mistake. It's best to consistently refuse.
Too much undefined behavior per-language by trying to be tolerant."*

The rule is stated in `docs/SPEC-TABLES.md` §4, §6.5 and §13.10 and in
`docs/FIXED-FORM-ALGORITHM.md` §4.5, §4.6 and §5.4; `docs/rulings.json`
`strict-only-read` lists what it overturns. In one paragraph: every read of
every form returns the whole value or refuses the whole load with ONE reason.
Damage, a kind mismatch, a value out of range, a count or a length past its
bound, broken framing and a non-finite float each refuse. Evolution along a
lawful lineage is kept: an unknown field, variant, arm or slot is skipped and
counted `unknown` (or retained), an absent field takes its default, a widening
decodes exactly and counts `widened`, and the fixed form selects its plan by
hash. The report holds the one reason, or `unknown`, `widened`, `retained` and
`retain_lost`. The reason is fixed by the input: the envelope, then the
input's own well-formedness in byte order, then the declaration in byte order
(§6.5).

This page is the work list. The roadmap entry is `strict-only-read-2026-11-03`
in `docs/roadmap.sexp`, one task per layer below.

## Decisions taken in the spec, each for Glenn to confirm or overrule

1. **`clamped` is renamed `out_of_range` in place**, the same ordinal, as
   `non_finite` was renamed `non_finite_float`, because no read clamps. The
   typed error is `TableOutOfRange{Field}`.
2. **The variable, message and JSON reads use `count_over_bound` and
   `length_over_bound`** for a count or a length past the reader's bound (the
   fixed form's names), and `out_of_range` for a value. A text list's `| total`
   is `length_over_bound`.
3. **The reason order is three phases, then byte order** (§6.5): the
   envelope; the input well-formed by itself (`wire_damaged`,
   `count_over_extent_cap`, `blob_over_size_cap`, `non_finite_float`,
   `blob_shared`, and `malformed` on JSON); the declaration (`kind_mismatch`,
   `out_of_range`, the two bound reasons, `missing_required`). This replaces
   the counter ranking in `TableReport.Err()` (#2777's "damage outranks").
   A NaN after damage is now `wire_damaged` and a NaN before it
   `non_finite_float`; the float "reach" rule is gone.
4. **An unknown enum variant, union arm or keyed slot stays evolution**: the
   value reads `None` (the slot is dropped) and counts `unknown`, as an unknown
   field does. The alternative is to refuse it.
5. **A repeated field id, map key, keyed slot, node-table field and JSON key
   are damage** (Glenn's 2026-10-31 list of input no writer produces, held in
   #2557). The `duplicate` counter is retired.
6. **Bytes after a nested union inside its arm's `L` are damage** (the same
   list, item 5).
7. **An array, keyed or map header lies inside its own `L`**; a header that
   crosses it is damage whatever its element kind. This retires N3's
   header-against-the-parent rule.
8. **A table writer refuses a finite compressed float outside its range**
   (`invalid_value`) instead of the quantizer clamping it, and the fixed form's
   plan carries the WRITER's compressed-float range, which overturns
   schema#1164 statement B (2026-09-19, "reversible by Glenn").
9. **A lone or out-of-order surrogate escape in JSON refuses
   `string_lone_surrogate`** now, instead of decoding to `U+FFFD`, and the two
   JSONTestSuite `y_object_duplicated_key*` files refuse (a dialect row).
10. **Non-zero slack in a fixed record stays unspecified and is not refused**
    (§3.4): no reader reads it, so no leg can disagree about it.
11. **Settled after the cold read**: map order and repeats are judged at step 3
    as the reader decodes the keys, an absent key field at its default; a record
    no pointer reaches is damage; a record out of first-visit order is not
    (the numbering is positional); every leg answers `layout_unknown`, and
    `layout_newer` and `count_over_length` are retired.

## (c) The layers, in order, each with its gate

Each layer is secured before the one above it starts (the house's
build-up-from-the-foundation rule). A layer's gate is green on its own;
everything above it is red by design until its own layer lands.

| layer | what | gate |
|---|---|---|
| 0 | **The spec** (this branch): SPEC-TABLES, FIXED-FORM-ALGORITHM, SPEC, VERSIONING, LAYERS/1-framing, FIXED-FORM-VERSIONING-TESTS, rulings.json, this plan | A cold read with no memory of the drafting, its findings applied; Glenn's answers to the decisions above. It lands together with layer 2, because the spec-agreement tests read the page |
| 1 | **The models**: `tla/Framing.tla` (the inert body and the H3 crossing span removed; the header inside its body), a new `tla/ReadOrder.tla` (the three phases and byte order, invariant `OneReason`: two walks in different orders over one input name one reason), `tla/CardVersioning.tla` (`kindMis` and `clamp` become refusals) | TLC green on a bench machine on a small instance, a reversed witness per new invariant, `tla/RUNS.tsv` rows |
| 2 | **The Go engine and oracle**: `internal/tablewire`, `internal/tabletext`, `internal/tablecook`, the report (`tabletext.Report`) and `tables/goruntime` `TableReport` and `Err()` | `go test` of those packages; `test/conformance/harness` `TestFramingVectorsReadThroughTheEngine`; the spec-agreement tests (`compiler/specgo_*`, `internal/baseline` `TestEvolutionTableDocsAgreeWithTheGolden`, `compiler/specgo_refusal_enum_test.go`) green against this spec; a negative control per phase |
| 3 | **The harness and corpora** (inventory (b)), re-pinned from the engine | The harness's own tests; the Go leg (layer 4's first) green on every surface; `make conformance-pin` writes from the engine until the C++ leg lands, then from C++ again |
| 4 | **Each leg** (inventory (a)), in this order: go, cpp, c, cs, rust, java, js, dart, elixir, goose, moo, lua, luau, squirrel, lobster. Go first because it is the engine's twin; C++ second because it is the reference the pins come from; then the original nine; then the six newer ports | For the leg: every conformance surface green; its wire fuzz with zero divergences against the engine; its negative controls; no tolerant entry point in its generated output or in `internal/tablenames/<lang>.go` (a grep gate per leg) |
| 5 | **The docs that describe implementations**: USAGE, PORTING, CARDS, COMPARISON-TABLES, COMPETITION, CPP-REFERENCE-REVIEW, GO-GENERATED-SIZE-SURVEY, the PORT pages, SECURITY, HOSTILE-CORPUS, CONTRIBUTING, `test/conformance/README.md`, `testdata/conformance/tables/FORMAT.md` | A grep for the retired names (`Tolerant`, `clamped`, `kind_mismatch` as a count, `duplicate`, `malformed` as a binary verdict) over `docs/` is empty, and a cold read of each page |

Every generated tree (`generated/`, `testdata/golden/tables/`,
`tables/cards/<lang>/`, `internal/codegen/gotable/fuzzunit/`) is regenerated in
the layer whose emitter it comes from, never edited.

## (a) The tolerant surface, per leg

Line numbers are at `origin/main` 2f56f1130. "Hits" counts lines matching the
tolerant vocabulary outside tests, testdata and generated trees, comments
included, as a measure of size.

**Two shapes.** In the first (go, cpp, c, lua, luau, moo, squirrel, goose,
lobster) the leg has named `...Tolerant` entry points and its strict read is a
wrapper that ranks the report's counters over the tolerant load: the work is
to make the walk itself refuse at the first condition and delete the twin. In
the second (cs, dart, java, js, elixir, rust) the plain name IS the tolerant
read, returning a partial value and counters: the work is to make it refuse,
and those legs have no strict read to keep (#2778 adds one beside the tolerant
load and must become the only read instead).

### The Go engine and oracle (layer 2)

- `internal/tabletext/value.go:26` `Report`: `KindMismatch` 28, `Clamped` 35,
  `Duplicate` 36, `MissingRequired` 37, `Malformed` 38, `BlobShared` 42;
  `Add` 74, `Silent()` 115.
- `internal/tabletext/read.go`: clamps in `scanString`, `scanText`,
  `scanWString` (407, 424, 464) and `placeInteger` (1634); last-wins
  `Duplicate++` in `readTableKeys` (717, at 764) and `readKeyed` (1396, at
  1435); `KindMismatch++` skip-and-read-on at 782, 1046, 1148, 1241, 1341,
  1446, 1600-1731; count and length clamps in `readArray` (1185) and
  `readTextList` (1277). `internal/tabletext/texttotal.go:20` `KeepTextTotal`;
  `internal/tabletext/maps.go:87-91` `MapKeyFits`.
- `internal/tablewire/decode.go`: repeated id last-wins 182-189;
  `KindMismatch` skip 498, 741, 1002, 1257; `Clamped++` 563, 600, 795, 1039,
  1206 and 1419-1690 in `scalarAt` (1555, the 128-bit widen 1648, `bits(N)`
  1686); map last-wins `Duplicate++` in `mapField` (720, at 832); parent reads
  on past `L` at 551, 581, 805, 821, 986; `nested` (652) and the reach comment
  (649-650); the INERT body in `arrayBody` (952, at 974). `text.go:21`
  `textBoundary`, `:87` `wideBoundary`.
- `internal/tablewire/decodenodes.go:125` `resolveCell`: `KindMismatch` 142,
  156, 174; `BlobShared++` 167.
- `internal/tablewire/messagedecode.go`: `KindMismatch` skip 333, 658; clamps
  409, 446, 523, 607, 790-857; `clampFloat` 931.
- `internal/tablewire/retain.go:216`: records kept under last-wins.
- `ir/fixedform.go`: `TableFixedOpBool` 917, `TableFixedClampNeeded*`
  1940-1977.
- `internal/tablenames/*.go`: every leg's registered tolerant names and the
  `clamped`/`malformed` reason names.

### go (hits 548 in gotable, 107 in goruntime)

- `internal/codegen/gotable/read.go:175` `emitTableRead` emits
  `<T>LoadTolerant`; `:241` the plain `Load` and `:245` `ReadStrict` wrap it
  with `Err()`; `:114` the kind-mismatch skip; `:278-280` text clamp; `:320-322`
  element kind skip; `:355-357` count clamp; `:440` `emitReadUnion`; `:492`,
  `:510` `emitReadArm`; `:558` `emitReadScalar` (clamps 625, 634, 638).
- `internal/codegen/gotable/regions.go:235`, `:240` region `LoadTolerant`,
  `LoadBuilderTolerant`; `:377` `tableRegionMeasureReason(..., tolerant)`;
  `:379` `tableRegionLoad`; `:438` `tableBuilderLoad`.
- `internal/codegen/gotable/integrity.go:53`, `:58` integrity-checked twins.
- `internal/codegen/gotable/json.go:102`, `:111`, `:126`, `:150`
  `FromJSONTolerant`, `FromJSONTolerantWith`; runtime `scanText` 1380,
  `tableJSONTokenInteger` 1598, `tableJSONReadScalar` 1732,
  `tableJSONReadField` 2055 (Duplicate 2223), `tableJSONReadTableFields` 2348
  (2397), `tableJSONRequiredWalk` 275. `jsonwide.go:70` `tableJSONReadWide`.
- `internal/codegen/gotable/messageread.go:47`, `:50`
  `LoadMessagesTolerant`; `:61` `emitMessageCompatibility`; `:134` count clamp.
- `internal/codegen/gotable/fixedform.go:1228` `FixedLoadTolerant`; clamp
  emitters 1391, 1541, 1592, 1612, 1657. `fixedruntime.go:471` bool
  normalization; `:580` `tableFixedForgedBounds`; `:1292`, `:1462` the
  `KindMismatch` census.
- `internal/codegen/gotable/maps.go:87` `emitMapRead`, `:146`
  `tableJSONReadMap`; `codecs.go:101` `tableClampEnds`, `:987`
  `scalarRangeBounds`; `refusal.go:212` `tableTextClamped`, `:220`
  `tableTextMalformed`; `textform.go:1532` `tableTextFormReportError`.
- `tables/goruntime/goruntime.go:29` `TableReport`; `:338` `Err()`; `:485`
  `TableTextClamped`, `:493` `TableClampedField`, `:501`
  `TableTextMalformed`, `:510` `TableMalformedField`, `:584`
  `TableUTF8Clamp`; `:1031` `TableFixedRun` (bool 1086); `:1195`
  `TableFixedForgedBounds`.
- Tests: `loadmeasuretolerant_test.go` (delete), `testgenbounds.go:16-17`,
  `:246`, `:693-717` (the bound probes), 61 test files naming the tolerant
  read.

### cpp (hits 797)

- `internal/codegen/cpptable/codecs.go:2306` `emitTableRead` emits
  `<T>LoadTolerant`; the strict wrapper ranks counters at 2319-2327;
  `:2445` `emitTableReadField`, `:2787` `emitTableReadScalarFrom`.
- `pointers.go:1102` `LoadBuilderTolerant`; `:1295`
  `emitVariableReadStrict`; `:1775` `emitVariableLoad`.
- `fixedform.go:656` `FixedLoadTolerant`; `:844` `emitFixedLoadStrict`;
  clamp emitters 1030, 1065, 1092, 1109, 1175. `fixedruntime.go:361`
  `TableFixedApply` (clamped++ 388-486, bool 66/372), `:502` `TableFixedRun`.
- `json.go:739`, `:748`, `:769`, `:815` `FromJsonTolerant`; counter ranking
  793-797, 834-838; `:316` `tableJsonWalk` (duplicate 447, 4361, 4604).
- `messageload.go:733` `LoadMessages` (tolerant body); clamps 171, 247, 620,
  640.
- `widen.go:344` `emitScalarClamps`; `arms.go:297` `emitArmLoad`;
  `lists.go:797` `emitListReadField` (inert); `maps.go:1325`
  `emitMapReadField` (duplicate 1391); `arena.go:1072` `blob_shared++`;
  `text.go:92` `TableUtf8Clamp`, `:151` `TableUtf16Clamp`, `:1661` text-form
  counter mapping.
- `cpptable.go:817` `struct TableReport`; `:575-577` the enum members'
  comments.
- Tests: `test/tables/main.cpp` (263 mentions), `fixedform_main.cpp`,
  `maps_main.cpp`, `lists_main.cpp`, `hostile_main.cpp:89-113`.

### c (hits 443)

- `internal/codegen/ctable/wire_read.go:241-245` `load_tolerant`; `:272` plain
  `load`; `:87` `wireWidenedScalar`, `:293` `wireReadFieldPayload`.
- `graph.go:962-963`, `:979-980` region and builder tolerant loads;
  `:1005`, `:1009` `emitGraphLoadStrict`; `:364` kind mismatch on a node,
  `:384` `blob_shared++`.
- `json.go:45`, `:69` `from_json_tolerant`; `:117` `emitJsonDefinitions`;
  `graph_json.go:141-509` (key clamp as kind mismatch 495, duplicate 502).
- `message_read.go:386-387` `load_messages_tolerant`; `:183` clamps.
- `fixedform.go:707-710` `fixed_load_tolerant`; `:875` strict wrapper; clamp
  emitters 986, 1003, 1028-1111, 1198, 1213. `fixedruntime.go:438`
  `table_fixed_apply` (bool 78/450).
- `cook.go:124` `table_report_strict_refuses`; `ctable.go:389` `TableReport`;
  `wire.go:476` `table_wire_utf8_clamp`; `text.go:25` UTF-16 clamp;
  `codecs.go:108`, `:486`; `sequence.go:479`; `unions.go:249`; `maps.go:226`.
- Tests: `test/c-tables/fixedform_fx1.c`, `fx2.c`; 34 files in ctable.

### cs (hits 363; the plain name is tolerant)

- `internal/codegen/cstable/wire.go:353` `<T>Load` over runtime
  `TableWire.Load` (1716); `region.go:33`, `:35`; `builder.go:37`;
  `json.go:45` `FromJson` (runtime `Read` 1994); `message.go:85`;
  `fixedform.go:2040`, `:2223`, `:2226`.
- `cstable.go:677` `TableReport` (KindMismatch 680, Clamped 681, Duplicate
  687, BlobShared 693, ClampedField 697, Malformed 706).
- `wire.go:1273` `ReadScalar`, `:1308` `ReadElement`, `:1449` `ReadArm`,
  `:1506` `TextClamp`, `:1541` `ReadField`; `region.go:245`, `:281`, `:316`,
  `:425`, `:505` (inert); `json.go:1032`, `:1239`, `:1375`, `:1729`;
  `widejson.go:63`; `maps.go:55`, `:117`, `:146`; `variable.go:48`
  `BlobShared++`; `fixedbounds.go:48-387`; `fixedruntime.go:736`.

### rust (hits 675; the plain name is tolerant)

- `internal/codegen/rusttable/variablecodec.go:1818` `<t>_load`, `:1824`
  `<t>_load_retain`; `variabletext.go:1661`, `:1666` `<t>_from_json`;
  `messagebatch.go:731`; `fixedform.go:1273`.
- `message.go:859` `TableReport`; `variabletext.go:302` `Jr` counters;
  `fixedruntime.go:315` `TableFixedReport`.
- `variablecodec.go:942`, `:1009`, `:1156`, `:1257` (inert 1243);
  `mapsarms.go:593`, `:702`, `:799` (duplicate 909), `:966`, `:1367` (1395);
  `variabletext.go:386`, `:552`, `:604`, `:1378`, `:1528`; `variable.go:642`
  `blob_shared`; `fixedform.go:1545-1854` clamp emitters;
  `fixedruntime.go:547`, `:1430`; `messagebatch.go:940`.

### java (hits 483; the plain name is tolerant)

- `internal/codegen/javatable/variableform.go:3606` `load`, `:4132`
  `fromJson`; `messageform.go:2498`, `:2502`; `fixedform.go:1892`.
- `variableform.go:67` `Report`; `messageform.go:158`; `fixedruntime.go:529-538`.
- `variableform.go:4968` `writeFieldRead` (inert), `:5215`, `:5261`,
  `:5337`; JSON `In`: `:2836` `integer`, `:2946` `readString`, `:4271`
  duplicate, `:4458`, `:4729`; `variablemaps.go:151`, `:197`;
  `variableform.go:1489` `blobShared++`, `:1473` `judge`, `:1744` `cyclic`;
  `variableunions.go:294`; `variablewide.go:157`; `messageform.go:1098`,
  `:2057`, `:2230`; `fixedruntime.go:761`, `:954`, `:1790`; bool
  `fixedform.go:1255`, `:1387`.

### js (hits 379; the plain name is tolerant)

- `internal/codegen/jstable/variableform.go:1161` `<T>Load`, `:1168`
  `<T>FromJson`; runtime `TableVariableLoad` (`variableruntime.go:633`),
  `TableVariableFromJson` (`:2022`); `messagebatch.go:948`;
  `fixedmodule.go:394`.
- `variableform.go:243` `TableVariableReport`; `fixedruntime.go:184`.
- `variableruntime.go:877`, `:966`, `:1043`, `:1157` (inert 1122), `:1215`,
  `:741` (blobShared 760), `:2088`, `:2333`, `:2467`, `:2516`;
  `variablearms.go:516` (554), `:571`, `:681`, `:861` (874, 903);
  `textlist.go:34`; `fixedruntime.go:458`, `:1126`; `fixedjs.go:1048`.

### dart (hits 531; the plain name is tolerant)

- `internal/codegen/darttable/wireform.go:3174` `<t>WireLoad`;
  `textform.go:1134` `<t>FromJson`; `fixedmodule.go:637`;
  `messageform_batch.go:168`; `retainwire.go:583`.
- Reports: `wireform.go:556`, `textform.go:132`,
  `messageform_announce.go:129`, `fixedruntime.go:247`.
- `wireform.go:552`, `:2386` (blobShared 1395, 1406); `wireunion.go:513`
  (inert 543, 613), `:719`, `:1102`; `wirewide.go:96`; `textform.go:129`,
  `:963`; `textmore.go:283`; `textlists.go:103`;
  `messageform_batch.go:714`, `:857`; `fixeddart.go:1175`, `:921`, bool `:335`.

### elixir (hits 564; the plain name is tolerant)

- `internal/codegen/elixirtable/variableform.go:1429` `load/2`, `:3594`
  `from_json/2`, `:5879` `load_retain`; `messageform.go:135`;
  `fixedelixir.go:1826`.
- `variableform.go:3221` `malformed_report`, `:3236` `empty_report`, `:5062`
  `merge_report`; `:2068` `decode`, `:2202-2420` `read_payload`, `:2585`
  `read_arm`; `:2077`, `:2121` the `:inert` body; `:2458` (duplicate 2497;
  JSON 4198, 4363, 4519); `:4734`, `:4790`, `:4719`, `:4967`; retain twins
  `:6370`, `:6796`, `:7162`; `fixedruntime.go:275`, `:313`;
  `fixedelixir.go:817`, `:1831`, bool `:1022`.

### goose (hits 470)

- `internal/codegen/goosetable/fixedform.go:1130` `FixedLoadTolerant`,
  `:1158`; `:697` `clampField`, `:712` `clampValue`, bool `:620`.
- `json_form.go:659` `FromJsonTolerant`, `:666`.
- `runtime/wire_load.goose:57` `LoadReport`, `:123` `load_error` (ranking),
  `:593`, `:700`, `:1392` (duplicate), `:1919`, `:1985`.
- `runtime/schema_json.goose:492`, `:511`, `:522`, `:970`;
  `runtime/json_value.goose:2009`, `:2128`; `messageform.go:576`, `:674`,
  `:1155`; `runtime/message.goose:62`; `runtime/fixed.goose:91`.

### moo (hits 375)

- `internal/codegen/mootable/runtime/table_load.moo:1924` `L.loadTolerant`,
  `:1745` `L.loadOpened`, `:2235` `L.ReadStrict`, `:2325`, `:2331`; `:329`,
  `:560`, `:874`, `:1060`, `:1271`, `:1405`, `:1724`.
- `runtime/table_refusal.moo:29` `R.ReadStrict` (ranking around 71);
  `runtime/table_values.moo:250`, `:261`; `runtime/json.moo:3339` `J.fromJson`
  (tolerant), `:1635`, `:2338`, `:2377`, `:2839`, `:3054`, `:3325`;
  `runtime/table_fixed.moo:505`, `:1040`, `:1145`;
  `runtime/table_message.moo:1560`, `:2064`; `internal/tablenames/moo.go:50-60`.

### lua (hits 394)

- `internal/codegen/luatable/runtime/load.lua:1968` `L.LoadTolerant`, `:2119`
  `strict_refusal`, `:2147` `L.Load`; `:490`, `:656`, `:857`, `:1054`,
  `:1150` (duplicate), `:1589`, `:1778`, `:1840`; inert 1010, 1543.
- `runtime/json.lua:2251` `J.FromJsonTolerant`, `:2295`; `:736`, `:1106`,
  `:1161`, `:1439`, `:1626`, `:1254`, `:1758`, `:1857`.
- `fixedform.go:402` `ReadFixedTolerant`; `messageform.go:102`
  `LoadMessagesTolerant`; `runtime/message.lua:981`; `runtime/refusal.lua:96`
  (table 24-28); `runtime/textform.lua:689`; `runtime/fixed.lua:482`.

### luau (hits 432)

- `internal/codegen/luautable/runtime/schema_load.luau:1762` `L.loadTolerant`,
  `:2183` `L.ReadStrict`, `:2256` `L.load`, `:2244`, `:1790`; `:571`, `:580`,
  `:713`, `:837`, `:938` (inert 890), `:1270`, `:1520`.
- `runtime/schema_json.luau:3084` `J.FromJsonTolerant`, `:3111`; `:622`,
  `:936`, `:1277`, `:1520`, `:1901`.
- `runtime/schema_fixed.luau:974` `F.loadTolerant`, `:1075`, `:416` bounds,
  `:922-931` census; `runtime/schema_message.luau:628`, `:686`;
  `runtime/schema_retain.luau:763` `R.LoadRetainTolerant`;
  `runtime/schema_refusal.luau:78`; `runtime/schema_textform.luau:1179`;
  `internal/tablenames/luau.go:33-41`.

### squirrel (hits 343)

- `internal/codegen/squirreltable/runtime/load.nut:1329` `_M.loadTolerant`,
  `:1534`, `:1526`; `:263` `clampScalar`, `:434`, `:613`, `:823` (inert),
  `:1283`.
- `runtime/json.nut:2388` `_M.FromJsonTolerant`, `:2411`; `:1631`, `:1824`,
  `:1948`, `:2122`, `:2170`, `:2190`, `:2337`.
- `fixedform.go:306` `FixedLoadTolerant`, `:389`; `runtime/refusal.nut:239`,
  `:223`; `runtime/message.nut:235`, `:307`, `:432`, `:643`;
  `runtime/document.nut:1200`; `internal/tablenames/squirrel.go:16-19`, `:80-84`.

### lobster (hits 445)

- `internal/codegen/lobstertable/runtime/load.lobster:2041` `load_tolerant`,
  `:2058` `strict_reason`, `:2085` `load`; `:31` `LoadReport` (33-38); `:606`,
  `:967`, `:1224` (inert), `:1535`, `:1630`.
- `runtime/message.lobster:1093` `load_messages_tolerant`, `:1142`, `:1077`;
  `fixedevolve.go:1432` fixed `LoadTolerant`, `:1419`, `:823`;
  `runtime/fixedevolve.lobster:1285`, `:1220` (bool), `:1312`;
  `runtime/json.lobster:2361` `json_read` (tolerant), `:1081`, `:1972`,
  `:2001`, `:2248`; `runtime/text_form.lobster:598`;
  `internal/tablenames/lobster.go:73-150`.

## (b) The harness, corpora and tests that encode tolerant answers

| surface | where | what it encodes | size | disposition |
|---|---|---|---|---|
| `report` rows | `testdata/conformance/tables/MANIFEST.txt` 238-395, 867-872, 992-1008, 1119-1176; `reports.txt` 9-70; `report-values/` | six counters plus a verdict, and a decoded value beside kind mismatch, clamp and damage | 62 rows: 24 kind_mismatch, 4 kind_mismatch with clamp, 3 clamp, 2 damage with a value, 4 refusals with no reason, 25 evolution; 56 value files | re-pin to `<case> <reason>` or the evolution counters and value; 31 value files deleted |
| `framing` rows | MANIFEST 1178-1294; `testdata/wire/tables/framing/` (98 `.bin`) | `malformed` verdicts, 4 INERT rows, 2 last-wins rows, 6 kind mismatches, 1 clamp | 98 rows | `wire_damaged` for damage, INERT and last-wins rows become `wire_damaged`, header-crossing rows `wire_damaged`; bytes kept |
| `json-hostile` rows | MANIFEST 643-911; `json-hostile/` (155 trees, 120 value files) | accept-and-count: duplicates, kind mismatches, clamps; `refused` with no name; `carried-by`/`uncarried` | 155 rows: 16 duplicate, 24 kind_mismatch, 18 clamp, 35 unnamed refusals, 5 uncarried, 60 keep | re-pin to the reason; 60 value files deleted; the `uncarried` mechanism retired once every leg carries the rows |
| strict-reason | `strict-reasons.txt`, `strict/` (on #2762/#2777, not main) | already one reason per wire | 164 rows, 8 of them `clamped` | KEEP and merge; rename the 8 rows; it becomes the report surface's answer |
| harness code | `test/conformance/harness/manifest.go:69-118`, `:146-163`, `:605-623`; `run.go:305-315`, `:382-441`, `:782`; `generate.go:214-287`; `wirefuzz.go:1`, `:375`, `:541-553`, `:630-668`, `:697-704` | the `Counts` struct, the six-counter parse, the pipe reply's counters and malformed byte, the reach rule, the oracle's partial read | — | re-pin to a reason; the pipe carries the reason (or its ordinal) and the evolution counters; the reach rule deleted |
| harness tests | `wirefuzz_test.go:189` (malformed is not a refusal: delete), `:203`; `wirefuzz_mapheader_test.go:22`; `wirefuzz_timestamp_bool_test.go:44`; `framing_vectors_test.go:240-393`; `maps_test.go:106-315`; `messagerules_test.go:421-533`, `:746-793`, `:1005-1057`, `:1397`; `unbounded_oracle_test.go:231`; `illformedatbound_test.go:14`; `feature_witness_native_test.go:91` | counters, clamps, last-wins, inert | about 20 tests | re-pin each to its reason |
| fuzz vectors | `testdata/wire/tables/fuzz-vectors/INDEX.txt` (367 lines) | the 33 `reach_*` vectors and about 20 last-wins, inert, read-on and clamp vectors | 98 vectors | the reach family collapses to about 8 phase-order vectors; the rest re-pinned |
| hostile corpus | `internal/ci/hostilecorpus/corpus/manifest.tsv`; `docs/HOSTILE-CORPUS.md:91`, `:127-135`; `internal/ci/schema_{lua,luau,squirrel}_runs_hostile_corpus_test.go` | 57 accepts mirroring tolerant json-hostile rows, 42 `damaged` reasons | 171 rows | re-pin to `refuse` with the reason; runners switch to the plain read |
| table-base64 | `test/table-base64/harness/main.go:32-135`; drivers `go/main.go:28-37`, `driver.c:38-42`, `cs/Program.cs:15-18` | 4 clamps, 34 kind_mismatch, column compare of counters | about 990 vectors | a reason column; drivers on the plain read |
| JSONTestSuite | `docs/SPEC-TABLES.md` §16.12; `internal/jsontestsuite/policy.go`; `testdata/jsontestsuite/PENDING.tsv` | duplicate keys accepted, lone surrogates pending | 2 `y_` files, 10 `i_` surrogate files | two `refuse (dialect)` rows; the surrogate rows are met |
| testgen | `internal/codegen/gotable/testgenbounds.go`; `compiler/testgen_luau.go:338-342`, `:410`, `:487` | the bound probes' tolerant read and clamped value | one generator | the probes refuse by reason (§16.11) |
| evolution goldens | `internal/baseline/frames_test.go:110`, `:163`; rows 206-570 | read cells as six counters | 38 rows, 12 tolerant | read cells become a reason; `TestEvolutionTableDocsAgreeWithTheGolden` reads §4.1 |
| fixed-form versioning | `docs/FIXED-FORM-VERSIONING-TESTS.md`; `test/tables/fixedform_dump.cpp:933-992`; `test/tables/versioning_numbers.cpp:223`, `:763`; `versioning_lists.cpp:431`, `:598`; `fixedform_main.cpp`, `fixedform_properties.cpp`, `fixedform_hostile_bytes.cpp`; `test/c-tables/fixedform_fx1.c`, `fx2.c`; 102 per-leg `fixedversioning_*_test.go` | clamp counts, census, bool normalization | families `count_clamp_ends` x10, `cfloat_range_widen` x10, `forged_ordinal_both_plans` x8, `union_tag_both_plans` x10, `writer_bound_count` x7, `writer_bound_count_two_endpoints` x10 | re-pin to refusals |
| sabotages | `tools/sabotage/message.go:100`, `:107`, `:277`, `:311`, `:348`, `:378`, `:393`; `main.go:138` | clamp guards | 8 of 86 | delete the clamp ones, re-pin the narrow-before-bound pair |
| negative controls | `make/negative-controls.json`; Makefile 2184, 2207, 2624, 2981, 3971, 3986, 4470; `make/rust.mk:1355`; `make/elixir.mk:290`, `:294`; `make/java.mk:1931` | clamp prefix, key clamp, duplicate overlay, count clamp | about 10 of 702 | delete or re-pin |
| models | `tla/Framing.tla:102-110` (inert), `:128-139` (H3 span); `tla/CardVersioning.tla:47-146`; `tla/README.md:226-280`; `tla/RUNS.tsv:13-17` | inert body, read-on, kind and clamp counters | 2 models | layer 1 |
| per-leg tests | 462 test files outside goldens name the tolerant read | — | go 61, lua 45, luau 39, compiler 38, c 34, squirrel 33, test/tables 26, conformance drivers 63, others | triage in each leg's layer; the five tests named `*Tolerant*` are deleted |
| conformance README | `test/conformance/README.md:165-211`, `:477-502` | the six counters, the tolerant text read, the pipe layout | — | layer 3 |

## (d) The open pull requests

Sixty-three open PRs by `rowan-claude` and `gafferongames` (the two dependabot
PRs are outside this list). `dev` is 2,353 commits behind main and every
`sprint/*` base 3,800 to 4,600; a PR on either base is stale whatever it
carries.

| PR | branch | class | why |
|---|---|---|---|
| #2788 | rowan/storage-cap-hardening | keep | storage-cap tests under a memory ceiling and 64-bit stored sizes; stacked on #2787 |
| #2787 | rowan/storage-cap-all-legs | keep | `storage_over_cap` is a refusal before any build; stacked on #2784 |
| #2786 | rowan/fuzz-round-2 | rework to refuse | keep the orphan-cycle and retained-id-0 fixes; drop the repeated-map-key slot handling and the superseded-occurrence retain logic; stacked on #2780 |
| #2785 | rowan/cook-write-scripting | keep | writer side: the cook carries unbounded text |
| #2784 | rowan/limit-followups | rework to refuse | depth and timestamp limits stand; its kind_mismatch-count fix and the map entry kept at default on a wrong shape now refuse; stacked on #2779 |
| #2783 | emma/certify-cpp-cs-20261103 | keep | C++ format ratchet and C# JIT warmup |
| #2782 | emma/certify-wfr-java-dart-elixir-20261103 | rework to refuse | Dart count-cap order and Java absent spill stand; the wire-fuzz `!report.Malformed` guard leans on the retired flag |
| #2781 | emma/cpp-go-missing-required-reason-20261103 | keep | `missing_required` at ordinal 25 in C++ and Go strict reads; drop the `!malformed` condition on rebase |
| #2780 | rowan/fuzz-lua-luau-squirrel | rework to refuse | keep the LEB length wrap and region alignment fixes; drop the repeated-key-widen reset and widening across `L` |
| #2779 | rowan/limit-vectors | rework to refuse | the at and over limit vectors stand; restate each over-limit answer as one reason |
| #2778 | rowan/strict-read-six | rework to refuse | adds a strict read BESIDE the tolerant load in cs, java, js, rust, dart and elixir; it must become the only read, with the phase order and `out_of_range`; stacked on #2762 |
| #2777 | rowan/strict-reasons-conform | keep | every leg names damage `wire_damaged`, exactly this spec; its SPEC-TABLES hunks are superseded by this branch; stacked on #2762 |
| #2776 | emma/certify-release-tooling-20261103 | keep | release-gate Makefile fix; its Java hunk fixes a loader for a clamp-counter test the harness layer deletes |
| #2775 | rowan/main-reds-20261103 | rework to refuse | the re-pins and the Lobster cycle fix stand; the Elixir `bits(N)` clamp must refuse `out_of_range` |
| #2771 | rowan/moo-tagged-green | keep | MOO cook writer and tagged CI gates |
| #2762 | rowan/harness-reasons | keep | the strict-reason surface and its cross-leg gate: the harness of the one read; lands first |
| #2749 | rowan/fixed-range-clamp-vector | rework to refuse | keep `hostile_range_widen.bin`; its answer is `out_of_range`, not a clamp to the writer's range; the PR body is stale |
| #2731 | codex/schema-lobster-readiness | drop | its purpose is last-wins on a repeated map key, which is damage |
| #2576 | rowan/c-cpp-hostile-runners | keep | C and C++ hostile-corpus runners under sanitizers; needs a rebase |
| #2560 | rowan/remove-wstring | keep | wstring retirement (Glenn, 2026-10-31); draft, needs a rebase |
| #2559 | rowan/go-cycle-damage-v2 | keep | Go refuses a pointer to an open node; retarget to main (its base #2551 merged) |
| #2557 | rowan/no-writer-input | rework to refuse | stopped by Glenn; its items (repeats are damage, `duplicate` retired, nested-union trailing bytes) are decisions 5 and 6 here; fold its engine and C++ commits into layers 2 and 4 |
| #2556 | rowan/rust-discard | drop | Rust discard already on main (82e6c1aa7) |
| #2544 | codex/moo-block-leads | keep | MOO block base alignment gate |
| #2543 | codex/moo-block-forgery | keep | MOO block forgery bounds |
| #2529 | rowan/message-announce-trim | keep | the announcement carries the message closure alone |
| #2184 | rowan/coverage-luau | drop | base `dev`; covered on main by other routes |
| #2183 | rowan/coverage-java-retain | drop | base `dev`, stale; re-file any retain-save fix main still lacks |
| #2182 | rowan/coverage-lua | drop | base `dev`; on main by other routes |
| #2180 | rowan/coverage-java-cook | drop | base `dev`, stale |
| #2179 | rowan/coverage-dart-2 | drop | base `dev`, stale |
| #2178 | rowan/nightly-conformance | drop | base `dev`; `nightly-conformance.yml` is on main |
| #2177 | rowan/coverage-elixir | drop | base `dev`, stale, and its arm-length read is tolerant |
| #2174 | rowan/certify-green | drop | base `dev`; fixes an alias of a retired tolerant entry point |
| #2173 | rowan/coverage-dart | drop | base `dev`, stale |
| #2172 | rowan/coverage-go | drop | base `dev`; re-file the `+Nr` label if still wanted |
| #2171 | rowan/coverage-java | drop | base `dev`, stale |
| #2170 | rowan/coverage-js | drop | base `dev`, stale |
| #2161 | rowan/caa-logins-only | drop | base `dev`, held; `caa-self-test` is on main |
| #2135 | sprint/schema-js-generated-checkjs-strict-eslint-gate.w4.g2.e21 | drop | base `sprint/schema-js`, stale |
| #2133 | sprint/shared-base-forward-merge-schema-go.w2.g1.e21 | drop | a sprint forward merge on a dead base |
| #2131 | sprint/squirrel-base-forward-merge-schema-go.w3.g1.e21 | drop | a sprint forward merge on a dead base |
| #2129 | sprint/schema-go-testgen-fuzz-target-per-table.w6.g1.e21 | drop | base `sprint/schema-go`, stale, held |
| #2127 | sprint/rust-base-forward-merge-schema-go.w1.g1.e21 | drop | a sprint forward merge on a dead base, held |
| #2118 | sprint/schema-go-json-unknown-key-strict.w7.g1.e21 | drop | stale base; the unknown-key path idea is evolution and can be re-filed |
| #2114 | sprint/schema-go-unlocked-builder-write-refuses.w9.g1.e21 | drop | stale base; writer side, re-file if wanted |
| #2112 | sprint/schema-go-json-unknown-key-strict.w6.g1.e21 | drop | an earlier attempt of the same card |
| #2109 | sprint/schema-go-json-unknown-key-strict.w5.g1.e21 | drop | an earlier attempt |
| #2099 | sprint/schema-go-json-unknown-key-strict.w4.g1.e21 | drop | an earlier attempt |
| #2096 | sprint/schema-go-json-unknown-key-strict.w3.g1.e21 | drop | an earlier attempt |
| #2092 | sprint/schema-go-testgen-fuzz-target-per-table.w1.g1.e21 | drop | superseded by #2129 |
| #2090 | sprint/schema-go-json-unknown-key-strict.w1.g1.e21 | drop | an earlier attempt, framed as strict and tolerant |
| #2086 | sprint/schema-go-unlocked-builder-write-refuses.w7.g1.e21 | drop | an earlier attempt of the arena card |
| #2082 | sprint/schema-go-unlocked-builder-write-refuses.w6.g1.e21 | drop | an earlier attempt of the arena card |
| #2050 | rowan/roadmap-shared-generator | keep | the roadmap generator move; waiting on a cold read |
| #2015 | rowan/base-reds-20261016 | drop | describes reds on the dead sprint bases |
| #2004 | sprint/elixir-generated-code-passes-credo-at-default-strictness.w2.g1.e21 | drop | stale base, red |
| #1993 | rowan/make-siblings-dev | drop | base `dev`, conflicting |
| #1990 | sprint/elixir-carries-payload-free-arms-on-the-table-wire.w2.g1.e21 | drop | stale base |
| #1988 | sprint/elixir-answers-the-guard-kind-and-scalar-version-units.w7.g1.e21 | drop | stale base |
| #1943 | fix/c-fixedform-harness-run-signature | drop | base `dev`; carries a clamp-count signature |
| #1933 | stella/schema-java-oracle-20261014 | drop | stale draft; its range-conflict topic is now `out_of_range` |
| #1906 | roadmap/github-cleanup-2026-10-14 | drop | a stale snapshot |

Sixteen keep, nine rework to refuse, thirty-eight drop. **#2762 and #2777
are the base of layer 3**; #2778 is rebuilt on them as the only read in the
six legs that have none; #2557's engine and C++ commits and #2781 belong to
layers 2 and 4. The limits stack #2779 → #2784 → #2787 → #2788 is sound and
needs only its over-limit answers restated. Closing the `dev` and `sprint/*`
PRs is Glenn's call and is not done by this branch.
