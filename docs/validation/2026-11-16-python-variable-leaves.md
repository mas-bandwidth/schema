# Python variable-table leaf qualification

This is evidence for 30 variable leaves, excluding the separately held wire-fuzzer leaf. The source is a stacked candidate, not a claim that its prerequisites have landed.

Source stack: `26536e381e8c1549c2e2fbce4191a9cdab2e222b` = Python acceptance `456fd42a013086e2ad9edb5c16475c750033b826`, reviewed public manifest bindings `8ed3e3bc457b186b4fbe4205cbf7c9d6366dc454`, flags field witness `404d54bf791cdd907ef81ab28c40deee17f21527`, and cards proof `a9e655e9f3b547e6b6b80dd9062bd693fada65e1`. The compiler, Python emitter and runtime are byte-identical between 456 and 265; within those paths the only additions are the blob-driver, cards and flags test files. Manifest-binding paths equal 8ed and cards-owned files equal a9 byte-for-byte.

Linux amd-3, pinned CPython 3.11.18 and 3.15.2, mypy 1.18.2 and default nova-reserve. The unfiltered Python/pythontable package at 456 exited 0: 1,574 passing test/subtest events and zero failing or skipped tests; the pyrun package reports no test files. A per-leaf event audit found every named test present in the 456 package passing; the separately added cards and flags tests were validated on 265, and the blob-driver tests by the binding controls below. Full JSON SHA-256 `a8757416837d8c8f993be737be0d4d50ba5a56623be2dd37fbae5917a52f411a`.

The 21-surface public manifest passed on both pins at 8ed, including wire 116/116, report 123/123, framing 102/102, message 13/13, retain 8/8 and retain-save 7/7, JSON read/write/hostile 108/108, 110/110 and 155/155, cook-write 142/142, and all remaining surfaces. The fixed-only message-inapplicability and the two actual blob_shared refusals are the three separately proved protocol artifacts, not blanket omissions. Framing, message applicability and blob public-reader/writer/driver positive and planted-fault controls passed on 265 with the 8ed-generated corpus. Manifest log SHA-256: CPython 3.11 `211e9cfb4724d9d72ebb35beec0248e249ec305337e358b32c1672c14aab0311`; CPython 3.15 `ed859c7b7a857796728280dd3fe52681cb4e947a69dc14320089064f5e435ff2`.

Additional compiler gates on 456 plus test-only 404: reflection public corpus, all kinds, packet-only, name collision and two negative controls; evolution old-writer C++/Go/Python parity, ID and rename controls; optional-variable carry and its planted control; 58 unsharded required-public-shape cases (the union of 16 CI partitions), timestamp carry and hosted/optional-float controls. All passed. `TestPythonReflectionSurfaceSelection` separately passed at source `f5acc48a62f69fd53075d31ebb13d0d6dd313d35` (reflection source unchanged from 265; the only runtime delta is the reviewed keyed-flags codec). An initial isolated attempt lacked pinned MYPY; the affected evolution case passed after using the existing pinned executable. The initial unpinned flags attempt likewise failed for missing PYTHON; the exact 404 test passed with both pins. Both setup failures are preserved in the receipts.

Cards: the nine surfaces and four facade controls passed on both pins at reviewed a9, including public C++/Go identity, mypy and CR/deadline refusal checks. All `TestPythonCards*` and the flags-field witness then passed on the assembled 265 source; the negative-control registry passed. These checks do not substitute for the unrelated Python wire-fuzz qualification.

The final assembled source `f5acc48a62f69fd53075d31ebb13d0d6dd313d35` adds the reviewed keyed-flags carrier and focused array/keyed/union/optional-array flags witnesses to 265. Its fresh unfiltered Python package passed with no test failures or skips; full JSON SHA-256 `f9a4d82176dffc4a6b783f37be69f132f986f7ef4403762d4416fe0f8f9f2f6f`. The unchanged standard manifest has no keyed-flags field; public keyed-driver, both-pin keyed reader/writer/refusal and independent historical widening witnesses cover that added shape directly. `TestPythonReflectionSurfaceSelection` also passed on f5.

The proof names below identify the source tests for each leaf. They are supplemented by the full package and public surfaces above; compiler-only tests are identified by their path.

## port/python/vt/fixed-class

- `internal/codegen/pythontable/vtfixedclass_test.go: TestPythonVtFixedClass`
- `internal/codegen/pythontable/vtfixedclass_test.go: TestPythonVtFixedClassNegativeControl`
- `internal/codegen/pythontable/vtfixedclass_test.go: TestPythonVtFixedClassBuildVersion`
- `internal/codegen/pythontable/vtfixedclass_test.go: TestPythonVtFixedClassBuildVersionImports`

## port/python/vt/optional-fields

- `internal/codegen/pythontable/vtcells_optional_test.go: TestPythonVtOptional`
- `internal/codegen/pythontable/vtcells_optional_test.go: TestPythonVtOptionalNegativeControl`
- `internal/codegen/pythontable/vtcells_optional_test.go: TestPythonVtOptionalArrays`
- `internal/codegen/pythontable/vtcells_optional_test.go: TestPythonVtOptionalArraysNegativeControl`
- `internal/codegen/pythontable/required_form1_test.go: TestPythonForm1RequiredRemoval`

## port/python/vt/text-form/hand-text

