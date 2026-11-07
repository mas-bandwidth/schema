# make/schema-lobster-generated-strict-gate.mk — the strict lens over the
# generated Lobster (docs/PORTING.md J4; the per-language lenses beside
# make/schema-go-generated-golangci-lint-gate.mk and
# make/schema-dart-generated-strict-analyze-gate.mk). A card-scoped gate, not a
# language port: its stem has a dash, so the port registry (SCHEMA_PORTS) never
# probes it as a --lang. The root Makefile's `include $(wildcard make/*.mk)`
# loads it; the guard keeps a double include from redefining the targets.
ifndef SCHEMA_LOBSTER_STRICT_GATE_INCLUDED
SCHEMA_LOBSTER_STRICT_GATE_INCLUDED := 1
#
# LOBSTER HAS NO SEPARATE LINTER. Its compiler is the strictest checker the
# language has: it type-checks every function of every module a program
# imports, called or not, and it WARNS on what a linter would (an unused
# variable, a `var` that is never reassigned and must be a `let`, ...). The
# warnings go to stdout and the exit status stays 0, so a gate that read only
# the status would be green over every one of them. So the gate is: EVERY
# GENERATED UNIT COMPILES UNDER THE PINNED TOOLCHAIN AND THE COMPILER PRINTS
# NOTHING. Each unit gets a main that imports every module the unit holds (the
# generated modules and the runtime emitted beside them, docs/PORT-LOBSTER.md,
# THE RUNTIME LAYOUT), compiled with `--pak`, which runs the whole front end
# and writes bytecode instead of running it; any line of output, a warning or
# an error, is a red.
#
# THE CORPUS is every unit the leg generates from the tree: examples/,
# examples128/ and examples-wide/ (the packet type surface and the type wire),
# and every unit of the conformance manifest (testdata/conformance/tables/
# MANIFEST.txt, the table surface: load, save, text, block, fixed form), each
# generated fresh from bin/schema into build/, never the committed tree. A unit
# the target refuses is a refusal by name, recorded and counted, not a unit
# this gate could compile. Nothing is pruned and no warning is filtered.
#
# Measured: the 36 units compile in about four seconds on the studio.

SCHEMA_LOBSTER_STRICT_DIR    := build/schema-lobster-strict
SCHEMA_LOBSTER_STRICT_NC_DIR := build/schema-lobster-strict-negative
SCHEMA_LOBSTER_STRICT_TREES  := examples examples128 examples-wide
SCHEMA_LOBSTER_STRICT_MAIN   := schema_strict_main.lobster

# The toolchain, resolved to an absolute path because each unit compiles from
# its own directory (a Lobster `import .<module>` resolves against the importing
# file's directory). A pin that does not resolve is a tool missing, never a pass.
define SCHEMA_LOBSTER_STRICT_PROBE
lob=$$(command -v "$(LOBSTER)" 2>/dev/null) || { echo "tool missing: lobster ($(LOBSTER)); run make dist-lobster"; exit 1; }; \
case "$$lob" in /*) ;; *) lob="$$PWD/$$lob" ;; esac
endef

# strict_unit <dir> compiles one unit: a main of one `import .<module>` per
# module in the directory, `--pak`, and the compiler's whole output kept in
# <dir>.log. It fails when the compiler exits nonzero OR prints anything.
define SCHEMA_LOBSTER_STRICT_FN
strict_unit() { \
	u="$$1"; rm -f "$$u/$(SCHEMA_LOBSTER_STRICT_MAIN)" "$$u/default.lpak"; \
	for f in "$$u"/*.lobster; do b="$${f##*/}"; echo "import .$${b%.lobster}"; done > "$$u.main" || return 1; \
	mv "$$u.main" "$$u/$(SCHEMA_LOBSTER_STRICT_MAIN)" || return 1; \
	( cd "$$u" && "$$lob" --pak $(SCHEMA_LOBSTER_STRICT_MAIN) ) > "$$u.log" 2>&1; st=$$?; \
	rm -f "$$u/default.lpak"; \
	[ "$$st" -eq 0 ] && [ ! -s "$$u.log" ]; \
}
endef

