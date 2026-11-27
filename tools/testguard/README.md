# testguard

Fail a CI run whose tests did not really run, or did not pass. A test run can
exit 0 while a harness printed "30 seeds absent", while a suite skipped every
test on every pull request, or while a filter left a package with nothing to
run. Each is green until someone reads the log. `testguard check` reads the
run's result files and the repository's checked-in config, and exits 1, naming
each cause, when:

1. **a test failed or the run is incomplete.** A failure, an error, a test that
   started and never ended, a count line with failures, a TAP bail-out or unmet
   plan, or a go package that failed outside its tests.
2. **a skip is not in the ledger.** Every skipped, ignored or pending test must
   be listed in the config with a reason. The match is on the exact set: a
   ledgered skip that now runs, or is gone, is red too, so the ledger only
   shrinks when the tests are fixed.
3. **a suite is missing or empty.** Every suite the config registers must be in
   the results and run at least one test, and every suite that ran tests must be
   registered, so its disappearance cannot go unseen.
4. **a required coverage table has a gap.** The config can name a table, such as
   every corpus root against every port driver: a source for its rows, one for
   its columns, and one for the pairs a test covers. Every pair is covered, or
   held as a gap with a reason.
5. **the run and the baseline differ.** A test the baseline lists did not run,
   or a test ran that the baseline does not list.

It never passes silently: no result files, a file of no known format, a run of
0 tests, a config that registers no suite or names no baseline, a baseline it
cannot read, and a coverage command that fails are each refused (exit 2) with a
line naming the cause.

## Run

Run it from the repository root, as `go run ./tools/testguard`.
It is one binary with no service or network access. It reads files and runs only
the coverage commands the config names. `testguard help` prints the manual
and changes nothing.

## Results

A result file's format comes from its extension, or from a prefix on its path
(`junit:`, `gojson:`, `tap:`, `count:`), never from its content:

| Format | Extensions | Suite | Test | Skip | Failure |
|---|---|---|---|---|---|
| JUnit XML | `.xml` | each `<testsuite>`'s name, nested to any depth, in every root of the file; a suite with no name takes its parent's, or the file's | `CLASSNAME.NAME`, or `NAME` when the class is the suite | a `<skipped>` child, or a status of `skipped`, `notrun` or `disabled` | a `<failure>` or `<error>` child |
| `go test -json` | `.json`, `.jsonl` | the package | the test, subtests included (`TestA/sub`) | a `skip` in any iteration | a `fail`, or a `run` with no end (a panic); a package `fail` is an incomplete run |
| TAP | `.tap` | the file's name without its extension | the description, or the number when there is none; a subtest, indented four spaces a level, is `PARENT/NAME` | a directive starting `skip` or `todo`, in any case | `not ok` without one; a missing, unmet or second plan, a repeated number, an unended YAML block or `Bail out!` is an incomplete run |
| `ran N, failed M` | `.log`, `.txt`, `.out` | the text before the colon (`cards (lua): ran 7, failed 0`), or the file's name | none: a count | none | `failed M` above 0 |

A count log's last line that is not blank is its count line: the line a
harness prints as it ends, so a log that ends in anything else is refused (the
run died after it counted, or never counted). A count suite has no test names,
so the skip check does not reach it; its floor in the baseline does (below).
TAP YAML blocks (`---` to `...`) are skipped; a TAP line at an indent that is
not a multiple of four spaces, or holds a tab, is refused. Unparseable JUnit,
text after its last root, a `<testcase>` with no name, and a line that is not a
go test event are refused.

## The config

One entry per line, fields separated by tabs, with no blanks at either end of a
field; blank lines and lines starting with `#` are left out. Paths are relative
to the working directory.

```text
# tests/testguard.tsv
suite	example.com/wire
suite	cards (lua)
skip	example.com/wire	TestBigEndian	needs a big-endian runner; covered nightly
cover	corpus	rows	file	tests/corpus-roots.txt
cover	corpus	columns	run	ls drivers
cover	corpus	pairs	run	sh tests/covered-pairs.sh
gap	corpus	fx9	lua	the Lua port has no codec for fx9 yet
baseline	tests/testguard
```

| Entry | Fields | |
|---|---|---|
| `suite` | NAME | a suite that must run at least one test; a config has one at least |
| `skip` | SUITE, TEST, REASON | a skip that is allowed, and must happen |
| `cover` | TABLE, `rows`, `columns` or `pairs`, `file` or `run`, PATH or COMMAND | one source of a coverage table; a table names all three |
| `gap` | TABLE, ROW, COLUMN, REASON | a pair allowed to be uncovered |
| `baseline` | DIR | the baseline directory; required |

