<!-- F-170 CORPUS CONTROL (green): the release-tag item stating each lane and what a fast-lane release does not run. internal/ci TestReleaseGuidePromisesWhatEachLaneRuns must accept it. -->
- **Release tags, `vMAJOR.MINOR.PATCH`.** A release is made only by
  `.github/workflows/release.yml`, dispatched by hand with the version, which
  runs `make release VERSION=vX.Y.Z PUSH=1`. It refuses, by name, a version
  that does not parse as `vMAJOR.MINOR.PATCH` (no prefix-less, pre-release or
  build forms, no leading zeros), a version that is not greater than the
  newest `v` tag already in the repository, a commit that is not on
  `main`, a patch release that moves the build version's form, and, before
  any of those, a commit whose compiler encodes different wire bytes than the
  newest `v` tag's under an equal protocol id (the previous-release
  differential). It tags that one commit with that one annotated tag and pushes
  nothing else, and the workflow then publishes the GitHub release on that
  tag: a release is never a bare tag. What must be green on the commit first
  depends on the release's lane, and the two lanes promise different things:

  - **The full lane** (`lane=full`, any release, and the only lane for a
    minor or major one): `make release` refuses unless a `ci-full.yml` run
    at that exact commit concluded success and its `proof` job's report is
    green there. That is the whole `go test`, the pins, every language's
    conformance legs, the negative controls, Windows, MSVC, big-endian and
    vuln, before the tag.
  - **The fast lane** (`lane=fast`, the workflow's default, patch releases
    only, Glenn, 2026-11-21: "Only run fast CI checks for the point
    releases."): `make release` refuses unless a `ci-fast.yml` run at that
    exact commit concluded success. Nothing more is proven before the tag.
    A fast-lane release does NOT run, before it is tagged: `ci-full.yml`
    (the whole `go test` with `SCHEMA_SLOW=1`, the pins, the conformance
    legs, the negative controls, Windows, MSVC, big-endian, vuln and the
    proof report), `certify.yml` (every `tables-<lang>-release` gate, the
    release wire-fuzz campaigns, the inline gate, the nightly negative
    controls), `nightly-conformance.yml`, the per-language hostile-corpus,
    fuzz, resource-bound and strict-analysis workflows, `tla-nightly.yml`,
    or `ci-fast.yml`'s dispatch-only leg gates; and the CI fast run it
    takes tested only what that run's planner selected from the push's
    diff. Those run nightly on `main` and on dispatch, so they may first
    see a fast-lane release's commit after it is published. The
    previous-release differential and the tagger's own refusals run in both
    lanes.
