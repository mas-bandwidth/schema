# make/lobster-serialize.mk — the vendored serialize.lobster port
# (docs/PORT-LOBSTER.md, THE RUNTIME LAYOUT).
#
# The type wire's bottom layer is the serialize.lobster port, copied VERBATIM
# into internal/codegen/lobster/runtime/ and pinned by SERIALIZE_PIN: the
# repository, the commit, and one sha256 per vendored file.
# TestLobsterVendoredSerializeIsThePin holds the tree to that pin; no test
# runs this target. Bump the port with a checkout,
#
#   make vendor-serialize-lobster LOBSTER_SERIALIZE_DIR=<checkout>
#
# or let the target clone the pinned commit into build/serialize.lobster.
# Either way it re-copies src/serialize*.lobster and rewrites SERIALIZE_PIN.
#
# The variables carry the LOBSTER_ prefix because the main Makefile already
# owns SERIALIZE_PIN and SERIALIZE_RUNTIME for the C++/C serialize checkouts.

LOBSTER_SERIALIZE_PIN_FILE := internal/codegen/lobster/runtime/SERIALIZE_PIN
LOBSTER_SERIALIZE_RUNTIME  := internal/codegen/lobster/runtime
LOBSTER_SERIALIZE_DIR      ?=
LOBSTER_SERIALIZE_REPO     := $(shell [ -f $(LOBSTER_SERIALIZE_PIN_FILE) ] && awk '$$1 == "repository" { print $$2 }' $(LOBSTER_SERIALIZE_PIN_FILE))
LOBSTER_SERIALIZE_COMMIT   := $(shell [ -f $(LOBSTER_SERIALIZE_PIN_FILE) ] && awk '$$1 == "commit" { print $$2 }' $(LOBSTER_SERIALIZE_PIN_FILE))

.PHONY: vendor-serialize-lobster
vendor-serialize-lobster:
	@if [ -z "$(LOBSTER_SERIALIZE_REPO)" ] || [ -z "$(LOBSTER_SERIALIZE_COMMIT)" ]; then \
		echo "vendor-serialize-lobster: $(LOBSTER_SERIALIZE_PIN_FILE) does not name a repository and a commit"; \
		exit 1; \
	fi
	@if [ -n "$(LOBSTER_SERIALIZE_DIR)" ]; then \
		src="$(LOBSTER_SERIALIZE_DIR)"; \
		commit="$$(git -C "$$src" rev-parse HEAD 2>/dev/null || echo "$(LOBSTER_SERIALIZE_COMMIT)")"; \
	else \
		src="build/serialize.lobster"; \
		rm -rf "$$src"; \
		git clone --quiet "$(LOBSTER_SERIALIZE_REPO)" "$$src"; \
		git -C "$$src" checkout --quiet "$(LOBSTER_SERIALIZE_COMMIT)"; \
		commit="$$(git -C "$$src" rev-parse HEAD)"; \
	fi; \
	if [ ! -d "$$src/src" ]; then \
		echo "vendor-serialize-lobster: $$src/src is not a serialize.lobster checkout"; \
		exit 1; \
	fi; \
	rm -f $(LOBSTER_SERIALIZE_RUNTIME)/serialize*.lobster; \
	cp "$$src"/src/serialize*.lobster $(LOBSTER_SERIALIZE_RUNTIME)/; \
	{ \
		echo "# Vendored serialize.lobster pin (docs/PORT-LOBSTER.md, THE RUNTIME LAYOUT)."; \
		echo "# Format: repository <url>, commit <sha>, then one line per vendored file:"; \
		echo "#   sha256  <hash>  <file>"; \
		echo "# The files are copied verbatim from the port's src/ directory at the commit."; \
		echo "repository $(LOBSTER_SERIALIZE_REPO)"; \
		echo "commit $$commit"; \
		( cd $(LOBSTER_SERIALIZE_RUNTIME) && sha256sum serialize*.lobster ) | sed 's/^/sha256  /'; \
	} > $(LOBSTER_SERIALIZE_PIN_FILE).tmp; \
	mv $(LOBSTER_SERIALIZE_PIN_FILE).tmp $(LOBSTER_SERIALIZE_PIN_FILE); \
	echo "vendor-serialize-lobster: $$commit -> $(LOBSTER_SERIALIZE_RUNTIME)/ ($$(ls $(LOBSTER_SERIALIZE_RUNTIME)/serialize*.lobster | wc -l | tr -d ' ') files)"
