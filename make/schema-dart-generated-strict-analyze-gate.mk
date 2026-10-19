# make/schema-dart-generated-strict-analyze-gate.mk — the strict Dart analyzer
# lens over the generated Dart (ideas roadmap group schema-per-language-lenses;
# Glenn, 2026-10-18).
#
# Every language's strictest checker is a separate lens on the generator's
# output. This file runs `dart analyze` with the three strict language modes
# ON — strict-casts, strict-inference and strict-raw-types — plus a curated
# correctness lint set, and treats warnings and infos as fatal. The corpus is
# the generated Dart the Makefile's own generate targets write (make/dart.mk):
# the packet libraries (readers and writers), the ludicrous 128-bit unit, the
# bench units, and the table backend's generated libraries. No second corpus is
# invented here: the target depends on those targets' stamps.
#
# The analyzer reads analysis_options.yaml from the directory it walks, so the
# trees are copied into build/schema-dart-strict-analyze/ and the options file
# sits at that scratch root. The tracked generated/ tree is never edited.
#
# THE LINT SET IS DELIBERATELY CURATED. It is the correctness half of the Dart
# lint registry: the lints that can fire on generated code by design are not
# here. `avoid_dynamic_calls` fires on the view registry's type-agnostic
# `(dynamic v) => v.field` closures; `non_constant_identifier_names` fires on
# schema-declared names (`BuildVersion`); `prefer_is_empty` and
# `curly_braces_in_flow_control_structures` are formatter/style opinions that
# `dart format` already owns; `unrelated_type_equality_checks` and
# `literal_only_boolean_expressions` fire on the fixed message form's
# always-present ref encoding (`ref != 0`, `if (true)`), which belongs to the
# fixed emitter, not to this packet corpus. The triage table in the card's
# report records each one as NOT with the action taken. Nothing else is
# disabled.

DART_STRICT_SDK_PIN ?= 3.13.2

SCHEMA_DART_STRICT_DIR      := build/schema-dart-strict-analyze
SCHEMA_DART_STRICT_NC_DIR   := build/schema-dart-strict-analyze-negative
SCHEMA_DART_STRICT_LOG      := build/schema-dart-strict-analyze/analyze.log
SCHEMA_DART_STRICT_NC_LOG   := build/schema-dart-strict-analyze-negative/analyze.log
SCHEMA_DART_STRICT_FAST_TREES := generated/dart generated/dart-ludicrous generated/bench/dart generated/bench/paired/dart
SCHEMA_DART_STRICT_TABLE_TREES := build/tables-generated-dart

# THE ANALYSIS OPTIONS ARE THE GATE. strict-casts refuses an implicit dynamic
# downcast, strict-inference refuses a type the analyzer had to infer from an
# empty or untyped expression, and strict-raw-types refuses a bare generic. The
# lints below are all in the Dart core/recommended registries; none needs a
# package download.
define SCHEMA_DART_STRICT_OPTIONS
analyzer:
  language:
    strict-casts: true
    strict-inference: true
    strict-raw-types: true

linter:
  rules:
    - always_declare_return_types
    - avoid_empty_else
    - avoid_relative_lib_imports
    - avoid_returning_null_for_void
    - avoid_shadowing_type_parameters
    - avoid_types_as_parameter_names
    - await_only_futures
    - camel_case_extensions
    - camel_case_types
    - collection_methods_unrelated_type
    - empty_statements
    - hash_and_equals
    - implicit_call_tearoffs
    - library_names
    - no_duplicate_case_values
    - null_check_on_nullable_type_parameter
    - prefer_generic_function_type_aliases
    - prefer_is_not_empty
    - prefer_typing_uninitialized_variables
    - test_types_in_equals
    - throw_in_finally
    - unnecessary_overrides
    - unnecessary_statements
    - use_rethrow_when_possible
    - valid_regexps
    - void_checks
endef
export SCHEMA_DART_STRICT_OPTIONS

# strict_probe refuses, by name, when the SDK is absent or is not the pinned
# one. A gate that runs on another SDK proves nothing about the pinned one, and
# a missing tool is reported as "tool missing", never as a pass.
define SCHEMA_DART_STRICT_PROBE
command -v $(DART) >/dev/null 2>&1 || { echo "tool missing: dart ($(DART))"; exit 1; }; \
v=$$($(DART) --version 2>&1); \
case "$$v" in "Dart SDK version: $(DART_STRICT_SDK_PIN) "*) ;; \
*) echo "schema-dart-generated-strict-analyze-gate: REFUSED: this gate is pinned to Dart SDK $(DART_STRICT_SDK_PIN) (DART_STRICT_SDK_PIN in make/schema-dart-generated-strict-analyze-gate.mk), running: $$v"; exit 1;; esac
endef

# strict_copy <trees> copies each generated tree under the scratch root. The
# copies are disposable: the gate never writes a tracked generated file.
define SCHEMA_DART_STRICT_COPY
rm -rf $(1) && mkdir -p $(1); \
for t in $(2); do \
	mkdir -p "$(1)/$$t"; \
	cp -R "$$t"/. "$(1)/$$t"/ || exit 1; \
done; \
printf '%s\n' "$$SCHEMA_DART_STRICT_OPTIONS" > "$(1)/analysis_options.yaml"
endef

