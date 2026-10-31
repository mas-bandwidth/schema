# make/schema-squirrel-generated-lint-gate.mk — the Squirrel port's strict
# lint of its GENERATED code, the gate every main port carries on its own
# output (Go golangci-lint, Dart analyze --fatal-infos, Elixir mix format and
# credo --strict, C clang-format, Rust clippy -D warnings, JS eslint). The
# Makefile's wildcard include pulls this in.
#
# WHAT IT HOLDS (make/squirrel-lint.sh): every generated .nut compiles under
# the pinned interpreter (make/squirrel.mk's dist-squirrel, `sq -c`, compile
# only) with exit 0 and no output — Squirrel 3.2's compiler has no warning
# class, so zero warnings is zero output — and holds the canonical text the
# emitters write (LF, no tab, no trailing whitespace, one final newline, no
# blank-line runs). No Squirrel formatter or linter is pinnable: upstream ships
# none, so the format half is those rules, checked here rather than invented
# as a tool.
#
# THE CORPUS IS EVERY GENERATED TREE THE PORT HAS:
#   * the committed packet trees, generated/squirrel (examples/) and
#     generated/squirrel-ludicrous (examples128/), read where they are;
#   * the table corpora the Makefile's generate targets read (SCHEMAS_TABLES*
#     directories and examples-wide/), regenerated into build/ at this head;
#   * every conformance unit of the tables manifest (build/conformance-squirrel,
#     make/squirrel.mk), the units the conformance leg runs.
# A corpus directory the target refuses as a whole is listed by name with its
# refusal (the refusal is the port's answer for it, docs/PORT-SQUIRREL.md, THE
# REGISTER); what it does generate is linted. It is seconds of work, so it runs
# on the pull request: ci-full.yml's conformance (squirrel) row builds it
# through test/conformance/squirrel/ci.json, and `make test-squirrel` runs it.

SQUIRREL_LINT_DIR    := build/schema-squirrel-generated-lint-gate
SQUIRREL_LINT_NC_DIR := build/schema-squirrel-generated-lint-gate-negative-control
SQUIRREL_LINT_TABLE_CORPORA := tables/examples tables/pointers tables/block \
	tables/blockhome tables/messages tables/stream tables/blobs tables/scalars \
	tables/maps tables/lists tables/arms tables/backend tables/vocab tables/vocab9 \
	examples-wide

$(SQUIRREL_LINT_DIR)/tables/.stamp: bin/schema make/schema-squirrel-generated-lint-gate.mk \
		$(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) \
		$(SCHEMAS_TABLES_MESSAGES) $(SCHEMAS_TABLES_BLOBS) $(SCHEMAS_TABLES_SCALARS) \
		$(SCHEMAS_TABLES_MAPS) $(SCHEMAS_TABLES_LISTS) $(SCHEMAS_TABLES_ARMS) \
		$(SCHEMAS_TABLES_BACKEND) $(SCHEMAS_TABLES_VOCAB) $(SCHEMAS_TABLES_VOCAB9) \
		$(SCHEMAS_WIDE)
	@rm -rf $(dir $@) && mkdir -p $(dir $@)
	@for d in $(SQUIRREL_LINT_TABLE_CORPORA); do \
		n=$$(printf '%s' "$$d" | tr / -); \
		if ! ./bin/schema generate --lang squirrel --out $(dir $@)$$n $$d > $(dir $@)$$n.log 2>&1; then \
			rm -rf $(dir $@)$$n; \
			echo "schema-squirrel-generated-lint-gate: $$d is refused by the target, not linted: $$(head -1 $(dir $@)$$n.log)"; \
		fi; rm -f $(dir $@)$$n.log; \
	done
	@touch $@

.PHONY: schema-squirrel-generated-lint-gate
schema-squirrel-generated-lint-gate: dist-squirrel generated/squirrel/.stamp \
		generated/squirrel-ludicrous/.stamp $(SQUIRREL_LINT_DIR)/tables/.stamp \
		build/conformance-squirrel
	sh make/squirrel-lint.sh "$(SQ)" generated/squirrel generated/squirrel-ludicrous \
		$(SQUIRREL_LINT_DIR)/tables build/conformance-squirrel

