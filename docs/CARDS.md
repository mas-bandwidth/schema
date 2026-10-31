# The cards page

`tables/cards` is the schema stream's work-card record: a typed declaration
(`Card.schema`) plus a thin handwritten facade that turns one wire buffer into a
`*Record` a customer can read, edit and hash. This page documents the facade a
customer imports; it is not a version declaration and it does not order
releases.

## The declaration

The current declaration, verbatim from `tables/cards/Card.schema`:

```go
// Card.schema — the CARDS corpus (docs/SPEC-TABLES.md §2): a typed work card
// plus one opaque body. Every bounded scalar and string field is declared at
// its exact bound; the four collections ride as lists of table elements, and
// the body is an optional unbounded string, deliberately outside the declared
// bounds (docs/SPEC-TABLES.md §2.9). Customer policy — the body byte ceiling
// and the collection-entry ceiling — lives in the owning facade, not here.
package cards

enum Tier { flash, pro, heavy, frontier }

enum Priority { normal, high, critical, blocker }

enum Column { waiting, ready, working, review, merging, landed, plan, check }

// PathText and CardID are the table-element spelling for a bounded string
// element: this compiler refuses a direct []string(N), so a list's element is
// a table holding the string (docs/SPEC-TABLES.md §2.9).
table PathText
{
    s string(192)
}

table CardID
{
    s string(64)
}

// KeyValue is the extra-header entry: an unrecognized header key rides here in
// arrival order, its optional value an unbounded string like done_when and
// source (docs/SPEC-TABLES.md §2.5).
table KeyValue
{
    key   string(32)
    value *string
}

// Consumer is one entry of a card's consumers collection: a route's processing
// lane and its outcome and cost.
table Consumer
{
    kind          string(16)
    who           string(32)
    route         string(48)
    model         string(64)
    started       int64
    ended         int64
    outcome       string(16)
    line          string(200)
    cost_micro_usd int64
}

table Card
{
    id               string(64)
    sha              string(40)
    tier             Tier
    repo             string(96)
    _base            string(96)
    kind             string(24)
    priority         Priority
    stream           string(32)
    paths            []PathText
    _new             []PathText
    shared           []PathText
    needs            []CardID
    test             string(192)
    done             *string | was = "done_when", json = "done_when"
    deadline_minutes int32       | min = 0, max = 100000
    source           *string
    epoch            int32
    column           Column
    holder           string(32)
    attempt          int32
    head             string(40)
    branch           string(128)
    body             *string
    extra            []KeyValue
    consumers        []Consumer
}
```

`done` is the append-default field: a wire written before the field existed
reads as absent, and its `was = "done_when"` alias lets an older text form name
it. `was` records a rename; the declaration carries no hash and does not claim
that one declaration orders another. Version order lives in the corpus
(`tables/cards/corpus`, `tables/cards/testdata`), not in this file.

## Import and module setup

The generated package is a local module named `cards`, produced by
`make cards-generate` into `build/tables-generated-go/cards`. A customer
imports it as:

```go
import "cards"
```

Its `go.mod` requires the serialize.go runtime and the two historical units and
replaces them with checkout-relative paths. The facade itself compiles with the
Go standard library only; a customer needs `github.com/mas-bandwidth/serialize.go/v2`
resolvable because the generated codecs call it.

