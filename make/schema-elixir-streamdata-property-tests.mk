# make/schema-elixir-streamdata-property-tests.mk — StreamData property tests
# for the generated Elixir table codecs (schema-elixir-streamdata-property-tests).
#
# Included by the top-level Makefile's `include make/*.mk`. The target builds
# the generated Elixir table corpus (build/tables-generated-elixir/.stamp),
# installs the pinned StreamData package into a build-local Mix home if it is
# not already cached, and runs test/elixir-streamdata/property_main.exs over
# every generated Table module (variable-form and fixed-form tables alike).
#
# For every table descriptor the property generates values in range (exercising
# scalars, strings, bytes, enums, lists, fixed arrays, keyed arrays, maps,
# unions, pointers and blobs), saves each, loads it, checks that `load(save(v))`
# equals `v` under the spec's value equality and that the load report is clean,
# and saves the loaded value again requiring byte equality. A second property
# constructs non-canonical LEB128 wires requiring strict `{:error, :malformed, _}`
# refusal, and mutates the encoding (bit flips, truncation, random/zero bytes)
# requiring load to answer a value or a refusal tuple without raising.
#
# StreamData is pinned exactly and installed from its official source, Hex:
#   stream_data 1.4.0, https://repo.hex.pm (hex.pm package page)
# The install lives under build/ so no user home is written: MIX_HOME,
# HEX_HOME, MIX_DEPS_PATH and MIX_BUILD_PATH are all inside the checkout.
ELIXIR_STREAMDATA_DIR    := $(CURDIR)/test/elixir-streamdata
ELIXIR_STREAMDATA_BUILD  := $(CURDIR)/build/elixir-streamdata
ELIXIR_STREAMDATA_GEN    ?= $(CURDIR)/build/tables-generated-elixir
ELIXIR_STREAMDATA_RUNS   ?= 100
ELIXIR_STREAMDATA_PIN    := 1.4.0

# MIX_OS_CONCURRENCY_LOCK=0 keeps Mix off its TCP lock. On a host where the
# loopback listen is denied (a sandbox without distribution) deps.get dies with
# "failed to acquire filesystem lock using TCP, reason: :eperm" before it ever
# looks at the pinned package; the lock only serializes concurrent Mix runs and
# this target is one process against its own build directory.
ELIXIR_STREAMDATA_ENV := \
	MIX_HOME=$(ELIXIR_STREAMDATA_BUILD)/mix \
	HEX_HOME=$(ELIXIR_STREAMDATA_BUILD)/hex \
	MIX_DEPS_PATH=$(ELIXIR_STREAMDATA_BUILD)/deps \
	MIX_BUILD_PATH=$(ELIXIR_STREAMDATA_BUILD)/_build \
	MIX_OS_CONCURRENCY_LOCK=0

.PHONY: elixir-streamdata-deps
elixir-streamdata-deps:
	@mkdir -p $(ELIXIR_STREAMDATA_BUILD)/mix $(ELIXIR_STREAMDATA_BUILD)/hex \
		$(ELIXIR_STREAMDATA_BUILD)/deps $(ELIXIR_STREAMDATA_BUILD)/_build
	@if [ ! -d $(ELIXIR_STREAMDATA_BUILD)/deps/stream_data ]; then \
		echo "elixir-streamdata: installing stream_data $(ELIXIR_STREAMDATA_PIN) from hex.pm"; \
		cd $(ELIXIR_STREAMDATA_DIR) && \
			$(ELIXIR_STREAMDATA_ENV) $(MIX) local.hex --force >/dev/null 2>&1 && \
			$(ELIXIR_STREAMDATA_ENV) $(MIX) deps.get || { \
				echo "tool missing: stream_data $(ELIXIR_STREAMDATA_PIN) (official source https://repo.hex.pm)"; \
				exit 1; }; \
	fi

.PHONY: schema-elixir-streamdata-property-tests
schema-elixir-streamdata-property-tests: build/tables-generated-elixir/.stamp elixir-streamdata-deps
	@cd $(ELIXIR_STREAMDATA_DIR) && \
		$(ELIXIR_STREAMDATA_ENV) $(MIX) run property_main.exs \
			--gen $(ELIXIR_STREAMDATA_GEN) --n $(ELIXIR_STREAMDATA_RUNS)
	@echo 'elixir streamdata: every table round-trips by value and bytes and every mutated encoding is a clean read or refusal'

