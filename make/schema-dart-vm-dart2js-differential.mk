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
# TRIAGE (every finding is REAL or NOT; a REAL finding is fixed in the
# generator unless another card owns it):
#
#   finding                                             verdict  action
#   putU64, wireform.go: 32+ shift                      REAL     fixed here (split by division)
#   readU64, wireform.go: 32+ shift                     REAL     fixed here (scale the high word)
#   _signExtend, wireform.go: 32+ shift                 REAL     fixed here (sign-bit test)
#   wireunion.go:624 (and :724) raw |= byte << (8*b)    REAL     fixed by schema-dart-u64-codec-exact-on-web (readU64 + web refusal; union-arm witness)
#   readLeb, wireform.go: (b & 0x7F) << (7*i)           REAL     fixed by schema-dart-u64-codec-exact-on-web (exact scaling; leb witness)
#   _RetainCursor u64/length, retainwire.go: shifts     REAL     fixed by schema-dart-u64-codec-exact-on-web (no lane value: retained ids fold below 2^31 here)
#   (bits & 0x80000000) << 32 (wide/WideFixed.dart)     REAL     fixed by schema-dart-u64-codec-exact-on-web (two 32-bit words); the fixed form's
#                                                                load reads getUint64, which dart2js refuses by name, so the lane cannot reach it
#   lo |= v << 32 (block/Render.dart, blockhome/Data.dart) REAL   NOT fixed: internal/codegen/dart's packet layer; it is all
#                                                                Uint64List/getUint64/setUint64, which dart2js refuses by name before the site
#   dartU64Literal emits a >2^53 int literal            REAL     compile lane records the by-name refusal (PATHS-PROPOSED: a literal, not an operator)
#
# The three fixes here are pinned by TestDartWireWideIntHelpersAreWebSafe and
# by webints.dart, whose wide values were red at BASE (2^32+5 read as 5, every
# negative int32 as 0, the wire bytes differed). The wireunion.go/128-bit and
# fixed-runtime sites are named, not fixed here: another card owns them.
#
# INT64 LANE BOUNDS: a declared range bound is not an id. The web-safe fold
# maps a bound to +-(2^53-1), the widest exact web int, so a u64/int64 value
# in 2^32..2^53 is inside its declared range and reads back unclamped on both
# runtimes. webints.dart prints roundtrip=ok and the target requires that line
# on both transcripts with the clamped counters at 0. Folding the int64 bounds
# to their low 31 bits would clamp every value to [-2^31, 2^31-1] and the lane
# would compare two equally-clamped transcripts while claiming to test 2^53.
#
# CROSS-PORT GREP: the other ports' generators sharing a flagged pattern are
# named in the report with the grep output itself. internal/codegen/jstable
# already splits the same read into two 32-bit lanes; internal/codegen/dart
# (the non-table painter) still emits lo |= v << 32 at functions.go:1385 and
# is not in this differential. Not fixed in this card.
#
# HEAVY: nightly and workflow_dispatch only. The fast part is the wiring test in
# internal/ci, which runs on push and pull_request.
DART_DIFF_DART ?= $(DART)
DART_DIFF_NODE ?= node
DART_DIFF_DIR := build/schema-dart-vm-dart2js-differential
DART_DIFF_PLANT ?=
DART_DIFF_DEFINE := $(if $(DART_DIFF_PLANT),-DDART_DIFF_PLANT=true,)
# THE REVERSED WITNESSES (schema-dart-u64-codec-exact-on-web): set only by the
# witness controls below, each puts the 32-bit form back at ONE site of the
# web-safe scratch copy (never a tracked file) and the lane must go red.
DART_DIFF_WITNESS ?=
DART_DIFF_A2 := $(DART_DIFF_DIR)/gen-websafe/a2/Tbla2Wire.dart

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
	# THE UNION INT64 ARM: A2.schema's union Value carries `a int64` (webints.dart).
	./bin/schema generate --lang dart --out $(DART_DIFF_DIR)/gen/a2 test/tables/A2.schema
	# THE VM REFERENCE: the unmodified tree, exact 64-bit values above 2^53.
	$(DART_DIFF_DART) test/dart-differential/vmref.dart > $(DART_DIFF_DIR)/vmref.txt
	# THE WEB-SAFE BUILD: fold every schema-internal 64-bit literal to its low
	# 31 bits in a scratch copy, on both lanes alike. This is not a fix and no
	# generated file in the tree is touched; it is the only shape dart2js can
	# represent, and the unsigned codec's bytes are compared in it.
	cp -r $(DART_DIFF_DIR)/gen $(DART_DIFF_DIR)/gen-websafe
	python3 test/dart-differential/websafe.py $(DART_DIFF_DIR)/gen-websafe
	# A REVERSED WITNESS, when one is asked for: union = wireunion.go's integer
	# arm reads an 8-byte arm with the byte-shift loop again (shifts 32..56);
	# leb = readLeb shifts each group by 7*i again. The plant is proved to
	# have landed before the lane runs, so a witness cannot pass vacuously.
	@case "$(DART_DIFF_WITNESS)" in \
		"") ;; \
		union) sed -i 's/if (width == 8) {/if (width == 99) {/' $(DART_DIFF_A2) && \
			grep -q 'if (width == 99) {' $(DART_DIFF_A2) ;; \
		leb) sed -i 's/v += (b [&] 0x7F) [*] scale;/v |= (b \& 0x7F) << (7 * i);/' $(DART_DIFF_A2) && \
			grep -q 'v |= (b & 0x7F) << (7 [*] i);' $(DART_DIFF_A2) ;; \
		*) echo "tables-dart-vm-dart2js-differential: unknown witness $(DART_DIFF_WITNESS)"; exit 1;; \
	esac || { echo "tables-dart-vm-dart2js-differential: the $(DART_DIFF_WITNESS) witness did not land in $(DART_DIFF_A2)"; exit 1; }
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
	# THE INT64 LANE BOUNDS: the wide values must read back unclamped, not
	# merely agree after both runtimes clamped them.
	@grep -qx 'roundtrip=ok' $(DART_DIFF_DIR)/vm.txt && \
		grep -qx 'roundtrip=ok' $(DART_DIFF_DIR)/dart2js.txt || \
		{ echo "tables-dart-vm-dart2js-differential: the 2^32..2^53 lane was clamped or lost a value (roundtrip is not ok on both runtimes; the int64 bounds were folded wrongly)"; \
		  grep '^roundtrip=' $(DART_DIFF_DIR)/vm.txt $(DART_DIFF_DIR)/dart2js.txt; exit 1; }
	@if [ -n "$(DART_DIFF_PLANT)" ]; then \
		echo "NEGATIVE CONTROL FAILED: the planted run-time divergence left the comparator green"; exit 1; fi
	@if [ -n "$(DART_DIFF_WITNESS)" ]; then \
		echo "NEGATIVE CONTROL FAILED: the $(DART_DIFF_WITNESS) witness left the lane green"; exit 1; fi
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

