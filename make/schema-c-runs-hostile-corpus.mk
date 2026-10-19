# make/schema-c-runs-hostile-corpus.mk — the C port's run of the SHARED
# HOSTILE-INPUT CORPUS (docs/HOSTILE-CORPUS.md; schema-shared-hostile-corpus).
#
# The corpus is the committed manifest plus one raw input per case. This target
# generates the C table reader for each schema the manifest names, compiles it
# in a scratch directory, and holds the port to the manifest's verdict, named
# reason and (for an accept) decoded-value digest. It reads files only: no
# network, no real time, no sockets.
#
# The runner is internal/ci/schema_c_runs_hostile_corpus_test.go. It is part of
# this port's unit tier: test-c runs it. The negative control plants a reader
# bug in a scratch copy and requires the same runner to go red; it is the
# nightly half because it runs the corpus twice.
#
# The generated 128-bit tables include serialize.h from the sibling checkout
# make/c.mk already names (SERIALIZE_C, default ../serialize.c); the runner
# passes that path to the compiler.
.PHONY: schema-c-runs-hostile-corpus schema-c-runs-hostile-corpus-negative-control
schema-c-runs-hostile-corpus:
	go test -v ./internal/ci -run '^TestCRunsHostileCorpus$$' -count=1 -timeout 600s

schema-c-runs-hostile-corpus-negative-control:
	go test ./internal/ci -run '^TestCRunsHostileCorpusPlantedPattern$$' -count=1 -timeout 600s

test-c: schema-c-runs-hostile-corpus
