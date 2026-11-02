# make/schema-cs-runs-hostile-corpus.mk — the C# port runs the shared
# hostile-input corpus (schema-cs-runs-hostile-corpus).
#
# The corpus is the committed portable directory docs/HOSTILE-CORPUS.md names:
# internal/ci/hostilecorpus/corpus/manifest.tsv plus one raw input per line.
# test/cs-hostile/gen reads the manifest, generates the C# unit every schema it
# names, and writes the dispatch that calls the port's own reader for each
# (schema, table, carrier); the runner (test/cs-hostile/Runner.cs) hands every
# input to it in pooled worker processes, so a stack overflow or a hang is a
# FAIL for its case and the rest still run. Reads files only: no network, no
# clock.
#
# A disagreement the port already owes is carried, by name, in
# test/cs-hostile/known-findings.tsv, and printed as a FIX-CARD. A disagreement
# no FIX-CARD names is a failure, and a FIX-CARD that has gone green is a
# failure too, so the list cannot rot into silence.
#
# The planted-verdict control requires the unplanted corpus green, then copies
# the corpus to a scratch directory, flips the committed verdict of one passing
# accepted case and one passing refused case, and requires the runner to go red
# naming each.

CS_HOSTILE_CORPUS   ?= internal/ci/hostilecorpus/corpus
CS_HOSTILE_BUILD    ?= build/cs-hostile
CS_HOSTILE_FINDINGS ?= test/cs-hostile/known-findings.tsv

CS_HOSTILE_DOTNET_FLAGS = -v q --nologo \
	-p:HostileGeneratedDir=$(CURDIR)/$(CS_HOSTILE_BUILD)/generated \
	-p:SerializeCsDir=$(abspath $(SERIALIZE_CS)) \
	-p:BaseIntermediateOutputPath=$(CURDIR)/$(CS_HOSTILE_BUILD)/obj/

$(CS_HOSTILE_BUILD)/generated/.stamp: bin/schema make/schema-cs-runs-hostile-corpus.mk \
		test/cs-hostile/gen/main.go internal/ci/hostilecorpus/corpus.go $(CS_HOSTILE_CORPUS)/manifest.tsv \
		$(wildcard tables/*/*.schema)
	go run ./test/cs-hostile/gen -schema bin/schema -corpus $(CS_HOSTILE_CORPUS) -out $(CS_HOSTILE_BUILD)/generated
	@touch $@

$(CS_HOSTILE_BUILD)/bin/.stamp: $(CS_HOSTILE_BUILD)/generated/.stamp test/cs-hostile/Runner.cs test/cs-hostile/cshostile.csproj
	$(DOTNET) build test/cs-hostile/cshostile.csproj $(CS_HOSTILE_DOTNET_FLAGS) -o $(CS_HOSTILE_BUILD)/bin
	@touch $@

.PHONY: schema-cs-runs-hostile-corpus
schema-cs-runs-hostile-corpus: $(CS_HOSTILE_BUILD)/bin/.stamp
	$(DOTNET) $(CS_HOSTILE_BUILD)/bin/cshostile.dll "$(CS_HOSTILE_CORPUS)" "$(CS_HOSTILE_FINDINGS)"

# The port's normal unit target runs the corpus: this file is included after
# make/cs.mk by the Makefile's wildcard, so the prerequisite lands here and
# make/cs.mk itself is untouched.
test-cs: schema-cs-runs-hostile-corpus

.PHONY: schema-cs-runs-hostile-corpus-negative-control
schema-cs-runs-hostile-corpus-negative-control: $(CS_HOSTILE_BUILD)/bin/.stamp
	@rm -rf $(CS_HOSTILE_BUILD)/plant && mkdir -p $(CS_HOSTILE_BUILD)/plant/corpus
	@cp -R $(CS_HOSTILE_CORPUS)/. $(CS_HOSTILE_BUILD)/plant/corpus/
	@$(DOTNET) $(CS_HOSTILE_BUILD)/bin/cshostile.dll "$(CS_HOSTILE_CORPUS)" "$(CS_HOSTILE_FINDINGS)" > $(CS_HOSTILE_BUILD)/plant/clean.log 2>&1 || \
		{ echo 'NEGATIVE CONTROL FAILED: the unplanted corpus is not green'; cat $(CS_HOSTILE_BUILD)/plant/clean.log; exit 1; }
	@sed -n 's/^PASS //p' $(CS_HOSTILE_BUILD)/plant/clean.log > $(CS_HOSTILE_BUILD)/plant/pass; \
	acc=$$(awk -F'\t' 'NR == FNR { ok[$$0] = 1; next } !/^#/ && $$7 == "accept" && ($$1 in ok) { print $$1; exit }' \
		$(CS_HOSTILE_BUILD)/plant/pass $(CS_HOSTILE_CORPUS)/manifest.tsv); \
	ref=$$(awk -F'\t' 'NR == FNR { ok[$$0] = 1; next } !/^#/ && $$7 == "refuse" && ($$1 in ok) { print $$1; exit }' \
		$(CS_HOSTILE_BUILD)/plant/pass $(CS_HOSTILE_CORPUS)/manifest.tsv); \
	if [ -z "$$acc" ] || [ -z "$$ref" ]; then echo 'NEGATIVE CONTROL FAILED: no passing accepted and refused case to plant'; exit 1; fi; \
	awk -F'\t' -v OFS='\t' -v acc="$$acc" -v ref="$$ref" \
		'$$1 == acc { $$7 = "refuse"; $$8 = "damaged"; $$9 = "-" } \
		 $$1 == ref { $$7 = "accept"; $$8 = "-"; $$9 = "0000000000000000000000000000000000000000000000000000000000000000" } { print }' \
		$(CS_HOSTILE_CORPUS)/manifest.tsv > $(CS_HOSTILE_BUILD)/plant/corpus/manifest.tsv; \
	if cmp -s $(CS_HOSTILE_CORPUS)/manifest.tsv $(CS_HOSTILE_BUILD)/plant/corpus/manifest.tsv; then \
		echo 'NEGATIVE CONTROL FAILED: the planted verdicts did not apply'; exit 1; fi; \
	if $(DOTNET) $(CS_HOSTILE_BUILD)/bin/cshostile.dll "$(CS_HOSTILE_BUILD)/plant/corpus" "$(CS_HOSTILE_FINDINGS)" \
			> $(CS_HOSTILE_BUILD)/plant/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: changed verdicts left the corpus green'; cat $(CS_HOSTILE_BUILD)/plant/log; exit 1; fi; \
	for name in $$acc $$ref; do \
		grep -q "^FAIL $$name " $(CS_HOSTILE_BUILD)/plant/log || \
			{ echo "NEGATIVE CONTROL FAILED: the runner went red without naming the planted case $$name"; \
			  cat $(CS_HOSTILE_BUILD)/plant/log; exit 1; }; \
	done; \
	echo "cs hostile-corpus negative control: planted verdicts on $$acc and $$ref red the corpus; unplanted it is green"
