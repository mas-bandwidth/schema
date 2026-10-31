# make/schema-luau-runs-hostile-corpus.mk — the Luau port's run of the SHARED
# HOSTILE-INPUT CORPUS (docs/HOSTILE-CORPUS.md; schema-shared-hostile-corpus).
#
# The corpus is the committed manifest plus one raw input per case. This target
# generates the Luau reader for each schema the manifest names, runs every
# carrier through it, and holds the port to the manifest's verdict, named reason
# and (for an accept) decoded-value digest. It reads files only: no network, no
# real time, no sockets.
#
# The runner is internal/ci/schema_luau_runs_hostile_corpus_test.go. It is part
# of this port's unit tier: test-luau runs it. The negative control plants a
# reader bug in a scratch copy and requires the same runner to go red; it is
# the nightly half because it runs the corpus twice.
.PHONY: schema-luau-runs-hostile-corpus schema-luau-runs-hostile-corpus-negative-control
# The tests shell out to the pinned interpreter, so they sit behind
# slowtest.Gate; both targets run them through test/slowgate/proof, which sets
# SCHEMA_SLOW=1, refuses a gate skip and requires each named --- PASS, so a
# skipped run can never pass as green.
schema-luau-runs-hostile-corpus: toolchain-luau
	sh test/slowgate/proof schema-luau-runs-hostile-corpus \
		'TestLuauRunsHostileCorpus TestLuauVocabularyDefaultEntryBound' \
		./internal/ci -run '^(TestLuauRunsHostileCorpus|TestLuauVocabularyDefaultEntryBound)$$' -count=1 -timeout 600s

schema-luau-runs-hostile-corpus-negative-control: toolchain-luau
	sh test/slowgate/proof schema-luau-runs-hostile-corpus-negative-control \
		'TestLuauRunsHostileCorpusPlantedPattern' \
		./internal/ci -run '^TestLuauRunsHostileCorpusPlantedPattern$$' -count=1 -timeout 600s

test-luau: schema-luau-runs-hostile-corpus
