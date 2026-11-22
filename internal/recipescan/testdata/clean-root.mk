# The clean control: pipefail named in SHELL itself, which make 3.81 and 4.x
# both honour, and recipes whose every status reaches make.
SHELL := bash -o pipefail

check:
	go test ./... | tee build/check.log

build/out.txt: in.txt
	./bin/convert in.txt > $@.tmp && mv $@.tmp $@
