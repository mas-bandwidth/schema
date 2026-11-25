# make/wire-fuzz-rows.mk — the wire fuzz as registry rows
# (test/conformance/harness/wirefuzz_registry.go; docs/TEST-REGISTRY.md).
#
#   make wire-fuzz-deps-<leg>       build what the leg's wire-fuzz driver needs
#   make wire-fuzz-adapter-<leg> WFZ_WORK=DIR
#                                   answer the request lines on standard input
#
# schema-test's wire-fuzz harness (tools/schema-test/adapters.go) calls these
# for the families wire-fuzz-vector (every pinned vector, replayed exactly)
# and wire-fuzz (every arm at every seed). The driver, its prerequisites and
# its environment are the leg's release wire-fuzz gate's
# (make/wire-fuzz-release.mk), so the two run one leg the same way.
#
# A leg answers in every configuration it has, each a --config NAME=COMMAND:
# Python in the current and the floor interpreter, each in normal and -X dev
# mode, and Ruby in the current and the floor interpreter, with the commands
# make/python-wirefuzz.mk and make/ruby-wirefuzz.mk run; every other leg in
# its one driver. An instance passes only when every configuration does.

WIRE_FUZZ_REGISTRY_JOBS ?= 4

wfz_python_cmd = $(1) $(2) -W error -I -B test/conformance/python/wirefuzz.py build/conformance-python
WFZ_CONFIGS_python = \
	'--config=current-normal=$(call wfz_python_cmd,$(PYTHON),)' \
	'--config=current-dev=$(call wfz_python_cmd,$(PYTHON),-X dev)' \
	'--config=floor-normal=$(call wfz_python_cmd,$(PYTHON_FLOOR),)' \
	'--config=floor-dev=$(call wfz_python_cmd,$(PYTHON_FLOOR),-X dev)'
WFZ_CONFIGS_ruby = \
	'--config=current=$(RUBY) --disable-gems -w test/conformance/ruby/wirefuzz.rb build/conformance-ruby' \
	'--config=floor=$(RUBY_FLOOR) --disable-gems -w test/conformance/ruby/wirefuzz.rb build/conformance-ruby'

# $(1) the leg: its configurations, else its one driver
wfz_configs = $(if $(WFZ_CONFIGS_$(1)),$(WFZ_CONFIGS_$(1)),'--config=default=$(strip $(WFR_DRIVER_$(1)))')

# $(1) the leg. The deps target refuses a leg with no driver by name; the
# adapter target builds nothing (schema-test runs the deps once per process,
# so legs run side by side never build one thing at the same time).
define wfz_leg
.PHONY: wire-fuzz-deps-$(1) wire-fuzz-adapter-$(1)
wire-fuzz-deps-$(1): build/conformance-harness $$(WFR_DEPS_$(1))
	@test -n "$$(strip $$(WFR_DRIVER_$(1)))" || { echo "wire-fuzz: the $(1) leg names no wire-fuzz driver (make/wire-fuzz-release.mk)"; exit 1; }
wire-fuzz-adapter-$(1):
	@test -n "$$(WFZ_WORK)" || { echo "wire-fuzz-adapter-$(1): WFZ_WORK names the call's work folder"; exit 1; }
	@$$(WFR_ENV_$(1)) ./build/conformance-harness wire-fuzz-adapter --jobs $$(WIRE_FUZZ_REGISTRY_JOBS) \
		--work "$$(WFZ_WORK)" $$(call wfz_configs,$(1))
endef
# The legs are make/wire-fuzz-release.mk's, which the Makefile's sorted
# wildcard include reads before this file. An empty list would define no leg
# at all, so it is an error, never a quiet nothing.
ifeq ($(strip $(WIRE_FUZZ_RELEASE_LEGS)),)
$(error make/wire-fuzz-rows.mk: WIRE_FUZZ_RELEASE_LEGS is empty, so make/wire-fuzz-release.mk was not read first)
endif
$(foreach l,$(WIRE_FUZZ_RELEASE_LEGS),$(eval $(call wfz_leg,$(l))))
