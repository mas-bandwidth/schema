# The complete historical widening matrices are delegated to bounded CI rows.
# The default package test still runs every case without a shard selector.
PYTHON_WIDEN_SCALAR_SHARDS := $(addprefix python-ci-widen-scalar-,0 1 2 3 4 5 6 7)
PYTHON_CI_SUPPORT_GROUPS += python-ci-widen-container $(PYTHON_WIDEN_SCALAR_SHARDS)

.PHONY: python-ci-widen-container $(PYTHON_WIDEN_SCALAR_SHARDS)
python-ci-widen-container:

$(PYTHON_WIDEN_SCALAR_SHARDS): python-ci-widen-scalar-%:

PYTHON_CI_SUPPORT_GROUPS += python-ci-widen-coverage
.PHONY: python-ci-widen-coverage
python-ci-widen-coverage:
