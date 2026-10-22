# tables/cards/c

The C leg of the schema stream work-card record: `cards.h` and `cards.c` form the
handwritten owning facade over the generated `Card` table (`CardTable.h`,
`CardsView.h`, `CardsView.c`). It owns the wire buffer, enforces the customer
bounds before anything is published, and turns one buffer into a record a
customer can read, edit and hash. `cards_test.c` holds the seven facade tests and
`driver.c` (`driver`) the conformance driver; both match the Go leg on the
shared corpus in [tables/cards/conformance](../conformance) (format:
`FORMAT.md` there).

The nine surfaces the conformance run requires: load, save, render, parse,
bounds, upgrade, edit, taskhash, view.

Make targets (`make/c.mk`, all part of `test-c`):

- `make cards-c-generate` generates the C unit from `Card.schema` into `build/tables-generated-c/cards`.
- `make cards-c-test` builds and runs the seven facade tests natively and under ASan/UBSan.
- `make cards-c-conformance` runs all nine surfaces against `tables/cards/conformance`.
- `make cards-c-conformance-negative-control`: one changed rendered byte turns exactly `render` red.
- `make cards-c-conformance-upgrade-negative-control`: one dropped retained byte turns exactly `upgrade` red.
- `make cards-c-conformance-hash-negative-control`: one changed task-hash digit turns exactly `taskhash` red.

The Go leg is described in [tables/cards/README.md](../README.md).
