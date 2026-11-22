# make/schema-unity-runs-hostile-corpus.mk — the C# port runs the shared
# hostile-input corpus (schema-unity-runs-hostile-corpus).
#
# The corpus is the committed portable directory docs/HOSTILE-CORPUS.md names:
# internal/ci/hostilecorpus/corpus/manifest.tsv plus one raw input per line.
# test/unity-hostile/gen reads the manifest, generates the C# unit every schema it
# names, and writes the dispatch that calls the port's own reader for each
# (schema, table, carrier); the runner (test/unity-hostile/Runner.cs) hands every
# input to it in pooled worker processes, so a stack overflow or a hang is a
# FAIL for its case and the rest still run. Reads files only: no network, no
# clock.
#
# A disagreement the port already owes is carried, by name, in
# test/unity-hostile/known-findings.tsv, and printed as a FIX-CARD. A disagreement
# no FIX-CARD names is a failure, and a FIX-CARD that has gone green is a
# failure too, so the list cannot rot into silence.
#
# The planted-verdict control requires the unplanted corpus green, then copies
# the corpus to a scratch directory, flips the committed verdict of one passing
# accepted case and one passing refused case, and requires the runner to go red
# naming each.

UNITY_HOSTILE_CORPUS   ?= internal/ci/hostilecorpus/corpus
UNITY_HOSTILE_BUILD    ?= build/unity-hostile
UNITY_HOSTILE_FINDINGS ?= test/unity-hostile/known-findings.tsv
# The rows the port may skip. A skip is no verdict: gen fails when the dispatch
# skips a row this list does not name, or a named row no longer skips.
UNITY_HOSTILE_SKIPS    ?= test/unity-hostile/known-skips.tsv

UNITY_HOSTILE_DOTNET_FLAGS = -v q --nologo \
	-p:HostileGeneratedDir=$(CURDIR)/$(UNITY_HOSTILE_BUILD)/generated \
	-p:SerializeCsRoot=$(abspath $(SERIALIZE_CS))

