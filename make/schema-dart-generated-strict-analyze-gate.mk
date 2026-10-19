# make/schema-dart-generated-strict-analyze-gate.mk — the strict Dart
# analyzer lens over the generated Dart (ideas roadmap group
# schema-per-language-lenses; Glenn, 2026-10-18). This is a card-scoped gate,
# not a language port, so it registers itself in TABLES_REFUSE_PORTS and the
# wide-scalar port registry (tables-ports-refuse-wide-scalars) skips it;
# make/dart.mk includes it by path, and the root Makefile's `include
# $(wildcard make/*.mk)` loads it too; the guard keeps a double include from
# redefining the targets.
ifndef SCHEMA_DART_STRICT_GATE_INCLUDED
SCHEMA_DART_STRICT_GATE_INCLUDED := 1
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
# THE LINT SET. It carries the correctness half of the Dart lint registry,
# including the three lints the first attempt left out because they fired:
# `avoid_dynamic_calls` (the table backend's type-agnostic `(dynamic v) =>
# v.field` view/descriptor closures), `literal_only_boolean_expressions` (the
# fixed form's structurally-always-true nested/count array guards) and
# `unrelated_type_equality_checks` (the fixed form's omit-if-default test on a
# nested message, which compares a record to int and is a REAL emitter bug —
# see the triage table in the card report and PATHS-PROPOSED
# internal/codegen/darttable). The fast corpus is the packet output, which is
# clean under all three; the table half reports them until PATHS allows the
# darttable fix. Nothing is disabled in analysis_options.yaml.
#
# Style-only lints stay out for the same reason as before: `dart format` owns
# them, and `non_constant_identifier_names` cannot be satisfied by
# schema-declared names (`BuildVersion`).

DART_STRICT_SDK_PIN ?= 3.13.2

# Not a language port: the wide-scalar refusal gate must not run `schema
# generate --lang schema-dart-generated-strict-analyze-gate` on this card's
# gate (it would stop with "not implemented" and red a file never meant to be
# a language).
TABLES_REFUSE_PORTS += schema-dart-generated-strict-analyze-gate

SCHEMA_DART_STRICT_DIR      := build/schema-dart-strict-analyze
SCHEMA_DART_STRICT_NC_DIR   := build/schema-dart-strict-analyze-negative
SCHEMA_DART_STRICT_LOG      := build/schema-dart-strict-analyze/analyze.log
SCHEMA_DART_STRICT_NC_LOG   := build/schema-dart-strict-analyze-negative/analyze.log
# The fast corpus is the packet port's own output, including the packet-defaults
# unit make/dart.mk generates. The table backend's trees (the paired unit, the
# build-time tables corpus, and the unit-view corpus) are the nightly half.
SCHEMA_DART_STRICT_FAST_TREES := generated/dart generated/dart-ludicrous generated/bench/dart build/packet-defaults/dart
SCHEMA_DART_STRICT_TABLE_TREES := generated/bench/paired/dart build/tables-generated-dart build/dart-view

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
    - avoid_dynamic_calls
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
    - literal_only_boolean_expressions
    - no_duplicate_case_values
    - null_check_on_nullable_type_parameter
    - prefer_generic_function_type_aliases
    - prefer_is_not_empty
    - prefer_typing_uninitialized_variables
    - test_types_in_equals
    - throw_in_finally
    - unnecessary_overrides
    - unnecessary_statements
    - unrelated_type_equality_checks
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

# THE UNIT-VIEW CORPUS is generated into build/dart-view by make/dart.mk's
# tables-dart-view too. The strict gate needs only the generated libraries, not
# the compiled listing programs, so its stamp generates the same units without
# compiling them; running tables-dart-view later removes the tree and the next
# strict run regenerates it.
build/dart-view/.stamp: bin/schema
	@rm -rf build/dart-view && mkdir -p build/dart-view
	# tables/scalars is the known form-3 gap make/dart.mk already carries beside
	# the tree: its fixed library imports an Int128.dart the packet emitter does
	# not write for a table-only 128-bit unit. It is not a strict-analyzer
	# finding, so it is left out of this corpus rather than reported as one.
	@set -e; for entry in $(VIEW_CORPUS); do \
		dir=$${entry%%:*}; \
		if [ "$$dir" = scalars ]; then continue; fi; \
		source=tables/$$dir; if [ "$$dir" = wide ]; then source=examples-wide; fi; \
		./bin/schema generate --lang dart --out build/dart-view/$$dir $$source; \
	done
	@touch $@

