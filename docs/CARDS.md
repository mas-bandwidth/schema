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
Go standard library only; a customer needs `github.com/mas-bandwidth/serialize.go`
resolvable because the generated codecs call it.

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
- `make cards-corpus-check` — pin the current declaration and require every SHIPPED case.
- `make cards-create` / `make cards-fuzz-fixture` — author fixtures under an explicit flag.

## Later sprint integration

This stream does not implement admission, indexes or server verbs. The intended
recipe is: parse a brief at admission; keep machine fields owned by the server;
build indexes that correspond to the authoritative record; preserve the wire for
storage; and hash the task-bearing fields with `TaskHash`. Those verbs arrive
with sprint integration, not here.
