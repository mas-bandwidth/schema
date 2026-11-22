# make/dart-compressed-negzero.mk — the Dart leg's compressed -0 writer corpus
# (docs/SPEC-TABLES.md §3.3, docs/rulings.json fixed-compressed-negzero).
# Included by the Makefile's wildcard include.
#
# conformance-dart-compressed-negzero  every row of
#                                      testdata/conformance/feature-witnesses/compressed-negzero-go
#                                      run through the generated Dart unit: a
#                                      compressed float's -0 saves to its +0
#                                      twin's bytes in the variable and fixed
#                                      forms, and in every other Dart writer
#                                      (retain save, message form, JSON, cook);
#                                      the plain-float rows keep -0 bit-exact.
#                                      It runs with and without --enable-asserts:
#                                      Dart's write checks are debug asserts, the
#                                      -0 store is not a check and holds in both.
#                                      internal/codegen/darttable's
#                                      TestDartCompressedFloatStoresAreGuarded is
#                                      its fast twin in the ordinary go test run.
.PHONY: conformance-dart-compressed-negzero
conformance-dart-compressed-negzero: toolchain-dart
	DART=$(DART) SCHEMA_REQUIRE_CORPUS=1 sh test/slowgate/proof conformance-dart-compressed-negzero 'TestDartCompressedNegZeroWriters' \
		-count=1 -timeout 600s ./internal/codegen/darttable/ -run '^TestDartCompressedNegZeroWriters$$'

test-dart: conformance-dart-compressed-negzero
