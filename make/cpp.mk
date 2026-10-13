# make/cpp.mk — the C++ leg of the card corpus (schema-cards). Included by the
# Makefile's wildcard include; the Makefile names no language. The card targets
# live here, not in the root Makefile (docs/CONTRIBUTING.md, "Adding a
# language").

CARDS_CPP_GENERATED := build/tables-generated-cpp/cards

.PHONY: cards-cpp-generate
cards-cpp-generate: bin/schema
	@rm -rf $(CARDS_CPP_GENERATED)
	@mkdir -p $(CARDS_CPP_GENERATED)
	./bin/schema generate --lang cpp --out $(CARDS_CPP_GENERATED) tables/cards

build/cards-cpp-test: cards-cpp-generate tables/cards/cpp/cards.cpp tables/cards/cpp/cards_test.cpp make/cpp.mk
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -I$(CARDS_CPP_GENERATED) \
		tables/cards/cpp/cards_test.cpp tables/cards/cpp/cards.cpp \
		$(CARDS_CPP_GENERATED)/CardTable.cpp -o $@

build/cards-cpp-test-asan: cards-cpp-generate tables/cards/cpp/cards.cpp tables/cards/cpp/cards_test.cpp make/cpp.mk
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -fsanitize=address,undefined -fno-sanitize-recover=all \
		-fno-omit-frame-pointer -g -I$(CARDS_CPP_GENERATED) \
		tables/cards/cpp/cards_test.cpp tables/cards/cpp/cards.cpp \
		$(CARDS_CPP_GENERATED)/CardTable.cpp -o $@

.PHONY: cards-cpp-test
cards-cpp-test: build/cards-cpp-test build/cards-cpp-test-asan
	./build/cards-cpp-test
	./build/cards-cpp-test-asan

build/cards-cpp-driver: cards-cpp-generate tables/cards/cpp/cards.cpp tables/cards/cpp/driver.cpp make/cpp.mk
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -I$(CARDS_CPP_GENERATED) \
		tables/cards/cpp/driver.cpp tables/cards/cpp/cards.cpp \
		$(CARDS_CPP_GENERATED)/CardTable.cpp -o $@

.PHONY: cards-cpp-conformance
cards-cpp-conformance: build/conformance-harness build/cards-cpp-driver
	./build/conformance-harness cards --corpus tables/cards/conformance --driver tables/cards/cpp/driver --require load,save,render,parse,bounds

.PHONY: cards-cpp-conformance-negative-control
cards-cpp-conformance-negative-control: build/conformance-harness build/cards-cpp-driver
	@mkdir -p build
	@if CARDS_CPP_RENDER_FLIP=1 ./build/conformance-harness cards --corpus tables/cards/conformance --driver tables/cards/cpp/driver --require load,save,render,parse,bounds > build/cards-cpp-conformance-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the card check stayed green with one rendered byte changed'; exit 1; \
	fi
	@grep -qE 'cards render .*FAIL' build/cards-cpp-conformance-neg.log || { echo 'NEGATIVE CONTROL FAILED: render did not go red'; cat build/cards-cpp-conformance-neg.log; exit 1; }
	@for s in load save parse bounds; do \
		grep -qE "cards $$s pass" build/cards-cpp-conformance-neg.log || { echo "NEGATIVE CONTROL FAILED: $$s did not stay green"; cat build/cards-cpp-conformance-neg.log; exit 1; }; \
	done
	@echo 'cards-cpp negative control: one changed render byte turns render red; load, save, parse and bounds stay green'

test: cards-cpp-test cards-cpp-conformance cards-cpp-conformance-negative-control
