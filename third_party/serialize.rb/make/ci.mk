# The CI gate: the workflows run the pinned toolchain and nothing found on
# PATH. Both targets read files in the tree only; they need no interpreter, no
# network and no clock.
.PHONY: ci-check ci-check-negative-control
ci-check:
	@bash tools/checkci/check-ci.sh .

ci-check-negative-control:
	@bash tools/checkci/negative-control.sh
