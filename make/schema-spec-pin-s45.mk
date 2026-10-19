# make/schema-spec-pin-s45.mk — wildcard-discovered shim for the §4/§5 spec-pins check.
# The target lives in make/checks/schema-spec-pin-s45.mk; the Makefile includes
# make/*.mk, so this line keeps the target reachable until the Makefile includes
# the check fragment by path (PATHS-PROPOSED: Makefile).
include make/checks/schema-spec-pin-s45.mk
