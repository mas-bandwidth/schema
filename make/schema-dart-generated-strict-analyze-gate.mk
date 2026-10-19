# make/schema-dart-generated-strict-analyze-gate.mk — the generated Dart tree's
# STRICT analyzer gate (docs/SPEC-TABLES.md §3.3, §3.4).
#
# `dart analyze` accepts a tree on the language's default lint set alone, and an
# object compared to an integer is legal Dart: `value.position != 0` on a nested
# message compiles and is always true, so the skip-the-default check never fires
# and the bytes disagree with the Go emitter's. unrelated_type_equality_checks
# is the analyzer's own name for that program, and this gate keeps it ON over
# build/tables-generated-dart (the -all tree) beside the rules every other
# analyzer run over the tree carries. The options file is written by
# build/tables-generated-dart/.stamp (make/dart.mk), so tables-dart-clean holds
# the same set.
#
# THE PLANTED CONTROL appends an `x != 0` on a non-int to a copy of one
# generated library and requires the gate to go RED naming the lint. The
# generated tree is never rewritten.

DART ?= $(CURDIR)/dist/dart-sdk-3.13.2/bin/dart
SCHEMA_DART_STRICT_DIR ?= build/tables-generated-dart

.PHONY: schema-dart-generated-strict-analyze-gate
schema-dart-generated-strict-analyze-gate: build/tables-generated-dart/.stamp
	@$(DART) analyze $(SCHEMA_DART_STRICT_DIR)
	@echo "schema Dart strict analyze gate: generated tree clean under unrelated_type_equality_checks"

.PHONY: schema-dart-generated-strict-analyze-negative-control
schema-dart-generated-strict-analyze-negative-control: build/tables-generated-dart/.stamp
	@rm -rf build/schema-dart-strict-analyze-nc && mkdir -p build/schema-dart-strict-analyze-nc
	@f=$$(ls build/tables-generated-dart/*/*Fixed.dart 2>/dev/null | head -1); \
		test -n "$$f" || { echo "NEGATIVE CONTROL FAILED: the generated tree carries no *Fixed.dart to plant in"; exit 1; }; \
		cp "$$f" build/schema-dart-strict-analyze-nc/planted.dart; \
		printf '\nclass PlantedNestedDefault {}\n\nvoid plantedNestedDefaultCheck(PlantedNestedDefault value) {\n  if (value != 0) { return; }\n}\n' >> build/schema-dart-strict-analyze-nc/planted.dart; \
		cp $(SCHEMA_DART_STRICT_DIR)/analysis_options.yaml build/schema-dart-strict-analyze-nc/analysis_options.yaml
	@if $(DART) analyze build/schema-dart-strict-analyze-nc > build/schema-dart-strict-analyze-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the strict analyzer stayed green with an object != 0 planted"; \
		cat build/schema-dart-strict-analyze-nc/log; exit 1; \
	fi
	@grep -q "unrelated_type_equality_checks" build/schema-dart-strict-analyze-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the analyzer went red for some other reason"; \
		  cat build/schema-dart-strict-analyze-nc/log; exit 1; }
	@grep -m1 "unrelated_type_equality_checks" build/schema-dart-strict-analyze-nc/log
	@echo "negative control: object != 0 turns the schema Dart strict analyze gate RED"
