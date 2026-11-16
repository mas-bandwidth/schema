# The generated Go APIs, independently of the engine that authored the corpus.
# One schema file of a locked unit (tables/examples/Guarded.schema, the
# Patrol row) is generated as its whole unit, as the Goose and C runners
# bind it: a unit is one directory and the lock holds the unit, never one
# file of it (SPEC §3.2, docs/SPEC-TABLES.md §2.10). Passing the file alone
# makes it a one-file unit beside tables/examples/schema.lock, and the lock
# refuses every block that file does not declare (the 30 schema.lock
# errors). The Go generator keys its dispatch by the manifest's schema
# name, so this gate stages a private copy of the corpus whose Guarded row
# names the directory, and the runner reads that staged manifest; the
# shared corpus is never rewritten.
# Generate each distinct unit once, then read every existing manifest row once.
# test-go includes this gate, so the full nightly build records its receipt.
GO_HOSTILE_CORPUS ?= internal/ci/hostilecorpus/corpus
GO_HOSTILE_BUILD := build/go-hostile
GO_HOSTILE_STAGE := $(GO_HOSTILE_BUILD)/corpus-stage

.PHONY: schema-go-hostile-generate schema-go-runs-hostile-corpus
schema-go-hostile-generate: bin/schema tables-go-sibling-runtime
	@rm -rf $(GO_HOSTILE_STAGE) && mkdir -p $(GO_HOSTILE_STAGE)
	@cp -R "$(GO_HOSTILE_CORPUS)/." $(GO_HOSTILE_STAGE)/
	@awk -F'\t' 'BEGIN { OFS = "\t" } $$3 == "tables/examples/Guarded.schema" { $$3 = "tables/examples" } { print }' \
		"$(GO_HOSTILE_CORPUS)/manifest.tsv" > $(GO_HOSTILE_STAGE)/manifest.tsv
	@if cmp -s "$(GO_HOSTILE_CORPUS)/manifest.tsv" $(GO_HOSTILE_STAGE)/manifest.tsv; then \
		echo 'go hostile corpus: the Guarded.schema row no longer names tables/examples/Guarded.schema; re-anchor the locked-unit staging in make/schema-go-runs-hostile-corpus.mk'; exit 1; fi
	go run ./test/go-hostile/gen -schema=./bin/schema -corpus "$(GO_HOSTILE_STAGE)" -out $(GO_HOSTILE_BUILD) -serialize "$(SERIALIZE_GO)"

schema-go-runs-hostile-corpus: schema-go-hostile-generate
	cd $(GO_HOSTILE_BUILD) && go build -mod=mod -o runner .
	$(GO_HOSTILE_BUILD)/runner "$(GO_HOSTILE_STAGE)"

test-go: schema-go-runs-hostile-corpus

.PHONY: schema-go-runs-hostile-corpus-negative-control
schema-go-runs-hostile-corpus-negative-control: schema-go-runs-hostile-corpus
	@rm -rf $(GO_HOSTILE_BUILD)/plant && mkdir -p $(GO_HOSTILE_BUILD)/plant
	@cp -R "$(GO_HOSTILE_STAGE)" $(GO_HOSTILE_BUILD)/plant/corpus
	@awk -F'\t' 'BEGIN { OFS = "\t" } $$1 == "wire-valid" && $$7 == "accept" { $$7 = "refuse"; $$8 = "damaged"; $$9 = "-" } { print }' \
		"$(GO_HOSTILE_STAGE)/manifest.tsv" > $(GO_HOSTILE_BUILD)/plant/corpus/manifest.tsv
	@if cmp -s "$(GO_HOSTILE_STAGE)/manifest.tsv" $(GO_HOSTILE_BUILD)/plant/corpus/manifest.tsv; then \
		echo 'NEGATIVE CONTROL FAILED: the planted verdict did not apply'; exit 1; fi
	@if $(GO_HOSTILE_BUILD)/runner $(GO_HOSTILE_BUILD)/plant/corpus > $(GO_HOSTILE_BUILD)/plant/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a changed verdict left generated Go green'; \
		cat $(GO_HOSTILE_BUILD)/plant/log; exit 1; fi
	@grep -q '^FAIL wire-valid ' $(GO_HOSTILE_BUILD)/plant/log || \
		{ echo 'NEGATIVE CONTROL FAILED: generated Go did not name the planted case'; cat $(GO_HOSTILE_BUILD)/plant/log; exit 1; }
	@if grep '^FAIL ' $(GO_HOSTILE_BUILD)/plant/log | grep -v '^FAIL wire-valid '; then \
		echo 'NEGATIVE CONTROL FAILED: the plant changed another case'; exit 1; fi
	@grep '^FAIL wire-valid ' $(GO_HOSTILE_BUILD)/plant/log
	@echo 'generated Go hostile-corpus negative control: changed verdict is red on exactly wire-valid; original corpus is green'