- `internal/codegen/pythontable/handtext_test.go: TestPythonHandtextArrayRoot`
- `internal/codegen/pythontable/handtext_test.go: TestPythonHandtextArrayRootNegativeControl`
- `internal/codegen/pythontable/handtext_test.go: TestPythonHandtextCombinedEditNegativeControl`
- `internal/codegen/pythontable/handtext_test.go: TestPythonHandtextCorpus`
- `internal/codegen/pythontable/handtext_reference_test.go: TestPythonHandtextCppCorpusFunctional`
- `internal/codegen/pythontable/handtext_test.go: TestPythonHandtextDottedKey`
- `internal/codegen/pythontable/handtext_test.go: TestPythonHandtextEditPathNegativeControl`
- `internal/codegen/pythontable/handtext_reference_test.go: TestPythonHandtextFixedCarrierCRCFunctional`
- `internal/codegen/pythontable/handtext_reference_test.go: TestPythonHandtextGoCorpusFunctional`
- `internal/codegen/pythontable/handtext_test.go: TestPythonHandtextKindArrays`
- `internal/codegen/pythontable/handtext_test.go: TestPythonHandtextKindsNegativeControl`
- `internal/codegen/pythontable/handtext_test.go: TestPythonHandtextNegativeControl`
- `internal/codegen/pythontable/handtext_reference_test.go: TestPythonHandtextReferencesFunctional`
- `internal/codegen/pythontable/handtext_test.go: TestPythonHandtextReservedName`
- `internal/codegen/pythontable/handtext_test.go: TestPythonHandtextResolution`
- `internal/codegen/pythontable/handtext_test.go: TestPythonHandtextSurfaceNegativeControl`
- `internal/codegen/pythontable/handtext_test.go: TestPythonHandtextSurfaceSelection`
- `internal/codegen/pythontable/handtext_test.go: TestPythonHandtextSyntax`
- `internal/codegen/pythontable/handtext_test.go: TestPythonHandtextUnbounded`
- `internal/codegen/pythontable/handtext_reference_test.go: TestPythonHandtextUnboundedReferencesFunctional`
- `internal/codegen/pythontable/handtext_test.go: TestPythonHandtextUnionBlobs`
- `internal/codegen/pythontable/handtext_test.go: TestPythonHandtextUnionBlobsNegativeControl`
- `internal/codegen/pythontable/handtext_test.go: TestPythonHandtextWireCarrier`
- `internal/codegen/pythontable/handtext_test.go: TestPythonHandtextWireCarrierCRC`
- `internal/codegen/pythontable/handtext_test.go: TestPythonHandtextWireCarrierCRCNegativeControl`
- `internal/codegen/pythontable/handtext_test.go: TestPythonHandtextWireCarrierNegativeControl`

## port/python/vt/text-form/json

- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonArrayRoot`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonBlobRef`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonBlobs`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonConformanceDriver`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonCountedMinimum`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonDeclaredForms`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonDepthAgainstEngine`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonEngineAndWire`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonFieldKeyAndRequired`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonFieldMatchNegativeControl`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonFixedClassIntegration`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonFixedDecimal`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonFixedGrammarCppOracle`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonFixedTextGrammarAgainstEngine`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonFloat32AgainstEngine`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonFloat64AgainstEngine`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonFloatRounding`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonGeneratedWriterMatchesWalk`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonHostedPacketKeyed`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonKeyedEnumAgainstEngine`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonKeyedWideAndVocabulary`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonList`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonMapKeyedValuesAgainstEngine`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonMapWideValue`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonNullableTextList`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonOptionalMapsAgainstEngine`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonOptionalWide`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonPointerArraysAgainstEngine`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonPublicMethodsAndNames`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonScalarRoundTrip`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonScan`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonScanPolicy`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonShapeFunctionalBench`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonSharedNodes`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonSharedNodesThroughMap`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonSharedNodesThroughUnion`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonTextAndBytes`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonTextList`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonTimestamp`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonUnboundedMapKeys`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonUnboundedNullableTextList`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonUnionArray`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonUnionBlobAgainstEngine`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonUnionWide`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonVocabularyAgainstEngine`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonWideCorpus`

## port/python/vt/reflection

- `internal/codegen/pythontable/reflection_test.go: TestPythonReflection`
- `internal/codegen/pythontable/reflection_layout_test.go: TestPythonReflectionAccessorNegativeControl`
- `internal/codegen/pythontable/reflection_layout_test.go: TestPythonReflectionAccessors`
- `internal/codegen/pythontable/reflection_docs_test.go: TestPythonReflectionAnnotationAgreement`
- `internal/codegen/pythontable/reflection_docs_test.go: TestPythonReflectionAnnotationAgreementNegativeControl`
- `internal/codegen/pythontable/reflection_docs_test.go: TestPythonReflectionAnnotationEscapes`
- `internal/codegen/pythontable/reflection_corpus_test.go: TestPythonReflectionCorpus`
- `internal/codegen/pythontable/reflection_corpus_test.go: TestPythonReflectionCorpusNegativeControl`
- `internal/codegen/pythontable/reflection_docs_test.go: TestPythonReflectionDescriptorDocs`
- `internal/codegen/pythontable/reflection_docs_test.go: TestPythonReflectionDescriptorDocsNegativeControl`
- `internal/codegen/pythontable/reflection_corpus_test.go: TestPythonReflectionDetachedVocabularyIDs`
- `compiler/pythonleg_reflection_on_test.go: TestPythonReflectionDriverAllKinds`
- `compiler/pythonleg_reflection_on_test.go: TestPythonReflectionDriverCorpus`
- `compiler/pythonleg_reflection_on_test.go: TestPythonReflectionDriverNameCollision`
- `compiler/pythonleg_reflection_on_test.go: TestPythonReflectionDriverNegativeControl`
- `compiler/pythonleg_reflection_on_test.go: TestPythonReflectionDriverPacketOnly`
- `compiler/pythonleg_reflection_on_test.go: TestPythonReflectionDriverPacketUnionNegativeControl`
- `internal/codegen/pythontable/reflection_docs_test.go: TestPythonReflectionFieldVocabulary`
- `internal/codegen/pythontable/reflection_docs_test.go: TestPythonReflectionFieldVocabularyNegativeControl`
- `internal/codegen/pythontable/reflection_corpus_test.go: TestPythonReflectionKindHoleNegativeControl`
- `internal/codegen/pythontable/reflection_layout_test.go: TestPythonReflectionLayout`
- `internal/codegen/pythontable/reflection_layout_test.go: TestPythonReflectionLayoutNegativeControl`
- `internal/codegen/pythontable/reflection_layout_test.go: TestPythonReflectionLiveIdentityAndReset`
- `internal/codegen/pythontable/reflection_layout_test.go: TestPythonReflectionResetLargeArrays`
- `internal/codegen/pythontable/reflection_layout_test.go: TestPythonReflectionResetLargeArraysNegativeControl`
- `internal/codegen/pythontable/reflection_corpus_test.go: TestPythonReflectionStrictTypes`
- `compiler/python_surface_behavior_test.go: TestPythonReflectionSurfaceSelection`
- `internal/codegen/pythontable/reflection_corpus_test.go: TestPythonReflectionViewIsLazy`
- `internal/codegen/pythontable/reflection_corpus_test.go: TestPythonReflectionVocabularyNegativeControl`

