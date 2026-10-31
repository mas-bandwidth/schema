# make/squirrel-serialize.mk — THE VENDORED SERIALIZE PORT (schema-squirrel-serialize-vendor;
# docs/PORT-SQUIRREL.md, THE RUNTIME LAYOUT): the type wire's bottom layer, its src files
# copied verbatim into internal/codegen/squirrel/serialize/ at the commit SQUIRREL_SERIALIZE_PIN
# names. This target re-copies them there and rewrites SQUIRREL_SERIALIZE_PIN for the next bump; no
# test runs it.
SQUIRREL_SERIALIZE_PIN := internal/codegen/squirrel/serialize/SERIALIZE_PIN
SQUIRREL_SERIALIZE_REPO   := $(shell sed -n 's/^repository //p' $(SQUIRREL_SERIALIZE_PIN))
SQUIRREL_SERIALIZE_COMMIT := $(shell sed -n 's/^commit //p' $(SQUIRREL_SERIALIZE_PIN))
SQUIRREL_SERIALIZE_RELEASE := $(shell sed -n 's/^release //p' $(SQUIRREL_SERIALIZE_PIN))
SQUIRREL_SERIALIZE_DIR    := build/serialize.nut

.PHONY: vendor-serialize-squirrel
vendor-serialize-squirrel:
	@command -v git >/dev/null || { echo "vendor-serialize-squirrel needs git"; exit 1; }
	@test -n "$(SQUIRREL_SERIALIZE_REPO)" && test -n "$(SQUIRREL_SERIALIZE_COMMIT)" || \
		{ echo "vendor-serialize-squirrel REFUSES: $(SQUIRREL_SERIALIZE_PIN) names no repository or commit"; exit 1; }
	@if [ -n "$(SERIALIZE_SQUIRREL_DIR)" ]; then \
		[ -d "$(SERIALIZE_SQUIRREL_DIR)/src" ] || \
			{ echo "SERIALIZE_SQUIRREL_DIR=$(SERIALIZE_SQUIRREL_DIR) has no src/"; exit 1; }; \
		SRC="$(SERIALIZE_SQUIRREL_DIR)"; \
		COMMIT=$$(git -C "$$SRC" rev-parse HEAD); \
	else \
		mkdir -p build; \
		if [ ! -d "$(SQUIRREL_SERIALIZE_DIR)/.git" ]; then git clone "$(SQUIRREL_SERIALIZE_REPO)" "$(SQUIRREL_SERIALIZE_DIR)"; fi; \
		git -C "$(SQUIRREL_SERIALIZE_DIR)" fetch --quiet origin; \
		git -C "$(SQUIRREL_SERIALIZE_DIR)" checkout --quiet --detach "$(SQUIRREL_SERIALIZE_COMMIT)"; \
		SRC="$(SQUIRREL_SERIALIZE_DIR)"; \
		COMMIT="$(SQUIRREL_SERIALIZE_COMMIT)"; \
	fi; \
	rm -f internal/codegen/squirrel/serialize/serialize*.nut; \
	cp "$$SRC"/src/serialize*.nut internal/codegen/squirrel/serialize/; \
	{ \
		echo "# The vendored serialize port (schema-squirrel-serialize-vendor; docs/PORT-SQUIRREL.md,"; \
		echo "# THE RUNTIME LAYOUT): the type wire's bottom layer. Every serialize*.nut here is copied"; \
		echo "# VERBATIM from the repository at the commit, and the sha256 below is each file's. The"; \
		echo "# test TestVendoredSquirrelSerializeIsThePin pins this file; make/squirrel-serialize.mk's"; \
		echo "# vendor-serialize-squirrel rewrites it on a bump (edit release and commit, then run it)."; \
		echo "repository $(SQUIRREL_SERIALIZE_REPO)"; \
		if [ -n "$(SQUIRREL_SERIALIZE_RELEASE)" ]; then echo "release $(SQUIRREL_SERIALIZE_RELEASE)"; fi; \
		echo "commit $$COMMIT"; \
		for f in $$(ls internal/codegen/squirrel/serialize/serialize*.nut | sort); do \
			echo "sha256 $$(sha256sum "$$f" | cut -d' ' -f1) $$(basename "$$f")"; \
		done; \
	} > $(SQUIRREL_SERIALIZE_PIN).tmp && mv $(SQUIRREL_SERIALIZE_PIN).tmp $(SQUIRREL_SERIALIZE_PIN); \
	echo "vendor-serialize-squirrel: vendored $$(ls internal/codegen/squirrel/serialize/serialize*.nut | wc -l) serialize files at $$COMMIT"
