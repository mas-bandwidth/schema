# Equivalent mutants

Each `*.tsv` here lists surviving mutants whose change cannot alter observable
behaviour, so the nightly report suppresses them instead of asking for a test.
The columns are tab-separated:

    file	line	mutator	original	why

`file` is repository-relative, `line` is the mutant's line in that file,
`mutator` is the gremlins mutator name (`CONDITIONALS_NEGATION`, ...),
`original` is the exact source text that line must still hold, and `why` is the
justification. When the line no longer contains `original` the row is reported
as `STALE` and no longer suppresses anything: re-read the mutant or delete the
row, never edit it silently.

A row is not a way to retire a test. Prefer a generated-unit test in
`internal/codegen/gotable`; use a row only when the mutation is provably
equivalent (for example a bound no generated input can reach) and name the
argument in `why`.