## port/python/vt/block-read

- `internal/codegen/pythontable/block_test.go: TestPythonBlockRead`
- `internal/codegen/pythontable/block_test.go: TestPythonBlockReadDamaged`
- `internal/codegen/pythontable/block_test.go: TestPythonBlockReadNativeOrder`
- `internal/codegen/pythontable/block_test.go: TestPythonBlockReadNegativeControls`
- `internal/codegen/pythontable/block_test.go: TestPythonBlockReadOppositeOrder`
- `internal/codegen/pythontable/blockbuild_test.go: TestPythonBlockBuilder`
- `internal/codegen/pythontable/blockbuild_test.go: TestPythonBlockBuilderNegativeControls`
- `internal/codegen/pythontable/block_cpp_test.go: TestPythonBlockCppIdentity`
- `internal/codegen/pythontable/forms_metadata_test.go: TestPythonFormsBlockMetadata`
- `internal/codegen/pythontable/forms_metadata_test.go: TestPythonFormsBlockMetadataNegativeControl`

## port/python/vt/cook-open

- `internal/codegen/pythontable/cook_test.go: TestPythonCook`
- `internal/codegen/pythontable/cook_blob_identity_test.go: TestPythonCookBlobIdentity`
- `internal/codegen/pythontable/cook_blob_name_test.go: TestPythonCookBlobRefName`
- `internal/codegen/pythontable/cook_blob_wire_test.go: TestPythonCookBlobRefWire`
- `internal/codegen/pythontable/cook_blob_cpp_test.go: TestPythonCookBlobSlotsCppIdentity`
- `internal/codegen/pythontable/cook_buffer_test.go: TestPythonCookBufferShape`
- `internal/codegen/pythontable/cook_padding_test.go: TestPythonCookCountedUnionPadding`
- `internal/codegen/pythontable/cook_cpp_test.go: TestPythonCookCppIdentity`
- `internal/codegen/pythontable/cook_deep_test.go: TestPythonCookDeepDump`
- `internal/codegen/pythontable/cook_deep_test.go: TestPythonCookDeepUnionPointer`
- `internal/codegen/pythontable/cook_deep_test.go: TestPythonCookDeepWrite`
- `internal/codegen/pythontable/cook_dump_arrays_test.go: TestPythonCookDumpArrays`
- `internal/codegen/pythontable/cook_extent_cpp_test.go: TestPythonCookExtentCppIdentity`
- `internal/codegen/pythontable/cook_fixedclass_test.go: TestPythonCookFixedClass`
- `internal/codegen/pythontable/cook_surfaces_test.go: TestPythonCookForeignAndForgerySurfaces`
- `internal/codegen/pythontable/cook_forms_test.go: TestPythonCookForms`
- `internal/codegen/pythontable/cook_guard_test.go: TestPythonCookInactiveGuard`
- `internal/codegen/pythontable/cook_guard_test.go: TestPythonCookInactiveGuardExtent`
- `internal/codegen/pythontable/cook_guard_test.go: TestPythonCookInactiveGuardSharedBlob`
- `internal/codegen/pythontable/cook_guard_storage_test.go: TestPythonCookInactiveGuardStorage`
- `internal/codegen/pythontable/cook_map_blobs_test.go: TestPythonCookMapBlobs`
- `internal/codegen/pythontable/cook_open_scale_test.go: TestPythonCookOpenFunctionalScale`
- `internal/codegen/pythontable/cook_optional_float_test.go: TestPythonCookOptionalFloat`
- `internal/codegen/pythontable/cook_optional_float_test.go: TestPythonCookOptionalFloatWire`
- `internal/codegen/pythontable/cook_optional_map_test.go: TestPythonCookOptionalMaps`
- `internal/codegen/pythontable/cook_optional_map_cpp_test.go: TestPythonCookOptionalMapsCppIdentity`
- `internal/codegen/pythontable/cook_optional_scalar_array_test.go: TestPythonCookOptionalScalarArrays`
- `internal/codegen/pythontable/cook_pointer_array_test.go: TestPythonCookPointerArrays`
- `internal/codegen/pythontable/cook_refusal_test.go: TestPythonCookRefusals`
- `internal/codegen/pythontable/cook_required_cpp_test.go: TestPythonCookRequiredCppIdentity`
- `internal/codegen/pythontable/cook_required_test.go: TestPythonCookRequiredRange`
- `internal/codegen/pythontable/cook_shape_test.go: TestPythonCookShapeFunctional`
- `internal/codegen/pythontable/cook_shared_blob_cpp_test.go: TestPythonCookSharedBlobCppIdentity`
- `internal/codegen/pythontable/cook_shared_surface_test.go: TestPythonCookSharedBlobSurface`
- `internal/codegen/pythontable/cook_forms_test.go: TestPythonCookSurfaceSelection`
- `internal/codegen/pythontable/cook_text_list_test.go: TestPythonCookTextLists`
- `internal/codegen/pythontable/cook_union_blob_test.go: TestPythonCookUnionBlobArm`
- `internal/codegen/pythontable/cook_union_blob_test.go: TestPythonCookUnionBytesArm`
- `internal/codegen/pythontable/cook_union_float_test.go: TestPythonCookUnionFloat`

