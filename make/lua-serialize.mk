# make/lua-serialize.mk — vendor the serialize port as the type wire's bottom
# layer (docs/PORT-LUA.md, THE RUNTIME LAYOUT). The type-wire cards call this
# library rather than a private bit reader or writer. SERIALIZE_PIN names the
# vendored commit and one sha256 per file; vendor-serialize-lua clones the port
# at that commit into build/serialize.lua (or uses the checkout
# SERIALIZE_LUA_DIR names), re-copies its src files and rewrites the pin.

SERIALIZE_REPO    := https://github.com/mas-bandwidth/serialize.lua
SERIALIZE_BRANCH  := sprint/serialize-lua
SERIALIZE_LUA_DIR ?= build/serialize.lua
SERIALIZE_PIN     := internal/codegen/lua/runtime/SERIALIZE_PIN

.PHONY: vendor-serialize-lua
vendor-serialize-lua:
	@if [ ! -d $(SERIALIZE_LUA_DIR) ]; then \
		mkdir -p build; \
		echo "vendor-serialize-lua: cloning $(SERIALIZE_REPO) at $(SERIALIZE_BRANCH)"; \
		git clone --branch $(SERIALIZE_BRANCH) --single-branch $(SERIALIZE_REPO) $(SERIALIZE_LUA_DIR); \
	fi
	@commit=$$(git -C $(SERIALIZE_LUA_DIR) rev-parse HEAD); \
	echo "# Vendored from $(SERIALIZE_REPO)" > $(SERIALIZE_PIN); \
	echo "# at $(SERIALIZE_BRANCH), commit $$commit." >> $(SERIALIZE_PIN); \
	echo "# Rewrite with: make vendor-serialize-lua" >> $(SERIALIZE_PIN); \
	for f in $(SERIALIZE_LUA_DIR)/src/*.lua; do \
		b=$$(basename "$$f"); \
		h=$$(sha256sum "$$f" | cut -d" " -f1); \
		echo "sha256  $$h  $$b" >> $(SERIALIZE_PIN); \
		cp "$$f" internal/codegen/lua/runtime/$$b; \
	done; \
	echo "vendor-serialize-lua: wrote $(SERIALIZE_PIN) at $$commit"
