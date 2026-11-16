# Python's generated-code verdict pipe and its planted refusal control.
.PHONY: tables-python-wire-fuzz tables-python-wire-fuzz-negative-control tables-python-wire-fuzz-retain-negative-control python-wirefuzz-retain-loss-negative-control python-wirefuzz-partition-negative-control python-array-widen-negative-control python-wirefuzz-ci-list

# Every row runs the full roster, enumerated mutations and the same caller's
# seed and random count. Separate rows retain complete coverage while allowing
# the CI planner to schedule the two pins, modes and arms independently.
PYTHON_WIRE_FUZZ_ROWS := $(foreach pin,current floor,$(foreach mode,normal dev,$(foreach arm,plain mixed retain,python-wirefuzz-$(pin)-$(mode)-$(arm))))
.PHONY: $(PYTHON_WIRE_FUZZ_ROWS)
python-wirefuzz-ci-list:
	@printf '%s\n' $(PYTHON_WIRE_FUZZ_ROWS)

# $(1) pin, $(2) mode, $(3) arm.
define python_wirefuzz_row
python-wirefuzz-$(1)-$(2)-$(3): build/conformance-harness build/conformance-python python-toolchain-check
	@set -eu; \
	if find build/conformance-python -name '*.refused' -print | grep .; then echo 'wire-fuzz: a corpus unit was refused'; exit 1; fi; \
	mkdir -p build/python-wirefuzz; log=build/python-wirefuzz/$(1)-$(2)-$(3).log; \
	test/conformance/wire-fuzz-complete "$$$$log" ./build/conformance-harness wire-fuzz $(if $(filter mixed,$(3)),--mixed-builder,$(if $(filter retain,$(3)),--retain,)) \
	  --driver '$(if $(filter current,$(1)),$(PYTHON),$(PYTHON_FLOOR)) $(if $(filter dev,$(2)),-X dev,) -W error -I -B test/conformance/python/wirefuzz.py build/conformance-python' \
	  --seed $(SEED) --n $(N) --failed build/python-wirefuzz/$(1)-$(2)-$(3).bin
endef
$(foreach pin,current floor,$(foreach mode,normal dev,$(foreach arm,plain mixed retain,$(eval $(call python_wirefuzz_row,$(pin),$(mode),$(arm))))))

tables-python-wire-fuzz: $(PYTHON_WIRE_FUZZ_ROWS)

tables-python-wire-fuzz-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonWireFuzzVerdictPipeNegativeControl$$' ./internal/codegen/pythontable/

tables-python-wire-fuzz-retain-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonWireFuzzRetainNegativeControl$$' ./internal/codegen/pythontable/

python-wirefuzz-partition-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonWireFuzzCIPartitionNegativeControl$$' ./internal/codegen/pythontable/

python-wirefuzz-retain-loss-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonWireFuzzRetainLoss$$' ./internal/codegen/pythontable/

python-array-widen-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonArrayWideningNegativeControl$$' ./internal/codegen/pythontable/
test-python: $(PYTHON_WIRE_FUZZ_ROWS) tables-python-wire-fuzz-negative-control tables-python-wire-fuzz-retain-negative-control python-wirefuzz-retain-loss-negative-control python-wirefuzz-partition-negative-control python-array-widen-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-array-widen-negative-control $(PYTHON_WIRE_FUZZ_ROWS) tables-python-wire-fuzz-negative-control tables-python-wire-fuzz-retain-negative-control python-wirefuzz-retain-loss-negative-control python-wirefuzz-partition-negative-control