## port/python/vt/build-version

- `internal/codegen/pythontable/vtfixedclass_test.go: TestPythonVtFixedClass`
- `internal/codegen/pythontable/vtfixedclass_test.go: TestPythonVtFixedClassNegativeControl`
- `internal/codegen/pythontable/vtfixedclass_test.go: TestPythonVtFixedClassBuildVersion`
- `internal/codegen/pythontable/vtfixedclass_test.go: TestPythonVtFixedClassBuildVersionImports`

## port/python/vt/variable-class

- `internal/codegen/pythontable/pointerarrays_test.go: TestPythonVtPointerArrays`
- `internal/codegen/pythontable/pointerarrays_cpp_test.go: TestPythonVtPointerArraysCppIdentity`
- `internal/codegen/pythontable/pointerarrays_bodybound_test.go: TestPythonVtPointerArrayBodyBound`
- `internal/codegen/pythontable/pointerarrays_test.go: TestPythonVtPointerArraysNegativeControl`

## port/python/vt/text-form-variable

- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonArrayRoot`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonBlobRef`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonBlobs`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonConformanceDriver`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonCountedMinimum`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonDeclaredForms`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonDepthAgainstEngine`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonEngineAndWire`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonFieldKeyAndRequired`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonFieldMatchNegativeControl`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonFixedClassIntegration`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonFixedDecimal`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonFixedGrammarCppOracle`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonFixedTextGrammarAgainstEngine`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonFloat32AgainstEngine`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonFloat64AgainstEngine`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonFloatRounding`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonGeneratedWriterMatchesWalk`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonHostedPacketKeyed`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonKeyedEnumAgainstEngine`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonKeyedWideAndVocabulary`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonList`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonMapKeyedValuesAgainstEngine`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonMapWideValue`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonNullableTextList`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonOptionalMapsAgainstEngine`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonOptionalWide`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonPointerArraysAgainstEngine`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonPublicMethodsAndNames`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonScalarRoundTrip`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonScan`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonScanPolicy`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonShapeFunctionalBench`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonSharedNodes`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonSharedNodesThroughMap`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonSharedNodesThroughUnion`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonTextAndBytes`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonTextList`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonTimestamp`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonUnboundedMapKeys`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonUnboundedNullableTextList`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonUnionArray`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonUnionBlobAgainstEngine`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonUnionWide`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonVocabularyAgainstEngine`
- `internal/codegen/pythontable/json_scan_test.go: TestPythonJsonWideCorpus`

## port/python/vt/block-build

- `internal/codegen/pythontable/block_test.go: TestPythonBlockRead`
- `internal/codegen/pythontable/block_test.go: TestPythonBlockReadDamaged`
- `internal/codegen/pythontable/block_test.go: TestPythonBlockReadNativeOrder`
- `internal/codegen/pythontable/block_test.go: TestPythonBlockReadNegativeControls`
- `internal/codegen/pythontable/block_test.go: TestPythonBlockReadOppositeOrder`
- `internal/codegen/pythontable/blockbuild_test.go: TestPythonBlockBuilder`
- `internal/codegen/pythontable/blockbuild_test.go: TestPythonBlockBuilderNegativeControls`
- `internal/codegen/pythontable/block_cpp_test.go: TestPythonBlockCppIdentity`
- `internal/codegen/pythontable/forms_metadata_test.go: TestPythonFormsBlockMetadata`
- `internal/codegen/pythontable/forms_metadata_test.go: TestPythonFormsBlockMetadataNegativeControl`

## port/python/vt/cook-write

- `internal/codegen/pythontable/cook_test.go: TestPythonCook`
- `internal/codegen/pythontable/cook_blob_identity_test.go: TestPythonCookBlobIdentity`
- `internal/codegen/pythontable/cook_blob_name_test.go: TestPythonCookBlobRefName`
- `internal/codegen/pythontable/cook_blob_wire_test.go: TestPythonCookBlobRefWire`
- `internal/codegen/pythontable/cook_blob_cpp_test.go: TestPythonCookBlobSlotsCppIdentity`
- `internal/codegen/pythontable/cook_buffer_test.go: TestPythonCookBufferShape`
- `internal/codegen/pythontable/cook_padding_test.go: TestPythonCookCountedUnionPadding`
- `internal/codegen/pythontable/cook_cpp_test.go: TestPythonCookCppIdentity`
- `internal/codegen/pythontable/cook_deep_test.go: TestPythonCookDeepDump`
- `internal/codegen/pythontable/cook_deep_test.go: TestPythonCookDeepUnionPointer`
- `internal/codegen/pythontable/cook_deep_test.go: TestPythonCookDeepWrite`
- `internal/codegen/pythontable/cook_dump_arrays_test.go: TestPythonCookDumpArrays`
- `internal/codegen/pythontable/cook_extent_cpp_test.go: TestPythonCookExtentCppIdentity`
- `internal/codegen/pythontable/cook_fixedclass_test.go: TestPythonCookFixedClass`
- `internal/codegen/pythontable/cook_surfaces_test.go: TestPythonCookForeignAndForgerySurfaces`
- `internal/codegen/pythontable/cook_forms_test.go: TestPythonCookForms`
- `internal/codegen/pythontable/cook_guard_test.go: TestPythonCookInactiveGuard`
- `internal/codegen/pythontable/cook_guard_test.go: TestPythonCookInactiveGuardExtent`
- `internal/codegen/pythontable/cook_guard_test.go: TestPythonCookInactiveGuardSharedBlob`
- `internal/codegen/pythontable/cook_guard_storage_test.go: TestPythonCookInactiveGuardStorage`
- `internal/codegen/pythontable/cook_map_blobs_test.go: TestPythonCookMapBlobs`
- `internal/codegen/pythontable/cook_open_scale_test.go: TestPythonCookOpenFunctionalScale`
- `internal/codegen/pythontable/cook_optional_float_test.go: TestPythonCookOptionalFloat`
- `internal/codegen/pythontable/cook_optional_float_test.go: TestPythonCookOptionalFloatWire`
- `internal/codegen/pythontable/cook_optional_map_test.go: TestPythonCookOptionalMaps`
- `internal/codegen/pythontable/cook_optional_map_cpp_test.go: TestPythonCookOptionalMapsCppIdentity`
- `internal/codegen/pythontable/cook_optional_scalar_array_test.go: TestPythonCookOptionalScalarArrays`
- `internal/codegen/pythontable/cook_pointer_array_test.go: TestPythonCookPointerArrays`
- `internal/codegen/pythontable/cook_refusal_test.go: TestPythonCookRefusals`
- `internal/codegen/pythontable/cook_required_cpp_test.go: TestPythonCookRequiredCppIdentity`
- `internal/codegen/pythontable/cook_required_test.go: TestPythonCookRequiredRange`
- `internal/codegen/pythontable/cook_shape_test.go: TestPythonCookShapeFunctional`
- `internal/codegen/pythontable/cook_shared_blob_cpp_test.go: TestPythonCookSharedBlobCppIdentity`
- `internal/codegen/pythontable/cook_shared_surface_test.go: TestPythonCookSharedBlobSurface`
- `internal/codegen/pythontable/cook_forms_test.go: TestPythonCookSurfaceSelection`
- `internal/codegen/pythontable/cook_text_list_test.go: TestPythonCookTextLists`
- `internal/codegen/pythontable/cook_union_blob_test.go: TestPythonCookUnionBlobArm`
- `internal/codegen/pythontable/cook_union_blob_test.go: TestPythonCookUnionBytesArm`
- `internal/codegen/pythontable/cook_union_float_test.go: TestPythonCookUnionFloat`

## port/python/vt/wide-scalars

- `internal/codegen/pythontable/vtwide_test.go: TestPythonVtWide`
- `internal/codegen/pythontable/vtwide_test.go: TestPythonVtWideRefused`
- `internal/codegen/pythontable/vtwide_test.go: TestPythonVtWideNegativeControl`
- `internal/codegen/pythontable/vtwide_cpp_test.go: TestPythonVtWideCppOracle`

## port/python/vt/union-table-arms

- `internal/codegen/pythontable/pythontable_unions_test.go: TestPythonVtUnions`
- `internal/codegen/pythontable/pythontable_unions_test.go: TestPythonVtUnionsNegativeControl`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageUnionArray`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageWidening`

## port/python/vt/pointer-arrays

- `internal/codegen/pythontable/pointerarrays_test.go: TestPythonVtPointerArrays`
- `internal/codegen/pythontable/pointerarrays_cpp_test.go: TestPythonVtPointerArraysCppIdentity`
- `internal/codegen/pythontable/pointerarrays_bodybound_test.go: TestPythonVtPointerArrayBodyBound`
- `internal/codegen/pythontable/pointerarrays_test.go: TestPythonVtPointerArraysNegativeControl`

## port/python/vt/union-arrays

- `internal/codegen/pythontable/pythontable_unions_test.go: TestPythonVtUnions`
- `internal/codegen/pythontable/pythontable_unions_test.go: TestPythonVtUnionsNegativeControl`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageUnionArray`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageWidening`

## port/python/vt/optional-arrays

- `internal/codegen/pythontable/vtcells_optional_test.go: TestPythonVtOptional`
- `internal/codegen/pythontable/vtcells_optional_test.go: TestPythonVtOptionalNegativeControl`
- `internal/codegen/pythontable/vtcells_optional_test.go: TestPythonVtOptionalArrays`
- `internal/codegen/pythontable/vtcells_optional_test.go: TestPythonVtOptionalArraysNegativeControl`
- `internal/codegen/pythontable/required_form1_test.go: TestPythonForm1RequiredRemoval`

## port/python/vt/maps

- `internal/codegen/pythontable/vtmaps_test.go: TestPythonVtMaps`
- `internal/codegen/pythontable/vtmaps_test.go: TestPythonVtMapsNegativeControl`
- `internal/codegen/pythontable/map_blobs_wire_test.go: TestPythonMapBlobManifest`
- `internal/codegen/pythontable/map_blobs_wire_test.go: TestPythonMapBlobSlots`
- `internal/codegen/pythontable/map_key_widen_wire_test.go: TestPythonMapKeyWidenKinds`
- `internal/codegen/pythontable/map_key_widen_wire_test.go: TestPythonMapKeyWidenCountNegativeControl`

## port/python/vt/union-arms-any

- `internal/codegen/pythontable/pythontable_unions_test.go: TestPythonVtUnions`
- `internal/codegen/pythontable/pythontable_unions_test.go: TestPythonVtUnionsNegativeControl`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageUnionArray`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageWidening`

## port/python/vt/defaults-and-table-rename

- `internal/codegen/pythontable/evolution_test.go: TestPythonVtEvolution`
- `internal/codegen/pythontable/evolution_test.go: TestPythonVtEvolutionNegativeControl`
- `internal/codegen/pythontable/evolution_test.go: TestPythonVtEvolutionNegativeControlDefaults`
- `internal/codegen/pythontable/evolution_test.go: TestPythonVtEvolutionNegativeControlRenames`
- `internal/codegen/pythontable/evolution_test.go: TestPythonVtEvolutionNegativeControlSkipCheck`
- `compiler/python_evolution_integration_test.go: TestPythonEvolutionOldWriterFunctional`
- `compiler/python_evolution_integration_test.go: TestPythonEvolutionWireIDs`
- `compiler/python_evolution_integration_test.go: TestPythonEvolutionRenameNegativeControls`

## port/python/vt/renames

- `internal/codegen/pythontable/evolution_test.go: TestPythonVtEvolution`
- `internal/codegen/pythontable/evolution_test.go: TestPythonVtEvolutionNegativeControl`
- `internal/codegen/pythontable/evolution_test.go: TestPythonVtEvolutionNegativeControlDefaults`
- `internal/codegen/pythontable/evolution_test.go: TestPythonVtEvolutionNegativeControlRenames`
- `internal/codegen/pythontable/evolution_test.go: TestPythonVtEvolutionNegativeControlSkipCheck`
- `compiler/python_evolution_integration_test.go: TestPythonEvolutionOldWriterFunctional`
- `compiler/python_evolution_integration_test.go: TestPythonEvolutionWireIDs`
- `compiler/python_evolution_integration_test.go: TestPythonEvolutionRenameNegativeControls`

## port/python/vt/id-table-wire

- `internal/codegen/pythontable/id_order_test.go: TestPythonIDOrder`
- `internal/codegen/pythontable/id_order_test.go: TestPythonIDOrderUnknownGraphCpp`
- `internal/codegen/pythontable/id_order_test.go: TestPythonIDOrderUnknownNestedCpp`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainKnownOnlyIDOverflow`

## port/python/vt/unbounded-arrays

- `internal/codegen/pythontable/vtunbounded_test.go: TestPythonVtUnbounded`
- `internal/codegen/pythontable/vtunbounded_test.go: TestPythonVtUnboundedNegativeControl`
- `internal/codegen/pythontable/array_compat_test.go: TestPythonForm1ArrayPriorWriters`
- `internal/codegen/pythontable/list_kinds_cpp_test.go: TestPythonListKindsCppOracle`

## port/python/vt/message-form

- `internal/codegen/pythontable/message_test.go: TestPythonMessage`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageAnnouncement`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageArrayEvolution`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageBits`
- `internal/codegen/pythontable/message_crc_test.go: TestPythonMessageCRC`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageDamage`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageEvolution`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageFixedKinds`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageHostileAnnouncement`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageHostilePayload`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageKeyedOptional`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageMixedKinds`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageNegativeControl`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageNestedArray`
- `internal/codegen/pythontable/message_onesided_test.go: TestPythonMessageOneSided`
- `internal/codegen/pythontable/message_test.go: TestPythonMessagePacketValue`
- `internal/codegen/pythontable/message_test.go: TestPythonMessagePublicNameCollision`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageRangeInformation`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageRootIdentity`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageShapeBenchFunctional`
- `internal/codegen/pythontable/message_substitution_test.go: TestPythonMessageSubstitutedEnum`
- `internal/codegen/pythontable/message_substitution_test.go: TestPythonMessageSubstitutedKey`
- `internal/codegen/pythontable/message_substitution_test.go: TestPythonMessageSubstitutedUnion`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageUnionArray`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageUnionEnum`
- `internal/codegen/pythontable/message_arm_shape_test.go: TestPythonMessageUnionKeyedArmShape`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageWide`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageWidening`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageWriteNegativeControl`
- `internal/codegen/pythontable/message_test.go: TestPythonMessageZeroWidthSkip`

