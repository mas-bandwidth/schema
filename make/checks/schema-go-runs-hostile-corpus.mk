# make/checks/schema-go-runs-hostile-corpus.mk — the Go port walks the shared hostile corpus.
#
# schema-go-runs-hostile-corpus: one unit test in internal/ci reads the corpus
# manifest and every raw input under internal/ci/hostilecorpus/corpus, decodes
# each with the carrier the manifest names, and refuses a verdict, a named
# reason or a decoded-value digest that differs. The planted-reader control is
# nightly: it runs the same comparison with a reader that is wrong for one
# accept case and requires the runner to go red.

GO ?= go

.PHONY: schema-go-runs-hostile-corpus schema-go-runs-hostile-corpus-negative-control

schema-go-runs-hostile-corpus:
	$(GO) test -v -count=1 -timeout 600s ./internal/ci -run '^TestSchemaGoRunsHostileCorpus$$'

schema-go-runs-hostile-corpus-negative-control:
	$(GO) test -v -count=1 -timeout 600s ./internal/ci -run '^TestSchemaGoRunsHostileCorpusPlantedReader$$'

test-go: schema-go-runs-hostile-corpus
