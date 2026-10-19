# make/checks/lua-conformance.mk — the LUA leg's conformance controls
# (test/conformance/README.md; docs/PORT-LUA.md). Included by the Makefile's
# wildcard include. The reference leg is cpp; this is a discovered, non-reference
# leg, so its absences are ordinary.

# THE LUA LEG's NEGATIVE CONTROL, on the same rule as the Go one: a harness that
# has never gone red on a leg is watching that leg. The emitted LOAD runtime of
# every generated unit is copied with one counter patched — a report is forced to
# differ from the corpus expectation — and the driver is pointed at that copy via
# LUA_CONFORMANCE_GENERATED (the same hook the default build uses). The matrix
# must go red on `lua / report` and stay green on `lua / wire` (whose answers are
# absence, unaffected by the load sabotage).
.PHONY: conformance-negative-control-lua
conformance-negative-control-lua: build/conformance-harness build/conformance-lua
	@set -eu; \
	out=build/lua-table-control; \
	rm -rf $$out && mkdir -p $$out; \
	cp -r build/conformance-lua $$out/generated; \
	sed -i 's/report.unknown = report.unknown + 1/report.unknown = report.unknown + 1 + 1/g' $$out/generated/tblv2/load.lua; \
	printf '#!/bin/sh\nexec env LUA_CONFORMANCE_GENERATED=%s/generated LUA=$${LUA:-dist/lua-5.4.8/src/lua} $$(dirname $$0)/../../test/conformance/lua/driver "$$@"\n' "$$out" > $$out/driver; \
	chmod +x $$out/driver; \
	printf 'lua %s/driver\n' "$$out" > $$out/drivers.txt; \
	if ./build/conformance-harness run --drivers $$out/drivers.txt --work $$out/work > $$out/log 2>&1; then \
		echo "negative control failed: the lua load sabotage left conformance green" >&2; \
		exit 1; \
	fi; \
	grep -q 'lua / report' $$out/log; \
	grep -q '^wire          pass' $$out/log; \
	echo "Lua table negative control: the load sabotage is caught by report and leaves wire green"
