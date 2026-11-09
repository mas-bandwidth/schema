# make/lobster-fuzz.mk — the LOBSTER leg's wire-fuzz differential against the
# Go engine (schema-lobster-fuzz-differential; docs/PORT-LOBSTER.md, THE
# HARNESS, "The wire-fuzz pipe"; test/conformance/README.md, "The wire-fuzz
# driver"). Included by the Makefile's wildcard include, after make/lobster.mk.
#
# The harness mutates the corpus and compares the native read verdict plus
# the complete canonical write-back with internal/tablewire. The host runs
# wirefuzz.lobster once per generated unit. The ordinary run covers all file
# roots (including declared-fixed form 3 through the native Record bridge)
# and fixed message batches. --builder covers variable file roots without
# region preflight; --retain covers their retaining load/save pair.
# Diagnostics and allocation measurements are not cross-language verdicts.
#
# Every pinned vector of test/conformance/lobster/vectors replays in the
# builder arm; the retention and message arms replay the corpus's own pinned
# vectors (testdata/wire/tables/fuzz-vectors).
#
# THE BUDGET IS A COUNT: the enumerated pass is a fixed function of the corpus
# (the run reports its effective live seeds, roots and enumerated mutants).
# The random pass is LOBSTER_WIRE_FUZZ_N per arm. test-lobster runs the small
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
# over the units in $(1), with an explicit unit/root assignment per vector
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

