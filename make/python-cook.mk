# Cook form's focused Python gate and planted-fault controls.
.PHONY: python-cook-test python-cook-negative-control
test-python: python-cook-test python-cook-negative-control

python-cook-test:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonCook($$|CppIdentity$$|ExtentCppIdentity$$|BlobSlotsCppIdentity$$|SharedBlobCppIdentity$$|SharedBlobSurface$$|MapBlobs$$|ForeignAndForgerySurfaces$$|BlobIdentity$$|BlobRefName$$|BlobRefWire$$|BufferShape$$|Refusals$$|OpenFunctionalScale$$|DeepDump$$|DumpArrays$$|CountedUnionPadding$$|DeepWrite$$|DeepUnionPointer$$|FixedClass$$|UnionFloat$$|UnionBlobArm$$|UnionBytesArm$$|OptionalFloat$$|OptionalFloatWire$$|OptionalScalarArrays$$|OptionalMaps$$|OptionalMapsCppIdentity$$|RequiredCppIdentity$$|RequiredRange$$|InactiveGuard$$|InactiveGuardExtent$$|InactiveGuardSharedBlob$$|PointerArrays$$|TextLists$$|Forms$$|SurfaceSelection$$|ShapeFunctional$$)' ./internal/codegen/pythontable/

python-cook-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonCook$$/^negative-controls$$' ./internal/codegen/pythontable/
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonCookRefusals$$' ./internal/codegen/pythontable/
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonCookOpenFunctionalScale$$' ./internal/codegen/pythontable/
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonCookForeignAndForgerySurfaces$$|^TestPythonCookBlobIdentity$$|^TestPythonCookBlobRefWire$$|^TestPythonCookPointerArrays$$|^TestPythonCookTextLists$$|^TestPythonCookForms$$|^TestPythonCookSurfaceSelection$$|^TestPythonCookOptionalScalarArrays$$|^TestPythonCookOptionalMaps$$|^TestPythonCookRequiredRange$$|^TestPythonCookShapeFunctional$$|^TestPythonCookUnionBlobArm$$|^TestPythonCookUnionBytesArm$$|^TestPythonCookSharedBlobCppIdentity$$|^TestPythonCookSharedBlobSurface$$|^TestPythonCookMapBlobs$$|^TestPythonCookBufferShape$$|^TestPythonCookDeepDump$$|^TestPythonCookDeepUnionPointer$$|^TestPythonCookDumpArrays$$|^TestPythonCookCountedUnionPadding$$|^TestPythonCookFixedClass$$|^TestPythonCookUnionFloat$$|^TestPythonCookOptionalFloat$$' ./internal/codegen/pythontable/
