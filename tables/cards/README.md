# tables/cards

The schema stream's work-card record: `Card.schema` and the handwritten facade
that turns one wire buffer into a `*Record` a customer can read, edit and hash.
The full contract — the declaration, the named facade, module setup, strict
versus tolerant reads and the corpus rule — is [docs/CARDS.md](../../docs/CARDS.md).

- `make cards-test cards-example` — check, vet and test the generated module, then run the separate `cardsexample` customer module.
- `make cards-java-test cards-java-conformance` — the Java facade (`tables/cards/java`): the same shared corpus load/save/render/parse round-trips, and the upgrade, edit, task-hash and view walks, pinned by the conformance and negative controls.

Generated code is produced by `make cards-generate` into
`build/tables-generated-go/cards`; the source templates here are not compiled by
the root module.
