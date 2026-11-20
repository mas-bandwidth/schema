# make/verify-reuse.mk — Cross-18-Port Dirty-Instance & Sub-Sliced
# Poison-Bracketed Buffer Reuse Gate (Round 3 Widening Track A), wired into
# `_verify-<lang>-bench` across all 9 compiled legs and all 9 VM legs.
#
#   make verify-reuse-<lang>             Dirty-instance reuse (A -> B -> C_bad -> A -> B) + [7x0xFF]/[13x0xFF] & [0xA5] sub-sliced reuse across Packet + Form 1/Retain + Form 2 + Form 3 + Form 4
#   make verify-reuse-negative-control   Sabotages BOTH dirty-instance Init reset and DataView byteOffset in jstable -> proves both halves fail verify-reuse immediately

VERIFY_REUSE_ALL_18_LEGS := cpp c cs unity go rust java dart elixir js python ruby lua luau squirrel moo goose lobster

VERIFY_REUSE_DEPS_cpp      := bin/schema
VERIFY_REUSE_DEPS_c        := bin/schema
VERIFY_REUSE_DEPS_cs       := toolchain-cs bin/schema
VERIFY_REUSE_DEPS_unity    := toolchain-unity bin/schema
VERIFY_REUSE_DEPS_go       := bin/schema
VERIFY_REUSE_DEPS_rust     := bin/schema
VERIFY_REUSE_DEPS_java     := toolchain-java bin/schema
VERIFY_REUSE_DEPS_dart     := toolchain-dart bin/schema
VERIFY_REUSE_DEPS_elixir   := toolchain-elixir bin/schema
VERIFY_REUSE_DEPS_js       := verify-toolchain-js bin/schema
VERIFY_REUSE_DEPS_python   := verify-toolchain-python bin/schema
VERIFY_REUSE_DEPS_ruby     := verify-toolchain-ruby bin/schema
VERIFY_REUSE_DEPS_lua      := verify-toolchain-lua bin/schema
VERIFY_REUSE_DEPS_luau     := verify-toolchain-luau bin/schema
VERIFY_REUSE_DEPS_squirrel := verify-toolchain-squirrel bin/schema
VERIFY_REUSE_DEPS_moo      := verify-toolchain-moo build/schema-moo
VERIFY_REUSE_DEPS_goose    := verify-toolchain-goose bin/schema
VERIFY_REUSE_DEPS_lobster  := verify-toolchain-lobster bin/schema

VERIFY_REUSE_ENV = \
	SERIALIZE="$(abspath $(SERIALIZE_RUNTIME))" SERIALIZE_GO="$(abspath $(SERIALIZE_GO_RUNTIME))" \
	CC="$(CC)" CXX="$(CXX)" DOTNET="$(DOTNET)" RUSTUP_BIN="$(RUSTUP_BIN)" \
	JAVA="$(JAVA)" JAVAC="$(JAVAC)" DART="$(DART)" BEAM_PATH="$(BEAM_PATH)" \
	NODE="$(NODE)" PYTHON3="$(PYTHON)" RUBY="$(RUBY)" LUA="$(LUA)" \
	LUAU="$(LUAU)" SQ="$(SQ)" MOO="$(MOO)" GOOSE="$(GOOSE)" LOBSTER="$(LOBSTER)"

