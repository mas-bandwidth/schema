# make/lobster-fuzz.mk — the LOBSTER leg's wire-fuzz differential against the
# Go engine (schema-lobster-fuzz-differential; docs/PORT-LOBSTER.md, THE
# HARNESS, "The wire-fuzz pipe"; test/conformance/README.md, "The wire-fuzz
# driver"). Included by the Makefile's wildcard include, after make/lobster.mk.
#
# The harness (build/conformance-harness wire-fuzz) mutates every corpus wire
# and holds the oracle, internal/tablewire. The leg is
# test/conformance/lobster/wirefuzzhost: it speaks the harness's stream and
# runs test/conformance/lobster/wirefuzz.lobster once per generated unit,
# which loads each mutant through the Lobster tolerant read and saves what it
# decoded. Two arms cover every form-1 root the corpus names:
#
#   the ordinary run   the FIXED-class roots (a fixed root always loads and
#                      answers the measure -1)
#   --builder          the VARIABLE-class roots, a mutable file reader held to
#                      tablewire.Decode with no region preflight: Lobster's
#                      load fills a record, not a region a LoadMeasure sizes
#
# The message form and retention are not carried (answered 0). Every pinned
# vector of test/conformance/lobster/vectors replays in the builder arm.
#
# THE BUDGET IS A COUNT: the enumerated pass is a fixed function of the corpus
# (about 155,000 mutants over both arms, about a minute on the studio) and the
# random pass is LOBSTER_WIRE_FUZZ_N per arm. test-lobster runs the small
# count; tables-lobster-release, which certify.yml discovers and runs by name,
# runs the shared N.
#
# The leg releases every graph it loads (schema_rt.release), and a variable
# load unlinks the node records its root does not reach, so a cyclic graph
# leaks nothing; the VM's exit-time leak report fails the leg unless
# test/conformance/lobster/wirefuzz-known-leaks.txt (empty) names the unit,
# and a listed unit that stops leaking fails it too.

LOBSTER_WIRE_FUZZ_N    ?= 200
LOBSTER_WIRE_FUZZ_SEED ?= $(SEED)
LOBSTER_WIRE_FUZZ_LEAKS := test/conformance/lobster/wirefuzz-known-leaks.txt
LOBSTER_WIRE_FUZZ_HOST  = build/lobster-wirefuzzhost --known-leaks $(LOBSTER_WIRE_FUZZ_LEAKS)

