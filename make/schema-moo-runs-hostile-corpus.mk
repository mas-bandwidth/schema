# make/schema-moo-runs-hostile-corpus.mk — the moo port's run of the SHARED
# HOSTILE-INPUT CORPUS (docs/HOSTILE-CORPUS.md; schema-shared-hostile-corpus).
#
# The corpus is the committed manifest plus one raw input per case
# (internal/ci/hostilecorpus/corpus). This target generates the moo reader for
# each schema the manifest names, runs every carrier through it under the pinned
# moo, and holds the port to the manifest's verdict and (for an accept)
# decoded-value digest; a refusal conforms on its verdict alone
# (docs/rulings.json refusal-verdict-only). moo has no io library, so the inputs
# go in as source and the answers come out on stdout (internal/codegen/moo/moorun).
# It reads files only: no network, no real time, no sockets. This card does not
# own the corpus and never edits it; a wrong manifest verdict is an engine-card
# finding.
#
# The runner is internal/ci/schema_moo_runs_hostile_corpus_test.go. It is part
# of the port's unit tier: test-moo runs it. The negative control plants a
# text-reader bug in a scratch copy of the generated json.moo and requires the
# same runner to go red on exactly the rows the plant moves; it runs the corpus
# once more, so it is the nightly half.
.PHONY: schema-moo-runs-hostile-corpus schema-moo-runs-hostile-corpus-negative-control

# The tests shell out to the pinned interpreter, so they sit behind
# slowtest.Gate; both targets run through test/slowgate/proof, which sets
# SCHEMA_SLOW=1, refuses a gate skip and requires each named --- PASS, so a
# skipped run can never pass as green.
schema-moo-runs-hostile-corpus: toolchain-moo
	sh test/slowgate/proof schema-moo-runs-hostile-corpus \
		'TestSchemaMooRunsHostileCorpus TestSchemaMooHostileScorerOutcomes' \
		./internal/ci -run '^(TestSchemaMooRunsHostileCorpus|TestSchemaMooHostileScorerOutcomes)$$' -count=1 -timeout 600s

schema-moo-runs-hostile-corpus-negative-control: toolchain-moo
	sh test/slowgate/proof schema-moo-runs-hostile-corpus-negative-control \
		'TestSchemaMooRunsHostileCorpusPlantedReader' \
		./internal/ci -run '^TestSchemaMooRunsHostileCorpusPlantedReader$$' -count=1 -timeout 600s

# The leg's normal test target includes the runner.
test-moo: schema-moo-runs-hostile-corpus
