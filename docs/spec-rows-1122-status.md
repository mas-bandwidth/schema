# Spec rows 1122: status

The rows below turn the rules PR #4664 pins (docs/SPEC.md, docs/SPEC-TABLES.md) into shared conformance rows, so
every language is held to the same answer. The rows are in `testdata/conformance/tables/MANIFEST.txt` under the
heading `SPEC ROWS 1122`. The expected answers come from the Go engine (`harness generate` agreed with every
verdict); the one place Go disagreed with the spec is fixed (below).

This PR is a DRAFT and does not merge until every language is green on these rows. Per-language fix PRs target
this branch.

## The rows (101, plus 10 `rewrite` lines for the accepted framing rows)

| group | rows | rule |
|---|---|---|
| `sp-skip-depth-129`, `-130` | 2 | JSON: 129 nested containers read, the 130th refuses, in a skipped value (SPEC-TABLES §16.2) |
| `sp-skip-key-*`, `sp-unknown-key-*` | 8 | the 255-byte cap on object keys inside a skipped value (plain, nested, escaped to 255/256 decoded bytes) and on an unknown key at the placed object |
| `sp-map-key-256/300/301-wide` | 3 | a placed map key over 255 bytes inside `string(300)` reads; 301 refuses (EdgeRow) |
| `sp-enum-*` | 3 | enum name `"None"` reads; unknown name of 255 bytes reads, of 256 refuses |
| `sp-ts-*` | 16 | timestamp bounds (min, min-1ns, max, max+1ns, truncation, offsets), year 0000, second 60, comma and empty fraction, lowercase `t`/`z`, a number token, offset hour 24 |
| `sp-qint-*` | 17 | the quoted 64-bit integer grammar, with escapes undone first |
| `sp-b64-*` | 6 | base64 is judged after escapes are undone |
| `sp_fixed_*` | 12 | fixed form (form 3): reserved bytes 1, 4, 7; 19 bytes; form bytes 1, 2, 4 (verdict only); negative count; count over bound; non-zero byte in a dead slot; negative and over-bound text length |
| `depth_*` | 9 | binary nesting bound 128/129, union levels 64/65, keyed 128/129, tables 5000 (held vectors promoted) |
| `array_elem_*` | 8 | closed element kinds (held vectors promoted) |
| `graph_*` | 14 | THE GRAPH: a node reached or not reached through a pointer the reader does not declare, blob records no pointer reaches (held vectors promoted) |
| `node_type_*` | 3 | reserved ids as a node record's type (held vectors promoted) |

The 34 held vectors lose `held: true` in `framing_vectors_test.go`; the held-vector tests now assert the opposite.
`node_chain_200000` and the three other held vectors stay held (not in this spec diff).

## Go fix

`internal/tabletext/read.go` `readMap`: a bounded string map key was cut at the 255-byte schema-name cap, so a key of
256..N bytes under `map[string(N)]V` with N above 255 refused `length_over_bound`. It is now judged against its own
declared N. Test: `TestTextMapKeyAboveNameCapReadsUnderItsOwnBound` (red on the old code).

## Port fixes (bounded string map key judged against its own N)

The 255-byte cap is on schema names and never reaches a map key; a key under map[string(N)]V is held to N alone.

- C++ (cpptable/json.go, the map reader): a bounded key with N >= 256 takes the whole-key scan path the unbounded key already took, so it is not cut into the 256-byte token buffer.
- Elixir (elixirtable/variableform.go, json_map_key): the bound is field.size, no longer min(field.size, 255).
- C# (cstable/maps.go) and C# (unity) (unitytable/runtime_json.go), ReadMap: a bounded key with N >= MaxKey is read through DecodedStringBody, not the 256-byte stack buffer.
- Planted back (old code restored in each leg, rebuilt): C++ 1/3 red (256, 300), Elixir 1/3 red (256, 300), C# 2/3 red (300), C# (unity) 2/3 red (300); green again with the fix.

## Failing (language, row) pairs

Run: `harness run --manifest <new rows only> --only <lang>`, every leg built on hetzner2. 101 rows per leg.

| language | failing rows |
|---|---|
| C++ (reference), Elixir, C#, C# (unity) | none: fixed (above); were sp-map-key-256-wide and -300-wide (C++, Elixir) and -300-wide (C#, unity) |
| C, Dart, Rust | none failing; `sp-map-key-256/300/301-wide` answer ABSENT (no map class in the leg: `+3a`) |
| Go, Goose, Java, JavaScript, Lobster, Lua, Luau, Moo, Python, Ruby, Squirrel | none |

Total: 0 failing (was 6 in 4 legs). Re-run on the 3 map-key rows after the fixes: C++, Elixir, C#, C# (unity) each json-hostile 3/3.

## Not expressible with an existing registered root (no row yet)

Each needs a new root in a registered unit, and every driver then registers it:

- `[A..B]T` count below A (file form and JSON): no registered root carries a counted minimum
  (`test/tables/Bases.schema` and `feature-witnesses/counted-minimum-message` are not manifest units).
- A `float32` field with `max = 0.1` refusing the text `0.1` (float bound compared as declared).
- An enum element in an array or map reading a deprecated variant as `None`.
- R7 `&node` rules (another child owns them).

## Found on the way (one line, not touched)

- `test/conformance/moo/main.moo` loads every module `stage.pl` lists, and `writerefusalpacket.moo` requires
  `./serialize`, which resolves nowhere: the moo driver fails on every surface in a clean clone. I ran the moo leg
  with that file moved aside (not committed): 101/101.
