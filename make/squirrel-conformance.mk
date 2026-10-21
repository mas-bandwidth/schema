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

# THE EMBEDDED RUNTIME IS A BUILD INPUT. squirreltable.go carries
# `//go:embed runtime/*.nut`, but the Makefile's GO_SOURCES names only *.go, so
# without this prerequisite a runtime change leaves build/schema-squirrel — and
# the corpus it generates — holding the OLD emitter (docs/PORT-SQUIRREL.md, THE
# HARNESS; docs/SPEC-TABLES.md §3). The wire surface then compares saves from a
# runtime the source no longer names. Naming the files here rebuilds the tagged
# compiler, and the corpus, whenever a runtime moves.
build/schema-squirrel: $(wildcard internal/codegen/squirreltable/runtime/*.nut)

# THE GENERATED CORPUS HAS THE SAME INPUT. The corpus copies the embedded
# runtime into every unit it generates, so a runtime move that leaves the
# compiler binary untouched still makes build/conformance-squirrel stale: the
# driver then saves with the previous runtime and every case whose fields moved
# comes out short (docs/SPEC-TABLES.md §3; docs/PORT-SQUIRREL.md, THE HARNESS).
# Naming the files on the corpus as well keeps the two in lockstep even when the
# compiler target is already up to date.
build/conformance-squirrel: $(wildcard internal/codegen/squirreltable/runtime/*.nut)

# THE WIRE PIN. A clean build every time (a stale corpus is the failure this
# gate exists to catch), then the Squirrel leg alone. The wire cell must be a
# pass with no `squirrel / wire:` failure line, and its passing count may never
# fall below SQUIRREL_WIRE_PIN — the count at the head this gate landed on.
# Setting it lower is how a regression becomes a new baseline, so it is raised
# only when a card adds wire cases (docs/SPEC-TABLES.md §3).
SQUIRREL_WIRE_PIN ?= 73

.PHONY: conformance-squirrel-wire-pin
conformance-squirrel-wire-pin: build/conformance-harness toolchain-squirrel
	@set -eu; \
	rm -rf build/conformance-squirrel build/schema-squirrel; \
	$(MAKE) build/conformance-harness build/conformance-squirrel; \
	SQ="$(SQ)" ./build/conformance-harness run --only squirrel > build/squirrel-wire-pin.log 2>&1 || true; \
	cat build/squirrel-wire-pin.log; \
	if grep -q 'squirrel / wire:' build/squirrel-wire-pin.log; then \
		echo "SQUIRREL WIRE PIN FAILED: the wire surface has a failure above" >&2; \
		exit 1; \
	fi; \
	cell=$$(grep '^wire ' build/squirrel-wire-pin.log | head -1); \
	if ! printf '%s\n' "$$cell" | grep -q 'pass '; then \
		echo "SQUIRREL WIRE PIN FAILED: the wire cell does not read a pass ($$cell)" >&2; \
		exit 1; \
	fi; \
	pass=$$(printf '%s\n' "$$cell" | sed -E 's/.*pass ([0-9]+)\/[0-9]+.*/\1/'); \
	if [ "$$pass" -lt "$(SQUIRREL_WIRE_PIN)" ]; then \
		echo "SQUIRREL WIRE PIN FAILED: $$pass passing, the pin is $(SQUIRREL_WIRE_PIN)" >&2; \
		exit 1; \
	fi; \
	echo "Squirrel wire pin: $$cell (pin $(SQUIRREL_WIRE_PIN))"
