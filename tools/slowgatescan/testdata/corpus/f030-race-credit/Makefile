# F-030, the accounting: an armed race line selecting TestNoRace and TestBoth.
# Under -race only TestBoth is built, so only TestBoth is credited to it.
.PHONY: race
race:
	SCHEMA_SLOW=1 go test -race -count=1 -run '^Test(NoRace|Both)$$' ./pkg/
