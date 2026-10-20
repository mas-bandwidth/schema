# make/goose-serialize.mk — the vendored serialize.goose files
# (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT). Included by the Makefile's
# wildcard include; the leg registers nothing here.
#
# runtime/SERIALIZE_PIN names the repository, the commit and one sha256 per
# file. vendor-serialize-goose re-copies every listed file from that commit,
# for the next bump: update the pin (repository, commit, hashes), run it, and
# the runtime directory is again exactly the pin. No test runs this target.
GOOSE_SERIALIZE_PIN := $(CURDIR)/internal/codegen/goose/runtime/SERIALIZE_PIN
GOOSE_SERIALIZE_RUNTIME := $(CURDIR)/internal/codegen/goose/runtime

.PHONY: vendor-serialize-goose
vendor-serialize-goose:
	@set -e; \
	repo=$$(sed -n 's/^repository //p' $(GOOSE_SERIALIZE_PIN)); \
	commit=$$(sed -n 's/^commit //p' $(GOOSE_SERIALIZE_PIN)); \
	test -n "$$repo"; test -n "$$commit"; \
	dir=build/serialize-goose-vendor; \
	rm -rf $$dir; mkdir -p $$dir; \
	git -C $$dir init -q; \
	git -C $$dir remote add origin $$repo; \
	git -C $$dir fetch -q --depth=1 origin $$commit; \
	git -C $$dir checkout -q --detach FETCH_HEAD; \
	for f in $$(sed -n 's/^sha256  [0-9a-f][0-9a-f]*  //p' $(GOOSE_SERIALIZE_PIN)); do \
		cp $$dir/src/$$f $(GOOSE_SERIALIZE_RUNTIME)/$$f; \
	done; \
	cd $(GOOSE_SERIALIZE_RUNTIME) && sed -n 's/^sha256  //p' $(GOOSE_SERIALIZE_PIN) | sha256sum -c -
	@echo "vendor-serialize-goose: re-copied $$(sed -n 's/^commit //p' $(GOOSE_SERIALIZE_PIN)) into internal/codegen/goose/runtime"
