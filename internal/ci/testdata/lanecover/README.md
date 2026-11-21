# The lane-coverage gate's fixture corpus

`tree/` is a small repository: two packages, a build-tagged test file, a
Windows-only test file, a nested Go module, a Makefile and one scheduled
workflow (`workflows/nightly.yml`, which the test installs as
`.github/workflows/nightly.yml`). As committed, every test in it has a lane.

`cases/<id>/` is one planted fault, or one valid control, laid over the tree:

- `CASE.txt`: the id and one line saying what the case is;
- `workflows/nightly.yml`, `Makefile`: replace the tree's when present;
- `files/...`: files added to the tree; `remove.txt`: files taken out of it;
- `want.txt`: one line per error the gate must give, each a substring of it.
  An empty `want.txt` is a control: the gate must be green.

Ids starting `F-` are the independent reviewer's findings on PR #4520
(F-020 to F-026; F-026-arm64* are its architecture half, on the certify
macOS ARM64 runner's labels; F-219* are the selector-rewrite finding
as the lane gate sees it), each kept as her repro beside its control. `control` is the
unchanged tree, the control for every case that has none of its own.

`internal/ci`'s TestLaneCoverageFindingsCorpus runs every case and fails if a
finding's case is missing. To add a case, add a directory; no list in the
test names them, except the finding ids that must not disappear.
