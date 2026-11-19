# make/verify-vm.mk — Fast (< 2m) per-language optimization verification gates
# for the 9 dynamic / VM / pinned-toolchain targets:
#
#   js, python, ruby, lua, luau, squirrel, moo, goose, lobster
#
# Each language exposes three independent sub-targets sized for ci-fast.yml's
# strict 2-minute job ceiling (<= 70s cold per sub-target), plus one umbrella:
#
#   make verify-<lang>-conformance   all 21 table surfaces (--claims) + packet wire byte-exact check
#   make verify-<lang>-bench         packet wire 64-variant gate + Form-1 table 64-variant gate
#   make verify-<lang>-fuzz          deterministic wire-fuzz smoke (plain + --retain / --builder, 8-20s)
#   make verify-<lang>               runs all three sub-targets in a parallel Make DAG

VERIFY_VM_LEGS       := js python ruby lua luau squirrel moo goose lobster
VERIFY_JOBS          ?= $(shell nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)
VERIFY_JFLAG          = $(if $(findstring j,$(MAKEFLAGS)),,-j$(VERIFY_JOBS))
VERIFY_FUZZ_SEED     ?= 1
VERIFY_FUZZ_RANDOM_N ?= 2500
VERIFY_JSON_FUZZ_N   ?= 300

# Per-VM mutant caps tuned so each verify-<lang>-fuzz finishes in ~8-20s cold/warm
# while running all pinned hostile vectors, all manifest seeds, stateful reuse
# (empty -> badform -> truncated -> valid) on every live root, and rational-stride mutants.
VERIFY_FUZZ_MAX_js       ?= 14000
VERIFY_FUZZ_MAX_python   ?= 4000
VERIFY_FUZZ_MAX_ruby     ?= 3500
VERIFY_FUZZ_MAX_lua      ?= 15000
VERIFY_FUZZ_MAX_luau     ?= 14000
VERIFY_FUZZ_MAX_squirrel ?= 5000
VERIFY_FUZZ_MAX_moo      ?= 14000
VERIFY_FUZZ_MAX_goose    ?= 60000
VERIFY_FUZZ_MAX_lobster  ?= 40000

