# Ruby Card Facade (`tables/cards/ruby`)

Owning pure-Ruby facade and nine-surface conformance driver over `tables/cards/Card.schema`
(`docs/CARDS.md`, `docs/PORT-RUBY.md`, `docs/plans/ruby-leg.md` R13).

- `facade.rb`: `CardsFacade` — `load`, `read_strict`, `from_json`, `to_json`, `parse`,
  `render`, `task_hash`, `edit`, `set_paths`, `set_needs`, `set_tier`, `set_priority`,
  `set_column`, `set_holder`, and `upgrade`.
- `driver` / `driver.rb`: nine-surface conformance driver (`load`, `save`, `render`,
  `parse`, `bounds`, `upgrade`, `edit`, `taskhash`, `view`).
