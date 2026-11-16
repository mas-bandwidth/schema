# make/moo-cook-write.mk — the moo leg's cook-write gate (docs/SPEC-TABLES.md
# §7.2; docs/rulings.json fixed-dead-bytes-are-zero): the cook writer writes an
# absent optional's payload and every dead slot of a counted array as zero
# bytes, in both byte orders, so every cook-write row of the manifest equals
# its fixture with none relaxed. test/conformance/moo/cook-write holds the
# check and says what each target requires.
#
# conformance-moo-cook-write                   the harness over the moo leg;
#                                              its cook-write line is pass N/N
#                                              and no cook-write row is relaxed
# conformance-moo-cook-write-negative-control  the cook writer's zeroing
#                                              removed through a private
#                                              compiler: the gate goes red
.PHONY: conformance-moo-cook-write conformance-moo-cook-write-negative-control
conformance-moo-cook-write: toolchain-moo build/conformance-harness build/conformance-moo
	sh test/conformance/moo/cook-write run

conformance-moo-cook-write-negative-control: toolchain-moo build/conformance-harness
	sh test/conformance/moo/cook-write control

test-moo: conformance-moo-cook-write conformance-moo-cook-write-negative-control