build/verify-reuse-bin: $(wildcard test/verify-reuse/*.go) test/verify-reuse/Reuse.schema test/verify-reuse/ReuseV2.schema $(GO_SOURCES)
	@mkdir -p build
	go build -o $@.$$$$ ./test/verify-reuse && mv $@.$$$$ $@

build/verify-reuse/corpus/.stamp: build/verify-reuse-bin bin/schema
	@mkdir -p build/verify-reuse
	@lockdir="build/verify-reuse/.corpus.lock"; \
	while ! mkdir "$$lockdir" 2>/dev/null; do sleep 1; done; \
	trap 'rmdir "$$lockdir" 2>/dev/null || true' EXIT INT TERM HUP; \
	if ! test -f "$@"; then \
		mkdir -p build/verify-reuse/corpus; \
		SERIALIZE_GO="$(abspath $(SERIALIZE_GO_RUNTIME))" ./build/verify-reuse-bin --generate-only \
			--schema ./bin/schema \
			--work build/verify-reuse/corpus-gen \
			--corpus build/verify-reuse/corpus && touch "$@"; \
	fi; \
	rmdir "$$lockdir" 2>/dev/null || true; \
	trap - EXIT INT TERM HUP

define verify_reuse_18_rules
.PHONY: verify-reuse-$(1)

verify-reuse-$(1): build/verify-reuse-bin build/verify-reuse/corpus/.stamp $$(VERIFY_REUSE_DEPS_$(1))
	$$(VERIFY_REUSE_ENV) CARGO_TARGET_DIR="$$(CURDIR)/build/verify-reuse/cargo-$(1)" \
		./build/verify-reuse-bin --lang $(1) \
		--schema ./bin/schema --schema-moo ./build/schema-moo \
		--work build/verify-reuse --corpus build/verify-reuse/corpus

_verify-$(1)-bench: verify-reuse-$(1)
endef

$(foreach L,$(VERIFY_REUSE_ALL_18_LEGS),$(eval $(call verify_reuse_18_rules,$(L))))

# ---- Sabotage Negative Control for Dirty-Instance & Sub-Slice Reuse Gate ----

.PHONY: verify-reuse-negative-control

# Proves BOTH halves of verify-reuse:
#  1. Sabotaging `Init%s(value);` in internal/codegen/jstable/messagebatch.go
#     leaves Maximal A's fields dirty on mbatch[0] when Minimal B is loaded -> fails immediately.
#  2. Sabotaging `bytes.byteOffset` -> `0` in internal/codegen/jstable/block.go
#     reads the 0xFF poison header instead of the +64 sub-slice -> fails immediately.
verify-reuse-negative-control: verify-toolchain-js build/verify-reuse-bin build/verify-reuse/corpus/.stamp
	@rm -rf build/verify-reuse-nc && mkdir -p build/verify-reuse-nc
	@python3 -c 'import pathlib; src = pathlib.Path("internal/codegen/jstable/messagebatch.go").read_text(); target = "  Init%s(value);\\n"; assert target in src, "missing JS Init reset anchor"; pathlib.Path("build/verify-reuse-nc/messagebatch.gotext").write_text(src.replace(target, "  /* sabotaged dirty-instance Init %s */\\n", 1))'
	@printf '{"Replace":{"%s/internal/codegen/jstable/messagebatch.go":"%s/build/verify-reuse-nc/messagebatch.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/verify-reuse-nc/overlay-dirty.json
	go build -overlay=build/verify-reuse-nc/overlay-dirty.json -o build/verify-reuse-nc/schema-dirty ./cmd/schema
	@if NODE="$(NODE)" ./build/verify-reuse-bin --lang js --schema ./build/verify-reuse-nc/schema-dirty --work build/verify-reuse-nc/work-dirty --corpus build/verify-reuse/corpus > build/verify-reuse-nc/dirty.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: sabotaged dirty-instance JS message reader passed verify-reuse" >&2; exit 1; fi
	@grep -q 'FAILED:' build/verify-reuse-nc/dirty.log || \
		{ echo "NEGATIVE CONTROL FAILED: unexpected dirty-instance failure log:" >&2; cat build/verify-reuse-nc/dirty.log >&2; exit 1; }
	@python3 -c 'import pathlib; src = pathlib.Path("internal/codegen/jstable/block.go").read_text(); target = "new DataView(bytes.buffer, bytes.byteOffset, bytes.length)"; assert target in src, "missing JS block byteOffset anchor"; pathlib.Path("build/verify-reuse-nc/block.gotext").write_text(src.replace(target, "new DataView(bytes.buffer, 0, bytes.length)", 2))'
	@printf '{"Replace":{"%s/internal/codegen/jstable/block.go":"%s/build/verify-reuse-nc/block.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/verify-reuse-nc/overlay-subslice.json
	go build -overlay=build/verify-reuse-nc/overlay-subslice.json -o build/verify-reuse-nc/schema-subslice ./cmd/schema
	@if NODE="$(NODE)" ./build/verify-reuse-bin --lang js --schema ./build/verify-reuse-nc/schema-subslice --work build/verify-reuse-nc/work-subslice --corpus build/verify-reuse/corpus > build/verify-reuse-nc/subslice.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: sabotaged JS DataView byteOffset passed verify-reuse" >&2; exit 1; fi
	@grep -q 'FAILED:' build/verify-reuse-nc/subslice.log || \
		{ echo "NEGATIVE CONTROL FAILED: unexpected subslice failure log:" >&2; cat build/verify-reuse-nc/subslice.log >&2; exit 1; }
	@echo "verify-reuse-negative-control: both dirty-instance and sub-slice byteOffset sabotages caught immediately by verify-reuse"
