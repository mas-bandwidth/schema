# The complete historical widening matrices are delegated to bounded CI rows.
# The default package test still runs every case without a shard selector.
PYTHON_WIDEN_SCALAR_SHARDS := $(addprefix python-ci-widen-scalar-,0 1 2 3 4 5 6 7)
PYTHON_CI_SUPPORT_GROUPS += python-ci-widen-container $(PYTHON_WIDEN_SCALAR_SHARDS)

.PHONY: python-ci-widen-container $(PYTHON_WIDEN_SCALAR_SHARDS)
python-ci-widen-container:
	SCHEMA_SLOW=1 $(GOTESTRUN) -v -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonContainerWidenHistorical$$' ./internal/codegen/pythontable/

$(PYTHON_WIDEN_SCALAR_SHARDS): python-ci-widen-scalar-%:
	SCHEMA_SLOW=1 PYTHON_SCALAR_WIDEN_SHARD=$* $(GOTESTRUN) -v -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonScalarWidenHistorical$$' ./internal/codegen/pythontable/

PYTHON_CI_SUPPORT_GROUPS += python-ci-widen-coverage
.PHONY: python-ci-widen-coverage
python-ci-widen-coverage:
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -run '^TestPythonWidenCIPlan$$' ./internal/ci/
