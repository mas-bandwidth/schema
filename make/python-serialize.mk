# make/python-serialize.mk — serialize.py, the Python leg's bit layer
# (docs/PORT-PYTHON.md). Included by the Makefile's wildcard include; the stem
# has a dash, so it is a support fragment and never probed as a --lang port.
#
# serialize.py is the serialize family's Python member at format 2.0. It is
# staged in third_party/serialize.py/, laid out as its own repository, until
# mas-bandwidth/serialize.py exists; the directory then moves there as it
# stands and is vendored back by sha256 like the other legs' runtimes. Its
# judge is the family corpus vendored inside it (VENDORED pins mas-bandwidth/
# serialize v2.1.1, commit bd3d67e23c8a): every vector, every sabotage control
# and the runner's own witnesses, under both pinned interpreters, plus mypy
# strict over the package and its tests.
#
# Each leg is its own target, so make -j runs them side by side.
test-python: python-serialize-test

PYTHON_SERIALIZE_DIR := third_party/serialize.py
PYTHON_SERIALIZE_PINS = PYTHON_FLOOR=$(PYTHON_FLOOR) PYTHON=$(PYTHON) MYPY=$(MYPY)

.PHONY: python-serialize-test python-serialize-test-floor python-serialize-test-current python-serialize-mypy python-serialize-negative-control
python-serialize-test: python-serialize-test-floor python-serialize-test-current python-serialize-mypy python-serialize-negative-control

python-serialize-test-floor: python-toolchain-check
	$(MAKE) --no-print-directory -C $(PYTHON_SERIALIZE_DIR) test-floor $(PYTHON_SERIALIZE_PINS)

python-serialize-test-current: python-toolchain-check
	$(MAKE) --no-print-directory -C $(PYTHON_SERIALIZE_DIR) test-current $(PYTHON_SERIALIZE_PINS)

python-serialize-mypy: python-toolchain-check
	$(MAKE) --no-print-directory -C $(PYTHON_SERIALIZE_DIR) mypy $(PYTHON_SERIALIZE_PINS)

# A corpus root with no vectors turns the corpus test red, by name. The
# per-rule sabotage controls run inside the test itself (test_controls).
python-serialize-negative-control: python-toolchain-check
	$(MAKE) --no-print-directory -C $(PYTHON_SERIALIZE_DIR) negative-control $(PYTHON_SERIALIZE_PINS)
