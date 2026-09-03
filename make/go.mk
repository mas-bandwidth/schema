# make/go.mk — the Go leg (docs/CONTRIBUTING.md, "Adding a language"). Included
# by the Makefile's wildcard include; the Makefile names no language. The leg
# registers itself at the end of this file.

# the serialize.go runtime the generated Go targets, a sibling checkout;
# test/go/go.mod and its ludicrous twin carry the same relative path
SERIALIZE_GO ?= ../serialize.go

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
build/tables-generated-go/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P3.schema
	@mkdir -p build/tables-generated-go
	./bin/schema generate --lang go --out build/tables-generated-go/examples tables/examples
	# the POINTERED unit: its Go WIRE surface is refused by name (§11) and its
	# two ACCELERATORS are emitted all the same, because neither needs a codec
	# (§7, §19). This is where the cook's Go read side comes from.
	./bin/schema generate --lang go --out build/tables-generated-go/pointers tables/pointers
	./bin/schema generate --lang go --out build/tables-generated-go/block tables/block
	./bin/schema generate --lang go --out build/tables-generated-go/blockhome tables/blockhome
	./bin/schema generate --lang go --out build/tables-generated-go/v1 test/tables/V1.schema
	./bin/schema generate --lang go --out build/tables-generated-go/v2 test/tables/V2.schema
	./bin/schema generate --lang go --out build/tables-generated-go/p1 test/tables/P1.schema
	./bin/schema generate --lang go --out build/tables-generated-go/p3 test/tables/P3.schema
	$(call go_table_module,examples,tabledemo)
	$(call go_table_module,pointers,graphdemo)
	$(call go_table_module,block,blockdemo)
	$(call go_table_module,blockhome,blockhomedemo)
	$(call go_table_module,v1,tblv1)
	$(call go_table_module,v2,tblv2)
	$(call go_table_module,p1,tblp1)
	$(call go_table_module,p3,tblp3)
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
	@sed -e 's|=> ../../build/tables-generated-go/block|=> $(CURDIR)/build/go-fuzz-$(1)/generated/block|' \
	     -e 's|=> ../../build/tables-generated-go/pointers|=> $(CURDIR)/build/go-fuzz-$(1)/generated/pointers|' \
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
conformance-negative-control-go: build/conformance-harness build/tables-generated-go/.stamp
	@rm -rf build/conformance-negative-go && mkdir -p build/conformance-negative-go
	@cp test/conformance/go/*.go build/conformance-negative-go/
	@printf 'module schemaconformance\n\ngo 1.23\n' > build/conformance-negative-go/go.mod
	@for m in tabledemo:examples tblv1:v1 tblv2:v2 tblp1:p1 tblp3:p3 blockdemo:block graphdemo:pointers; do \
		printf 'require %s v0.0.0\nreplace %s => $(CURDIR)/build/tables-generated-go/%s\n' \
			"$${m%%:*}" "$${m%%:*}" "$${m##*:}" >> build/conformance-negative-go/go.mod; \
	done
	@printf 'require github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => $(CURDIR)/$(SERIALIZE_GO)\n' >> build/conformance-negative-go/go.mod
	@sed -i.bak 's|if err := spill(out, f\[1\], scratch); err != nil {|scratch[0] ^= 1 // SABOTAGED\n\t\tif err := spill(out, f[1], scratch); err != nil {|' build/conformance-negative-go/main.go
	@grep -q SABOTAGED build/conformance-negative-go/main.go || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	cd build/conformance-negative-go && go build -o ../conformance-negative-go-bin .
	@printf '#!/bin/sh\nexec build/conformance-negative-go-bin "$$@"\n' > build/conformance-negative-go/driver
	@chmod +x build/conformance-negative-go/driver
	@printf 'go build/conformance-negative-go/driver\n' > build/conformance-negative-go/drivers.txt
	@if ./build/conformance-harness run --drivers build/conformance-negative-go/drivers.txt \
			--work build/conformance-negative-go/work > build/conformance-negative-go/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: one byte off in a wire answer left the harness green"; \
		cat build/conformance-negative-go/log; exit 1; \
	fi
	@grep -q "go / wire" build/conformance-negative-go/log || \
		{ echo "NEGATIVE CONTROL FAILED: the harness went red, but not on the sabotaged surface"; \
		  cat build/conformance-negative-go/log; exit 1; }
	@grep -q "report        pass" build/conformance-negative-go/log || \
		{ echo "NEGATIVE CONTROL FAILED: the whole matrix went red, so it localises nothing"; \
		  cat build/conformance-negative-go/log; exit 1; }
	@grep -m1 "go / wire" build/conformance-negative-go/log
	@echo "negative control (go): one byte off in one wire answer turns the harness RED on that surface alone"

CONFORMANCE_NEGATIVE_GOWALK := build/conformance-negative-gowalk
CONFORMANCE_NEGATIVE_GOWALK_SED := /^func tableJsonReadField/,/^}/ s|storage := unsafe.Add(base, uintptr(f.Offset))|storage := unsafe.Add(base, uintptr(f.Offset^4)) // SABOTAGED|
.PHONY: conformance-negative-control-go-walk
conformance-negative-control-go-walk: build/conformance-harness
	@rm -rf $(CONFORMANCE_NEGATIVE_GOWALK) && mkdir -p $(CONFORMANCE_NEGATIVE_GOWALK)
	@sed '$(CONFORMANCE_NEGATIVE_GOWALK_SED)' internal/codegen/gotable/json.go > $(CONFORMANCE_NEGATIVE_GOWALK)/gotable-json.go.txt
	@cmp -s internal/codegen/gotable/json.go $(CONFORMANCE_NEGATIVE_GOWALK)/gotable-json.go.txt && \
		{ echo "NEGATIVE CONTROL: the Go emitter sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/gotable/json.go":"%s/$(CONFORMANCE_NEGATIVE_GOWALK)/gotable-json.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(CONFORMANCE_NEGATIVE_GOWALK)/overlay.json
	go build -overlay $(CONFORMANCE_NEGATIVE_GOWALK)/overlay.json -o $(CONFORMANCE_NEGATIVE_GOWALK)/schema ./cmd/schema
	@rm -rf $(CONFORMANCE_NEGATIVE_GOWALK)/generated
	$(CONFORMANCE_NEGATIVE_GOWALK)/schema generate --lang go --out $(CONFORMANCE_NEGATIVE_GOWALK)/generated/examples tables/examples
	$(CONFORMANCE_NEGATIVE_GOWALK)/schema generate --lang go --out $(CONFORMANCE_NEGATIVE_GOWALK)/generated/pointers tables/pointers
	$(CONFORMANCE_NEGATIVE_GOWALK)/schema generate --lang go --out $(CONFORMANCE_NEGATIVE_GOWALK)/generated/block tables/block
	$(CONFORMANCE_NEGATIVE_GOWALK)/schema generate --lang go --out $(CONFORMANCE_NEGATIVE_GOWALK)/generated/v1 test/tables/V1.schema
	$(CONFORMANCE_NEGATIVE_GOWALK)/schema generate --lang go --out $(CONFORMANCE_NEGATIVE_GOWALK)/generated/v2 test/tables/V2.schema
	$(CONFORMANCE_NEGATIVE_GOWALK)/schema generate --lang go --out $(CONFORMANCE_NEGATIVE_GOWALK)/generated/p1 test/tables/P1.schema
	$(CONFORMANCE_NEGATIVE_GOWALK)/schema generate --lang go --out $(CONFORMANCE_NEGATIVE_GOWALK)/generated/p3 test/tables/P3.schema
	@grep -lq SABOTAGED $(CONFORMANCE_NEGATIVE_GOWALK)/generated/*/*TableJson.go || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter emitted an unsabotaged walk"; exit 1; }
	@cp test/conformance/go/*.go $(CONFORMANCE_NEGATIVE_GOWALK)/
	@printf 'module schemaconformance\n\ngo 1.23\n' > $(CONFORMANCE_NEGATIVE_GOWALK)/go.mod
	@for m in tabledemo:examples tblv1:v1 tblv2:v2 tblp1:p1 tblp3:p3 blockdemo:block graphdemo:pointers; do \
		mod=$${m%%:*}; dir=$${m##*:}; \
		printf 'module %s\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => $(CURDIR)/$(SERIALIZE_GO)\n' \
			"$$mod" > $(CONFORMANCE_NEGATIVE_GOWALK)/generated/$$dir/go.mod; \
		printf 'require %s v0.0.0\nreplace %s => $(CURDIR)/$(CONFORMANCE_NEGATIVE_GOWALK)/generated/%s\n' \
			"$$mod" "$$mod" "$$dir" >> $(CONFORMANCE_NEGATIVE_GOWALK)/go.mod; \
	done
	@printf 'require github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => $(CURDIR)/$(SERIALIZE_GO)\n' >> $(CONFORMANCE_NEGATIVE_GOWALK)/go.mod
	cd $(CONFORMANCE_NEGATIVE_GOWALK) && go build -o driver-bin .
	@printf '#!/bin/sh\nexec %s/driver-bin "$$@"\n' "$(CURDIR)/$(CONFORMANCE_NEGATIVE_GOWALK)" > $(CONFORMANCE_NEGATIVE_GOWALK)/driver
	@chmod +x $(CONFORMANCE_NEGATIVE_GOWALK)/driver
	@printf 'go %s/driver\n' "$(CURDIR)/$(CONFORMANCE_NEGATIVE_GOWALK)" > $(CONFORMANCE_NEGATIVE_GOWALK)/drivers.txt
	@if ./build/conformance-harness run --drivers $(CONFORMANCE_NEGATIVE_GOWALK)/drivers.txt \
			--work $(CONFORMANCE_NEGATIVE_GOWALK)/work > $(CONFORMANCE_NEGATIVE_GOWALK)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a sabotaged Go walker left the harness green"; \
		cat $(CONFORMANCE_NEGATIVE_GOWALK)/log; exit 1; \
	fi
	@grep -q "go / json-read" $(CONFORMANCE_NEGATIVE_GOWALK)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the harness went red, but not on the sabotaged surface"; \
		  cat $(CONFORMANCE_NEGATIVE_GOWALK)/log; exit 1; }
	@grep -q "json-write    pass" $(CONFORMANCE_NEGATIVE_GOWALK)/log || \
		{ echo "NEGATIVE CONTROL FAILED: json-write went red too, so the control does not localise the READER"; \
		  cat $(CONFORMANCE_NEGATIVE_GOWALK)/log; exit 1; }
	@grep -q "wire          pass" $(CONFORMANCE_NEGATIVE_GOWALK)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the whole matrix went red, so it localises nothing"; \
		  cat $(CONFORMANCE_NEGATIVE_GOWALK)/log; exit 1; }
	@grep -m1 "go / json-read" $(CONFORMANCE_NEGATIVE_GOWALK)/log
	@echo "negative control: one field offset off in the Go walk turns the harness RED on json-read alone"

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
