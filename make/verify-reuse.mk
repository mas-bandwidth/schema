# make/verify-reuse.mk — Cross-18-Port Dirty-Instance & Sub-Sliced
# Poison-Bracketed Buffer Reuse Gate (Round 3 Widening Track A), wired into
# `_verify-<lang>-bench` across all 9 compiled legs and all 9 VM legs.
#
#   make verify-reuse-<lang>             Dirty-instance reuse (A -> B -> C_bad -> A -> B) + [7x0xFF]/[13x0xFF] & [0xA5] sub-sliced reuse across Packet + Form 1/Retain + Form 2 + Form 3 + Form 4
#   make verify-reuse-negative-control   Sabotages BOTH dirty-instance Init reset and DataView byteOffset in jstable -> proves both halves fail verify-reuse immediately

VERIFY_REUSE_ALL_18_LEGS := cpp c cs unity go rust java dart elixir js python ruby lua luau squirrel moo goose lobster

# Each leg's gate is a rule of its own, written out by name (with the leg's
# toolchain), so a scan of the make files reads every gate the registry runs.
verify-reuse-cpp: bin/schema
verify-reuse-c: bin/schema
verify-reuse-cs: toolchain-cs bin/schema
verify-reuse-unity: toolchain-unity bin/schema
verify-reuse-go: bin/schema
verify-reuse-rust: bin/schema
verify-reuse-java: toolchain-java bin/schema
verify-reuse-dart: toolchain-dart bin/schema
verify-reuse-elixir: toolchain-elixir bin/schema
verify-reuse-js: verify-toolchain-js bin/schema
verify-reuse-python: verify-toolchain-python bin/schema
verify-reuse-ruby: verify-toolchain-ruby bin/schema
verify-reuse-lua: verify-toolchain-lua bin/schema
verify-reuse-luau: verify-toolchain-luau bin/schema
verify-reuse-squirrel: verify-toolchain-squirrel bin/schema
verify-reuse-moo: verify-toolchain-moo build/schema-moo
verify-reuse-goose: verify-toolchain-goose bin/schema
verify-reuse-lobster: verify-toolchain-lobster bin/schema

VERIFY_REUSE_ENV = \
	SERIALIZE="$(abspath $(SERIALIZE_RUNTIME))" SERIALIZE_GO="$(abspath $(SERIALIZE_GO_RUNTIME))" \
	CC="$(CC)" CXX="$(CXX)" DOTNET="$(DOTNET)" RUSTUP_BIN="$(RUSTUP_BIN)" \
	JAVA="$(JAVA)" JAVAC="$(JAVAC)" DART="$(DART)" BEAM_PATH="$(BEAM_PATH)" \
	NODE="$(NODE)" PYTHON3="$(PYTHON)" RUBY="$(RUBY)" LUA="$(LUA)" \
	LUAU="$(LUAU)" SQ="$(SQ)" MOO="$(MOO)" GOOSE="$(GOOSE)" LOBSTER="$(LOBSTER)"

build/verify-reuse-bin: $(wildcard test/verify-reuse/*.go) test/verify-reuse/Reuse.schema test/verify-reuse/ReuseV2.schema $(GO_SOURCES)
	@mkdir -p build
	go build -o $@.$$$$ ./test/verify-reuse && mv $@.$$$$ $@

# Made the way make/verify-widen-18.mk makes its corpus stamps (F-046): under
# the lock, regenerate unless the stamp is newer than every prerequisite, and
# exit with the generator's status, touching the stamp only on success.
build/verify-reuse/corpus/.stamp: build/verify-reuse-bin bin/schema
	@mkdir -p build/verify-reuse
	@lockdir="build/verify-reuse/.corpus.lock"; \
	while ! mkdir "$$lockdir" 2>/dev/null; do sleep 1; done; \
	trap '[ ! -d "$$lockdir" ] || rmdir "$$lockdir"' EXIT INT TERM HUP; \
	status=0; current=1; \
	for p in $^; do test "$@" -nt "$$p" || current=0; done; \
	if test "$$current" -eq 0; then \
		rm -f "$@"; \
		mkdir -p build/verify-reuse/corpus; \
		if SERIALIZE_GO="$(abspath $(SERIALIZE_GO_RUNTIME))" ./build/verify-reuse-bin --generate-only \
			--schema ./bin/schema \
			--work build/verify-reuse/corpus-gen \
			--corpus build/verify-reuse/corpus; then touch "$@"; else status=$$?; fi; \
	fi; \
	[ ! -d "$$lockdir" ] || rmdir "$$lockdir"; \
	trap - EXIT INT TERM HUP; \
	exit $$status

VERIFY_REUSE_GATES := $(addprefix verify-reuse-,$(VERIFY_REUSE_ALL_18_LEGS))
.PHONY: $(VERIFY_REUSE_GATES)
$(VERIFY_REUSE_GATES): verify-reuse-%: build/verify-reuse-bin build/verify-reuse/corpus/.stamp
	$(VERIFY_REUSE_ENV) CARGO_TARGET_DIR="$(CURDIR)/build/verify-reuse/cargo-$*" \
		./build/verify-reuse-bin --lang $* \
		--schema ./bin/schema --schema-moo ./build/schema-moo \
		--work build/verify-reuse --corpus build/verify-reuse/corpus

$(foreach L,$(VERIFY_REUSE_ALL_18_LEGS),$(eval _verify-$(L)-bench: verify-reuse-$(L)))

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
