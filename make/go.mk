# make/go.mk — the Go leg (docs/CONTRIBUTING.md, "Adding a language"). Included
# by the Makefile's wildcard include; the Makefile names no language. The leg
# registers itself at the end of this file.

# the serialize.go runtime the generated Go targets, a sibling checkout;
# test/go/go.mod and its ludicrous twin carry the same relative path
SERIALIZE_GO ?= ../serialize.go

build/packet-text/go/.stamp: bin/schema test/packet-text/Narrow.schema make/go.mk
	./bin/schema generate --lang go --out build/packet-text/go test/packet-text/Narrow.schema
	@printf 'module packettext\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => "%s/$(SERIALIZE_GO)"\n' "$(CURDIR)" > build/packet-text/go/go.mod
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
	@printf 'module ludicrous\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => ../../$(SERIALIZE_GO)\n' > generated/go-ludicrous/go.mod
	@touch $@

# the Go target: generated package + module wiring (the go.mod is build
# wiring, not schema output — the emitter writes only .go files)
generated/go/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang go --out generated/go examples
	@printf 'module example\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => ../../$(SERIALIZE_GO)\n' > generated/go/go.mod
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
build/tables-generated-go/.stamp: bin/schema make/go.mk test/tables/RT1.schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P3.schema $(wildcard test/tables/[MAKR][12].schema) tables/scalars/Scalars.schema test/tables/Scalars2.schema $(wildcard examples-wide/*.schema) tables/messages/Messages.schema tables/backend/Backend.schema tables/vocab/Vocab.schema tables/vocab9/Vocab9.schema $(wildcard tables/stream/*.schema tables/blobs/*.schema tables/lists/*.schema tables/maps/*.schema)
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

# one generated unit's module wiring (build wiring, not schema output — the
# emitter writes only .go files)
define go_table_module
@printf 'module %s\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => ../../../$(SERIALIZE_GO)\n' $(2) > build/tables-generated-go/$(1)/go.mod
endef

.PHONY: tables-go-soak
tables-go-soak: build/tables-generated-go/.stamp
	cd test/go-tables && go test -run Soak -timeout 0 -soak $(SOAK) -v .

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

generated/bench/tables/go/.stamp: bin/schema bench/corpus/BenchTable.schema
	@mkdir -p generated/bench/tables/go
	./bin/schema generate --lang go --out generated/bench/tables/go bench/corpus/BenchTable.schema
	@printf 'module benchtable\n\ngo 1.24\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => ../../../../$(SERIALIZE_GO)\n' > generated/bench/tables/go/go.mod

generated/bench/go/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang go --out generated/bench/go bench/corpus/Bench.schema
	./bin/schema generate --lang go --out generated/bench/go/realworld bench/corpus/RealWorld.schema
	@printf 'module bench\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => ../../../$(SERIALIZE_GO)\n' > generated/bench/go/go.mod
	@touch $@

build/conformance-go: build/tables-generated-go/.stamp $(wildcard test/conformance/go/*.go) test/conformance/go/go.mod
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
build/conformance-go-be: build/tables-generated-go/.stamp $(wildcard test/conformance/go/*.go) test/conformance/go/go.mod
	@mkdir -p build
	cd test/conformance/go && GOOS=linux GOARCH=s390x go build -o ../../../$@ .

.PHONY: conformance-big-endian
conformance-big-endian: build/conformance-harness build/conformance-go-be
	@printf 'go-be test/conformance/go/driver-be\n' > build/conformance-be-drivers.txt
	./build/conformance-harness run --drivers build/conformance-be-drivers.txt --work build/conformance-be-work
	@echo "big-endian leg: the Go table wire, the read report and the text form cross the byte order"

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

# THE GO LEG of `make test`: the two conformance negative controls, THE GO
# PORT's own instruments (docs/SPEC-TABLES.md) — the allocation gate and its
# negative control, the forgery fuzzer plain and under -race, and two seconds
# of the soak; the hour is `make tables-go-soak` — the bench units' compile
# gates, and the packet tests.
.PHONY: test-go
test-go: generated/bench/tables/go/.stamp generated/go/.stamp generated/go-ludicrous/.stamp generated/bench/go/.stamp
	$(MAKE) conformance-negative-control-go
	$(MAKE) conformance-negative-control-go-walk
	$(MAKE) tables-go-json-walk
	$(MAKE) tables-go-fuzz
	cd test/go-tables && go test -count 1 .
	$(MAKE) tables-go-fuzz-extent-negative-control
	$(MAKE) tables-go-fuzz-maximum-negative-control
	bench/tables/go/leg build
	cd generated/bench/go && go build ./...
	cd test/go && go run .
	cd test/go-ludicrous && go run .

TEST_LEGS         += test-go
CONFORMANCE_LEGS  += build/conformance-go
BENCH_TABLES_LEGS += generated/bench/tables/go/.stamp


build/packet-wide/go/.stamp: bin/schema build/packet-wide/source/WideText.schema test/packet-wide/Shapes.schema make/go.mk
	./bin/schema generate --lang go --out build/packet-wide/go build/packet-wide/source/WideText.schema
	./bin/schema generate --lang go --out build/packet-wide/go/shapes test/packet-wide/Shapes.schema
	@printf 'module packetwide\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => "%s/$(SERIALIZE_GO)"\n' "$(CURDIR)" > build/packet-wide/go/go.mod
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
	./build/conformance-harness wire-fuzz --driver 'build/conformance-go wire-fuzz' --seed $(SEED) --n $(N)

tables-go-wire-fuzz-negative-control: build/conformance-harness build/conformance-go
	sh test/conformance/go/negative-control leb

test-go: tables-go-wire-fuzz tables-go-wire-fuzz-negative-control

.PHONY: tables-go-containers tables-go-containers-negative-controls
tables-go-containers:
	go test ./internal/codegen/gotable -run 'Test(RegionLists|ListsThroughUnionArrays|BuilderCountRecovery|NestedTerminationAndStringDefault|RegionMaps|MapReadReports|MapJsonKeyDomains)'
tables-go-containers-negative-controls:
	@set -e; for mode in sort ascending duplicate key-domain dead cap; do sh test/conformance/go/container-negative-control $$mode; done

test-go: tables-go-containers

# The disjoint fill is held under Go's thread sanitizer. Its control makes
# every worker fill the whole array; byte identity alone cannot see that race.
.PHONY: tables-go-block-build tables-go-block-race-negative-control tables-go-block-fill-refuser tables-go-block-fill-refuser-negative-control
tables-go-block-build:
	go test ./internal/codegen/gotable -run '^TestBlockBuilderStorageAndParallelFill$$' -count=1
tables-go-block-race-negative-control:
	go test ./internal/codegen/gotable -run '^TestBlockBuilderRaceNegativeControl$$' -count=1
tables-go-block-fill-refuser:
	go test ./internal/codegen/gotable -run '^TestBlockFillRefuser$$' -count=1
tables-go-block-fill-refuser-negative-control:
	go test ./internal/codegen/gotable -run '^TestBlockFillRefuserNegativeControl$$' -count=1

test-go: tables-go-block-build tables-go-block-fill-refuser

.PHONY: tables-go-retain
tables-go-retain:
	go test ./internal/codegen/gotable -run '^TestRetain' -count=1
test-go: tables-go-retain

.PHONY: tables-go-allocator tables-go-allocator-negative-controls tables-go-allocator-runtime-negative-control tables-go-retain-negative-controls
tables-go-allocator:
	SCHEMA_GO_ALLOC_CERTIFY=1 GOTOOLCHAIN=go1.26.0 go test ./internal/codegen/gotable -run '^TestAllocatorOwnershipAndStandaloneWriters$$' -count=1
tables-go-allocator-negative-controls:
	@set -e; for mode in original-slice pair frame; do sh test/conformance/go/ownership-negative-control $$mode; done
tables-go-allocator-runtime-negative-control:
	sh test/conformance/go/ownership-negative-control runtime
tables-go-retain-negative-controls:
	@set -e; for mode in file-count-floor message-depth; do sh test/conformance/go/ownership-negative-control $$mode; done

test-go: tables-go-allocator
