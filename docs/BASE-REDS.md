# Base reds

**Rule: a card's worker cites this file for the reds its stream base already has, and does not re-run the whole tree at base to find them. The worker runs the gates its own PATHS touch, and a red that appears in this file is not a finding of that card.**

This page lists every red still standing at each `sprint/schema-*` stream base after the 2026-10-16 forward merges of `main`. For each red it gives the test or gate by name, its cause, and the card or PR that owns it. If a base moves and a red here goes green, strike the row in the PR that greened it. If a red appears that is not listed here, it is a finding.

Measured 2026-10-16 on hetzner2 and amd-3. Each tree was staged with `stage-schema` (the six serialize siblings at the CI pins) and run with the bench SDKs (`NODE=node JAVA=java JAVAC=javac DART=dart`). The suites:

- `SCHEMA_SLOW=1 go test -count=1 ./compiler/`
- `make generated-current`
- the stream's own `make test-<lang>` where one exists

`main` is 4d9941fbc throughout.

## How each base got here

| stream | forward merge | base after | compiler suite after |
|---|---|---|---|
| c | #2010 (merge of main) | 310e99207 | 0 failing |
| cpp | #2011 (merge of main) | 12001f203 | 0 failing |
| cs | #2012 (merge of main) | e29946444 | 0 failing |
| java | #2013 (merge of main) | cebf18f1e | 1 failing |
| js | #2014 (merge of main) | 8ea510762 | 5 failing |
| elixir | #1999 (cherry-picks of main's fixes) | 821cf4129 | 0 failing (per #1999) |
| rust | #2001 (cherry-picks of main's fixes) | 0b424c164 | 0 failing (per #2001) |
| shared | #2002 (cherry-picks of main's fixes) | 0623c36dc | 0 failing (per #2002) |
| dart | **not merged**: a design conflict, see below | 87be5063b | see below |

## c — sprint/schema-c after #2010

| red | cause | owner |
|---|---|---|
| `make test-c`: `build/schema_test_c_fixedform`, `build/schema_test_c_fixedform_asan` | `test/c-tables/fixedform_fx1.c:270` calls `table_fixed_run` with its old signature (`-Werror=int-conversion`, 102 errors). Red on `main` too. | #1943 (into dev) |

Compiler suite and `generated-current` are green.

## cpp — sprint/schema-cpp after #2011

No red. The compiler suite and `generated-current` are green. There is no `test-cpp` target; the C++ reference's gates ride `make test`.

## cs — sprint/schema-cs after #2012

| red | cause | owner |
|---|---|---|
| `make test-cs`: `tables-cs-clean` | `dotnet format --verify-no-changes` reports WHITESPACE findings over `build/tables-generated-cs` (for example `block/RenderTable.cs`). The generated table units are not format-clean. | the cs format gate card (#547), owed |
| `make test-cs`: `tables-cs-clean-negative-control` | follows from the row above: the control finds drift outside its planted unit (`rt1/RT1Table.cs`) | same |

Compiler suite and `generated-current` are green.

## java — sprint/schema-java after #2013

| red | cause | owner |
|---|---|---|
| `compiler`: `TestJavaFixedFormIsEmittedForAFixedRootAndForNothingElse` | `compiler/tablesjava_test.go:553` pins `public static int load(Value[] values, int count, byte[] data,` on one line. 62d886f42 (#547) lays the parameters one per line, so the pin fails. | the java format card (#547) |

`generated-current` and `make test-java` are green.

## js — sprint/schema-js after #2014

| red | cause | owner |
|---|---|---|
| `compiler`: `TestMapCarriers`, `TestMapRefusalNamesTheCarrier` | the stream registers js as a map carrier (`compiler/target_javascript.go:151`), and the tests still pin `[c cpp cs go]` | the js carrier card, owed (main's f4bbc1051 is the same fix for lists) |
| `compiler`: `TestOptionalArraysCarriers/js`, `TestUnionArraysCarriers/js`, `TestWasRowsCarriers` | the same cause: js now carries optional arrays, union arrays and was rows, and the tests still expect a refusal | same |
| `make test-js`: `packet-wide-js` | `test/packet-wide/js/main.mjs:36`: under contracts `WriteWideSevenFlat` answers 1 where -1 is wanted. Green on `main`. | owed |
| `make test-js`: `cards-js-wire-fuzz` | **hangs**: `conformance-harness wire-fuzz` and `node test/conformance/js/main.mjs wire-fuzz` deadlock at 0% CPU, and the recipe has no timeout. Completes on `main`. | owed; a worker should kill it rather than wait |

`generated-current` is green.

## dart — sprint/schema-dart, not forward-merged

The forward merge stopped on a design conflict. The stream (25d3ddd4f, dart/R28) and main (c7aa19e69, R28 and W16) each implement the dart guard chain, with different structures: the stream uses a two-lane `guard2` plus a chain, and main uses a chain stack, `links`, and a three-int pool. The conflicting files are `internal/codegen/darttable/fixedplan.go`, `fixedruntime.go` and `fixedmodule.go`. Choosing one design is not a merge decision. These are the reds at the unmerged base, 87be5063b, measured on amd-3 under load:

| red | cause | owner |
|---|---|---|
| `compiler`: `TestCTableMessageSave` PANIC | `ctable.fixedCollectTypes` nil dereference on a payload-free arm. Fixed on main by 444087041 and ca1a8a9dc. The panic aborts the suite, so every later test is unmeasured. | blocked on the guard-chain design decision |
| `compiler`: `TestPortingRegisterHoldsTheTree`, `TestPortingRegisterGateGoesRed` | M5 / dart: `tables-dart-keyed-negative-control` exists but nothing reaches it (not `make test`, not a release target, not a workflow) | owed on the dart stream |
| `compiler`: `TestIssue710PacketStringBounds/c` | `run: signal: killed` on a loaded CI runner. Not confirmed as a code red. | unconfirmed |

## main — 4d9941fbc, for reference

The compiler suite and `generated-current` are green. `make test-c` is red as in the c row above (#1943). `make test-js` is red on `tables-js-fixed-form`: the W6 partition, and an `ArgW` and `C5` plan entry carries 11 int32 lanes where 9 are wanted. `make test-cs` is red at `make/cs.mk:514`: `generated/bench/paired/cs/BenchTable.cs(8139,28)` and `(8140,33)` fail with CS9342, because operator resolution is ambiguous between `Int128Value`'s `<` and `>` and `Int128`'s. None of these three is carried into a stream base above.