## port/python/vt/retain-unknown

- `internal/codegen/pythontable/retain_test.go: TestPythonRetainArrayWideningReport`
- `internal/codegen/pythontable/retain_crc_test.go: TestPythonRetainCRC`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainCapture`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainCrossVersionInternal`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainDepth128`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainDirectFrameBoundaries`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainDriverNegativeControl`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainEnumArrayCapture`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainFixedArrayGrowth`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainFixedRefusal`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainGuardedBody`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainKeyedAndMapInternal`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainKeyedEnum`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainKnownIDNegativeControl`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainKnownKeyedIDLimit`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainKnownOnlyIDOverflow`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainManifestSave`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainMapEntryIDBudget`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainNames`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainNestedBodyInternal`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainNodeBodyInternal`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainNodeRemapNegativeControl`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainOptionalEnumArray`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainOptionalMap`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainOptionalTableArray`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainOutputAllocationShape`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainPathIndex`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainPathIndexNegativeControl`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainPointerArrayIdentity`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainPublicNegativeControl`
- `internal/codegen/pythontable/retain_required_test.go: TestPythonRetainRequiredContainers`
- `internal/codegen/pythontable/retain_required_test.go: TestPythonRetainRequiredDefault`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainResolvedPayload`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainResolverNegativeControl`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainRootBodyInternal`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainRoundTrip`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainSelectedRuntime`
- `internal/codegen/pythontable/surface_test.go: TestPythonRetainSurfaceSelection`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainTableArrayInternal`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainTail`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainTailNegativeControl`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainTotalIDDroppedDefaultSlot`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainTotalIDKnownLast`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainUnionArrayCapture`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainUnionEnumArrayArm`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainUnionUnionArrayArm`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainZeroIDField`
- `internal/codegen/pythontable/retain_test.go: TestPythonRetainZeroIDNegativeControl`