# THE NEGATIVE CONTROLS plant observable behavior in copied generated units:
#   fixed    refuse a valid fixed file (also proves the roster carries it)
#   guard    remove a field guard, changing the canonical decoded value
#   retain   omit retained unknown fields from the accepted write-back
#   message  admit ill-formed UTF-8 in a message batch
# A dropped unknown counter is a positive control: diagnostics alone must
# leave a valid read and its write-back green. Unplanted vectors are green.
LOBSTER_WIRE_FUZZ_PLANT := build/lobster-wire-fuzz-plant
tables-lobster-wire-fuzz-negative-control: lobster-dist-present build/conformance-harness build/conformance-lobster build/lobster-wirefuzzhost
	@rm -rf $(LOBSTER_WIRE_FUZZ_PLANT) && mkdir -p $(LOBSTER_WIRE_FUZZ_PLANT)
	@cp -R build/conformance-lobster $(LOBSTER_WIRE_FUZZ_PLANT)/unknown
	@sed -i.orig 's/rep\.unknown++$$/rep.unknown += 0/' $(LOBSTER_WIRE_FUZZ_PLANT)/unknown/*/load.lobster
	@if cmp -s build/conformance-lobster/tabledemo/load.lobster $(LOBSTER_WIRE_FUZZ_PLANT)/unknown/tabledemo/load.lobster; then \
		echo 'CONTROL FAILED: the diagnostic-only change did not apply'; exit 1; fi
	@./build/conformance-harness wire-fuzz --driver '$(LOBSTER_WIRE_FUZZ_HOST) --units $(LOBSTER_WIRE_FUZZ_PLANT)/unknown' \
		--replay testdata/wire/tables/retain_rt2.bin --unit tblrt1 --root Node > $(LOBSTER_WIRE_FUZZ_PLANT)/unknown.log 2>&1 || \
		{ echo 'CONTROL FAILED: changing an unknown counter changed the verdict or value'; cat $(LOBSTER_WIRE_FUZZ_PLANT)/unknown.log; exit 1; }
	@cp -R build/conformance-lobster $(LOBSTER_WIRE_FUZZ_PLANT)/fixed
	@sed -i.orig 's/return fixed_record_codec(root).read_fixed(data, report)/return nil/' $(LOBSTER_WIRE_FUZZ_PLANT)/fixed/*/load.lobster
	@if cmp -s build/conformance-lobster/tblv1/load.lobster $(LOBSTER_WIRE_FUZZ_PLANT)/fixed/tblv1/load.lobster; then \
		echo 'NEGATIVE CONTROL FAILED: the planted fixed-read refusal did not apply'; exit 1; fi
	@if ./build/conformance-harness wire-fuzz --driver '$(LOBSTER_WIRE_FUZZ_HOST) --units $(LOBSTER_WIRE_FUZZ_PLANT)/fixed' \
			--replay testdata/conformance/tables/wire/v1_cfg.bin --unit tblv1 --root Cfg > $(LOBSTER_WIRE_FUZZ_PLANT)/fixed.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: refusing a valid fixed file left the replay green'; cat $(LOBSTER_WIRE_FUZZ_PLANT)/fixed.log; exit 1; fi
	@grep -q 'read acceptance differs' $(LOBSTER_WIRE_FUZZ_PLANT)/fixed.log || \
		{ echo 'NEGATIVE CONTROL FAILED: fixed replay did not differ on acceptance'; cat $(LOBSTER_WIRE_FUZZ_PLANT)/fixed.log; exit 1; }
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
	@sed -i.orig 's/^    register_retain_tail(RetainTailImpl { retain: retain, report: report })$$/    \/\/ negative control: omit retained fields/' $(LOBSTER_WIRE_FUZZ_PLANT)/retain/*/retain_unknown.lobster
	@if cmp -s build/conformance-lobster/tblrt1/retain_unknown.lobster $(LOBSTER_WIRE_FUZZ_PLANT)/retain/tblrt1/retain_unknown.lobster; then \
		echo 'NEGATIVE CONTROL FAILED: the planted retained-field omission did not apply'; exit 1; fi
	@if ./build/conformance-harness wire-fuzz --retain --driver '$(LOBSTER_WIRE_FUZZ_HOST) --units $(LOBSTER_WIRE_FUZZ_PLANT)/retain' \
			--replay testdata/wire/tables/retain_rt2.bin --unit tblrt1 --root Node \
			> $(LOBSTER_WIRE_FUZZ_PLANT)/retain.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: omitting retained fields left the replay green'; cat $(LOBSTER_WIRE_FUZZ_PLANT)/retain.log; exit 1; fi
	@grep -q 'the decoded value differs' $(LOBSTER_WIRE_FUZZ_PLANT)/retain.log || \
		{ echo 'NEGATIVE CONTROL FAILED: retained-field omission did not differ on value'; cat $(LOBSTER_WIRE_FUZZ_PLANT)/retain.log; exit 1; }
	@./build/conformance-harness wire-fuzz --driver '$(LOBSTER_WIRE_FUZZ_HOST) --units $(LOBSTER_WIRE_FUZZ_PLANT)/retain' \
		--replay testdata/wire/tables/retain_rt2.bin --unit tblrt1 --root Node > /dev/null || \
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
	@grep -q 'read acceptance differs' $(LOBSTER_WIRE_FUZZ_PLANT)/message.log || \
		{ echo 'NEGATIVE CONTROL FAILED: the vector went red, but not on acceptance'; cat $(LOBSTER_WIRE_FUZZ_PLANT)/message.log; exit 1; }
	$(call lobster_wire_fuzz_vectors,build/conformance-lobster)
	$(call lobster_wire_fuzz_arm_vectors,build/conformance-lobster)
	@./build/conformance-harness wire-fuzz --driver '$(LOBSTER_WIRE_FUZZ_HOST)' --replay testdata/conformance/tables/wire/v1_cfg.bin --unit tblv1 --root Cfg
	@./build/conformance-harness wire-fuzz --retain --driver '$(LOBSTER_WIRE_FUZZ_HOST)' --replay testdata/wire/tables/retain_rt2.bin --unit tblrt1 --root Node
	@echo 'lobster wire-fuzz controls: fixed refusal, unguarded save, omitted retained fields and admitted message damage each diverge; diagnostic-only and unplanted controls stay green'

# THE VERDICT PIPE is the strict-read gate. The builder arm is one load
# (the whole value or a refusal). The retain arm is the retaining write-back.
# SCHEMA_SLOW=1 is required so slowtest.Gate does not skip. The negative
# control plants its fault in the generated artifact the VM compiles.
.PHONY: tables-lobster-wire-fuzz-verdict tables-lobster-wire-fuzz-verdict-negative-control
tables-lobster-wire-fuzz-verdict:
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 40m ./internal/codegen/lobstertable/ -run '^TestLobsterWireFuzzVerdictPipe$$'
tables-lobster-wire-fuzz-verdict-negative-control:
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 40m ./internal/codegen/lobstertable/ -run '^TestLobsterWireFuzzVerdictPipeNegativeControl$$'
test-lobster: tables-lobster-wire-fuzz-verdict tables-lobster-wire-fuzz-verdict-negative-control
