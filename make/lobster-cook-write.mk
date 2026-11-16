# make/lobster-cook-write.mk — the LOBSTER leg's cook-write surface held whole
# (docs/SPEC-TABLES.md §7.2, §7.6; docs/rulings.json fixed-dead-bytes-are-zero):
# an absent optional's payload and a counted array's slots past its live count
# are zero in the cooked file, in both byte orders.
#
# conformance-lobster-cook-write  runs the harness over the leg, its log in
#                                 build/conformance-lobster-cook-write/log, and
#                                 passes only when cook-write reads pass N/N
#                                 with no absence suffix and no RELAXED line
# conformance-lobster-cook-write-negative-control
#                                 re-emits the cook writer with the dead-slot
#                                 zeroing removed and requires the check red
#                                 (test/conformance/lobster/cook-write)
.PHONY: conformance-lobster-cook-write conformance-lobster-cook-write-negative-control
conformance-lobster-cook-write: lobster-dist-present build/conformance-harness build/conformance-lobster
	sh test/conformance/lobster/cook-write check

conformance-lobster-cook-write-negative-control: lobster-dist-present build/conformance-harness
	sh test/conformance/lobster/cook-write negative-control

test-lobster: conformance-lobster-cook-write conformance-lobster-cook-write-negative-control
