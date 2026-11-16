# Python's generated-code verdict pipe and its planted refusal control.
.PHONY: tables-python-wire-fuzz tables-python-wire-fuzz-negative-control tables-python-wire-fuzz-retain-negative-control python-array-widen-negative-control

tables-python-wire-fuzz: build/conformance-harness build/conformance-python python-toolchain-check
	@set -eu; \
	if find build/conformance-python -name '*.refused' -print | grep .; then echo 'wire-fuzz: a corpus unit was refused'; exit 1; fi; \
	for python in '$(PYTHON)' '$(PYTHON_FLOOR)'; do \
	  for arm in plain mixed retain; do \
	    opts=''; case $$arm in mixed) opts=--mixed-builder;; retain) opts=--retain;; esac; \
	    for mode in normal dev; do \
	      flags=''; [ "$$mode" = normal ] || flags='-X dev'; \
	      ./build/conformance-harness wire-fuzz $$opts --driver "$$python $$flags -W error -I -B test/conformance/python/wirefuzz.py build/conformance-python" --seed $(SEED) --n $(N) > build/python-wirefuzz.log 2>&1 || { cat build/python-wirefuzz.log; exit 1; }; \
	      cat build/python-wirefuzz.log; \
	      grep -q '0 divergences' build/python-wirefuzz.log; \
	      if grep -q 'seeds absent' build/python-wirefuzz.log; then echo 'wire-fuzz: missing roster entries'; exit 1; fi; \
	    done; \
	  done; \
	done

tables-python-wire-fuzz-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonWireFuzzVerdictPipeNegativeControl$$' ./internal/codegen/pythontable/

tables-python-wire-fuzz-retain-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonWireFuzzRetainNegativeControl$$' ./internal/codegen/pythontable/

python-array-widen-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonArrayWideningNegativeControl$$' ./internal/codegen/pythontable/
test-python: tables-python-wire-fuzz tables-python-wire-fuzz-negative-control tables-python-wire-fuzz-retain-negative-control python-array-widen-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-array-widen-negative-control tables-python-wire-fuzz tables-python-wire-fuzz-negative-control tables-python-wire-fuzz-retain-negative-control
