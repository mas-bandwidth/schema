# F-066 REFUSAL missing-also: the workflow names tables-cs-wire-fuzz as a
# campaign it always runs, and no rule defines it. The old planner added the
# name unchecked and the job failed at run time, or, renamed, ran nothing by
# that name; the planner refuses by name before the matrix exists.
#
# want: refuse tables-cs-wire-fuzz

all: ; @:

WIRE_FUZZ_RELEASE_LEGS := c go

define wfr_leg
tables-wire-fuzz-release-$(1):
	@echo wire fuzz $(1)
endef
$(foreach leg,$(WIRE_FUZZ_RELEASE_LEGS),$(eval $(call wfr_leg,$(leg))))

tables-c-release:
	@echo c

tables-go-release:
	@echo go

.PHONY: all tables-c-release tables-go-release
