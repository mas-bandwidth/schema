# make/unity-consumers.mk — Unity C# 9 consumer facades, bench gate, conformance
# controls, and release tier (docs/UNITY.md; docs/PORTING.md).

CARDS_UNITY_GENERATED := build/tables-generated-unity/cards
CARDS_UNITY_STAMP     := $(CARDS_UNITY_GENERATED)/.stamp
CARDS_UNITY_SURFACES  := load,save,render,parse,bounds,upgrade,edit,taskhash,view

.PHONY: cards-unity-generate cards-unity-test cards-unity-driver cards-unity-conformance
.PHONY: cards-unity-conformance-negative-control cards-unity-conformance-upgrade-negative-control cards-unity-conformance-hash-negative-control
.PHONY: unity-ci-cards tables-unity-bench-gate tables-unity-bench-gate-negative-control
.PHONY: tables-unity-standalone conformance-unity conformance-negative-control-unity
.PHONY: tables-unity-wire-fuzz tables-unity-wire-fuzz-negative-control tables-unity-release

cards-unity-generate: $(CARDS_UNITY_STAMP)
$(CARDS_UNITY_STAMP): bin/schema tables/cards/Card.schema tables/cards/schema.lock
	@rm -rf $(CARDS_UNITY_GENERATED) && mkdir -p $(CARDS_UNITY_GENERATED)
	./bin/schema generate --lang unity --out $(CARDS_UNITY_GENERATED) tables/cards
	@touch $@

cards-unity-test: build/mustrun toolchain-unity cards-unity-generate
	$(DOTNET) build tables/cards/unity/Cards.csproj -c Debug --nologo -v quiet
	$(DOTNET) build tables/cards/unity/Cards.csproj -c Release --nologo -v quiet
	$(MUSTRUN) -name cards-unity-test-debug -count 7 -- $(DOTNET) run --project tables/cards/unity/Tests/Tests.csproj -c Debug --nologo
	$(MUSTRUN) -name cards-unity-test-release -count 7 -- $(DOTNET) run --project tables/cards/unity/Tests/Tests.csproj -c Release --nologo

cards-unity-driver: toolchain-unity cards-unity-generate
	@mkdir -p build/cards-driver-unity
	$(DOTNET) build tables/cards/unity/Driver/Driver.csproj -c Release -o build/cards-driver-unity --nologo -v quiet

cards-unity-conformance: build/conformance-harness cards-unity-driver
	./build/conformance-harness cards --work build/cards-unity-work --corpus tables/cards/conformance --driver tables/cards/unity/cards-driver --require $(CARDS_UNITY_SURFACES)

cards-unity-conformance-negative-control: build/conformance-harness cards-unity-driver
	@mkdir -p build
	@if CARDS_UNITY_SABOTAGE=1 ./build/conformance-harness cards --work build/cards-unity-neg-work --corpus tables/cards/conformance --driver tables/cards/unity/cards-driver --require $(CARDS_UNITY_SURFACES) > build/cards-unity-conformance-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: sabotaged Unity card renderer left conformance green'; exit 1; fi
	@grep -qE 'cards render .*FAIL' build/cards-unity-conformance-neg.log || { echo 'NEGATIVE CONTROL FAILED: render did not go red'; cat build/cards-unity-conformance-neg.log; exit 1; }
	@for s in load save parse bounds upgrade edit taskhash view; do \
		grep -qE "cards $$s pass" build/cards-unity-conformance-neg.log || { echo "NEGATIVE CONTROL FAILED: $$s did not stay green"; cat build/cards-unity-conformance-neg.log; exit 1; }; \
	done
	@echo 'cards Unity render negative control: render failed and the other eight surfaces stayed green'

