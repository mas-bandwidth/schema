# F-060: pipefail through .SHELLFLAGS. GNU Make 3.81 ignores .SHELLFLAGS and
# runs "bash -c", so this pipeline's failing first stage is green there.
SHELL := bash
.SHELLFLAGS := -o pipefail -c

check:
	go test ./... | tee build/check.log
