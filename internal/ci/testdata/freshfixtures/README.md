# F-194: a failed fixture producer must not be masked by an earlier run

Case ids, each a step of `internal/ci`'s
TestFixtureProducerFailureIsNotMaskedByAnEarlierRun, which copies this
directory and `test/slowgate/fixtures` into a temporary directory and runs
`make` there:

- `F-194-fresh`: `make fresh` with a producer that works: exit 0, the consumer
  reads this run's file.
- `F-194`: the same rule again with a producer that exits 17 before writing:
  make must fail, and the earlier run's file must be gone.
- `F-194-empty`: a producer that exits 0 and writes nothing: make must fail.
- `F-194-again`: a working producer after the failures: exit 0, the new text.
- `F-194-unnamed`: `make unnamed`, a producer command that does not name
  `--work work`: refused (exit 2) before the producer runs.
- `F-194-old-shape` (the control that shows the defect): `make stale-shape`,
  the `|| true` recipe the real rules had, after one good run and with the
  failing producer: exit 0 and the old text.
