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
#   $(call hostile_ledgered,<port>,<log file>,<runner command>,<hostileledger flags>)
#
# runs the command with its output captured and shown, keeps the command's own
# exit status, and then checks the ledger. The registered count is read from
# the folder on every run; no recipe carries a number.
HOSTILE_LEDGER_CORPUS ?= internal/ci/hostilecorpus/corpus

define hostile_ledgered
	@mkdir -p $(dir $(2))
	@set +e; { $(3); } > $(2) 2>&1; rc=$$?; cat $(2); [ $$rc -eq 0 ] || exit $$rc; \
	go run ./tools/hostileledger -port $(1) -corpus "$(HOSTILE_LEDGER_CORPUS)" -log $(2) $(4)
endef
