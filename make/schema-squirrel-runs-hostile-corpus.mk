# make/schema-squirrel-runs-hostile-corpus.mk — the Squirrel port's run of
# the shared hostile-input corpus (docs/HOSTILE-CORPUS.md).
#
# ONE CORPUS, EVERY PORT. The committed corpus lives at
# internal/ci/hostilecorpus/corpus: one raw file per input and a line manifest
# whose verdicts are the Go engine's (internal/tablewire, internal/tabletext).
# This card does not own it and never edits it; a wrong manifest verdict is an
# engine-card finding, not a reason to change the corpus.
#
# THE UNIT TIER. The runner is a unit test in internal/ci, so `make
# test-squirrel` runs it with the port's other unit tests. It is files-only:
# the pinned interpreter reads generated modules and raw inputs, no network and
# no real time. The planted-bad-pattern control is the nightly half.

GO ?= go

.PHONY: schema-squirrel-runs-hostile-corpus schema-squirrel-runs-hostile-corpus-negative-control

# THE CORPUS GATE: every manifest input through the port's generated reader,
# and the scorer's own outcomes. Each case's verdict is a line the runner
# writes to $SCHEMA_HOSTILE_VERDICTS, which the registry's hostile rows read
# (tools/schema-test/hostile.go). The planted control is its own target below,
# so the corpus run proves the corpus and nothing runs twice.
#
# THE SLOW GATE. The two interpreter-driven tests fail by name on a machine
# without dist/squirrel-3.2/bin/sq (docs/rulings.json no-skips). These targets
# build the interpreter first and run through test/slowgate/proof, which
# refuses a SKIP in the log and requires each named test's own --- PASS line,
# so a run that tested nothing is refused, never a vacuous pass.
schema-squirrel-runs-hostile-corpus: dist-squirrel
	sh test/slowgate/proof schema-squirrel-runs-hostile-corpus \
		'TestHostileCorpus_squirrel TestSquirrelHostileScorerOutcomes' \
		./internal/ci -run '^(TestHostileCorpus_squirrel|TestSquirrelHostileScorerOutcomes)$$' -count=1 -timeout 600s

# THE NIGHTLY CONTROL: the same plant alone, so a control that stops going red
# is named without a full corpus run.
schema-squirrel-runs-hostile-corpus-negative-control: dist-squirrel
	sh test/slowgate/proof schema-squirrel-runs-hostile-corpus-negative-control \
		'TestSquirrelRunsHostileCorpusPlant' \
		./internal/ci -run '^TestSquirrelRunsHostileCorpusPlant$$' -count=1 -timeout 600s

# The port's normal unit target includes the runner (the card's STEP 1).
test-squirrel: schema-squirrel-runs-hostile-corpus
