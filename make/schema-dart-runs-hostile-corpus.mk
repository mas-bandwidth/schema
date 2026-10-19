# THE DART PORT RUNS THE WHOLE SHARED HOSTILE CORPUS
# (schema-dart-runs-hostile-corpus; docs/HOSTILE-CORPUS.md).
#
# Every input in internal/ci/hostilecorpus/corpus is decoded by the Dart port's
# generated reader and must reach the manifest's verdict: accept with the same
# decoded-value digest, or refuse with the same named reason. The runner is a
# Go test that generates the Dart readers, writes the Dart runner into a temp
# directory, runs it with the SDK on PATH (or DART=), and compares every line.
# It reads files only: no network, no real time.
#
# A mismatch is a finding against the Dart reader. The test is red first and
# names every mismatch; the negative control plants a reader bug in a scratch
# copy and requires the comparison to see it.

.PHONY: schema-dart-runs-hostile-corpus
schema-dart-runs-hostile-corpus:
	SCHEMA_REQUIRE_CORPUS=1 go test ./internal/ci/ -count=1 -timeout 600s \
		-run '^TestSchemaDartRunsHostileCorpus$$'
	@echo 'dart hostile corpus: every verdict, named reason and decoded-value digest agrees'

.PHONY: schema-dart-runs-hostile-corpus-negative-control
schema-dart-runs-hostile-corpus-negative-control:
	SCHEMA_REQUIRE_CORPUS=1 go test ./internal/ci/ -count=1 -timeout 600s \
		-run '^TestSchemaDartHostileCorpusNegativeControl$$'
	@echo 'dart hostile corpus negative control: a planted reader bug is seen'

test-dart: schema-dart-runs-hostile-corpus schema-dart-runs-hostile-corpus-negative-control