## port/python/vt/doc-tags

- `internal/codegen/pythontable/reflection_test.go: TestPythonReflection`
- `internal/codegen/pythontable/reflection_layout_test.go: TestPythonReflectionAccessorNegativeControl`
- `internal/codegen/pythontable/reflection_layout_test.go: TestPythonReflectionAccessors`
- `internal/codegen/pythontable/reflection_docs_test.go: TestPythonReflectionAnnotationAgreement`
- `internal/codegen/pythontable/reflection_docs_test.go: TestPythonReflectionAnnotationAgreementNegativeControl`
- `internal/codegen/pythontable/reflection_docs_test.go: TestPythonReflectionAnnotationEscapes`
- `internal/codegen/pythontable/reflection_corpus_test.go: TestPythonReflectionCorpus`
- `internal/codegen/pythontable/reflection_corpus_test.go: TestPythonReflectionCorpusNegativeControl`
- `internal/codegen/pythontable/reflection_docs_test.go: TestPythonReflectionDescriptorDocs`
- `internal/codegen/pythontable/reflection_docs_test.go: TestPythonReflectionDescriptorDocsNegativeControl`
- `internal/codegen/pythontable/reflection_corpus_test.go: TestPythonReflectionDetachedVocabularyIDs`
- `compiler/pythonleg_reflection_on_test.go: TestPythonReflectionDriverAllKinds`
- `compiler/pythonleg_reflection_on_test.go: TestPythonReflectionDriverCorpus`
- `compiler/pythonleg_reflection_on_test.go: TestPythonReflectionDriverNameCollision`
- `compiler/pythonleg_reflection_on_test.go: TestPythonReflectionDriverNegativeControl`
- `compiler/pythonleg_reflection_on_test.go: TestPythonReflectionDriverPacketOnly`
- `compiler/pythonleg_reflection_on_test.go: TestPythonReflectionDriverPacketUnionNegativeControl`
- `internal/codegen/pythontable/reflection_docs_test.go: TestPythonReflectionFieldVocabulary`
- `internal/codegen/pythontable/reflection_docs_test.go: TestPythonReflectionFieldVocabularyNegativeControl`
- `internal/codegen/pythontable/reflection_corpus_test.go: TestPythonReflectionKindHoleNegativeControl`
- `internal/codegen/pythontable/reflection_layout_test.go: TestPythonReflectionLayout`
- `internal/codegen/pythontable/reflection_layout_test.go: TestPythonReflectionLayoutNegativeControl`
- `internal/codegen/pythontable/reflection_layout_test.go: TestPythonReflectionLiveIdentityAndReset`
- `internal/codegen/pythontable/reflection_layout_test.go: TestPythonReflectionResetLargeArrays`
- `internal/codegen/pythontable/reflection_layout_test.go: TestPythonReflectionResetLargeArraysNegativeControl`
- `internal/codegen/pythontable/reflection_corpus_test.go: TestPythonReflectionStrictTypes`
- `compiler/python_surface_behavior_test.go: TestPythonReflectionSurfaceSelection`
- `internal/codegen/pythontable/reflection_corpus_test.go: TestPythonReflectionViewIsLazy`
- `internal/codegen/pythontable/reflection_corpus_test.go: TestPythonReflectionVocabularyNegativeControl`

## port/python/vt/unit-registry

