# F-066 CASE split-assignment: the leg list is one `:=` and a `+=`
# continuation. GNU Make expands both, and the template defines a rule for
# every leg. The old planner read only the `:=` line with sed and silently lost
# the legs the `+=` added (17 of 18 jobs in the reviewer's control); the make
# database holds the whole expanded list, so the planner must select them all.
#
# want: tables-c-release tables-cs-wire-fuzz tables-go-release
#       tables-wire-fuzz-release-c tables-wire-fuzz-release-go

all: ; @:

WIRE_FUZZ_RELEASE_LEGS := c
WIRE_FUZZ_RELEASE_LEGS += go

define wfr_leg
tables-wire-fuzz-release-$(1):
	@echo wire fuzz $(1)
endef
$(foreach leg,$(WIRE_FUZZ_RELEASE_LEGS),$(eval $(call wfr_leg,$(leg))))

tables-wire-fuzz-release-negative-control:
	@echo not a leg

tables-c-release:
	@echo c

tables-go-release:
	@echo go

tables-cs-wire-fuzz:
	@echo cs

.PHONY: all tables-c-release tables-go-release tables-cs-wire-fuzz
