# make/luau-conformance.mk — the LUAU leg's conformance controls
# (test/conformance/README.md; docs/PORT-LUAU.md). Included by the Makefile's
# wildcard include. The reference leg is cpp; this is a discovered, non-reference
# leg, so its absences are ordinary.

# THE LUAU LEG's NEGATIVE CONTROL, on the same rule as the Go one: a harness
# that has never gone red on a leg is watching that leg. The emitted LOAD
# runtime is copied with one report counter patched — a read report is forced
# to differ from the corpus expectation — and the driver is pointed at that copy
# via LUAU_CONFORMANCE_GENERATED (the same hook the default build uses). The
# matrix must go red on `luau / report` and stay green on `luau / wire`, whose
# saved bytes are unaffected by the counter.
.PHONY: conformance-negative-control-luau
conformance-negative-control-luau: build/conformance-harness build/conformance-luau
	@set -eu; \
	if [ ! -x "$(LUAU)" ]; then $(MAKE) dist-luau; fi; \
	out=build/luau-table-control; \
	rm -rf $$out && mkdir -p $$out; \
	cp -r build/conformance-luau $$out/generated; \
	sed -i 's/report.unknown = report.unknown + 1/report.unknown = report.unknown + 1 + 1/g' $$out/generated/tblv1/load.luau; \
	printf '#!/bin/sh\nexec env LUAU_CONFORMANCE_GENERATED=%s/generated LUAU=$${LUAU:-$(LUAU)} $(CURDIR)/test/conformance/luau/driver "$$@"\n' "$$out" > $$out/driver; \
	chmod +x $$out/driver; \
	printf 'luau %s/driver\n' "$$out" > $$out/drivers.txt; \
	if ./build/conformance-harness run --drivers $$out/drivers.txt --work $$out/work > $$out/log 2>&1; then \
		echo "negative control failed: the luau load sabotage left conformance green" >&2; \
		exit 1; \
	fi; \
	grep -q 'luau / report' $$out/log; \
	grep -q '^wire          pass' $$out/log; \
	echo "Luau table negative control: the load sabotage is caught by report and leaves wire green"
