# make/go.mk — the Go leg (docs/CONTRIBUTING.md, "Adding a language"). Included
# by the Makefile's wildcard include; the Makefile names no language. The leg
# registers itself at the end of this file.

# the serialize.go runtime the generated Go targets, a sibling checkout;
# test/go/go.mod and its ludicrous twin carry the same relative path
SERIALIZE_GO ?= ../serialize.go
SERIALIZE_GO_FETCHED := build/serialize.go
SERIALIZE_GO_RUNTIME := $(if $(wildcard $(SERIALIZE_GO)/go.mod),$(SERIALIZE_GO),$(SERIALIZE_GO_FETCHED))

build/packet-text/go/.stamp: bin/schema test/packet-text/Narrow.schema make/go.mk
	./bin/schema generate --lang go --out build/packet-text/go test/packet-text/Narrow.schema
	@printf 'module packettext\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => "%s/$(SERIALIZE_GO)"\n' "$(CURDIR)" > build/packet-text/go/go.mod
	@touch $@

.PHONY: packet-utf8-go packet-utf8-go-negative-control
packet-utf8-go: build/packet-text/go/.stamp build/packet-text/cpp/driver build/packet-text/harness
	cd test/packet-text/go && go build -o ../../../build/packet-text/go/driver .
	./build/packet-text/harness ./build/packet-text/go/driver

packet-utf8-go-negative-control: packet-utf8-go
	@mkdir -p build/packet-text/go-negative/checker
	go run ./tools/sabotage -name packet-utf8-go-read -out build/packet-text/go-negative/functions.gotext internal/codegen/golang/functions.go
	@printf '{"Replace":{"%s/internal/codegen/golang/functions.go":"%s/build/packet-text/go-negative/functions.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-text/go-negative/overlay.json
	go run -overlay=build/packet-text/go-negative/overlay.json ./cmd/schema generate --lang go --out build/packet-text/go-negative test/packet-text/Narrow.schema
	cp build/packet-text/go/go.mod build/packet-text/go-negative/go.mod
	cp test/packet-text/go/main.go build/packet-text/go-negative/checker/main.go
	cd build/packet-text/go-negative/checker && go build -o ../driver .
	@if ./build/packet-text/harness -mutations-only ./build/packet-text/go-negative/driver > build/packet-text/go-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: Go UTF-8 removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-text/go-negative/log || { cat build/packet-text/go-negative/log; exit 1; }
	@echo 'packet UTF-8 Go negative control: removed read validation fails bit-flip agreement'

test-go: packet-utf8-go packet-utf8-go-negative-control

generated/go-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang go --out generated/go-ludicrous examples128
	@printf 'module ludicrous\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => ../../$(SERIALIZE_GO)\n' > generated/go-ludicrous/go.mod
	@touch $@

# the Go target: generated package + module wiring (the go.mod is build
# wiring, not schema output — the emitter writes only .go files)
generated/go/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang go --out generated/go examples
	@printf 'module example\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => ../../$(SERIALIZE_GO)\n' > generated/go/go.mod
	@touch $@

# The same corpus through the C# table backend (docs/SPEC-TABLES.md, schema#262):
# the tables corpus plus the evolution pair, generated at build time into
# build/ — test-only, never part of the committed generated/ tree. The full
# unit is generated (packet .cs + <Base>Table.cs), because a table's closure
# decodes into the packet emitter's own classes.
# THE GO GENERIC-WALK GATE (docs/SPEC-TABLES.md §16), the C++ and C# gates'
# twin. The extracted walkers take a .txt suffix, for the reason the block
# fuzz's sabotage files do: `go build ./...` and `go test ./...` walk build/,
# and would otherwise find seven packages sitting in one directory.
# what makes the text form SCHEMA's rather than a packer's is that there
# is ONE walk, and the way to hold that is to compare the emitted bytes. One
# walker per unit — Go emits it into <Home>TableJson.go — and the same bytes in
# every unit of the corpus.
.PHONY: tables-go-fixedform tables-go-sibling-runtime
tables-go-sibling-runtime: deps
	@test -f "$(SERIALIZE_GO)/go.mod" || { echo "tables Go sibling runtime: make deps did not provision $(SERIALIZE_GO)" >&2; exit 1; }
tables-go-fixedform: tables-fixedform-corpus tables-go-sibling-runtime
	sh test/slowgate/proof tables-go-fixedform \
		'TestFixedFormCountAndLengthOverBoundRefuseByName TestFixedFormRoundTrip TestFixedFormHostileBoolByte TestFixedFormHostileUnionTag TestFixedFormArgLaneOldEncodingControl TestFixedFormClampLiveCount TestFixedFormRecordBound TestFixedFormIdentityEqualsCompiled TestFixedFormZeroBehindNarrowerArm TestFixedPlanMaxCoversEveryLineagePlan TestTimestampKindInGo TestFixedLoadAllocatesNothingPerCall TestFixedLoadNamesEveryRefusal TestFixedFormWriteSideRefusesInEveryGoBuild TestGoTableWritersRefuseWhatTheirReadersRefuse TestFixedLoadUnknownHashIsLayoutUnknown' \
		./internal/codegen/gotable -run 'TestFixedForm|TestFixedPlanMaxCoversEveryLineagePlan|TestTimestampKindInGo|TestFixedLoadAllocatesNothingPerCall|TestFixedFormCountAndLengthOverBoundRefuseByName|TestFixedLoadNamesEveryRefusal|TestFixedFormWriteSideRefusesInEveryGoBuild|TestGoTableWritersRefuseWhatTheirReadersRefuse|TestFixedLoadUnknownHashIsLayoutUnknown' -count=1
test-go: tables-go-fixedform

# The hand-editable text form (docs/SPEC-TABLES.md §16.9): the textform surface,
# both table classes, held to test/conformance/textform/ (the positive vectors
# and their oracles, the leaf edits, every refusal by name, the attachment
# oracle and a seeded edit fuzz).
.PHONY: tables-go-textform
tables-go-textform:
	sh test/slowgate/proof tables-go-textform \
		'TestCommentedTextForm/emitted TestCommentedTextForm/fixed TestCommentedTextForm/variable' \
		./internal/codegen/gotable -run '^TestCommentedTextForm$$' -count=1 -timeout 600s
test-go: tables-go-textform

# THE OPTIONAL SHARED GO RUNTIME (docs/SPEC-TABLES.md §8.4). tables/goruntime is
# GENERATED from the emitter, never edited: `make go-runtime` rewrites it, and
# the gate fails when the checked-in package is not what the emitter derives, or
# when a unit generated with runtime=shared stops producing the standalone
# unit's wires and reports.
.PHONY: go-runtime tables-go-shared-runtime
go-runtime:
	SCHEMA_SLOW=1 SCHEMA_WRITE_GORUNTIME=1 go test -count=1 -timeout 600s ./internal/codegen/gotable -run TestGoSharedRuntimePackageIsCurrent

tables-go-shared-runtime:
	sh test/slowgate/proof tables-go-shared-runtime \
		'TestGoSharedRuntimePackageIsCurrent TestGoSharedRuntimeIsOptionalAndEquivalent' \
		./internal/codegen/gotable -run 'TestGoSharedRuntime' -count=1
test-go: tables-go-shared-runtime

# THE GO CONFORMANCE ROWS (docs/SPEC-CARD-CONTRACT.md). The rows live in
# test/conformance/go/rows under the test/conformance/go module; the fixed-form
# rows read the C++ reference corpus, so the gate stands on the two build
# stamps and the sibling runtime, exactly as tables-go-fixedform does.
.PHONY: conformance-go-rows conformance-go-rows-negative-control
conformance-go-rows: build/tables-generated-go/.stamp build/fixedform-corpus/.stamp tables-go-sibling-runtime
	cd test/conformance/go && SLOWGATE_LOG_DIR="$(CURDIR)/build/slowgate" sh $(CURDIR)/test/slowgate/proof conformance-go-rows \
		'TestRowC12 TestRowC13 TestRowC14 TestRowC14WriterRefusesOutOfRange TestRowC7 TestRowC8 TestRowC8WriterRefusesOutOfRange TestRowCountedArraysValidData TestRowE5 TestRowF12 TestRowF8 TestRowF8NegativeControl TestRowF8RaggedTailByName TestRowFloat32ValuesNonFiniteRefused TestRowFloat32ValuesValidData TestRowP2 TestRowP3 TestRowR10 TestRowR14 TestRowR14LayoutRecordTooLargeByName TestRowR15 TestRowR17 TestRowR19 TestRowR19NegativeControl TestRowR20AddedFieldDefault TestRowR20DroppedFieldUnknown TestRowR20NarrowIntWidensExactly TestRowR20SameWidthBaseline TestRowR20ShortStringSlackZeros TestRowR20VocabularyIdentity TestRowR20 TestRowR22 TestRowR26 TestRowR26NegativeControl TestRowR27 TestRowR31 TestRowR6 TestRowR7 TestRowW12 TestRowW12NegativeControl TestRowW13 TestRowW2 TestRowW2WriterRefusesTheOldPayload TestRowW3 TestRowW4 TestRowW7' \
		./rows/ -count=1 -timeout 600s
test-go: conformance-go-rows

# The reversed witness, modelled on projection-variant-order-negative-control:
# plant one failing row without touching a tracked file, via `go test -overlay`
# adding a new file, and require the gate to go red ON that row. The planted
# source is written with a leading underscore (build/rows-planted/_ZZ_planted_test.go)
# so the go tool ignores it: `go list ./...` from the checkout root must not
# pick up a second package in build/rows-planted.
conformance-go-rows-negative-control: build/tables-generated-go/.stamp build/fixedform-corpus/.stamp tables-go-sibling-runtime
	@mkdir -p build/rows-planted
	@printf 'package main\n\nimport "testing"\n\nfunc TestRowZZPlanted(t *testing.T) { t.Parallel(); t.Fatal("planted failing row") }\n' > build/rows-planted/_ZZ_planted_test.go
	@printf '{"Replace":{"%s/test/conformance/go/rows/ZZ_planted_test.go":"%s/build/rows-planted/_ZZ_planted_test.go"}}\n' "$(CURDIR)" "$(CURDIR)" > build/rows-planted/overlay.json
	@if cd test/conformance/go && SLOWGATE_LOG_DIR="$(CURDIR)/build/slowgate" sh $(CURDIR)/test/slowgate/proof conformance-go-rows-negative-control \
		'TestRowC12 TestRowC13 TestRowC14 TestRowC14WriterRefusesOutOfRange TestRowC7 TestRowC8 TestRowC8WriterRefusesOutOfRange TestRowCountedArraysValidData TestRowE5 TestRowF12 TestRowF8 TestRowF8NegativeControl TestRowF8RaggedTailByName TestRowFloat32ValuesNonFiniteRefused TestRowFloat32ValuesValidData TestRowP2 TestRowP3 TestRowR10 TestRowR14 TestRowR14LayoutRecordTooLargeByName TestRowR15 TestRowR17 TestRowR19 TestRowR19NegativeControl TestRowR20AddedFieldDefault TestRowR20DroppedFieldUnknown TestRowR20NarrowIntWidensExactly TestRowR20SameWidthBaseline TestRowR20ShortStringSlackZeros TestRowR20VocabularyIdentity TestRowR20 TestRowR22 TestRowR26 TestRowR26NegativeControl TestRowR27 TestRowR31 TestRowR6 TestRowR7 TestRowW12 TestRowW12NegativeControl TestRowW13 TestRowW2 TestRowW2WriterRefusesTheOldPayload TestRowW3 TestRowW4 TestRowW7' \
		./rows/ -count=1 -timeout 600s -overlay=$(CURDIR)/build/rows-planted/overlay.json > $(CURDIR)/build/rows-planted/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the planted failing row left the gate green"; \
		cat $(CURDIR)/build/rows-planted/log; exit 1; fi
	@grep -q "planted failing row" $(CURDIR)/build/rows-planted/log || { echo "NEGATIVE CONTROL FAILED: it went red, but not on the planted row"; cat $(CURDIR)/build/rows-planted/log; exit 1; }
	@echo "rows negative control: the planted failing row turns the gate red"

