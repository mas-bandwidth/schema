# make/python-vt-unbounded.mk — the Python table leg's unbounded arrays
# (docs/PORT-PYTHON.md). Included by the Makefile's wildcard include; the stem
# has a dash, so it is a support fragment and never probed as a --lang port.
#
# The unbounded array's negative controls: each read refusal of the array
# codec, the write's element validation and its length cap are removed one at
# a time from a COPY of the generated runtime, and the gate case that watches
# each must go red by name. The driver's control removes the list-carrier
# registration from a COPY of the compiler file through `go test -overlay`, so
# the driver-level gate sees the construct refused again. Both need the pins or
# the tagged compiler, so test-python runs them beside the table tests, and
# make/negative-controls.d schedules the runtime one in the python-table-core
# group.
test-python: python-vt-unbounded-negative-control python-vt-unbounded-driver-negative-control

.PHONY: python-vt-unbounded-negative-control
python-vt-unbounded-negative-control:
	SCHEMA_SLOW=1 go test -tags schema_leg_python -run 'TestPythonVtUnboundedNegativeControl' ./internal/codegen/pythontable/ -count=1

# python-vt-unbounded-driver-negative-control: the driver's list-carrier
# registration (compiler/carries_python_unbounded.go) is removed from a COPY
# through `go test -overlay`, and TestPythonTableDriverCarriesUnboundedArrays
# must go red naming the refusal.
python-vt-unbounded-driver-negative-control:
	@rm -rf build/python-vt-unbounded-driver-nc && mkdir -p build/python-vt-unbounded-driver-nc
	@sed 's|registerListCarrier("python")|_ = 0 // SABOTAGED: no list carrier|' \
		compiler/carries_python_unbounded.go > build/python-vt-unbounded-driver-nc/carries.go.txt
	@grep -q 'SABOTAGED: no list carrier' build/python-vt-unbounded-driver-nc/carries.go.txt || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/compiler/carries_python_unbounded.go":"%s/build/python-vt-unbounded-driver-nc/carries.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/python-vt-unbounded-driver-nc/overlay.json
	@if go test -tags schema_leg_python -overlay build/python-vt-unbounded-driver-nc/overlay.json \
			-count=1 ./compiler -run '^TestPythonTableDriverCarriesUnboundedArrays$$' \
			> build/python-vt-unbounded-driver-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: TestPythonTableDriverCarriesUnboundedArrays stayed green under the sabotage"; \
		cat build/python-vt-unbounded-driver-nc/log; exit 1; \
	fi
	@grep -q -- '--- FAIL: TestPythonTableDriverCarriesUnboundedArrays' build/python-vt-unbounded-driver-nc/log && \
		grep -qF 'the driver refused a []T table unit' build/python-vt-unbounded-driver-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on the driver refusal"; \
		cat build/python-vt-unbounded-driver-nc/log; exit 1; }
	@grep -m1 -F 'the driver refused a []T table unit' build/python-vt-unbounded-driver-nc/log
	@echo "negative control: without the list-carrier registration the driver refuses a []T unit"
