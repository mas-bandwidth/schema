# docs/USAGE.md's Python fragments (docs/PORTING.md I12): each
# `<!-- python-usage <unit> -->` fragment runs against its unit's generated
# package under both pinned interpreters, and the control plants a renamed
# documented method or runtime class in a copy of that package and requires
# the fragment to fail naming it.
PYTHON_CI_SUPPORT_GROUPS += tables-python-usage tables-python-usage-negative-control

.PHONY: tables-python-usage tables-python-usage-negative-control
tables-python-usage: python-toolchain-check

tables-python-usage-negative-control: python-toolchain-check

test-python: tables-python-usage tables-python-usage-negative-control
