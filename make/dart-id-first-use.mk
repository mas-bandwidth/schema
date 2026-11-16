# The control blinds the generated reader to unused ID table entries.
.PHONY: dart-id-first-use-negative-control
dart-id-first-use-negative-control:
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 DART=$(DART) go test -count=1 -timeout 600s -run '^TestDartWireIdFirstUseNegativeControl$$' ./internal/codegen/darttable/
test-dart: dart-id-first-use-negative-control
