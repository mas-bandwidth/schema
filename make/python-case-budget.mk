# The complete local tests remain unsplit. CI delegates their actual cases and
# phases so cold Go compilation plus a selected test fits the two-minute cap.
PYTHON_CI_SUPPORT_GROUPS += python-array-widening-0 python-array-widening-1 python-array-widening-2 python-pointer-arrays-functional python-pointer-arrays-deep-current python-pointer-arrays-deep-floor python-pointer-arrays-typing
.PHONY: python-array-widening python-array-widening-0 python-array-widening-1 python-array-widening-2 python-pointer-array-phases python-pointer-arrays-functional python-pointer-arrays-deep python-pointer-arrays-deep-current python-pointer-arrays-deep-floor python-pointer-arrays-typing
python-array-widening: python-array-widening-0 python-array-widening-1 python-array-widening-2
python-pointer-array-phases: python-pointer-arrays-functional python-pointer-arrays-deep python-pointer-arrays-typing
python-array-widening-0: python-toolchain-check
python-array-widening-1: python-toolchain-check
python-array-widening-2: python-toolchain-check
python-pointer-arrays-functional: python-toolchain-check
python-pointer-arrays-deep: python-pointer-arrays-deep-current python-pointer-arrays-deep-floor
python-pointer-arrays-deep-current: python-toolchain-check
python-pointer-arrays-deep-floor: python-toolchain-check
python-pointer-arrays-typing: python-toolchain-check
test-python: python-array-widening-0 python-array-widening-1 python-array-widening-2 python-pointer-arrays-functional python-pointer-arrays-deep-current python-pointer-arrays-deep-floor python-pointer-arrays-typing
