# F-066 CONTROL: the shapes the repository's own make files use today. Every
# release target is a plain `name:` rule, and the wire-fuzz legs are one `:=`
# list whose rules come from a foreach/eval template, as make/wire-fuzz-release.mk
# makes them. The planner selects every one of them.
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

tables-go-release:
	@echo go

tables-cs-wire-fuzz:
	@echo cs

.PHONY: all tables-c-release tables-go-release tables-cs-wire-fuzz
