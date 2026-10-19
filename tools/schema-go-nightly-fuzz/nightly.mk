# tools/schema-go-nightly-fuzz/nightly.mk — the Go port's nightly table-reader fuzzing.
#
# Included by make/go.mk (itself included by the Makefile's make/*.mk wildcard),
# not from make/ directly: make/*.mk is the language-port registry and the
# wide-scalar refusal gate runs `schema generate --lang <file name>` for every
# one, so a non-port fragment there would be read as a backend.
#
# The corpus set is make/schema-go-nightly-fuzz-corpus.txt: every directory is
# one a Go generate target in make/go.mk reads, chosen to cover every table
# shape the port emits a reader for. The target generates that set with the
# port's own emitter (runtime=standalone), and internal/ci/nightlyfuzzgen.go
# writes the harness: ONE fuzz entry point per reader kind (FuzzLoad,
# FuzzReadStrict, FuzzTextJson, FuzzFromJson), each dispatching over every table
# the generated packages expose. The harness discovers tables from the
# emitter's own output, so a new table shape joins by adding its corpus
# directory to the list, never by editing a second list.
#
# The oracle: no panic, no hang, no sanitizer/assert finding, and a refusal is a
# clean typed error — a read that returns an error must not also return a value,
# and a read that succeeds must return one. An oracle violation is t.Fatalf,
# never a log line; a typed refusal is a pass.
#
# The nightly time budget is FUZZTIME, per reader kind. Heavy by design: it runs
# on the schedule in .github/workflows/schema-go-nightly-fuzz.yml, never per
# push. The push/PR leg is schema-go-nightly-fuzz-replay, which only replays the
# committed regression corpus and the seed corpus (no -fuzz, no clock).

SCHEMA_GO_NIGHTLY_FUZZ_CORPUS ?= make/schema-go-nightly-fuzz-corpus.txt
SCHEMA_GO_NIGHTLY_FUZZ_WORK ?= build/schema-go-nightly-fuzz
SCHEMA_GO_NIGHTLY_FUZZ_FUZZTIME ?= 10m
SCHEMA_GO_NIGHTLY_FUZZ_PARALLEL ?= 4
SCHEMA_GO_NIGHTLY_FUZZ_REGRESSIONS ?= internal/ci/nightly-fuzz-corpus
SCHEMA_GO_NIGHTLY_FUZZ_SEEDS ?= internal/ci/nightly-fuzz-corpus:tables/cards/conformance/expected:tables/cards/fuzz

SCHEMA_GO_NIGHTLY_FUZZ_MODULE := $(SCHEMA_GO_NIGHTLY_FUZZ_WORK)/corpus

# the seed list is colon separated on the command line; each entry must become
# absolute before the recipe cd's into the generated module
empty :=
space := $(empty) $(empty)
SCHEMA_GO_NIGHTLY_FUZZ_SEEDPATHS := $(subst $(space),:,$(foreach d,$(subst :,$(space),$(SCHEMA_GO_NIGHTLY_FUZZ_SEEDS)),$(abspath $(d))))

# The fuzz targets the harness writes, one per reader kind.
SCHEMA_GO_NIGHTLY_FUZZ_TARGETS := FuzzLoad FuzzReadStrict FuzzTextJson FuzzFromJson

# The tool is the Go toolchain the module pins (go.mod's `go` directive) and the
# repository's own emitter; both are probed by name, never faked.
.PHONY: schema-go-nightly-fuzz-tool-check
schema-go-nightly-fuzz-tool-check:
	@command -v go >/dev/null 2>&1 || { echo "tool missing: go"; exit 1; }
	@command -v sed >/dev/null 2>&1 || { echo "tool missing: sed"; exit 1; }
	@go version

.PHONY: schema-go-nightly-fuzz-corpus-check
schema-go-nightly-fuzz-corpus-check:
	@n=0; while IFS= read -r dir; do \
		case "$$dir" in ''|'#'*) continue;; esac; \
		if [ ! -d "$$dir" ]; then \
			echo "schema-go-nightly-fuzz REFUSES: the corpus directory $$dir does not exist"; exit 1; \
		fi; \
		n=$$((n+1)); \
	done < "$(SCHEMA_GO_NIGHTLY_FUZZ_CORPUS)"; \
	if [ "$$n" -eq 0 ]; then \
		echo "schema-go-nightly-fuzz REFUSES: $(SCHEMA_GO_NIGHTLY_FUZZ_CORPUS) names no corpus directory"; \
		exit 1; \
	fi; \
	if [ ! -d "$(SCHEMA_GO_NIGHTLY_FUZZ_REGRESSIONS)" ]; then \
		echo "schema-go-nightly-fuzz REFUSES: the regression corpus $(SCHEMA_GO_NIGHTLY_FUZZ_REGRESSIONS) does not exist"; \
		exit 1; \
	fi; \
	echo "schema-go-nightly-fuzz corpus: $$n directories"

