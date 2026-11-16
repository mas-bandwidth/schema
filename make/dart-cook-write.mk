# THE DART LEG'S COOK-WRITE SURFACE (docs/SPEC-TABLES.md §7.2, §7.6;
# test/conformance/README.md). The Dart leg alone through the harness, and only
# its cook-write line is this target's verdict (the leg's other surfaces are
# other gates): every row answered, root_full and archive included, with the
# engine's bytes, so the line reads `pass N/N` with no `+Ka` absence suffix,
# and no case of it is relaxed. A counted array's slots past its count are the
# zero the region fill wrote (docs/rulings.json fixed-dead-bytes-are-zero).
DART_COOK_WRITE := build/conformance-dart-cook-write
.PHONY: conformance-dart-cook-write conformance-dart-cook-write-negative-control
conformance-dart-cook-write: build/conformance-harness build/conformance-dart
	@rm -rf $(DART_COOK_WRITE) && mkdir -p $(DART_COOK_WRITE)
	./build/conformance-harness run --only dart --work $(DART_COOK_WRITE)/work > $(DART_COOK_WRITE)/log 2>&1 || true
	@grep -E '^cook-write ' $(DART_COOK_WRITE)/log || \
		{ cat $(DART_COOK_WRITE)/log; echo "conformance-dart-cook-write: the run printed no cook-write line"; exit 1; }
	@grep -qE '^cook-write +pass ([0-9]+)/\1 *$$' $(DART_COOK_WRITE)/log || \
		{ cat $(DART_COOK_WRITE)/log; echo "conformance-dart-cook-write: the Dart cook-write line is not pass N/N with no absence"; exit 1; }
	@! grep -E 'RELAXED .* dart / cook-write' $(DART_COOK_WRITE)/log || \
		{ echo "conformance-dart-cook-write: a Dart cook-write case is relaxed"; exit 1; }
	@echo "conformance-dart-cook-write: the Dart leg answers every cook-write row with the engine's bytes"

# ITS NEGATIVE CONTROL: the generated tabledemo cook writer rebuilt under
# build/ with root_full's counted weapons array writing one nonzero byte into
# the first slot past its count, where the engine leaves zero. root_full holds
# fewer weapons than its bound, so the harness must fail that row, and the
# cook-write line must stop reading pass N/N.
DART_COOK_WRITE_NC := build/conformance-dart-cook-write-negative-control
DART_COOK_WRITE_SED := s|if (i >= value.weapons.length) break;|if (i >= value.weapons.length) { out?.setUint8(at + 24 + i * 28, 1); break; } /* SABOTAGED */|
conformance-dart-cook-write-negative-control: build/conformance-harness build/conformance-dart
	@rm -rf $(DART_COOK_WRITE_NC) && mkdir -p $(DART_COOK_WRITE_NC)
	@cp -a build/tables-generated-dart $(DART_COOK_WRITE_NC)/generated
	@sed '$(DART_COOK_WRITE_SED)' build/tables-generated-dart/examples/TablesCook.dart > $(DART_COOK_WRITE_NC)/generated/examples/TablesCook.dart
	@test "$$(grep -c SABOTAGED $(DART_COOK_WRITE_NC)/generated/examples/TablesCook.dart)" = 1 || \
		{ echo "NEGATIVE CONTROL FAILED: the dead-slot sabotage did not apply to the cook writer"; exit 1; }
	@sed -e "s|../../../build/tables-generated-dart/|$(CURDIR)/$(DART_COOK_WRITE_NC)/generated/|g" \
	     -e "s|../../../build/|$(CURDIR)/build/|g" \
		test/conformance/dart/main.dart > $(DART_COOK_WRITE_NC)/main.dart
	$(DART) compile exe -o $(DART_COOK_WRITE_NC)/driver-bin $(DART_COOK_WRITE_NC)/main.dart >/dev/null
	@printf 'dart %s/%s/driver-bin\n' "$(CURDIR)" "$(DART_COOK_WRITE_NC)" > $(DART_COOK_WRITE_NC)/drivers.txt
	./build/conformance-harness run --drivers $(DART_COOK_WRITE_NC)/drivers.txt \
		--work $(DART_COOK_WRITE_NC)/work > $(DART_COOK_WRITE_NC)/log 2>&1 || true
	@if grep -qE '^cook-write +pass ([0-9]+)/\1 *$$' $(DART_COOK_WRITE_NC)/log; then \
		echo "NEGATIVE CONTROL FAILED: a cook writer that fills a counted array's dead slot left the Dart cook-write line green"; \
		cat $(DART_COOK_WRITE_NC)/log; exit 1; \
	fi
	@grep -E 'root_full' $(DART_COOK_WRITE_NC)/log | grep -m1 'cook-write' || \
		{ echo "NEGATIVE CONTROL FAILED: the cook-write line went red, but not on root_full"; \
		  cat $(DART_COOK_WRITE_NC)/log; exit 1; }
	@echo "negative control: a cook writer that fills a counted array's dead slot turns Dart cook-write RED on root_full"

test-dart: conformance-dart-cook-write conformance-dart-cook-write-negative-control