.PHONY: schema-lobster-generated-strict-gate tables-lobster-clean
# tables-lobster-clean is the register's name for this gate (docs/PORTING.md
# J4, `tables-<lang>-clean`).
tables-lobster-clean: schema-lobster-generated-strict-gate
schema-lobster-generated-strict-gate: toolchain-lobster bin/schema
	@$(SCHEMA_LOBSTER_STRICT_PROBE); $(SCHEMA_LOBSTER_STRICT_FN); \
	rm -rf $(SCHEMA_LOBSTER_STRICT_DIR) && mkdir -p $(SCHEMA_LOBSTER_STRICT_DIR)/manifest || exit 1; \
	for t in $(SCHEMA_LOBSTER_STRICT_TREES); do \
		./bin/schema generate --lang lobster --out $(SCHEMA_LOBSTER_STRICT_DIR)/$$t $$t || exit 1; \
	done; \
	grep '^unit ' $(LOBSTER_MANIFEST) | while read -r _ key paths; do \
		if ! ./bin/schema generate --lang lobster --out $(SCHEMA_LOBSTER_STRICT_DIR)/manifest/$$key $$paths \
				> $(SCHEMA_LOBSTER_STRICT_DIR)/manifest/$$key.refused 2>&1; then \
			echo "lobster refuses $$key: $$(head -1 $(SCHEMA_LOBSTER_STRICT_DIR)/manifest/$$key.refused)"; \
			rm -rf $(SCHEMA_LOBSTER_STRICT_DIR)/manifest/$$key; \
		else \
			rm -f $(SCHEMA_LOBSTER_STRICT_DIR)/manifest/$$key.refused; \
		fi; \
	done || exit 1; \
	refused=$$(ls $(SCHEMA_LOBSTER_STRICT_DIR)/manifest/*.refused 2>/dev/null | wc -l | tr -d ' '); \
	units=0; modules=0; red=0; \
	for u in $(addprefix $(SCHEMA_LOBSTER_STRICT_DIR)/,$(SCHEMA_LOBSTER_STRICT_TREES)) $(SCHEMA_LOBSTER_STRICT_DIR)/manifest/*; do \
		[ -d "$$u" ] || continue; \
		units=$$((units+1)); modules=$$((modules+$$(ls "$$u"/*.lobster | wc -l))); \
		strict_unit "$$u" || { red=$$((red+1)); echo "== $$u: the compiler printed"; cat "$$u.log"; }; \
	done; \
	if [ "$$units" -eq 0 ]; then echo "schema-lobster-generated-strict-gate: REFUSED: no unit was generated, so there is nothing to hold"; exit 1; fi; \
	if [ "$$red" -ne 0 ]; then \
		echo "schema-lobster-generated-strict-gate: $$red of $$units generated units do not compile clean under $$lob (every line above is a warning or an error in generated Lobster)"; \
		exit 1; \
	fi; \
	echo "schema Lobster strict gate: $$units generated units ($$modules modules, $$refused manifest units refused by name) compile under $$lob with zero warnings"

# PROOF THE GATE FIRES, AND ONLY ON WHAT WAS PLANTED. The control generates the
# examples/ unit fresh, requires it clean first (a red over a dirty baseline
# proves nothing), then APPENDS one function to a COPY of a real generated
# module, Types.lobster: a `var` that is never reassigned, which the compiler
# warns must be a `let`. The gate must go red, and the compiler's output must
# be exactly ONE line, the planted variable's warning in Types.lobster. A gate
# that stayed green would be reading the status instead of the output, or
# compiling nothing.
.PHONY: schema-lobster-generated-strict-gate-negative-control
schema-lobster-generated-strict-gate-negative-control: toolchain-lobster bin/schema
	@$(SCHEMA_LOBSTER_STRICT_PROBE); $(SCHEMA_LOBSTER_STRICT_FN); \
	nc=$(SCHEMA_LOBSTER_STRICT_NC_DIR); \
	rm -rf $$nc && mkdir -p $$nc || exit 1; \
	./bin/schema generate --lang lobster --out $$nc/examples examples || exit 1; \
	strict_unit $$nc/examples || { echo "NEGATIVE CONTROL FAILED: the unplanted examples unit does not compile clean, so a red below would prove nothing:"; cat $$nc/examples.log; exit 1; }; \
	printf '%s\n' '' '// PLANTED by make/schema-lobster-generated-strict-gate.mk: a never-reassigned var must warn.' \
		'def schema_strict_plant() -> int:' \
		'    var schema_strict_planted = 1' \
		'    return schema_strict_planted' >> $$nc/examples/Types.lobster; \
	if strict_unit $$nc/examples; then echo "NEGATIVE CONTROL FAILED: the strict gate stayed green with a planted warning in Types.lobster"; exit 1; fi; \
	lines=$$(grep -c . $$nc/examples.log); \
	if [ "$$lines" -ne 1 ] || ! grep -q '^Types\.lobster([0-9]*): warning: .*schema_strict_planted' $$nc/examples.log; then \
		echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the planted line alone:"; cat $$nc/examples.log; exit 1; \
	fi; \
	cat $$nc/examples.log; \
	echo "negative control: a planted never-reassigned var in a real generated module reds the strict gate on that line alone"

endif
