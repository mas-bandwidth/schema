# make/hostile-ledger.mk — a hostile-corpus runner proves it ran
# (docs/HOSTILE-CORPUS.md, "The ledger"; internal/ci/hostilecorpus/ledger.go).
#
# Every port's runner loops over the corpus manifest, and its summary printed
# the manifest's length as the case count whether or not the loop reached every
# row. A runner now prints the cases its reader gave a verdict for, per
# carrier (`hostile-ledger ran <carrier> <n>` lines closed by `hostile-ledger
# end`), and tools/hostileledger holds that to the corpus folder: cases run
# equals cases registered on every carrier, less the cases and carriers a list
# holds by name. A short count, a run of nothing, a log with no ledger and a
# stale held name are red; the tool then runs its own negative control on the
# same log (one case short of the registry must be red).
#
#   $(call hostile_ledgered,<port>,<log file>,<runner command>,<corpus folder>,<hostileledger flags>)
#
# runs the command with its output captured and shown, keeps the command's own
# exit status, and then checks the ledger. The registered count is read from
# the folder on every run; no recipe carries a number.
define hostile_ledgered
	@mkdir -p $(dir $(2))
	@set +e; { $(3); } > $(2) 2>&1; rc=$$?; cat $(2); [ $$rc -eq 0 ] || exit $$rc; \
	go run ./tools/hostileledger -port $(1) -corpus "$(4)" -log $(2) $(5)
endef

# THE LEDGER'S OWN CORPUS (tools/hostileledger/testdata/ledger-corpus): committed
# runner logs and the verdict each owes, the reviewer's findings beside their
# controls, replayed through the tool. The generated-Go gate depends on it, so
# it runs wherever test-go does; it reads files only.
.PHONY: hostile-ledger-corpus
hostile-ledger-corpus:
	go run ./tools/hostileledger -ledger-corpus tools/hostileledger/testdata/ledger-corpus

schema-go-runs-hostile-corpus: hostile-ledger-corpus
