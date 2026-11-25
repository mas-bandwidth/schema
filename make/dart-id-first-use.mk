# The control blinds the generated reader to unused ID table entries.
.PHONY: dart-id-first-use-negative-control
dart-id-first-use-negative-control:
test-dart: dart-id-first-use-negative-control
