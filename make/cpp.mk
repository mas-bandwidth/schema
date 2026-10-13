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
