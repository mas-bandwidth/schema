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
# decoded. Two arms cover every form-1 root the corpus names, and a third the retention form:
#
#   the ordinary run   the FIXED-class roots (a fixed root always loads and
#                      answers the measure -1)
#   --builder          the VARIABLE-class roots, a mutable file reader held to
#                      tablewire.Decode with no region preflight: Lobster's
#                      load fills a record, not a region a LoadMeasure sizes
#
# the third is a flag of the ordinary run, with the
# message roots riding the ordinary run itself:
#
#   --retain           the variable roots through load_retain and save_retain
#                      with both capacities large (§6.6), the retention report
#                      held to the oracle beside the six counters
#   message roots      the harness's ordinary run already mutates the manifest's
#                      message batches and holds the leg to the oracle: the
#                      FIXED message roots (§3.3) through the tolerant batch
#                      read. A variable message root is not carried (the
#                      port's message form is fixed roots only, as Go's is).
#                      --message only REPLAYS a mutant as a batch, which the
#                      pinned message vectors below use
#
# Every pinned vector of test/conformance/lobster/vectors replays in the
# builder arm; the retention and message arms replay the corpus's own pinned
# vectors (testdata/wire/tables/fuzz-vectors).
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
# lobster_wire_fuzz_arm_vectors replays the corpus's pinned vectors the retain
# and message arms are answerable for, over the units in $(1)
define lobster_wire_fuzz_arm_vectors
	./build/conformance-harness wire-fuzz --retain --driver '$(LOBSTER_WIRE_FUZZ_HOST) --units $(1)' \
		--replay testdata/wire/tables/fuzz-vectors/map_retained_replaced.bin --unit mapdemo --root Text
	./build/conformance-harness wire-fuzz --message --driver '$(LOBSTER_WIRE_FUZZ_HOST) --units $(1)' \
		--replay testdata/wire/tables/fuzz-vectors/message_ill_formed_text.bin --unit backenddemo --root StorePurchase
	./build/conformance-harness wire-fuzz --message --driver '$(LOBSTER_WIRE_FUZZ_HOST) --units $(1)' \
		--replay testdata/wire/tables/fuzz-vectors/message_bytes_truncated.bin --unit backenddemo --root LoginRequest
endef

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
	./build/conformance-harness wire-fuzz --retain --driver '$(LOBSTER_WIRE_FUZZ_HOST)' \
		--seed $(LOBSTER_WIRE_FUZZ_SEED) --n $(LOBSTER_WIRE_FUZZ_N) --failed build/wire-fuzz/failed-lobster-retain.bin
	$(call lobster_wire_fuzz_arm_vectors,build/conformance-lobster)

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
#   retain   the retaining map read stops forgetting the entry bodies an
#            earlier occurrence built (load.lobster's forget_built), so the
#            pinned map_retained_replaced vector must go red on the retention
#            report in the --retain arm and stay green in the plain arm
#   message  the batch read stops refusing ill-formed text, so the pinned
#            message_ill_formed_text vector must go red replayed as a batch
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
	@grep -qE 'the report differs|the oracle discards a damaged load' $(LOBSTER_WIRE_FUZZ_PLANT)/unknown.log || \
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
	@cp -R build/conformance-lobster $(LOBSTER_WIRE_FUZZ_PLANT)/retain
	@sed -i.orig 's/^            if built_slots\[i\] == sl:$$/            if false:/' $(LOBSTER_WIRE_FUZZ_PLANT)/retain/*/retain_unknown.lobster
	@if cmp -s build/conformance-lobster/mapdemo/retain_unknown.lobster $(LOBSTER_WIRE_FUZZ_PLANT)/retain/mapdemo/retain_unknown.lobster; then \
		echo 'NEGATIVE CONTROL FAILED: the planted retain bug did not apply'; exit 1; fi
	@if ./build/conformance-harness wire-fuzz --retain --driver '$(LOBSTER_WIRE_FUZZ_HOST) --units $(LOBSTER_WIRE_FUZZ_PLANT)/retain' \
			--replay testdata/wire/tables/fuzz-vectors/map_retained_replaced.bin --unit mapdemo --root Text \
			> $(LOBSTER_WIRE_FUZZ_PLANT)/retain.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a retaining read that forgets no map entry left the pinned vector green'; \
		cat $(LOBSTER_WIRE_FUZZ_PLANT)/retain.log; exit 1; fi
	@grep -q 'the retention report differs' $(LOBSTER_WIRE_FUZZ_PLANT)/retain.log || \
		{ echo 'NEGATIVE CONTROL FAILED: the vector went red, but not on the retention report'; cat $(LOBSTER_WIRE_FUZZ_PLANT)/retain.log; exit 1; }
	@./build/conformance-harness wire-fuzz --driver '$(LOBSTER_WIRE_FUZZ_HOST) --units $(LOBSTER_WIRE_FUZZ_PLANT)/retain' \
		--replay testdata/wire/tables/fuzz-vectors/map_retained_replaced.bin --unit mapdemo --root Text > /dev/null || \
		{ echo 'NEGATIVE CONTROL FAILED: the retain plant reddened the plain arm too'; exit 1; }
	@cp -R build/conformance-lobster $(LOBSTER_WIRE_FUZZ_PLANT)/message
	@sed -i.orig 's/^    if not text_valid(r\.b, start, n):$$/    if false:/' $(LOBSTER_WIRE_FUZZ_PLANT)/message/*/message.lobster
	@if cmp -s build/conformance-lobster/backenddemo/message.lobster $(LOBSTER_WIRE_FUZZ_PLANT)/message/backenddemo/message.lobster; then \
		echo 'NEGATIVE CONTROL FAILED: the planted message bug did not apply'; exit 1; fi
	@if ./build/conformance-harness wire-fuzz --message --driver '$(LOBSTER_WIRE_FUZZ_HOST) --units $(LOBSTER_WIRE_FUZZ_PLANT)/message' \
			--replay testdata/wire/tables/fuzz-vectors/message_ill_formed_text.bin --unit backenddemo --root StorePurchase \
			> $(LOBSTER_WIRE_FUZZ_PLANT)/message.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a batch read that admits ill-formed text left the pinned vector green'; \
		cat $(LOBSTER_WIRE_FUZZ_PLANT)/message.log; exit 1; fi
	@grep -qE 'the report differs|the oracle discards a damaged load' $(LOBSTER_WIRE_FUZZ_PLANT)/message.log || \
		{ echo 'NEGATIVE CONTROL FAILED: the vector went red, but not on the report'; cat $(LOBSTER_WIRE_FUZZ_PLANT)/message.log; exit 1; }
	$(call lobster_wire_fuzz_vectors,build/conformance-lobster)
	$(call lobster_wire_fuzz_arm_vectors,build/conformance-lobster)
	@echo 'lobster wire-fuzz negative control: a dropped unknown count reds the fuzz, an unguarded save, a retaining read that forgets no map entry and a batch read that admits ill-formed text each red their pinned vector; unplanted all are green'