.PHONY: schema-dart-generated-strict-analyze-gate
schema-dart-generated-strict-analyze-gate: toolchain-dart \
		generated/dart/.stamp generated/dart-ludicrous/.stamp \
		generated/bench/dart/.stamp build/packet-defaults/dart/.stamp
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
schema-dart-generated-strict-analyze-gate-tables: toolchain-dart \
		generated/bench/paired/dart/.stamp build/tables-generated-dart/.stamp \
		build/dart-view/.stamp
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
		generated/bench/dart/.stamp build/packet-defaults/dart/.stamp \
		generated/bench/paired/dart/.stamp \
		build/tables-generated-dart/.stamp build/dart-view/.stamp
	@$(SCHEMA_DART_STRICT_PROBE)
	@$(call SCHEMA_DART_STRICT_COPY,$(SCHEMA_DART_STRICT_DIR),$(SCHEMA_DART_STRICT_FAST_TREES) $(SCHEMA_DART_STRICT_TABLE_TREES))
	@$(DART) analyze --fatal-infos --fatal-warnings $(SCHEMA_DART_STRICT_DIR) \
		> $(SCHEMA_DART_STRICT_LOG) 2>&1 || { cat $(SCHEMA_DART_STRICT_LOG); exit 1; }
	@cat $(SCHEMA_DART_STRICT_LOG)
	@echo 'schema Dart strict analyze gate (all): every generated tree is clean under the strict modes and lint set'

# PROOF THE GATE FIRES. The control APPENDS one bare generic to a COPY of a
# REAL generated file the gate covers (generated/dart/Types.dart, its generated
# header intact); strict-raw-types must reject it and the log must name that
# file and the rule. A gate that stayed green here would be reading the wrong
# directory or not reading analysis_options.yaml at all, and planting in a real
# artifact means a default exclusion or a tool refusal cannot fake the red.
.PHONY: schema-dart-generated-strict-analyze-gate-negative-control
schema-dart-generated-strict-analyze-gate-negative-control: toolchain-dart \
		generated/dart/.stamp generated/bench/paired/dart/.stamp
	@$(SCHEMA_DART_STRICT_PROBE)
	@$(call SCHEMA_DART_STRICT_COPY,$(SCHEMA_DART_STRICT_NC_DIR),generated/dart generated/bench/paired/dart)
	@printf '%s\n' '' '// PLANTED by make/schema-dart-generated-strict-analyze-gate.mk: a bare generic must be fatal.' \
		'List schemaStrictAnalyzePlant = [];' >> $(SCHEMA_DART_STRICT_NC_DIR)/generated/dart/Types.dart
	@if $(DART) analyze --fatal-infos --fatal-warnings $(SCHEMA_DART_STRICT_NC_DIR) \
			> $(SCHEMA_DART_STRICT_NC_LOG) 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the strict-analyze gate stayed green with a planted raw type'; \
		cat $(SCHEMA_DART_STRICT_NC_LOG); exit 1; \
	fi
	@grep -q 'generated/dart/Types.dart.*strict_raw_type' $(SCHEMA_DART_STRICT_NC_LOG) || \
		{ echo 'NEGATIVE CONTROL FAILED: the gate went red, but not on the planted raw type'; \
		  cat $(SCHEMA_DART_STRICT_NC_LOG); exit 1; }
	@grep -m1 'generated/dart/Types.dart.*strict_raw_type' $(SCHEMA_DART_STRICT_NC_LOG)
	@echo 'negative control: the planted raw type in a real generated file reds the strict analyzer by file and rule'

endif
