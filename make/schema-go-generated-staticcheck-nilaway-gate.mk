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
# THE TOOLS are provisioned fleet-wide from their official sources. Before use,
# their embedded module versions are checked against these pins; a missing or
# mismatched PATH tool fails by name. The runner never installs tools itself.
STATICCHECK_VERSION ?= v0.8.2
NILAWAY_VERSION     ?= v0.0.0-20260918162853-acb8859b9031
SCHEMA_GO_LENS_DIR  ?= build/schema-go-staticcheck-nilaway

# Run the exact fleet-provisioned binaries after verifying their embedded module
# versions. This prevents a PATH binary from silently drifting from the pin.
staticcheck_run = staticcheck
nilaway_run     = nilaway

.PHONY: schema-go-staticcheck-tool-versions
schema-go-staticcheck-tool-versions:
	@set -e; \
	check_version() { \
		tool="$$1"; module="$$2"; want="$$3"; \
		path=$$(command -v "$$tool" 2>/dev/null) || { printf "tool missing: %s\n" "$$tool" >&2; exit 1; }; \
		actual=$$(go version -m "$$path" | awk -v module="$$module" '$$1 == "mod" && $$2 == module { print $$3; exit }'); \
		[ "$$actual" = "$$want" ] || { printf "tool version mismatch: %s expected %s, got %s\n" "$$tool" "$$want" "$${actual:-unknown}" >&2; exit 1; }; \
		printf "tool verified: %s %s\n" "$$tool" "$$actual"; \
	}; \
	check_version staticcheck honnef.co/go/tools "$(STATICCHECK_VERSION)"; \
	check_version nilaway go.uber.org/nilaway "$(NILAWAY_VERSION)"

.PHONY: schema-go-generated-staticcheck-nilaway-gate
schema-go-generated-staticcheck-nilaway-gate: schema-go-staticcheck-tool-versions build/tables-generated-go/.stamp $(SERIALIZE_GO_RUNTIME)/go.mod
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

# THE PLANTED-PATTERN CONTROL. One REAL generated unit from
# build/tables-generated-go is copied into the lens's scratch tree twice: a clean
# copy must pass both checkers, and a copy carrying the known-bad patterns must
# go red by name. This proves the lens's own per-unit loop reaches the corpus;
# the plants live only in the scratch copy, never in tracked generated output.
#
# Each checker gets a plant it really names. staticcheck does not report a bare
# nil dereference (SA5011 needs a deref after a nil check), so its plant is the
# &*p round trip it calls SA4001; nilaway's plant is the nil dereference it
# calls a potential nil panic. Asserting each check ID separately keeps a
# checker that silently stops analysing from passing on the other's red.
.PHONY: schema-go-generated-staticcheck-nilaway-gate-negative-control
schema-go-generated-staticcheck-nilaway-gate-negative-control: schema-go-staticcheck-tool-versions build/tables-generated-go/.stamp $(SERIALIZE_GO_RUNTIME)/go.mod
	@rm -rf build/schema-go-lens-nc && mkdir -p build/schema-go-lens-nc/clean/unit build/schema-go-lens-nc/planted/unit
	@cp -R build/tables-generated-go/maps/. build/schema-go-lens-nc/clean/unit/
	@cp -R build/tables-generated-go/maps/. build/schema-go-lens-nc/planted/unit/
	@printf '\nfunc PlantedNil() int {\n\tvar p *int\n\treturn *p\n}\n' >> build/schema-go-lens-nc/planted/unit/CellsCook.go
	@printf '\nfunc PlantedAddr(p *int) *int { return &*p }\n' >> build/schema-go-lens-nc/planted/unit/CellsCook.go
	@set -e; S="$(CURDIR)"; R="$(CURDIR)/$(SERIALIZE_GO_RUNTIME)"; \
	for d in build/schema-go-lens-nc/clean/unit build/schema-go-lens-nc/planted/unit; do \
		m=$$(awk '/^module /{print $$2; exit}' "$$d/go.mod"); \
		printf 'module %s\n\ngo 1.23\n\nrequire (\n\tgithub.com/mas-bandwidth/schema/v2 v2.0.0\n\tgithub.com/mas-bandwidth/serialize.go v0.0.0\n)\n\nreplace github.com/mas-bandwidth/schema/v2 => %s\n\nreplace github.com/mas-bandwidth/serialize.go => %s\n' "$$m" "$$S" "$$R" > "$$d/go.mod"; \
		(cd "$$d" && GOFLAGS=-mod=mod go mod tidy >/dev/null 2>&1); \
	done
	@( cd build/schema-go-lens-nc/clean/unit && $(call staticcheck_run) ./... ) > build/schema-go-lens-nc/clean-sc.log 2>&1 || { echo "NEGATIVE CONTROL FAILED: staticcheck is red on a clean generated unit"; cat build/schema-go-lens-nc/clean-sc.log; exit 1; }
	@( cd build/schema-go-lens-nc/clean/unit && $(call nilaway_run) ./... ) > build/schema-go-lens-nc/clean-nil.log 2>&1 || { echo "NEGATIVE CONTROL FAILED: nilaway is red on a clean generated unit"; cat build/schema-go-lens-nc/clean-nil.log; exit 1; }
	@if ( cd build/schema-go-lens-nc/planted/unit && $(call staticcheck_run) ./... ) > build/schema-go-lens-nc/red-sc.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: staticcheck stayed green on the planted &*p round trip"; exit 1; \
	fi
	@grep -q 'SA4001' build/schema-go-lens-nc/red-sc.log || { echo "NEGATIVE CONTROL FAILED: staticcheck red without naming SA4001"; cat build/schema-go-lens-nc/red-sc.log; exit 1; }
	@if ( cd build/schema-go-lens-nc/planted/unit && $(call nilaway_run) ./... ) > build/schema-go-lens-nc/red-nil.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: nilaway stayed green on the planted nil dereference"; exit 1; \
	fi
	@grep -q 'potential nil panic\|Potential nil panic' build/schema-go-lens-nc/red-nil.log || { echo "NEGATIVE CONTROL FAILED: nilaway red without naming a nil panic"; cat build/schema-go-lens-nc/red-nil.log; exit 1; }
	@grep -q 'CellsCook.go:' build/schema-go-lens-nc/red-nil.log || { echo "NEGATIVE CONTROL FAILED: nilaway red without naming the planted CellsCook.go line"; cat build/schema-go-lens-nc/red-nil.log; exit 1; }
	@echo "schema-go lens negative control: clean generated unit is green; staticcheck names SA4001 and nilaway names the nil panic in the planted CellsCook.go"