# THE NEGATIVE CONTROL. Three independent plants are verified:
# 1. SCHEMA_ELIXIR_STREAMDATA_PLANT=roundtrip corrupts the bytes between save
#    and load, so the round-trip property must go red naming the plant.
# 2. A lossy idempotent encoder (`int_bytes` clamping `v` with `min(v, 100)`)
#    is planted in a scratch copy of `tblp1/Table.ex`; `save(load(save(v)))`
#    still equals `save(v)`, so only `load(save(v)) == v` catches it.
# 3. A lax LEB128 reader (removing the trailing-zero-group check in `leb/3`)
#    is planted in a scratch copy of `tblp1/Table.ex`; the non-canonical
#    LEB128 check in `mutations` must go red.
.PHONY: schema-elixir-streamdata-property-tests-negative-control
schema-elixir-streamdata-property-tests-negative-control: build/tables-generated-elixir/.stamp elixir-streamdata-deps
	@mkdir -p $(ELIXIR_STREAMDATA_BUILD)
	@if cd $(ELIXIR_STREAMDATA_DIR) && \
			SCHEMA_ELIXIR_STREAMDATA_PLANT=roundtrip \
			$(ELIXIR_STREAMDATA_ENV) $(MIX) run property_main.exs \
				--gen $(ELIXIR_STREAMDATA_GEN) --n 1 \
			> $(ELIXIR_STREAMDATA_BUILD)/negative.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a corrupted save left the round-trip property green'; \
		cat $(ELIXIR_STREAMDATA_BUILD)/negative.log; exit 1; \
	fi
	@grep -q 'PLANT: elixir-streamdata round-trip corruption' $(ELIXIR_STREAMDATA_BUILD)/negative.log || \
		{ echo 'NEGATIVE CONTROL FAILED: the property went red without naming the plant'; \
		  cat $(ELIXIR_STREAMDATA_BUILD)/negative.log; exit 1; }
	@rm -rf $(ELIXIR_STREAMDATA_BUILD)/plant-lossy $(ELIXIR_STREAMDATA_BUILD)/plant-lax-leb
	@mkdir -p $(ELIXIR_STREAMDATA_BUILD)/plant-lossy/tblp1 $(ELIXIR_STREAMDATA_BUILD)/plant-lax-leb/tblp1
	@cp $(ELIXIR_STREAMDATA_GEN)/tblp1/Table.ex $(ELIXIR_STREAMDATA_GEN)/tblp1/TableRuntime.ex $(ELIXIR_STREAMDATA_BUILD)/plant-lossy/tblp1/
	@sed -i 's|defp int_bytes(v, 4, true), do: <<v::little-signed-32>>|defp int_bytes(v, 4, true), do: <<min(v, 100)::little-signed-32>>|' \
		$(ELIXIR_STREAMDATA_BUILD)/plant-lossy/tblp1/Table.ex
	@if cmp -s $(ELIXIR_STREAMDATA_GEN)/tblp1/Table.ex $(ELIXIR_STREAMDATA_BUILD)/plant-lossy/tblp1/Table.ex; then \
		echo 'NEGATIVE CONTROL FAILED: lossy int_bytes plant did not apply'; exit 1; fi
	@if cd $(ELIXIR_STREAMDATA_DIR) && \
			$(ELIXIR_STREAMDATA_ENV) $(MIX) run property_main.exs \
				--gen $(ELIXIR_STREAMDATA_BUILD)/plant-lossy --n 25 \
			> $(ELIXIR_STREAMDATA_BUILD)/negative-lossy.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: lossy int_bytes min(v, 100) left the round-trip property green'; \
		cat $(ELIXIR_STREAMDATA_BUILD)/negative-lossy.log; exit 1; \
	fi
	@grep -q 'loaded value != input value' $(ELIXIR_STREAMDATA_BUILD)/negative-lossy.log || \
		{ echo 'NEGATIVE CONTROL FAILED: lossy int_bytes failed without naming loaded value != input value'; \
		  cat $(ELIXIR_STREAMDATA_BUILD)/negative-lossy.log; exit 1; }
	@cp $(ELIXIR_STREAMDATA_GEN)/tblp1/Table.ex $(ELIXIR_STREAMDATA_GEN)/tblp1/TableRuntime.ex $(ELIXIR_STREAMDATA_BUILD)/plant-lax-leb/tblp1/
	@sed -i 's|if i > 0 and b == 0, do: :error|if false and i > 0 and b == 0, do: :error|' \
		$(ELIXIR_STREAMDATA_BUILD)/plant-lax-leb/tblp1/Table.ex
	@if cmp -s $(ELIXIR_STREAMDATA_GEN)/tblp1/Table.ex $(ELIXIR_STREAMDATA_BUILD)/plant-lax-leb/tblp1/Table.ex; then \
		echo 'NEGATIVE CONTROL FAILED: lax LEB128 plant did not apply'; exit 1; fi
	@if cd $(ELIXIR_STREAMDATA_DIR) && \
			$(ELIXIR_STREAMDATA_ENV) $(MIX) run property_main.exs \
				--gen $(ELIXIR_STREAMDATA_BUILD)/plant-lax-leb --n 5 \
			> $(ELIXIR_STREAMDATA_BUILD)/negative-lax-leb.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: lax LEB128 reader left the mutation property green'; \
		cat $(ELIXIR_STREAMDATA_BUILD)/negative-lax-leb.log; exit 1; \
	fi
	@grep -q 'failed to refuse non-canonical LEB128' $(ELIXIR_STREAMDATA_BUILD)/negative-lax-leb.log || \
		{ echo 'NEGATIVE CONTROL FAILED: lax LEB128 reader failed without naming non-canonical LEB128'; \
		  cat $(ELIXIR_STREAMDATA_BUILD)/negative-lax-leb.log; exit 1; }
	@echo 'elixir streamdata negative control: corrupted save, lossy int_bytes, and lax LEB128 reader all red the property suite'

.PHONY: test-elixir-streamdata
test-elixir-streamdata: schema-elixir-streamdata-property-tests schema-elixir-streamdata-property-tests-negative-control
