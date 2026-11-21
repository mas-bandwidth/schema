# make/python-conformance.mk — the Python leg's conformance targets and emitter
# sabotage controls (docs/PORT-PYTHON.md; docs/PORTING.md I2, I11).

.PHONY: conformance-python conformance-negative-control-python conformance-negative-control-python-walk

PYTHON_CI_SUPPORT_GROUPS += conformance-python conformance-negative-control-python conformance-negative-control-python-walk

conformance-python: python-toolchain-check build/conformance-harness build/conformance-cpp build/schema_test_cook build/conformance-python
	PYTHON=$(PYTHON) ./build/conformance-harness run --only python --work build/conformance-python-current
	PYTHON=$(PYTHON_FLOOR) ./build/conformance-harness run --only python --work build/conformance-python-floor

conformance-negative-control-python: python-toolchain-check build/conformance-harness build/conformance-cpp build/schema_test_cook
	PYTHON=$(PYTHON) sh test/conformance/python/negative-control wire

conformance-negative-control-python-walk: python-toolchain-check build/conformance-harness build/conformance-cpp build/schema_test_cook
	PYTHON=$(PYTHON) sh test/conformance/python/negative-control walk

test-python: conformance-python conformance-negative-control-python conformance-negative-control-python-walk

# THE DRIVER'S CASE OUTCOMES (review F-087, F-088, F-090; the fixture and its
# answers are test/conformance/case-outcomes). On both pins, over the real
# generated tree: an absent codec is an EMPTY <case>.absent, a present codec's
# refusal is the surface's refusal answer, a driver error names its case and
# reds the surface after the rest have run. Over an EMPTY tree, the same rows
# all answer absent and no surface aborts, the must-refuse rows among them.
.PHONY: conformance-python-case-outcomes conformance-python-empty-tree
PYTHON_CI_SUPPORT_GROUPS += conformance-python-case-outcomes conformance-python-empty-tree
conformance-python-case-outcomes: python-toolchain-check build/conformance-python
	@mkdir -p build/conformance-python-empty-gen
	@test -z "$$(ls -A build/conformance-python-empty-gen)" || { echo 'build/conformance-python-empty-gen is not empty'; exit 1; }
	@for python in $(PYTHON) $(PYTHON_FLOOR); do \
		PYTHON=$$python sh test/conformance/case-outcomes/check.sh test/conformance/python/driver \
			test/conformance/case-outcomes/manifest.txt test/conformance/case-outcomes/expected.txt \
			build/conformance-python-case-outcomes || exit 1; \
		PYTHON=$$python PYTHON_CONFORMANCE_GENERATED=build/conformance-python-empty-gen sh test/conformance/case-outcomes/check.sh \
			test/conformance/python/driver test/conformance/case-outcomes/manifest.txt \
			test/conformance/case-outcomes/expected-empty.txt build/conformance-python-case-outcomes-empty || exit 1; \
	done

# THE WHOLE CORPUS OVER AN EMPTY TREE (review F-087): every surface, every
# row, through the harness, with no Python codec at all. The run is green (a
# missing codec is absence, never a failure or a pass) and every answer the
# driver leaves is an EMPTY <case>.absent.
conformance-python-empty-tree: python-toolchain-check build/conformance-harness build/conformance-cpp build/schema_test_cook
	@mkdir -p build/conformance-python-empty-gen
	@test -z "$$(ls -A build/conformance-python-empty-gen)" || { echo 'build/conformance-python-empty-gen is not empty'; exit 1; }
	@PYTHON=$(PYTHON) PYTHON_CONFORMANCE_GENERATED=build/conformance-python-empty-gen \
		./build/conformance-harness run --only python --work build/conformance-python-empty > build/conformance-python-empty.log 2>&1 || \
		{ echo 'conformance-python-empty-tree: the run over an empty tree is not green'; tail -40 build/conformance-python-empty.log; exit 1; }
	@answers=$$(find build/conformance-python-empty/out/python -type f | wc -l | tr -d ' '); \
	stray=$$(find build/conformance-python-empty/out/python -type f \( ! -name '*.absent' -o -size +0 \) | head -5); \
	test "$$answers" -gt 0 || { echo 'conformance-python-empty-tree: the driver answered nothing'; exit 1; }; \
	test -z "$$stray" || { echo "conformance-python-empty-tree: answers beside a missing codec that are not an EMPTY .absent: $$stray"; exit 1; }; \
	echo "conformance-python-empty-tree: $$answers cases, every one an EMPTY .absent, run green"

test-python: conformance-python-case-outcomes conformance-python-empty-tree
