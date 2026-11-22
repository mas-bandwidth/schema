# The complete retention suite stays one local target, python-retain. CI runs
# it as four bounded rows so cold Go compilation plus one slice fits the
# two-minute per-commit cap; each row carries about a quarter of the heavy
# interpreter-plus-mypy tests. internal/ci's TestPythonRetainCIPlan proves that
# every ^TestPythonRetain function in the package source is selected by exactly
# one row, so a new retention test fails ci-policy until it is given a row.
PYTHON_RETAIN_SHARDS := $(addprefix python-retain-,0 1 2 3)
PYTHON_CI_SUPPORT_GROUPS += $(PYTHON_RETAIN_SHARDS) python-retain-negative-control
.PHONY: python-retain $(PYTHON_RETAIN_SHARDS) python-retain-negative-control
python-retain: $(PYTHON_RETAIN_SHARDS)
python-retain-0: dist-python
	SCHEMA_SLOW=1 $(GOTESTRUN) -tags schema_leg_python -count=1 -timeout 600s -run '^TestPythonRetain(CRC|RequiredDefault|RoundTrip|SurfaceSelection|Names|ResolverNegativeControl|Capture|ManifestSave|OutputAllocationShape|DirectFrameBoundaries|PublicNegativeControl|DriverNegativeControl|ReadSurface|ReadSurfaceNegativeControl)$$' ./internal/codegen/pythontable/
python-retain-1: dist-python
	SCHEMA_SLOW=1 $(GOTESTRUN) -tags schema_leg_python -count=1 -timeout 600s -run '^TestPythonRetain(ResolvedPayload|PathIndex|Tail|RequiredContainers|PathIndexNegativeControl|TailNegativeControl|NodeRemapNegativeControl|TotalIDKnownLast|TotalIDDroppedDefaultSlot|KnownIDNegativeControl|KnownOnlyIDOverflow|MapEntryIDBudget|KnownKeyedIDLimit)$$' ./internal/codegen/pythontable/
python-retain-2: dist-python
	SCHEMA_SLOW=1 $(GOTESTRUN) -tags schema_leg_python -count=1 -timeout 600s -run '^TestPythonRetain(UnionArrayCapture|RootBodyInternal|TableArrayInternal|EnumArrayCapture|NestedBodyInternal|NodeBodyInternal|CrossVersionInternal|UnionEnumArrayArm|UnionUnionArrayArm|OptionalEnumArray|OptionalTableArray|PointerArrayIdentity|ResetsBeforeOpen|RefusalEmptiesTheStore)$$' ./internal/codegen/pythontable/
python-retain-3: dist-python
	SCHEMA_SLOW=1 $(GOTESTRUN) -tags schema_leg_python -count=1 -timeout 600s -run '^TestPythonRetain(KeyedAndMapInternal|FixedRefusal|SelectedRuntime|OptionalMap|KeyedEnum|FixedArrayGrowth|ArrayWideningReport|GuardedBody|Depth128|ZeroIDField|MapNestedZeroHash|MapNestedZeroHashNegativeControl|ContainerWidening|ZeroIDNegativeControl)$$' ./internal/codegen/pythontable/
python-retain-negative-control: dist-python
	SCHEMA_SLOW=1 $(GOTESTRUN) -tags schema_leg_python -count=1 -timeout 600s -run '^TestPythonRetain(ResolverNegativeControl|PathIndexNegativeControl|NodeRemapNegativeControl|KnownIDNegativeControl|TailNegativeControl|PublicNegativeControl|DriverNegativeControl|ReadSurfaceNegativeControl|MapNestedZeroHashNegativeControl|ZeroIDNegativeControl)$$' ./internal/codegen/pythontable/
test-python: $(PYTHON_RETAIN_SHARDS) python-retain-negative-control
