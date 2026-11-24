# make/go-test-shard.mk — one go test gate, run whole or as shard k of n.
#
# A versioning gate is one `go test` over a codegen package, and on the fleet
# some of them do not fit the two-minute per-commit cap as one job (C 307 s,
# C# 250 s on the nightly). ci-fast.yml's nightly-fast rows versioning-<leg>-k
# run the same make target with GO_TEST_SHARD=k/n: tools/gotestshard lists the
# package's tests under the gate's own -run pattern and runs those whose name
# hashes to shard k, so every test runs in exactly one shard and a new test
# needs no edit. Unset, the gate runs whole, exactly as before.
#
#   $(call go-test-gate,RUN-PATTERN,GO-TEST-FLAGS,PACKAGE)
GO_TEST_SHARD ?=
go-test-gate = $(if $(GO_TEST_SHARD),go run ./tools/gotestshard -shard $(GO_TEST_SHARD) -run '$(1)' -- $(2) $(3),go test $(2) -run '$(1)' $(3))

# AN AGGREGATE'S PREREQUISITES, run as shard SHARD=k/n (tools/makeshard; the
# nightly-fast rows test-goose-k, fixedform-python-proof-k, fixedform-ruby-k).
# Each aggregate is named literally in its recipe: tools/wiredtargets reads a
# makeshard line's last word as the aggregate it reaches.
.PHONY: test-goose-shard python-fixed-form-proof-shard ruby-fixed-form-shard
test-goose-shard:
	go run ./tools/makeshard -shard "$(SHARD)" test-goose
python-fixed-form-proof-shard:
	go run ./tools/makeshard -shard "$(SHARD)" python-fixed-form-proof
ruby-fixed-form-shard:
	go run ./tools/makeshard -each -shard "$(SHARD)" ruby-fixed-form
