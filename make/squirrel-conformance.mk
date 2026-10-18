# make/squirrel-conformance.mk — the SQUIRREL leg's conformance negative control
# (docs/PORT-SQUIRREL.md, THE CARD MAP; test/conformance/README.md, "Seven
# negative controls"). The Makefile's wildcard include pulls this in; it names
# no language.
#
# The control overlays the emitted load runtime (the generated copy of
# internal/codegen/squirreltable/runtime/load.nut, rebuilt by the tagged
# compiler under a Go -overlay) so the report's kind-mismatch counter stops
# incrementing, regenerates the corpus, and runs the SAME driver against the
# saboteur's tree via $SCHEMA_SQUIRREL_CONFORMANCE. The report surface must go
# red; the wire surface must stay green. No tracked file is written to: the
# saboteur's tree lives under build/.

.PHONY: conformance-negative-control-squirrel
conformance-negative-control-squirrel: build/conformance-harness build/conformance-squirrel toolchain-squirrel
	@set -eu; \
	out=build/squirrel-report-sabotage; \
	mkdir -p "$$out"; \
	source=internal/codegen/squirreltable/runtime/load.nut; \
	sed 's/report.kindMismatch = report.kindMismatch + 1;/report.kindMismatch = report.kindMismatch;/g' "$$source" > "$$out/load.nut"; \
	if cmp -s "$$source" "$$out/load.nut"; then \
		echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing" >&2; \
		exit 1; \
	fi; \
	root=$$(pwd); \
	printf '{"Replace":{"%s/%s":"%s/%s/load.nut"}}\n' "$$root" "$$source" "$$root" "$$out" > "$$out/overlay.json"; \
	"$${GO:-go}" build -overlay "$$root/$$out/overlay.json" -tags schema_leg_squirrel -o "$$out/schema" ./cmd/schema; \
	rm -rf "$$out/conformance-squirrel" && mkdir -p "$$out/conformance-squirrel"; \
	: > "$$out/conformance-squirrel/REFUSED.txt"; \
	grep '^unit ' testdata/conformance/tables/MANIFEST.txt | while read -r _ key paths; do \
		if ! "$$out/schema" generate --lang squirrel --out "$$out/conformance-squirrel/$$key" $$paths > "$$out/$$key.log" 2>&1; then \
			printf '%s %s\n' "$$key" "$$(tr '\n' ' ' < "$$out/$$key.log")" >> "$$out/conformance-squirrel/REFUSED.txt"; \
		fi; \
		rm -f "$$out/$$key.log"; \
	done; \
	printf 'squirrel test/conformance/squirrel/driver\n' > "$$out/drivers.txt"; \
	if SCHEMA_SQUIRREL_CONFORMANCE="$$root/$$out/conformance-squirrel" ./build/conformance-harness run --drivers "$$out/drivers.txt" --work "$$out/work" > "$$out/log" 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the sabotage left conformance green" >&2; \
		cat "$$out/log" >&2; \
		exit 1; \
	fi; \
	if ! grep -q 'squirrel / report:' "$$out/log"; then \
		echo "NEGATIVE CONTROL FAILED: the report surface did not go red" >&2; \
		cat "$$out/log" >&2; \
		exit 1; \
	fi; \
	if grep -q 'squirrel / wire:' "$$out/log"; then \
		echo "NEGATIVE CONTROL FAILED: the wire surface went red" >&2; \
		cat "$$out/log" >&2; \
		exit 1; \
	fi; \
	echo "Squirrel conformance negative control: the load saboteur is caught by report, not wire"
