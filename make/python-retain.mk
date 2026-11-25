# The complete retention suite stays one local target, python-retain. CI runs
# it as four bounded rows so cold Go compilation plus one slice fits the
# two-minute per-commit cap; each row carries about a quarter of the heavy
# interpreter-plus-mypy tests. internal/ci's TestPythonRetainCIPlan proves that
# every ^TestPythonRetain function in the package source is selected by exactly
# one row, so a new retention test fails ci-policy until it is given a row.
PYTHON_RETAIN_SHARDS := $(addprefix python-retain-,0 1 2 3)
PYTHON_CI_SUPPORT_GROUPS += $(PYTHON_RETAIN_SHARDS) python-retain-negative-control
.PHONY: python-retain $(PYTHON_RETAIN_SHARDS) python-retain-negative-control
python-retain: $(PYTHON_RETAIN_SHARDS)
python-retain-0: dist-python
python-retain-1: dist-python
python-retain-2: dist-python
python-retain-3: dist-python
python-retain-negative-control: dist-python
test-python: $(PYTHON_RETAIN_SHARDS) python-retain-negative-control
