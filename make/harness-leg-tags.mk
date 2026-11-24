# make/harness-leg-tags.mk — the conformance harness under every leg build tag.
#
# Parts of test/conformance/harness compile only under a leg's build tag
# (implementation_moo.go is `//go:build schema_leg_moo`), so `go vet ./...`
# and every untagged go test skip them: a syntax error there was green on
# every per-PR check and red only in ci-full.yml's nightly leg-tagged job.
# This target vets the package once per tag the package's own //go:build
# lines name, and once with all of them together (the nightly harness build,
# nightly-conformance.yml HARNESS_TAGS). The tags are read from the source,
# never listed here. ci-fast.yml runs it as the nightly-fast row
# harness-leg-tags (.github/nightly-fast-rows.txt) on any diff to the package.
HARNESS_LEG_TAGS = $(sort $(patsubst !%,%,$(shell sed -n 's|^//go:build ||p' test/conformance/harness/*.go | grep -oE '!?schema_leg_[a-z0-9_]+')))

.PHONY: harness-leg-tags-vet
harness-leg-tags-vet:
	@test -n "$(HARNESS_LEG_TAGS)" || { echo "harness-leg-tags-vet: no schema_leg_* build tag in test/conformance/harness: nothing would be vetted"; exit 1; }
	@set -e; for tag in $(HARNESS_LEG_TAGS); do \
		echo "go vet -tags $$tag ./test/conformance/harness"; \
		go vet -tags "$$tag" ./test/conformance/harness; \
	done
	@tags=$$(echo $(HARNESS_LEG_TAGS) | tr ' ' ,); \
		echo "go vet -tags $$tags ./test/conformance/harness"; \
		go vet -tags "$$tags" ./test/conformance/harness