cards-unity-conformance-upgrade-negative-control: build/conformance-harness cards-unity-driver
	@mkdir -p build
	@if CARDS_UNITY_UPGRADE_SABOTAGE=1 ./build/conformance-harness cards --work build/cards-unity-upgrade-neg-work --corpus tables/cards/conformance --driver tables/cards/unity/cards-driver --require $(CARDS_UNITY_SURFACES) > build/cards-unity-conformance-upgrade-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: sabotaged Unity card upgrader left conformance green'; exit 1; fi
	@grep -qE 'cards upgrade .*FAIL' build/cards-unity-conformance-upgrade-neg.log || { echo 'NEGATIVE CONTROL FAILED: upgrade did not go red'; cat build/cards-unity-conformance-upgrade-neg.log; exit 1; }
	@for s in load save render parse bounds edit taskhash view; do \
		grep -qE "cards $$s pass" build/cards-unity-conformance-upgrade-neg.log || { echo "NEGATIVE CONTROL FAILED: $$s did not stay green"; cat build/cards-unity-conformance-upgrade-neg.log; exit 1; }; \
	done
	@echo 'cards Unity upgrade negative control: upgrade failed and the other eight surfaces stayed green'

cards-unity-conformance-hash-negative-control: build/conformance-harness cards-unity-driver
	@mkdir -p build
	@if CARDS_UNITY_HASH_SABOTAGE=1 ./build/conformance-harness cards --work build/cards-unity-hash-neg-work --corpus tables/cards/conformance --driver tables/cards/unity/cards-driver --require $(CARDS_UNITY_SURFACES) > build/cards-unity-conformance-hash-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: sabotaged Unity card taskhash left conformance green'; exit 1; fi
	@grep -qE 'cards taskhash .*FAIL' build/cards-unity-conformance-hash-neg.log || { echo 'NEGATIVE CONTROL FAILED: taskhash did not go red'; cat build/cards-unity-conformance-hash-neg.log; exit 1; }
	@for s in load save render parse bounds upgrade edit view; do \
		grep -qE "cards $$s pass" build/cards-unity-conformance-hash-neg.log || { echo "NEGATIVE CONTROL FAILED: $$s did not stay green"; cat build/cards-unity-conformance-hash-neg.log; exit 1; }; \
	done
	@echo 'cards Unity taskhash negative control: taskhash failed and the other eight surfaces stayed green'

unity-ci-cards: cards-unity-test cards-unity-conformance cards-unity-conformance-negative-control cards-unity-conformance-upgrade-negative-control cards-unity-conformance-hash-negative-control

tables-unity-bench-gate: toolchain-unity bin/schema
	bench/tables/unity/leg build
	bench/tables/unity/leg run --gate

