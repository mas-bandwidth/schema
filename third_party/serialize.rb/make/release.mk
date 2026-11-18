# THE RELEASE. A release vX.Y.Z is made by .github/workflows/release.yml and
# nowhere else: dispatched by hand with the version, on main, it runs the
# test suite on the pins and then make release, which tags that one commit
# with that one annotated tag and pushes only that tag. tools/release/release.sh
# refuses, by name, a version that does not parse as vMAJOR.MINOR.PATCH, one
# not greater than the newest v tag, an existing tag, and a commit not on main.
# The version lives in one place: the tag the tool makes.
#
#   gh workflow run release.yml --ref main -f version=vX.Y.Z
#
# Run here, make release VERSION=vX.Y.Z tags locally and pushes nothing
# (PUSH=1 pushes the tag).
VERSION        ?=
RELEASE_COMMIT ?= HEAD
RELEASE_MAIN   ?= origin/main
PUSH           ?=

.PHONY: release release-negative-control
release:
	@bash tools/release/release.sh '$(VERSION)' '$(RELEASE_COMMIT)' '$(RELEASE_MAIN)' '$(PUSH)'

# Each refusal of the release tool, by name, in a scratch repository under
# build/ with no remote; then one good tag at the commit asked for. This is
# the release tooling's dry run: it tags nothing in this repository.
release-negative-control:
	@bash tools/release/negative-control.sh
