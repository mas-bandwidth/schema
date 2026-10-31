# make/lua-serialize.mk — vendor the serialize port as the type wire's bottom
# layer (docs/PORT-LUA.md, THE RUNTIME LAYOUT). The type-wire cards call this
# library rather than a private bit reader or writer. The vendored runtime is a
# RELEASE of serialize.lua: SERIALIZE_LUA_PIN is its tag and SERIALIZE_LUA_COMMIT
# the commit that tag must resolve to (the shape of the Makefile's
# SERIALIZE_GO_PIN / SERIALIZE_GO_COMMIT). vendor-serialize-lua clones the port
# at that tag into build/serialize.lua (or uses the checkout SERIALIZE_LUA_DIR
# names), refuses a checkout whose HEAD is not the pinned commit, re-copies its
# src files and rewrites LUA_SERIALIZE_PIN: the tag, the commit and one sha256
# per file. internal/codegen/lua/serialize_vendor_test.go holds the vendored
# files to that pin and the pin to these two variables.

LUA_SERIALIZE_REPO   := https://github.com/mas-bandwidth/serialize.lua
SERIALIZE_LUA_PIN    := v2.1.1
SERIALIZE_LUA_COMMIT := 90e7c32695a891f6323f20878094dd992b768c3d
SERIALIZE_LUA_DIR    ?= build/serialize.lua
LUA_SERIALIZE_PIN    := internal/codegen/lua/runtime/SERIALIZE_PIN

.PHONY: vendor-serialize-lua
vendor-serialize-lua:
	@if [ ! -d $(SERIALIZE_LUA_DIR) ]; then \
		mkdir -p build; \
		echo "vendor-serialize-lua: cloning $(LUA_SERIALIZE_REPO) at $(SERIALIZE_LUA_PIN)"; \
		git clone --quiet --depth 1 --branch $(SERIALIZE_LUA_PIN) $(LUA_SERIALIZE_REPO) $(SERIALIZE_LUA_DIR) || exit 1; \
	fi
	@commit=$$(git -C $(SERIALIZE_LUA_DIR) rev-parse HEAD); \
	if [ "$$commit" != "$(SERIALIZE_LUA_COMMIT)" ]; then \
		echo "vendor-serialize-lua REFUSES: $(SERIALIZE_LUA_DIR) is at $$commit; $(SERIALIZE_LUA_PIN) is pinned to $(SERIALIZE_LUA_COMMIT)"; \
		exit 1; \
	fi; \
	rm -f internal/codegen/lua/runtime/serialize*.lua; \
	echo "# Vendored from $(LUA_SERIALIZE_REPO)" > $(LUA_SERIALIZE_PIN); \
	echo "# tag $(SERIALIZE_LUA_PIN)" >> $(LUA_SERIALIZE_PIN); \
	echo "# commit $(SERIALIZE_LUA_COMMIT)" >> $(LUA_SERIALIZE_PIN); \
	echo "# Rewrite with: make vendor-serialize-lua (make/lua-serialize.mk holds the tag and commit)" >> $(LUA_SERIALIZE_PIN); \
	for f in $(SERIALIZE_LUA_DIR)/src/*.lua; do \
		b=$$(basename "$$f"); \
		h=$$(shasum -a 256 "$$f" | cut -d" " -f1); \
		echo "sha256  $$h  $$b" >> $(LUA_SERIALIZE_PIN); \
		cp "$$f" internal/codegen/lua/runtime/$$b; \
	done; \
	echo "vendor-serialize-lua: wrote $(LUA_SERIALIZE_PIN) at $(SERIALIZE_LUA_PIN) ($(SERIALIZE_LUA_COMMIT))"
