# The complete local tests remain unsplit. CI delegates their actual cases and
# phases so cold Go compilation plus a selected test fits the two-minute cap.
PYTHON_CI_SUPPORT_GROUPS += python-array-widening-0 python-array-widening-1 python-array-widening-2 python-pointer-arrays-functional python-pointer-arrays-deep-current python-pointer-arrays-deep-floor python-pointer-arrays-typing
.PHONY: python-array-widening python-array-widening-0 python-array-widening-1 python-array-widening-2 python-pointer-array-phases python-pointer-arrays-functional python-pointer-arrays-deep python-pointer-arrays-deep-current python-pointer-arrays-deep-floor python-pointer-arrays-typing
python-array-widening: python-array-widening-0 python-array-widening-1 python-array-widening-2
python-pointer-array-phases: python-pointer-arrays-functional python-pointer-arrays-deep python-pointer-arrays-typing
python-array-widening-0: python-toolchain-check
	SCHEMA_SLOW=1 $(GOTESTRUN) -v -count=1 -timeout 120s -tags schema_leg_python -run '^TestPythonArrayWidening$$/(int8_to_int16|int16_to_int32|int32_to_int64)$$' ./internal/codegen/pythontable
python-array-widening-1: python-toolchain-check
	SCHEMA_SLOW=1 $(GOTESTRUN) -v -count=1 -timeout 120s -tags schema_leg_python -run '^TestPythonArrayWidening$$/(int64_to_int128|uint8_to_uint16|uint16_to_uint32)$$' ./internal/codegen/pythontable
python-array-widening-2: python-toolchain-check
	SCHEMA_SLOW=1 $(GOTESTRUN) -v -count=1 -timeout 120s -tags schema_leg_python -run '^TestPythonArrayWidening$$/(uint32_to_uint64|uint64_to_uint128|float32_to_float64)$$' ./internal/codegen/pythontable
python-pointer-arrays-functional: python-toolchain-check
	SCHEMA_SLOW=1 SCHEMA_PYTHON_POINTER_ARRAY_PHASE=functional $(GOTESTRUN) -v -count=1 -timeout 120s -tags schema_leg_python -run '^TestPythonVtPointerArrays$$/functional$$' ./internal/codegen/pythontable
python-pointer-arrays-deep: python-pointer-arrays-deep-current python-pointer-arrays-deep-floor
python-pointer-arrays-deep-current: python-toolchain-check
	SCHEMA_SLOW=1 SCHEMA_PYTHON_POINTER_ARRAY_PHASE=deep $(GOTESTRUN) -v -count=1 -timeout 120s -tags schema_leg_python -run '^TestPythonVtPointerArrays$$/deep$$/current$$' ./internal/codegen/pythontable
python-pointer-arrays-deep-floor: python-toolchain-check
	SCHEMA_SLOW=1 SCHEMA_PYTHON_POINTER_ARRAY_PHASE=deep $(GOTESTRUN) -v -count=1 -timeout 120s -tags schema_leg_python -run '^TestPythonVtPointerArrays$$/deep$$/floor$$' ./internal/codegen/pythontable
python-pointer-arrays-typing: python-toolchain-check
	SCHEMA_SLOW=1 SCHEMA_PYTHON_POINTER_ARRAY_PHASE=typing $(GOTESTRUN) -v -count=1 -timeout 120s -tags schema_leg_python -run '^TestPythonVtPointerArrays$$/typing$$' ./internal/codegen/pythontable
test-python: python-array-widening-0 python-array-widening-1 python-array-widening-2 python-pointer-arrays-functional python-pointer-arrays-deep-current python-pointer-arrays-deep-floor python-pointer-arrays-typing