# PROOF THE GATE FIRES, AND ON EXACTLY ITS ROWS. A copy of the committed packet
# tree must lint clean first (the plant, not the copy, is what goes red); then
# three plants in three REAL generated files, one per kind of finding: a
# statement the compiler refuses (Types.nut), a trailing space (Enums.nut) and
# a blank-line run (Constants.nut). The gate must go red with exactly three
# findings, each naming its file and its rule, and nothing else.
.PHONY: schema-squirrel-generated-lint-gate-negative-control
schema-squirrel-generated-lint-gate-negative-control: dist-squirrel generated/squirrel/.stamp
	@rm -rf $(SQUIRREL_LINT_NC_DIR) && mkdir -p $(SQUIRREL_LINT_NC_DIR)
	@cp -R generated/squirrel $(SQUIRREL_LINT_NC_DIR)/squirrel
	@rm -f $(SQUIRREL_LINT_NC_DIR)/squirrel/.stamp
	@sh make/squirrel-lint.sh "$(SQ)" $(SQUIRREL_LINT_NC_DIR)/squirrel > $(SQUIRREL_LINT_NC_DIR)/clean.log 2>&1 || \
		{ echo 'NEGATIVE CONTROL FAILED: the unplanted copy of generated/squirrel is already red, so a red below proves nothing'; \
		  cat $(SQUIRREL_LINT_NC_DIR)/clean.log; exit 1; }
	@printf '%s\n' '// PLANTED by make/schema-squirrel-generated-lint-gate.mk: the compiler must refuse this.' \
		'local schemaLintPlant = ;' >> $(SQUIRREL_LINT_NC_DIR)/squirrel/Types.nut
	@printf '%s\n' '// PLANTED by make/schema-squirrel-generated-lint-gate.mk: a trailing space. ' \
		>> $(SQUIRREL_LINT_NC_DIR)/squirrel/Enums.nut
	@printf '%s\n' '' '' '// PLANTED by make/schema-squirrel-generated-lint-gate.mk: two blank lines above.' \
		>> $(SQUIRREL_LINT_NC_DIR)/squirrel/Constants.nut
	@if sh make/squirrel-lint.sh "$(SQ)" $(SQUIRREL_LINT_NC_DIR)/squirrel > $(SQUIRREL_LINT_NC_DIR)/planted.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the Squirrel lint gate stayed green with three planted defects'; \
		cat $(SQUIRREL_LINT_NC_DIR)/planted.log; exit 1; \
	fi
	@grep -E '^(COMPILE|FORMAT) ' $(SQUIRREL_LINT_NC_DIR)/planted.log > $(SQUIRREL_LINT_NC_DIR)/findings.log || true
	@n=$$(wc -l < $(SQUIRREL_LINT_NC_DIR)/findings.log | tr -d ' '); \
	ok=1; \
	grep -q '^COMPILE $(SQUIRREL_LINT_NC_DIR)/squirrel/Types.nut: ' $(SQUIRREL_LINT_NC_DIR)/findings.log || ok=0; \
	grep -q '^FORMAT $(SQUIRREL_LINT_NC_DIR)/squirrel/Enums.nut:[0-9]*: trailing whitespace$$' $(SQUIRREL_LINT_NC_DIR)/findings.log || ok=0; \
	grep -q '^FORMAT $(SQUIRREL_LINT_NC_DIR)/squirrel/Constants.nut:[0-9]*: two blank lines in a row$$' $(SQUIRREL_LINT_NC_DIR)/findings.log || ok=0; \
	if [ "$$n" != 3 ] || [ "$$ok" != 1 ]; then \
		echo "NEGATIVE CONTROL FAILED: the gate went red, but not on exactly the three planted rows ($$n finding(s))"; \
		cat $(SQUIRREL_LINT_NC_DIR)/planted.log; exit 1; \
	fi
	@cat $(SQUIRREL_LINT_NC_DIR)/findings.log
	@echo 'negative control: a refused statement, a trailing space and a blank-line run planted in real generated files red the Squirrel lint gate by file and rule, and nothing else'

# The port's normal unit target includes the gate, as test-dart includes its
# analyze and format pass.
test-squirrel: schema-squirrel-generated-lint-gate
