# make/schema-luau-generated-analyze-strict-gate.mk — THE LUAU STRICT-ANALYZE
# GATE (schema-per-language-lenses). Schema generates the same algorithm in many
# languages, so the language's strictest checker over the generated output is a
# separate lens on bugs. This file runs luau-analyze --mode=strict over every
# module the Luau leg generates for the test-schema corpus: the type surface,
# the table surface (readers and writers), and the emitted runtime modules.
#
# THE CORPUS is the Luau leg's own generate target, build/conformance-luau
# (make/luau.mk), which reads testdata/conformance/tables/MANIFEST.txt and the
# schema paths it names — no second corpus is invented here. mapdemo is
# recorded by that target as a refusal (its schema file collides with the
# runtime's text.luau) and is not analyzed.
#
# THE TOOL is Luau 0.741, the interpreter pin make/luau.mk already owns; this
# file adds no second version. run `make dist-luau` (or set LUAU_ANALYZE) when
# the pinned release is not unpacked.
#
# SUPPRESSION, and why (the only one): the generator emits every read holder
# (BITS, BIG, BOOL, STR, WIDE) and the UTF-8 validator into every module,
# whether or not a given schema's reads use them. Those are LocalUnused and
# FunctionUnused lint diagnostics — no runtime effect and not type errors —
# so .luaurc turns exactly those two lints off for the generated tree. Every
# other diagnostic, of any kind, fails the gate; there is no blanket nolint.
#
# The gate fails on a non-zero analyzer status OR on any output line, because
# luau-analyze exits 0 for lint warnings it prints.

LUAU_STRICT_TREE    := build/conformance-luau
LUAU_STRICT_OUT     := build/luau-analyze-strict
LUAU_STRICT_LINT    := {"lint":{"LocalUnused":false,"FunctionUnused":false}}

.PHONY: luau-generated-analyze-strict luau-generated-analyze-strict-negative-control

luau-generated-analyze-strict: $(LUAU_STRICT_TREE)
	@if [ ! -x "$(LUAU_ANALYZE)" ]; then \
		echo "tool missing: luau-analyze ($(LUAU_ANALYZE)); run make dist-luau or set LUAU_ANALYZE"; \
		exit 1; \
	fi
	@mkdir -p $(LUAU_STRICT_OUT)
	@printf '%s\n' '$(LUAU_STRICT_LINT)' > $(LUAU_STRICT_TREE)/.luaurc
	@n=$$(cd $(LUAU_STRICT_TREE) && find . -name '*.luau' | wc -l | tr -d ' '); \
	status=0; \
	( cd $(LUAU_STRICT_TREE) && $(LUAU_ANALYZE) --mode=strict --formatter=gnu $$(find . -name '*.luau' | sort) ) > $(LUAU_STRICT_OUT)/log 2>&1 || status=$$?; \
	if [ $$status -ne 0 ] || [ -s $(LUAU_STRICT_OUT)/log ]; then \
		echo "luau-generated-analyze-strict: FAILED over $$n generated Luau modules"; \
		cat $(LUAU_STRICT_OUT)/log; \
		exit 1; \
	fi; \
	echo "luau-generated-analyze-strict: PASS: luau-analyze --mode=strict, $$n generated Luau modules, no findings"

# THE NEGATIVE CONTROL. The gate is only a gate if a strict type error in the
# generated tree makes it red and names the plant. A scratch copy of the tree
# is first analyzed green, then a planted_strict_gate.luau module gains a typed
# local that cannot hold its literal; the analyzer must refuse and name it.
luau-generated-analyze-strict-negative-control: $(LUAU_STRICT_TREE)
	@if [ ! -x "$(LUAU_ANALYZE)" ]; then \
		echo "tool missing: luau-analyze ($(LUAU_ANALYZE)); run make dist-luau or set LUAU_ANALYZE"; \
		exit 1; \
	fi
	@out=$(LUAU_STRICT_OUT)-control; \
	rm -rf $$out; mkdir -p $$out; \
	cp -a $(LUAU_STRICT_TREE) $$out/tree; \
	printf '%s\n' '$(LUAU_STRICT_LINT)' > $$out/tree/.luaurc; \
	status=0; \
	( cd $$out/tree && $(LUAU_ANALYZE) --mode=strict --formatter=gnu $$(find . -name '*.luau' | sort) ) > $$out/green.log 2>&1 || status=$$?; \
	if [ $$status -ne 0 ] || [ -s $$out/green.log ]; then \
		echo "luau-generated-analyze-strict negative control: the unplanted copy is not green"; \
		cat $$out/green.log; exit 1; \
	fi; \
	printf '%s\n' '--!strict' 'local planted_strict_gate: number = "planted-bad-pattern"' > $$out/tree/planted_strict_gate.luau; \
	status=0; \
	( cd $$out/tree && $(LUAU_ANALYZE) --mode=strict --formatter=gnu $$(find . -name '*.luau' | sort) ) > $$out/red.log 2>&1 || status=$$?; \
	if [ $$status -eq 0 ]; then \
		echo "luau-generated-analyze-strict negative control: the plant left the analyzer green"; \
		cat $$out/red.log; exit 1; \
	fi; \
	if ! grep -q 'planted_strict_gate' $$out/red.log; then \
		echo "luau-generated-analyze-strict negative control: the red does not name the plant"; \
		cat $$out/red.log; exit 1; \
	fi; \
	echo "luau-generated-analyze-strict negative control: the strict type plant in planted_strict_gate.luau is caught by name"
