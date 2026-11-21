# F-030 control: the same line without -race builds and runs TestNoRace alone,
# and selects no gated test: proven.
.PHONY: check
check:
	go test -count=1 -run TestNoRace ./pkg
