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

CARDS_CPP_SRCS := tables/cards/cpp/cards.cpp tables/cards/cpp/sha256.cpp \
	$(CARDS_CPP_GENERATED)/CardTable.cpp $(CARDS_CPP_GENERATED)/CardsView.cpp

build/cards-cpp-test: cards-cpp-generate tables/cards/cpp/cards.cpp tables/cards/cpp/sha256.cpp tables/cards/cpp/cards_test.cpp make/cpp.mk
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -I$(CARDS_CPP_GENERATED) \
		tables/cards/cpp/cards_test.cpp $(CARDS_CPP_SRCS) -o $@

build/cards-cpp-test-asan: cards-cpp-generate tables/cards/cpp/cards.cpp tables/cards/cpp/sha256.cpp tables/cards/cpp/cards_test.cpp make/cpp.mk
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -fsanitize=address,undefined -fno-sanitize-recover=all \
		-fno-omit-frame-pointer -g -I$(CARDS_CPP_GENERATED) \
		tables/cards/cpp/cards_test.cpp $(CARDS_CPP_SRCS) -o $@

.PHONY: cards-cpp-test
cards-cpp-test: build/cards-cpp-test build/cards-cpp-test-asan
	./build/cards-cpp-test
	./build/cards-cpp-test-asan

build/cards-cpp-driver: cards-cpp-generate tables/cards/cpp/cards.cpp tables/cards/cpp/sha256.cpp tables/cards/cpp/driver.cpp make/cpp.mk
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -I$(CARDS_CPP_GENERATED) \
		tables/cards/cpp/driver.cpp $(CARDS_CPP_SRCS) -o $@

CARDS_CPP_SURFACES := load,save,render,parse,bounds,upgrade,edit,taskhash,view

.PHONY: cards-cpp-conformance
cards-cpp-conformance: build/conformance-harness build/cards-cpp-driver
	./build/conformance-harness cards --corpus tables/cards/conformance --driver tables/cards/cpp/driver --require $(CARDS_CPP_SURFACES)

.PHONY: cards-cpp-conformance-negative-control
cards-cpp-conformance-negative-control: build/conformance-harness build/cards-cpp-driver
	@mkdir -p build
	@if CARDS_CPP_RENDER_FLIP=1 ./build/conformance-harness cards --corpus tables/cards/conformance --driver tables/cards/cpp/driver --require $(CARDS_CPP_SURFACES) > build/cards-cpp-conformance-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the card check stayed green with one rendered byte changed'; exit 1; \
	fi
	@grep -qE 'cards render .*FAIL' build/cards-cpp-conformance-neg.log || { echo 'NEGATIVE CONTROL FAILED: render did not go red'; cat build/cards-cpp-conformance-neg.log; exit 1; }
	@for s in load save parse bounds upgrade edit taskhash view; do \
		grep -qE "cards $$s pass" build/cards-cpp-conformance-neg.log || { echo "NEGATIVE CONTROL FAILED: $$s did not stay green"; cat build/cards-cpp-conformance-neg.log; exit 1; }; \
	done
	@echo 'cards-cpp negative control: one changed render byte turns render red; the other eight surfaces stay green'

.PHONY: cards-cpp-conformance-upgrade-negative-control
cards-cpp-conformance-upgrade-negative-control: build/conformance-harness build/cards-cpp-driver
	@mkdir -p build
	@if CARDS_CPP_UPGRADE_DROP=1 ./build/conformance-harness cards --corpus tables/cards/conformance --driver tables/cards/cpp/driver --require $(CARDS_CPP_SURFACES) > build/cards-cpp-conformance-upgrade-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the card check stayed green when one retained field was dropped'; exit 1; \
	fi
	@grep -qE 'cards upgrade .*FAIL' build/cards-cpp-conformance-upgrade-neg.log || { echo 'NEGATIVE CONTROL FAILED: upgrade did not go red'; cat build/cards-cpp-conformance-upgrade-neg.log; exit 1; }
	@for s in load save render parse bounds edit taskhash view; do \
		grep -qE "cards $$s pass" build/cards-cpp-conformance-upgrade-neg.log || { echo "NEGATIVE CONTROL FAILED: $$s did not stay green"; cat build/cards-cpp-conformance-upgrade-neg.log; exit 1; }; \
	done
	@echo 'cards-cpp upgrade negative control: dropping one retained field turns upgrade red; the other eight surfaces stay green'

.PHONY: cards-cpp-conformance-hash-negative-control
cards-cpp-conformance-hash-negative-control: build/conformance-harness build/cards-cpp-driver
	@mkdir -p build
	@if CARDS_CPP_HASH_NEEDS=1 ./build/conformance-harness cards --corpus tables/cards/conformance --driver tables/cards/cpp/driver --require $(CARDS_CPP_SURFACES) > build/cards-cpp-conformance-hash-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the card check stayed green when needs was hashed'; exit 1; \
	fi
	@grep -qE 'cards taskhash .*FAIL' build/cards-cpp-conformance-hash-neg.log || { echo 'NEGATIVE CONTROL FAILED: taskhash did not go red'; cat build/cards-cpp-conformance-hash-neg.log; exit 1; }
	@for s in load save render parse bounds upgrade edit view; do \
		grep -qE "cards $$s pass" build/cards-cpp-conformance-hash-neg.log || { echo "NEGATIVE CONTROL FAILED: $$s did not stay green"; cat build/cards-cpp-conformance-hash-neg.log; exit 1; }; \
	done
	@echo 'cards-cpp hash negative control: hashing needs turns taskhash red; the other eight surfaces stay green'

test: cards-cpp-test cards-cpp-conformance cards-cpp-conformance-negative-control cards-cpp-conformance-upgrade-negative-control cards-cpp-conformance-hash-negative-control
