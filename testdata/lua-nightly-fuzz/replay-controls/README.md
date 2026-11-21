# Lua fuzz replay controls

`make schema-lua-fuzz-replay-controls` (make/schema-lua-nightly-fuzz.mk) runs
the Lua fuzz harness's replay over each directory here before every replay of
the saved corpus. One saved case per line format: `<kind> <unit> <table>
<base64 payload>`.

- `good/`: one ordinary case the generated `examples` unit's JSON reader takes.
  It must replay with `1 cases, 0 failed`.
- `unbound-unit/`, `unsupported-kind/`, `unregistered-table/`: the same case
  with its unit, kind or table changed to one no reader takes. Each must be
  refused. Each used to replay as `1 cases, 0 failed` without calling a reader
  (finding F-077).
- `unreadable-beside-good/`: the good case beside a file that does not decode.
  It must be refused. It used to report `1 cases, 0 failed` for two saved files
  (finding F-078).

The recipe adds an empty directory and a missing one, which must also be
refused (both used to report `0 cases, 0 failed` and exit 0, F-078).
internal/ci's TestLuaFuzzReplayControls holds this directory to the recipe's
list, both ways.
