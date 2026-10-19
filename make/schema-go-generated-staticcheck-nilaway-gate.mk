# schema: support fragment, not a language backend
# THE GO LENS: staticcheck + Uber NilAway over the GENERATED Go output for the
# test-schema corpus (schema-per-language-lenses). Schema generates one
# algorithm in many languages; each language's strictest checker over the
# generated output is a separate lens on bugs. This card owns the Go lens and
# nothing else.
#
# THE CORPUS is the generated Go the Makefile's own targets write, never a
# second corpus: `build/tables-generated-go/*` (every reader, writer, cook,
# block, message, card and wide unit that `build/tables-generated-go/.stamp`
# generates from tables/..., test/tables/*.schema, examples-wide and
# test/packet-* sources) and the committed packet units under generated/go and
# generated/go-ludicrous, copied into the lens's scratch tree so nothing
# tracked is edited. Every unit is vetted as its own package/module, exactly as
# Go resolves it.
#
# THE TOOLS are pinned, and installed from their official module paths
# (honnef.co/go/tools, go.uber.org/nilaway) through the fleet play on the bench;
# a host without them still runs the same pinned versions through `go run`.
# The target PROBES by name and says `tool missing: <name>` rather than
# pretending the lens ran (the gate-with-a-blade rule, issue #599).
STATICCHECK_VERSION ?= v0.8.1
NILAWAY_VERSION     ?= v0.0.0-20260918162853-acb8859b9031
SCHEMA_GO_LENS_DIR  ?= build/schema-go-staticcheck-nilaway

# staticcheck_run / nilaway_run resolve to the installed binary when the fleet
# play has put one on PATH, else to the pinned official module through go run.
staticcheck_run = $(if $(shell command -v staticcheck 2>/dev/null),staticcheck,go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION))
nilaway_run     = $(if $(shell command -v nilaway 2>/dev/null),nilaway,go run go.uber.org/nilaway/cmd/nilaway@$(NILAWAY_VERSION))

.PHONY: schema-go-generated-staticcheck-nilaway-gate
schema-go-generated-staticcheck-nilaway-gate: build/tables-generated-go/.stamp $(SERIALIZE_GO_RUNTIME)/go.mod
	@command -v staticcheck >/dev/null 2>&1 || echo "tool missing: staticcheck on PATH; using pinned $(STATICCHECK_VERSION) through go run"
	@command -v nilaway >/dev/null 2>&1 || echo "tool missing: nilaway on PATH; using pinned $(NILAWAY_VERSION) through go run"
	@rm -rf $(SCHEMA_GO_LENS_DIR) && mkdir -p $(SCHEMA_GO_LENS_DIR)
	@for d in build/tables-generated-go/*/; do \
		[ -f "$$d/go.mod" ] || continue; \
		name=$$(basename $$d); \
		cp -R "$$d" "$(SCHEMA_GO_LENS_DIR)/$$name"; \
	done
	@cp -R generated/go $(SCHEMA_GO_LENS_DIR)/packet-example
	@cp -R generated/go-ludicrous $(SCHEMA_GO_LENS_DIR)/packet-ludicrous
	@set -e; S="$(CURDIR)"; R="$(CURDIR)/$(SERIALIZE_GO_RUNTIME)"; \
	for d in $(SCHEMA_GO_LENS_DIR)/*/; do \
		[ -f "$$d/go.mod" ] || continue; \
		m=$$(awk '/^module /{print $$2; exit}' "$$d/go.mod"); \
		[ -n "$$m" ] || { echo "LENS FAILED: $$d/go.mod names no module"; exit 1; }; \
		printf 'module %s\n\ngo 1.23\n\nrequire (\n\tgithub.com/mas-bandwidth/schema/v2 v2.0.0\n\tgithub.com/mas-bandwidth/serialize.go v0.0.0\n)\n\nreplace github.com/mas-bandwidth/schema/v2 => %s\n\nreplace github.com/mas-bandwidth/serialize.go => %s\n' "$$m" "$$S" "$$R" > "$$d/go.mod"; \
		(cd "$$d" && GOFLAGS=-mod=mod go mod tidy >/dev/null 2>&1); \
		echo "staticcheck: $$d"; \
		(cd "$$d" && $(call staticcheck_run) ./...); \
		echo "nilaway: $$d"; \
		(cd "$$d" && $(call nilaway_run) ./...); \
	done
	@echo "schema-go lens: staticcheck $(STATICCHECK_VERSION) and nilaway $(NILAWAY_VERSION) are clean over the generated Go corpus"

