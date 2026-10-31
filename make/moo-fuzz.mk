# make/moo-fuzz.mk — the MOO leg's packet-wire fuzz differential
# (docs/PORT-MOO.md, THE LAYERS: layer 6). Included by the Makefile's wildcard
# include, after make/moo.mk.
#
# The differential is internal/codegen/moo/fuzz_test.go: random instances of
# every packet type and union of every packet corpus the leg generates, drawn
# inside their declared contracts, written by the generated moo codecs and by
# the Go target's (serialize.go, the sibling checkout) and required to be
# byte-identical; the Go images and mutants of them read by both readers, which
# must agree on accept/refuse, on the refusal reason the runtimes latch, and on
# the bytes each writes back. `make test-moo` runs it too (it is a go test of the
# package); these targets name it, with its seed and size, and its two reversed
# witnesses (a planted writer bug and a planted reader bug, each in a scratch
# copy of the generated moo, each required to turn the differential red).
#
# The budget is an instance count, never a clock: a seed and a count replay a
# round exactly. The nightly rounds are make/schema-moo-nightly-fuzz.mk.

MOO_FUZZ_SEED ?= 20261028
MOO_FUZZ_N    ?= 400

.PHONY: moo-wire-fuzz
moo-wire-fuzz: toolchain-moo
	SCHEMA_MOO_FUZZ_SEED=$(MOO_FUZZ_SEED) SCHEMA_MOO_FUZZ_N=$(MOO_FUZZ_N) SCHEMA_MOO_FUZZ_ROUNDS=1 \
		sh test/slowgate/proof moo-wire-fuzz \
		'TestMooWireFuzzAgreesWithTheGoTarget TestMooWireFuzzWriteNegativeControl TestMooWireFuzzReadNegativeControl' \
		-count=1 -timeout 600s ./internal/codegen/moo/ \
		-run '^TestMooWireFuzz(AgreesWithTheGoTarget|WriteNegativeControl|ReadNegativeControl)$$' -v

# THE TABLE WIRE-FUZZ LEG (test/conformance/README.md, "The wire-fuzz driver";
# docs/SPEC-TABLES.md §4.2; docs/PORTING.md I15). The harness owns the mutants
# and the oracle (internal/tablewire); the leg is ONE process on a pipe,
# test/conformance/moo/wirefuzz.moo under the native host tools/moofuzzhost,
# started by test/conformance/moo/wirefuzz-driver over the units
# build/conformance-moo generated. The pinned moo CLI has no io library, so the
# host is built here from the moo PIN's own source (make/moo.mk MOO_REPO and
# MOO_PIN, the same checkout dist-moo builds), its VM and compiler linked as
# the CLI links them, and nothing but the pipe added.
#
# THE ROOTS THE LEG ANSWERS 0 FOR are held to a pinned list BY NAME, as the
# Luau leg holds its own (make/luau-fuzz.mk): the driver notes each one into
# the file MOOFUZZ_NOTE names (made empty before the run), and the recipe fails on any difference from
# test/conformance/moo/wire-fuzz-absent.txt and on any count other than
# MOO_WIRE_FUZZ_ABSENT_COUNT, naming each root that moved.
#
# THE PINNED MUTANTS (test/conformance/moo/wire-fuzz-pinned.txt) are the
# divergences this leg's runs found, each replayed alone through the same pipe
# (harness --replay) after the run: a fix that regresses goes red on its own
# row whatever the seed reaches.

MOOFUZZHOST         := build/moofuzzhost/moofuzzhost
MOOFUZZHOST_SRC     := build/moofuzzhost/src
MOOFUZZHOST_CMAKE   := build/moofuzzhost/cmake
MOO_WIRE_FUZZ_SEED  ?= 1
MOO_WIRE_FUZZ_N     ?= 2000
MOO_WIRE_FUZZ_ARGS  ?=
MOO_WIRE_FUZZ_ABSENT_COUNT := 2
MOO_WIRE_FUZZ_ABSENT_PIN   := test/conformance/moo/wire-fuzz-absent.txt
MOO_WIRE_FUZZ_RETAIN_ABSENT_COUNT := 0
MOO_WIRE_FUZZ_RETAIN_ABSENT_PIN   := test/conformance/moo/wire-fuzz-absent-retain.txt
MOO_WIRE_FUZZ_PINNED       := test/conformance/moo/wire-fuzz-pinned.txt
MOO_WIRE_FUZZ_LOG          := build/wire-fuzz/moo.log
MOO_WIRE_FUZZ_ABSENT       := build/wire-fuzz/moo.absent

$(MOOFUZZHOST): tools/moofuzzhost/main.cpp make/moo.mk
	@rm -rf $(MOOFUZZHOST_SRC) $(MOOFUZZHOST_CMAKE) && mkdir -p $(MOOFUZZHOST_SRC)
	@cd $(MOOFUZZHOST_SRC) && git init -q && git fetch -q --depth 1 $(MOO_REPO) $(MOO_PIN) && git checkout -q FETCH_HEAD
	@got=$$(git -C $(MOOFUZZHOST_SRC) rev-parse HEAD); if [ "$$got" != "$(MOO_PIN)" ]; then \
		echo "moofuzzhost REFUSES: the checkout is $$got, not the pin $(MOO_PIN)"; exit 1; fi
	@cmake -S $(MOOFUZZHOST_SRC) -B $(MOOFUZZHOST_CMAKE) -DCMAKE_BUILD_TYPE=Release >/dev/null
	@cmake --build $(MOOFUZZHOST_CMAKE) --target Luau.Compiler Luau.VM Luau.Inliner Luau.Bytecode Luau.Ast Moo.Fixed -j $(MOO_BUILD_JOBS) >/dev/null
	@$(CXX) -std=c++17 -O2 -DNDEBUG -Wall -Wextra -Wshadow -Werror \
		-isystem $(MOOFUZZHOST_SRC)/VM/include -isystem $(MOOFUZZHOST_SRC)/Compiler/include \
		-isystem $(MOOFUZZHOST_SRC)/Common/include -isystem $(MOOFUZZHOST_SRC)/Ast/include \
		-c tools/moofuzzhost/main.cpp -o build/moofuzzhost/main.o
	@$(CXX) -o $@ build/moofuzzhost/main.o $(foreach l,Luau.Compiler Luau.Inliner Luau.Bytecode Luau.Ast Luau.VM Moo.Fixed Luau.Common,$(MOOFUZZHOST_CMAKE)/lib$(l).a) -lm -lpthread
	@echo "moofuzzhost: built against moo $(MOO_PIN)"

