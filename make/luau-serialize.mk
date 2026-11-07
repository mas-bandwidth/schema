# make/luau-serialize.mk — the vendored serialize.luau port
# (docs/PORT-LUAU.md, THE RUNTIME LAYOUT).
#
# The type wire's bottom layer is the serialize.luau port, copied VERBATIM
# into internal/codegen/luau/runtime/ and pinned by SERIALIZE_PIN: the
# repository, the commit, and one sha256 per vendored file.
# TestVendoredLuauSerializeIsThePin holds the tree to that pin; no test runs
# this target. Bump the port with a checkout,
#
#   make vendor-serialize-luau LUAU_SERIALIZE_DIR=<checkout>
#
# or let the target clone the pinned commit into build/serialize.luau. Either
# way it re-copies src/serialize*.luau and rewrites SERIALIZE_PIN.
#
# The variables carry the LUAU_ prefix because the main Makefile already owns
# SERIALIZE_PIN and SERIALIZE_RUNTIME for the C++/C serialize checkouts.

LUAU_SERIALIZE_PIN_FILE := internal/codegen/luau/runtime/SERIALIZE_PIN
LUAU_SERIALIZE_RUNTIME  := internal/codegen/luau/runtime
LUAU_SERIALIZE_DIR      ?=
LUAU_SERIALIZE_REPO     := $(shell [ -f $(LUAU_SERIALIZE_PIN_FILE) ] && awk '$$1 == "repository" { print $$2 }' $(LUAU_SERIALIZE_PIN_FILE))
LUAU_SERIALIZE_COMMIT   := $(shell [ -f $(LUAU_SERIALIZE_PIN_FILE) ] && awk '$$1 == "commit" { print $$2 }' $(LUAU_SERIALIZE_PIN_FILE))

.PHONY: vendor-serialize-luau
vendor-serialize-luau:
	@if [ -z "$(LUAU_SERIALIZE_REPO)" ] || [ -z "$(LUAU_SERIALIZE_COMMIT)" ]; then \
		echo "vendor-serialize-luau: $(LUAU_SERIALIZE_PIN_FILE) does not name a repository and a commit"; \
		exit 1; \
	fi
	@if [ -n "$(LUAU_SERIALIZE_DIR)" ]; then \
		src="$(LUAU_SERIALIZE_DIR)"; \
		commit="$$(git -C "$$src" rev-parse HEAD 2>/dev/null || echo "$(LUAU_SERIALIZE_COMMIT)")"; \
	else \
		src="build/serialize.luau"; \
		rm -rf "$$src"; \
		git clone --quiet "$(LUAU_SERIALIZE_REPO)" "$$src"; \
		git -C "$$src" checkout --quiet "$(LUAU_SERIALIZE_COMMIT)"; \
		commit="$$(git -C "$$src" rev-parse HEAD)"; \
	fi; \
	if [ ! -d "$$src/src" ]; then \
		echo "vendor-serialize-luau: $$src/src is not a serialize.luau checkout"; \
		exit 1; \
	fi; \
	rm -f $(LUAU_SERIALIZE_RUNTIME)/serialize*.luau; \
	cp "$$src"/src/serialize*.luau $(LUAU_SERIALIZE_RUNTIME)/; \
	{ \
		echo "# Vendored serialize.luau pin (docs/PORT-LUAU.md, THE RUNTIME LAYOUT)."; \
		echo "# Format: repository <url>, commit <sha>, then one line per vendored file:"; \
		echo "#   sha256  <hash>  <file>"; \
		echo "# The files are copied verbatim from the port's src/ directory at the commit."; \
		echo "repository $(LUAU_SERIALIZE_REPO)"; \
		echo "commit $$commit"; \
		( cd $(LUAU_SERIALIZE_RUNTIME) && sha256sum serialize*.luau ) | sed 's/^/sha256  /'; \
	} > $(LUAU_SERIALIZE_PIN_FILE).tmp; \
	mv $(LUAU_SERIALIZE_PIN_FILE).tmp $(LUAU_SERIALIZE_PIN_FILE); \
	echo "vendor-serialize-luau: $$commit -> $(LUAU_SERIALIZE_RUNTIME)/ ($$(ls $(LUAU_SERIALIZE_RUNTIME)/serialize*.luau | wc -l | tr -d ' ') files)"