# Harness built with all VM build tags so `--claims` resolves compiler.Carried(<lang>)
# across every VM backend (including schema_leg_moo and schema_leg_python).
build/conformance-harness-vm: $(wildcard test/conformance/harness/*.go) $(GO_SOURCES) $(wildcard internal/codegen/moo*/runtime/*)
	@mkdir -p build
	go build -tags schema_leg_moo,schema_leg_python,schema_leg_ruby,schema_leg_squirrel,schema_leg_goose \
		-o $@ ./test/conformance/harness

# ---- Fast toolchain staging ($NOVA_SDK / dist/ fast path) ----

.PHONY: verify-toolchain-js verify-toolchain-python verify-toolchain-ruby \
	verify-toolchain-lua verify-toolchain-luau verify-toolchain-squirrel \
	verify-toolchain-moo verify-toolchain-goose verify-toolchain-lobster

verify-toolchain-js:
	@command -v "$(NODE)" >/dev/null 2>&1 || test -x "$(NODE)" || \
		{ echo "verify-js: missing node at $(NODE)" >&2; exit 1; }
	@$(call stage-sibling,$(SERIALIZE_JS_PIN),mas-bandwidth/serialize.js.git,https://github.com/mas-bandwidth/serialize.js.git,../serialize.js,JS)

verify-toolchain-python:
	@if [ ! -x "$(PYTHON)" ] || [ ! -x "$(PYTHON_FLOOR)" ]; then $(MAKE) --no-print-directory dist-python; fi
	@$(MAKE) --no-print-directory python-toolchain-check

verify-toolchain-ruby:
	@if [ ! -x "$(RUBY)" ] || [ ! -x "$(RUBY_FLOOR)" ]; then $(MAKE) --no-print-directory dist-ruby; fi
	@$(MAKE) --no-print-directory ruby-toolchain-check

verify-toolchain-lua:
	@test -x "$(LUA)" || command -v "$(LUA)" >/dev/null 2>&1 || $(MAKE) --no-print-directory dist-lua

verify-toolchain-luau:
	@test -x "$(LUAU)" || command -v "$(LUAU)" >/dev/null 2>&1 || $(MAKE) --no-print-directory dist-luau

verify-toolchain-squirrel:
	@test -x "$(SQ)" || command -v "$(SQ)" >/dev/null 2>&1 || $(MAKE) --no-print-directory dist-squirrel

verify-toolchain-moo:
	@if [ ! -x "$(MOO)" ] || [ ! -x "$(MOO_ANALYZE)" ]; then \
		$(MAKE) --no-print-directory dist-moo MOO_BUILD_JOBS=$(VERIFY_JOBS); \
	fi

verify-toolchain-goose:
	@test -x "$(GOOSE)" || command -v "$(GOOSE)" >/dev/null 2>&1 || $(MAKE) --no-print-directory dist-goose

verify-toolchain-lobster:
	@if [ ! -d "$(LOBSTER_DIST)/modules" ] && [ -d "$${NOVA_SDK:-/home/nova/sdk}/lobster-$(LOBSTER_VERSION)/modules" ]; then \
		mkdir -p "$(LOBSTER_DIST)" && ln -sfn "$${NOVA_SDK:-/home/nova/sdk}/lobster-$(LOBSTER_VERSION)"/* "$(LOBSTER_DIST)/"; \
	elif [ ! -x "$(LOBSTER)" ] && ! command -v "$(LOBSTER)" >/dev/null 2>&1; then \
		$(MAKE) --no-print-directory dist-lobster; \
	fi

# ---- Missing standalone packet & Form-1 table bench-gate targets ----

.PHONY: bench-js-gate tables-js-bench-gate \
	bench-lua-gate tables-luau-bench-gate \
	bench-squirrel-gate tables-squirrel-bench-gate \
	bench-moo-gate tables-moo-bench-gate \
	bench-goose-gate tables-goose-bench-gate \
	tables-lobster-bench-gate

bench-js-gate: verify-toolchain-js generated/bench/js/.stamp
	NODE_ENV=production $(NODE) bench/js/main.mjs --gate --wire-dir testdata/wire --variant-dir bench/corpus/variants
	$(NODE) bench/js/main.mjs --gate --wire-dir testdata/wire --variant-dir bench/corpus/variants

tables-js-bench-gate: verify-toolchain-js bin/schema generated/bench/paired/js/.stamp
	NODE="$(NODE)" bench/tables/js/leg build
	NODE="$(NODE)" bench/tables/js/leg run --gate
	$(MAKE) --no-print-directory tables-js-fixed-matched

bench-lua-gate: verify-toolchain-lua bin/schema
	LUA="$(LUA)" bench/lua/run --gate

tables-luau-bench-gate: verify-toolchain-luau generated/bench/tables/luau/.stamp
	LUAU="$(LUAU)" bench/tables/luau/leg build
	LUAU="$(LUAU)" bench/tables/luau/leg run --gate

bench-squirrel-gate: verify-toolchain-squirrel bin/schema
	SQ="$(SQ)" bench/squirrel/run --gate

tables-squirrel-bench-gate: verify-toolchain-squirrel bin/schema
	SQ="$(SQ)" bench/tables/squirrel/leg build
	SQ="$(SQ)" bench/tables/squirrel/leg run --gate

bench-moo-gate: verify-toolchain-moo build/schema-moo
	MOO="$(MOO)" bench/moo/run --gate

tables-moo-bench-gate: verify-toolchain-moo build/schema-moo
	MOO="$(MOO)" bench/tables/moo/leg build
	MOO="$(MOO)" bench/tables/moo/leg run --gate

bench-goose-gate: verify-toolchain-goose bin/schema
	@rm -rf build/bench/goose-gate-src && mkdir -p build/bench/goose-gate-src build/bench
	./bin/schema generate --lang goose --out build/bench/goose-gate-src bench/corpus/Bench.schema
	cp bench/goose/main.goose build/bench/goose-gate-src/
	"$(GOOSE)" build -O2 build/bench/goose-gate-src/main.goose -o build/bench/schema_bench_goose_gate
	cd bench/goose && ../../build/bench/schema_bench_goose_gate --gate

tables-goose-bench-gate: verify-toolchain-goose bin/schema
	GOOSE="$(GOOSE)" bench/tables/goose/leg build
	GOOSE="$(GOOSE)" bench/tables/goose/leg run --gate

tables-lobster-bench-gate: verify-toolchain-lobster generated/bench/tables/lobster/.stamp
	LOBSTER="$(LOBSTER)" bench/tables/lobster/leg build
	LOBSTER="$(LOBSTER)" bench/tables/lobster/leg run --gate

# ---- Umbrella wrapper macro (matches make/verify-compiled.mk) ----

define verify_vm_entrypoints
.PHONY: _verify-$(1)-all verify-$(1) verify-$(1)-conformance verify-$(1)-bench verify-$(1)-fuzz
_verify-$(1)-all: _verify-$(1)-conformance _verify-$(1)-bench _verify-$(1)-fuzz

verify-$(1):
	@$$(MAKE) $$(VERIFY_JFLAG) --no-print-directory _verify-$(1)-all

verify-$(1)-conformance:
	@$$(MAKE) $$(VERIFY_JFLAG) --no-print-directory _verify-$(1)-conformance

verify-$(1)-bench:
	@$$(MAKE) $$(VERIFY_JFLAG) --no-print-directory _verify-$(1)-bench

verify-$(1)-fuzz:
	@$$(MAKE) $$(VERIFY_JFLAG) --no-print-directory _verify-$(1)-fuzz
endef

$(foreach leg,$(VERIFY_VM_LEGS),$(eval $(call verify_vm_entrypoints,$(leg))))

# ---- 1. js ----

.PHONY: _verify-js-conformance _verify-js-bench _verify-js-fuzz
_verify-js-conformance: verify-toolchain-js build/conformance-harness-vm build/tables-generated-js/.stamp build/packet-defaults/js/.stamp packet-end-js packet-write-cfloat-js
	$(NODE) test/packet-defaults/js/main.mjs testdata/wire/packet-defaults
	NODE_ENV=production $(NODE) test/packet-defaults/js/main.mjs testdata/wire/packet-defaults
	@rm -rf build/verify/conformance-js && mkdir -p build/verify/conformance-js
	NODE="$(NODE)" ./build/conformance-harness-vm run --only js --claims --work build/verify/conformance-js

_verify-js-bench: bench-js-gate tables-js-bench-gate

_verify-js-fuzz: verify-toolchain-js build/conformance-harness-vm build/tables-generated-js/.stamp cards-js-generate
	@mkdir -p build/verify
	NODE="$(NODE)" ./build/conformance-harness-vm wire-fuzz $(WFR_FLAGS_js) \
		--driver "$(WFR_DRIVER_js)" \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_FUZZ_RANDOM_N) --max-mutants $(VERIFY_FUZZ_MAX_js) \
		--failed build/verify/fuzz-js-plain.bin
	NODE="$(NODE)" ./build/conformance-harness-vm wire-fuzz --retain $(WFR_FLAGS_js) \
		--driver "$(WFR_DRIVER_js)" \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_FUZZ_RANDOM_N) --max-mutants $(VERIFY_FUZZ_MAX_js) \
		--failed build/verify/fuzz-js-retain.bin
	NODE="$(NODE)" ./build/conformance-harness-vm json-fuzz --only js \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_JSON_FUZZ_N) \
		--failed build/verify/json-fuzz-js

# ---- 2. python ----

.PHONY: _verify-python-conformance _verify-python-bench _verify-python-fuzz
_verify-python-conformance: verify-toolchain-python build/conformance-harness-vm build/conformance-python packet-defaults-python packet-finite-python packet-void-python
	@rm -rf build/verify/conformance-python && mkdir -p build/verify/conformance-python
	PYTHON="$(PYTHON)" ./build/conformance-harness-vm run --only python --claims --work build/verify/conformance-python

_verify-python-bench: verify-toolchain-python bench-python-gate tables-python-bench-gate

_verify-python-fuzz: verify-toolchain-python build/conformance-harness-vm build/conformance-python
	@mkdir -p build/verify
	./build/conformance-harness-vm wire-fuzz \
		--driver "$(WFR_DRIVER_python)" \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_FUZZ_RANDOM_N) --max-mutants $(VERIFY_FUZZ_MAX_python) \
		--failed build/verify/fuzz-python-plain.bin
	./build/conformance-harness-vm wire-fuzz --retain \
		--driver "$(WFR_DRIVER_python)" \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_FUZZ_RANDOM_N) --max-mutants $(VERIFY_FUZZ_MAX_python) \
		--failed build/verify/fuzz-python-retain.bin
	PYTHON="$(PYTHON)" ./build/conformance-harness-vm json-fuzz --only python \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_JSON_FUZZ_N) \
		--failed build/verify/json-fuzz-python

# ---- 3. ruby ----

.PHONY: _verify-ruby-conformance _verify-ruby-bench _verify-ruby-fuzz
_verify-ruby-conformance: verify-toolchain-ruby build/conformance-harness-vm build/conformance-ruby packet-defaults-ruby packet-finite-ruby packet-void-ruby
	@rm -rf build/verify/conformance-ruby && mkdir -p build/verify/conformance-ruby
	RUBY="$(RUBY)" ./build/conformance-harness-vm run --only ruby --claims --work build/verify/conformance-ruby

_verify-ruby-bench: verify-toolchain-ruby bench-ruby-gate tables-ruby-bench-gate

_verify-ruby-fuzz: verify-toolchain-ruby build/conformance-harness-vm build/conformance-ruby
	@mkdir -p build/verify
	./build/conformance-harness-vm wire-fuzz \
		--driver "$(WFR_DRIVER_ruby)" \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_FUZZ_RANDOM_N) --max-mutants $(VERIFY_FUZZ_MAX_ruby) \
		--failed build/verify/fuzz-ruby-plain.bin
	./build/conformance-harness-vm wire-fuzz --retain \
		--driver "$(WFR_DRIVER_ruby)" \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_FUZZ_RANDOM_N) --max-mutants $(VERIFY_FUZZ_MAX_ruby) \
		--failed build/verify/fuzz-ruby-retain.bin
	RUBY="$(RUBY)" ./build/conformance-harness-vm json-fuzz --only ruby \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_JSON_FUZZ_N) \
		--failed build/verify/json-fuzz-ruby

# ---- 4. lua ----

.PHONY: _verify-lua-conformance _verify-lua-bench _verify-lua-fuzz
_verify-lua-conformance: verify-toolchain-lua build/conformance-harness-vm build/conformance-lua packet-defaults-lua packet-end-lua packet-finite-lua
	@rm -rf build/verify/conformance-lua && mkdir -p build/verify/conformance-lua
	LUA="$(LUA)" ./build/conformance-harness-vm run --only lua --claims --work build/verify/conformance-lua

_verify-lua-bench: verify-toolchain-lua bench-lua-gate tables-lua-bench-gate

# lua's retain wire-fuzz arm is owed in test/conformance/wire-fuzz-owed.txt;
# plain exercises all fixed + variable + message roots.
_verify-lua-fuzz: verify-toolchain-lua build/conformance-harness-vm build/conformance-lua
	@mkdir -p build/verify
	./build/conformance-harness-vm wire-fuzz \
		--driver "$(WFR_DRIVER_lua)" \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_FUZZ_RANDOM_N) --max-mutants $(VERIFY_FUZZ_MAX_lua) \
		--failed build/verify/fuzz-lua-plain.bin
	LUA="$(LUA)" ./build/conformance-harness-vm json-fuzz --only lua \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_JSON_FUZZ_N) \
		--failed build/verify/json-fuzz-lua

# ---- 5. luau ----

.PHONY: _verify-luau-conformance _verify-luau-bench _verify-luau-fuzz
_verify-luau-conformance: verify-toolchain-luau build/conformance-harness-vm build/conformance-luau packet-defaults-luau packet-end-luau packet-write-text-luau
	@rm -rf build/verify/conformance-luau && mkdir -p build/verify/conformance-luau
	LUAU="$(LUAU)" ./build/conformance-harness-vm run --only luau --claims --work build/verify/conformance-luau

_verify-luau-bench: verify-toolchain-luau bench-luau-gate tables-luau-bench-gate

_verify-luau-fuzz: verify-toolchain-luau build/conformance-harness-vm build/conformance-luau build/luau-driver/units.luau build/luaufuzzhost/luaufuzzhost
	@mkdir -p build/verify
	$(WFR_ENV_luau) LUAU="$(LUAU)" ./build/conformance-harness-vm wire-fuzz \
		--driver "$(WFR_DRIVER_luau)" \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_FUZZ_RANDOM_N) --max-mutants $(VERIFY_FUZZ_MAX_luau) \
		--failed build/verify/fuzz-luau-plain.bin
	$(WFR_ENV_luau) LUAU="$(LUAU)" ./build/conformance-harness-vm wire-fuzz --retain \
		--driver "$(WFR_DRIVER_luau)" \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_FUZZ_RANDOM_N) --max-mutants $(VERIFY_FUZZ_MAX_luau) \
		--failed build/verify/fuzz-luau-retain.bin
	LUAU="$(LUAU)" ./build/conformance-harness-vm json-fuzz --only luau \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_JSON_FUZZ_N) \
		--failed build/verify/json-fuzz-luau

# ---- 6. squirrel ----

.PHONY: _verify-squirrel-conformance _verify-squirrel-bench _verify-squirrel-fuzz
_verify-squirrel-conformance: verify-toolchain-squirrel build/conformance-harness-vm build/conformance-squirrel packet-end-squirrel packet-write-bareint-squirrel packet-write-cfloat-squirrel packet-write-text-squirrel
	@rm -rf build/verify/conformance-squirrel && mkdir -p build/verify/conformance-squirrel
	SQ="$(SQ)" ./build/conformance-harness-vm run --only squirrel --claims --work build/verify/conformance-squirrel

_verify-squirrel-bench: verify-toolchain-squirrel bench-squirrel-gate tables-squirrel-bench-gate

# Squirrel splits fixed-class roots (plain arm) and variable-class roots (`builder` / --builder);
# retain is owed in test/conformance/wire-fuzz-owed.txt.
_verify-squirrel-fuzz: verify-toolchain-squirrel build/conformance-harness-vm build/conformance-squirrel
	@mkdir -p build/verify
	./build/conformance-harness-vm wire-fuzz \
		--driver "$(SQ) test/conformance/squirrel/wirefuzz.nut" \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_FUZZ_RANDOM_N) --max-mutants $(VERIFY_FUZZ_MAX_squirrel) \
		--failed build/verify/fuzz-squirrel-fixed.bin
	./build/conformance-harness-vm wire-fuzz --builder \
		--driver "$(SQ) test/conformance/squirrel/wirefuzz.nut builder" \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_FUZZ_RANDOM_N) --max-mutants $(VERIFY_FUZZ_MAX_squirrel) \
		--failed build/verify/fuzz-squirrel-builder.bin
	SQ="$(SQ)" ./build/conformance-harness-vm json-fuzz --only squirrel \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_JSON_FUZZ_N) \
		--failed build/verify/json-fuzz-squirrel

# ---- 7. moo ----

.PHONY: _verify-moo-conformance _verify-moo-bench _verify-moo-fuzz
_verify-moo-conformance: verify-toolchain-moo build/conformance-harness-vm build/conformance-moo packet-end-moo packet-finite-moo packet-write-bareint-moo
	@rm -rf build/verify/conformance-moo && mkdir -p build/verify/conformance-moo
	MOO="$(MOO)" MOO_ANALYZE="$(MOO_ANALYZE)" ./build/conformance-harness-vm run --only moo --claims --work build/verify/conformance-moo

_verify-moo-bench: verify-toolchain-moo bench-moo-gate tables-moo-bench-gate

_verify-moo-fuzz: verify-toolchain-moo build/conformance-harness-vm build/conformance-moo
	@$(MAKE) --no-print-directory $(MOOFUZZHOST) MOO_BUILD_JOBS=$(VERIFY_JOBS)
	@mkdir -p build/verify
	$(WFR_ENV_moo) MOO="$(MOO)" ./build/conformance-harness-vm wire-fuzz \
		--driver "$(WFR_DRIVER_moo)" \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_FUZZ_RANDOM_N) --max-mutants $(VERIFY_FUZZ_MAX_moo) \
		--failed build/verify/fuzz-moo-plain.bin
	$(WFR_ENV_moo) MOO="$(MOO)" ./build/conformance-harness-vm wire-fuzz --retain \
		--driver "$(WFR_DRIVER_moo)" \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_FUZZ_RANDOM_N) --max-mutants $(VERIFY_FUZZ_MAX_moo) \
		--failed build/verify/fuzz-moo-retain.bin
	MOO="$(MOO)" MOO_ANALYZE="$(MOO_ANALYZE)" ./build/conformance-harness-vm json-fuzz --only moo \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_JSON_FUZZ_N) \
		--failed build/verify/json-fuzz-moo

# ---- 8. goose ----

.PHONY: _verify-goose-conformance _verify-goose-bench _verify-goose-fuzz
_verify-goose-conformance: verify-toolchain-goose build/conformance-harness-vm build/conformance-goose build/goose-packet/.stamp
	cd build/goose-packet/normal && "$(GOOSE)" run main.goose
	cd build/goose-packet/ludicrous && "$(GOOSE)" run main.goose
	@rm -rf build/verify/conformance-goose && mkdir -p build/verify/conformance-goose
	GOOSE="$(GOOSE)" ./build/conformance-harness-vm run --only goose --claims --work build/verify/conformance-goose

_verify-goose-bench: verify-toolchain-goose bench-goose-gate tables-goose-bench-gate

build/goose-wirefuzz-legs/.verify-stamp: $(GO_SOURCES) $(CONFORMANCE_UNIT_SCHEMAS) \
	$(filter internal/codegen/goose/% internal/codegen/goosetable/%,$(EMBED_SOURCES)) \
	test/conformance/goose/wirefuzz.goose test/conformance/goose/wirefuzz_io.c test/conformance/goose/wirefuzzdispatch/main.go
	@GOOSE_FUZZ_JOBS=$(VERIFY_JOBS) $(MAKE) --no-print-directory build/conformance-goose-wirefuzz
	@touch $@

_verify-goose-fuzz: verify-toolchain-goose build/conformance-harness-vm build/conformance-goose build/goose-wirefuzz-legs/.verify-stamp
	@mkdir -p build/verify
	./build/conformance-harness-vm wire-fuzz --mixed-builder \
		--driver "$(GOOSE_DISPATCH) $(GOOSE_LEGS_DIR)/legs" \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_FUZZ_RANDOM_N) --max-mutants $(VERIFY_FUZZ_MAX_goose) \
		--failed build/verify/fuzz-goose-plain.bin
	./build/conformance-harness-vm wire-fuzz --mixed-builder --retain \
		--driver "$(GOOSE_DISPATCH) $(GOOSE_LEGS_DIR)/legs" \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_FUZZ_RANDOM_N) --max-mutants $(VERIFY_FUZZ_MAX_goose) \
		--failed build/verify/fuzz-goose-retain.bin
	GOOSE="$(GOOSE)" ./build/conformance-harness-vm json-fuzz --only goose \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_JSON_FUZZ_N) \
		--failed build/verify/json-fuzz-goose

# ---- 9. lobster ----

.PHONY: _verify-lobster-conformance _verify-lobster-bench _verify-lobster-fuzz
_verify-lobster-conformance: verify-toolchain-lobster build/conformance-harness-vm build/conformance-lobster packet-end-lobster packet-write-bareint-lobster packet-write-cfloat-lobster
	@rm -rf build/verify/conformance-lobster && mkdir -p build/verify/conformance-lobster
	LOBSTER="$(LOBSTER)" ./build/conformance-harness-vm run --only lobster --claims --work build/verify/conformance-lobster

_verify-lobster-bench: verify-toolchain-lobster bench-lobster-gate tables-lobster-bench-gate

_verify-lobster-fuzz: verify-toolchain-lobster build/conformance-harness-vm build/conformance-lobster build/lobster-wirefuzzhost
	@mkdir -p build/verify
	LOBSTER="$(LOBSTER)" ./build/conformance-harness-vm wire-fuzz \
		--driver "$(WFR_DRIVER_lobster)" \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_FUZZ_RANDOM_N) --max-mutants $(VERIFY_FUZZ_MAX_lobster) \
		--failed build/verify/fuzz-lobster-plain.bin
	LOBSTER="$(LOBSTER)" ./build/conformance-harness-vm wire-fuzz --retain \
		--driver "$(WFR_DRIVER_lobster)" \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_FUZZ_RANDOM_N) --max-mutants $(VERIFY_FUZZ_MAX_lobster) \
		--failed build/verify/fuzz-lobster-retain.bin
	LOBSTER="$(LOBSTER)" ./build/conformance-harness-vm json-fuzz --only lobster \
		--seed $(VERIFY_FUZZ_SEED) --n $(VERIFY_JSON_FUZZ_N) \
		--failed build/verify/json-fuzz-lobster

# ---- Sabotage Negative Control ----
# Proves that a 1-bit / 1-constant corruption in an optimization-pass emitter
# causes the fast per-language verification gates (bench + conformance) to fail
# immediately on a byte mismatch.

.PHONY: verify-sabotage-negative-control
verify-sabotage-negative-control: verify-toolchain-js build/conformance-harness-vm build/tables-generated-js/.stamp
	@rm -rf build/verify-sabotage-nc && mkdir -p build/verify-sabotage-nc/bench
	@python3 -c 'import pathlib; src = pathlib.Path("internal/codegen/jstable/variableform.go").read_text(); target = "export const TableVariableForm = 1;"; assert target in src, "missing TableVariableForm anchor"; pathlib.Path("build/verify-sabotage-nc/variableform.go").write_text(src.replace(target, "export const TableVariableForm = 9;", 1))'
	@printf '{"Replace":{"%s/internal/codegen/jstable/variableform.go":"%s/build/verify-sabotage-nc/variableform.go"}}\n' "$(CURDIR)" "$(CURDIR)" > build/verify-sabotage-nc/overlay.json
	go build -overlay=build/verify-sabotage-nc/overlay.json -o build/verify-sabotage-nc/schema ./cmd/schema
	@sed 's/^fixed table /table /' bench/corpus/BenchTable.schema > build/verify-sabotage-nc/BenchTable.schema
	./build/verify-sabotage-nc/schema generate --lang js --out build/verify-sabotage-nc/bench build/verify-sabotage-nc/BenchTable.schema
	@if NODE="$(NODE)" $(NODE) bench/tables/js/form1_main.mjs build/verify-sabotage-nc/bench --gate > build/verify-sabotage-nc/bench.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: sabotaged JS Form-1 emitter passed tables-js-bench-gate" >&2; exit 1; fi
	@grep -q 'FAILED: bench_table' build/verify-sabotage-nc/bench.log || \
		{ echo "NEGATIVE CONTROL FAILED: unexpected bench failure log:" >&2; cat build/verify-sabotage-nc/bench.log >&2; exit 1; }
	@cp -a build/tables-generated-js build/verify-sabotage-nc/conformance-js
	./build/verify-sabotage-nc/schema generate --lang js --out build/verify-sabotage-nc/conformance-js/scalars tables/scalars
	@if SCHEMA_JS_GENERATED=build/verify-sabotage-nc/conformance-js NODE="$(NODE)" ./build/conformance-harness-vm run --only js --surface wire --work build/verify-sabotage-nc/work > build/verify-sabotage-nc/conf.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: sabotaged JS emitter passed conformance wire surface" >&2; exit 1; fi
	@echo "verify-sabotage-negative-control: 1-constant emitter corruption caught immediately by both bench gate and conformance"