# one run of the pipe: $(1) the extra harness flags, $(2) the log, $(3) the absent
# list the driver notes, $(4) the pinned list, $(5) its count
define moo-wire-fuzz-run
	@mkdir -p build/wire-fuzz
	@rm -f $(3) && touch $(3)
	@MOOFUZZ_NOTE=$(CURDIR)/$(3) \
		./build/conformance-harness wire-fuzz --driver test/conformance/moo/wirefuzz-driver \
		--seed $(MOO_WIRE_FUZZ_SEED) --n $(MOO_WIRE_FUZZ_N) $(1) --failed build/wire-fuzz/failed-moo.bin \
		> $(2) 2>&1; \
		status=$$?; cat $(2); \
		sort -u $(3) > $(3).sorted; \
		grep -v '^#' $(4) | sort > $(3).pinned; \
		n=$$(wc -l < $(3).sorted | tr -d ' '); \
		if ! cmp -s $(3).sorted $(3).pinned || [ "$$n" -ne $(5) ]; then \
			echo "tables-moo-wire-fuzz: $$n roots absent, $(5) pinned ($(4))"; \
			comm -23 $(3).sorted $(3).pinned | sed 's/^/  the leg cannot open: /'; \
			comm -13 $(3).sorted $(3).pinned | sed 's/^/  pinned absent but open now (unpin it): /'; \
			exit 1; fi; \
		echo "tables-moo-wire-fuzz: $$n roots absent, as pinned"; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@grep -qE '(^|[^0-9])0 divergences' $(2)
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' $(2)
	@grep -qE '[1-9][0-9]* mutants' $(2)
endef

.PHONY: tables-moo-wire-fuzz tables-moo-wire-fuzz-plain tables-moo-wire-fuzz-retain tables-moo-wire-fuzz-pinned tables-moo-wire-fuzz-negative-control
tables-moo-wire-fuzz: tables-moo-wire-fuzz-plain tables-moo-wire-fuzz-retain tables-moo-wire-fuzz-pinned

tables-moo-wire-fuzz-plain: toolchain-moo build/conformance-harness build/conformance-moo $(MOOFUZZHOST)
	$(call moo-wire-fuzz-run,$(MOO_WIRE_FUZZ_ARGS),$(MOO_WIRE_FUZZ_LOG),$(MOO_WIRE_FUZZ_ABSENT),$(MOO_WIRE_FUZZ_ABSENT_PIN),$(MOO_WIRE_FUZZ_ABSENT_COUNT))

# THE RETENTION ARM (--retain, docs/SPEC-TABLES.md §6.6): the same mutants
# through LoadRetain and SaveRetain. Its roster is the variable-class file
# roots alone, so its absent list is its own.
tables-moo-wire-fuzz-retain: toolchain-moo build/conformance-harness build/conformance-moo $(MOOFUZZHOST)
	$(call moo-wire-fuzz-run,--retain $(MOO_WIRE_FUZZ_ARGS),build/wire-fuzz/moo-retain.log,build/wire-fuzz/moo-retain.absent,$(MOO_WIRE_FUZZ_RETAIN_ABSENT_PIN),$(MOO_WIRE_FUZZ_RETAIN_ABSENT_COUNT))

# each pinned mutant replayed alone: <file> <unit> <root> [message|retain]
tables-moo-wire-fuzz-pinned: toolchain-moo build/conformance-harness build/conformance-moo $(MOOFUZZHOST)
	@n=0; while read -r file unit root mode; do \
		case "$$file" in ""|\#*) continue ;; esac; \
		flag=""; [ "$$mode" = message ] && flag=--message; [ "$$mode" = retain ] && flag=--retain; \
		if ! ./build/conformance-harness wire-fuzz --driver test/conformance/moo/wirefuzz-driver \
			--replay "$$file" --unit "$$unit" --root "$$root" $$flag --failed build/wire-fuzz/failed-moo-pinned.bin \
			> build/wire-fuzz/moo-pinned.log 2>&1; then \
			cat build/wire-fuzz/moo-pinned.log; echo "tables-moo-wire-fuzz-pinned: $$file diverges again"; exit 1; fi; \
		n=$$((n + 1)); \
	done < $(MOO_WIRE_FUZZ_PINNED); \
	echo "tables-moo-wire-fuzz-pinned: $$n pinned mutants agree with the oracle"

# THE PIPE's CONTROL: a scratch copy of the units with one reader bug planted
# (a string frame past its body not counted malformed), and the harness must
# stop on exactly that counter (test/conformance/moo/wire-fuzz-negative-control).
tables-moo-wire-fuzz-negative-control: toolchain-moo build/conformance-harness build/conformance-moo $(MOOFUZZHOST)
	sh test/conformance/moo/wire-fuzz-negative-control

test-moo: tables-moo-wire-fuzz tables-moo-wire-fuzz-negative-control
