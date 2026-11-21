# Lua fuzz replay controls

`make schema-lua-fuzz-replay-controls` (make/schema-lua-nightly-fuzz.mk) runs
the Lua fuzz harness's replay over each directory here before every replay of
the saved corpus. One saved case per file: `<kind> <unit> <table> <base64
payload>`.

- `good/`: two ordinary cases the generated `examples` unit's JSON reader
  takes. It must replay with `2 cases, 0 failed`.
- `unbound-unit/`, `unsupported-kind/`, `unregistered-table/`: `good/ordinary.case`
  with its unit, kind or table changed to one no reader takes. Each must be
  refused. Each used to replay as `1 cases, 0 failed` without calling a reader
  (finding F-077).
- `unreadable-beside-good/`: `good/ordinary.case` beside a file that does not
  decode. It must be refused. It used to report `1 cases, 0 failed` for two
  saved files (finding F-078).
- `malformed-base64/`: `good/ordinary.case` with one character after the
  padding (`e30=!`). It must be refused. It used to decode as `e30=` and replay
  green (F-078).

The recipe adds three more, each of which must be refused: an empty directory,
a missing one, and `partial-find`, which replays `good/` with
`../partial-find.sh` installed as `find` (it lists one file and exits 7). All
three used to report `0 failed` and exit 0 (F-078). internal/ci's
TestLuaFuzzReplayControls holds this directory to the recipe's list, both ways.
