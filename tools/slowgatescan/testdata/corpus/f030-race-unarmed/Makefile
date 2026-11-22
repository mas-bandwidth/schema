# F-030: an unarmed line selecting only TestNoRace, under -race. TestNoRace is
# behind //go:build !race, so Go builds no test this line selects and runs
# nothing: the scan must not count the line as proven.
.PHONY: check
check:
	go test -race -count=1 -run TestNoRace ./pkg