- `internal/codegen/pythontable/reflection_test.go: TestPythonReflection`
- `internal/codegen/pythontable/reflection_layout_test.go: TestPythonReflectionAccessorNegativeControl`
- `internal/codegen/pythontable/reflection_layout_test.go: TestPythonReflectionAccessors`
- `internal/codegen/pythontable/reflection_docs_test.go: TestPythonReflectionAnnotationAgreement`
- `internal/codegen/pythontable/reflection_docs_test.go: TestPythonReflectionAnnotationAgreementNegativeControl`
- `internal/codegen/pythontable/reflection_docs_test.go: TestPythonReflectionAnnotationEscapes`
- `internal/codegen/pythontable/reflection_corpus_test.go: TestPythonReflectionCorpus`
- `internal/codegen/pythontable/reflection_corpus_test.go: TestPythonReflectionCorpusNegativeControl`
- `internal/codegen/pythontable/reflection_docs_test.go: TestPythonReflectionDescriptorDocs`
- `internal/codegen/pythontable/reflection_docs_test.go: TestPythonReflectionDescriptorDocsNegativeControl`
- `internal/codegen/pythontable/reflection_corpus_test.go: TestPythonReflectionDetachedVocabularyIDs`
- `compiler/pythonleg_reflection_on_test.go: TestPythonReflectionDriverAllKinds`
- `compiler/pythonleg_reflection_on_test.go: TestPythonReflectionDriverCorpus`
- `compiler/pythonleg_reflection_on_test.go: TestPythonReflectionDriverNameCollision`
- `compiler/pythonleg_reflection_on_test.go: TestPythonReflectionDriverNegativeControl`
- `compiler/pythonleg_reflection_on_test.go: TestPythonReflectionDriverPacketOnly`
- `compiler/pythonleg_reflection_on_test.go: TestPythonReflectionDriverPacketUnionNegativeControl`
- `internal/codegen/pythontable/reflection_docs_test.go: TestPythonReflectionFieldVocabulary`
- `internal/codegen/pythontable/reflection_docs_test.go: TestPythonReflectionFieldVocabularyNegativeControl`
- `internal/codegen/pythontable/reflection_corpus_test.go: TestPythonReflectionKindHoleNegativeControl`
- `internal/codegen/pythontable/reflection_layout_test.go: TestPythonReflectionLayout`
- `internal/codegen/pythontable/reflection_layout_test.go: TestPythonReflectionLayoutNegativeControl`
- `internal/codegen/pythontable/reflection_layout_test.go: TestPythonReflectionLiveIdentityAndReset`
- `internal/codegen/pythontable/reflection_layout_test.go: TestPythonReflectionResetLargeArrays`
- `internal/codegen/pythontable/reflection_layout_test.go: TestPythonReflectionResetLargeArraysNegativeControl`
- `internal/codegen/pythontable/reflection_corpus_test.go: TestPythonReflectionStrictTypes`
- `compiler/python_surface_behavior_test.go: TestPythonReflectionSurfaceSelection`
- `internal/codegen/pythontable/reflection_corpus_test.go: TestPythonReflectionViewIsLazy`
- `internal/codegen/pythontable/reflection_corpus_test.go: TestPythonReflectionVocabularyNegativeControl`

## port/python/vt/cards

- `internal/codegen/pythontable/cards_test.go: TestPythonCards, TestPythonCardsCppIdentity, TestPythonCardsCppJSON, TestPythonCardsFacade, TestPythonCardsFacadeNegativeControl`
- `make/python-cards.mk: cards-python-conformance and four facade controls`


## port/python/vt/enum-keyed-arrays

- `internal/codegen/pythontable/vtkeyed_test.go: TestPythonVtKeyed`
- `internal/codegen/pythontable/vtkeyed_test.go: TestPythonVtKeyedNegativeControl`
- `internal/codegen/pythontable/vtkeyed_enum_test.go: TestPythonVtKeyedEnumRoundTripFunctional`
- `internal/codegen/pythontable/vtkeyed_enum_test.go: TestPythonVtKeyedEnumEvolutionFunctional`
- `internal/codegen/pythontable/vtkeyed_enum_test.go: TestPythonVtKeyedEnumNegativeControlFunctional`
- `internal/codegen/pythontable/keyed_flags_test.go: TestPythonKeyedFlags and TestPythonKeyedFlagsNegativeControl`
- `compiler/python_keyed_flags_test.go: TestPythonKeyedFlagsPublicDriver`
- `internal/codegen/pythontable/flags_widen_audit_test.go: TestPythonAuditWidenFlags and TestPythonAuditWidenFlagsContainers`

## port/python/vt/widening

- `internal/codegen/pythontable/scalar_widen_test.go: TestPythonScalarWidenDamageAndControl`
- `internal/codegen/pythontable/scalar_widen_test.go: TestPythonScalarWidenHistorical`
- `internal/codegen/pythontable/scalar_widen_test.go: TestPythonScalarWidenMapInteraction`
- `internal/codegen/pythontable/scalar_widen_test.go: TestPythonScalarWidenNonFinite`
- `internal/codegen/pythontable/scalar_widen_test.go: TestPythonScalarWidenRefusals`
- `internal/codegen/pythontable/container_widen_test.go: TestPythonContainerWidenHistorical`
- `internal/codegen/pythontable/container_widen_test.go: TestPythonContainerWidenRefusalsAndControls`
- `internal/codegen/pythontable/scalar_widen_test.go: TestPythonOptionalScalarWiden`
- `internal/codegen/pythontable/array_widen_test.go: TestPythonArrayWidening`
- `internal/codegen/pythontable/array_widen_test.go: TestPythonArrayWideningBoundaries`
- `internal/codegen/pythontable/array_widen_test.go: TestPythonArrayWideningNegativeControl`
- `internal/codegen/pythontable/array_widen_test.go: TestPythonArrayWideningRejectsIncompatible`
- `internal/codegen/pythontable/array_widen_test.go: TestPythonArrayWideningWithFixedGrowth`
- `internal/codegen/pythontable/map_key_widen_wire_test.go: TestPythonMapKeyWidenCountNegativeControl`
- `internal/codegen/pythontable/map_key_widen_wire_test.go: TestPythonMapKeyWidenKinds`
- `internal/codegen/pythontable/map_key_widen_wire_test.go: TestPythonMapKeyWidenManifest`
- `internal/codegen/pythontable/timestamp_test.go: TestPythonVtTimestamp`
- `internal/codegen/pythontable/timestamp_test.go: TestPythonVtTimestampNegativeControl`
- `internal/codegen/pythontable/flags_widen_audit_test.go: TestPythonAuditWidenFlags, proposed durable test commit 404d54bf791cdd907ef81ab28c40deee17f21527 (not in 456)`
- `internal/codegen/pythontable/keyed_flags_test.go: TestPythonKeyedFlags and TestPythonKeyedFlagsNegativeControl`
- `compiler/python_keyed_flags_test.go: TestPythonKeyedFlagsPublicDriver`
- `internal/codegen/pythontable/flags_widen_audit_test.go: TestPythonAuditWidenFlags and TestPythonAuditWidenFlagsContainers`
