# make/verify-widen-18.mk — Cross-18-port Packet Bit-Phase Sweep (Pillar #1)
# and Canonical-Write Input Permutations (Pillar #5), wired into
# `_verify-<lang>-bench` across all 9 compiled legs and all 9 VM legs.
#
#   make verify-bitphase-<lang>              66 vectors across bit phases 0,1,7,31,32,33,56,63 + EmptyZero
#   make verify-canonical-<lang>             6 permutations (3 JSON + 3 native) of unsorted maps & distinct equal blobs
#   make verify-bitphase-negative-control    sabotage unaligned JS 32-bit read shift -> fails bitphase immediately
#   make verify-canonical-negative-control   sabotage JS map entry sort -> fails canonical immediately

VERIFY_ALL_18_LEGS := cpp c cs unity go rust java dart elixir js python ruby lua luau squirrel moo goose lobster

build/verify-bitphase-bin: $(wildcard test/verify-bitphase/*.go) test/verify-bitphase/cpp/oracle.cpp test/verify-bitphase/BitPhase.schema $(GO_SOURCES)
	@mkdir -p build
	go build -o $@ ./test/verify-bitphase

build/verify-bitphase/corpus/.stamp: build/verify-bitphase-bin bin/schema
	@mkdir -p build/verify-bitphase/corpus
	CXX="$(CXX)" ./build/verify-bitphase-bin --generate-only \
		--schema ./bin/schema \
		--work build/verify-bitphase \
		--corpus build/verify-bitphase/corpus
	@touch $@

build/verify-canonical-bin: $(wildcard test/verify-canonical/*.go) test/verify-canonical/Canonical.schema $(GO_SOURCES)
	@mkdir -p build
	go build -o $@ ./test/verify-canonical

build/verify-canonical/corpus/.stamp: build/verify-canonical-bin
	@mkdir -p build/verify-canonical/corpus
	./build/verify-canonical-bin --generate-only \
		--work build/verify-canonical \
		--corpus build/verify-canonical/corpus
	@touch $@

VERIFY_WIDEN_DEPS_cpp      := bin/schema
VERIFY_WIDEN_DEPS_c        := bin/schema
VERIFY_WIDEN_DEPS_cs       := toolchain-cs bin/schema
VERIFY_WIDEN_DEPS_unity    := toolchain-unity bin/schema
VERIFY_WIDEN_DEPS_go       := bin/schema
VERIFY_WIDEN_DEPS_rust     := bin/schema
VERIFY_WIDEN_DEPS_java     := toolchain-java bin/schema
VERIFY_WIDEN_DEPS_dart     := toolchain-dart bin/schema
VERIFY_WIDEN_DEPS_elixir   := toolchain-elixir bin/schema
VERIFY_WIDEN_DEPS_js       := verify-toolchain-js bin/schema
VERIFY_WIDEN_DEPS_python   := verify-toolchain-python bin/schema
VERIFY_WIDEN_DEPS_ruby     := verify-toolchain-ruby bin/schema
VERIFY_WIDEN_DEPS_lua      := verify-toolchain-lua bin/schema
VERIFY_WIDEN_DEPS_luau     := verify-toolchain-luau bin/schema
VERIFY_WIDEN_DEPS_squirrel := verify-toolchain-squirrel bin/schema
VERIFY_WIDEN_DEPS_moo      := verify-toolchain-moo build/schema-moo
VERIFY_WIDEN_DEPS_goose    := verify-toolchain-goose bin/schema
VERIFY_WIDEN_DEPS_lobster  := verify-toolchain-lobster bin/schema

VERIFY_WIDEN_ENV = \
	CC="$(CC)" CXX="$(CXX)" DOTNET="$(DOTNET)" RUSTUP_BIN="$(RUSTUP_BIN)" \
	JAVA="$(JAVA)" JAVAC="$(JAVAC)" DART="$(DART)" BEAM_PATH="$(BEAM_PATH)" \
	NODE="$(NODE)" PYTHON3="$(PYTHON)" RUBY="$(RUBY)" LUA="$(LUA)" \
	LUAU="$(LUAU)" SQ="$(SQ)" MOO="$(MOO)" GOOSE="$(GOOSE)" LOBSTER="$(LOBSTER)"

define verify_widen_18_rules
.PHONY: verify-bitphase-$(1) verify-canonical-$(1)

verify-bitphase-$(1): build/verify-bitphase-bin build/verify-bitphase/corpus/.stamp $$(VERIFY_WIDEN_DEPS_$(1))
	$$(VERIFY_WIDEN_ENV) CARGO_TARGET_DIR="$$(CURDIR)/build/verify-bitphase/cargo-$(1)" \
		./build/verify-bitphase-bin --lang $(1) \
		--schema ./bin/schema --schema-moo ./build/schema-moo \
		--work build/verify-bitphase --corpus build/verify-bitphase/corpus

verify-canonical-$(1): build/verify-canonical-bin build/verify-canonical/corpus/.stamp $$(VERIFY_WIDEN_DEPS_$(1))
	$$(VERIFY_WIDEN_ENV) CARGO_TARGET_DIR="$$(CURDIR)/build/verify-canonical/cargo-$(1)" \
		./build/verify-canonical-bin --lang $(1) \
		--schema ./bin/schema --schema-moo ./build/schema-moo \
		--work build/verify-canonical --corpus build/verify-canonical/corpus

_verify-$(1)-bench: verify-bitphase-$(1) verify-canonical-$(1)
endef

$(foreach L,$(VERIFY_ALL_18_LEGS),$(eval $(call verify_widen_18_rules,$(L))))

# ---- Sabotage Negative Controls for Bit-Phase & Canonical-Write Gates ----

.PHONY: verify-bitphase-negative-control verify-canonical-negative-control

# Sabotages the unaligned 32-bit static-cursor shift (`32-sh` -> `31-sh`) in
# internal/codegen/js/flat.go (leaving byte-aligned sh==0 untouched) and proves
# verify-bitphase-js fails immediately on unaligned bit phases.
verify-bitphase-negative-control: verify-toolchain-js build/verify-bitphase-bin build/verify-bitphase/corpus/.stamp
	@rm -rf build/verify-bitphase-nc && mkdir -p build/verify-bitphase-nc
	@python3 -c 'import pathlib; src = pathlib.Path("internal/codegen/js/flat.go").read_text(); target = "b0, sh, b0+4, 32-sh)"; assert target in src, "missing JS unaligned shift anchor"; pathlib.Path("build/verify-bitphase-nc/flat.gotext").write_text(src.replace(target, "b0, sh, b0+4, 31-sh)", 1))'
	@printf '{"Replace":{"%s/internal/codegen/js/flat.go":"%s/build/verify-bitphase-nc/flat.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/verify-bitphase-nc/overlay.json
	go build -overlay=build/verify-bitphase-nc/overlay.json -o build/verify-bitphase-nc/schema ./cmd/schema
	@if NODE="$(NODE)" ./build/verify-bitphase-bin --lang js --schema ./build/verify-bitphase-nc/schema --work build/verify-bitphase-nc/work --corpus build/verify-bitphase/corpus > build/verify-bitphase-nc/run.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: sabotaged unaligned JS bit-shift passed verify-bitphase" >&2; exit 1; fi
	@grep -q 'FAILED:' build/verify-bitphase-nc/run.log || \
		{ echo "NEGATIVE CONTROL FAILED: unexpected bitphase failure log:" >&2; cat build/verify-bitphase-nc/run.log >&2; exit 1; }
	@echo "verify-bitphase-negative-control: unaligned bit-shift sabotage caught immediately by verify-bitphase"

# Sabotages map entry sorting in internal/codegen/jstable/variablearms.go and
# proves verify-canonical-js fails immediately on unsorted input permutations.
verify-canonical-negative-control: verify-toolchain-js build/verify-canonical-bin build/verify-canonical/corpus/.stamp
	@rm -rf build/verify-canonical-nc && mkdir -p build/verify-canonical-nc
	@python3 -c 'import pathlib; src = pathlib.Path("internal/codegen/jstable/variablearms.go").read_text(); target = "out.sort((a, b) => TableVariableKeyOrder(fd, a.key, b.key));"; assert target in src, "missing JS map sort anchor"; pathlib.Path("build/verify-canonical-nc/variablearms.gotext").write_text(src.replace(target, "/* sabotaged unsorted */", 1))'
	@printf '{"Replace":{"%s/internal/codegen/jstable/variablearms.go":"%s/build/verify-canonical-nc/variablearms.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/verify-canonical-nc/overlay.json
	go build -overlay=build/verify-canonical-nc/overlay.json -o build/verify-canonical-nc/schema ./cmd/schema
	@if NODE="$(NODE)" ./build/verify-canonical-bin --lang js --schema ./build/verify-canonical-nc/schema --work build/verify-canonical-nc/work --corpus build/verify-canonical/corpus > build/verify-canonical-nc/run.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: sabotaged unsorted JS map emitter passed verify-canonical" >&2; exit 1; fi
	@grep -q 'FAILED:' build/verify-canonical-nc/run.log || \
		{ echo "NEGATIVE CONTROL FAILED: unexpected canonical failure log:" >&2; cat build/verify-canonical-nc/run.log >&2; exit 1; }
	@echo "verify-canonical-negative-control: unsorted map permutation sabotage caught immediately by verify-canonical"
