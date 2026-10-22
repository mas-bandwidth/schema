# tables/cards

The schema stream's work-card record: `Card.schema` and the handwritten facade
that turns one wire buffer into a `*Record` a customer can read, edit and hash.
The full contract — the declaration, the named facade, module setup, strict
versus tolerant reads and the corpus rule — is [docs/CARDS.md](../../docs/CARDS.md).

- `make cards-test cards-example` — check, vet and test the generated module, then run the separate `cardsexample` customer module.
- `make cards-cs-wire-fuzz cards-cs-retain-fuzz` — the C# leg's Card codec on the oracle wire fuzzer: the C# conformance driver (`test/conformance/cs`) compiles `build/tables-generated-cs/cards` and is held to the Go leg's bytes over `fuzz/MANIFEST.txt` with the same seed as `cards-wire-fuzz` and the Dart pair, plain and through the retaining pair. `make cards-cs-retain-negative-control` drops the driver's Card retention (`CARDS_CS_RETAIN_DROP=1`, test-only) and requires the retain arm alone to go red. All three ride `test-cs`.

Generated code is produced by `make cards-generate` into
`build/tables-generated-go/cards`; the source templates here are not compiled by
the root module.
