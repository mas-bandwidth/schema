# The generated Go APIs, independently of the engine that authored the corpus.
# Generate each distinct unit once, then read every existing manifest row once.
# test-go includes this gate, so the full nightly build records its receipt.
GO_HOSTILE_CORPUS ?= internal/ci/hostilecorpus/corpus
GO_HOSTILE_BUILD := build/go-hostile

.PHONY: schema-go-hostile-generate schema-go-runs-hostile-corpus
schema-go-hostile-generate: bin/schema tables-go-sibling-runtime
	go run ./test/go-hostile/gen -schema=./bin/schema -corpus "$(GO_HOSTILE_CORPUS)" -out $(GO_HOSTILE_BUILD) -serialize "$(SERIALIZE_GO)"

schema-go-runs-hostile-corpus: schema-go-hostile-generate
	cd $(GO_HOSTILE_BUILD) && go build -mod=mod -o runner .
	$(GO_HOSTILE_BUILD)/runner "$(GO_HOSTILE_CORPUS)"

test-go: schema-go-runs-hostile-corpus

.PHONY: schema-go-runs-hostile-corpus-negative-control
schema-go-runs-hostile-corpus-negative-control: schema-go-runs-hostile-corpus
	@rm -rf $(GO_HOSTILE_BUILD)/plant && mkdir -p $(GO_HOSTILE_BUILD)/plant
	@cp -R "$(GO_HOSTILE_CORPUS)" $(GO_HOSTILE_BUILD)/plant/corpus
	@awk -F'\t' 'BEGIN { OFS = "\t" } $$1 == "wire-valid" && $$7 == "accept" { $$7 = "refuse"; $$8 = "damaged"; $$9 = "-" } { print }' \
		"$(GO_HOSTILE_CORPUS)/manifest.tsv" > $(GO_HOSTILE_BUILD)/plant/corpus/manifest.tsv
	@if cmp -s "$(GO_HOSTILE_CORPUS)/manifest.tsv" $(GO_HOSTILE_BUILD)/plant/corpus/manifest.tsv; then \
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