.PHONY: tables-go-json-walk
tables-go-json-walk: build/tables-generated-go/.stamp
	@rm -rf build/json-walk-go && mkdir -p build/json-walk-go
	@for d in build/tables-generated-go/*/; do \
		unit=$$(basename $$d); n=0; \
		for f in $$d*TableJson.go; do \
			[ -e "$$f" ] || continue; \
			out=build/json-walk-go/$$unit.$$(basename $$f).txt; \
			awk '/---- json walk: begin ----/,/---- json walk: end ----/' $$f > $$out; \
			if [ -s $$out ]; then n=$$((n+1)); else rm -f $$out; fi; \
		done; \
		if [ -n "$$(ls $$d*TableJson.go 2>/dev/null)" ] && [ $$n -ne 1 ]; then \
			echo "GENERIC-WALK GATE FAILED: unit $$unit carries $$n walkers, not one"; exit 1; \
		fi; \
	done
	@if [ -z "$$(ls build/json-walk-go 2>/dev/null)" ]; then \
		echo "GENERIC-WALK GATE FAILED: no walker in any generated .go"; exit 1; fi
	@first=""; for f in build/json-walk-go/*; do \
		if [ -z "$$first" ]; then first=$$f; else \
			cmp -s $$first $$f || { echo "GENERIC-WALK GATE FAILED: the walker in $$f is not the walker in $$first"; exit 1; }; \
		fi; \
	done
	@echo "tables Go generic-walk gate: one walker per unit, byte-identical across $$(ls build/json-walk-go | wc -l | tr -d ' ') units"

# THE GO TABLE LEG's generated packages. Each unit is its own MODULE, because
# a generated package names its schema's `package` and Go resolves an import by
# module path — so the conformance leg's go.mod replaces one path per unit,
# exactly as test/go/go.mod already does for the packet corpus.
build/tables-generated-go/.stamp: bin/schema make/go.mk test/tables/RT1.schema test/tables/P2.schema test/tables/W1.schema test/tables/W2.schema test/tables/G1.schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P3.schema $(wildcard test/tables/[MAKR][12].schema) tables/scalars/Scalars.schema test/tables/Scalars2.schema $(wildcard examples-wide/*.schema) tables/messages/Messages.schema tables/backend/Backend.schema tables/vocab/Vocab.schema tables/vocab9/Vocab9.schema $(wildcard tables/stream/*.schema tables/blobs/*.schema tables/lists/*.schema tables/maps/*.schema)
	@mkdir -p build/tables-generated-go
	./bin/schema generate --lang go --out build/tables-generated-go/examples tables/examples
	# The pointer corpus exercises the wire, region and cook read surfaces.
	./bin/schema generate --lang go --out build/tables-generated-go/pointers tables/pointers
	./bin/schema generate --lang go --out build/tables-generated-go/block tables/block
	./bin/schema generate --lang go --out build/tables-generated-go/blockhome tables/blockhome
	./bin/schema generate --lang go --out build/tables-generated-go/v1 test/tables/V1.schema
	./bin/schema generate --lang go --out build/tables-generated-go/v2 test/tables/V2.schema
	./bin/schema generate --lang go --out build/tables-generated-go/p1 test/tables/P1.schema
	./bin/schema generate --lang go --out build/tables-generated-go/p3 test/tables/P3.schema
	./bin/schema generate --lang go --out build/tables-generated-go/p2 test/tables/P2.schema
	$(call go_table_module,p2,tblp2)
	./bin/schema generate --lang go --out build/tables-generated-go/w1 test/tables/W1.schema
	$(call go_table_module,w1,tblw1)
	./bin/schema generate --lang go --out build/tables-generated-go/w2 test/tables/W2.schema
	$(call go_table_module,w2,tblw2)
	./bin/schema generate --lang go --out build/tables-generated-go/g1 test/tables/G1.schema
	$(call go_table_module,g1,tblg1)
	./bin/schema generate --lang go --out build/tables-generated-go/m1 test/tables/M1.schema
	./bin/schema generate --lang go --out build/tables-generated-go/m2 test/tables/M2.schema
	./bin/schema generate --lang go --out build/tables-generated-go/a1 test/tables/A1.schema
	./bin/schema generate --lang go --out build/tables-generated-go/a2 test/tables/A2.schema
	./bin/schema generate --lang go --out build/tables-generated-go/k1 test/tables/K1.schema
	./bin/schema generate --lang go --out build/tables-generated-go/k2 test/tables/K2.schema
	./bin/schema generate --lang go --out build/tables-generated-go/r1 test/tables/R1.schema
	./bin/schema generate --lang go --out build/tables-generated-go/r2 test/tables/R2.schema
	./bin/schema generate --lang go --out build/tables-generated-go/scalars tables/scalars
	./bin/schema generate --lang go --out build/tables-generated-go/scalars2 test/tables/Scalars2.schema
	./bin/schema generate --lang go --out build/tables-generated-go/wide examples-wide
	$(call go_table_module,wide,widedemo)
	./bin/schema generate --lang go --out build/tables-generated-go/messages tables/messages
	./bin/schema generate --lang go --out build/tables-generated-go/stream tables/stream
	$(call go_table_module,stream,streamdemo)
	./bin/schema generate --lang go --out build/tables-generated-go/blobs tables/blobs
	$(call go_table_module,blobs,blobdemo)
	./bin/schema generate --lang go --out build/tables-generated-go/lists tables/lists
	$(call go_table_module,lists,listdemo)
	./bin/schema generate --lang go --out build/tables-generated-go/maps tables/maps
	$(call go_table_module,maps,mapdemo)
	./bin/schema generate --lang go --out build/tables-generated-go/backend tables/backend
	$(call go_table_module,backend,backenddemo)
	./bin/schema generate --lang go --out build/tables-generated-go/vocab tables/vocab
	$(call go_table_module,vocab,vocabdemo)
	./bin/schema generate --lang go --out build/tables-generated-go/vocab9 tables/vocab9
	$(call go_table_module,vocab9,vocab9demo)
	./bin/schema generate --lang go --out build/tables-generated-go/rt1 test/tables/RT1.schema
	$(call go_table_module,rt1,tblrt1)
	$(call go_table_module,messages,messagedemo)
	$(call go_table_module,examples,tabledemo)
	$(call go_table_module,pointers,graphdemo)
	$(call go_table_module,block,blockdemo)
	$(call go_table_module,blockhome,blockhomedemo)
	$(call go_table_module,v1,tblv1)
	$(call go_table_module,v2,tblv2)
	$(call go_table_module,p1,tblp1)
	$(call go_table_module,p3,tblp3)
	$(call go_table_module,scalars,scalardemo)
	$(call go_table_module,scalars2,tblscalars2)
	$(call go_table_module,m1,tblm1)
	$(call go_table_module,m2,tblm2)
	$(call go_table_module,a1,tbla1)
	$(call go_table_module,a2,tbla2)
	$(call go_table_module,k1,tblk1)
	$(call go_table_module,k2,tblk2)
	$(call go_table_module,r1,tblr1)
	$(call go_table_module,r2,tblr2)
	@touch $@

# THE CARDS UNIT (schema-cards-c01): its own generated Go module plus the two
# handwritten files that own the generated symbols. cards-generate produces ONLY
# build/tables-generated-go/cards, cleaning that owned output first so a deleted
# symbol cannot survive a regeneration. The source go.mod is NOT copied; the
# handwritten facade and test are copied by name, never a recursive wildcard.
# c03 adds two test-only historical units (cards-initial, cards-future), each in
# its own generated directory/module and imported under an alias: two
# declarations whose package is cards are never compiled in one package.
.PHONY: cards-generate cards-test cards-example cards-fuzz-fixture cards-wire-fuzz cards-wire-fuzz-run cards-fuzz cards-corpus-create cards-corpus-check
CARDS_FUZZ_TIME ?= 30s
cards-generate: bin/schema
	@rm -rf build/tables-generated-go/cards build/tables-generated-go/cards-initial build/tables-generated-go/cards-future
	@mkdir -p build/tables-generated-go/cards build/tables-generated-go/cards-initial build/tables-generated-go/cards-future
	./bin/schema generate --lang go --out build/tables-generated-go/cards tables/cards
	./bin/schema generate --lang go --out build/tables-generated-go/cards-initial tables/cards/corpus/initial/DECLARATION.schema
	./bin/schema generate --lang go --out build/tables-generated-go/cards-future tables/cards/testdata/upgrade/future/DECLARATION.schema
	$(call go_table_module,cards-initial,cardsinitial)
	$(call go_table_module,cards-future,cardsfuture)
	@printf 'module cards\n\ngo 1.26\n\nrequire (\n\tcardsfuture v0.0.0\n\tcardsinitial v0.0.0\n\t// the shared Go runtime is the default (docs/SPEC-TABLES.md §8.4)\n\tgithub.com/mas-bandwidth/schema/v2 v2.0.0\n\tgithub.com/mas-bandwidth/serialize.go/v2 v2.0.0\n)\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => ../../../$(SERIALIZE_GO)\n\n// the shared Go runtime is the default (docs/SPEC-TABLES.md §8.4)\nreplace github.com/mas-bandwidth/schema/v2 => ../../..\n\nreplace cardsinitial => ../cards-initial\n\nreplace cardsfuture => ../cards-future\n' > build/tables-generated-go/cards/go.mod
	cp tables/cards/cards.go build/tables-generated-go/cards/cards.go
	cp tables/cards/cards_test.go build/tables-generated-go/cards/cards_test.go
	cp tables/cards/parse.go build/tables-generated-go/cards/parse.go
	cp tables/cards/render.go build/tables-generated-go/cards/render.go
	cp tables/cards/parse_test.go build/tables-generated-go/cards/parse_test.go
	cp tables/cards/edit.go build/tables-generated-go/cards/edit.go
	cp tables/cards/edit_test.go build/tables-generated-go/cards/edit_test.go
	cp tables/cards/view_test.go build/tables-generated-go/cards/view_test.go
	cp tables/cards/upgrade.go build/tables-generated-go/cards/upgrade.go
	cp tables/cards/corpus_test.go build/tables-generated-go/cards/corpus_test.go
	cp tables/cards/upgrade_test.go build/tables-generated-go/cards/upgrade_test.go
	cp tables/cards/page_test.go build/tables-generated-go/cards/page_test.go
	@mkdir -p build/tables-generated-go/cards/fuzz
	@if [ -f tables/cards/fuzz/card-populated.wire ]; then cp tables/cards/fuzz/card-populated.wire build/tables-generated-go/cards/fuzz/card-populated.wire; fi
	@if [ -f tables/cards/fuzz/card-populated.json ]; then cp tables/cards/fuzz/card-populated.json build/tables-generated-go/cards/fuzz/card-populated.json; fi
	@mkdir -p build/tables-generated-go/cards/testdata/epoch17
	cp tables/cards/testdata/epoch17/rejected-legacy.md build/tables-generated-go/cards/testdata/epoch17/rejected-legacy.md
	cp tables/cards/testdata/epoch17/normalized-valid.md build/tables-generated-go/cards/testdata/epoch17/normalized-valid.md
	@for c in initial consumers-and-rename; do \
		mkdir -p build/tables-generated-go/cards/corpus/$$c; \
		[ -f tables/cards/corpus/$$c/DECLARATION.schema ] && cp tables/cards/corpus/$$c/DECLARATION.schema build/tables-generated-go/cards/corpus/$$c/DECLARATION.schema; \
		[ -f tables/cards/corpus/$$c/METADATA.txt ] && cp tables/cards/corpus/$$c/METADATA.txt build/tables-generated-go/cards/corpus/$$c/METADATA.txt; \
		for w in tables/cards/corpus/$$c/populated.wire tables/cards/corpus/$$c/empty-consumers.wire; do \
			[ -f "$$w" ] && cp "$$w" "build/tables-generated-go/cards/corpus/$$c/$$(basename $$w)"; \
		done; \
		for j in tables/cards/corpus/$$c/populated.json tables/cards/corpus/$$c/empty-consumers.json; do \
			[ -f "$$j" ] && cp "$$j" "build/tables-generated-go/cards/corpus/$$c/$$(basename $$j)"; \
		done; \
		true; \
	done

# THE CUSTOMER EXAMPLE (schema-cards-c05): a SEPARATE module, cardsexample,
# that imports only the generated `cards` package and the standard library,
# proving local adoption (docs/CARDS.md). Go does not inherit replace
# directives from module cards, so the two replaces this consuming module needs
# are materialized into the owned build output from absolute paths. The c03
# future probe wire is regenerated under build/ from the c03 future
# declaration; no source fixture is written and the source-template go.mod is
# never edited.
cards-example: cards-generate
	@rm -rf build/cards-example
	@mkdir -p build/cards-example/testdata
	sed "s|=> ../../..$$|=> $(CURDIR)|" tables/cards/example/go.mod > build/cards-example/go.mod
	cp tables/cards/example/example_test.go build/cards-example/example_test.go
	@printf '\nreplace cards => %s\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => %s\n' \
		"$(CURDIR)/build/tables-generated-go/cards" "$(abspath $(SERIALIZE_GO))" >> build/cards-example/go.mod
	@mkdir -p build/tables-generated-go/cards-future/probe
	@printf '%s\n' 'package main' '' 'import (' '	"os"' '	"strings"' '' '	cardsfuture "cardsfuture"' ')' '' 'func main() {' '	var b cardsfuture.CardBuilder' '	if !b.Init() {' '		os.Exit(2)' '	}' '	r := b.GetRoot()' '	set := func(dst []byte, n *int32, s string) { copy(dst, s); *n = int32(len(s)) }' '	set(r.ID[:], &r.IDLength, "future-probe")' '	set(r.Sha[:], &r.ShaLength, strings.Repeat("e", 40))' '	r.Tier = cardsfuture.Tierpro' '	set(r.FutureNote[:], &r.FutureNoteLength, "future-value")' '	if !b.Lock() {' '		os.Exit(3)' '	}' '	size := cardsfuture.CardMeasure(b.AsConst())' '	w := make([]byte, size)' '	if cardsfuture.CardSave(b.AsConst(), w) != size {' '		os.Exit(4)' '	}' '	os.Stdout.Write(w)' '}' > build/tables-generated-go/cards-future/probe/main.go
	cd build/tables-generated-go/cards-future && go run "$(CURDIR)/build/tables-generated-go/cards-future/probe" > $(CURDIR)/build/cards-example/testdata/future-note.wire
	cd build/cards-example && go vet ./... && SCHEMA_SLOW=1 CARDS_SOURCE_ROOT=$(CURDIR) CARDS_DOC_PATH=$(CURDIR)/docs/CARDS.md CARDS_DECL_PATH=$(CURDIR)/tables/cards/Card.schema CARDS_FUTURE_WIRE=$(CURDIR)/build/cards-example/testdata/future-note.wire go test -count=1 -timeout 600s -v ./...

# The read-only corpus gate (docs/SPEC-TABLES.md §4, §5, §6.6): the current
# declaration must match the committed current-case declaration, and every
# SHIPPED case and its named fixture evidence must be present. It never creates
# wire/JSON/declaration evidence and never accepts absence as a skip.
cards-corpus-check:
	@cmp -s tables/cards/Card.schema tables/cards/corpus/consumers-and-rename/DECLARATION.schema || { echo "cards corpus check FAILED: Card.schema does not match corpus/consumers-and-rename/DECLARATION.schema"; exit 1; }
	@for c in initial consumers-and-rename; do \
		[ -f tables/cards/corpus/$$c/DECLARATION.schema ] || { echo "cards corpus check FAILED: missing corpus/$$c/DECLARATION.schema"; exit 1; }; \
		[ -f tables/cards/corpus/$$c/METADATA.txt ] || { echo "cards corpus check FAILED: missing corpus/$$c/METADATA.txt"; exit 1; }; \
		grep -q 'class: SHIPPED' tables/cards/corpus/$$c/METADATA.txt || { echo "cards corpus check FAILED: corpus/$$c is not SHIPPED"; exit 1; }; \
	done
	@for f in initial/populated consumers-and-rename/populated consumers-and-rename/empty-consumers; do \
		[ -f tables/cards/corpus/$$f.wire ] || { echo "cards corpus check FAILED: missing corpus/$$f.wire"; exit 1; }; \
		[ -f tables/cards/corpus/$$f.json ] || { echo "cards corpus check FAILED: missing corpus/$$f.json"; exit 1; }; \
	done
	@echo "cards corpus check: declaration pinned, every SHIPPED case and fixture present"

# Fixture authoring: distinct from check, gated, and it refuses to overwrite an
# existing named case. Normal tests never invoke it; it writes into the named
# source corpus directory.
cards-corpus-create: cards-generate
	cd build/tables-generated-go/cards && CARDS_UPDATE_CORPUS=1 go test -run '^TestCorpusFixtureWrite$$' -count=1 -timeout 600s .

cards-test: cards-generate cards-corpus-check
	./bin/schema check tables/cards
	cd build/tables-generated-go/cards && go vet ./... && SCHEMA_SLOW=1 CARDS_DOC_PATH=$(CURDIR)/docs/CARDS.md CARDS_DECL_PATH=$(CURDIR)/tables/cards/Card.schema CARDS_SOURCE_ROOT=$(CURDIR) go test -v -count=1 -timeout 600s ./...
	cd build/tables-generated-go/cards-initial && go vet ./...
	cd build/tables-generated-go/cards-future && go vet ./...
	go test -count=1 -timeout 600s ./internal/ci/ ./tools/agentsmap/

# ---------------------------------------------------------------------------
# THE CARD CONFORMANCE SET (the-card-corpus-is-a-language-neutral-conformance-set)
# ---------------------------------------------------------------------------
# A language-neutral card corpus under tables/cards/conformance (FORMAT.md), the
# Go reference driver that writes its expected answers, and the harness `cards`
# subcommand that runs any language's driver over it and compares bytes. The
# abstract claim is tla/CardHops.tla (ImplementationsAgree, HopOrderIrrelevant);
# this is its concrete side. Every leg's cards-<lang>-conformance target runs
# the harness `cards` subcommand with --require $(CARDS_REQUIRE); card targets
# live in that leg's own make fragment.
CARDS_REQUIRE ?= load,save,render,parse,bounds,upgrade,edit,taskhash,view

# THE GO REFERENCE DRIVER: cards-driver-go copies the handwritten driver into the
# generated cards module (so it imports `cards` in process) and builds
# build/cards-driver-go.
.PHONY: cards-driver-go
cards-driver-go: cards-generate
	@mkdir -p build/tables-generated-go/cards/cmd/cards-driver
	cp tables/cards/cmd/cards-driver/main.go build/tables-generated-go/cards/cmd/cards-driver/main.go
	cd build/tables-generated-go/cards/cmd/cards-driver && go build -o "$(CURDIR)/build/cards-driver-go" .

# THE CREATE TARGET: generate the large bounds inputs and the future-declaration
# wire into cases/, then run the Go driver into expected/. It REFUSES to
# overwrite an existing expected file, and a normal test never runs it. The
# future wire is built the way `make cards-example` builds future-note.wire, from
# the c03 future declaration the current reader must retain.
.PHONY: cards-conformance-create
cards-conformance-create: cards-driver-go
	@if [ -n "$$(ls tables/cards/conformance/expected/ 2>/dev/null)" ]; then \
		echo "cards-conformance-create REFUSED: tables/cards/conformance/expected is not empty"; exit 1; fi
	@mkdir -p build/tables-generated-go/cards-future/probe
	@printf '%s\n' 'package main' '' 'import (' '	"os"' '	"strings"' '' '	cardsfuture "cardsfuture"' ')' '' 'func main() {' '	var b cardsfuture.CardBuilder' '	if !b.Init() {' '		os.Exit(2)' '	}' '	r := b.GetRoot()' '	set := func(dst []byte, n *int32, s string) { copy(dst, s); *n = int32(len(s)) }' '	set(r.ID[:], &r.IDLength, "future-probe")' '	set(r.Sha[:], &r.ShaLength, strings.Repeat("e", 40))' '	r.Tier = cardsfuture.Tierpro' '	set(r.FutureNote[:], &r.FutureNoteLength, "future-value")' '	if !b.Lock() {' '		os.Exit(3)' '	}' '	size := cardsfuture.CardMeasure(b.AsConst())' '	w := make([]byte, size)' '	if cardsfuture.CardSave(b.AsConst(), w) != size {' '		os.Exit(4)' '	}' '	os.Stdout.Write(w)' '}' > build/tables-generated-go/cards-future/probe/main.go
	cd build/tables-generated-go/cards-future && go run "$(CURDIR)/build/tables-generated-go/cards-future/probe" > "$(CURDIR)/tables/cards/conformance/cases/future-note.wire"
	@printf '{"id":"x","body":"' > tables/cards/conformance/cases/bounds-body-65536.json
	@head -c 65536 /dev/zero | tr '\0' 'y' >> tables/cards/conformance/cases/bounds-body-65536.json
	@printf '"}' >> tables/cards/conformance/cases/bounds-body-65536.json
	@printf '{"id":"x","body":"' > tables/cards/conformance/cases/bounds-body-65537.json
	@head -c 65537 /dev/zero | tr '\0' 'y' >> tables/cards/conformance/cases/bounds-body-65537.json
	@printf '"}' >> tables/cards/conformance/cases/bounds-body-65537.json
	@for spec in 'paths paths {"s":"p"}' 'new _new {"s":"n"}' 'shared shared {"s":"s"}' 'needs needs {"s":"d"}' 'consumers consumers {"kind":"k"}'; do \
		set -- $$spec; suffix=$$1; field=$$2; elem=$$3; \
		for n in 512 513; do \
			out=tables/cards/conformance/cases/bounds-$$suffix-$$n.json; \
			printf '{"id":"x","%s":[' "$$field" > $$out; \
			i=1; while [ $$i -le $$n ]; do \
				[ $$i -gt 1 ] && printf ',' >> $$out; \
				printf '%s' "$$elem" >> $$out; \
				i=$$((i+1)); \
			done; \
			printf ']}' >> $$out; \
		done; \
	done
	@for s in load save render parse bounds upgrade edit taskhash view; do \
		./build/cards-driver-go tables/cards/conformance/MANIFEST.txt $$s tables/cards/conformance/expected; \
	done
	@echo "cards-conformance-create: wrote the Go reference answers into tables/cards/conformance/expected"

# THE GO CHECK: the STEP-5 `cards` subcommand over the Go reference driver. It
# writes the derived manifest into build/, runs the driver per surface into a
# fresh outdir, and compares each answer byte for byte with expected/.
.PHONY: cards-conformance-go
cards-conformance-go: build/conformance-harness cards-driver-go
	./build/conformance-harness cards --corpus tables/cards/conformance --driver 'build/cards-driver-go' --require $(CARDS_REQUIRE)

# ONE GATE FOR EVERY LEG (the-cards-page-and-roadmap-name-every-language): runs
# cards-conformance-go and every cards-<lang>-conformance target across all nine
# languages with CARDS_REQUIRE set to all nine surfaces (load, save, render,
# parse, bounds, upgrade, edit, taskhash, view), continuing after a failure so
# the full matrix is collected, prints one `<leg> <surface> pass n/m` (or FAIL /
# absent) line per leg and surface, and exits nonzero if any leg fails, answers
# a required surface absent, or has no target.
CARDS_ALL_SURFACES := load,save,render,parse,bounds,upgrade,edit,taskhash,view
.PHONY: cards-conformance-all
cards-conformance-all:
	@mkdir -p build/cards-conformance-all
	@fail=0; \
	for leg in go c cpp cs rust java js dart elixir lua; do \
		if [ "$$leg" = "go" ]; then target=cards-conformance-go; else target=cards-$$leg-conformance; fi; \
		log=build/cards-conformance-all/$$leg.log; \
		if ! $(MAKE) -n $$target >/dev/null 2>&1; then \
			echo "$$leg FAIL: missing target $$target"; \
			fail=1; \
			continue; \
		fi; \
		if $(MAKE) --no-print-directory $$target CARDS_REQUIRE=$(CARDS_ALL_SURFACES) CARDS_CPP_SURFACES=$(CARDS_ALL_SURFACES) CARDS_JAVA_REQUIRE=$(CARDS_ALL_SURFACES) > "$$log" 2>&1; then \
			status=0; \
		else \
			status=$$?; fail=1; \
		fi; \
		lines=$$(sed -n "s/^cards /$$leg /p" "$$log"); \
		if [ -z "$$lines" ]; then \
			cat "$$log"; \
			echo "$$leg FAIL: $$target exited $$status without surface lines"; \
			fail=1; \
			continue; \
		fi; \
		printf '%s\n' "$$lines"; \
		for s in load save render parse bounds upgrade edit taskhash view; do \
			printf '%s\n' "$$lines" | grep -qE "^$$leg $$s pass [1-9][0-9]*/[1-9][0-9]*$$" || fail=1; \
		done; \
	done; \
	exit $$fail

