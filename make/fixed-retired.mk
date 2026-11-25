# make/fixed-retired.mk — NO FIXED-FORM GATE PRINTS A SKIP.
#
# docs/FIXED-FORM-ALGORITHM.md §5.6 retired the run-time walk of a stranger's
# layout. A leg's fixed-form program that still prints a line for a check it
# skips (§5.7 step 5, §5.9 #23) has a test that did not run, and nothing
# excuses one: build/retiredskips wraps each leg's fixed-form run and any
# printed SKIP, SKIPPED or RETIRED line is red.
build/retiredskips: $(wildcard tools/retiredskips/*.go)
	@mkdir -p build
	go build -o $@ ./tools/retiredskips