tables-unity-bench-gate-negative-control: toolchain-unity tables-unity-bench-gate
	@rm -rf build/unity-bench-negative && mkdir -p build/unity-bench-negative/golden build/unity-bench-negative/roundtrip
	@cp testdata/wire/bench_table.bin bench/corpus/variants/bench_table.variants.bin build/unity-bench-negative/golden/
	@cp testdata/wire/bench_table.bin bench/corpus/variants/bench_table.variants.bin build/unity-bench-negative/roundtrip/
	@printf '\377' | dd of=build/unity-bench-negative/golden/bench_table.bin bs=1 seek=100 conv=notrunc 2>/dev/null
	@size=$$(wc -c < bench/corpus/variants/bench_table.variants.bin); \
	printf '\377' | dd of=build/unity-bench-negative/roundtrip/bench_table.variants.bin bs=1 seek=$$((size / 64 * 3 + 100)) conv=notrunc 2>/dev/null
	@for c in golden:WIRE\ GOLDEN\ MISMATCH roundtrip:FAILED:\ bench_table; do \
		name=$${c%%:*}; want=$${c#*:*}; want=$${c#*:}; \
		if bench/tables/unity/leg run --gate \
			--wire-dir build/unity-bench-negative/$$name --variant-dir build/unity-bench-negative/$$name \
			> build/unity-bench-negative/$$name.log 2>&1; then \
			echo "NEGATIVE CONTROL FAILED: the $$name corpus passed the Unity bench gate"; exit 1; fi; \
		grep -q "$$want" build/unity-bench-negative/$$name.log || \
			{ echo "NEGATIVE CONTROL FAILED: the $$name corpus went red, but not on $$want"; cat build/unity-bench-negative/$$name.log; exit 1; }; \
	done
	@echo 'tables-unity-bench-gate negative control: a flipped golden byte and a flipped variant byte each fail the gate by name'

tables-unity-standalone: toolchain-unity bin/schema
	@rm -rf build/tables-unity-standalone && mkdir -p build/tables-unity-standalone/gen
	./bin/schema generate --lang unity --out build/tables-unity-standalone/gen tables/pointers
	$(DOTNET) build test/conformance/unity/unit/schemaunit.csproj -c Release \
		-p:UnityUnitDir="$(CURDIR)/build/tables-unity-standalone/gen" \
		-p:ArtifactsPath="$(CURDIR)/build/tables-unity-standalone/art" \
		-o build/tables-unity-standalone/bin --nologo -v quiet >/dev/null
	@test -f build/tables-unity-standalone/bin/schemaunit.dll
	@echo 'tables Unity standalone gate: generated Unity table unit compiles self-contained in strict Unity dialect'

conformance-unity: toolchain-unity build/conformance-harness build/conformance-unity/.stamp
	@mkdir -p build/conformance-unity-run
	@printf 'unity test/conformance/unity/driver\n' > build/conformance-unity-run/drivers
	./build/conformance-harness run --drivers build/conformance-unity-run/drivers --work build/conformance-unity-run/work

conformance-negative-control-unity: toolchain-unity build/conformance-harness build/conformance-unity/.stamp
	@rm -rf build/conformance-unity-nc && mkdir -p build/conformance-unity-nc
	@for d in build/conformance-unity/*/; do \
		name=$$(basename "$$d"); \
		case "$$name" in _*) continue ;; esac; \
		cp -R "$$d" "build/conformance-unity-nc/$$name"; \
	done
	@mkdir -p build/conformance-unity-nc/_gen_tabledemo
	@cp build/conformance-unity/_gen/tabledemo/*.cs build/conformance-unity-nc/_gen_tabledemo/
	@sed -i.orig 's/f.SetRaw(owner, index, 1); return true;/f.SetRaw(owner, index, 0); return true;/' build/conformance-unity-nc/_gen_tabledemo/Tables.Runtime.cs
	@cmp -s build/conformance-unity/_gen/tabledemo/Tables.Runtime.cs build/conformance-unity-nc/_gen_tabledemo/Tables.Runtime.cs && \
		{ echo 'NEGATIVE CONTROL FAILED: Tables.Runtime.cs JSON bool anchor matched nothing'; exit 1; } || true
	@rm -f build/conformance-unity-nc/_gen_tabledemo/*.orig
	@$(DOTNET) build test/conformance/unity/unit/schemaunit.csproj -c Debug \
		-p:UnityUnitDir="$(CURDIR)/build/conformance-unity-nc/_gen_tabledemo" \
		-p:ArtifactsPath="$(CURDIR)/build/conformance-unity-nc/_art_tabledemo" \
		-o build/conformance-unity-nc/tabledemo --nologo -v quiet >/dev/null
	@printf 'unity test/conformance/unity/driver\n' > build/conformance-unity-nc/drivers
	@if UNITY_CONFORMANCE_UNITS=build/conformance-unity-nc ./build/conformance-harness run --drivers build/conformance-unity-nc/drivers --work build/conformance-unity-nc/work > build/conformance-unity-nc/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: sabotaged Unity JSON reader left conformance green'; exit 1; fi
	@grep -Eq '^json-read +FAIL ' build/conformance-unity-nc/log || \
		{ echo 'NEGATIVE CONTROL FAILED: json-read did not go red'; cat build/conformance-unity-nc/log; exit 1; }
	@grep -Eq '^wire +pass ' build/conformance-unity-nc/log || \
		{ echo 'NEGATIVE CONTROL FAILED: wire did not stay green'; cat build/conformance-unity-nc/log; exit 1; }
	@echo 'conformance-negative-control-unity: sabotaged JSON reader caught on json-read while wire stayed green'

tables-unity-wire-fuzz: toolchain-unity build/conformance-harness build/conformance-unity/.stamp
	@mkdir -p build/unity-wire-fuzz
	sh test/conformance/wire-fuzz-complete build/unity-wire-fuzz/plain.log ./build/conformance-harness wire-fuzz \
		--driver 'test/conformance/unity/driver wire-fuzz' --seed $(SEED) --n $(N) --failed build/unity-wire-fuzz/plain.bin
	sh test/conformance/wire-fuzz-complete build/unity-wire-fuzz/retain.log ./build/conformance-harness wire-fuzz --retain \
		--driver 'test/conformance/unity/driver wire-fuzz' --seed $(SEED) --n $(N) --failed build/unity-wire-fuzz/retain.bin

tables-unity-wire-fuzz-negative-control: toolchain-unity build/conformance-harness build/conformance-unity/.stamp
	@rm -rf build/conformance-unity-wf-nc && mkdir -p build/conformance-unity-wf-nc
	@for d in build/conformance-unity/*/; do \
		name=$$(basename "$$d"); \
		case "$$name" in _*) continue ;; esac; \
		cp -R "$$d" "build/conformance-unity-wf-nc/$$name"; \
	done
	@mkdir -p build/conformance-unity-wf-nc/_gen_mapdemo
	@cp build/conformance-unity/_gen/mapdemo/*.cs build/conformance-unity-wf-nc/_gen_mapdemo/
	@sed -i.orig 's/if (count > 4096) { Damage(report); return Finish(report, Verdict.Damaged, retain); }/if (count > 1) { Damage(report); return Finish(report, Verdict.Damaged, retain); }/' build/conformance-unity-wf-nc/_gen_mapdemo/Tables.Runtime.cs
	@cmp -s build/conformance-unity/_gen/mapdemo/Tables.Runtime.cs build/conformance-unity-wf-nc/_gen_mapdemo/Tables.Runtime.cs && \
		{ echo 'NEGATIVE CONTROL FAILED: Tables.Runtime.cs 4096 ID cap anchor matched nothing'; exit 1; } || true
	@rm -f build/conformance-unity-wf-nc/_gen_mapdemo/*.orig
	@$(DOTNET) build test/conformance/unity/unit/schemaunit.csproj -c Debug \
		-p:UnityUnitDir="$(CURDIR)/build/conformance-unity-wf-nc/_gen_mapdemo" \
		-p:ArtifactsPath="$(CURDIR)/build/conformance-unity-wf-nc/_art_mapdemo" \
		-o build/conformance-unity-wf-nc/mapdemo --nologo -v quiet >/dev/null
	@if UNITY_CONFORMANCE_UNITS=build/conformance-unity-wf-nc ./build/conformance-harness wire-fuzz \
		--driver 'test/conformance/unity/driver wire-fuzz' --replay testdata/wire/tables/map_full.bin --unit mapdemo --root Fleet --failed build/conformance-unity-wf-nc/failed.bin > build/conformance-unity-wf-nc/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: sabotaged Unity wire counter passed wire-fuzz'; exit 1; fi
	@grep -q 'FAILED' build/conformance-unity-wf-nc/log || { cat build/conformance-unity-wf-nc/log; exit 1; }
	@echo 'tables-unity-wire-fuzz-negative-control: sabotaged wire report counter detected by wire-fuzz'

tables-unity-release: toolchain-unity bin/schema build/conformance-harness build/conformance-unity/.stamp
	$(MAKE) conformance-unity
	$(MAKE) tables-unity-wire-fuzz N=100
	$(MAKE) tables-wire-fuzz-release-unity
	$(MAKE) tables-unity-wire-fuzz-negative-control
	$(MAKE) conformance-negative-control-unity
	$(MAKE) tables-unity-bench-gate
	$(MAKE) tables-unity-bench-gate-negative-control
	bench/tables/run.sh --only unity --bare
	@echo 'tables unity release gate: release wire-fuzz, negative controls, bench gate, and measured pass'

test-unity: unity-ci-cards tables-unity-bench-gate tables-unity-bench-gate-negative-control tables-unity-standalone
