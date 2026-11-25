# make/python-vt-pointer-arrays.mk — the Python leg's pointer arrays ([]*T,
# [N]*T, [..N]*T, [Min..N]*T) and table field guards (docs/PORT-PYTHON.md).
# Included by the Makefile's wildcard include.
#
# One negative control per check. Each runs one subtest of
# TestPythonVtPointerArraysNegativeControl, which runs its probe under both
# pinned interpreters twice: on the generated tree as emitted, where the probe
# must pass, and on a copy with exactly that check planted out of the
# runtime, where it must fail with the check's own name in its stderr.
.PHONY: python-vt-pointer-arrays-count-over-bound-negative-control \
	python-vt-pointer-arrays-read-cycle-negative-control \
	python-vt-pointer-arrays-wrong-type-target-negative-control \
	python-vt-pointer-arrays-index-past-node-table-negative-control \
	python-vt-pointer-arrays-element-kind-negative-control \
	python-vt-pointer-arrays-shared-identity-negative-control \
	python-vt-pointer-arrays-field-guard-negative-control \
	python-vt-pointer-arrays-write-not-list-negative-control \
	python-vt-pointer-arrays-write-fixed-length-negative-control \
	python-vt-pointer-arrays-write-counted-range-negative-control \
	python-vt-pointer-arrays-write-over-count-negative-control \
	python-vt-pointer-arrays-write-non-node-negative-control \
	python-vt-pointer-arrays-write-wrong-type-node-negative-control \
	python-vt-pointer-arrays-write-cycle-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-vt-pointer-arrays-count-over-bound-negative-control \
	python-vt-pointer-arrays-read-cycle-negative-control \
	python-vt-pointer-arrays-wrong-type-target-negative-control \
	python-vt-pointer-arrays-index-past-node-table-negative-control \
	python-vt-pointer-arrays-element-kind-negative-control \
	python-vt-pointer-arrays-shared-identity-negative-control \
	python-vt-pointer-arrays-field-guard-negative-control \
	python-vt-pointer-arrays-write-not-list-negative-control \
	python-vt-pointer-arrays-write-fixed-length-negative-control \
	python-vt-pointer-arrays-write-counted-range-negative-control \
	python-vt-pointer-arrays-write-over-count-negative-control \
	python-vt-pointer-arrays-write-non-node-negative-control \
	python-vt-pointer-arrays-write-wrong-type-node-negative-control \
	python-vt-pointer-arrays-write-cycle-negative-control
test-python: python-vt-pointer-arrays-count-over-bound-negative-control \
	python-vt-pointer-arrays-read-cycle-negative-control \
	python-vt-pointer-arrays-wrong-type-target-negative-control \
	python-vt-pointer-arrays-index-past-node-table-negative-control \
	python-vt-pointer-arrays-element-kind-negative-control \
	python-vt-pointer-arrays-shared-identity-negative-control \
	python-vt-pointer-arrays-field-guard-negative-control \
	python-vt-pointer-arrays-write-not-list-negative-control \
	python-vt-pointer-arrays-write-fixed-length-negative-control \
	python-vt-pointer-arrays-write-counted-range-negative-control \
	python-vt-pointer-arrays-write-over-count-negative-control \
	python-vt-pointer-arrays-write-non-node-negative-control \
	python-vt-pointer-arrays-write-wrong-type-node-negative-control \
	python-vt-pointer-arrays-write-cycle-negative-control

# the read's count-over-bound check on a bounded pointer array
python-vt-pointer-arrays-count-over-bound-negative-control:

# the read's open-node cycle refusal
python-vt-pointer-arrays-read-cycle-negative-control:

# the read's refusal of a pointer whose declared target is another table
python-vt-pointer-arrays-wrong-type-target-negative-control:

# the read's refusal of an index past the node table
python-vt-pointer-arrays-index-past-node-table-negative-control:

# the read's element-kind check (17) on a pointer array
python-vt-pointer-arrays-element-kind-negative-control:

# the save's reuse of one index for a shared node
python-vt-pointer-arrays-shared-identity-negative-control:

# the save's gating of an inactive guarded field
python-vt-pointer-arrays-field-guard-negative-control:

# the write refusal of a pointer array that is not a list
python-vt-pointer-arrays-write-not-list-negative-control:

# the write refusal of a [N]*T of the wrong length
python-vt-pointer-arrays-write-fixed-length-negative-control:

# the write refusal of a [Min..N]*T outside its range
python-vt-pointer-arrays-write-counted-range-negative-control:

# the write refusal of a []*T over the int32 count cap
python-vt-pointer-arrays-write-over-count-negative-control:

# the write refusal of an element that is not a table
python-vt-pointer-arrays-write-non-node-negative-control:

# the write refusal of an element that is another table
python-vt-pointer-arrays-write-wrong-type-node-negative-control:

# the write refusal of a pointer cycle, by its path from the root
python-vt-pointer-arrays-write-cycle-negative-control:

.PHONY: python-vt-pointer-array-body-bound-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-vt-pointer-array-body-bound-negative-control
test-python: python-vt-pointer-array-body-bound-negative-control
python-vt-pointer-array-body-bound-negative-control:
