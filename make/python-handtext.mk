# Hand-editable text grammar, strict resolution and lossless document edits.
.PHONY: python-handtext python-handtext-core python-handtext-go-reference python-handtext-cpp-reference python-handtext-negative-control python-handtext-ci-coverage python-handtext-ci-patterns

PYTHON_HANDTEXT_CORE = ^TestPythonHandtext(Syntax|Resolution|ReservedName|SurfaceSelection|Corpus|Unbounded|DottedKey|KindArrays|ArrayRoot|WireCarrier|WireCarrierCRC|UnionBlobs)$$
PYTHON_HANDTEXT_GO_REFERENCE = ^TestPythonHandtext(ReferencesFunctional|GoCorpusFunctional|UnboundedReferencesFunctional)$$
PYTHON_HANDTEXT_CPP_REFERENCE = ^TestPythonHandtext(CppCorpusFunctional|FixedCarrierCRCFunctional)$$
PYTHON_HANDTEXT_CONTROLS = ^TestPythonHandtext(NegativeControl|KindsNegativeControl|ArrayRootNegativeControl|WireCarrierNegativeControl|WireCarrierCRCNegativeControl|UnionBlobsNegativeControl|EditPathNegativeControl|CombinedEditNegativeControl|SurfaceNegativeControl)$$

test-python: python-handtext-core python-handtext-go-reference python-handtext-cpp-reference python-handtext-negative-control python-handtext-ci-coverage
PYTHON_CI_SUPPORT_GROUPS += python-handtext-core python-handtext-go-reference python-handtext-cpp-reference python-handtext-negative-control python-handtext-ci-coverage

python-handtext: python-handtext-core python-handtext-go-reference python-handtext-cpp-reference
python-handtext-core: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '$(PYTHON_HANDTEXT_CORE)' ./internal/codegen/pythontable/
python-handtext-go-reference: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '$(PYTHON_HANDTEXT_GO_REFERENCE)' ./internal/codegen/pythontable/
python-handtext-cpp-reference: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '$(PYTHON_HANDTEXT_CPP_REFERENCE)' ./internal/codegen/pythontable/

python-handtext-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -tags schema_leg_python -run '$(PYTHON_HANDTEXT_CONTROLS)' ./internal/codegen/pythontable/
python-handtext-ci-coverage:
	SCHEMA_SLOW=1 go test -count=1 -tags schema_leg_python -run '^TestPythonHandtextCISelection$$' ./internal/ci/
python-handtext-ci-patterns:
	@printf '%s\n' '$(PYTHON_HANDTEXT_CORE)' '$(PYTHON_HANDTEXT_GO_REFERENCE)' '$(PYTHON_HANDTEXT_CPP_REFERENCE)' '$(PYTHON_HANDTEXT_CONTROLS)'

.PHONY: table-base64-python
build/table-base64/python/.stamp: build/schema-python test/table-base64/Bytes.schema
	build/schema-python generate --lang python --out build/table-base64/python test/table-base64/Bytes.schema
	@touch $@

table-base64-python: python-toolchain-check build/table-base64/harness build/table-base64/python/.stamp
	build/table-base64/harness $(PYTHON) -I -B test/table-base64/python/driver.py build/table-base64/python
	build/table-base64/harness $(PYTHON_FLOOR) -I -B test/table-base64/python/driver.py build/table-base64/python

.PHONY: table-base64-python-negative-control
test-python: table-base64-python table-base64-python-negative-control
PYTHON_CI_SUPPORT_GROUPS += table-base64-python table-base64-python-negative-control

table-base64-python-negative-control: python-toolchain-check build/table-base64/harness build/table-base64/python/.stamp
	@rm -rf build/table-base64/python-control
	@cp -R build/table-base64/python build/table-base64/python-control
	@sed -i.bak 's/if base64.b64encode(raw).decode("ascii") != value:/if False:/' build/table-base64/python-control/base64test/_schema/json_table.py && rm -f build/table-base64/python-control/base64test/_schema/json_table.py.bak
	@for python in "$(PYTHON)" "$(PYTHON_FLOOR)"; do \
	  if build/table-base64/harness "$$python" -I -B test/table-base64/python/driver.py build/table-base64/python-control > build/table-base64/python-control.log 2>&1; then cat build/table-base64/python-control.log; echo 'base64 control unexpectedly passed'; exit 1; fi; \
	  grep -q 'padding-//==' build/table-base64/python-control.log || { cat build/table-base64/python-control.log; exit 1; }; \
	done
	@echo 'base64 noncanonical control failed by name'