# THE NEGATIVE CONTROL (STEP 6): copy expected/ into build/, flip one byte of one
# render answer and one taskhash answer, run the check against the copy, and
# require it to fail naming those two surfaces while the rest pass.
.PHONY: cards-conformance-negative-control
cards-conformance-negative-control: build/conformance-harness cards-driver-go
	@rm -rf build/cards-conformance-neg && mkdir -p build/cards-conformance-neg/expected
	cp tables/cards/conformance/expected/* build/cards-conformance-neg/expected/
	@printf '\000' | dd of=build/cards-conformance-neg/expected/card-populated.txt bs=1 seek=0 count=1 conv=notrunc 2>/dev/null
	@c=$$(head -c 1 build/cards-conformance-neg/expected/card-populated.hex); \
	if [ "$$c" = "0" ]; then v=1; else v=0; fi; \
	printf '%s' "$$v" | dd of=build/cards-conformance-neg/expected/card-populated.hex bs=1 seek=0 count=1 conv=notrunc 2>/dev/null
	@if ./build/conformance-harness cards --corpus tables/cards/conformance --expected build/cards-conformance-neg/expected --driver 'build/cards-driver-go' --require $(CARDS_REQUIRE) > build/cards-conformance-neg/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the card check stayed green with one render and one taskhash answer byte changed'; exit 1; fi
	@grep -qE 'cards render .*FAIL' build/cards-conformance-neg/log || { echo 'NEGATIVE CONTROL FAILED: render did not go red'; cat build/cards-conformance-neg/log; exit 1; }
	@grep -qE 'cards taskhash .*FAIL' build/cards-conformance-neg/log || { echo 'NEGATIVE CONTROL FAILED: taskhash did not go red'; cat build/cards-conformance-neg/log; exit 1; }
	@grep -qE 'cards (load|save|parse|bounds|upgrade|edit|view) pass' build/cards-conformance-neg/log || { echo 'NEGATIVE CONTROL FAILED: the other surfaces did not stay green'; cat build/cards-conformance-neg/log; exit 1; }
	@echo 'cards negative control: one changed render and taskhash byte turns exactly those surfaces red'

test-go: cards-conformance-go cards-conformance-negative-control

# The populated fixture is created ONLY under an explicit update flag, into the
# named source fuzz directory; ordinary tests read the committed evidence and
# fail when it is missing (docs/SPEC-TABLES.md §16, §7).
cards-fuzz-fixture: cards-generate
	cd build/tables-generated-go/cards && CARDS_UPDATE_FIXTURE=1 go test -run '^TestCardFixtureWrite$$' -count=1 -timeout 600s .

# THE CARD WIRE-FUZZ GATE: the existing oracle harness over the Card root only.
# --vectors '' keeps the default pinned vectors (other units) out of this
# Card-only manifest. The gate refuses a zero-coverage roster: the harness
# errors when the leg has a codec for none of the named roots, and the greps
# below hold the nonzero seed/mutant claim explicitly.
#
# THE SIBLING RUNTIME IS A STATED PREREQUISITE: build/conformance-go links the
# serialize.go checkout ($(SERIALIZE_GO)), a sibling this tree does not carry.
# A machine without it must not fail with a go.mod replacement error mid-build;
# it skips BY NAME and the same target runs in CI, which clones the sibling.
cards-wire-fuzz:
	@if [ -f "$(SERIALIZE_GO)/go.mod" ]; then \
		$(MAKE) cards-wire-fuzz-run; \
	else \
		echo "cards-fuzz SKIPPED: the sibling runtime '$(SERIALIZE_GO)' (the serialize.go checkout) is absent; it is a stated bench prerequisite and is present in CI"; \
	fi

cards-wire-fuzz-run: build/conformance-harness build/conformance-go
	@timeout $(CARDS_FUZZ_TIME) ./build/conformance-harness wire-fuzz \
		--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \
		--driver 'build/conformance-go wire-fuzz' --seed 24845619678 --n 100 \
		> build/cards-wire-fuzz.log 2>&1; \
		status=$$?; cat build/cards-wire-fuzz.log; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@grep -q '0 divergences' build/cards-wire-fuzz.log
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' build/cards-wire-fuzz.log
	@grep -qE '[1-9][0-9]* mutants' build/cards-wire-fuzz.log

cards-fuzz: cards-wire-fuzz

test-go: cards-test cards-example cards-wire-fuzz

# one generated unit's module wiring (build wiring, not schema output — the
# emitter writes only .go files)
# The shared Go runtime is the default (docs/SPEC-TABLES.md §8.4), so every
# generated table module requires this module and replaces it with the tree.
define go_table_module
@printf 'module %s\n\ngo 1.26\n\nrequire (\n\t// the shared Go runtime is the default (docs/SPEC-TABLES.md §8.4)\n\tgithub.com/mas-bandwidth/schema/v2 v2.0.0\n\tgithub.com/mas-bandwidth/serialize.go/v2 v2.0.0\n)\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => ../../../$(SERIALIZE_GO)\n\n// the shared Go runtime is the default (docs/SPEC-TABLES.md §8.4)\nreplace github.com/mas-bandwidth/schema/v2 => ../../..\n' $(2) > build/tables-generated-go/$(1)/go.mod
endef

# THE STANDALONE GATE, Go — the M4b twin of `tables-cs-standalone`,
# `tables-js-standalone` and `tables-dart-standalone` (docs/PORTING.md): a
# generated <Package>Table.go carries the unit's table runtime — its cursor,
# its readers and its writers — INSIDE the file rather than importing it from
# a sibling runtime module, so what the unit reaches for a table codec is the
# platform's own library. The one external spelling the form admits is the
# 128-bit scalar TYPE the sibling runtime owns (`serialize.Int128`,
# `serialize.Uint128`, `serialize.Int128From64`), named only by a unit whose
# closure declares a 128-bit field; any other symbol of that module marks the
# unit as dependent and is refused. The scan runs over the CODE, with every
# // comment stripped first, on the same terms as the C++ zero-cost and
# view-containment gates: the banner's "serialize.go storage" is prose, not a
# dependency.
.PHONY: tables-go-standalone
tables-go-standalone: build/tables-generated-go/.stamp
	@n=$$(ls build/tables-generated-go/*/*Table.go 2>/dev/null | wc -l | tr -d ' '); \
		if [ "$$n" -lt 8 ]; then \
			echo "STANDALONE GATE FAILED: found $$n generated Table sources, expected at least 8 — the glob, not the property, is what broke"; exit 1; \
		fi
	@for f in build/tables-generated-go/*/*Table.go; do \
		bad=$$(sed -E 's://.*$$::' $$f | grep -oE 'serialize\.[A-Za-z0-9_]+' | sort -u | grep -vxE 'serialize\.(Int128|Uint128|Int128From64|go)'); \
		if [ -n "$$bad" ]; then \
			echo "STANDALONE GATE FAILED: the sibling runtime leaked into $$f beyond its 128-bit scalar type:"; \
			echo "$$bad"; exit 1; \
		fi; \
	done
	@echo "tables Go standalone gate: generated Table sources carry the runtime inline, naming the sibling only for its 128-bit scalar type"

.PHONY: tables-go-soak
tables-go-soak: build/tables-generated-go/.stamp
	cd test/go-tables && go test -run Soak -timeout 0 -soak-iterations $(TABLES_GO_SOAK_ITERATIONS) -v .

# FUZZ-TABLES-GO: the permanent native fuzz target over the table compiler
# front end and the Go table emitter (internal/fuzz/tablefrontend_test.go).
# The long campaign runs by hand or on the certify nightly, never in ci-fast or
# ci-full. FUZZTIME and FUZZPARALLEL default to 10m and 4.
FUZZTIME ?= 10m
FUZZPARALLEL ?= 4
.PHONY: fuzz-tables-go
fuzz-tables-go:
	go test ./internal/fuzz/ -run '^$$' -fuzz '^FuzzTableFrontEnd$$' -fuzztime $(FUZZTIME) -parallel $(FUZZPARALLEL) -timeout 600s

# FUZZ-GENERATED-READERS-GO: a functional smoke over every generated Go table
# reader (internal/codegen/gotable/fuzzunit). The targets are listed from the
# package, so a new reader or entry-point kind joins with no edit here; each
# runs briefly by hand or on the certify nightly, never in ci-fast or ci-full.
.PHONY: fuzz-generated-readers-go
fuzz-generated-readers-go:
	@targets=$$(nice -n 19 go test -list '^Fuzz' -timeout 600s ./internal/codegen/gotable/fuzzunit/ | grep '^Fuzz'); \
	if [ -z "$$targets" ]; then echo "FUZZ-GENERATED-READERS-GO: no Fuzz targets"; exit 1; fi; \
	for name in $$targets; do \
		race=; \
		if [ "$$name" = FuzzOpen ]; then race=-race; fi; \
		echo "fuzz-generated-readers-go: $$name 10s"; \
		nice -n 19 go test ./internal/codegen/gotable/fuzzunit/ -run '^$$' -fuzz "^$$name$$" -fuzztime 10s -timeout 600s $$race || exit 1; \
	done

.PHONY: tables-go-fuzz
tables-go-fuzz: build/tables-generated-go/.stamp build/conformance-harness
	./build/conformance-harness run --drivers /dev/null --work build/conformance > /dev/null 2>&1 || true
	cd test/go-tables && SEED=$(SEED) N=$(N) go test -run Fuzz -count 1 .
	cd test/go-tables && SEED=$(SEED) N=$(N) go test -race -run Fuzz -count 1 .

# The sed programs that remove one check from the GO emitter, and the sabotage
# that proves the fuzzer finds it. Same shape as the C++/C# pair above, through
# `go build -overlay`, so no tracked file is edited.
# The sabotage is a SUBSTITUTION and not a deletion, and the reason is that a
# deleted check takes its variables with it: removing the rows test leaves
# `rows` declared and unused, and the generated Go then does not compile, which
# proves nothing. Turning the condition into `false` removes exactly the check
# and nothing else.
GO_FUZZ_SED_extent := s|if rows > uint64(bytes)-offsetOf {|if false {|; s|if padding > bytes-used {|if false {|
GO_FUZZ_SED_maximum := s|if count > %d {|if false \&\& count > %d {|

# what the fuzzer must say when it goes red, so a control that turned some
# OTHER leg red is not mistaken for a pass
GO_FUZZ_EXPECT_extent := leave an extent|used bytes inside an extent
GO_FUZZ_EXPECT_maximum := past the declared maximum

define go_fuzz_sabotage
	@rm -rf build/go-fuzz-$(1) && mkdir -p build/go-fuzz-$(1)
	@sed '$(GO_FUZZ_SED_$(1))' internal/codegen/gotable/block.go > build/go-fuzz-$(1)/gotable-block.go.txt
	@cmp -s internal/codegen/gotable/block.go build/go-fuzz-$(1)/gotable-block.go.txt && \
		{ echo "NEGATIVE CONTROL: the Go emitter sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/gotable/block.go":"%s/build/go-fuzz-$(1)/gotable-block.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/go-fuzz-$(1)/overlay.json
	go build -overlay build/go-fuzz-$(1)/overlay.json -o build/go-fuzz-$(1)/schema ./cmd/schema
	@rm -rf build/go-fuzz-$(1)/generated
	./build/go-fuzz-$(1)/schema generate --lang go --out build/go-fuzz-$(1)/generated/block tables/block
	./build/go-fuzz-$(1)/schema generate --lang go --out build/go-fuzz-$(1)/generated/pointers tables/pointers
	@printf 'module blockdemo\n\ngo 1.23\n' > build/go-fuzz-$(1)/generated/block/go.mod
	@printf 'module graphdemo\n\ngo 1.23\n' > build/go-fuzz-$(1)/generated/pointers/go.mod
	@sed -e 's|=> ../../build/tables-generated-go/block|=> "$(CURDIR)/build/go-fuzz-$(1)/generated/block"|' \
	     -e 's|=> ../../build/tables-generated-go/pointers|=> "$(CURDIR)/build/go-fuzz-$(1)/generated/pointers"|' \
	     test/go-tables/go.mod > build/go-fuzz-$(1)/go.mod.txt
	@printf '{"Replace":{"%s/test/go-tables/go.mod":"%s/build/go-fuzz-$(1)/go.mod.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/go-fuzz-$(1)/modoverlay.json
	@if ( cd test/go-tables && SEED=$(SEED) N=$(N) go test -overlay ../../build/go-fuzz-$(1)/modoverlay.json \
			-run BlockForgeryFuzz -count 1 . ) > build/go-fuzz-$(1)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the Go fuzzer stayed green with the $(1) check removed from the emitter"; \
		cat build/go-fuzz-$(1)/log; exit 1; \
	fi
	@grep -q "fuzz_test.go" build/go-fuzz-$(1)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the Go leg went red, but not on the oracle"; cat build/go-fuzz-$(1)/log; exit 1; }
	@grep -qE "$(GO_FUZZ_EXPECT_$(1))" build/go-fuzz-$(1)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the oracle went red on some other check, not the $(1) one"; \
		  cat build/go-fuzz-$(1)/log; exit 1; }
	@grep -m1 "fuzz_test.go" build/go-fuzz-$(1)/log
	@echo "go fuzz $(1) negative control: removing that check from the Go emitter turns the fuzzer red"
endef

.PHONY: tables-go-fuzz-extent-negative-control
tables-go-fuzz-extent-negative-control: build/tables-generated-go/.stamp
	$(call go_fuzz_sabotage,extent)

.PHONY: tables-go-fuzz-maximum-negative-control
tables-go-fuzz-maximum-negative-control: build/tables-generated-go/.stamp
	$(call go_fuzz_sabotage,maximum)

# The runtime the committed Go bench trees are generated with, and the runtime
# the rules below pass. One source: internal/ci/gobenchcurrent_test.go's
# goBenchRuntime pins the same spelling, and
# TestGeneratedCurrentEmitsStandaloneBenchTrees holds the two to it.
GO_BENCH_RUNTIME := standalone

generated/bench/tables/go/.stamp: bin/schema bench/corpus/BenchTable.schema
	@mkdir -p generated/bench/tables/go
	./bin/schema generate --lang go --runtime $(GO_BENCH_RUNTIME) --out generated/bench/tables/go bench/corpus/BenchTable.schema
	@printf 'module benchtable\n\ngo 1.24\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => ../../../../$(SERIALIZE_GO)\n' > generated/bench/tables/go/go.mod

# Keep both compiler-owned source and module wiring in the discovered generation
# graph. Default wiring is byte-identical to the paired driver's generated unit.
generated/bench/paired/go/.stamp: bin/schema bench/corpus/Bench.schema bench/corpus/FixedTable.schema make/go.mk
	@mkdir -p generated/bench/paired/go
	./bin/schema generate --lang go --runtime $(GO_BENCH_RUNTIME) --out generated/bench/paired/go bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	@printf 'module benchtable\n\ngo 1.24\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => ../../../../$(SERIALIZE_GO)\n' > generated/bench/paired/go/go.mod
	@touch $@

generated/bench/go/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang go --runtime $(GO_BENCH_RUNTIME) --out generated/bench/go bench/corpus/Bench.schema
	./bin/schema generate --lang go --runtime $(GO_BENCH_RUNTIME) --out generated/bench/go/realworld bench/corpus/RealWorld.schema
	@printf 'module bench\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => ../../../$(SERIALIZE_GO)\n' > generated/bench/go/go.mod
	@touch $@

build/conformance-go: build/tables-generated-go/.stamp cards-generate $(wildcard test/conformance/go/*.go) test/conformance/go/go.mod
	@mkdir -p build
	cd test/conformance/go && go build -o ../../../$@ .

# THE GO LEG, CROSS-BUILT BIG-ENDIAN (docs/SPEC-TABLES.md §3, §19.1, §7). Go
# cross-compiles, and the pinned emulator is already installed for the C++
# big-endian legs above, so the table WIRE's own claim — every field id, every
# length and every scalar rides little-endian whatever the host is — becomes a
# gate rather than a sentence: a big-endian reader loads the goldens a
# little-endian host wrote, writes them back and is byte-compared.
#
# The driver lists the byte-order-NEUTRAL surfaces and no others, and the reason
# is in test/conformance/go/driver-be: a block and a cook are produced in the
# order of the build that wrote them, so a big-endian reader is CORRECT to
# refuse this corpus's fixtures and has no neutral verdict to give.
build/conformance-go-be: build/tables-generated-go/.stamp cards-generate $(wildcard test/conformance/go/*.go) test/conformance/go/go.mod
	@mkdir -p build
	cd test/conformance/go && GOOS=linux GOARCH=s390x go build -o ../../../$@ .

.PHONY: conformance-big-endian
conformance-big-endian: build/conformance-harness build/conformance-go-be
	@printf 'go test/conformance/go/driver-be\n' > build/conformance-be-drivers.txt
	./build/conformance-harness run --drivers build/conformance-be-drivers.txt --work build/conformance-be-work
	@echo "big-endian leg: the Go table wire, the read report and the text form cross the byte order"

# ITS NEGATIVE CONTROL (docs/PORTING.md M12, I7), on the C++ big-endian
# control's rule: one byte-order-neutral read is put back to HOST order and the
# leg must go RED on the target while the same sabotaged driver stays GREEN on
# this little-endian host — which is the whole argument for running the leg.
# Every generated unit carries its own wire reader, and each one's 32-bit read
# becomes binary.NativeEndian through `go build -overlay`, so no file under
# build/ or the tree is written to. The leg is named `go` because it IS the Go
# port: the corpus's carried-by rows name ports, not drivers. It runs in
# ci-full.yml's big-endian job (row go-control), which installs the emulator;
# the shared negative-control rows do not, so it is in the manifest's exclusion
# list with that reason.
GO_BE_NC := build/go-be-negative-control
.PHONY: conformance-big-endian-negative-control
conformance-big-endian-negative-control: build/conformance-harness build/conformance-go-be build/conformance-go
	@rm -rf $(GO_BE_NC) && mkdir -p $(GO_BE_NC)
	@entries=""; n=0; for f in $$(grep -l 'v := binary.LittleEndian.Uint32(r.Buffer\[r.Offset:\])' build/tables-generated-go/*/*.go); do \
		copy=$(GO_BE_NC)/$$(echo $$f | tr '/' '_').gotext; \
		sed 's/v := binary.LittleEndian.Uint32(r.Buffer\[r.Offset:\])/v := binary.NativeEndian.Uint32(r.Buffer[r.Offset:]) \/\/ SABOTAGED: host order/' $$f > $$copy; \
		entry="\"$(CURDIR)/$$f\":\"$(CURDIR)/$$copy\""; \
		if [ -z "$$entries" ]; then entries=$$entry; else entries="$$entries,$$entry"; fi; n=$$((n+1)); \
	done; \
	[ $$n -gt 0 ] || { echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }; \
	printf '{"Replace":{%s}}\n' "$$entries" > $(GO_BE_NC)/overlay.json; \
	echo "host-order 32-bit read in $$n generated units"
	cd test/conformance/go && GOOS=linux GOARCH=s390x go build -overlay $(CURDIR)/$(GO_BE_NC)/overlay.json -o $(CURDIR)/$(GO_BE_NC)/driver-be-bin .
	cd test/conformance/go && go build -overlay $(CURDIR)/$(GO_BE_NC)/overlay.json -o $(CURDIR)/$(GO_BE_NC)/driver-host-bin .
	@sed 's|build/conformance-go-be|$(GO_BE_NC)/driver-be-bin|' test/conformance/go/driver-be > $(GO_BE_NC)/driver-be && chmod +x $(GO_BE_NC)/driver-be
	@grep -q '$(GO_BE_NC)/driver-be-bin' $(GO_BE_NC)/driver-be || { echo "NEGATIVE CONTROL FAILED: the target driver was not substituted"; exit 1; }
	@printf '#!/bin/sh\nexec $(GO_BE_NC)/driver-host-bin "$$@"\n' > $(GO_BE_NC)/driver-host && chmod +x $(GO_BE_NC)/driver-host
	@printf 'go $(GO_BE_NC)/driver-host\n' > $(GO_BE_NC)/host.txt
	@printf 'go $(GO_BE_NC)/driver-be\n' > $(GO_BE_NC)/be.txt
	@./build/conformance-harness run --drivers $(GO_BE_NC)/host.txt --work $(GO_BE_NC)/host-work > $(GO_BE_NC)/host.log 2>&1 || \
		{ echo "NEGATIVE CONTROL FAILED: the host-order read is red on the little-endian HOST too, so the target run would prove nothing about byte order"; cat $(GO_BE_NC)/host.log; exit 1; }
	@if ./build/conformance-harness run --drivers $(GO_BE_NC)/be.txt --work $(GO_BE_NC)/be-work > $(GO_BE_NC)/be.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a host-order read left the big-endian leg GREEN"; cat $(GO_BE_NC)/be.log; exit 1; \
	fi
	@grep -q 'go / wire:' $(GO_BE_NC)/be.log || { echo "NEGATIVE CONTROL FAILED: the big-endian leg went red, but not on the wire"; cat $(GO_BE_NC)/be.log; exit 1; }
	@echo "big-endian negative control (Go): a host-order read is green on the host and red on the target"