# the generated units move with the emitters and both runtimes, not only with
# the manifest make/lobster.mk names
build/conformance-lobster: $(wildcard internal/codegen/lobster/*.go internal/codegen/lobstertable/*.go \
	internal/codegen/lobster/runtime/*.lobster internal/codegen/lobstertable/runtime/*.lobster)

build/lobster-wirefuzzhost: $(wildcard test/conformance/lobster/wirefuzzhost/*.go)
	@mkdir -p build
	go build -o $@ ./test/conformance/lobster/wirefuzzhost

# lobster-dist-present fetches the pinned toolchain (sha256 checked by
# dist-lobster) where none resolves: the negative-control rows run on a runner
# that installs no Lobster of its own.
.PHONY: lobster-dist-present
lobster-dist-present:
	@[ -x "$(LOBSTER)" ] || $(MAKE) --no-print-directory dist-lobster

# lobster_wire_fuzz_vectors replays every pinned vector of the builder arm
# over the units in $(1); each name is <unit>.<root>.<anything>.bin
define lobster_wire_fuzz_vectors
	@for v in test/conformance/lobster/vectors/*.bin; do \
		case "$$v" in *guard-false-pointer*) unit=tblg1 root=Guarded ;; *) echo "no root for $$v"; exit 1 ;; esac; \
		./build/conformance-harness wire-fuzz --builder --driver '$(LOBSTER_WIRE_FUZZ_HOST) --builder --units $(1)' \
			--replay $$v --unit $$unit --root $$root || exit 1; \
	done
endef

.PHONY: tables-lobster-wire-fuzz tables-lobster-wire-fuzz-negative-control tables-lobster-release
tables-lobster-wire-fuzz: lobster-dist-present build/conformance-harness build/conformance-lobster build/lobster-wirefuzzhost
	@mkdir -p build/wire-fuzz
	./build/conformance-harness wire-fuzz --driver '$(LOBSTER_WIRE_FUZZ_HOST)' \
		--seed $(LOBSTER_WIRE_FUZZ_SEED) --n $(LOBSTER_WIRE_FUZZ_N) --failed build/wire-fuzz/failed-lobster.bin
	./build/conformance-harness wire-fuzz --builder --driver '$(LOBSTER_WIRE_FUZZ_HOST) --builder' \
		--seed $(LOBSTER_WIRE_FUZZ_SEED) --n $(LOBSTER_WIRE_FUZZ_N) --failed build/wire-fuzz/failed-lobster-builder.bin
	$(call lobster_wire_fuzz_vectors,build/conformance-lobster)

test-lobster: tables-lobster-wire-fuzz

tables-lobster-release:
	$(MAKE) tables-lobster-wire-fuzz LOBSTER_WIRE_FUZZ_N=$(N)
	$(MAKE) tables-lobster-wire-fuzz-negative-control

# THE NEGATIVE CONTROL, two plants in a copy of the generated units:
#   unknown  the load stops counting one unknown field (load.lobster), and the
#            ordinary run must go red on the report
#   guard    the descriptors lose their `when` guards, so the save writes a
#            pointer under a false guard; the pinned guard-false-pointer vector
#            must go red on the decoded value
# Unplanted, the same units are green.
LOBSTER_WIRE_FUZZ_PLANT := build/lobster-wire-fuzz-plant
tables-lobster-wire-fuzz-negative-control: lobster-dist-present build/conformance-harness build/conformance-lobster build/lobster-wirefuzzhost
	@rm -rf $(LOBSTER_WIRE_FUZZ_PLANT) && mkdir -p $(LOBSTER_WIRE_FUZZ_PLANT)
	@cp -R build/conformance-lobster $(LOBSTER_WIRE_FUZZ_PLANT)/unknown
	@sed -i.orig 's/^        rep\.unknown++$$/        rep.unknown += 0/' $(LOBSTER_WIRE_FUZZ_PLANT)/unknown/*/load.lobster
	@if cmp -s build/conformance-lobster/tabledemo/load.lobster $(LOBSTER_WIRE_FUZZ_PLANT)/unknown/tabledemo/load.lobster; then \
		echo 'NEGATIVE CONTROL FAILED: the planted unknown-count bug did not apply'; exit 1; fi
	@if ./build/conformance-harness wire-fuzz --driver '$(LOBSTER_WIRE_FUZZ_HOST) --units $(LOBSTER_WIRE_FUZZ_PLANT)/unknown' \
			--seed 1 --n 0 --failed $(LOBSTER_WIRE_FUZZ_PLANT)/unknown.bin > $(LOBSTER_WIRE_FUZZ_PLANT)/unknown.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a load that drops the unknown count left the wire fuzz green'; \
		cat $(LOBSTER_WIRE_FUZZ_PLANT)/unknown.log; exit 1; fi
	@grep -q 'the report differs' $(LOBSTER_WIRE_FUZZ_PLANT)/unknown.log || \
		{ echo 'NEGATIVE CONTROL FAILED: the fuzz went red, but not on the report'; cat $(LOBSTER_WIRE_FUZZ_PLANT)/unknown.log; exit 1; }
	@cp -R build/conformance-lobster $(LOBSTER_WIRE_FUZZ_PLANT)/guard
	@sed -i.orig -E 's/, when: \[[^]]*\]//' $(LOBSTER_WIRE_FUZZ_PLANT)/guard/tblg1/*Table.lobster
	@if cmp -s build/conformance-lobster/tblg1/G1Table.lobster $(LOBSTER_WIRE_FUZZ_PLANT)/guard/tblg1/G1Table.lobster; then \
		echo 'NEGATIVE CONTROL FAILED: the planted guard bug did not apply'; exit 1; fi
	@if ./build/conformance-harness wire-fuzz --builder --driver '$(LOBSTER_WIRE_FUZZ_HOST) --builder --units $(LOBSTER_WIRE_FUZZ_PLANT)/guard' \
			--replay test/conformance/lobster/vectors/guard-false-pointer.bin --unit tblg1 --root Guarded \
			> $(LOBSTER_WIRE_FUZZ_PLANT)/guard.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a save that writes a pointer under a false guard left the pinned vector green'; \
		cat $(LOBSTER_WIRE_FUZZ_PLANT)/guard.log; exit 1; fi
	@grep -q 'the decoded value differs' $(LOBSTER_WIRE_FUZZ_PLANT)/guard.log || \
		{ echo 'NEGATIVE CONTROL FAILED: the vector went red, but not on the value'; cat $(LOBSTER_WIRE_FUZZ_PLANT)/guard.log; exit 1; }
	$(call lobster_wire_fuzz_vectors,build/conformance-lobster)
	@echo 'lobster wire-fuzz negative control: a dropped unknown count reds the fuzz and an unguarded save reds the pinned vector; unplanted both are green'
