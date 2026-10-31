# make/squirrel-conformance.mk — the SQUIRREL leg's conformance negative controls
# (docs/PORT-SQUIRREL.md, THE CARD MAP; test/conformance/README.md, "Seven
# negative controls"). The Makefile's wildcard include pulls this in; it names
# no language.
#
# Each control is test/conformance/squirrel/negative-control: it overlays ONE
# emitted runtime file (internal/codegen/squirreltable/runtime/*.nut, rebuilt
# into the compiler under a Go -overlay), regenerates the corpus, and runs the
# SAME driver against the saboteur's tree via $SCHEMA_SQUIRREL_CONFORMANCE. One
# surface must go red and the named others stay green. No tracked file is
# written to: the saboteur's tree lives under build/.

.PHONY: conformance-negative-control-squirrel
conformance-negative-control-squirrel: build/conformance-harness build/conformance-squirrel toolchain-squirrel
	@sh test/conformance/squirrel/negative-control load load.nut \
		's/report.kindMismatch = report.kindMismatch + 1;/report.kindMismatch = report.kindMismatch;/g' \
		report wire

# THE WALK CONTROL, one per port with a text form (test/conformance/README.md):
# the generated text reader's bool arm reads `true` as false, so json-read goes
# red while json-write and wire stay green, which says the break is the
# READER's.
.PHONY: conformance-negative-control-squirrel-walk
conformance-negative-control-squirrel-walk: build/conformance-harness build/conformance-squirrel toolchain-squirrel
	@sh test/conformance/squirrel/negative-control walk json.nut \
		's/holder\[key\] = true;/holder[key] = false;/' \
		json-read json-write wire

# THE PITCH CHECK (§19.2), as conformance-negative-control-java-block breaks
# Java's: the block Open stops refusing a row stride the descriptor does not
# state, so forgery goes red while block and block-dump, which no valid image
# can move, stay green.
.PHONY: conformance-negative-control-squirrel-block
conformance-negative-control-squirrel-block: build/conformance-harness build/conformance-squirrel toolchain-squirrel
	@sh test/conformance/squirrel/negative-control block block.nut \
		's/if (stride != a.stride) return refuse(report, "bad_layout");//' \
		forgery block block-dump

# THE RETAINED-ID CAPACITY (§6.6): the save interns retained ids past the
# caller's list, so retain_ids_short keeps a record it must drop and the retain
# surface goes red; the plain wire and the report never touch retention.
.PHONY: conformance-negative-control-squirrel-retain
conformance-negative-control-squirrel-retain: build/conformance-harness build/conformance-squirrel toolchain-squirrel
	@sh test/conformance/squirrel/negative-control retain retain.nut \
		's/if (store.ids.len() >= store.idCapacity) return false;//' \
		retain wire report

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
SQUIRREL_WIRE_PIN ?= 97

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

# J3 (docs/PORTING.md) — THE RELEASE TIER. The Squirrel leg's release act: the
# whole conformance matrix pinned (a clean corpus, the wire count held), the
# tolerant wire's differential fuzzer over both arms, and every conformance
# negative control, each of which must go red on exactly its own surface.
# The release SCALES the PR tier's fuzzer: the builder arm (the variable roots,
# which the plain arm does not answer) also runs seeds 2 and 3 at ten times the
# PR count. The plain arm's three seeds at the release count are
# tables-wire-fuzz-release-squirrel (make/wire-fuzz-release.mk). Its control is
# tables-squirrel-release-negative-control (make/squirrel-fuzz.mk).
SQUIRREL_RELEASE_FUZZ_N ?= 20000
.PHONY: tables-squirrel-release
tables-squirrel-release:
	$(MAKE) conformance-squirrel-wire-pin
	$(MAKE) tables-squirrel-wire-fuzz
	for s in 2 3; do ./build/conformance-harness wire-fuzz --driver "$(SQ) test/conformance/squirrel/wirefuzz.nut builder" --builder --seed $$s --n $(SQUIRREL_RELEASE_FUZZ_N) --failed build/wire-fuzz/failed-squirrel-builder-$$s.bin || exit 1; done
	$(MAKE) conformance-negative-control-squirrel
	$(MAKE) conformance-negative-control-squirrel-walk
	$(MAKE) conformance-negative-control-squirrel-block
	$(MAKE) conformance-negative-control-squirrel-retain

# I4, PER-CASE ABSENT (docs/PORTING.md): the leg answers a case it cannot
# carry with `<case>.absent`, and the harness counts it. The control runs the
# driver with those answers written under another name and requires the wire
# surface to go red (test/conformance/squirrel/absent-negative-control).
.PHONY: conformance-negative-control-squirrel-absent
conformance-negative-control-squirrel-absent: build/conformance-harness build/conformance-squirrel toolchain-squirrel
	@SQ="$(SQ)" sh test/conformance/squirrel/absent-negative-control