# THE PLANTED-PATTERN CONTROL. A clean scratch package passes both tools; then
# one known-bad pattern per tool is planted — a nil dereference NilAway must
# name, and an `&*p` round trip staticcheck must name (SA4001) — and each tool
# must exit non-zero naming its plant. The plant is written into a scratch copy,
# never into generated output, so no tracked file and no generator is touched.
.PHONY: schema-go-generated-staticcheck-nilaway-gate-negative-control
schema-go-generated-staticcheck-nilaway-gate-negative-control:
	@rm -rf build/schema-go-lens-nc && mkdir -p build/schema-go-lens-nc/clean build/schema-go-lens-nc/planted
	@printf 'module lensclean\n\ngo 1.23\n' > build/schema-go-lens-nc/clean/go.mod
	@printf 'package clean\n\nfunc Clean(p *int) int {\n\tif p == nil {\n\t\treturn 0\n\t}\n\treturn *p\n}\n' > build/schema-go-lens-nc/clean/clean.go
	@printf 'module lensplant\n\ngo 1.23\n' > build/schema-go-lens-nc/planted/go.mod
	@printf 'package planted\n\nfunc PlantedNil() int {\n\tvar p *int\n\treturn *p\n}\n\nfunc PlantedDerefRoundTrip(p *int) *int {\n\treturn &*p\n}\n' > build/schema-go-lens-nc/planted/planted.go
	@set -e; \
	( cd build/schema-go-lens-nc/clean && $(call staticcheck_run) ./... ) > build/schema-go-lens-nc/clean-sc.log 2>&1 || { echo "NEGATIVE CONTROL FAILED: staticcheck is red on the clean package"; cat build/schema-go-lens-nc/clean-sc.log; exit 1; }; \
	( cd build/schema-go-lens-nc/clean && $(call nilaway_run) ./... ) > build/schema-go-lens-nc/clean-nil.log 2>&1 || { echo "NEGATIVE CONTROL FAILED: nilaway is red on the clean package"; cat build/schema-go-lens-nc/clean-nil.log; exit 1; }
	@if ( cd build/schema-go-lens-nc/planted && $(call staticcheck_run) ./... ) > build/schema-go-lens-nc/red-sc.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: staticcheck stayed green on the planted &*p"; exit 1; \
	fi
	@grep -q 'SA4001' build/schema-go-lens-nc/red-sc.log || { echo "NEGATIVE CONTROL FAILED: staticcheck red without naming SA4001"; cat build/schema-go-lens-nc/red-sc.log; exit 1; }
	@if ( cd build/schema-go-lens-nc/planted && $(call nilaway_run) ./... ) > build/schema-go-lens-nc/red-nil.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: nilaway stayed green on the planted nil dereference"; exit 1; \
	fi
	@grep -q 'potential nil panic\|Potential nil panic' build/schema-go-lens-nc/red-nil.log || { echo "NEGATIVE CONTROL FAILED: nilaway red without naming a nil panic"; cat build/schema-go-lens-nc/red-nil.log; exit 1; }
	@grep -q 'planted.go:5' build/schema-go-lens-nc/red-nil.log || { echo "NEGATIVE CONTROL FAILED: nilaway red without naming the planted dereference line"; cat build/schema-go-lens-nc/red-nil.log; exit 1; }
	@echo "schema-go lens negative control: the clean package is green and the planted nil and &*p patterns red both tools by name"
