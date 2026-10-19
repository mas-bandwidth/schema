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
#      over a WEB-SAFE build: a scratch copy of the generated tree whose id
#      literals are folded to their low 31 bits and whose declared clamp BOUNDS
#      are folded to +/- (2^53-1), so dart2js can accept the tree and the signed
#      lane still reads 2^32..2^53 unclamped. This is the lane attempt 1 lacked: it
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
# TRIAGE (finding, REAL or NOT, action):
#   putU64/readU64/_signExtend shifts of 32+   REAL  fixed in wireform.go here
#     (internal/codegen/darttable/wireform.go:646-690,1111-1128). The fix is
#     carried by this card, which now OWNS the wide Dart codec: the coordinator
#     should retire/re-scope schema-dart-u64-codec-exact-on-web so the two
#     cards stop colliding on wireform.go. webints.dart keeps the values red at
#     BASE; TestDartWireWideIntHelpersAreWebSafe is red at BASE, green here.
#   dartU64Literal >2^53 literal               REAL  PATHS-PROPOSED (literal,
#     not an operator): the compile lane records dart2js's named refusal and
#     vmref.dart proves the VM value.
#   wireunion.go:624 and :724 `raw |= readByte() << (8 * b)` with width up to
#     8                                       REAL  PATHS-PROPOSED; a 64-bit
#     union int/element arm still loses bytes 4..7 under dart2js (generated
#     in TabledemoWire, MessagedemoWire, Mapdemo, r1/r2/v2/wide). Not fixed
#     here: it is the codec fix card's scope.
#   fixedruntime.go:1266 `(bits & 0x80000000) << 32`  REAL  PATHS-PROPOSED;
#     the f64 sign bit is dropped when widening a NaN payload (WideFixed.dart).
#   internal/codegen/dart/functions.go:1385,1401 `lo |= v << 32`  REAL
#     PATHS-PROPOSED; the non-table Dart painter emits it for 33..64-bit
#     groups, so Render.dart/Data.dart read v not v*2^32 under dart2js.
#   internal/codegen/java/functions.go:1370,1386 same `v << 32`  REAL (other
#     port's generator, named not fixed): Java's int shift by 32 is the same
#     identity, so the same 33..64-bit groups lose their high word.
#   wireform.go:1745,1768,1972 `<< 8*b` loops  NOT; widths are 1,2,4 only, so
#     the largest shift is 24.
#   jstable u64 (BigInt, >> 8n) and fixedruntime 32-bit lanes  NOT; already
#     split. internal/codegen/dart (value >>> 32) is a different hazard and is
#     named above.
#
# CROSS-PORT GREP (from the checkout, pasted verbatim):
#   internal/codegen/darttable/wireunion.go:624 ... raw |= p.readByte() << (8 * b) ... width up to 8
#   internal/codegen/darttable/wireunion.go:724 ... raw |= p.readByte() << (8 * b) ... w up to 8
#   internal/codegen/darttable/fixedruntime.go:1266 ... final sign = (bits & 0x80000000) << 32; ...
#   internal/codegen/dart/functions.go:1385 ... lo |= v << 32; ...
#   internal/codegen/dart/functions.go:1401 ... hi |= v << 32; ...
#   internal/codegen/java/functions.go:1370 ... lo |= v << 32; ...
#   internal/codegen/java/functions.go:1386 ... hi |= v << 32; ...
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
	# THE WEB-SAFE BUILD: in a scratch copy, fold id literals to their low 31
	# bits and declared clamp BOUNDS to +/- (2^53-1), on both lanes alike. This
	# is not a fix and no generated file in the tree is touched; it is the only
	# shape dart2js can represent. Bounds stay wide enough that the signed lane
	# reads 2^32..2^53 back unclamped instead of cutting it to a 31-bit bound.
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
	# THE SIGNED LANE'S BOUNDS: the web-safe clamp bounds must let the int64
	# values in 2^32..2^53 read back unclamped on BOTH runtimes. A 31-bit fold
	# clamped all four (sreport clamped:4) and hid the codec behind the bound;
	# the lane is only a lens if the value survives the read.
	@grep -q '^i64=9007199254740991,-9007199254740991,-4294967301,4294967301$$' \
		$(DART_DIFF_DIR)/vm.txt || \
		{ echo "tables-dart-vm-dart2js-differential: the signed lane did not read 2^32..2^53 back (wanted i64=9007199254740991,-9007199254740991,-4294967301,4294967301):"; \
		  cat $(DART_DIFF_DIR)/vm.txt; exit 1; }
	@grep -q '^sreport=clamped:0,' $(DART_DIFF_DIR)/vm.txt || \
		{ echo "tables-dart-vm-dart2js-differential: the signed lane clamped a value in 2^32..2^53; give the web-safe bounds room:"; \
		  cat $(DART_DIFF_DIR)/vm.txt; exit 1; }
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
