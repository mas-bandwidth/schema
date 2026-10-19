# make/schema-elixir-streamdata-property-tests.mk — the Elixir leg's
# StreamData property lens (ideas roadmap group schema-per-language-lenses).
#
# The corpus is the one the Makefile's generate targets already read: the
# generated table units under build/tables-generated-elixir (ELIXIR_TABLE_UNITS
# in make/elixir.mk). Nothing here invents a second corpus. For every VARIABLE
# table in those units the property draws a value with StreamData, saves it,
# loads it and saves the loaded value again; the two encodings must be byte
# equal. A second property mutates the encoding (bit flips, truncation, random
# bytes) and requires load to answer a value or a refusal tuple without
# raising.
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

ELIXIR_STREAMDATA_ENV := \
	MIX_HOME=$(ELIXIR_STREAMDATA_BUILD)/mix \
	HEX_HOME=$(ELIXIR_STREAMDATA_BUILD)/hex \
	MIX_DEPS_PATH=$(ELIXIR_STREAMDATA_BUILD)/deps \
	MIX_BUILD_PATH=$(ELIXIR_STREAMDATA_BUILD)/_build

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
	@echo 'elixir streamdata: every variable table round-trips and every mutated encoding is a clean read or refusal'

# THE NEGATIVE CONTROL. The property script turns the plant on with
# SCHEMA_ELIXIR_STREAMDATA_PLANT=roundtrip: it corrupts the bytes between save
# and load, so the round-trip property must go red naming the plant. The
# control passes only when the target exits non-zero AND the plant's name is
# in the log; it fails by name when the property stayed green.
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
	@echo 'elixir streamdata negative control: a corrupted save reds the round-trip property naming the plant'

.PHONY: test-elixir-streamdata
test-elixir-streamdata: schema-elixir-streamdata-property-tests schema-elixir-streamdata-property-tests-negative-control
