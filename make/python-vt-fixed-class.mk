# make/python-vt-fixed-class.mk — the Python table leg's fixed-class kind
# controls (docs/PORT-PYTHON.md, THE TABLE FRAME). Included by the Makefile's
# wildcard include; the stem has a dash, so it is a support fragment and never
# probed as a --lang port.
#
# One array body's count is moved in a copy of the generated runtime, and
# TestPythonVtFixedClassNegativeControl must go red naming
# vt_fixed_class_roundtrip; each by-value elision, float, range,
# deprecated-variant, subclass and equality rule is broken in its own copy, and
# TestPythonVtFixedClassElisionNegativeControl must go red naming that case.
test-python: python-vt-fixed-class-negative-control

.PHONY: python-vt-fixed-class-negative-control
python-vt-fixed-class-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run 'TestPythonVtFixedClass(Elision)?NegativeControl' ./internal/codegen/pythontable/
