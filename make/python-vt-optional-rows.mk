# The optional rows (port/python/vt/optional-fields and
# port/python/vt/optional-arrays) on the Python table leg, held over every
# conformance-manifest instance and construct the rows name.
#
# python-vt-optional-rows runs TestPythonVtOptionalRows under both pinned
# interpreters (internal/codegen/pythontable/vtoptional_rows_test.go).
#
# python-vt-optional-rows-negative-control plants each optional fault in a COPY
# of the emitted runtime (kind_optional.py, fixedform.py's optional shape, the
# emitted class's optional default) and each must turn its named cases red.
.PHONY: python-vt-optional-rows python-vt-optional-rows-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-vt-optional-rows python-vt-optional-rows-negative-control
test-python: python-vt-optional-rows python-vt-optional-rows-negative-control

python-vt-optional-rows:
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonVtOptionalRows$$' ./internal/codegen/pythontable/

python-vt-optional-rows-negative-control:
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonVtOptionalRowsNegativeControl$$' ./internal/codegen/pythontable/
