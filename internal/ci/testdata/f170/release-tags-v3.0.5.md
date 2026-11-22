<!-- F-170 CORPUS CASE (red): docs/VERSIONING.md release-tag item at v3.0.5 (f397f5f776), which promised full certification before every release. internal/ci TestReleaseGuidePromisesWhatEachLaneRuns must refuse it. -->
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
  tag: a release is never a bare tag. CI must be green on the commit, including the full
  certification run across every language, before the release is dispatched.
