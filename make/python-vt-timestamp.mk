# make/python-vt-timestamp.mk — the Python leg's `timestamp` kind on the
# form-1 table wire (docs/PORT-PYTHON.md, "timestamp is the int64 it is").
#
# python-vt-timestamp: the compiler half. TestPythonCarriesTimestamp holds that
# the Python target generates the timestamp feature witness and refuses nothing
# for the construct; the proof requires its named PASS. The table half,
# TestPythonVtTimestamp, runs in test-python's SCHEMA_SLOW run of the
# pythontable package.
#
# python-vt-timestamp-negative-control: one fault per probe check is planted in
# a copy of the generated package (the kind-36 width, the kind_mismatch reason,
# the second widening direction, the cut-body reason, the int check and the
# range), and TestPythonVtTimestampNegativeControl must go red naming that
# check's case. It needs the pins, so it depends on python-toolchain-check.
.PHONY: python-vt-timestamp python-vt-timestamp-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-ci-vt-timestamp
python-ci-vt-timestamp: python-vt-timestamp python-vt-timestamp-negative-control
python-vt-timestamp:
	sh test/slowgate/proof python-vt-timestamp-compiler 'TestPythonCarriesTimestamp' -tags schema_leg_python ./compiler -run '^TestPythonCarriesTimestamp$$' -count=1
python-vt-timestamp-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run TestPythonVtTimestampNegativeControl ./internal/codegen/pythontable/