# THE REVERSED WITNESSES (schema-dart-u64-codec-exact-on-web), one per site the
# runtime lane reaches. Each runs the REAL target with the 32-bit form put back
# at that one site of the scratch copy, and requires the lane to go red naming
# the value the site loses on the web.
#   union: wireunion.go's integer arm (the emitter's `if (width == 8)` branch);
#          dart2js reads the int64 arm 2^32+5 as 5.
#   leb:   wireform.go's readLeb; dart2js reads the five-byte LEB 2^32+8 as 8.
.PHONY: tables-dart-vm-dart2js-differential-union-arm-negative-control tables-dart-vm-dart2js-differential-leb-negative-control
tables-dart-vm-dart2js-differential-union-arm-negative-control:
	@mkdir -p $(DART_DIFF_DIR)
	@if DART_DIFF_WITNESS=union $(MAKE) -s tables-dart-vm-dart2js-differential > $(DART_DIFF_DIR)/w-union.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the union-arm witness left the lane green"; cat $(DART_DIFF_DIR)/w-union.log; exit 1; fi
	@grep -q '^+union\[4294967301\]=' $(DART_DIFF_DIR)/w-union.log || \
		{ echo "NEGATIVE CONTROL FAILED: the lane went red for another reason than the union arm 2^32+5"; \
		  cat $(DART_DIFF_DIR)/w-union.log; exit 1; }
	@grep -m1 '^+union\[4294967301\]=' $(DART_DIFF_DIR)/w-union.log
	@echo "negative control: the 32-bit union arm read reds the lane at 2^32+5"

tables-dart-vm-dart2js-differential-leb-negative-control:
	@mkdir -p $(DART_DIFF_DIR)
	@if DART_DIFF_WITNESS=leb $(MAKE) -s tables-dart-vm-dart2js-differential > $(DART_DIFF_DIR)/w-leb.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the readLeb witness left the lane green"; cat $(DART_DIFF_DIR)/w-leb.log; exit 1; fi
	@grep -q '^+lebHigh=' $(DART_DIFF_DIR)/w-leb.log || \
		{ echo "NEGATIVE CONTROL FAILED: the lane went red for another reason than the LEB length 2^32+8"; \
		  cat $(DART_DIFF_DIR)/w-leb.log; exit 1; }
	@grep -m1 '^+lebHigh=' $(DART_DIFF_DIR)/w-leb.log
	@echo "negative control: the 32-bit readLeb reds the lane at the length 2^32+8"
