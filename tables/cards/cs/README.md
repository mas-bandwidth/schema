# tables/cards/cs

The C# leg of the schema stream work-card record: `Cards.cs` is the handwritten
facade over the generated `Card` table. It owns the wire, enforces the customer
bounds before anything is published, and turns one buffer into a record a
customer can read, edit and hash. `Tests/` holds the facade tests and `Driver/`
the conformance driver; both match the Go leg on the shared corpus in
[tables/cards/conformance](../conformance) (format: `FORMAT.md` there).

The nine surfaces the conformance run requires: load, save, render, parse,
bounds, upgrade, edit, taskhash, view.

Make targets (make/cs.mk, all part of `test-cs`):

- `make cards-cs-generate` generates the C# unit from `Card.schema`.
- `make cards-cs-test` runs the facade tests, Debug and Release.
- `make cards-cs-driver` builds the conformance driver.
- `make cards-cs-conformance` runs all nine surfaces against the corpus.
- `make cards-cs-conformance-negative-control`: one changed rendered byte turns exactly render red.
- `make cards-cs-conformance-upgrade-negative-control`: dropping the retained fields turns exactly upgrade red.
- `make cards-cs-conformance-hash-negative-control`: folding needs into the hash turns exactly taskhash red.

Needs the .NET SDK (`DOTNET`) and the sibling `../serialize.cs` checkout. The Go
leg is described in [tables/cards/README.md](../README.md).
