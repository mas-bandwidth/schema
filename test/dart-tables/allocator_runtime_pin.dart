// THE DART ALLOCATION GATE's RUNTIME PIN (docs/PORTING.md §I14, schema#420).
//
// The gate reads the running SDK and REFUSES to certify on any other than the
// pin, which it reads from test/conformance/dart/ci.json — the same row CI
// builds its matrix from, so the gate cannot drift from the SDK CI installs.
// SCHEMA_DART_ALLOC_ANY_DART=1 reports without certifying. THERE IS NO
// ALLOCATION MEASUREMENT ON THIS LEG YET (schema#420): this gate holds the
// refusal only.
//
//   usage: dart test/dart-tables/allocator_runtime_pin.dart
//   env:   SCHEMA_DART_ALLOC_VERSION=<override> SCHEMA_DART_ALLOC_ANY_DART=1
import 'dart:convert';
import 'dart:io';

void main(List<String> args) {
  final File pinFile = File('test/conformance/dart/ci.json');
  if (!pinFile.existsSync()) {
    print(
      'FAILED: the dart pin is not readable at test/conformance/dart/ci.json',
    );
    exit(1);
  }
  final dynamic decoded = jsonDecode(pinFile.readAsStringSync());
  final String? pinField = decoded is Map ? decoded['dart'] as String? : null;
  final String pinned = (pinField ?? '').trim();
  if (pinned.isEmpty) {
    print(
      'FAILED: the dart pin is not readable at test/conformance/dart/ci.json',
    );
    exit(1);
  }

  final String override =
      Platform.environment['SCHEMA_DART_ALLOC_VERSION'] ?? '';
  final String observed = override.isNotEmpty
      ? override
      : Platform.version.split(' ').first;

  print('pin: $pinned');
  print('observed: $observed');

  if (override.isNotEmpty && observed == pinned) {
    print('FAILED: the version override may not claim the pin');
    exit(1);
  }

  final String anyDart =
      Platform.environment['SCHEMA_DART_ALLOC_ANY_DART'] ?? '';

  if (observed != pinned) {
    if (anyDart == '1') {
      stderr.writeln(
        'dart $observed is not the pinned $pinned — reporting, not certifying',
      );
      exit(0);
    }
    print(
      'FAILED: allocation certification requires dart $pinned, running dart $observed',
    );
    print(
      'the JIT inlines a generated body differently between SDKs, so a floor measured on '
      'whatever dart a PATH lookup found says nothing about the runtime the claim is for; '
      'unpack the pinned SDK into dist/ (make/dart.mk says how) and re-run, or set '
      'SCHEMA_DART_ALLOC_ANY_DART=1 to read without certifying',
    );
    exit(1);
  }

  print('dart $pinned is the pin: the runtime is certified.');
  print(
    'NO ALLOCATION MEASUREMENT EXISTS ON THIS LEG (schema#420); this gate holds the refusal only.',
  );
  exit(0);
}
