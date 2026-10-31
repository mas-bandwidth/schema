# make/moo-conformance.mk — the MOO leg's conformance build and its control
# (test/conformance/README.md; docs/PORT-MOO.md, THE LAYERS, layer 5).
# Included by the Makefile's wildcard include; a dashed stem, so the port
# registry never probes it as a --lang (Makefile, SCHEMA_PORTS). make/moo.mk
# registers build/conformance-moo on CONFORMANCE_LEGS; this file builds it.
#
# build/conformance-moo is the manifest's units generated for moo, one
# directory per unit. EVERY unit is generated: a unit the target refuses is
# recorded as build/conformance-moo/<unit>.refused, holding the refusal, and
# its one line is printed — never dropped in silence — and the driver answers
# every case naming it absent (docs/PORTING.md I4). The schema_leg_moo tag is
# the target's until the port joins the builtins (compiler/target_moo.go).
#
# The leg's driver (test/conformance/moo/driver) stages the manifest and the
# fixtures as moo source per surface under build/conformance-moo-driver/, apart
# from this tree because this recipe wipes it.
MOO_CONFORMANCE_MANIFEST := testdata/conformance/tables/MANIFEST.txt
# Every .schema file the manifest's units name: a unit path is a file or a
# directory of them.
MOO_CONFORMANCE_UNIT_PATHS := $(shell [ -f $(MOO_CONFORMANCE_MANIFEST) ] && awk '/^unit[ \t]/ { for (i = 3; i <= NF; i++) print $$i }' $(MOO_CONFORMANCE_MANIFEST))
MOO_CONFORMANCE_SCHEMAS := $(foreach p,$(MOO_CONFORMANCE_UNIT_PATHS),$(if $(filter %.schema,$(p)),$(p),$(wildcard $(p)/*.schema)))

# the moo runtimes are //go:embed'ded into the binary, which GO_SOURCES (*.go
# only) does not see: a changed runtime file rebuilds it
build/schema-moo: $(GO_SOURCES) $(wildcard internal/codegen/moo*/runtime/*)
	@mkdir -p build
	go build -tags schema_leg_moo -o $@ ./cmd/schema

build/conformance-moo: build/schema-moo $(MOO_CONFORMANCE_MANIFEST) make/moo-conformance.mk \
	$(MOO_CONFORMANCE_SCHEMAS) $(wildcard internal/codegen/moo*/runtime/*)
	@rm -rf $@ && mkdir -p $@
	@grep '^unit[[:space:]]' $(MOO_CONFORMANCE_MANIFEST) | { carried=0; refused=0; \
		while read -r _ key paths; do \
			if ./build/schema-moo generate --lang moo --out $@/$$key $$paths >$@/$$key.log 2>&1; then \
				rm -f $@/$$key.log; carried=$$((carried+1)); \
			else \
				rm -rf $@/$$key; mv $@/$$key.log $@/$$key.refused; refused=$$((refused+1)); \
				why=$$(grep -m1 '^schema:' $@/$$key.refused || tail -n 1 $@/$$key.refused); \
				echo "moo refuses $$key: $$why"; \
			fi; \
		done; \
		echo "conformance-moo: $$carried units generated, $$refused refused (each recorded as $@/<unit>.refused)"; }

# THE LEG'S CONFORMANCE NEGATIVE CONTROLS (docs/PORTING.md I2 and I11). Both
# sabotage the EMITTER: a sed over a copy of one runtime file the table emitter
# embeds, checked to have matched, `go build -overlay` into a private compiler,
# the units regenerated, and the harness required red on exactly the named
# surfaces with the neighbours green. test/conformance/moo/negative-control
# holds both and says what each one requires.
#
# conformance-negative-control-moo       the load report's unknown counter
#                                        (report and retain red, wire green)
# conformance-negative-control-moo-walk  the text walk's field-key index
#                                        (I11: json-read and json-hostile red,
#                                        json-write and wire green)
.PHONY: conformance-negative-control-moo conformance-negative-control-moo-walk
conformance-negative-control-moo: toolchain-moo build/conformance-harness
	sh test/conformance/moo/negative-control report

conformance-negative-control-moo-walk: toolchain-moo build/conformance-harness
	sh test/conformance/moo/negative-control walk