# THE GO LEG's NEGATIVE CONTROL, on the same rule as the C++ one: a harness
# that has never gone red on a leg is watching that leg. One byte of ONE wire
# answer is flipped in a COPY of the Go driver — no tracked file is written to,
# so an interrupt cannot leave a sabotaged working tree — and the matrix must go
# red, on that surface and on no other.
.PHONY: conformance-negative-control-go
conformance-negative-control-go: build/conformance-harness build/conformance-go
	sh test/conformance/go/negative-control wire

.PHONY: conformance-negative-control-go-walk
conformance-negative-control-go-walk: build/conformance-harness build/conformance-go
	sh test/conformance/go/negative-control walk

# THE FRAMING SURFACE's own controls (docs/LAYERS/1-framing.md): the generated
# Go reader's 4096 id-table entry cap is removed, and then its refusal of a
# pointer naming an open node; each turns its own framing row red
# (frame_entries_4097, node_cycle_self) while the wire surface stays green.
.PHONY: conformance-negative-control-go-framing
conformance-negative-control-go-framing: build/conformance-harness build/conformance-go
	sh test/conformance/go/negative-control frame-cap
	sh test/conformance/go/negative-control frame-cycle

# THE GO LEG of `make test`: the two conformance negative controls, THE GO
# PORT's own instruments (docs/SPEC-TABLES.md) — the allocation gate and its
# negative control, the forgery fuzzer plain and under -race, and the soak's
# short default count; the hour is `make tables-go-soak` — the standalone gate, the
# bench units' compile gates, and the packet tests.
.PHONY: test-go
test-go: generated/bench/tables/go/.stamp generated/bench/paired/go/.stamp generated/go/.stamp generated/go-ludicrous/.stamp generated/bench/go/.stamp
	$(MAKE) conformance-negative-control-go
	$(MAKE) conformance-negative-control-go-walk
	$(MAKE) conformance-negative-control-go-framing
	$(MAKE) tables-go-json-walk
	$(MAKE) tables-go-standalone
	$(MAKE) tables-go-fuzz
	cd test/go-tables && go test -count 1 .
	$(MAKE) tables-go-fuzz-extent-negative-control
	$(MAKE) tables-go-fuzz-maximum-negative-control
	bench/tables/go/leg build
	cd generated/bench/go && go build ./...
	cd generated/bench/paired/go && go build ./...
	cd test/go && go run .
	cd test/go-ludicrous && go run .

