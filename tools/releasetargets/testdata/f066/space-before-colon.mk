# F-066 CASE space-before-colon: `tables-go-release :` is the same rule to
# GNU Make as `tables-go-release:`. The old planner grepped the source for
# '^tables-[a-z0-9]+-release:' and dropped this target's certify job while the
# rule stayed intact; the make database still defines it, so the planner must
# select it.
#
# want: tables-c-release tables-cs-wire-fuzz tables-go-release
#       tables-wire-fuzz-release-c tables-wire-fuzz-release-go

all: ; @:

WIRE_FUZZ_RELEASE_LEGS := c go

define wfr_leg
tables-wire-fuzz-release-$(1):
	@echo wire fuzz $(1)
endef
$(foreach leg,$(WIRE_FUZZ_RELEASE_LEGS),$(eval $(call wfr_leg,$(leg))))

tables-wire-fuzz-release-negative-control:
	@echo not a leg

tables-c-release:
	@echo c

tables-go-release :
	@echo go

tables-cs-wire-fuzz:
	@echo cs

.PHONY: all tables-c-release tables-go-release tables-cs-wire-fuzz
