# serialize.py: the tests and the analyzer, under the interpreters the caller
# names. Nothing here finds an interpreter on PATH: PYTHON_FLOOR (CPython
# 3.11, the oldest release supported), PYTHON (the newest stable CPython) and
# MYPY (mypy, run under the floor) come from the environment or the command
# line, and a target that needs one refuses by name when it is unset.
#
#   make test PYTHON_FLOOR=... PYTHON=... MYPY=...
#
# Inside mas-bandwidth/schema, make/python-serialize.mk runs the same three
# legs under the pins schema builds (make dist-python).

PYTHON_FLOOR ?=
PYTHON ?=
MYPY ?=

.PHONY: test test-floor test-current mypy negative-control
test: test-floor test-current mypy negative-control

need = @if [ -z "$($(1))" ]; then echo "make $@ REFUSES: $(1) is unset; name the interpreter: make $@ $(1)=/path/to/it"; exit 1; fi

# Every test under one interpreter, isolated (-I): tests/run.py puts src/ and
# tests/ on sys.path and refuses a run that discovers no test.
test-floor:
	$(call need,PYTHON_FLOOR)
	$(PYTHON_FLOOR) -I tests/run.py

test-current:
	$(call need,PYTHON)
	$(PYTHON) -I tests/run.py

# The package and its tests, strict, at the floor's language version.
mypy:
	$(call need,MYPY)
	MYPYPATH=src $(MYPY) --strict --python-version 3.11 --cache-dir build/mypy-cache src/serialize tests

# The corpus runner over a root with no vectors must go red by that name: a
# runner that ran nothing is never green.
negative-control:
	$(call need,PYTHON_FLOOR)
	@rm -rf build/negative-control && mkdir -p build/negative-control/empty
	@if SERIALIZE_CORPUS_DIR=build/negative-control/empty $(PYTHON_FLOOR) -I tests/run.py test_conformance.ConformanceTest.test_corpus > build/negative-control/log 2>&1; then \
		cat build/negative-control/log; echo "NEGATIVE CONTROL FAILED: the corpus test passed over a root with no vectors"; exit 1; fi
	@grep -q "is missing or empty" build/negative-control/log || \
		{ cat build/negative-control/log; echo "NEGATIVE CONTROL FAILED: the empty corpus root went red, but not by name"; exit 1; }
	@echo "negative control: a corpus root with no vectors turns the corpus test red, by name"
