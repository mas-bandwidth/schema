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
#      AND a generated corpus driver that imports every *Wire.dart unit and
#      round-trips every table through its own writer and reader, over a
#      WEB-SAFE build: a scratch copy of the generated tree whose
#      schema-internal 64-bit ids are folded to their low 31 bits, while
#      declared range BOUNDS are folded to +-(2^53-1) so the wide lane is not
#      clamped; either way dart2js can accept the tree. This is the lane
#      attempt 1 lacked: it
#      really runs both runtimes and diffs their bytes. The VALUES it carries
#      are deliberately in 2^32..2^53 (u64 fields, counts, bits(64) and the
#      signed int64 table), exact in a JS double but past the 32-bit bitwise
#      operators dart2js uses, so the wide-integer codec is actually exercised.
#
#   1a. THE ABOVE-2^53 RUNTIME LANE (range.dart) runs the generated writer and
#      readU64 over a 64-bit integer ABOVE the exact web range on both
#      runtimes and holds each to its specified side: the VM carries it
#      exactly, dart2js REFUSES it by name ("TableWire: 64-bit value is
#      outside the exact web int range (2^53)"). It also round-trips 2^53-1 on
#      both, so the refusal is not a blanket one. This is the runtime case the
#      compile-refusal lane cannot see and the byte-diff over 25 units does
#      not reach.
#
#   2. THE COMPILE LANE compiles the unmodified generated tree with dart2js,
#      through the same generated driver over every unit, and requires the
#      pin-named refusal ("can't be represented exactly in JavaScript") for the
#      schema-internal ids above 2^53. It first proves the refusal is not
#      vacuous by finding the literal. The VM reference (vmref.dart) proves the
#      VM carries those values exactly. On the web-safe tree the same driver
#      compiles, so every unit is handed to dart2js and executed on both
#      runtimes; no unit is covered by a single example import.
#
# TRIAGE (every finding is REAL or NOT; a REAL finding is fixed in the
# generator unless another card owns it):
#
#   finding                                             verdict  action
#   putU64, wireform.go: 32+ shift                      REAL     fixed here (split by division)
#   readU64, wireform.go: 32+ shift                     REAL     fixed here (scale the high word)
#   _signExtend, wireform.go: 32+ shift                 REAL     fixed here (sign-bit test)
#   putU64 accepts and writes a web int above 2^53     REAL     fixed here (refuse by name on the web)
#   readU64 reconstructs a >2^53 pattern inexactly     REAL     fixed here (refuse by name on the web)
#   f64 raw bits are not integers                      NOT      the raw-bit path keeps _putU64Bits/_readU64Bits
#   wireunion.go:624 (and :724) raw |= byte << (8*b)    REAL     owned by schema-dart-u64-codec-exact-on-web
#   lo |= v << 32 (block/Render.dart, blockhome/Data.dart) REAL   owned by schema-dart-u64-codec-exact-on-web
#   (bits & 0x80000000) << 32 (wide/WideFixed.dart)     REAL     owned by schema-dart-u64-codec-exact-on-web
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
# CROSS-PORT GREP (the other ports' generators sharing a flagged pattern).
# The commands and their hits, verbatim:
#
#   $ grep -n 'raw |=' internal/codegen/darttable/wireunion.go
#   624:	g.pf("%sfor (var b = 0; b < width; b++) { raw |= %s.readByte() << (8 * b); }\n", ind, p)
#   724:	g.pf("%sfor (var b = 0; b < %d; b++) { raw |= %s.readByte() << (8 * b); }\n", ind, w, p)
#
#   $ grep -n 'lo |= v << 32' build/tables-generated-dart/block/Render.dart
#   390:  lo |= v << 32;       407:  lo |= v << 32;
#   424:  lo |= v << 32;       557:  lo |= v << 32;
#   574:  lo |= v << 32;       591:  lo |= v << 32;
#   608:  lo |= v << 32;
#   $ grep -n 'lo |= v << 32' build/tables-generated-dart/blockhome/Data.dart
#   134:  lo |= v << 32;       309:  lo |= v << 32;
#   337:  lo |= v << 32;
#
#   $ grep -n '(bits & 0x80000000) << 32' build/tables-generated-dart/wide/WideFixed.dart
#   1247:          final sign = (bits & 0x80000000) << 32; // the f64 sign bit
#
#   $ grep -n 'v << 32' internal/codegen/dart/functions.go
#   1385:	g.pf("%slo |= v << 32;\n", ind)
#   1401:	g.pf("%shi |= v << 32;\n", ind)
#
#   $ grep -n '>>> 32' internal/codegen/dart/dart.go internal/codegen/dart/int128.go
#   dart.go:431:	h.WriteString("    ? (value >>> 32).toRadixString(16) +\n")
#   int128.go:40:    (value >>> 32).toRadixString(16).padLeft(8, '0') +
#
# internal/codegen/jstable already splits the same read into two 32-bit lanes
# (fixedruntime.go:460-470 and fixedjs.go:424-426) and is not in this
# differential. The table-wire generator shares the high-word-shift pattern
# with the non-table painters (functions.go) and the 128-bit halves (dart.go,
# int128.go); the generated block/runtime sites above are the real sand. All
# of the hits except jstable are owned by schema-dart-u64-codec-exact-on-web.
# Not fixed in this card.
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
	# The driver is generated from the tree, so EVERY unit's writer and reader
	# runs on both runtimes, not one hand-picked example.
	python3 test/dart-differential/corpus_driver.py $(DART_DIFF_DIR)/gen-websafe $(DART_DIFF_DIR)/corpus_websafe.dart
	$(DART_DIFF_DART) $(DART_DIFF_DEFINE) test/dart-differential/webints.dart > $(DART_DIFF_DIR)/vm.txt
	$(DART_DIFF_DART) $(DART_DIFF_DIR)/corpus_websafe.dart >> $(DART_DIFF_DIR)/vm.txt
	$(DART_DIFF_DART) compile js -O2 $(DART_DIFF_DEFINE) -o $(DART_DIFF_DIR)/webints.js \
		test/dart-differential/webints.dart > $(DART_DIFF_DIR)/webints-compile.log 2>&1
	$(DART_DIFF_DART) compile js -O2 -o $(DART_DIFF_DIR)/corpus_websafe.js \
		$(DART_DIFF_DIR)/corpus_websafe.dart > $(DART_DIFF_DIR)/corpus-compile.log 2>&1
	$(DART_DIFF_NODE) $(DART_DIFF_DIR)/webints.js > $(DART_DIFF_DIR)/dart2js.txt
	$(DART_DIFF_NODE) $(DART_DIFF_DIR)/corpus_websafe.js >> $(DART_DIFF_DIR)/dart2js.txt
	@grep -qx 'corpus-units=25' $(DART_DIFF_DIR)/vm.txt || \
		{ echo "tables-dart-vm-dart2js-differential: the runtime lane did not run every generated unit"; \
		  grep '^corpus-units=' $(DART_DIFF_DIR)/vm.txt; exit 1; }
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
	# THE ABOVE-2^53 RUNTIME LANE: the generated writer and readU64 over a
	# 64-bit integer above the exact web-int range. The VM carries it; the web
	# refuses it by name. Each runtime is held to its own specified side, and
	# 2^53-1 still round-trips on both.
	$(DART_DIFF_DART) test/dart-differential/range.dart > $(DART_DIFF_DIR)/range-vm.txt
	$(DART_DIFF_DART) compile js -O2 -o $(DART_DIFF_DIR)/range.js \
		test/dart-differential/range.dart > $(DART_DIFF_DIR)/range-compile.log 2>&1
	$(DART_DIFF_NODE) $(DART_DIFF_DIR)/range.js > $(DART_DIFF_DIR)/range-web.txt
	@grep -Fqx 'range-writer-low=exact' $(DART_DIFF_DIR)/range-vm.txt || \
		{ echo "tables-dart-vm-dart2js-differential: the VM writer did not carry 2^53-1 exactly"; \
		  cat $(DART_DIFF_DIR)/range-vm.txt; exit 1; }
	@grep -Fqx 'range-writer-edge=exact' $(DART_DIFF_DIR)/range-vm.txt || \
		{ echo "tables-dart-vm-dart2js-differential: the VM writer did not carry 2^53 exactly"; \
		  cat $(DART_DIFF_DIR)/range-vm.txt; exit 1; }
	@grep -Fqx 'range-writer-over=exact' $(DART_DIFF_DIR)/range-vm.txt || \
		{ echo "tables-dart-vm-dart2js-differential: the VM writer did not carry 2^53+1 exactly"; \
		  cat $(DART_DIFF_DIR)/range-vm.txt; exit 1; }
	@grep -Fqx 'range-reader-edge=exact:9007199254740992' $(DART_DIFF_DIR)/range-vm.txt || \
		{ echo "tables-dart-vm-dart2js-differential: the VM reader did not return 2^53 exactly"; \
		  cat $(DART_DIFF_DIR)/range-vm.txt; exit 1; }
	@grep -Fqx 'range-reader-over=exact:9007199254740993' $(DART_DIFF_DIR)/range-vm.txt || \
		{ echo "tables-dart-vm-dart2js-differential: the VM reader did not return 2^53+1 exactly"; \
		  cat $(DART_DIFF_DIR)/range-vm.txt; exit 1; }
	@grep -Fqx 'range-reader-neg=exact:-9007199254740992' $(DART_DIFF_DIR)/range-vm.txt || \
		{ echo "tables-dart-vm-dart2js-differential: the VM reader did not return -2^53 exactly"; \
		  cat $(DART_DIFF_DIR)/range-vm.txt; exit 1; }
	@grep -Fqx 'range-safe=9007199254740991' $(DART_DIFF_DIR)/range-vm.txt || \
		{ echo "tables-dart-vm-dart2js-differential: the VM lost the 2^53-1 safe value"; \
		  cat $(DART_DIFF_DIR)/range-vm.txt; exit 1; }
	@grep -Fqx 'range-writer-low=exact' $(DART_DIFF_DIR)/range-web.txt || \
		{ echo "tables-dart-vm-dart2js-differential: dart2js did not carry 2^53-1 exactly"; \
		  cat $(DART_DIFF_DIR)/range-web.txt; exit 1; }
	@grep -Fqx 'range-writer-edge=refused:TableWire: 64-bit value is outside the exact web int range (2^53)' $(DART_DIFF_DIR)/range-web.txt || \
		{ echo "tables-dart-vm-dart2js-differential: dart2js did not refuse the 2^53 table write by name"; \
		  cat $(DART_DIFF_DIR)/range-web.txt; exit 1; }
	@grep -Fqx 'range-writer-over=refused:TableWire: 64-bit value is outside the exact web int range (2^53)' $(DART_DIFF_DIR)/range-web.txt || \
		{ echo "tables-dart-vm-dart2js-differential: dart2js did not refuse the 2^53+1 table write by name"; \
		  cat $(DART_DIFF_DIR)/range-web.txt; exit 1; }
	@grep -Fqx 'range-reader-edge=refused:TableWire: 64-bit value is outside the exact web int range (2^53)' $(DART_DIFF_DIR)/range-web.txt || \
		{ echo "tables-dart-vm-dart2js-differential: dart2js did not refuse the 2^53 read by name"; \
		  cat $(DART_DIFF_DIR)/range-web.txt; exit 1; }
	@grep -Fqx 'range-reader-over=refused:TableWire: 64-bit value is outside the exact web int range (2^53)' $(DART_DIFF_DIR)/range-web.txt || \
		{ echo "tables-dart-vm-dart2js-differential: dart2js did not refuse the 2^53+1 read by name"; \
		  cat $(DART_DIFF_DIR)/range-web.txt; exit 1; }
	@grep -Fqx 'range-reader-neg=refused:TableWire: 64-bit value is outside the exact web int range (2^53)' $(DART_DIFF_DIR)/range-web.txt || \
		{ echo "tables-dart-vm-dart2js-differential: dart2js did not refuse the -2^53 read by name"; \
		  cat $(DART_DIFF_DIR)/range-web.txt; exit 1; }
	@grep -Fqx 'range-safe=9007199254740991' $(DART_DIFF_DIR)/range-web.txt || \
		{ echo "tables-dart-vm-dart2js-differential: dart2js lost the 2^53-1 safe value"; \
		  cat $(DART_DIFF_DIR)/range-web.txt; exit 1; }
	# THE COMPILE LANE: the unmodified tree cannot name its >2^53 ids to
	# dart2js, and refuses by name. Prove the literal is there first.
	@found=0; for f in $(DART_DIFF_DIR)/gen/*/*Wire.dart; do \
		grep -Eq '^const int [A-Za-z_0-9]+ = -?[0-9]{16,};' $$f && found=$$((found+1)); \
	done; [ $$found -gt 0 ] || \
		{ echo "tables-dart-vm-dart2js-differential: no >2^53 literal in the generated corpus, so the refusal lane is vacuous"; exit 1; }
	@python3 test/dart-differential/corpus_driver.py $(DART_DIFF_DIR)/gen $(DART_DIFF_DIR)/corpus_gen.dart
	@if $(DART_DIFF_DART) compile js -O2 -o $(DART_DIFF_DIR)/corpus_gen.js \
			$(DART_DIFF_DIR)/corpus_gen.dart > $(DART_DIFF_DIR)/corpus-gen-compile.log 2>&1; then \
		$(DART_DIFF_DART) $(DART_DIFF_DIR)/corpus_gen.dart > $(DART_DIFF_DIR)/corpus-vm.txt; \
		$(DART_DIFF_NODE) $(DART_DIFF_DIR)/corpus_gen.js > $(DART_DIFF_DIR)/corpus-js.txt; \
		if ! diff -u $(DART_DIFF_DIR)/corpus-vm.txt $(DART_DIFF_DIR)/corpus-js.txt; then \
			echo "tables-dart-vm-dart2js-differential: DIVERGENCE over the whole unmodified corpus"; \
			exit 1; \
		fi; \
		echo "tables-dart-vm-dart2js-differential: the whole unmodified corpus compiled and agreed"; \
	else \
		grep -q "can't be represented exactly in JavaScript" $(DART_DIFF_DIR)/corpus-gen-compile.log || \
			{ echo "tables-dart-vm-dart2js-differential: dart2js refused the corpus for an unrecognised reason"; \
			  cat $(DART_DIFF_DIR)/corpus-gen-compile.log; exit 1; }; \
		echo "tables-dart-vm-dart2js-differential: the VM carried >2^53 exactly and dart2js refused the whole corpus by name ($(DART_DIFF_DIR)/vmref.txt, $(DART_DIFF_DIR)/corpus-gen-compile.log)"; \
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