TEST_LEGS         += test-go
CONFORMANCE_LEGS  += build/conformance-go
BENCH_TABLES_LEGS += generated/bench/tables/go/.stamp


build/packet-wide/go/.stamp: bin/schema build/packet-wide/source/WideText.schema test/packet-wide/Shapes.schema make/go.mk
	./bin/schema generate --lang go --out build/packet-wide/go build/packet-wide/source/WideText.schema
	./bin/schema generate --lang go --out build/packet-wide/go/shapes test/packet-wide/Shapes.schema
	@printf 'module packetwide\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => "%s/$(SERIALIZE_GO)"\n' "$(CURDIR)" > build/packet-wide/go/go.mod
	@touch $@

.PHONY: packet-wide-go packet-wide-go-negative-control
packet-wide-go: build/packet-wide/go/.stamp build/packet-wide/cpp/driver build/packet-text/harness
	cd test/packet-wide/go && go test -count=1 . && go build -o ../../../build/packet-wide/go/driver .
	./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver ./build/packet-wide/go/driver

packet-wide-go-negative-control: packet-wide-go
	@mkdir -p build/packet-wide/go-negative/checker
	go run ./tools/sabotage -name packet-wide-go-pairing -out build/packet-wide/go-negative/wstring.gotext internal/codegen/golang/wstring.go
	@printf '{"Replace":{"%s/internal/codegen/golang/wstring.go":"%s/build/packet-wide/go-negative/wstring.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-wide/go-negative/overlay.json
	go run -overlay=build/packet-wide/go-negative/overlay.json ./cmd/schema generate --lang go --out build/packet-wide/go-negative build/packet-wide/source/WideText.schema
	cp build/packet-wide/go/go.mod build/packet-wide/go-negative/go.mod
	cp test/packet-wide/go/main.go build/packet-wide/go-negative/checker/main.go
	cd build/packet-wide/go-negative/checker && go build -o ../driver .
	@if ./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver -mutations-only ./build/packet-wide/go-negative/driver > build/packet-wide/go-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: Go wide pairing removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-wide/go-negative/log || { cat build/packet-wide/go-negative/log; exit 1; }
	@echo 'packet wide Go negative control: removed pairing fails bit-flip agreement'

