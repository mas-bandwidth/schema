# make/schema-luau-runs-hostile-corpus.mk — the Luau port's run of the SHARED
# HOSTILE-INPUT CORPUS (docs/HOSTILE-CORPUS.md; schema-shared-hostile-corpus).
#
# The corpus is the committed manifest plus one raw input per case. This target
# generates the Luau reader for each schema the manifest names, runs every
# carrier through it, and holds the port to the manifest's verdict and (for an
# accept) decoded-value digest; a refusal conforms on its verdict alone
# (docs/rulings.json refusal-verdict-only). It reads files only: no network, no
# real time, no sockets.
#
# The runner is internal/ci/schema_luau_runs_hostile_corpus_test.go. It is part
# of this port's unit tier: test-luau runs it. The negative control plants a
# reader bug in a scratch copy and requires the same runner to go red; it is
# the nightly half because it runs the corpus twice.
.PHONY: schema-luau-runs-hostile-corpus schema-luau-runs-hostile-corpus-negative-control
# The tests shell out to the pinned interpreter and fail by name without it
# (docs/rulings.json no-skips); both targets run them through
# test/slowgate/proof, which refuses a SKIP in the log and requires each named
# --- PASS, so a run that tested nothing can never pass as green.
# THE BUILD, DECLARED: the prerequisites the corpus target builds first.
# tools/schema-test builds this target before a row's deadline starts (the
# hostile harness's Build step), so what it builds is never a case's timeout;
# what the run recipe itself still compiles is paid inside the run.
.PHONY: schema-luau-runs-hostile-corpus-build
schema-luau-runs-hostile-corpus-build: toolchain-luau

schema-luau-runs-hostile-corpus: schema-luau-runs-hostile-corpus-build
	sh test/slowgate/proof schema-luau-runs-hostile-corpus \
		'TestHostileCorpus_luau TestLuauHostileScorerOutcomes' \
		./internal/ci -run '^(TestHostileCorpus_luau|TestLuauHostileScorerOutcomes)$$' -count=1 -timeout 600s

schema-luau-runs-hostile-corpus-negative-control: toolchain-luau
	sh test/slowgate/proof schema-luau-runs-hostile-corpus-negative-control \
		'TestLuauRunsHostileCorpusPlantedPattern' \
		./internal/ci -run '^TestLuauRunsHostileCorpusPlantedPattern$$' -count=1 -timeout 600s

test-luau: schema-luau-runs-hostile-corpus
