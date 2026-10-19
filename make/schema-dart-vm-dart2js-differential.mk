# THE DART VM / dart2js DIFFERENTIAL (the schema-per-language-lenses lens).
#
# The generated Dart table codec is run twice over the same corpus: on the VM,
# whose `int` is an exact 64-bit integer, and through dart2js under node, where
# `int` is a 53-bit double and the bitwise operators are 32-bit. A value above
# 2^53 must either be carried identically or be REFUSED by name; silently
# rounding it is the bug this lens exists to see.
#
# THE CORPUS is the one the Dart generate target reads, not a second one:
# build/tables-generated-dart/.stamp (make/dart.mk) plus test/tables/Bases.schema,
# the source of the uint64 defaults above 2^53.
#
# TWO LANES, BOTH REQUIRED:
#
#   1. THE RUNTIME LANE compares the VM and node transcripts of webints.dart
#      over a WEB-SAFE build: a scratch copy of the generated tree whose
#      schema-internal 64-bit literal BOUNDS are folded to their low 31 bits,
#      so dart2js can accept the tree. This is the lane attempt 1 lacked: it
#      really runs both runtimes and diffs their bytes. The VALUES it carries
#      are deliberately in 2^32..2^53 (u64 fields, counts, bits(64) and the
#      signed int64 table), exact in a JS double but past the 32-bit bitwise
#      operators dart2js uses, so the wide-integer codec is actually exercised.
#      The compile lane is the refusal for values above 2^53.
#
#   2. THE COMPILE LANE compiles the unmodified generated tree with dart2js and
#      requires the pin-named refusal ("can't be represented exactly in
#      JavaScript") for the schema-internal ids above 2^53. It first proves the
#      refusal is not vacuous by finding the literal. The VM reference
#      (vmref.dart) proves the VM carries those values exactly.
#
# TRIAGE. REAL, fixed in the generator (internal/codegen/darttable/wireform.go,
# in this card's PATHS): putU64/readU64 shifted by 32 or more, which dart2js
# computes with its 32-bit operator, so every u64 from 2^32 to 2^53 was silently
# corrupted (2^32+5 wrote as 5) and the wire bytes differed; _signExtend used a
# 32-bit shift too, so every negative int32 read back as 0. Both are fixed with
# division/scaling and a sign-bit test, and webints.dart keeps the values that
# were red at BASE. REAL, still reported as PATHS-PROPOSED because it is a
# literal rather than an operator: dartU64Literal emits a 64-bit int literal,
# which dart2js refuses by name; the compile lane records that refusal and the
# JS port already splits the same hash into two uint32 lanes.
#
# CROSS-PORT GREP: internal/codegen/jstable has the same pattern and already
# splits it (variableform.go u64 uses BigInt and >> 8n; fixedruntime.go splits
# two 32-bit lanes with a comment on the MOD-32 hazard). internal/codegen/dart
# (the non-table painter) emits (value >>> 32) in dart.go and int128.go; that
# port is not in this differential and is named in the report, not fixed here.
#
# HEAVY: nightly and workflow_dispatch only. The fast part is the wiring test in
# internal/ci, which runs on push and pull_request.
DART_DIFF_DART ?= $(DART)
DART_DIFF_NODE ?= node
DART_DIFF_DIR := build/schema-dart-vm-dart2js-differential
DART_DIFF_PLANT ?=
DART_DIFF_DEFINE := $(if $(DART_DIFF_PLANT),-DDART_DIFF_PLANT=true,)

