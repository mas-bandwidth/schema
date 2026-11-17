# make/squirrel-cook-write.mk — THE SQUIRREL COOK WRITER's whole cook-write cell
# (docs/SPEC-TABLES.md §7.2, §7.6; docs/rulings.json fixed-dead-bytes-are-zero).
# The leg runs alone and the log lands in build/conformance-squirrel-cook-write/log.
# The leg's other surfaces belong to other gates, so the run's own exit status
# is not this target's verdict: test/conformance/squirrel/cook-write-check is.
# It passes only when the cook-write cell reads `pass N/N` with no absence
# suffix and the log carries no RELAXED line for `squirrel / cook-write`: an
# absent optional's payload and every dead slot of a counted array are zero.

.PHONY: conformance-squirrel-cook-write
conformance-squirrel-cook-write: build/conformance-harness build/conformance-squirrel toolchain-squirrel
	@rm -rf build/conformance-squirrel-cook-write
	@mkdir -p build/conformance-squirrel-cook-write
	@SQ="$(SQ)" ./build/conformance-harness run --only squirrel \
		--work build/conformance-squirrel-cook-write/work \
		> build/conformance-squirrel-cook-write/log 2>&1 || true
	@sh test/conformance/squirrel/cook-write-check build/conformance-squirrel-cook-write/log

# THE CONTROL: the cook writer is re-emitted under build/ with an absent
# optional's payload written again (its declared default lands in dead bytes),
# through the leg's own overlay control (test/conformance/squirrel/negative-control):
# cook-write must go red while wire stays green, and the target's own check
# must refuse the saboteur's log.
.PHONY: conformance-squirrel-cook-write-negative-control
conformance-squirrel-cook-write-negative-control: build/conformance-harness build/conformance-squirrel toolchain-squirrel
	@SQ="$(SQ)" sh test/conformance/squirrel/negative-control cook-write-zero cookwrite.nut \
		's/if (!present) return true;//' \
		cook-write wire
	@if sh test/conformance/squirrel/cook-write-check build/squirrel-negative-control-cook-write-zero/log > /dev/null 2>&1; then \
		echo "NEGATIVE CONTROL FAILED (cook-write-zero): conformance-squirrel-cook-write passed the saboteur's log" >&2; \
		exit 1; \
	fi
	@echo "Squirrel cook-write negative control: conformance-squirrel-cook-write refuses the planted absent-optional payload"

test-squirrel: conformance-squirrel-cook-write conformance-squirrel-cook-write-negative-control
