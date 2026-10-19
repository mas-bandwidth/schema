# THE DART VM / dart2js DIFFERENTIAL (the schema-per-language-lenses lens).
#
# The generated Dart table codec is run twice over the same corpus: on the VM,
# whose `int` is an exact 64-bit integer, and through dart2js under node, where
# `int` is a 53-bit double and the bitwise operators are 32-bit. A value above
# 2^53 must either be carried identically or be REFUSED by name; silently
# rounding it is the bug this lens exists to see.
#
# The corpus is the one the Makefile's own generate targets read:
# tables/examples (the tables Dart generate target) and test/tables/Bases.schema
# (read by the cpp, go and reference generate targets, and the source of the
# uint64 defaults above 2^53).
#
# HEAVY: this is a nightly differential, never a per-push gate. The fast part is
# the wiring test in internal/ci, which runs on push and pull_request.
DART_DIFF_DART ?= $(DART)
DART_DIFF_NODE ?= node
DART_DIFF_DIR := build/schema-dart-vm-dart2js-differential

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
	@rm -rf $(DART_DIFF_DIR)/gen
	./bin/schema generate --lang dart --out $(DART_DIFF_DIR)/gen/examples tables/examples
	./bin/schema generate --lang dart --out $(DART_DIFF_DIR)/gen/bases test/tables/Bases.schema
	$(DART_DIFF_DART) test/dart-differential/webints.dart > $(DART_DIFF_DIR)/vm.txt
	# THE REFUSAL ARM: dart2js may refuse the generated tree by name (the
	# field ids and uint64 literals are above 2^53). That is the card's
	# "refused": the web build cannot silently round what it cannot hold. Only
	# when it compiles is there a run-time output to compare.
	@if $(DART_DIFF_DART) compile js -O2 -o $(DART_DIFF_DIR)/webints.js \
			test/dart-differential/webints.dart > $(DART_DIFF_DIR)/webints-compile.log 2>&1; then \
		$(DART_DIFF_NODE) $(DART_DIFF_DIR)/webints.js > $(DART_DIFF_DIR)/dart2js.txt; \
		if ! diff -u $(DART_DIFF_DIR)/vm.txt $(DART_DIFF_DIR)/dart2js.txt > $(DART_DIFF_DIR)/diff.txt; then \
			echo "tables-dart-vm-dart2js-differential: DIVERGENCE between the Dart VM and dart2js (REAL finding; $(DART_DIFF_DIR)/diff.txt):"; \
			cat $(DART_DIFF_DIR)/diff.txt; \
			exit 1; \
		fi; \
		echo "tables-dart-vm-dart2js-differential: the VM and dart2js agree above 2^53"; \
	else \
		grep -q "can't be represented exactly in JavaScript" $(DART_DIFF_DIR)/webints-compile.log || \
			{ echo "tables-dart-vm-dart2js-differential: dart2js refused the generated tree for an unrecognised reason"; \
			  cat $(DART_DIFF_DIR)/webints-compile.log; exit 1; }; \
		echo "tables-dart-vm-dart2js-differential: dart2js REFUSED the generated code by name (a value above 2^53 cannot be represented); the VM carried it exactly"; \
	fi
	# THE COMPILE-REFUSAL ROW: a >2^53 literal must be refused by name. If
	# dart2js ever compiles it, the program's values are compared instead.
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
		echo "tables-dart-vm-dart2js-differential: dart2js refused the >2^53 uint64 literal by name"; \
	fi

# THE NEGATIVE CONTROL: a line the web run actually produces for a rounded
# value (int64 max printed as 2^63) must red the comparator, and two equal
# files must leave it green. This is what proves the gate can see a divergence
# instead of comparing a file with itself.
.PHONY: tables-dart-vm-dart2js-differential-negative-control
tables-dart-vm-dart2js-differential-negative-control:
	@mkdir -p $(DART_DIFF_DIR)
	@printf 'u64span=9007199254740993\nu64high=9007199254740993\ni64high=9223372036854775807\n' \
		> $(DART_DIFF_DIR)/nc-vm.txt
	@cp $(DART_DIFF_DIR)/nc-vm.txt $(DART_DIFF_DIR)/nc-same.txt
	@diff -u $(DART_DIFF_DIR)/nc-vm.txt $(DART_DIFF_DIR)/nc-same.txt || \
		{ echo "NEGATIVE CONTROL FAILED: equal files did not compare equal"; exit 1; }
	@sed 's/^i64high=.*/i64high=9223372036854775808/' \
		$(DART_DIFF_DIR)/nc-vm.txt > $(DART_DIFF_DIR)/nc-planted.txt
	@if diff -u $(DART_DIFF_DIR)/nc-vm.txt $(DART_DIFF_DIR)/nc-planted.txt \
			> $(DART_DIFF_DIR)/nc-diff.txt; then \
		echo "NEGATIVE CONTROL FAILED: a rounded 2^63-1 left the comparator green"; exit 1; \
	fi
	@grep -q '^+i64high=9223372036854775808' $(DART_DIFF_DIR)/nc-diff.txt || \
		{ echo "NEGATIVE CONTROL FAILED: the comparator went red for another reason"; \
		  cat $(DART_DIFF_DIR)/nc-diff.txt; exit 1; }
	@grep -m1 '^+i64high=9223372036854775808' $(DART_DIFF_DIR)/nc-diff.txt
	@echo "negative control: the rounded int64 max reds the VM/dart2js comparator"