.PHONY: schema-dart-generated-strict-analyze-gate
schema-dart-generated-strict-analyze-gate: toolchain-dart \
		generated/dart/.stamp generated/dart-ludicrous/.stamp \
		generated/bench/dart/.stamp generated/bench/paired/dart/.stamp
	@$(SCHEMA_DART_STRICT_PROBE)
	@$(call SCHEMA_DART_STRICT_COPY,$(SCHEMA_DART_STRICT_DIR),$(SCHEMA_DART_STRICT_FAST_TREES))
	@$(DART) analyze --fatal-infos --fatal-warnings $(SCHEMA_DART_STRICT_DIR) \
		> $(SCHEMA_DART_STRICT_LOG) 2>&1 || { cat $(SCHEMA_DART_STRICT_LOG); exit 1; }
	@cat $(SCHEMA_DART_STRICT_LOG)
	@echo 'schema Dart strict analyze gate: generated packet trees are clean under strict-casts, strict-inference, strict-raw-types and the correctness lint set'

# THE TABLE HALF. The table backend's libraries are generated into build/, not
# committed, so this target builds the corpus first; it is the nightly part
# because generation plus analysis is the costlier half.
.PHONY: schema-dart-generated-strict-analyze-gate-tables
schema-dart-generated-strict-analyze-gate-tables: toolchain-dart build/tables-generated-dart/.stamp
	@$(SCHEMA_DART_STRICT_PROBE)
	@$(call SCHEMA_DART_STRICT_COPY,$(SCHEMA_DART_STRICT_DIR),$(SCHEMA_DART_STRICT_TABLE_TREES))
	@$(DART) analyze --fatal-infos --fatal-warnings $(SCHEMA_DART_STRICT_DIR) \
		> $(SCHEMA_DART_STRICT_LOG) 2>&1 || { cat $(SCHEMA_DART_STRICT_LOG); exit 1; }
	@cat $(SCHEMA_DART_STRICT_LOG)
	@echo 'schema Dart strict analyze gate (tables): the table backend output is clean under the same strict modes and lint set'

# THE WHOLE CORPUS, in one scratch root: the committed packet trees and the
# table backend's generated tree. This is the target the nightly workflow runs.
.PHONY: schema-dart-generated-strict-analyze-gate-all
schema-dart-generated-strict-analyze-gate-all: toolchain-dart \
		generated/dart/.stamp generated/dart-ludicrous/.stamp \
		generated/bench/dart/.stamp generated/bench/paired/dart/.stamp \
		build/tables-generated-dart/.stamp
	@$(SCHEMA_DART_STRICT_PROBE)
	@$(call SCHEMA_DART_STRICT_COPY,$(SCHEMA_DART_STRICT_DIR),$(SCHEMA_DART_STRICT_FAST_TREES) $(SCHEMA_DART_STRICT_TABLE_TREES))
	@$(DART) analyze --fatal-infos --fatal-warnings $(SCHEMA_DART_STRICT_DIR) \
		> $(SCHEMA_DART_STRICT_LOG) 2>&1 || { cat $(SCHEMA_DART_STRICT_LOG); exit 1; }
	@cat $(SCHEMA_DART_STRICT_LOG)
	@echo 'schema Dart strict analyze gate (all): every generated tree is clean under the strict modes and lint set'

# PROOF THE GATE FIRES. A copy of the committed packet trees gets one planted
# raw type in StrictAnalyzePlant.dart; strict-raw-types must reject it and the
# log must name the plant and the rule. A gate that stayed green here would be
# reading the wrong directory or not reading analysis_options.yaml at all.
.PHONY: schema-dart-generated-strict-analyze-gate-negative-control
schema-dart-generated-strict-analyze-gate-negative-control: toolchain-dart \
		generated/dart/.stamp generated/bench/paired/dart/.stamp
	@$(SCHEMA_DART_STRICT_PROBE)
	@$(call SCHEMA_DART_STRICT_COPY,$(SCHEMA_DART_STRICT_NC_DIR),generated/dart generated/bench/paired/dart)
	@printf '%s\n' '// PLANTED by make/schema-dart-generated-strict-analyze-gate.mk: a bare generic must be fatal.' \
		'List schemaStrictAnalyzePlant = [];' > $(SCHEMA_DART_STRICT_NC_DIR)/StrictAnalyzePlant.dart
	@if $(DART) analyze --fatal-infos --fatal-warnings $(SCHEMA_DART_STRICT_NC_DIR) \
			> $(SCHEMA_DART_STRICT_NC_LOG) 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the strict-analyze gate stayed green with a planted raw type'; \
		cat $(SCHEMA_DART_STRICT_NC_LOG); exit 1; \
	fi
	@grep -q 'StrictAnalyzePlant.dart.*strict_raw_type' $(SCHEMA_DART_STRICT_NC_LOG) || \
		{ echo 'NEGATIVE CONTROL FAILED: the gate went red, but not on the planted raw type'; \
		  cat $(SCHEMA_DART_STRICT_NC_LOG); exit 1; }
	@grep -m1 'StrictAnalyzePlant.dart.*strict_raw_type' $(SCHEMA_DART_STRICT_NC_LOG)
	@echo 'negative control: the planted raw type reds the strict analyzer by file and rule'
