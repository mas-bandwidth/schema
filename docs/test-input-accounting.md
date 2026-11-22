# Test input accounting

The rule (2026-11-22): every test input on disk is consumed and judged by
every leg that claims it, or the run fails and names the input and the leg.
A change to an input selects every leg that reads it, and a required job the
plan skips is green only when the plan shows the job reads nothing the
change touched.

## Where it is enforced

- **Selection.** `internal/inputfamilies` names the shared input families
  and derives each family's reader legs from the tree: a leg reads a family
  when its make target for the family is defined. `tools/inputfamilies
  select` runs in ci-fast.yml's plan and emits one `input-family` row per
  reader leg of every family the diff touches (every family on a push to
  main), and adds the Go packages whose source names a changed input to the
  go test selection. `tools/inputfamilies check` (and `go test
  ./internal/inputfamilies`) refuse a family with no input on disk, no
  reader, or a driver directory whose leg has no target.
- **Consumption.** `tools/inputaccount` runs each row. It enumerates the
  family's inputs from disk (`inputfamilies.Account`), runs the leg's target
  with every other reader of the family held as made (`make -o`), and records
  every open of a file in the input directories through the kernel
  (inotify). An input the leg never opened fails the row, named with the
  leg. No driver runs this check, so no driver can skip it.
- **Canaries.** Each row then plants an extra input (a copy of the first
  input under a new name) and, separately, a wrong answer (the first byte of
  one input flipped). Each planted run must go red; a canary that stays
  green fails the row.
- **Push to main.** Every push to main runs every leg's verify rows, every
  clean gate and lint, and every input family's every reader, with no path
  filter. The verify rows have no cap: a shared edit gets all eighteen legs.

## The families

| Family | Inputs | Readers | Set from | Consumed check | Plan selects readers |
|---|---|---|---|---|---|
| packet-defaults | `testdata/wire/packet-defaults/` (.bin, .bits), `test/conformance/packet/testdata/constant-defaults/`, `test/packet-defaults/*` | `packet-defaults-<leg>`, 17 legs; Go tests in cpp, lobster, python, ruby, test/conformance/packet | drivers: hand-typed pin names, some a wildcard listing | inputaccount, every row | family + go (f2e1c41 selected nothing before) |
| packet-end | `testdata/wire/packet-end/`, `test/packet-end/*` | `packet-end-<leg>`, 14 legs | wildcard in goose, lua, squirrel; hard names elsewhere | inputaccount | family |
| packet-finite, packet-void, packet-write-bareint, packet-write-cfloat, packet-write-text | `test/packet-<x>/*` (schemas, `cases.txt`) | `packet-<x>-<leg>` | `cases.txt`; mustrun counts | inputaccount | family |
| conformance corpus | `testdata/conformance/tables/` | `verify-<leg>-conformance`, 18 legs; the harness | `MANIFEST.txt` | inputaccount over the manifest and `wire/*.bin`; the harness checks rows against files | family + go |
| hostile corpus | `internal/ci/hostilecorpus/corpus/` | `schema-<leg>-runs-hostile-corpus`, 18 legs; Go engine tests | manifest; per-port counts typed | source trees swept (`corpus.go` Uncovered); inputaccount owed | the per-port runs are the heavy workflows moved off pull requests (2026-11-15), so not a PR row; the Go tests run in ci-policy on every PR |
| hostile ledgers | `test/*-hostile/known-{skips,findings}.tsv` | the leg's hostile target | hand list | both ways | the leg's own path rule |
| feature witnesses | `testdata/conformance/feature-witnesses/` | most legs' make files, Go tests | hand-typed paths | none | go |
| wire vector sets | `testdata/wire/{non-canonical,skipped-value,pointer-target-type,message-one-encoding,fixed-empty-batch,json-*}` | `<lang>table` Go tests, harness, compiler spec tests | manifests and hard names | manifest rows; no reverse sweep | go |
| table wire corpus, fuzz vectors | `testdata/wire/tables/` | harness wire-fuzz, Go tests, leg make files | `INDEX.txt`, framing script list | framing both ways; fuzz vectors not (being fixed separately) | go |
| top-level wire pins | `testdata/wire/*.bin`, `*.bits` | per-leg drivers `test/<leg>/`, `bench/` | hand-typed names | none | go only |
| goldens | `testdata/golden/` | `internal/goldens` | ReadDir | yes, both ways | `./internal/goldens` |
| examples, bench corpus, guard | `examples/`, `examples128/`, `bench/corpus/`, `test/guard/` | every leg's generate step | wildcard | canonical-format sweep | lints and goldens on a PR; every leg on a push to main |
| cards corpus | `tables/cards/` | `conformance-harness cards`, 18 facades | manifest + ReadDir | refuses empty sets | broad rule: every verify leg |
| fuzz seed corpora | `*/testdata/fuzz/Fuzz*/` | go test replay | go test | go replays every file | package-local owner rule |
| nightly fuzz saved inputs | `testdata/{lua,luau}-nightly-fuzz/`, `testdata/moo-wire-fuzz-pinned-controls/` | nightly fuzz make files, internal/ci tests | hand lists | luau INPUTS and moo list both ways | go |
| JSONTestSuite | `testdata/jsontestsuite/` | tabletext, gotable, luautable, mootable, pythontable | ReadDir + SHA256SUMS | both ways | go |
| skip ledger | `.github/skip-ledger.txt` | `tools/gotestreport -skips`, internal/ci | hand list | every skip names a ledger reason | ci-policy always |
