# make/schema-moo-runs-hostile-corpus.mk — the MOO port's run of the SHARED
# HOSTILE-INPUT CORPUS (docs/HOSTILE-CORPUS.md; schema-shared-hostile-corpus).
#
# The corpus is the committed manifest plus one raw input per case. This target
# generates the moo reader for each schema the manifest names through the
# default build's compiler, runs every carrier through it under the pinned moo
# interpreter, and holds the port to the manifest's verdict, named reason and
# (for an accept) decoded-value digest. A case that raises answers `error` and
# fails its own row by name. It reads files only: no network, no real time, no
# sockets.
#
# The runner is internal/ci/schema_moo_runs_hostile_corpus_test.go, the
# sibling of the Luau port's (make/schema-luau-runs-hostile-corpus.mk). It is
# part of this port's unit tier: test-moo runs it. The negative control plants
# a text-reader bug in a scratch copy of the emitted json.moo and requires the
# same runner to go red on exactly the rows it moves; it is the nightly half
# because it runs the corpus twice.
.PHONY: schema-moo-runs-hostile-corpus schema-moo-runs-hostile-corpus-negative-control
# The tests shell out to the pinned interpreter, so they sit behind
# slowtest.Gate; both targets run them through test/slowgate/proof, which sets
# SCHEMA_SLOW=1, refuses a gate skip and requires each named --- PASS, so a
# skipped run can never pass as green.
schema-moo-runs-hostile-corpus: toolchain-moo
	sh test/slowgate/proof schema-moo-runs-hostile-corpus \
		'TestMooRunsHostileCorpus' \
		./internal/ci -run '^TestMooRunsHostileCorpus$$' -count=1 -timeout 600s

schema-moo-runs-hostile-corpus-negative-control: toolchain-moo
	sh test/slowgate/proof schema-moo-runs-hostile-corpus-negative-control \
		'TestMooRunsHostileCorpusPlantedPattern' \
		./internal/ci -run '^TestMooRunsHostileCorpusPlantedPattern$$' -count=1 -timeout 600s

test-moo: schema-moo-runs-hostile-corpus
