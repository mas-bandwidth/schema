# make/luau-fuzz.mk — the LUAU leg's wire-fuzz gate (docs/PORT-LUAU.md, THE
# CARD MAP: schema-luau-fuzz-differential; test/conformance/README.md, "The
# wire-fuzz driver"; docs/SPEC-TABLES.md §4.2). Included by the Makefile's
# wildcard include.
#
# The pinned Luau has no io library, so the harness's little-endian pipe is
# spoken by test/conformance/luau/wirefuzz.luau under the native host
# tools/luaufuzzhost (its __luaufuzz.read and __luaufuzz.write are stdin and
# stdout), started by the executable driver test/conformance/luau/
# wirefuzz-driver. The in-package differential and its negative control run
# beside it, over the same corpus and the same pinned interpreter.
#
# THE ROOTS THE LEG CANNOT OPEN are held to a pinned list BY NAME, as the
# Elixir leg holds its own (make/elixir.mk, tables-elixir-wire-fuzz-all): the
# driver notes each roster entry it answers 0 for into the file LUAUFUZZ_NOTE
# names (__luaufuzz.note), and the recipe fails on any difference from
# test/conformance/luau/wire-fuzz-absent.txt and on any count other than
# LUAU_WIRE_FUZZ_ABSENT_COUNT, naming each root that moved. The absent check
# runs before the harness status is read, so a red run still names them.
#
# WHERE IT RUNS: the pipe run is the leg's `make test` half (test-luau below,
# which certify.yml runs nightly), as every port's tables-<lang>-wire-fuzz is;
# its enumerated pass is about 155 000 mutants, past the CI-full split's
# per-job cap on a hosted runner. On every merge, ci-full's unit-codegen row
# runs the in-package differential under SCHEMA_SLOW=1
# (TestLuauWireFuzzAgreesWithTheGoEngine and its negative control), and the
# negative-controls row of the luau group runs
# tables-luau-wire-fuzz-negative-control, which drives this same pipe.

LUAU_FUZZ_SEED ?= 1
LUAU_FUZZ_N    ?= 2000

LUAU_WIRE_FUZZ_ABSENT_COUNT := 9
LUAU_WIRE_FUZZ_ABSENT_PIN   := test/conformance/luau/wire-fuzz-absent.txt
LUAU_WIRE_FUZZ_LOG          := build/wire-fuzz/luau.log
LUAU_WIRE_FUZZ_ABSENT       := build/wire-fuzz/luau.absent

# build/luaufuzzhost/luaufuzzhost is built by make/schema-luau-nightly-fuzz.mk;
# its path is spelled out because that file is included after this one.
.PHONY: tables-luau-wire-fuzz tables-luau-wire-fuzz-negative-control
tables-luau-wire-fuzz: build/conformance-harness build/conformance-luau build/luau-driver/units.luau build/luaufuzzhost/luaufuzzhost
	@mkdir -p build/wire-fuzz
	@rm -f $(LUAU_WIRE_FUZZ_ABSENT)
	@LUAUFUZZ_NOTE=$(CURDIR)/$(LUAU_WIRE_FUZZ_ABSENT) \
		./build/conformance-harness wire-fuzz --driver test/conformance/luau/wirefuzz-driver \
		--seed $(LUAU_FUZZ_SEED) --n $(LUAU_FUZZ_N) --failed build/wire-fuzz/failed-luau.bin \
		> $(LUAU_WIRE_FUZZ_LOG) 2>&1; \
		status=$$?; cat $(LUAU_WIRE_FUZZ_LOG); \
		if [ ! -f $(LUAU_WIRE_FUZZ_ABSENT) ]; then \
			echo "tables-luau-wire-fuzz: the driver noted no absent-root list"; exit 1; fi; \
		sort $(LUAU_WIRE_FUZZ_ABSENT) > $(LUAU_WIRE_FUZZ_ABSENT).sorted; \
		grep -v '^#' $(LUAU_WIRE_FUZZ_ABSENT_PIN) | sort > $(LUAU_WIRE_FUZZ_ABSENT).pinned; \
		n=$$(wc -l < $(LUAU_WIRE_FUZZ_ABSENT).sorted | tr -d ' '); \
		if ! cmp -s $(LUAU_WIRE_FUZZ_ABSENT).sorted $(LUAU_WIRE_FUZZ_ABSENT).pinned || \
			[ "$$n" -ne $(LUAU_WIRE_FUZZ_ABSENT_COUNT) ]; then \
			echo "tables-luau-wire-fuzz: $$n roots absent, $(LUAU_WIRE_FUZZ_ABSENT_COUNT) pinned ($(LUAU_WIRE_FUZZ_ABSENT_PIN))"; \
			comm -23 $(LUAU_WIRE_FUZZ_ABSENT).sorted $(LUAU_WIRE_FUZZ_ABSENT).pinned | sed 's/^/  the leg cannot open: /'; \
			comm -13 $(LUAU_WIRE_FUZZ_ABSENT).sorted $(LUAU_WIRE_FUZZ_ABSENT).pinned | sed 's/^/  pinned absent but open now (unpin it): /'; \
			exit 1; fi; \
		echo "tables-luau-wire-fuzz: $$n roots absent, as pinned"; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@grep -qE '(^|[^0-9])0 divergences' $(LUAU_WIRE_FUZZ_LOG)
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' $(LUAU_WIRE_FUZZ_LOG)
	@grep -qE '[1-9][0-9]* mutants' $(LUAU_WIRE_FUZZ_LOG)
	sh test/slowgate/proof tables-luau-wire-fuzz \
		'TestLuauWireFuzzAgreesWithTheGoEngine TestLuauWireFuzzNegativeControl' \
		-count=1 -timeout 600s ./internal/codegen/luautable/ \
		-run '^TestLuauWireFuzz(AgreesWithTheGoEngine|NegativeControl)$$' -v

# THE PIPE's CONTROL: a scratch copy of the units with one reader bug planted
# (a string frame past its body not counted malformed), and the harness must
# stop on exactly that counter (test/conformance/luau/wire-fuzz-negative-control).
tables-luau-wire-fuzz-negative-control: build/conformance-harness build/conformance-luau build/luau-driver/units.luau build/luaufuzzhost/luaufuzzhost
	sh test/conformance/luau/wire-fuzz-negative-control

test-luau: tables-luau-wire-fuzz tables-luau-wire-fuzz-negative-control
