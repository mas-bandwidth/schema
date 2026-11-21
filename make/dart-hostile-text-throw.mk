# make/dart-hostile-text-throw.mk — the Dart hostile-corpus runner's text
# carrier answers a refusal only for the exception the Dart text read promises
# as one, FormatException (review 2026-11-21 DART-2). A RangeError, a TypeError
# or a stack overflow escaping <T>FromJson is not a refusal: it answers
# `error`, as the wire carriers answer it, so a corpus case whose verdict is
# `refuse damaged` cannot pass on a crash.
#
# The control plants `throw RangeError('PLANTED')` after the parse in every
# generated text read in a scratch copy and requires the runner to go red with
# a text case answered `got=error`. Before the fix the runner folded every
# throwable into `refuse damaged`, so every text case whose verdict is a
# refusal stayed green under the plant. It reuses the unplanted runner's
# generated units and bindings (schema-dart-runs-hostile-corpus).

.PHONY: schema-dart-hostile-text-throw-negative-control
schema-dart-hostile-text-throw-negative-control: build/dart-hostile/dispatch.dart
	@rm -rf $(DART_HOSTILE_BUILD)/throw && mkdir -p $(DART_HOSTILE_BUILD)/throw/build/dart-hostile $(DART_HOSTILE_BUILD)/throw/test/dart-hostile
	@cp -R $(DART_HOSTILE_BUILD)/generated $(DART_HOSTILE_BUILD)/throw/build/dart-hostile/generated
	@cp -R $(DART_HOSTILE_BUILD)/packet $(DART_HOSTILE_BUILD)/throw/build/dart-hostile/packet
	@cp $(DART_HOSTILE_BUILD)/dispatch.dart $(DART_HOSTILE_BUILD)/throw/build/dart-hostile/dispatch.dart
	@cp test/dart-hostile/runner.dart $(DART_HOSTILE_BUILD)/throw/test/dart-hostile/runner.dart
	@sed -i.orig "s|^  final root = _jsonDecodeExact(_jsonPrepare(text, r));|  final root = _jsonDecodeExact(_jsonPrepare(text, r)); throw RangeError('PLANTED');|" \
		$(DART_HOSTILE_BUILD)/throw/build/dart-hostile/generated/*/*Wire.dart
	@if cmp -s $(DART_HOSTILE_BUILD)/generated/tabledemo/TabledemoWire.dart $(DART_HOSTILE_BUILD)/throw/build/dart-hostile/generated/tabledemo/TabledemoWire.dart; then \
		echo 'NEGATIVE CONTROL FAILED: the planted RangeError did not apply'; exit 1; fi
	@if $(DART) run $(DART_HOSTILE_BUILD)/throw/test/dart-hostile/runner.dart "$(DART_HOSTILE_CORPUS)" "$(DART_HOSTILE_FINDINGS)" \
			> $(DART_HOSTILE_BUILD)/throw/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a RangeError escaping every text read left the corpus green'; \
		cat $(DART_HOSTILE_BUILD)/throw/log; exit 1; fi
	@grep -q '^FAIL conformance-.* carrier=text got=error .*want=refuse' $(DART_HOSTILE_BUILD)/throw/log || \
		{ echo 'NEGATIVE CONTROL FAILED: no text case whose verdict is a refusal answered error under the planted RangeError'; \
		  cat $(DART_HOSTILE_BUILD)/throw/log; exit 1; }
	@echo 'dart hostile-corpus text-throw negative control: a RangeError escaping the text read answers error, never refuse damaged'

test-dart: schema-dart-hostile-text-throw-negative-control
