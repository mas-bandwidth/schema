# F-066 REFUSAL leg-without-rule: the leg list names `rust`, but no rule
# tables-wire-fuzz-release-rust is defined (only named as a prerequisite, which
# the make database prints under "# Not a target:"). A job for it would run a
# target make cannot build; the planner refuses by name instead of planning it.
#
# want: refuse tables-wire-fuzz-release-rust

all: ; @:

WIRE_FUZZ_RELEASE_LEGS := c go rust

define wfr_leg
tables-wire-fuzz-release-$(1):
	@echo wire fuzz $(1)
endef
$(foreach leg,c go,$(eval $(call wfr_leg,$(leg))))

wire-fuzz-release-everything: tables-wire-fuzz-release-rust

tables-c-release:
	@echo c

tables-go-release:
	@echo go

tables-cs-wire-fuzz:
	@echo cs

.PHONY: all tables-c-release tables-go-release tables-cs-wire-fuzz