For adopting generated tables into a Go module of your own — the pinned
`go:generate` line, when a `go.mod` needs a dependency at all, and a save and
a load of the variable and the fixed class — see "Tables from Go" in
[USAGE.md](USAGE.md#tables-from-go).

## The facade

`Record` privately owns a defensive copy of the wire. `(*Record).Wire() []byte`
returns another copy, so a caller can never mutate stored bytes and no raw
generated `*Card` or shallow relative-pointer root escapes (docs/SPEC-TABLES.md
§6.2).

```go
rec, report, err := cards.Load(wire)   // tolerant read, original wire preserved
strict, err := cards.ReadStrict(wire)  // refuses unknown/lossy data
json, report, err := cards.ToJSON(rec) // known-field view
rec2, err := cards.FromJSON(json)      // checked reconstruct, no loss events
text, report, err := cards.Render(rec) // informational text, body verbatim
out, report, err := cards.Upgrade(wire)// retained-region rewrite, idempotent
rec3, err := cards.Parse(brief)        // admitted brief text -> typed Record
```

`Load` never rewrites: it copies the input and reports tolerant events in the
generated `Report` alias. Malformed or failed loads refuse; a `Report` counter
is a count, not a field name. `ReadStrict` rejects unknown or lossy reads on top
of `Load`; it does not detect writer chronology, and a compatible newer wire may
carry zero unknowns. `Upgrade` is not implicit in `Load`: it runs the
retained-region path, accumulates one report, and refuses retention loss, kind
mismatch, clamp, malformed input or any failed operation. `Unknown`, `Retained`
and `Widened` alone are not loss. A shipped historical fixture upgrades, keeps
its values and body, and is idempotent.

`Render` preserves the stored body suffix byte for byte and inserts no extra
separator; text and JSON are informative known-field views with read reports,
never lossless machine-state persistence. Store `Record.Wire()` or successful
`Upgrade` bytes when a caller needs the bytes back.

## Editing and hashing

Six setters edit a record. Each validates its input, allocates a complete
replacement, serializes it, and only then swaps the owned wire; a refusal leaves
the original bytes unchanged.

```go
err = cards.SetNeeds(rec, []string{"dep-alpha"})       // 64-byte elements
err = cards.SetPaths(rec, []string{"tables/cards"})    // 192-byte elements
err = cards.SetTier(rec, cards.Tierpro)                // generated enum value
err = cards.SetPriority(rec, cards.Priorityhigh)       // generated enum value
err = cards.SetColumn(rec, cards.Columnworking)        // generated enum value
err = cards.SetHolder(rec, "runner")                   // string, 32 bytes
```

Enum spellings are the generated constants in package `cards` (`Tierflash`,
`Tierpro`, `Tierheavy`, `Tierfrontier`; `Prioritynormal`, `Priorityhigh`,
`Prioritycritical`, `Priorityblocker`; `Columnwaiting` through `Columncheck`).
Ordinary setters refuse unknown or lossy input before any builder rewrite. The
bounds are `MaxBodyBytes = 64<<10` and `MaxListEntries = 512`, plus each
element's declared string bound. An invalid write refuses rather than truncates.

`TaskHash` hashes the task-bearing fields only: body, paths, new, shared, test
and done, framed so boundaries cannot collide and excluding needs, tier,
priority and machine fields.

```go
sum, err := cards.TaskHash(rec) // [32]byte; refused for a lossy record
```

## The external customer

`tables/cards/example` is a separate Go module, `cardsexample`, that imports
only `cards` and the standard library. It is a source-template module boundary,
not a published module: the make target stages it into `build/cards-example`,
materializes its `go.mod`, and runs it. A customer import proves local adoption
of generated code, not a `github.com/.../tables/cards` distribution promise.

The checkout-relative replaces are materialized from absolute paths because Go
does not inherit replace directives from module `cards`:

```go
replace cards => <checkout>/build/tables-generated-go/cards
replace github.com/mas-bandwidth/serialize.go/v2 => <checkout>/../serialize.go
```

`make cards-example` is the runnable command. Its test runs the marked example
below (the page test holds the two in step), plus the read/edit/upgrade and
lossy-probe assertions. This is the recipe a later sprint integration follows:
parse briefly at admission, keep machine fields server-owned, index the
authoritative record, preserve the wire, and hash the task-bearing fields.

```go
// BEGIN CARDS-CUSTOMER-EXAMPLE
	rec, err := cards.Parse([]byte(
		"RESULT: probe-card sha=0123456789ab tier: pro\n" +
			"REPO: mas-bandwidth/schema\n" +
			"PATHS: tables/cards\n" +
			"DEPENDS-ON: dep-alpha\n" +
			"TEST: TestCardsCustomerExample\n" +
			"\ndistinctive body line\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// END CARDS-CUSTOMER-EXAMPLE
```

## Corpus and make targets

The corpus is fixed historical evidence. `tables/cards/corpus` and
`tables/cards/testdata` hold frozen declarations and named wire/JSON fixtures
read-only: tests never rewrite them and CI does not regenerate missing
historical evidence. Fixture creation is deliberate and separate:
`make cards-corpus-create` writes evidence under an explicit update flag and
refuses to overwrite an existing named case.

Existing targets:

- `make cards-generate` — generate the local module and copy the named facade/test files.
- `make cards-test` — check the declaration, vet and test the generated module.
- `make cards-example` — stage and run the separate `cardsexample` customer module.
- `make cards-corpus-check` — pin the current declaration and require every SHIPPED case.
- `make cards-corpus-create` / `make cards-fuzz-fixture` — author fixtures under an explicit flag.
- `make cards-java-test cards-java-conformance` — the Java facade (`tables/cards/java`): the same shared corpus load/save/render/parse round-trips, and the upgrade, edit, task-hash and view walks, pinned by conformance and the negative controls.
- `make cards-conformance-go` — run the Go reference driver across all nine conformance surfaces (`load,save,render,parse,bounds,upgrade,edit,taskhash,view`).
- `make cards-conformance-all` — run `cards-conformance-go` and every language leg's `cards-<lang>-conformance` target (`c`, `cpp`, `cs`, `rust`, `java`, `js`, `dart`, `elixir`, `lua`) with `--require-surfaces load,save,render,parse,bounds,upgrade,edit,taskhash,view`, printing the 90-line leg-by-surface matrix.

## Checked TLA+ models

Two finite-state TLC models check the facade's upgrade, edit, and cross-language hop contracts (`make tla-card-versioning` and `make tla-card-hops`):

- `tla/CardVersioning.tla` models the atomic retained round trip (`Upgrade`) and strict read (`ReadStrict`) across `Legacy`, `Current`, and `Compatible` declarations:
  - `TypeOK` — record slots and the last-action observation remain within their finite domains.
  - `NoFieldLost` — after a successful `Upgrade`, every pre-attempt logical field identity and value survives in the output's known fields or retained tail (`was` renames preserve identity).
  - `BodyUnchanged` — `Read`, `ReadStrict`, and `Upgrade` (whether published or refused) preserve the stored body byte-for-byte; only `Write` replaces it.
  - `RefusalKeepsOriginal` — a refused `Upgrade` publishes no replacement and leaves the original slot untouched.
  - `UnknownIsReported` — physically present unknown fields produce the exact `unknown` count on the report; a compatible writer label alone forces no unknown count.
  - `ShippedUpgradeSucceeds` — every shipped upgrade triple succeeds, and repeating `Upgrade` on the same declaration is idempotent (`RepeatUpgradeStable`).
  - Four reversed fault configs (`DropRetained`, `SuppressUnknown`, `ChangeBody`, `RefuseShipped`) prove each invariant bites when violated.
- `tla/CardHops.tla` models a card record hopping across two language implementations (`A` and `B`) and declarations (`Older` and `Current`) under `Write`, `Read`, `Upgrade`, `Edit`, and `Hash`:
  - `TypeOK` — the record and observation remain within their finite domains.
  - `NoFieldLost` — every field identity and value present before an `Upgrade`, `Read`, or refused `Edit` survives; a successful `Edit` mutates only its target field and refuses when any unknown field is present.
  - `BodyUnchanged` — `Read`, `Upgrade`, `Edit`, and `Hash` never alter the body.
  - `RefusalKeepsOriginal` — a refused `Upgrade` or `Edit` leaves the stored record unchanged.
  - `ImplementationsAgree` — for every reachable record, declaration, and retention capacity, `Upgrade` by leg `A` and by leg `B` produce identical output.
  - `HopOrderIrrelevant` — upgrading by `A` then `B` equals upgrading by `B` then `A` equals one `Upgrade` on the same declaration and capacity.
  - `TaskHashStable` — editing `needs` or `tier` leaves `TaskHash` unchanged, whereas editing `paths` to a new value changes `TaskHash`.
  - Four reversed fault configs (`EditDropsUnknown`, `DivergentLeg`, `EditTouchesBody`, `HashSeesNeeds`) prove each invariant bites when violated.

## Every language

Every language leg wraps its generated `Card` codec in an owning facade that enforces the same bounds, retention ceilings, brief parser/renderer, retained upgrade, atomic field edits, task hash, and generated descriptor view, and proves byte-for-byte agreement against `tables/cards/conformance/expected/` under `make cards-conformance-all`.

### C++

Generate the C++ card unit with:

```sh
./bin/schema generate --lang cpp --out build/tables-generated-cpp/cards tables/cards
```

Facade (`tables/cards/cpp/cards.hpp`, `tables/cards/cpp/cards.cpp`, namespace `cards`):

| Go name | C++ spelling | Notes |
| --- | --- | --- |
| `Record` | `cards::Record` | Owns a defensive `std::vector<uint8_t>` copy of the wire. |
| `Report` | `cards::TableReport` | Tolerant load/save counters (`retained`, `retain_lost`, `widened`, `unknown`, `kind_mismatch`, `clamped`, `duplicate`, `malformed`). |
| `MaxBodyBytes` | `cards::MaxBodyBytes` (`64 << 10`) | 65,536-byte ceiling on `body`. |
| `MaxListEntries` | `cards::MaxListEntries` (`512`) | 512-entry ceiling on each collection field. |
| `MaxRetainBytes` / `MaxRetainIDs` | `cards::MaxRetainBytes` (`1 << 20`), `cards::MaxRetainIDs` (`4096`) | Caller-owned retention store ceilings per `Upgrade` call. |
| `Load` | `cards::CardLoad(wire, len, rec, report)` | Tolerant load into `cards::Record`; copies `wire`. |
| `FromJSON` | `cards::CardFromJSON(text, len, rec, report, refusal_class)` | Strict JSON read and checked save. |
| `ToJSON` | `cards::CardToJSON(rec, out, report)` | Known-field JSON view. |
| `Wire` | `cards::CardWire(rec)` | Returns a defensive copy of the owned wire bytes. |
| `Parse` | `cards::Parse(text, len, rec, refusal_class)` | Admitted brief text to `cards::Record`. |
| `Render` | `cards::Render(rec, out, report)` | Informational brief text view. |
| `Upgrade` | `cards::Upgrade(wire, len, out, report, refusal_class)` | Atomic retained region round trip (`CardLoadRetain` / `CardSaveRetain`). |
| `ReadStrict` | `cards::ReadStrict(wire, len, rec, report, refusal_class)` | Refuses `unknown`, `kind_mismatch`, `clamped`, and `malformed`. |
| `TaskHash` | `cards::TaskHash(rec, out, refusal_class)` | Framed SHA-256 over `body`, `paths`, `_new`, `shared`, `test`, `done`. |
| `SetNeeds` | `cards::SetNeeds(rec, needs, refusal_class)` | Atomic `needs` replacement (64-byte element bound, 512 ceiling). |
| `SetPaths` | `cards::SetPaths(rec, paths, refusal_class)` | Atomic `paths` replacement (192-byte element bound, 512 ceiling). |
| `SetTier` | `cards::SetTier(rec, tier, refusal_class)` | Atomic `tier` replacement (`cards::Tier`). |
| `SetPriority` | `cards::SetPriority(rec, priority, refusal_class)` | Atomic `priority` replacement (`cards::Priority`). |
| `SetColumn` | `cards::SetColumn(rec, column, refusal_class)` | Atomic `column` replacement (`cards::Column`). |
| `SetHolder` | `cards::SetHolder(rec, holder, refusal_class)` | Atomic `holder` replacement (32-byte bound). |
| View | `cards::WriteView()` | Walks `cards::UnitView()` in table-name and declaration order. |

Make targets: `make cards-cpp-generate`, `make cards-cpp-test`, `make cards-cpp-conformance`, `make cards-cpp-conformance-negative-control`, `make cards-cpp-conformance-upgrade-negative-control`, `make cards-cpp-conformance-hash-negative-control`.

### C

Generate the C card unit with:

```sh
./bin/schema generate --lang c --out build/tables-generated-c/cards tables/cards
```

Facade (`tables/cards/c/cards.h`, `tables/cards/c/cards.c`):

| Go name | C spelling | Notes |
| --- | --- | --- |
| `Record` | `CardsRecord` | Owns a heap copy of `wire` and the last `TableReport`; released with `cards_record_clear`. |
| `Report` | `TableReport` | Generated C report struct (`retained`, `retain_lost`, `widened`, `unknown`, `kind_mismatch`, `clamped`, `duplicate`, `malformed`). |
| `MaxBodyBytes` | `CARDS_MAX_BODY_BYTES` (`64 << 10`) | 65,536-byte ceiling on `body`. |
| `MaxListEntries` | `CARDS_MAX_LIST_ENTRIES` (`512`) | 512-entry ceiling on each collection field. |
| `MaxRetainBytes` / `MaxRetainIDs` | `CARDS_MAX_RETAIN_BYTES` (`1 << 20`), `CARDS_MAX_RETAIN_IDS` (`4096`) | Caller-owned retention store ceilings per `cards_upgrade` call. |
| `Load` | `cards_load(wire, wire_len, rec, report, err, err_cap)` | Tolerant load into `CardsRecord`; copies `wire`. |
| `FromJSON` | `cards_from_json(text, text_len, rec, err, err_cap)` | Strict JSON read and checked save. |
| `ToJSON` | `cards_to_json(rec, out, out_len, err, err_cap)` | Known-field JSON view. |
| `Wire` | `cards_wire(rec, out, out_len, err, err_cap)` | Allocates and copies the owned wire bytes. |
| `Parse` | `cards_parse(text, text_len, rec, err, err_cap)` | Admitted brief text to `CardsRecord`. |
| `Render` | `cards_render(rec, out, out_len, err, err_cap)` | Informational brief text view. |
| `Upgrade` | `cards_upgrade(wire, wire_len, out, out_len, report, err, err_cap)` | Atomic retained region round trip (`Card_load_retain` / `Card_save_retain`). |
| `ReadStrict` | `cards_read_strict(wire, wire_len, rec, err, err_cap)` | Refuses `unknown`, `kind_mismatch`, `clamped`, and `malformed`. |
| `TaskHash` | `cards_task_hash(rec, hex_out, err, err_cap)` | Framed SHA-256 over `body`, `paths`, `_new`, `shared`, `test`, `done`. |
| `SetNeeds` | `cards_set_needs(rec, needs, count, err, err_cap)` | Atomic `needs` replacement. |
| `SetPaths` | `cards_set_paths(rec, paths, count, err, err_cap)` | Atomic `paths` replacement. |
| `SetTier` | `cards_set_tier(rec, tier, err, err_cap)` | Atomic `tier` replacement (`Tier`). |
| `SetPriority` | `cards_set_priority(rec, priority, err, err_cap)` | Atomic `priority` replacement (`Priority`). |
| `SetColumn` | `cards_set_column(rec, column, err, err_cap)` | Atomic `column` replacement (`Column`). |
| `SetHolder` | `cards_set_holder(rec, holder, err, err_cap)` | Atomic `holder` replacement. |
| View | `cards_view(out, out_len, err, err_cap)` | Walks `unit_view()` in table-name and declaration order. |

Make targets: `make cards-c-generate`, `make cards-c-test`, `make cards-c-conformance`, `make cards-c-conformance-negative-control`, `make cards-c-conformance-upgrade-negative-control`, `make cards-c-conformance-hash-negative-control`.

### C#

Generate the C# card unit with:

```sh
./bin/schema generate --lang cs --out build/tables-generated-cs/cards tables/cards
```

Facade (`tables/cards/cs/Cards.cs`, `tables/cards/cs/Driver/Program.cs`, namespace `Cards`):

| Go name | C# spelling | Notes |
| --- | --- | --- |
| `Record` | `Cards.Record` | Owns a private `byte[]` copy of the wire and the decoded `Card`; refusals throw `Cards.CardException`. |
| `Report` | `Cards.TableReport` (`record.Report`) | Generated C# report class carried on `Record` and `out TableReport`. |
| `MaxBodyBytes` | `Record.MaxBodyBytes` (`64 << 10`) | 65,536-byte ceiling on `body`. |
| `MaxListEntries` | `Record.MaxListEntries` (`512`) | 512-entry ceiling on each collection field. |
| `MaxRetainBytes` / `MaxRetainIDs` | `Record.MaxRetainBytes` (`1 << 20`), `Record.MaxRetainIds` (`4096`) | Caller-owned retention store ceilings per `Record.Upgrade` call. |
| `Load` | `Record.Load(byte[] wire, out TableReport report)` | Tolerant load into `Record`; copies `wire`. |
| `FromJSON` | `Record.FromJson(byte[] text)` | Strict JSON read and checked save. |
| `ToJSON` | `record.ToJson()` | Known-field JSON `byte[]` view. |
| `Wire` | `record.Wire()` | Returns a fresh `byte[]` copy of the owned wire. |
| `Parse` | `Record.Parse(byte[] bytes)` | Admitted brief text to `Record`. |
| `Render` | `record.Render()` | Informational brief text `byte[]` view. |
| `Upgrade` | `Record.Upgrade(byte[] wire, out TableReport report)` | Atomic retained region round trip (`Schema.CardLoadRetain` / `Schema.CardSaveRetain`). |
| `ReadStrict` | `Record.ReadStrict(byte[] wire)` | Refuses `Unknown`, `KindMismatch`, `Clamped`, and `Malformed`. |
| `TaskHash` | `Record.TaskHash(Record record)` | Framed SHA-256 `byte[32]` digest over the six task fields. |
| `SetNeeds` | `record.SetNeeds(string[] needs)` | Atomic `needs` replacement. |
| `SetPaths` | `record.SetPaths(string[] paths)` | Atomic `paths` replacement. |
| `SetTier` | `record.SetTier(Tier tier)` | Atomic `tier` replacement. |
| `SetPriority` | `record.SetPriority(Priority priority)` | Atomic `priority` replacement. |
| `SetColumn` | `record.SetColumn(Column column)` | Atomic `column` replacement. |
| `SetHolder` | `record.SetHolder(string holder)` | Atomic `holder` replacement. |
| View | `Schema.UnitView()` (`Program.ViewText`) | Walks `Schema.UnitView().Tables.Span` in table-name and declaration order. |

Make targets: `make cards-cs-generate`, `make cards-cs-test`, `make cards-cs-conformance`, `make cards-cs-conformance-negative-control`, `make cards-cs-conformance-upgrade-negative-control`, `make cards-cs-conformance-hash-negative-control`.

### Go

Generate the Go card unit with:

```sh
./bin/schema generate --lang go --out build/tables-generated-go/cards tables/cards
```

Facade (`tables/cards/cards.go`, `parse.go`, `render.go`, `upgrade.go`, `edit.go`, package `cards`):

| Go name | Go spelling | Notes |
| --- | --- | --- |
| `Record` | `cards.Record` | Owns a defensive `[]byte` copy of the wire and the decoded `Card`. |
| `Report` | `cards.Report` (`= cards.TableReport`) | Tolerant load/save report counters. |
| `MaxBodyBytes` | `cards.MaxBodyBytes` (`64 << 10`) | 65,536-byte ceiling on `body`. |
| `MaxListEntries` | `cards.MaxListEntries` (`512`) | 512-entry ceiling on each collection field. |
| `MaxRetainBytes` / `MaxRetainIDs` | `cards.MaxRetainBytes` (`1 << 20`), `cards.MaxRetainIDs` (`4096`) | Caller-owned retention store ceilings per `Upgrade` call. |
| `Load` | `cards.Load(wire []byte) (*Record, Report, error)` | Tolerant load into `*Record`; copies `wire`. |
| `FromJSON` | `cards.FromJSON(text []byte) (*Record, error)` | Strict JSON read and checked save. |
| `ToJSON` | `cards.ToJSON(rec *Record) ([]byte, Report, error)` | Known-field JSON view. |
| `Wire` | `(*Record).Wire() []byte` | Returns a defensive copy of the owned wire bytes. |
| `Parse` | `cards.Parse(text []byte) (*Record, error)` | Admitted brief text to `*Record`. |
| `Render` | `cards.Render(rec *Record) ([]byte, Report, error)` | Informational brief text view. |
| `Upgrade` | `cards.Upgrade(wire []byte) ([]byte, Report, error)` | Atomic retained region round trip (`CardLoadRetain` / `CardSaveRetain`). |
| `ReadStrict` | `cards.ReadStrict(wire []byte) (*Record, Report, error)` | Refuses `Unknown`, `KindMismatch`, `Clamped`, and `Malformed`. |
| `TaskHash` | `cards.TaskHash(rec *Record) ([32]byte, error)` | Framed SHA-256 digest over `body`, `paths`, `_new`, `shared`, `test`, `done`. |
| `SetNeeds` | `cards.SetNeeds(rec *Record, needs []string) error` | Atomic `needs` replacement. |
| `SetPaths` | `cards.SetPaths(rec *Record, paths []string) error` | Atomic `paths` replacement. |
| `SetTier` | `cards.SetTier(rec *Record, tier Tier) error` | Atomic `tier` replacement. |
| `SetPriority` | `cards.SetPriority(rec *Record, priority Priority) error` | Atomic `priority` replacement. |
| `SetColumn` | `cards.SetColumn(rec *Record, column Column) error` | Atomic `column` replacement. |
| `SetHolder` | `cards.SetHolder(rec *Record, holder string) error` | Atomic `holder` replacement. |
| View | `cards.UnitView()` | Generated unit descriptor table walked by `tables/cards/cmd/cards-driver/main.go`. |

Make targets: `make cards-generate`, `make cards-test`, `make cards-example`, `make cards-conformance-go`, `make cards-conformance-negative-control`, `make cards-conformance-all`.

### Rust

Generate the Rust card unit with:

```sh
./bin/schema generate --lang rust --out build/tables-generated-rust/cards/src tables/cards
```

Facade (`tables/cards/rust/src/lib.rs`, crate `cards`):

| Go name | Rust spelling | Notes |
| --- | --- | --- |
| `Record` | `cards::Record` | Owns a private `Vec<u8>` copy of the wire, `Card`, and `TableReport`. |
| `Report` | `cards::TableReport` (`record.report()`) | Generated report struct (`retained`, `retain_lost`, `widened`, `unknown`, `kind_mismatch`, `clamped`, `duplicate`, `malformed`). |
| `MaxBodyBytes` | `cards::MAX_BODY_BYTES` (`64 << 10`) | 65,536-byte ceiling on `body`. |
| `MaxListEntries` | `cards::MAX_LIST_ENTRIES` (`512`) | 512-entry ceiling on each collection field. |
| `MaxRetainBytes` / `MaxRetainIDs` | `cards::MAX_RETAIN_BYTES` (`1 << 20`), `cards::MAX_RETAIN_IDS` (`4096`) | Caller-owned retention store ceilings per `upgrade` call. |
| `Load` | `cards::load(wire: &[u8]) -> Result<(Record, TableReport), String>` | Tolerant load into `Record`; clones `wire`. |
| `FromJSON` | `cards::from_json(text: &str) -> Result<Record, String>` | Strict JSON read and checked save. |
| `ToJSON` | `cards::to_json(record: &Record) -> Result<(String, TableReport), String>` | Known-field JSON view. |
| `Wire` | `Record::wire(&self) -> Vec<u8>` | Returns a cloned `Vec<u8>` of the owned wire. |
| `Parse` | `cards::parse(text: &[u8]) -> Result<Record, String>` | Admitted brief text to `Record`. |
| `Render` | `cards::render(record: &Record) -> Result<Vec<u8>, String>` | Informational brief text view. |
| `Upgrade` | `cards::upgrade(wire: &[u8]) -> Result<(Vec<u8>, TableReport), String>` | Atomic retained round trip (`card_load_retain` / `card_save_retain`). |
| `ReadStrict` | `cards::read_strict(wire: &[u8]) -> Result<Record, String>` | Refuses `unknown`, `kind_mismatch`, `clamped`, and `malformed`. |
| `TaskHash` | `cards::task_hash(record: &Record) -> Result<[u8; 32], String>` | Framed SHA-256 digest over the six task fields. |
| `SetNeeds` | `cards::set_needs(record: &mut Record, needs: &[String]) -> Result<(), String>` | Atomic `needs` replacement. |
| `SetPaths` | `cards::set_paths(record: &mut Record, paths: &[String]) -> Result<(), String>` | Atomic `paths` replacement. |
| `SetTier` | `cards::set_tier(record: &mut Record, tier: Tier) -> Result<(), String>` | Atomic `tier` replacement. |
| `SetPriority` | `cards::set_priority(record: &mut Record, priority: Priority) -> Result<(), String>` | Atomic `priority` replacement. |
| `SetColumn` | `cards::set_column(record: &mut Record, column: Column) -> Result<(), String>` | Atomic `column` replacement. |
| `SetHolder` | `cards::set_holder(record: &mut Record, holder: &str) -> Result<(), String>` | Atomic `holder` replacement. |
| View | `cards::view() -> String` | Walks `card_descriptors::TABLES` in table-name and declaration order. |

Make targets: `make cards-rust-generate`, `make cards-rust-test`, `make cards-rust-conformance`, `make cards-rust-conformance-negative-control`, `make cards-rust-conformance-upgrade-negative-control`, `make cards-rust-conformance-hash-negative-control`.

### Java

Generate the Java card unit with:

```sh
./bin/schema generate --lang java --out build/tables-generated-java/cards tables/cards
```

Facade (`tables/cards/java/src/cards/Cards.java`, `tables/cards/java/src/cards/Driver.java`, package `cards`):

| Go name | Java spelling | Notes |
| --- | --- | --- |
| `Record` | `Cards.Record` | Owns a private `byte[]` copy of the wire, `CardVariable.Value`, and `TableVariable.Report`; refusals throw `Cards.Refusal`. |
| `Report` | `TableVariable.Report` (`rec.report`, `UpgradeResult.report`) | Generated report counters. |
| `MaxBodyBytes` | `Cards.MaxBodyBytes` (`64 << 10`) | 65,536-byte ceiling on `body`. |
| `MaxListEntries` | `Cards.MaxListEntries` (`512`) | 512-entry ceiling on each collection field. |
| `MaxRetainBytes` / `MaxRetainIDs` | `Cards.MaxRetainBytes` (`1 << 20`), `Cards.MaxRetainIDs` (`4096`) | Caller-owned retention store ceilings per `Cards.upgrade` call. |
| `Load` | `Cards.load(byte[] wire)` | Tolerant load into `Cards.Record`; copies `wire`. |
| `FromJSON` | `Cards.fromJSON(byte[] text)` | Strict JSON read and checked save. |
| `ToJSON` | `Cards.toJSON(Cards.Record record)` | Known-field JSON `byte[]` view. |
| `Wire` | `record.wire()` | Returns `Arrays.copyOf` of the owned wire bytes. |
| `Parse` | `Cards.parse(byte[] text)` | Admitted brief text to `Cards.Record`. |
| `Render` | `Cards.render(Cards.Record record)` | Informational brief text `byte[]` view. |
| `Upgrade` | `Cards.upgrade(byte[] wire)` | Atomic retained round trip (`CardRetain.loadRetain` / `CardRetain.saveRetain`). |
| `ReadStrict` | `Cards.readStrict(byte[] wire)` | Refuses `unknown`, `kindMismatch`, `clamped`, and `malformed`. |
| `TaskHash` | `Cards.taskHash(byte[] wire)` | Framed SHA-256 64-char hex digest over the six task fields. |
| `SetNeeds` | `Cards.setNeeds(byte[] wire, List<String> needs)` | Atomic `needs` replacement returning a fresh `Cards.Record`. |
| `SetPaths` | `Cards.setPaths(byte[] wire, List<String> paths)` | Atomic `paths` replacement. |
| `SetTier` | `Cards.setTier(byte[] wire, int tier)` | Atomic `tier` replacement. |
| `SetPriority` | `Cards.setPriority(byte[] wire, int priority)` | Atomic `priority` replacement. |
| `SetColumn` | `Cards.setColumn(byte[] wire, int column)` | Atomic `column` replacement. |
| `SetHolder` | `Cards.setHolder(byte[] wire, String holder)` | Atomic `holder` replacement. |
| View | `Driver.view()` | Walks `*Descriptor.tableType()` in table-name and declaration order. |

Make targets: `make cards-java-generate`, `make cards-java-test`, `make cards-java-conformance`, `make cards-java-conformance-negative-control`, `make cards-java-conformance-upgrade-negative-control`, `make cards-java-conformance-hash-negative-control`.

### JavaScript

Generate the JavaScript card unit with:

```sh
./bin/schema generate --lang js --out build/tables-generated-js/cards tables/cards
```

Facade (`tables/cards/js/cards.mjs`, `tables/cards/js/driver.mjs`):

| Go name | JavaScript spelling | Notes |
| --- | --- | --- |
| `Record` | `Record` (`cards.Record`) | Owns a private `Uint8Array` copy of the wire, decoded `Card`, and report; refusals carry `CardError`. |
| `Report` | `TableVariableReport` | Generated report object returned as `{ record, report, error }` / `{ out, report, error }`. |
| `MaxBodyBytes` | `MaxBodyBytes` (`64 << 10`) | 65,536-byte ceiling on `body`. |
| `MaxListEntries` | `MaxListEntries` (`512`) | 512-entry ceiling on each collection field. |
| `MaxRetainBytes` / `MaxRetainIDs` | `MaxRetainBytes` (`1 << 20`), `MaxRetainIDs` (`4096`) | Caller-owned retention store ceilings per `Upgrade` call. |
| `Load` | `Load(wire)` | Tolerant load returning `{ record, report, error }`; copies `wire`. |
| `FromJSON` | `FromJSON(text)` | Strict JSON read and checked save returning `{ record, error }`. |
| `ToJSON` | `ToJSON(record)` | Known-field JSON view returning `{ text, report, error }`. |
| `Wire` | `record.Wire()` | Returns a fresh `Uint8Array` copy of the owned wire. |
| `Parse` | `Parse(text)` | Admitted brief text to `{ record, error }`. |
| `Render` | `Render(record)` | Informational brief text view returning `{ text, report, error }`. |
| `Upgrade` | `Upgrade(wire)` | Atomic retained round trip (`CardLoadRetain` / `CardSaveRetain`) returning `{ out, report, error }`. |
| `ReadStrict` | `ReadStrict(wire)` | Refuses `unknown`, `kindMismatch`, `clamped`, and `malformed`. |
| `TaskHash` | `TaskHash(record)` | Framed SHA-256 64-char hex digest over the six task fields. |
| `SetNeeds` | `SetNeeds(record, needs)` | Atomic `needs` replacement on `record`. |
| `SetPaths` | `SetPaths(record, paths)` | Atomic `paths` replacement on `record`. |
| `SetTier` | `SetTier(record, tier)` | Atomic `tier` replacement on `record`. |
| `SetPriority` | `SetPriority(record, priority)` | Atomic `priority` replacement on `record`. |
| `SetColumn` | `SetColumn(record, column)` | Atomic `column` replacement on `record`. |
| `SetHolder` | `SetHolder(record, holder)` | Atomic `holder` replacement on `record`. |
| View | `CardsUnitRegistry` (`view()` in `driver.mjs`) | Walks `CardsUnitRegistry.tables` in table-name and declaration order. |

Make targets: `make cards-js-generate`, `make cards-js-test`, `make cards-js-conformance`, `make cards-js-conformance-negative-control`, `make cards-js-conformance-upgrade-negative-control`, `make cards-js-conformance-hash-negative-control`.

### Dart

Generate the Dart card unit with:

```sh
./bin/schema generate --lang dart --out build/tables-generated-dart/cards tables/cards
```

Facade (`tables/cards/dart/lib/cards.dart`):

| Go name | Dart spelling | Notes |
| --- | --- | --- |
| `Record` | `Record` | Owns a private `Uint8List` copy of the wire, `Card`, and `TableWireReport`; refusals throw `CardException`. |
| `Report` | `TableWireReport` (`record.report`, `UpgradeResult.report`) | Generated report object carried on `Record` and `UpgradeResult`. |
| `MaxBodyBytes` | `maxBodyBytes` (`64 << 10`) | 65,536-byte ceiling on `body`. |
| `MaxListEntries` | `maxListEntries` (`512`) | 512-entry ceiling on each collection field. |
| `MaxRetainBytes` / `MaxRetainIDs` | `maxRetainBytes` (`1 << 20`), `maxRetainIds` (`4096`) | Caller-owned retention store ceilings per `upgrade` call. |
| `Load` | `load(Uint8List wire) -> Record` | Tolerant load into `Record`; copies `wire`. |
| `FromJSON` | `fromJson(String text) -> Record` | Strict JSON read and checked save. |
| `ToJSON` | `toJson(Record record) -> String` | Known-field JSON view. |
| `Wire` | `record.wire() -> Uint8List` | Returns `Uint8List.fromList` of the owned wire. |
| `Parse` | `parse(String text) -> Record` | Admitted brief text to `Record`. |
| `Render` | `render(Record record) -> String` | Informational brief text view. |
| `Upgrade` | `upgrade(Uint8List wire) -> UpgradeResult` | Atomic retained round trip (`cardWireLoadRetain` / `cardWireSaveRetain`). |
| `ReadStrict` | `readStrict(Uint8List wire) -> Record` | Refuses `unknown`, `kindMismatch`, `clamped`, and `malformed`. |
| `TaskHash` | `taskHash(Record record) -> Uint8List` (`taskHashHex`) | Framed SHA-256 digest over the six task fields. |
| `SetNeeds` | `setNeeds(Record r, List<String> needs)` | Atomic `needs` replacement on `r`. |
| `SetPaths` | `setPaths(Record r, List<String> paths)` | Atomic `paths` replacement on `r`. |
| `SetTier` | `setTier(Record r, int tier)` | Atomic `tier` replacement on `r`. |
| `SetPriority` | `setPriority(Record r, int priority)` | Atomic `priority` replacement on `r`. |
| `SetColumn` | `setColumn(Record r, int column)` | Atomic `column` replacement on `r`. |
| `SetHolder` | `setHolder(Record r, String holder)` | Atomic `holder` replacement on `r`. |
| View | `viewText() -> String` | Walks `cardDescriptors()` in table-name and declaration order. |

Make targets: `make cards-dart-generate`, `make cards-dart-test`, `make cards-dart-conformance`, `make cards-dart-conformance-negative-control`, `make cards-dart-conformance-upgrade-negative-control`, `make cards-dart-conformance-hash-negative-control`.

### Elixir

Generate the Elixir card unit with:

```sh
./bin/schema generate --lang elixir --out build/tables-generated-elixir/cards tables/cards
```

Facade (`tables/cards/elixir/cards.ex`, module `Cards.Cards`):

| Go name | Elixir spelling | Notes |
| --- | --- | --- |
| `Record` | `%Cards.Cards.Record{wire, value, report}` | Struct owning the immutable BEAM binary `wire`, decoded `value` map, and `report`. |
| `Report` | `record.report` (map from `Cards.Table`) | Tolerant read/save counters (`:retained`, `:retain_lost`, `:widened`, `:unknown`, `:kind_mismatch`, `:clamped`, `:duplicate`, `:malformed`). |
| `MaxBodyBytes` | `@max_body_bytes` (`64 * 1024`) | 65,536-byte ceiling on `body`. |
| `MaxListEntries` | `@max_list_entries` (`512`) | 512-entry ceiling on each collection field. |
| `MaxRetainBytes` / `MaxRetainIDs` | `@max_retain_bytes` (`1_048_576`), `@max_retain_ids` (`4096`) | Caller-owned retention store ceilings per `Cards.Cards.upgrade/1` call. |
| `Load` | `Cards.Cards.load(wire)` | Tolerant load returning `{:ok, record, report}` or `{:error, reason, report}`. |
| `FromJSON` | `Cards.Cards.from_json(text)` | Strict JSON read and checked save returning `{:ok, record}` or `{:error, reason}`. |
| `ToJSON` | `Cards.Cards.to_json(record)` | Known-field JSON view returning `{:ok, text, report}`. |
| `Wire` | `Cards.Cards.wire(record)` | Returns the record's owned wire binary. |
| `Parse` | `Cards.Cards.parse(text)` | Admitted brief text to `{:ok, record}` or `{:error, reason}`. |
| `Render` | `Cards.Cards.render(record)` | Informational brief text view returning `{:ok, text, report}`. |
| `Upgrade` | `Cards.Cards.upgrade(wire)` | Atomic retained round trip (`Cards.Table.load_retain` / `Cards.Table.save_retain`). |
| `ReadStrict` | `Cards.Cards.read_strict(wire)` | Refuses `:unknown`, `:kind_mismatch`, `:clamped`, and `:malformed`. |
| `TaskHash` | `Cards.Cards.task_hash(record)` | Framed SHA-256 `{:ok, <<digest::binary-size(32)>>}` over the six task fields. |
| `SetNeeds` | `Cards.Cards.set_needs(record, needs)` | Atomic `needs` replacement returning `{:ok, record}` or `{:error, reason}`. |
| `SetPaths` | `Cards.Cards.set_paths(record, paths)` | Atomic `paths` replacement. |
| `SetTier` | `Cards.Cards.set_tier(record, tier)` | Atomic `tier` replacement. |
| `SetPriority` | `Cards.Cards.set_priority(record, priority)` | Atomic `priority` replacement. |
| `SetColumn` | `Cards.Cards.set_column(record, column)` | Atomic `column` replacement. |
| `SetHolder` | `Cards.Cards.set_holder(record, holder)` | Atomic `holder` replacement. |
| View | `Cards.Cards.view()` | Walks `Cards.Table.descriptors()` in table-name and declaration order. |

Make targets: `make cards-elixir-generate`, `make cards-elixir-test`, `make cards-elixir-conformance`, `make cards-elixir-conformance-negative-control`, `make cards-elixir-conformance-upgrade-negative-control`, `make cards-elixir-conformance-hash-negative-control`.

### Lua

Vanilla Lua 5.4 (PUC-Rio, pinned by `make dist-lua`). Generate the Lua card unit with:

```sh
./bin/schema generate --lang lua --out build/tables-generated-lua/cards tables/cards
```

The facade is `tables/cards/lua/cards.lua` (`require("cards")`, with the generated directory and the facade's directory on `package.path`). A refusal is the Lua pair `nil, err`: `err` is a `CardError` whose `class` is one of the closed classes of `tables/cards/conformance/FORMAT.md` and whose `tostring` is the message; a read that completed answers its report as the second value, and a refusal after a read carries it as `err.report`. A `*string` field (`body`, `done`, `source`, an extra's `value`) is a box `{ value = ... }`, `nil` when null; an enum is its variant name, `nil` for None.

| Go name | Lua spelling | Notes |
| --- | --- | --- |
| `Record` | `cards.Record` (`{ wire, root, report }`) | Owns the wire and the decoded Card; a Lua string is immutable, so the wire is its own by construction. |
| `Report` | the zeroed counters table (`cards.NewReport()`) | `retained`, `retainLost`, `widened`, `unknown`, `kindMismatch`, `clamped`, `duplicate`, `malformed`. |
| `MaxBodyBytes` | `cards.MaxBodyBytes` (`64 << 10`) | 65,536-byte ceiling on `body`. |
| `MaxListEntries` | `cards.MaxListEntries` (`512`) | 512-entry ceiling on each collection field. |
| `MaxRetainBytes` / `MaxRetainIDs` | `cards.MaxRetainBytes` (`1 << 20`), `cards.MaxRetainIDs` (`4096`) | Caller-owned retention store ceilings per `Upgrade` call. |
| `Load` | `cards.Load(wire)` | Tolerant load returning `record, report` or `nil, err`. |
| `FromJSON` | `cards.FromJSON(text)` | Strict JSON read and checked save returning `record, report` or `nil, err`. |
| `ToJSON` | `cards.ToJSON(record)` | Known-field JSON view returning `text, report`. |
| `Wire` | `record:Wire()` | The owned wire bytes. |
| `Parse` | `cards.Parse(text)` | Admitted brief text to `record, report` or `nil, err`. |
| `Render` | `cards.Render(record)` | Informational brief text view returning `text, report`. |
| `Upgrade` | `cards.Upgrade(wire)` | Atomic retained round trip (`retain.LoadRetain` / `retain.SaveRetain`) returning `out, report` or `nil, err`. |
| `ReadStrict` | `cards.ReadStrict(wire)` | Refuses `unknown`, `kindMismatch`, `clamped` and `malformed`. |
| `TaskHash` | `cards.TaskHash(record)` | Framed SHA-256 64-char hex digest over the six task fields (a pure-Lua SHA-256 in the facade). |
| `SetNeeds` | `cards.SetNeeds(record, needs)` | Atomic `needs` replacement returning `true` or `nil, err`. |
| `SetPaths` | `cards.SetPaths(record, paths)` | Atomic `paths` replacement. |
| `SetTier` | `cards.SetTier(record, tier)` | Atomic `tier` replacement. |
| `SetPriority` | `cards.SetPriority(record, priority)` | Atomic `priority` replacement. |
| `SetColumn` | `cards.SetColumn(record, column)` | Atomic `column` replacement. |
| `SetHolder` | `cards.SetHolder(record, holder)` | Atomic `holder` replacement. |
| View | `view()` in `tables/cards/lua/driver.lua` | Walks `CardsTables.Tables` in table-name and declaration order; every word comes from the descriptors. |

Make targets: `make cards-lua-generate`, `make cards-lua-test`, `make cards-lua-conformance`, `make cards-lua-conformance-negative-control`, `make cards-lua-conformance-upgrade-negative-control`, `make cards-lua-conformance-hash-negative-control`.

## Later sprint integration

This stream does not implement admission, indexes or server verbs. The intended
recipe is: parse a brief at admission; keep machine fields owned by the server;
build indexes that correspond to the authoritative record; preserve the wire for
storage; and hash the task-bearing fields with `TaskHash`. Those verbs arrive
with sprint integration, not here.