test-go: packet-wide-go packet-wide-go-negative-control

.PHONY: tables-go-wire-fuzz tables-go-wire-fuzz-negative-control
tables-go-wire-fuzz: build/conformance-harness build/conformance-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s ./internal/codegen/gotable -run '^TestGoFileLoadOfDamagedFeedChainSavesWhatTheOracleSaves$$'
	./build/conformance-harness wire-fuzz --driver 'build/conformance-go wire-fuzz' --seed $(SEED) --n $(N)

tables-go-wire-fuzz-negative-control: build/conformance-harness build/conformance-go
	sh test/conformance/go/negative-control leb

test-go: tables-go-wire-fuzz tables-go-wire-fuzz-negative-control

.PHONY: tables-go-containers tables-go-containers-negative-controls
tables-go-containers:
	sh test/slowgate/proof tables-go-containers \
		'TestRegionLists TestListsThroughUnionArrays TestBuilderCountRecovery TestNestedTerminationAndStringDefault TestRegionMaps TestMapReadReports TestMapJsonKeyDomains' \
		./internal/codegen/gotable -run 'Test(RegionLists|ListsThroughUnionArrays|BuilderCountRecovery|NestedTerminationAndStringDefault|RegionMaps|MapReadReports|MapJsonKeyDomains)' -count=1
tables-go-containers-negative-controls:
	@set -e; for mode in sort ascending duplicate key-domain dead cap; do sh test/conformance/go/container-negative-control $$mode; done

test-go: tables-go-containers tables-go-containers-negative-controls

# THE ACCESSOR/DESCRIPTOR AGREEMENT GATE, the Go half of the JavaScript J1
# technique (docs/PORTING.md, schema#421): the generated accessor and the
# generated descriptor are two independent derivations of one layout, so the
# leg reads every field of a block and of a cook both ways and requires
# agreement — including a pointer's SLOT, whose position is what a
# self-relative delta is relative to (§6.3). The two controls move one
# derivation — a generated block scalar four bytes, a cook pointer slot eight —
# and each must turn the gate red, or the accessor half could be reading the
# descriptors twice and nobody would know.
.PHONY: tables-go-accessor-descriptor-agreement tables-go-accessor-negative-control tables-go-slot-negative-control
tables-go-accessor-descriptor-agreement:
	sh test/slowgate/proof tables-go-accessor-descriptor-agreement \
		'TestAccessorDescriptorAgreement' \
		./internal/codegen/gotable -run '^TestAccessorDescriptorAgreement$$' -count=1
tables-go-accessor-negative-control:
	sh test/slowgate/proof tables-go-accessor-negative-control \
		'TestAccessorDescriptorAgreementScalarNegativeControl' \
		./internal/codegen/gotable -run '^TestAccessorDescriptorAgreementScalarNegativeControl$$' -count=1
tables-go-slot-negative-control:
	sh test/slowgate/proof tables-go-slot-negative-control \
		'TestAccessorDescriptorAgreementSlotNegativeControl' \
		./internal/codegen/gotable -run '^TestAccessorDescriptorAgreementSlotNegativeControl$$' -count=1
test-go: tables-go-accessor-descriptor-agreement tables-go-accessor-negative-control tables-go-slot-negative-control

# THE RUNTIME HOME GATE, the Go half of the C# J2 technique (docs/PORTING.md,
# schema#422): a unit's shared table runtime lives in <Package>Table.go, named
# by the PACKAGE and never by the file that happens to sort first, so a schema
# file that sorts earlier relocates nothing. The gate adds exactly such a file
# (Aaa.schema, ahead of Guarded.schema) to a COPY of tables/examples and
# requires the home not to move. The negative control puts the file-order rule
# back — through `go build -overlay`, so no tracked file is written — and
# requires the home to move.
.PHONY: tables-go-runtime-home tables-go-runtime-home-negative-control
tables-go-runtime-home:
	sh test/slowgate/proof tables-go-runtime-home \
		'TestSharedRuntimeHomeIsPackageNamed' \
		./internal/codegen/gotable -run '^TestSharedRuntimeHomeIsPackageNamed$$' -count=1