.PHONY: schema-go-nightly-fuzz-generate
schema-go-nightly-fuzz-generate: schema-go-nightly-fuzz-tool-check bin/schema schema-go-nightly-fuzz-corpus-check internal/ci/nightlyfuzzgen.go
	@rm -rf "$(SCHEMA_GO_NIGHTLY_FUZZ_MODULE)"
	@mkdir -p "$(SCHEMA_GO_NIGHTLY_FUZZ_MODULE)"
	@while IFS= read -r dir; do \
		case "$$dir" in ''|'#'*) continue;; esac; \
		name=$$(basename "$$dir" | sed 's/[^A-Za-z0-9]/_/g'); \
		./bin/schema generate --lang go --runtime standalone --out "$(SCHEMA_GO_NIGHTLY_FUZZ_MODULE)/$$name" "$$dir" || exit 1; \
		go run internal/codegen/gotable/fuzzunit/filter_generate.go "$(SCHEMA_GO_NIGHTLY_FUZZ_MODULE)/$$name" || exit 1; \
		for f in "$(SCHEMA_GO_NIGHTLY_FUZZ_MODULE)/$$name"/*.go; do \
			if grep -q 'serialize\.' "$$f" && ! grep -q 'mas-bandwidth/serialize.go' "$$f"; then \
				sed -i '0,/^import (/s//import (\n\t"github.com\/mas-bandwidth\/serialize.go"/' "$$f" || exit 1; \
			fi; \
		done; \
	done < "$(SCHEMA_GO_NIGHTLY_FUZZ_CORPUS)"
	@printf 'module nightlyfuzz\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => ./serialize\n' > "$(SCHEMA_GO_NIGHTLY_FUZZ_MODULE)/go.mod"
	@mkdir -p "$(SCHEMA_GO_NIGHTLY_FUZZ_MODULE)/serialize"
	@printf 'module github.com/mas-bandwidth/serialize.go\n\ngo 1.23\n' > "$(SCHEMA_GO_NIGHTLY_FUZZ_MODULE)/serialize/go.mod"
	@printf 'package serialize\n\n// The only spellings a standalone table unit admits from the sibling runtime\n// are the 128-bit scalar TYPES and their comparisons (docs/PORTING.md,\n// tables-go-standalone). This is that shape for the fuzz module, which carries\n// no sibling checkout.\ntype Int128 struct {\n\tLo uint64\n\tHi uint64\n}\n\ntype Uint128 struct {\n\tLo uint64\n\tHi uint64\n}\n\nfunc Int128From64(value int64) Int128 {\n\tif value < 0 {\n\t\treturn Int128{Lo: uint64(value), Hi: ^uint64(0)}\n\t}\n\treturn Int128{Lo: uint64(value)}\n}\n\nfunc (a Int128) Cmp(b Int128) int {\n\tif a.Hi != b.Hi {\n\t\tif int64(a.Hi) < int64(b.Hi) {\n\t\t\treturn -1\n\t\t}\n\t\treturn 1\n\t}\n\tif a.Lo != b.Lo {\n\t\tif a.Lo < b.Lo {\n\t\t\treturn -1\n\t\t}\n\t\treturn 1\n\t}\n\treturn 0\n}\n\nfunc (a Uint128) Cmp(b Uint128) int {\n\tif a.Hi != b.Hi {\n\t\tif a.Hi < b.Hi {\n\t\t\treturn -1\n\t\t}\n\t\treturn 1\n\t}\n\tif a.Lo != b.Lo {\n\t\tif a.Lo < b.Lo {\n\t\t\treturn -1\n\t\t}\n\t\treturn 1\n\t}\n\treturn 0\n}\n' > "$(SCHEMA_GO_NIGHTLY_FUZZ_MODULE)/serialize/serialize.go"
	go run internal/ci/nightlyfuzzgen.go "$(SCHEMA_GO_NIGHTLY_FUZZ_CORPUS)" "$(SCHEMA_GO_NIGHTLY_FUZZ_MODULE)"

# The nightly campaign: FUZZTIME per reader kind, over every table in the set.
.PHONY: schema-go-nightly-fuzz
schema-go-nightly-fuzz: schema-go-nightly-fuzz-generate
	@for target in $(SCHEMA_GO_NIGHTLY_FUZZ_TARGETS); do \
		echo "schema-go-nightly-fuzz: $$target $(SCHEMA_GO_NIGHTLY_FUZZ_FUZZTIME)"; \
		( cd "$(SCHEMA_GO_NIGHTLY_FUZZ_MODULE)" && \
		  SCHEMA_GO_FUZZ_SEEDS="$(SCHEMA_GO_NIGHTLY_FUZZ_SEEDPATHS)" \
		  SCHEMA_GO_FUZZ_REGRESSIONS="$(abspath $(SCHEMA_GO_NIGHTLY_FUZZ_REGRESSIONS))" \
		  go test -run '^$$' -fuzz "^$$target$$" -fuzztime $(SCHEMA_GO_NIGHTLY_FUZZ_FUZZTIME) \
			-parallel $(SCHEMA_GO_NIGHTLY_FUZZ_PARALLEL) -timeout 600s ) || exit 1; \
	done

# The per-push leg: no -fuzz, no real time. It replays the committed regression
# inputs and every seed, so a crasher saved by the nightly fails the next push.
.PHONY: schema-go-nightly-fuzz-replay
schema-go-nightly-fuzz-replay: schema-go-nightly-fuzz-generate
	cd "$(SCHEMA_GO_NIGHTLY_FUZZ_MODULE)" && \
		SCHEMA_GO_FUZZ_SEEDS="$(SCHEMA_GO_NIGHTLY_FUZZ_SEEDPATHS)" \
		SCHEMA_GO_FUZZ_REGRESSIONS="$(abspath $(SCHEMA_GO_NIGHTLY_FUZZ_REGRESSIONS))" \
		go test -run 'TestSavedInputsReplay|TestSeedsRun|Fuzz' -count=1 -timeout 600s
