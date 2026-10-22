# tables/cards/rust

The Rust leg of the schema stream work-card record: `src/lib.rs` is the
handwritten owning facade over the generated `Card` table modules (`card.rs`,
`card_records.rs`, `card_table.rs`, `card_variable.rs`, `card_descriptors.rs`,
`variable_runtime.rs`). It owns the wire buffer, enforces the customer bounds
before anything is published, and turns one buffer into a record a customer can
read, edit and hash. `tests/cards.rs` holds the facade tests and
`src/bin/cards-driver.rs` (`driver`) the conformance driver; both match the Go
leg on the shared corpus in [tables/cards/conformance](../conformance) (format:
`FORMAT.md` there).

The nine surfaces the conformance run requires: load, save, render, parse,
bounds, upgrade, edit, taskhash, view.

Make targets (`make/rust.mk`, all part of `test-rust`):

- `make cards-rust-generate` generates the Rust unit from `Card.schema` into `build/tables-generated-rust/cards`.
- `make cards-rust-test` builds and runs the facade tests and `cargo clippy -- -D warnings` in `build/cards-rust`.
- `make cards-rust-conformance` runs all nine surfaces against `tables/cards/conformance`.
- `make cards-rust-conformance-negative-control`: one changed rendered byte turns exactly `render` red.
- `make cards-rust-conformance-upgrade-negative-control`: one changed upgraded byte turns exactly `upgrade` red.
- `make cards-rust-conformance-hash-negative-control`: one changed hash digit turns exactly `taskhash` red.

The Go leg is described in [tables/cards/README.md](../README.md).