tables-go-runtime-home-negative-control: bin/schema
	@mkdir -p build
	@sed 's|home := ir.GoExportName(u.Package)|home := ir.ProtocolIdHome(u) // SABOTAGED: back to the file order|' \
		internal/codegen/gotable/gotable.go > build/goruntime-fileorder.gotext
	@grep -q SABOTAGED build/goruntime-fileorder.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/gotable/gotable.go":"%s/build/goruntime-fileorder.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/goruntime-overlay.json
	@go build -overlay=build/goruntime-overlay.json -o build/schema-goruntime-sabotaged ./cmd/schema
	@rm -rf build/runtime-home-go && mkdir -p build/runtime-home-go/src
	@cp tables/examples/*.schema build/runtime-home-go/src/
	@printf 'package tabledemo\n\ntable AaaRow\n{\n    tag uint8\n}\n' > build/runtime-home-go/src/Aaa.schema
	@./build/schema-goruntime-sabotaged generate --lang go --out build/runtime-home-go/base-sabotage tables/examples
	@./build/schema-goruntime-sabotaged generate --lang go --out build/runtime-home-go/added-sabotage build/runtime-home-go/src
	@base=$$(cd build/runtime-home-go/base-sabotage && grep -l "type TableTypeInfo struct" *Table.go); \
	 added=$$(cd build/runtime-home-go/added-sabotage && grep -l "type TableTypeInfo struct" *Table.go); \
	 rm -rf build/runtime-home-go; \
	 if [ "$$base" = "$$added" ]; then \
		echo "NEGATIVE CONTROL FAILED: the file-order rule kept the runtime in $$base — the gate is watching nothing"; exit 1; \
	 fi; \
	 echo "runtime home negative control (Go): the file-order rule moves the runtime from $$base to $$added"
test-go: tables-go-runtime-home tables-go-runtime-home-negative-control

# The disjoint fill is held under Go's thread sanitizer. Its control makes
# every worker fill the whole array; byte identity alone cannot see that race.
.PHONY: tables-go-block-build tables-go-block-race-negative-control tables-go-block-fill-refuser tables-go-block-fill-refuser-negative-control
tables-go-block-build:
	sh test/slowgate/proof tables-go-block-build \
		'TestBlockBuilderStorageAndParallelFill' \
		./internal/codegen/gotable -run '^TestBlockBuilderStorageAndParallelFill$$' -count=1
tables-go-block-race-negative-control:
	sh test/slowgate/proof tables-go-block-race-negative-control \
		'TestBlockBuilderRaceNegativeControl' \
		./internal/codegen/gotable -run '^TestBlockBuilderRaceNegativeControl$$' -count=1
tables-go-block-fill-refuser:
	go test ./internal/codegen/gotable -run '^TestBlockFillRefuser$$' -count=1
tables-go-block-fill-refuser-negative-control:
	go test ./internal/codegen/gotable -run '^TestBlockFillRefuserNegativeControl$$' -count=1

test-go: tables-go-block-build tables-go-block-fill-refuser

.PHONY: tables-go-retain
tables-go-retain:
	sh test/slowgate/proof tables-go-retain \
		'TestRetainMatchesIndependentRewrite TestRetainReplacementAndShapeChanges TestRetainFileCapacityBeforeExpansion TestRetainUnknownNodeRecord' \
		./internal/codegen/gotable -run '^TestRetain' -count=1
test-go: tables-go-retain

.PHONY: tables-go-allocator tables-go-allocator-negative-controls tables-go-allocator-runtime-negative-control tables-go-retain-negative-controls
tables-go-allocator:
	@if [ "$${SCHEMA_GO_ALLOC_ANY_GO:-}" = 1 ]; then echo "Go allocation observation mode: NOT CERTIFIED"; fi
	GOTOOLCHAIN=go1.26.0 sh test/slowgate/proof tables-go-allocator \
		'TestAllocatorOwnershipAndStandaloneWriters' \
		./internal/codegen/gotable -run '^TestAllocatorOwnershipAndStandaloneWriters$$' -count=1

# THE SPAN, on its own (docs/SPEC-TABLES.md §2.5, M17). A blob larger than a
# slab takes a span of the arena's address space. The negative control makes
# grab() never span, so the blob is bump-allocated in a slab it does not fit
# and later allocations overwrite it.
.PHONY: tables-go-blob-span tables-go-blob-span-negative-control
tables-go-blob-span:
	sh test/slowgate/proof tables-go-blob-span \
		'TestBlobSpanHoldsAfterLaterAllocations' \
		./internal/codegen/gotable -run '^TestBlobSpanHoldsAfterLaterAllocations$$' -count=1
tables-go-blob-span-negative-control:
	sh test/slowgate/proof tables-go-blob-span-negative-control \
		'TestBlobSpanNegativeControl' \
		./internal/codegen/gotable -run '^TestBlobSpanNegativeControl$$' -count=1
test-go: tables-go-blob-span tables-go-blob-span-negative-control
tables-go-allocator-negative-controls:
	@set -e; for mode in original-slice pair frame; do sh test/conformance/go/ownership-negative-control $$mode; done
tables-go-allocator-runtime-negative-control:
	sh test/conformance/go/ownership-negative-control runtime
tables-go-retain-negative-controls:
	@set -e; for mode in file-count-floor unknown-node; do sh test/conformance/go/ownership-negative-control $$mode; done

test-go: tables-go-allocator tables-go-allocator-negative-controls tables-go-allocator-runtime-negative-control tables-go-retain-negative-controls

.PHONY: tables-go-builders tables-go-builders-negative-control tables-go-typed-refusals
tables-go-builders: build/conformance-harness build/conformance-go
	sh test/slowgate/proof tables-go-builders \
		'TestBuilderRefusalThroughUnion' \
		./internal/codegen/gotable -run '^TestBuilderRefusalThroughUnion$$' -count=1
	./build/conformance-harness wire-fuzz --builder --driver 'build/conformance-go wire-fuzz-builder' --seed $(SEED) --n $(N)
tables-go-builders-negative-control:
	sh test/conformance/go/ownership-negative-control builder-union
tables-go-typed-refusals:
	GOTOOLCHAIN=go1.26.0 sh test/slowgate/proof tables-go-typed-refusals \
		'TestAcceleratorTypedRefusals TestMeasureRefusalReasons' \
		./internal/codegen/gotable -run '^Test(AcceleratorTypedRefusals|MeasureRefusalReasons)$$' -count=1

test-go: tables-go-builders tables-go-builders-negative-control tables-go-typed-refusals

.PHONY: tables-go-view tables-go-view-negative-controls
tables-go-view:
	sh test/slowgate/proof tables-go-view-corpus \
		'TestGoUnitViewCorpus' \
		./compiler -run '^TestGoUnitViewCorpus$$' -count=1
	sh test/slowgate/proof tables-go-view-packet-storage \
		'TestUnitViewPacketStorage' \
		./internal/codegen/gotable -run '^TestUnitViewPacketStorage$$' -count=1
tables-go-view-negative-controls:
	@set -e; for mode in identity packet-offset arm-offset; do sh test/tables/view-go-control $$mode; done

test-go: tables-go-view tables-go-view-negative-controls

.PHONY: tables-go-measure-negative-controls tables-go-retain-wire-fuzz
tables-go-measure-negative-controls:
	@set -e; for mode in measure-cycle measure-count wire-damage counted-tail counted-work arm-framing; do sh test/conformance/go/ownership-negative-control $$mode; done
tables-go-retain-wire-fuzz: build/conformance-harness build/conformance-go
	./build/conformance-harness wire-fuzz --retain --driver 'build/conformance-go wire-fuzz' --seed $(SEED) --n $(N)
test-go: tables-go-measure-negative-controls tables-go-retain-wire-fuzz

# Release certification reuses the PR checks at a second seed, a longer soak,
# and a larger allocation sample. Local invocations may shorten
# TABLES_GO_RELEASE_SOAK_ITERATIONS.
#
# The default is HALF of TABLES_GO_SOAK_ITERATIONS (the hour's count), about
# 30 minutes, so the soak fits the certify.yml release-gates job's 60-minute
# timeout with its stated 15-minute pre-soak allowance (30m + 15m < 60m).
# internal/ci/gosoak_test.go holds that ratio. A longer soak would be killed
# at the cap, which is never raised to fit a growing suite (certify.yml note,
# 2026-09-18) — the full hour lives on in `make tables-go-soak`, for local and
# nightly runs.
TABLES_GO_RELEASE_SOAK_ITERATIONS ?= 300000
GO_RELEASE_SEED ?= 68719476731
.PHONY: tables-go-release tables-go-clean tables-go-bench-gate
tables-go-release: build/conformance-harness build/conformance-go tables-go-bench-gate tables-go-clean
	$(MAKE) tables-go-release-fuzz
	$(MAKE) tables-go-wire-fuzz-negative-control
	cd test/go-tables && GOTOOLCHAIN=go1.26.0 go test -run '^TestSoak$$' -count=1 -timeout 2h -soak-iterations $(TABLES_GO_RELEASE_SOAK_ITERATIONS)
	SCHEMA_GO_ALLOC_RUNS=200 GOTOOLCHAIN=go1.26.0 sh test/slowgate/proof tables-go-release-allocator \
		'TestAllocatorOwnershipAndStandaloneWriters' \
		./internal/codegen/gotable -run '^TestAllocatorOwnershipAndStandaloneWriters$$' -count=1
tables-go-clean: build/tables-generated-go/.stamp
	@$(MAKE) --no-print-directory tables-go-clean-units
# the gate's body, over GO_CLEAN_UNITS (every generated unit by default), so its
# negative control runs this same recipe over a unit a sabotaged emitter wrote
GO_CLEAN_UNITS ?= build/tables-generated-go/*
.PHONY: tables-go-clean-units
tables-go-clean-units:
	@set -e; for d in $(GO_CLEAN_UNITS); do \
		[ -f "$$d/go.mod" ] || continue; \
		bad=$$(gofmt -l "$$d"/*.go); [ -z "$$bad" ] || { echo "noncanonical generated Go: $$bad"; exit 1; }; \
		(cd "$$d" && go vet ./...); \
	done
tables-go-bench-gate: generated/bench/tables/go/.stamp
	bench/tables/go/leg build
	bench/tables/go/leg run --gate
test-go: tables-go-clean tables-go-bench-gate

# THE RELEASE TIER's fuzz half (docs/PORTING.md J3): the PR tier's wire, retain
# and builder fuzzers at the release count on the release seed, over
# GO_FUZZ_DRIVER. Its negative control hands it a driver whose generated
# readers accept a noncanonical LEB128 (test/conformance/go/negative-control
# leb's sabotage) and requires the release tier RED on the oracle's verdict.
GO_FUZZ_DRIVER ?= build/conformance-go
GO_RELEASE_N ?= 100000
.PHONY: tables-go-release-fuzz tables-go-release-negative-control
tables-go-release-fuzz: build/conformance-harness build/conformance-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s ./internal/codegen/gotable -run '^TestGoFileLoadOfDamagedFeedChainSavesWhatTheOracleSaves$$'
	./build/conformance-harness wire-fuzz --driver '$(GO_FUZZ_DRIVER) wire-fuzz' --seed $(GO_RELEASE_SEED) --n $(GO_RELEASE_N)
	./build/conformance-harness wire-fuzz --retain --driver '$(GO_FUZZ_DRIVER) wire-fuzz' --seed $(GO_RELEASE_SEED) --n $(GO_RELEASE_N)
	$(MAKE) tables-go-builders N=$(GO_RELEASE_N) SEED=$(GO_RELEASE_SEED)
tables-go-release-negative-control: build/conformance-harness build/conformance-go
	sh test/conformance/go/negative-control leb-driver
	@if $(MAKE) --no-print-directory tables-go-release-fuzz GO_FUZZ_DRIVER=build/go-table-control-leb-driver/driver-bin > build/go-table-control-leb-driver/release.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the release tier stayed green over a reader that accepts noncanonical LEB128"; exit 1; \
	fi
	@grep -q 'harness: FAILED after' build/go-table-control-leb-driver/release.log || \
		{ echo "NEGATIVE CONTROL FAILED: the release tier went red, but not on the fuzzer's verdict"; cat build/go-table-control-leb-driver/release.log; exit 1; }
	@echo "Go release negative control: a reader accepting noncanonical LEB128 turns the release tier red"

# THE CLEAN GATE's negative control (docs/PORTING.md J4): the emitter is
# sabotaged twice through `go build -overlay` and one unit regenerated from it
# each time, and the gate's own recipe must go red on each half -- gofmt, when
# the emitter skips go/format; go vet, when a printf call loses an argument.
.PHONY: tables-go-clean-negative-control
tables-go-clean-negative-control: build/tables-generated-go/.stamp
	sh test/go-tables/negative-control clean-format
	sh test/go-tables/negative-control clean-vet

test-go: tables-go-release-negative-control tables-go-clean-negative-control

# ---------------------------------------------------------------------------
# THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4) — THE GO LEG
# ---------------------------------------------------------------------------
#
# THE BYTES ARE THE C++ REFERENCE'S, and that is the whole point of this gate.
# bench/paired/corpus/bench_fixed.bin and .vocab are what the reference's own
# form-3 writer put down over the paired bench's sixty-four logical records
# (test/bench/paired_main.cpp, `bench-paired-check`), committed. This target
# runs the GO leg over them in the runner's no-clock `--gate` mode, which:
#
#   1. compares this build's LAYOUT against the corpus's, byte for byte and
#      before anything else — the record's positions, ids, kinds, size and the
#      hash every record carries are all in those bytes, so a leg that matches
#      them is speaking the form and not a near miss;
#   2. checks Measure against the file's own length;
#   3. loads all sixty-four records out of the reference's file;
#   4. writes them back and compares the WHOLE FILE, byte for byte, twice more
#      into reused storage.
#
# It is the twin of `tables-fixed-matched` for the two legs that already carry
# the form, and it is a correctness gate: it starts no clock, so it belongs in
# `make test-go` where `go run ./bench/paired` does not. The VERSIONING half is
# NOT held here and no longer held by TestFixedFormPlanPath, which §5.6 of
# docs/FIXED-FORM-ALGORITHM.md retired by name (it skips itself: under the new
# law the FX1/FX2 files come back `layout_newer`). It is held by the LINEAGE
# harness in internal/codegen/gotable/fixedversioning_test.go, and the target
# that runs it is `tables-go-versioning`, immediately below.
build/schema_tables_bench_go_matched: generated/bench/paired/go/.stamp bench/tables/go/table_main.go bench/tables/go/shape_matched.go bench/paired/go/go.mod
	@mkdir -p build
	cd bench/paired/go && go build -tags matched -o $(CURDIR)/$@ ../../tables/go/table_main.go ../../tables/go/shape_matched.go

.PHONY: tables-go-fixed-form
tables-go-fixed-form: build/schema_tables_bench_go_matched build/schema_test_bench_paired
	./build/schema_test_bench_paired verify
	./build/schema_tables_bench_go_matched --gate --indexed --wire-dir bench/paired/corpus --variant-dir bench/paired/corpus
	@echo 'tables Go fixed form: the layout and the whole 80915-byte file match the C++ reference, byte for byte'

test-go: tables-go-fixed-form

test-go: tables-go-versioning

# THE VERSIONING HALF OF THE FIXED FORM ON THE GO LEG (§5 of
# docs/FIXED-FORM-ALGORITHM.md, the rows of docs/FIXED-FORM-VERSIONING-TESTS.md).
# internal/codegen/gotable/fixedversioning_test.go reads the C++ reference's
# byte oracle out of build/fixedform-corpus — `old_<row>.bin`, `new_<row>.bin`
# and the floor, hash and lineage-merge files — and holds each row's two read
# columns: NEW-READS-OLD lands every old value with §5.4's counters, and
# OLD-REFUSES-NEW answers `layout_newer` before any record, on the file's hash
# alone (§5.3).
#
# THIS TARGET EXISTS BECAUSE `go test ./...` ASSERTS NOTHING HERE. The suite's
# harness SKIPS itself when build/fixedform-corpus is absent, which is right for
# a bare `go test ./...` on a tree that never built the oracle — and is exactly
# how a §5 regression would have ridden into CI green, since the go-test job
# runs nothing but `go test ./...`. So the target BUILDS THE ORACLE FIRST
# (`tables-fixedform-corpus`, the C++ reference's own dump, about 7 s) and then
# sets SCHEMA_REQUIRE_CORPUS=1, which turns that skip into a FAILURE: under this
# name a missing corpus can never pass silently.
.PHONY: tables-go-versioning
tables-go-versioning: tables-fixedform-corpus $(SERIALIZE_GO_RUNTIME)/go.mod
	SCHEMA_REQUIRE_CORPUS=1 go test ./internal/codegen/gotable/ -count=1 -run 'TestFixedVersioning|TestHash|TestFloor|TestLineageMerge|TestFixedRoadmapGoT02Plans|TestFixedRoadmapGoT12ConformanceHostile'
	@echo 'tables Go versioning: §5 read both columns of every row against the C++ reference bytes'

# ITS NEGATIVE CONTROL: move one byte of the WRITE TEMPLATE and the leg must go
# red against the reference's corpus. Without this the byte comparison could be
# comparing a file with itself and nobody would know. The sabotage adds one to
# every text length the form-3 writer lays down — through `go build -overlay`,
# so no tracked file moves — and the gate must fail with the round-trip's own
# words and not for some other reason.
.PHONY: tables-go-fixed-form-negative-control
tables-go-fixed-form-negative-control: bin/schema build/schema_test_bench_paired
	@rm -rf build/go-fixed-sabotage && mkdir -p build/go-fixed-sabotage
	@sed 's|uint32(%s.%sLength))|uint32(%s.%sLength+1)) // SABOTAGED|' \
		internal/codegen/gotable/fixedform.go > build/go-fixed-sabotage/fixedform.gotext
	@grep -q SABOTAGED build/go-fixed-sabotage/fixedform.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/gotable/fixedform.go":"%s/build/go-fixed-sabotage/fixedform.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/go-fixed-sabotage/overlay.json
	@go build -overlay=build/go-fixed-sabotage/overlay.json -o build/go-fixed-sabotage/schema ./cmd/schema
	@./build/go-fixed-sabotage/schema generate --lang go --runtime $(GO_BENCH_RUNTIME) --out build/go-fixed-sabotage/gen bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	@printf 'module benchtable\n\ngo 1.24\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => $(CURDIR)/$(SERIALIZE_GO)\n' > build/go-fixed-sabotage/gen/go.mod
	@cp bench/paired/go/go.mod build/go-fixed-sabotage/table.mod
	@cd bench/paired/go && go mod edit -modfile $(CURDIR)/build/go-fixed-sabotage/table.mod \
		-replace benchtable=$(CURDIR)/build/go-fixed-sabotage/gen \
		-replace github.com/mas-bandwidth/serialize.go/v2=$(CURDIR)/$(SERIALIZE_GO)
	@cd bench/paired/go && go build -modfile $(CURDIR)/build/go-fixed-sabotage/table.mod -tags matched \
		-o $(CURDIR)/build/go-fixed-sabotage/leg ../../tables/go/table_main.go ../../tables/go/shape_matched.go
	@if ./build/go-fixed-sabotage/leg --gate --indexed --wire-dir bench/paired/corpus \
			--variant-dir bench/paired/corpus > build/go-fixed-sabotage/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a text length one byte off left the fixed form green"; \
		cat build/go-fixed-sabotage/log; exit 1; \
	fi
	@grep -Fq 'round-trip bytes differ' build/go-fixed-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the fixed form went red for another reason"; cat build/go-fixed-sabotage/log; exit 1; }
	@echo 'tables Go fixed form negative control: one byte off the write template reds the reference byte match'

.PHONY: tables-go-usage
tables-go-usage: build/tables-generated-go/.stamp
	cd test/go-tables && go test -run '^TestUsage$$' -count=1
test-go: tables-go-usage

# ITS NEGATIVE CONTROL (docs/PORTING.md I12): the unit the page's example
# imports spells its strict read differently, through an overlay the gate's own
# go vet and go run inherit, and the gate must go red naming the page.
.PHONY: tables-go-usage-negative-control
tables-go-usage-negative-control: build/tables-generated-go/.stamp
	sh test/go-tables/negative-control usage
test-go: tables-go-usage-negative-control

# THE ENUM-KEYED ARRAY's guard (docs/PORTING.md M5): TestKeyedRefusesNone,
# TestKeyedRefusesPastMax and TestKeyedPlacesByKey over the generated
# TableKeyed. Its negative control removes the None compare, the past-E.Max
# compare and the k-1 shift from the generated unit in turn, each through an
# overlay, and each must turn exactly its own test red.
.PHONY: tables-go-keyed tables-go-keyed-negative-control
tables-go-keyed: build/tables-generated-go/.stamp
	cd test/go-tables && go test -run '^TestKeyed' -count=1
tables-go-keyed-negative-control: build/tables-generated-go/.stamp
	sh test/go-tables/negative-control keyed-none
	sh test/go-tables/negative-control keyed-max
	sh test/go-tables/negative-control keyed-shift
test-go: tables-go-keyed tables-go-keyed-negative-control

# THE LAYOUT CONTRACT IN GENERATED CODE (docs/PORTING.md M11): the block
# init refuses a runtime whose struct disagrees with the compiler's model.
# Its negative control makes the emitted init's two refusing helpers stop
# panicking, and the gate must go red on the disagreeing runtime it now loads.
.PHONY: tables-go-layout-contract tables-go-layout-contract-negative-control
tables-go-layout-contract:
	SCHEMA_SLOW=1 go test -count=1 ./internal/codegen/gotable -run '^TestBlockLayoutContractRefusesADisagreeingRuntime$$'
tables-go-layout-contract-negative-control:
	sh test/go-tables/negative-control layout
test-go: tables-go-layout-contract tables-go-layout-contract-negative-control

# THE `&node` LABEL (docs/PORTING.md M14): TestRegionGraphs writes a graph
# whose node two pointers share as text and requires the read to share it
# again. Its negative control makes the emitter's text writer write a shared
# node as plain copies, with no label, and the gate must go red on the sharing.
.PHONY: tables-go-graph-label-negative-control
tables-go-graph-label-negative-control:
	sh test/go-tables/negative-control graph-label
test-go: tables-go-graph-label-negative-control

# PER-CASE ABSENT (docs/PORTING.md I4): the Go leg alone, under a substituted
# registry, answers the cases it cannot carry ABSENT one by one and passes,
# and the matrix shows it did (`+Na`). Its negative control makes the driver
# write nothing for such a case, and the harness must go red on the missing
# answer.
.PHONY: conformance-go-absent conformance-negative-control-go-absent
conformance-go-absent: build/conformance-harness build/conformance-go
	@mkdir -p build/conformance-go-absent
	@printf 'go test/conformance/go/driver\n' > build/conformance-go-absent/drivers.txt
	./build/conformance-harness run --drivers build/conformance-go-absent/drivers.txt --work build/conformance-go-absent/work | tee build/conformance-go-absent/log
	@for s in wire framing; do grep -Eq "^$$s +pass [0-9]+/[0-9]+ \+[1-9][0-9]*a" build/conformance-go-absent/log || \
		{ echo "PER-CASE ABSENT GATE FAILED: the Go leg's $$s surface answered no case absent, so the grain is not exercised"; exit 1; }; done
conformance-negative-control-go-absent: build/conformance-harness build/conformance-go
	sh test/conformance/go/negative-control absent
test-go: conformance-go-absent conformance-negative-control-go-absent

# THE CARD CHECK, its Go leg (docs/PORTING.md J6): the Go card renderer
# writes a header line's separator one space wider, through an overlay of the
# copy the driver builds from, and the check must go red on render and stay
# green on load.
.PHONY: cards-conformance-go-negative-control
cards-conformance-go-negative-control: build/conformance-harness cards-driver-go
	CARDS_REQUIRE=$(CARDS_REQUIRE) sh test/go-tables/negative-control cards
test-go: cards-conformance-go-negative-control

# The Go table output compiles with any subset of its optional forms {json,
# cook, block, message} omitted (docs/SPEC-TABLES.md, "GO's optional forms"): every corpus unit,
# all sixteen subsets, through the emitter's own omit path (GenerateOmitting),
# built and vetted against the sibling serialize.go. Part of the default gate:
# it needs no SCHEMA_SLOW.
.PHONY: tables-go-subsets
tables-go-subsets:
	sh test/slowgate/proof tables-go-subsets \
		'TestGoTableCompilesWithAnySubsetOfItsForms' \
		./internal/codegen/gotable -run '^TestGoTableCompilesWithAnySubsetOfItsForms$$' -count=1 -timeout 600s
test-go: tables-go-subsets

# I13 (docs/PORTING.md, the text differential): the go leg's text differential against a third
# implementation. The test generates N random float-only instances with the
# generated setters, writes each as (wire, text), then byte-compares the leg's
# text against internal/tabletext's text and packs the leg's text back to wire
# the other way. The probe builds its own unit into a temp directory, so
# build/tables-generated-go/.stamp is not a prerequisite and the control is not
# slowed by a corpus build. test-go is wired only because the row is GREEN at
# base.
.PHONY: tables-go-text-differential tables-go-text-differential-negative-control
tables-go-text-differential:
	SCHEMA_SLOW=1 go test ./internal/codegen/gotable -run '^TestTextDifferential$$' -count=1
tables-go-text-differential-negative-control:
	SCHEMA_SLOW=1 go test ./internal/codegen/gotable -run '^TestTextDifferentialNegativeControl$$' -count=1
test-go: tables-go-text-differential tables-go-text-differential-negative-control

# FINITE TABLE FLOATS (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27: "Tables go
# finite-only too, same rule as packets"): every read refuses a NaN or an
# infinity on its raw bits as non_finite_float and every writer refuses one.
# The gate is TestGoPlainFloatSpecialValuesAgreeAcrossForms; its reversed
# witness sabotages the read's bit test, the fixed form's finite pass and the
# writer's clause in turn, and each must turn the probe red on its own half.
.PHONY: tables-go-finite-float tables-go-finite-float-negative-control
tables-go-finite-float:
	SCHEMA_SLOW=1 go test ./internal/codegen/gotable -run '^TestGoPlainFloatSpecialValuesAgreeAcrossForms$$' -count=1
tables-go-finite-float-negative-control:
	SCHEMA_SLOW=1 go test ./internal/codegen/gotable -run '^TestGoFiniteFloatNegativeControl$$' -count=1
test-go: tables-go-finite-float

# The random-schema table differential (card
# schema-go-fuzz-random-table-differential): seeds 0..SCHEMA_TABLE_DIFF_TO of
# generated VALID units through compiler.New().Generate, the generated Go leg
# under -race, every read/write path, every damage, hostile JSON plants and the
# compiler's engine in both directions. Nightly in certify.yml, never in
# ci-fast or ci-full: SCHEMA_TABLE_DIFF=1, set only here, is what opts the two
# tests in; ci-full's SCHEMA_SLOW=1 run of ./internal/fuzz skips them.
.PHONY: fuzz-tables-diff
fuzz-tables-diff:
	SCHEMA_SLOW=1 SCHEMA_TABLE_DIFF=1 SCHEMA_TABLE_DIFF_TO=200 go test -count=1 -timeout 600s ./internal/fuzz/ -run '^TestRandomTableDifferential$$' -v
	SCHEMA_SLOW=1 SCHEMA_TABLE_DIFF=1 go test -count=1 -timeout 600s ./internal/fuzz/ -run '^TestRandomTableDifferentialNegativeControl$$' -v

# The property-based random-schema tests (card schema-go-rapid-property-tests):
# rapid drives the SAME generator as the differential above, so a failure
# shrinks to a small schema and value. Each check compiles a generated unit, so
# the check count is bounded here; nightly in certify.yml, never in ci-fast or
# ci-full (SCHEMA_RAPID_PROPS=1 opts them in, and a CI process without it skips
# them).
.PHONY: fuzz-tables-props
fuzz-tables-props:
	SCHEMA_SLOW=1 SCHEMA_RAPID_PROPS=1 go test -count=1 -timeout 600s ./internal/fuzz/ -run '^TestProp' -rapid.checks=24 -v
	SCHEMA_SLOW=1 SCHEMA_RAPID_PROPS=1 go test -count=1 -timeout 600s ./internal/fuzz/ -run '^TestRapidPropsNegativeControl$$' -rapid.checks=4 -v

.PHONY: test-go
test-go: deps

# The same reversed witness on the MESSAGE surface (docs/SPEC-TABLES.md §3.3):
# one byte of the login_full batch answer flipped in a copy of the driver, and
# the matrix must go red on that message row while the wire surface stays green.
.PHONY: conformance-negative-control-go-message
conformance-negative-control-go-message: build/conformance-harness build/conformance-go
	sh test/conformance/go/negative-control message

# The generated message form's own controls (internal/codegen/gotable,
# messageform_negative_test.go): each plants one fault in a copy of the
# generated source and requires its gate's probe red on its own message.
.PHONY: tables-go-message-form-negative-controls
tables-go-message-form-negative-controls:
	SCHEMA_SLOW=1 go test ./internal/codegen/gotable -count=1 -timeout 600s \
		-run '^(TestGoMessageFormGoldensNegativeControl|TestGoMessageFormRefusalsNegativeControl|TestGoMessageFormStrictNegativeControl|TestGoMessageFormVersioningNegativeControl|TestGoMessageFormFixedOnlyNegativeControl|TestGoWireIntegrityMessageFormNegativeControl)$$'

test-go: conformance-negative-control-go-message tables-go-message-form-negative-controls
