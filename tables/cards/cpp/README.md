# tables/cards/cpp

The C++ leg of the schema stream work-card record: `cards.hpp` and `cards.cpp`
form the handwritten owning facade over the generated `Card` table
(`CardTable.h`, `CardTable.cpp`, `CardsView.h`, `CardsView.cpp`). It owns the
wire buffer, enforces the customer bounds before anything is published, and
turns one buffer into a record a customer can read, edit and hash.
`cards_test.cpp` holds the seven facade tests and `driver.cpp` (`driver`) the
conformance driver; both match the Go leg on the shared corpus in
[tables/cards/conformance](../conformance) (format: `FORMAT.md` there).

The nine surfaces the conformance run requires: load, save, render, parse,
bounds, upgrade, edit, taskhash, view.

Make targets (`make/cpp.mk`, all part of `test`):

- `make cards-cpp-generate` generates the C++ unit from `Card.schema` into `build/tables-generated-cpp/cards`.
- `make cards-cpp-test` builds and runs the seven facade tests natively and under ASan/UBSan.
- `make cards-cpp-conformance` runs all nine surfaces against `tables/cards/conformance`.
- `make cards-cpp-conformance-negative-control`: one changed rendered byte turns exactly `render` red.
- `make cards-cpp-conformance-upgrade-negative-control`: dropping one retained field turns exactly `upgrade` red.
- `make cards-cpp-conformance-hash-negative-control`: hashing `needs` turns exactly `taskhash` red.

The Go leg is described in [tables/cards/README.md](../README.md).