Rows and columns are one name a line; pairs are `ROW<TAB>COLUMN` lines. A `run`
source runs under `bash -e -o pipefail -c` in the working directory, so a
failure anywhere in it fails it, and its standard output is the list.

## The baseline

The baseline is a directory with one empty file per test that runs:
`DIR/SUITE/TEST`, for example `tests/testguard/example.com%2Fwire/TestA%2Fsub`.
The suite and the test are each escaped so every filesystem git checks out on
can hold them: letters, digits, `_`, `-` and dots inside the name stay, every
other byte is `%XX` (so `/ \ : * ? " < > |` and blanks never reach a file name),
a name Windows reserves (`CON`, `NUL`, `COM1`, ...) has its first letter
escaped, and a name longer than 200 bytes is cut and ends in `~` and 16 hex
digits of its SHA-256. A test listed there that does not run is red
(`vanished`), and a test that runs with no entry is red (`unlisted`). A pull
request that adds a test adds its file; one that removes a test deletes its
file. Each test is its own file, so pull requests landing side by side conflict
only when they add or remove the same test, and there is no refresh step. On a
filesystem that ignores case, two tests whose names differ only in case share an
entry.

A count suite names no tests, so its entry is `DIR/SUITE/#count`, holding the
fewest tests its count line may report: fewer is red (`vanished`), and a count
suite with no floor is red (`unlisted`). Raise the floor as tests are added;
lower it in the change that removes them.

`--write-baseline` bootstraps it: when the run's only findings are unlisted
tests and missing floors, it writes them, making the directory if it is missing;
with any other finding it writes nothing. It never removes an entry, and it
always exits 1, so a workflow that passes it can never go green: commit what it
wrote and check again without it. A `.gitkeep` is never an entry; anything else
in the directory that is not a registered suite's entry is red (`baseline`).

## Check

```sh
go test -json ./... > go.json
testguard check --config tests/testguard.tsv go.json
```

Each finding is one line (a test is named `SUITE<TAB>TEST`), then a summary:

```text
RED skip example.com/wire	TestBigEndian: skipped, and the ledger does not hold it; fix: make it run, or add the line skip<TAB>SUITE<TAB>TEST<TAB>REASON to tests/testguard.tsv
RED uncovered corpus fx2 x lua: no test covers this pair, and no gap holds it; fix: cover it, or add a gap line with the reason to tests/testguard.tsv
RED: 2 findings (skip 1, uncovered 1); read 1 result files: 4 suites, 812 tests run, 1 skipped; 1 coverage tables, 38 pairs
```

The rules a finding names:

| Rule | What it means |
|---|---|
| `failed` | a test that failed or never ended; a count line with failures |
| `incomplete` | a bail-out, an unmet or missing plan, a repeated test number, a package that failed outside its tests |
| `skip` | a skipped test the ledger does not hold |
| `stale-skip` | a ledgered skip that did not skip |
| `missing-suite` | a registered suite absent from every result file |
| `empty-suite` | a registered suite that ran 0 tests |
| `unregistered-suite` | a suite that ran tests and is not registered |
| `uncovered` | a table pair no test covers and no gap holds; an empty rows or columns list; a covered pair outside the table |
| `stale-gap` | a gap whose pair is covered, or that names no row or column of the table |
| `vanished` | a test the baseline lists that did not run; a count below its floor |
| `unlisted` | a test that ran and that the baseline does not list; a count suite with no floor |
| `baseline` | an entry in the baseline that is not a registered suite's test |

Exit codes: 0 every test that should run ran and passed; 1 findings; 2 refused.

## Refusals

Each refusal is one stderr line ending in a command to run.

| Refusal | Exit |
|---|---|
| `testguard check: wants --config and result files (none were read, and reading none is never a pass); run: go run ./tools/testguard check -h` | 2 |
| `testguard check: <file> has no format: ...; run: go run ./tools/testguard check -h` | 2 |
| `testguard check: the result files hold 0 tests that ran; a run of nothing is never a pass; run: ls -l <files>` | 2 |
| `testguard check: <config> has no suite line (a config that registers nothing checks nothing) and no baseline line; run: go run ./tools/testguard help check` | 2 |
| `testguard check: the baseline <dir> cannot be read: ...; run: ls -ld <dir>` | 2 |
| `testguard check: table <table> <axis>: the command "<command>" failed: ...; run: <command>` | 2 |
