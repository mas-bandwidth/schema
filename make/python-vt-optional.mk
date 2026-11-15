# The optional field kind (?T, ?[N]T, ?[..N]T, and ?T over a variable closure)
# on the Python table leg.
#
# python-vt-optional runs TestPythonVtOptional and TestPythonVtOptionalArrays
# under both pinned interpreters (internal/codegen/pythontable/
# vtcells_optional_test.go) and the compiler's carry gate for the construct
# (compiler/target_python_carries_optional_variable_closure_test.go).
#
# python-vt-optional-negative-control changes the generated runtime's
# kind_optional.py in a COPY twice: a present optional writes nothing (red by
# optional_roundtrip), and a present EMPTY array writes as absent (red by
# optional_roundtrip arrays_empty).
#
# python-vt-optional-carries-negative-control builds the compiler under a Go
# overlay whose compiler/carries_python_optional.go lacks the carryPython line,
# and the carry gate must go red by name, with Compiler.Generate refusing the
# construct.
.PHONY: python-vt-optional python-vt-optional-negative-control python-vt-optional-carries-negative-control
test-python: python-vt-optional python-vt-optional-negative-control python-vt-optional-carries-negative-control

python-vt-optional:
	SCHEMA_SLOW=1 go test -tags schema_leg_python -run '^(TestPythonVtOptional|TestPythonVtOptionalArrays)$$' ./internal/codegen/pythontable/ -count=1
	go test -tags schema_leg_python -run '^TestPythonCarriesOptionalVariableClosure$$' ./compiler -count=1

python-vt-optional-negative-control:
	SCHEMA_SLOW=1 go test -tags schema_leg_python -run '^(TestPythonVtOptionalNegativeControl|TestPythonVtOptionalArraysNegativeControl)$$' ./internal/codegen/pythontable/ -count=1

PYTHON_VT_OPTIONAL_CARRIES_NEGATIVE := build/python-vt-optional/carries-negative

python-vt-optional-carries-negative-control:
	@rm -rf $(PYTHON_VT_OPTIONAL_CARRIES_NEGATIVE) && mkdir -p $(PYTHON_VT_OPTIONAL_CARRIES_NEGATIVE)
	grep -v 'carryPython(carryOptionalVariableClosure)' compiler/carries_python_optional.go > $(PYTHON_VT_OPTIONAL_CARRIES_NEGATIVE)/carries.gotext
	@printf '{"Replace":{"%s/compiler/carries_python_optional.go":"%s/$(PYTHON_VT_OPTIONAL_CARRIES_NEGATIVE)/carries.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(PYTHON_VT_OPTIONAL_CARRIES_NEGATIVE)/overlay.json
	@if go test -overlay $(PYTHON_VT_OPTIONAL_CARRIES_NEGATIVE)/overlay.json -tags schema_leg_python \
		-run '^TestPythonCarriesOptionalVariableClosure$$' ./compiler -count=1 > $(PYTHON_VT_OPTIONAL_CARRIES_NEGATIVE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: removing the optional-variable-closure registration left the carry gate green"; \
		cat $(PYTHON_VT_OPTIONAL_CARRIES_NEGATIVE)/log; exit 1; fi
	@grep -q -- '--- FAIL: TestPythonCarriesOptionalVariableClosure' $(PYTHON_VT_OPTIONAL_CARRIES_NEGATIVE)/log && \
		grep -q 'refuses the construct optional over a variable closure on field Guarded.optional' $(PYTHON_VT_OPTIONAL_CARRIES_NEGATIVE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the carry gate went red, but not by name"; \
		  cat $(PYTHON_VT_OPTIONAL_CARRIES_NEGATIVE)/log; exit 1; }
	@echo "negative control: removing carryPython(carryOptionalVariableClosure) turns TestPythonCarriesOptionalVariableClosure RED"