$(UNITY_HOSTILE_BUILD)/generated/.stamp: bin/schema make/schema-unity-runs-hostile-corpus.mk \
		test/unity-hostile/gen/main.go internal/ci/hostilecorpus/corpus.go $(UNITY_HOSTILE_CORPUS)/manifest.tsv \
		$(UNITY_HOSTILE_SKIPS) $(wildcard tables/*/*.schema)
	go run ./test/unity-hostile/gen -schema bin/schema -corpus $(UNITY_HOSTILE_CORPUS) -out $(UNITY_HOSTILE_BUILD)/generated -known-skips $(UNITY_HOSTILE_SKIPS)
	@touch $@

$(UNITY_HOSTILE_BUILD)/bin/.stamp: $(UNITY_HOSTILE_BUILD)/generated/.stamp test/unity-hostile/Runner.cs test/unity-hostile/unityhostile.csproj test/unity-hostile/units/unityhostileunits.csproj
	$(DOTNET) build test/unity-hostile/unityhostile.csproj $(UNITY_HOSTILE_DOTNET_FLAGS) -o $(UNITY_HOSTILE_BUILD)/bin
	@touch $@

# The registered count: the runner prints `unity-hostile: ran N, failed M`,
# where N is every case the port answered (a SKIP is not run), and
# build/mustrun holds N to this number, so a manifest that loses cases or a case
# that starts skipping is red. A commit that adds a hostile case changes it.
UNITY_HOSTILE_CASES ?= $(shell grep -vc '^#' $(UNITY_HOSTILE_CORPUS)/manifest.tsv)
.PHONY: schema-unity-runs-hostile-corpus
schema-unity-runs-hostile-corpus: build/mustrun $(UNITY_HOSTILE_BUILD)/bin/.stamp
	$(DOTNET) $(UNITY_HOSTILE_BUILD)/bin/unityhostile.dll --scorer tools/hostilecorpusexec/testdata/scorer-outcomes.tsv
	$(MUSTRUN) -name schema-unity-runs-hostile-corpus -count $(UNITY_HOSTILE_CASES) -- $(DOTNET) $(UNITY_HOSTILE_BUILD)/bin/unityhostile.dll "$(UNITY_HOSTILE_CORPUS)" "$(UNITY_HOSTILE_FINDINGS)"

# The port's normal unit target runs the corpus: this file is included after
# make/cs.mk by the Makefile's wildcard, so the prerequisite lands here and
# make/cs.mk itself is untouched.
test-unity: schema-unity-runs-hostile-corpus

.PHONY: schema-unity-runs-hostile-corpus-negative-control
schema-unity-runs-hostile-corpus-negative-control: $(UNITY_HOSTILE_BUILD)/bin/.stamp
	@rm -rf $(UNITY_HOSTILE_BUILD)/plant && mkdir -p $(UNITY_HOSTILE_BUILD)/plant/corpus
	@cp -R $(UNITY_HOSTILE_CORPUS)/. $(UNITY_HOSTILE_BUILD)/plant/corpus/
	@$(DOTNET) $(UNITY_HOSTILE_BUILD)/bin/unityhostile.dll "$(UNITY_HOSTILE_CORPUS)" "$(UNITY_HOSTILE_FINDINGS)" > $(UNITY_HOSTILE_BUILD)/plant/clean.log 2>&1 || \
		{ echo 'NEGATIVE CONTROL FAILED: the unplanted corpus is not green'; cat $(UNITY_HOSTILE_BUILD)/plant/clean.log; exit 1; }
	@sed -n 's/^PASS //p' $(UNITY_HOSTILE_BUILD)/plant/clean.log > $(UNITY_HOSTILE_BUILD)/plant/pass; \
	acc=$$(awk -F'\t' 'NR == FNR { ok[$$0] = 1; next } !/^#/ && $$7 == "accept" && ($$1 in ok) { print $$1; exit }' \
		$(UNITY_HOSTILE_BUILD)/plant/pass $(UNITY_HOSTILE_CORPUS)/manifest.tsv); \
	ref=$$(awk -F'\t' 'NR == FNR { ok[$$0] = 1; next } !/^#/ && $$7 == "refuse" && ($$1 in ok) { print $$1; exit }' \
		$(UNITY_HOSTILE_BUILD)/plant/pass $(UNITY_HOSTILE_CORPUS)/manifest.tsv); \
	out=$$(awk -F'\t' -v acc="$$acc" -v ref="$$ref" 'NR == FNR { ok[$$0] = 1; next } !/^#/ && $$7 == "accept" && ($$1 in ok) && $$1 != acc && $$1 != ref { print $$1; exit }' \
		$(UNITY_HOSTILE_BUILD)/plant/pass $(UNITY_HOSTILE_CORPUS)/manifest.tsv); \
	if [ -z "$$acc" ] || [ -z "$$ref" ] || [ -z "$$out" ]; then echo 'NEGATIVE CONTROL FAILED: no passing accepted and refused case to plant'; exit 1; fi; \
	awk -F'\t' -v OFS='\t' -v acc="$$acc" -v ref="$$ref" -v out="$$out" \
		'$$1 == acc { $$7 = "refuse"; $$8 = "damaged"; $$9 = "-" } \
		 $$1 == ref { $$7 = "accept"; $$8 = "-"; $$9 = "0000000000000000000000000000000000000000000000000000000000000000" } \
		 $$1 == out { $$5 = "inputs/../manifest.tsv" } { print }' \
		$(UNITY_HOSTILE_CORPUS)/manifest.tsv > $(UNITY_HOSTILE_BUILD)/plant/corpus/manifest.tsv; \
	if cmp -s $(UNITY_HOSTILE_CORPUS)/manifest.tsv $(UNITY_HOSTILE_BUILD)/plant/corpus/manifest.tsv; then \
		echo 'NEGATIVE CONTROL FAILED: the planted verdicts did not apply'; exit 1; fi; \
	if $(DOTNET) $(UNITY_HOSTILE_BUILD)/bin/unityhostile.dll "$(UNITY_HOSTILE_BUILD)/plant/corpus" "$(UNITY_HOSTILE_FINDINGS)" \
			> $(UNITY_HOSTILE_BUILD)/plant/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: changed verdicts left the corpus green'; cat $(UNITY_HOSTILE_BUILD)/plant/log; exit 1; fi; \
	for name in $$acc $$ref; do \
		grep -q "^FAIL $$name " $(UNITY_HOSTILE_BUILD)/plant/log || \
			{ echo "NEGATIVE CONTROL FAILED: the runner went red without naming the planted case $$name"; \
			  cat $(UNITY_HOSTILE_BUILD)/plant/log; exit 1; }; \
	done; \
	grep -q "^FAIL $$out .*outside the corpus" $(UNITY_HOSTILE_BUILD)/plant/log || \
		{ echo "NEGATIVE CONTROL FAILED: the runner read an input outside the corpus without refusing it"; \
		  cat $(UNITY_HOSTILE_BUILD)/plant/log; exit 1; }; \
	echo "unity hostile-corpus negative control: planted verdicts on $$acc and $$ref and an outside input on $$out red the corpus; unplanted it is green"
