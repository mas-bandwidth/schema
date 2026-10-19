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

# THE FAST GATE (push and pull request): every manifest input through the
# port's generated reader, the planted-bad-pattern control, and the workflow's
# own fleet/fork guard.
#
# THE SLOW GATE. The two interpreter-driven tests skip at internal/slowtest.Gate
# in a plain `go test ./internal/ci/` (a machine without dist/squirrel-3.2/bin/sq
# must stay green). These targets build the interpreter first and run through
# test/slowgate/proof, which sets SCHEMA_SLOW=1, refuses a gate skip and
# requires each named test's own --- PASS line, so a missing interpreter is red
# and a run that tested nothing is refused, never a vacuous pass.
schema-squirrel-runs-hostile-corpus: dist-squirrel
	sh test/slowgate/proof schema-squirrel-runs-hostile-corpus \
		'TestSquirrelRunsHostileCorpus TestSquirrelRunsHostileCorpusPlant TestSquirrelRunsHostileCorpusWorkflowIsFleetOnly' \
		./internal/ci -run '^TestSquirrelRunsHostileCorpus' -count=1 -timeout 600s

# THE NIGHTLY CONTROL: the same plant alone, so a control that stops going red
# is named without a full corpus run.
schema-squirrel-runs-hostile-corpus-negative-control: dist-squirrel
	sh test/slowgate/proof schema-squirrel-runs-hostile-corpus-negative-control \
		'TestSquirrelRunsHostileCorpusPlant' \
		./internal/ci -run '^TestSquirrelRunsHostileCorpusPlant$$' -count=1 -timeout 600s

# The port's normal unit target includes the runner (the card's STEP 1).
test-squirrel: schema-squirrel-runs-hostile-corpus