.PHONY: tables-dart-vm-dart2js-differential
tables-dart-vm-dart2js-differential: bin/schema
	@mkdir -p $(DART_DIFF_DIR)
	@command -v $(DART_DIFF_DART) >/dev/null 2>&1 || \
		{ echo "tool missing: dart ($(DART_DIFF_DART))"; exit 1; }
	@command -v $(DART_DIFF_NODE) >/dev/null 2>&1 || \
		{ echo "tool missing: node ($(DART_DIFF_NODE))"; exit 1; }
	@v=$$($(DART_DIFF_DART) --version 2>&1) || { echo "tool missing: dart ($$v)"; exit 1; }; \
		case "$$v" in \
		"Dart SDK version: $(DART_SDK_PIN) "*) ;; \
		*) echo "differential pin: want Dart SDK $(DART_SDK_PIN), running: $$v"; exit 1;; esac
	@n=$$($(DART_DIFF_NODE) --version 2>&1); \
		case "$$n" in \
		"v26.7.0") ;; \
		*) echo "differential pin: want node v26.7.0, running: $$n"; exit 1;; esac
	@rm -rf $(DART_DIFF_DIR)/gen $(DART_DIFF_DIR)/gen-websafe
	$(MAKE) -s build/tables-generated-dart/.stamp
	cp -r build/tables-generated-dart $(DART_DIFF_DIR)/gen
	./bin/schema generate --lang dart --out $(DART_DIFF_DIR)/gen/bases test/tables/Bases.schema
	# THE VM REFERENCE: the unmodified tree, exact 64-bit values above 2^53.
	$(DART_DIFF_DART) test/dart-differential/vmref.dart > $(DART_DIFF_DIR)/vmref.txt
	# THE WEB-SAFE BUILD: fold every schema-internal 64-bit literal to its low
	# 31 bits in a scratch copy, on both lanes alike. This is not a fix and no
	# generated file in the tree is touched; it is the only shape dart2js can
	# represent, and the unsigned codec's bytes are compared in it.
	cp -r $(DART_DIFF_DIR)/gen $(DART_DIFF_DIR)/gen-websafe
	python3 test/dart-differential/websafe.py $(DART_DIFF_DIR)/gen-websafe
	# THE RUNTIME LANE: the same web-safe source on the VM and under dart2js.
	$(DART_DIFF_DART) $(DART_DIFF_DEFINE) test/dart-differential/webints.dart > $(DART_DIFF_DIR)/vm.txt
	$(DART_DIFF_DART) compile js -O2 $(DART_DIFF_DEFINE) -o $(DART_DIFF_DIR)/webints.js \
		test/dart-differential/webints.dart > $(DART_DIFF_DIR)/webints-compile.log 2>&1
	$(DART_DIFF_NODE) $(DART_DIFF_DIR)/webints.js > $(DART_DIFF_DIR)/dart2js.txt
	@if ! diff -u $(DART_DIFF_DIR)/vm.txt $(DART_DIFF_DIR)/dart2js.txt > $(DART_DIFF_DIR)/diff.txt; then \
		if [ -n "$(DART_DIFF_PLANT)" ]; then \
			grep -q '^+plant=' $(DART_DIFF_DIR)/diff.txt && \
			{ echo "tables-dart-vm-dart2js-differential: PLANT seen: the web run rounded a value above 2^53"; \
			  cat $(DART_DIFF_DIR)/diff.txt; exit 1; }; \
		fi; \
		echo "tables-dart-vm-dart2js-differential: DIVERGENCE between the Dart VM and dart2js (REAL finding; $(DART_DIFF_DIR)/diff.txt):"; \
		cat $(DART_DIFF_DIR)/diff.txt; \
		exit 1; \
	fi
	@if [ -n "$(DART_DIFF_PLANT)" ]; then \
		echo "NEGATIVE CONTROL FAILED: the planted run-time divergence left the comparator green"; exit 1; fi
	# THE COMPILE LANE: the unmodified tree cannot name its >2^53 ids to
	# dart2js, and refuses by name. Prove the literal is there first.
	@found=0; for f in $(DART_DIFF_DIR)/gen/*/*Wire.dart; do \
		grep -Eq '^const int [A-Za-z_0-9]+ = -?[0-9]{16,};' $$f && found=$$((found+1)); \
	done; [ $$found -gt 0 ] || \
		{ echo "tables-dart-vm-dart2js-differential: no >2^53 literal in the generated corpus, so the refusal lane is vacuous"; exit 1; }
	@if $(DART_DIFF_DART) compile js -O2 -o $(DART_DIFF_DIR)/bases.js \
			test/dart-differential/bases_literals.dart > $(DART_DIFF_DIR)/bases-compile.log 2>&1; then \
		$(DART_DIFF_DART) test/dart-differential/bases_literals.dart > $(DART_DIFF_DIR)/bases-vm.txt; \
		$(DART_DIFF_NODE) $(DART_DIFF_DIR)/bases.js > $(DART_DIFF_DIR)/bases-js.txt; \
		if ! diff -u $(DART_DIFF_DIR)/bases-vm.txt $(DART_DIFF_DIR)/bases-js.txt; then \
			echo "tables-dart-vm-dart2js-differential: DIVERGENCE on the >2^53 literals of Bases.schema"; \
			exit 1; \
		fi; \
		echo "tables-dart-vm-dart2js-differential: Bases.schema compiled and agreed"; \
	else \
		grep -q "can't be represented exactly in JavaScript" $(DART_DIFF_DIR)/bases-compile.log || \
			{ echo "tables-dart-vm-dart2js-differential: dart2js refused Bases.schema for an unrecognised reason"; \
			  cat $(DART_DIFF_DIR)/bases-compile.log; exit 1; }; \
		echo "tables-dart-vm-dart2js-differential: the VM carried >2^53 exactly and dart2js refused the tree by name ($(DART_DIFF_DIR)/vmref.txt, $(DART_DIFF_DIR)/diff.txt)"; \
	fi

# THE NEGATIVE CONTROL runs the REAL target with a planted run-time divergence:
# webints.dart prints a 2^53+1 value (as int.parse, not a literal) only when
# DART_DIFF_PLANT=1. The VM prints 2^53+3 and dart2js prints 2^53+2, so the
# target must exit non-zero and name the plant. No hand-written file pair is
# compared here.
.PHONY: tables-dart-vm-dart2js-differential-negative-control
tables-dart-vm-dart2js-differential-negative-control:
	@mkdir -p $(DART_DIFF_DIR)
	@if DART_DIFF_PLANT=1 $(MAKE) -s tables-dart-vm-dart2js-differential > $(DART_DIFF_DIR)/nc.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a planted run-time divergence left the target green"; \
		cat $(DART_DIFF_DIR)/nc.log; exit 1; \
	fi
	@grep -q "PLANT seen" $(DART_DIFF_DIR)/nc.log || \
		{ echo "NEGATIVE CONTROL FAILED: the target went red for another reason"; \
		  cat $(DART_DIFF_DIR)/nc.log; exit 1; }
	@grep -m1 "PLANT seen" $(DART_DIFF_DIR)/nc.log
	@echo "negative control: the target itself reds on a planted VM/dart2js run-time divergence"
