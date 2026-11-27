# make/fixed-retired.mk — THE §5.6 RETIREMENTS EVERY FIXED-FORM GATE PRINTS.
#
# docs/FIXED-FORM-ALGORITHM.md §5.6 retired the run-time walk of a stranger's
# layout. Each leg's fixed-form program keeps its retired checks in the tree
# and prints one line per check it skips (§5.7 step 5, §5.9 #23). A skip is a
# test that did not run, so every such line is named in ONE ledger,
# test/conformance/fixed-retired.json (leg, exact line, the tests that carry
# its coverage now), and build/retiredskips wraps each leg's fixed-form run:
# a skip line the ledger does not name for the leg is red, and so is a ledger
# line the run did not print. The nightly readiness gate reads the same file
# and also requires each replacement test to pass.
build/retiredskips: $(wildcard tools/retiredskips/*.go)
	@mkdir -p build
	go build -o $@ ./tools/retiredskips
